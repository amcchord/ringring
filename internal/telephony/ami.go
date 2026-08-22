package telephony

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
	"strings"
	"time"

	extensionrules "github.com/amcchord/ringring/internal/extension"
)

const (
	defaultAMITimeout = 5 * time.Second
	contactsActionID  = "ringring-contacts"
	ringActionID      = "ringring-phone-check"
	maxAMIFrames      = 10_000
	maxAMIFrameBytes  = 64 * 1024
)

var amiObjectPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

type ContactState string

const (
	ContactReachable    ContactState = "reachable"
	ContactUnreachable  ContactState = "unreachable"
	ContactNonQualified ContactState = "nonqualified"
	ContactUnknown      ContactState = "unknown"
)

type AMI struct {
	Address  string
	Username string
	Secret   string
	Timeout  time.Duration
}

// RingDevice asks Asterisk to call one validated RingRing endpoint and enter a
// fixed internal-only dialplan context after answer. The caller supplies no
// channel technology, context, application, or arbitrary AMI field.
func (a AMI) RingDevice(ctx context.Context, sipUsername, extension string) error {
	if a.Address == "" || a.Secret == "" {
		return errors.New("AMI is not configured")
	}
	if !amiObjectPattern.MatchString(sipUsername) {
		return errors.New("phone ring target is invalid")
	}
	if !extensionrules.Valid(extension) {
		return errors.New("phone ring extension is invalid")
	}
	connection, reader, err := a.connect(ctx)
	if err != nil {
		return err
	}
	defer connection.Close()

	if err := writeAMIAction(connection, "Originate",
		"ActionID", ringActionID,
		"Channel", "PJSIP/"+sipUsername,
		"Context", "rr-phone-check",
		"Exten", "s",
		"Priority", "1",
		"Timeout", "15000",
		"CallerID", "RingRing setup <"+extension+">",
		"Variable", "RINGRING_EXTENSION="+extension,
		"Async", "true",
	); err != nil {
		return fmt.Errorf("write AMI phone ring: %w", err)
	}
	if err := expectResponse(reader, "Success"); err != nil {
		return fmt.Errorf("AMI phone ring: %w", err)
	}
	_ = writeAMIAction(connection, "Logoff")
	return nil
}

func (a AMI) Reload(ctx context.Context) error {
	if a.Address == "" || a.Secret == "" {
		return nil
	}
	connection, reader, err := a.connect(ctx)
	if err != nil {
		return err
	}
	defer connection.Close()

	if err := writeAMIAction(connection, "Command", "Command", "core reload"); err != nil {
		return fmt.Errorf("write AMI reload: %w", err)
	}
	if err := expectResponse(reader, "Success", "Follows"); err != nil {
		return fmt.Errorf("AMI reload: %w", err)
	}
	_ = writeAMIAction(connection, "Logoff")
	return nil
}

// ContactStatuses returns only RingRing's endpoint identifier and normalized
// reachability state. Contact URIs, addresses, user agents, and other AMI fields
// intentionally remain inside this private protocol boundary.
func (a AMI) ContactStatuses(ctx context.Context) (map[string]ContactState, error) {
	if a.Address == "" || a.Secret == "" {
		return nil, errors.New("AMI is not configured")
	}
	connection, reader, err := a.connect(ctx)
	if err != nil {
		return nil, err
	}
	defer connection.Close()

	if err := writeAMIAction(connection, "PJSIPShowContacts", "ActionID", contactsActionID); err != nil {
		return nil, fmt.Errorf("write AMI contact query: %w", err)
	}

	statuses := make(map[string]ContactState)
	started := false
	for frameNumber := 0; frameNumber < maxAMIFrames; frameNumber++ {
		frame, err := readAMIFrame(reader)
		if err != nil {
			return nil, fmt.Errorf("read AMI contact query: %w", err)
		}
		if actionID := frame["actionid"]; actionID != "" && actionID != contactsActionID {
			continue
		}
		if response := frame["response"]; response != "" {
			if !strings.EqualFold(response, "Success") {
				// Asterisk answers with an error instead of an empty completed
				// list when no SIP device is currently registered.
				if strings.EqualFold(response, "Error") && strings.EqualFold(frame["message"], "No Contacts found") {
					_ = writeAMIAction(connection, "Logoff")
					return statuses, nil
				}
				return nil, fmt.Errorf("AMI contact query response was %s", response)
			}
			started = true
		}
		switch {
		case strings.EqualFold(frame["event"], "ContactList"):
			endpoint := frame["endpoint"]
			if !amiObjectPattern.MatchString(endpoint) {
				continue
			}
			state := normalizeContactState(frame["status"])
			statuses[endpoint] = preferContactState(statuses[endpoint], state)
		case strings.EqualFold(frame["event"], "ContactListComplete"):
			if !started {
				return nil, errors.New("AMI contact query completed before its response")
			}
			_ = writeAMIAction(connection, "Logoff")
			return statuses, nil
		}
	}
	return nil, errors.New("AMI contact query exceeded its frame limit")
}

func (a AMI) connect(ctx context.Context) (net.Conn, *bufio.Reader, error) {
	timeout := a.Timeout
	if timeout <= 0 {
		timeout = defaultAMITimeout
	}
	dialer := net.Dialer{Timeout: timeout}
	connection, err := dialer.DialContext(ctx, "tcp", a.Address)
	if err != nil {
		return nil, nil, fmt.Errorf("connect to AMI: %w", err)
	}
	deadline := time.Now().Add(timeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	if err := connection.SetDeadline(deadline); err != nil {
		_ = connection.Close()
		return nil, nil, fmt.Errorf("set AMI deadline: %w", err)
	}

	reader := bufio.NewReader(connection)
	greeting, err := reader.ReadString('\n')
	if err != nil {
		_ = connection.Close()
		return nil, nil, fmt.Errorf("read AMI greeting: %w", err)
	}
	if !strings.HasPrefix(strings.TrimSpace(greeting), "Asterisk Call Manager/") {
		_ = connection.Close()
		return nil, nil, errors.New("read AMI greeting: unexpected banner")
	}
	if err := writeAMIAction(connection, "Login", "Username", a.Username, "Secret", a.Secret, "Events", "off"); err != nil {
		_ = connection.Close()
		return nil, nil, fmt.Errorf("write AMI login: %w", err)
	}
	if err := expectResponse(reader, "Success"); err != nil {
		_ = connection.Close()
		return nil, nil, fmt.Errorf("AMI login: %w", err)
	}
	return connection, reader, nil
}

func writeAMIAction(writer io.Writer, action string, fields ...string) error {
	if len(fields)%2 != 0 {
		return errors.New("AMI action has an unmatched field")
	}
	values := append([]string{"Action", action}, fields...)
	var message strings.Builder
	for index := 0; index < len(values); index += 2 {
		key, value := values[index], values[index+1]
		if key == "" || strings.ContainsAny(key, ":\r\n") || strings.ContainsAny(value, "\r\n") {
			return errors.New("AMI action contains an invalid field")
		}
		message.WriteString(key)
		message.WriteString(": ")
		message.WriteString(value)
		message.WriteString("\r\n")
	}
	message.WriteString("\r\n")
	_, err := io.WriteString(writer, message.String())
	return err
}

func readAMIFrame(reader *bufio.Reader) (map[string]string, error) {
	frame := make(map[string]string)
	frameBytes := 0
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		frameBytes += len(line)
		if frameBytes > maxAMIFrameBytes {
			return nil, errors.New("AMI frame was too large")
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			return frame, nil
		}
		key, value, ok := strings.Cut(trimmed, ":")
		if !ok {
			continue
		}
		frame[strings.ToLower(strings.TrimSpace(key))] = strings.TrimSpace(value)
	}
}

func expectResponse(reader *bufio.Reader, allowed ...string) error {
	frame, err := readAMIFrame(reader)
	if err != nil {
		return err
	}
	response := frame["response"]
	if response == "" {
		return errors.New("response had no status")
	}
	for _, value := range allowed {
		if strings.EqualFold(response, value) {
			return nil
		}
	}
	return fmt.Errorf("response was %s", response)
}

func normalizeContactState(value string) ContactState {
	switch {
	case strings.EqualFold(value, "Reachable"):
		return ContactReachable
	case strings.EqualFold(value, "Unreachable"):
		return ContactUnreachable
	case strings.EqualFold(value, "NonQualified"):
		return ContactNonQualified
	default:
		return ContactUnknown
	}
}

func preferContactState(current, candidate ContactState) ContactState {
	priority := func(state ContactState) int {
		switch state {
		case ContactReachable:
			return 4
		case ContactNonQualified:
			return 3
		case ContactUnknown:
			return 2
		case ContactUnreachable:
			return 1
		default:
			return 0
		}
	}
	if priority(candidate) > priority(current) {
		return candidate
	}
	return current
}
