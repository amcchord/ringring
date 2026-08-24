package telephony

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	extensionrules "github.com/amcchord/ringring/internal/extension"
)

const (
	defaultAMITimeout     = 5 * time.Second
	contactsActionID      = "ringring-contacts"
	conferenceActionID    = "ringring-conferences"
	channelsActionID      = "ringring-channels"
	ringActionID          = "ringring-phone-check"
	announcementActionID  = "ringring-join-announcement"
	maxAMIFrames          = 10_000
	maxAMIFrameBytes      = 64 * 1024
	maxActiveConferences  = 128
	maxCallElapsedSeconds = 31 * 24 * 60 * 60
)

var (
	amiObjectPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
	playbackPattern  = regexp.MustCompile(`^(?:[A-Za-z0-9_-]{1,80}|/[A-Za-z0-9_/-]{1,240})$`)
)

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

// AnnounceJoin injects one validated sound into an existing RingRing
// conference through a fixed Local channel. The caller cannot choose an AMI
// application, dialplan context, channel technology, or arbitrary variable.
func (a AMI) AnnounceJoin(ctx context.Context, conference, playback string) error {
	if a.Address == "" || a.Secret == "" {
		return errors.New("AMI is not configured")
	}
	if _, _, ok := ParseConferenceName(conference); !ok {
		return errors.New("conference announcement target is invalid")
	}
	if !playbackPattern.MatchString(playback) || strings.Contains(playback, "..") || strings.HasSuffix(playback, "/") {
		return errors.New("conference announcement audio is invalid")
	}
	connection, reader, err := a.connect(ctx)
	if err != nil {
		return err
	}
	defer connection.Close()

	if err := writeAMIAction(connection, "Originate",
		"ActionID", announcementActionID,
		"Channel", "Local/s@rr-party-announcement/n",
		"Application", "Playback",
		"Data", playback,
		"Variable", "RINGRING_CONFERENCE="+conference,
		"Async", "true",
	); err != nil {
		return fmt.Errorf("write AMI join announcement: %w", err)
	}
	if err := expectResponse(reader, "Success"); err != nil {
		return fmt.Errorf("AMI join announcement: %w", err)
	}
	_ = writeAMIAction(connection, "Logoff")
	return nil
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

// ActiveConferenceCalls returns active RingRing conferences, authenticated
// PJSIP endpoint names, and bounded elapsed seconds only. It deliberately drops
// caller ID, channel IDs, connected-line data, addresses, exact timestamps, and
// non-RingRing conferences.
func (a AMI) ActiveConferenceCalls(ctx context.Context) ([]ActiveConference, error) {
	if a.Address == "" || a.Secret == "" {
		return nil, errors.New("AMI is not configured")
	}
	connection, reader, err := a.connect(ctx)
	if err != nil {
		return nil, err
	}
	defer connection.Close()

	if err := writeAMIAction(connection, "ConfbridgeListRooms", "ActionID", conferenceActionID); err != nil {
		return nil, fmt.Errorf("write AMI conference query: %w", err)
	}
	var rooms []ActiveConference
	started := false
	for frameNumber := 0; frameNumber < maxAMIFrames; frameNumber++ {
		frame, err := readAMIFrame(reader)
		if err != nil {
			return nil, fmt.Errorf("read AMI conference query: %w", err)
		}
		if actionID := frame["actionid"]; actionID != "" && actionID != conferenceActionID {
			continue
		}
		if response := frame["response"]; response != "" {
			if strings.EqualFold(response, "Error") && strings.Contains(strings.ToLower(frame["message"]), "no active conference") {
				_ = writeAMIAction(connection, "Logoff")
				return []ActiveConference{}, nil
			}
			if !strings.EqualFold(response, "Success") {
				return nil, fmt.Errorf("AMI conference query response was %s", response)
			}
			started = true
		}
		switch {
		case strings.EqualFold(frame["event"], "ConfbridgeListRooms"):
			partyID, extension, ok := ParseConferenceName(frame["conference"])
			if ok && len(rooms) < maxActiveConferences {
				rooms = append(rooms, ActiveConference{Name: frame["conference"], PartyID: partyID, JoinExtension: extension})
			}
		case strings.EqualFold(frame["event"], "ConfbridgeListRoomsComplete"):
			if !started {
				return nil, errors.New("AMI conference query completed before its response")
			}
			for index := range rooms {
				endpoints, elapsedSeconds, err := listConferenceEndpoints(connection, reader, rooms[index].Name, index)
				if err != nil {
					return nil, err
				}
				rooms[index].Endpoints = endpoints
				rooms[index].ElapsedSeconds = elapsedSeconds
			}
			active := rooms[:0]
			for _, room := range rooms {
				if len(room.Endpoints) >= 2 {
					active = append(active, room)
				}
			}
			_ = writeAMIAction(connection, "Logoff")
			return active, nil
		}
	}
	return nil, errors.New("AMI conference query exceeded its frame limit")
}

// PhoneActivities returns only a PJSIP endpoint, a normalized live state, and
// bounded elapsed seconds. It deliberately drops channel/bridge IDs, caller
// and connected-line values, dialed digits, application data, and exact start
// timestamps.
func (a AMI) PhoneActivities(ctx context.Context) ([]PhoneActivity, error) {
	if a.Address == "" || a.Secret == "" {
		return nil, errors.New("AMI is not configured")
	}
	connection, reader, err := a.connect(ctx)
	if err != nil {
		return nil, err
	}
	defer connection.Close()

	if err := writeAMIAction(connection, "CoreShowChannels", "ActionID", channelsActionID); err != nil {
		return nil, fmt.Errorf("write AMI channel query: %w", err)
	}
	activities := make(map[string]PhoneActivity)
	started := false
	for frameNumber := 0; frameNumber < maxAMIFrames; frameNumber++ {
		frame, err := readAMIFrame(reader)
		if err != nil {
			return nil, fmt.Errorf("read AMI channel query: %w", err)
		}
		if actionID := frame["actionid"]; actionID != "" && actionID != channelsActionID {
			continue
		}
		if response := frame["response"]; response != "" {
			if !strings.EqualFold(response, "Success") {
				return nil, fmt.Errorf("AMI channel query response was %s", response)
			}
			started = true
		}
		switch {
		case strings.EqualFold(frame["event"], "CoreShowChannel"):
			activity, ok := reducedPhoneActivity(frame)
			if !ok {
				continue
			}
			current, exists := activities[activity.Endpoint]
			if !exists || phoneActivityRank(activity.State) > phoneActivityRank(current.State) ||
				(activity.State == current.State && activity.ElapsedSeconds > current.ElapsedSeconds) {
				activities[activity.Endpoint] = activity
			}
		case strings.EqualFold(frame["event"], "CoreShowChannelsComplete"):
			if !started {
				return nil, errors.New("AMI channel query completed before its response")
			}
			result := make([]PhoneActivity, 0, len(activities))
			for _, activity := range activities {
				result = append(result, activity)
			}
			sort.Slice(result, func(i, j int) bool { return result[i].Endpoint < result[j].Endpoint })
			_ = writeAMIAction(connection, "Logoff")
			return result, nil
		}
	}
	return nil, errors.New("AMI channel query exceeded its frame limit")
}

func reducedPhoneActivity(frame map[string]string) (PhoneActivity, bool) {
	endpoint, ok := pjsipEndpoint(frame["channel"])
	if !ok {
		return PhoneActivity{}, false
	}
	application := strings.ToLower(strings.TrimSpace(frame["application"]))
	channelState := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(frame["channelstatedesc"]), " ", ""))
	state := PhoneOffHook
	switch {
	case application == "confbridge":
		state = PhoneInCall
	case application == "dial":
		state = PhoneCalling
	case channelState == "ring" || channelState == "ringing" || application == "appdial":
		state = PhoneRinging
	case channelState == "up":
		state = PhoneInCall
	case strings.Contains(channelState, "dial"):
		state = PhoneCalling
	}
	return PhoneActivity{Endpoint: endpoint, State: state, ElapsedSeconds: parseAMIDuration(frame["duration"])}, true
}

func phoneActivityRank(state PhoneActivityState) int {
	switch state {
	case PhoneInCall:
		return 4
	case PhoneCalling:
		return 3
	case PhoneRinging:
		return 2
	case PhoneOffHook:
		return 1
	default:
		return 0
	}
}

func parseAMIDuration(value string) int {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 3 {
		return 0
	}
	values := make([]int, 3)
	for index, part := range parts {
		parsed, err := strconv.Atoi(part)
		if err != nil || parsed < 0 || (index > 0 && parsed > 59) {
			return 0
		}
		values[index] = parsed
	}
	seconds := values[0]*60*60 + values[1]*60 + values[2]
	if seconds > maxCallElapsedSeconds {
		return maxCallElapsedSeconds
	}
	return seconds
}

func parseAMISeconds(value string) int {
	seconds, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || seconds < 0 {
		return 0
	}
	if seconds > maxCallElapsedSeconds {
		return maxCallElapsedSeconds
	}
	return seconds
}

func listConferenceEndpoints(connection net.Conn, reader *bufio.Reader, conference string, index int) ([]string, int, error) {
	actionID := fmt.Sprintf("ringring-conference-%d", index)
	if err := writeAMIAction(connection, "ConfbridgeList", "ActionID", actionID, "Conference", conference); err != nil {
		return nil, 0, fmt.Errorf("write AMI conference participant query: %w", err)
	}
	started := false
	seen := make(map[string]struct{})
	var endpoints []string
	elapsedSeconds := 0
	for frameNumber := 0; frameNumber < maxAMIFrames; frameNumber++ {
		frame, err := readAMIFrame(reader)
		if err != nil {
			return nil, 0, fmt.Errorf("read AMI conference participant query: %w", err)
		}
		if id := frame["actionid"]; id != "" && id != actionID {
			continue
		}
		if response := frame["response"]; response != "" {
			if strings.EqualFold(response, "Error") {
				return []string{}, 0, nil
			}
			if !strings.EqualFold(response, "Success") {
				return nil, 0, fmt.Errorf("AMI conference participant query response was %s", response)
			}
			started = true
		}
		switch {
		case strings.EqualFold(frame["event"], "ConfbridgeList"):
			endpoint, ok := pjsipEndpoint(frame["channel"])
			if ok {
				if answered := parseAMISeconds(frame["answeredtime"]); answered > elapsedSeconds {
					elapsedSeconds = answered
				}
				if _, exists := seen[endpoint]; !exists {
					seen[endpoint] = struct{}{}
					endpoints = append(endpoints, endpoint)
				}
			}
		case strings.EqualFold(frame["event"], "ConfbridgeListComplete"):
			if !started {
				return nil, 0, errors.New("AMI conference participant query completed before its response")
			}
			return endpoints, elapsedSeconds, nil
		}
	}
	return nil, 0, errors.New("AMI conference participant query exceeded its frame limit")
}

func pjsipEndpoint(channel string) (string, bool) {
	if !strings.HasPrefix(channel, "PJSIP/") {
		return "", false
	}
	value := strings.TrimPrefix(channel, "PJSIP/")
	separator := strings.LastIndexByte(value, '-')
	if separator <= 0 {
		return "", false
	}
	endpoint := value[:separator]
	return endpoint, amiObjectPattern.MatchString(endpoint)
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
