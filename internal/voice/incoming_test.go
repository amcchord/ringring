package voice

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/amcchord/ringring/internal/apns"
	"github.com/amcchord/ringring/internal/model"
	"github.com/amcchord/ringring/internal/secure"
	"github.com/amcchord/ringring/internal/store"
)

type fakePhonePushSource struct {
	member        model.Member
	authError     error
	registrations []store.PhonePushRegistration
	deletedHash   []byte
	partyID       string
	caller        string
	extension     string
}

func (source *fakePhonePushSource) PartyMemberForDevice(_ context.Context, partyID, caller string) (model.Member, error) {
	source.partyID = partyID
	source.caller = caller
	return source.member, source.authError
}

func (source *fakePhonePushSource) PhonePushRegistrationsForExtension(_ context.Context, partyID, extension string) ([]store.PhonePushRegistration, error) {
	source.partyID = partyID
	source.extension = extension
	return source.registrations, nil
}

func (source *fakePhonePushSource) DeletePhonePushRegistrationByHash(_ context.Context, hash []byte) error {
	source.deletedHash = append([]byte(nil), hash...)
	return nil
}

type fakePhonePushNotifier struct {
	token  string
	callID string
	result apns.SendResult
	err    error
}

type fakeDecryptor struct{}

func (*fakeDecryptor) Decrypt(string, []byte) (string, error) { return "", nil }

func (notifier *fakePhonePushNotifier) SendVoIP(_ context.Context, token, callID string) (apns.SendResult, error) {
	notifier.token = token
	notifier.callID = callID
	return notifier.result, notifier.err
}

func TestIncomingCallPushAuthenticatesCallerAndWakesOnlyTargetExtension(t *testing.T) {
	cipher, err := secure.NewCipher(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := cipher.Encrypt("ab"+string(bytes.Repeat([]byte{'c'}, 62)), []byte("phone-push:dev_target"))
	if err != nil {
		t.Fatal(err)
	}
	source := &fakePhonePushSource{
		member: model.Member{PartyID: "pty_family", Extension: "101"},
		registrations: []store.PhonePushRegistration{{
			DeviceID: "dev_target", TokenHash: bytes.Repeat([]byte{3}, 32), TokenCiphertext: ciphertext,
			Environment: "production", UpdatedAt: time.Now(),
		}},
	}
	notifier := &fakePhonePushNotifier{}
	server := &Server{PhonePushes: source, PushNotifier: notifier, PushEnvironment: "production", Cipher: cipher}
	commands := &bytes.Buffer{}
	server.handleIncomingCallPush(scriptedAGI("0", "0"), bufio.NewWriter(commands), map[string]string{
		"agi_arg_1": "pty_family", "agi_arg_2": "sip_caller", "agi_arg_3": "102",
		"agi_arg_4": "4cdb5b42-d53d-4f43-9151-bd33a5324ed7",
	})
	if commands.String() != "SET VARIABLE RINGRING_PUSH_SENT 0\nSET VARIABLE RINGRING_PUSH_SENT 1\n" {
		t.Fatalf("unexpected FastAGI commands:\n%s", commands.String())
	}
	if source.partyID != "pty_family" || source.caller != "sip_caller" || source.extension != "102" {
		t.Fatalf("push boundary = %#v", source)
	}
	if notifier.callID != "4cdb5b42-d53d-4f43-9151-bd33a5324ed7" || len(notifier.token) != 64 {
		t.Fatalf("notification = %#v", notifier)
	}
}

func TestIncomingCallPushFailsClosedForUnauthenticatedCaller(t *testing.T) {
	source := &fakePhonePushSource{authError: errors.New("not found")}
	notifier := &fakePhonePushNotifier{}
	server := &Server{PhonePushes: source, PushNotifier: notifier, PushEnvironment: "production", Cipher: &fakeDecryptor{}}
	commands := &bytes.Buffer{}
	server.handleIncomingCallPush(scriptedAGI("0"), bufio.NewWriter(commands), map[string]string{
		"agi_arg_1": "pty_family", "agi_arg_2": "sip_other", "agi_arg_3": "102",
		"agi_arg_4": "4cdb5b42-d53d-4f43-9151-bd33a5324ed7",
	})
	if commands.String() != "SET VARIABLE RINGRING_PUSH_SENT 0\n" || notifier.token != "" || source.extension != "" {
		t.Fatalf("unauthenticated caller reached push delivery: %#v %#v\n%s", source, notifier, commands.String())
	}
}

func TestIncomingCallPushDropsTokenAppleMarksUnregistered(t *testing.T) {
	cipher, err := secure.NewCipher(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := cipher.Encrypt("ab"+string(bytes.Repeat([]byte{'c'}, 62)), []byte("phone-push:dev_target"))
	if err != nil {
		t.Fatal(err)
	}
	tokenHash := bytes.Repeat([]byte{7}, 32)
	source := &fakePhonePushSource{
		member: model.Member{},
		registrations: []store.PhonePushRegistration{{
			DeviceID: "dev_target", TokenHash: tokenHash, TokenCiphertext: ciphertext,
			Environment: "production", UpdatedAt: time.Now(),
		}},
	}
	server := &Server{
		PhonePushes: source, PushNotifier: &fakePhonePushNotifier{result: apns.SendResult{Unregistered: true}},
		PushEnvironment: "production", Cipher: cipher,
	}
	commands := &bytes.Buffer{}
	server.handleIncomingCallPush(scriptedAGI("0"), bufio.NewWriter(commands), map[string]string{
		"agi_arg_1": "pty_family", "agi_arg_2": "sip_caller", "agi_arg_3": "102",
		"agi_arg_4": "4cdb5b42-d53d-4f43-9151-bd33a5324ed7",
	})
	if !bytes.Equal(source.deletedHash, tokenHash) || commands.String() != "SET VARIABLE RINGRING_PUSH_SENT 0\n" {
		t.Fatalf("dead token was not removed: %#v\n%s", source, commands.String())
	}
}
