package telephony

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func TestAMIContactStatuses(t *testing.T) {
	address, finished := serveAMI(t, func(connection net.Conn, reader *bufio.Reader) error {
		if err := acceptTestLogin(connection, reader); err != nil {
			return err
		}
		action, err := readAMIFrame(reader)
		if err != nil {
			return err
		}
		if action["action"] != "PJSIPShowContacts" || action["actionid"] != contactsActionID {
			return fmt.Errorf("unexpected contact action: %#v", action)
		}
		_, err = io.WriteString(connection, strings.Join([]string{
			"Response: Success\r\nActionID: ringring-contacts\r\nEventList: start\r\n\r\n",
			"Event: ContactList\r\nActionID: ringring-contacts\r\nEndpoint: rrd_alpha\r\nStatus: Unreachable\r\nUri: sip:secret-address@example.test\r\nUserAgent: private test phone\r\n\r\n",
			"Event: ContactList\r\nActionID: ringring-contacts\r\nEndpoint: rrd_alpha\r\nStatus: Reachable\r\n\r\n",
			"Event: ContactList\r\nActionID: ringring-contacts\r\nEndpoint: rrd_beta\r\nStatus: NonQualified\r\n\r\n",
			"Event: ContactList\r\nActionID: ringring-contacts\r\nEndpoint: rrd_gamma\r\nStatus: FutureStatus\r\n\r\n",
			"Event: ContactList\r\nActionID: ringring-contacts\r\nEndpoint: invalid/endpoint\r\nStatus: Reachable\r\n\r\n",
			"Event: ContactListComplete\r\nActionID: ringring-contacts\r\nListItems: 5\r\n\r\n",
		}, ""))
		return err
	})

	statuses, err := (AMI{Address: address, Username: "ringring", Secret: "test-only-secret", Timeout: time.Second}).ContactStatuses(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	want := map[string]ContactState{
		"rrd_alpha": ContactReachable,
		"rrd_beta":  ContactNonQualified,
		"rrd_gamma": ContactUnknown,
	}
	if len(statuses) != len(want) {
		t.Fatalf("statuses = %#v, want %#v", statuses, want)
	}
	for endpoint, state := range want {
		if statuses[endpoint] != state {
			t.Errorf("status for %s = %q, want %q", endpoint, statuses[endpoint], state)
		}
	}
}

func TestAMIContactStatusesFailsClosed(t *testing.T) {
	address, finished := serveAMI(t, func(connection net.Conn, reader *bufio.Reader) error {
		if err := acceptTestLogin(connection, reader); err != nil {
			return err
		}
		if _, err := readAMIFrame(reader); err != nil {
			return err
		}
		_, err := io.WriteString(connection, "Response: Error\r\nActionID: ringring-contacts\r\nMessage: Permission denied\r\n\r\n")
		return err
	})

	_, err := (AMI{Address: address, Username: "ringring", Secret: "test-only-secret", Timeout: time.Second}).ContactStatuses(t.Context())
	if err == nil || !strings.Contains(err.Error(), "response was Error") {
		t.Fatalf("ContactStatuses error = %v", err)
	}
	if strings.Contains(err.Error(), "test-only-secret") {
		t.Fatal("AMI error disclosed the login secret")
	}
	if serverErr := <-finished; serverErr != nil {
		t.Fatal(serverErr)
	}
}

func TestAMIContactStatusesAcceptsAsterisksEmptyListResponse(t *testing.T) {
	address, finished := serveAMI(t, func(connection net.Conn, reader *bufio.Reader) error {
		if err := acceptTestLogin(connection, reader); err != nil {
			return err
		}
		if _, err := readAMIFrame(reader); err != nil {
			return err
		}
		_, err := io.WriteString(connection, "Response: Error\r\nActionID: ringring-contacts\r\nMessage: No Contacts found\r\n\r\n")
		return err
	})

	statuses, err := (AMI{Address: address, Username: "ringring", Secret: "test-only-secret", Timeout: time.Second}).ContactStatuses(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 0 {
		t.Fatalf("statuses = %#v, want empty", statuses)
	}
	if serverErr := <-finished; serverErr != nil {
		t.Fatal(serverErr)
	}
}

func TestAMIReload(t *testing.T) {
	address, finished := serveAMI(t, func(connection net.Conn, reader *bufio.Reader) error {
		if err := acceptTestLogin(connection, reader); err != nil {
			return err
		}
		action, err := readAMIFrame(reader)
		if err != nil {
			return err
		}
		if action["action"] != "Command" || action["command"] != "core reload" {
			return fmt.Errorf("unexpected reload action: %#v", action)
		}
		_, err = io.WriteString(connection, "Response: Follows\r\nPrivilege: Command\r\nOutput: All modules reloaded\r\n\r\n")
		return err
	})

	if err := (AMI{Address: address, Username: "ringring", Secret: "test-only-secret", Timeout: time.Second}).Reload(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
}

func TestAMIRingDeviceUsesOnlyValidatedInternalOriginate(t *testing.T) {
	address, finished := serveAMI(t, func(connection net.Conn, reader *bufio.Reader) error {
		if err := acceptTestLogin(connection, reader); err != nil {
			return err
		}
		action, err := readAMIFrame(reader)
		if err != nil {
			return err
		}
		want := map[string]string{
			"action": "Originate", "actionid": ringActionID,
			"channel": "PJSIP/rrd_alpha", "context": "rr-phone-check",
			"exten": "s", "priority": "1", "timeout": "15000",
			"callerid": "RingRing setup <101>", "variable": "RINGRING_EXTENSION=101", "async": "true",
		}
		for key, expected := range want {
			if action[key] != expected {
				return fmt.Errorf("AMI phone ring %s = %q, want %q", key, action[key], expected)
			}
		}
		if len(action) != len(want) {
			return fmt.Errorf("AMI phone ring included unexpected fields: %#v", action)
		}
		_, err = io.WriteString(connection, "Response: Success\r\nActionID: ringring-phone-check\r\nMessage: Originate successfully queued\r\n\r\n")
		return err
	})

	if err := (AMI{Address: address, Username: "ringring", Secret: "test-only-secret", Timeout: time.Second}).RingDevice(t.Context(), "rrd_alpha", "101"); err != nil {
		t.Fatal(err)
	}
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
}

func TestAMIRingDeviceRejectsUntrustedTargetsBeforeConnecting(t *testing.T) {
	ami := AMI{Address: "127.0.0.1:1", Username: "ringring", Secret: "test-only-secret"}
	for _, test := range []struct {
		username  string
		extension string
	}{
		{username: "rrd_safe\r\nChannel: Local/911", extension: "101"},
		{username: "PJSIP/rrd_safe", extension: "101"},
		{username: "rrd_safe", extension: "911"},
		{username: "rrd_safe", extension: "10a"},
	} {
		if err := ami.RingDevice(t.Context(), test.username, test.extension); err == nil || strings.Contains(err.Error(), "connect to AMI") {
			t.Errorf("RingDevice(%q, %q) reached AMI or succeeded: %v", test.username, test.extension, err)
		}
	}
}

func TestAMIAnnounceJoinUsesOnlyValidatedInternalOriginate(t *testing.T) {
	address, finished := serveAMI(t, func(connection net.Conn, reader *bufio.Reader) error {
		if err := acceptTestLogin(connection, reader); err != nil {
			return err
		}
		action, err := readAMIFrame(reader)
		if err != nil {
			return err
		}
		want := map[string]string{
			"action": "Originate", "actionid": announcementActionID,
			"channel": "Local/s@rr-party-announcement/n", "application": "Playback",
			"data":     "/var/lib/ringring/asterisk/audio/join_abc123",
			"variable": "RINGRING_CONFERENCE=rrc-pty_alpha-102", "async": "true",
		}
		for key, expected := range want {
			if action[key] != expected {
				return fmt.Errorf("AMI announcement %s = %q, want %q", key, action[key], expected)
			}
		}
		if len(action) != len(want) {
			return fmt.Errorf("AMI announcement included unexpected fields: %#v", action)
		}
		_, err = io.WriteString(connection, "Response: Success\r\nActionID: ringring-join-announcement\r\nMessage: Originate successfully queued\r\n\r\n")
		return err
	})

	ami := AMI{Address: address, Username: "ringring", Secret: "test-only-secret", Timeout: time.Second}
	if err := ami.AnnounceJoin(t.Context(), "rrc-pty_alpha-102", "/var/lib/ringring/asterisk/audio/join_abc123"); err != nil {
		t.Fatal(err)
	}
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
}

func TestAMIAnnounceJoinRejectsUntrustedInputBeforeConnecting(t *testing.T) {
	ami := AMI{Address: "127.0.0.1:1", Username: "ringring", Secret: "test-only-secret"}
	for _, test := range []struct{ conference, playback string }{
		{"other-party-room", "beep"},
		{"rrc-pty_alpha-102\r\nApplication: System", "beep"},
		{"rrc-pty_alpha-102", "../../private"},
		{"rrc-pty_alpha-102", "beep\r\nVariable: unsafe"},
	} {
		if err := ami.AnnounceJoin(t.Context(), test.conference, test.playback); err == nil || strings.Contains(err.Error(), "connect to AMI") {
			t.Errorf("AnnounceJoin(%q, %q) reached AMI or succeeded: %v", test.conference, test.playback, err)
		}
	}
}

func TestAMIActiveConferenceCallsReturnsOnlyRingRingPJSIPEndpoints(t *testing.T) {
	address, finished := serveAMI(t, func(connection net.Conn, reader *bufio.Reader) error {
		if err := acceptTestLogin(connection, reader); err != nil {
			return err
		}
		action, err := readAMIFrame(reader)
		if err != nil {
			return err
		}
		if action["action"] != "ConfbridgeListRooms" || action["actionid"] != conferenceActionID {
			return fmt.Errorf("unexpected conference action: %#v", action)
		}
		if _, err := io.WriteString(connection, strings.Join([]string{
			"Response: Success\r\nActionID: ringring-conferences\r\nEventList: start\r\n\r\n",
			"Event: ConfbridgeListRooms\r\nActionID: ringring-conferences\r\nConference: rrc-pty_alpha-102\r\nParties: 3\r\n\r\n",
			"Event: ConfbridgeListRooms\r\nActionID: ringring-conferences\r\nConference: rrc-pty_alpha-103\r\nParties: 1\r\n\r\n",
			"Event: ConfbridgeListRooms\r\nActionID: ringring-conferences\r\nConference: customer-conference\r\nParties: 20\r\n\r\n",
			"Event: ConfbridgeListRoomsComplete\r\nActionID: ringring-conferences\r\nListItems: 3\r\n\r\n",
		}, "")); err != nil {
			return err
		}
		for index, participantFrames := range [][]string{
			{
				"Event: ConfbridgeList\r\nChannel: PJSIP/rrd_alpha-00000001\r\nCallerIDName: Private Alpha\r\n\r\n",
				"Event: ConfbridgeList\r\nChannel: PJSIP/rrd_beta-00000002\r\nCallerIDName: Private Beta\r\n\r\n",
				"Event: ConfbridgeList\r\nChannel: Local/s@rr-party-announcement-00000003;2\r\n\r\n",
			},
			{
				"Event: ConfbridgeList\r\nChannel: PJSIP/rrd_gamma-00000004\r\nCallerIDName: Private Gamma\r\n\r\n",
			},
		} {
			action, err := readAMIFrame(reader)
			if err != nil {
				return err
			}
			actionID := fmt.Sprintf("ringring-conference-%d", index)
			if action["action"] != "ConfbridgeList" || action["actionid"] != actionID {
				return fmt.Errorf("unexpected participant action: %#v", action)
			}
			if _, err := io.WriteString(connection, "Response: Success\r\nActionID: "+actionID+"\r\nEventList: start\r\n\r\n"); err != nil {
				return err
			}
			for _, frame := range participantFrames {
				frame = strings.Replace(frame, "\r\n\r\n", "\r\nActionID: "+actionID+"\r\n\r\n", 1)
				if _, err := io.WriteString(connection, frame); err != nil {
					return err
				}
			}
			if _, err := io.WriteString(connection, "Event: ConfbridgeListComplete\r\nActionID: "+actionID+"\r\nListItems: 1\r\n\r\n"); err != nil {
				return err
			}
		}
		return nil
	})

	calls, err := (AMI{Address: address, Username: "ringring", Secret: "test-only-secret", Timeout: time.Second}).ActiveConferenceCalls(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].PartyID != "pty_alpha" || calls[0].JoinExtension != "102" || strings.Join(calls[0].Endpoints, ",") != "rrd_alpha,rrd_beta" {
		t.Fatalf("active conference calls = %#v", calls)
	}
}

func TestAMIActiveConferenceCallsAcceptsAsteriskEmptyResponse(t *testing.T) {
	address, finished := serveAMI(t, func(connection net.Conn, reader *bufio.Reader) error {
		if err := acceptTestLogin(connection, reader); err != nil {
			return err
		}
		if _, err := readAMIFrame(reader); err != nil {
			return err
		}
		_, err := io.WriteString(connection, "Response: Error\r\nActionID: ringring-conferences\r\nMessage: No active conferences.\r\n\r\n")
		return err
	})
	calls, err := (AMI{Address: address, Username: "ringring", Secret: "test-only-secret", Timeout: time.Second}).ActiveConferenceCalls(t.Context())
	if err != nil || len(calls) != 0 {
		t.Fatalf("empty active conferences = %#v, %v", calls, err)
	}
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
}

func TestAMIContactStatusesRejectsIncompleteList(t *testing.T) {
	address, finished := serveAMI(t, func(connection net.Conn, reader *bufio.Reader) error {
		if err := acceptTestLogin(connection, reader); err != nil {
			return err
		}
		if _, err := readAMIFrame(reader); err != nil {
			return err
		}
		_, err := io.WriteString(connection, "Response: Success\r\nActionID: ringring-contacts\r\nEventList: start\r\n\r\n")
		return err
	})

	_, err := (AMI{Address: address, Username: "ringring", Secret: "test-only-secret", Timeout: time.Second}).ContactStatuses(t.Context())
	if err == nil || !strings.Contains(err.Error(), "read AMI contact query") {
		t.Fatalf("ContactStatuses error = %v", err)
	}
	if serverErr := <-finished; serverErr != nil {
		t.Fatal(serverErr)
	}
}

func TestWriteAMIActionRejectsLineInjection(t *testing.T) {
	var output strings.Builder
	err := writeAMIAction(&output, "Login", "Secret", "safe\r\nAction: Command")
	if err == nil || output.Len() != 0 {
		t.Fatalf("writeAMIAction = %q, %v", output.String(), err)
	}
}

func serveAMI(t *testing.T, handler func(net.Conn, *bufio.Reader) error) (string, <-chan error) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() {
		defer close(finished)
		connection, err := listener.Accept()
		if err != nil {
			finished <- err
			return
		}
		defer connection.Close()
		_ = connection.SetDeadline(time.Now().Add(2 * time.Second))
		if _, err := io.WriteString(connection, "Asterisk Call Manager/9.9.9\r\n"); err != nil {
			finished <- err
			return
		}
		finished <- handler(connection, bufio.NewReader(connection))
	}()
	t.Cleanup(func() { _ = listener.Close() })
	return listener.Addr().String(), finished
}

func acceptTestLogin(connection net.Conn, reader *bufio.Reader) error {
	login, err := readAMIFrame(reader)
	if err != nil {
		return err
	}
	if login["action"] != "Login" || login["username"] != "ringring" || login["secret"] != "test-only-secret" || login["events"] != "off" {
		return fmt.Errorf("unexpected login fields: %#v", login)
	}
	_, err = io.WriteString(connection, "Response: Success\r\nMessage: Authentication accepted\r\n\r\n")
	return err
}

func TestAMIContactStatusesHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := (AMI{Address: "127.0.0.1:1", Username: "ringring", Secret: "test-only-secret"}).ContactStatuses(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ContactStatuses error = %v, want context canceled", err)
	}
}
