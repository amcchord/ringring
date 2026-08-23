package store

import (
	"bytes"
	"context"
	"testing"
	"time"
)

func TestPhonePushRegistrationStaysDeviceAndPartyScoped(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	seedPhonePushDevice(t, database, "pty_one", "mem_one", "dev_one", "101", "sip_one", "cipher-one")
	seedPhonePushDevice(t, database, "pty_two", "mem_two", "dev_two", "101", "sip_two", "cipher-two")

	now := time.Unix(1_800_000_000, 0)
	first := PhonePushRegistration{
		DeviceID: "dev_one", TokenHash: bytes.Repeat([]byte{1}, 32), TokenCiphertext: "push-one",
		Environment: "production", UpdatedAt: now,
	}
	if err := database.SavePhonePushRegistration(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	if err := database.SavePhonePushRegistration(context.Background(), PhonePushRegistration{
		DeviceID: "dev_two", TokenHash: bytes.Repeat([]byte{2}, 32), TokenCiphertext: "push-two",
		Environment: "development", UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	registrations, err := database.PhonePushRegistrationsForExtension(context.Background(), "pty_one", "101")
	if err != nil {
		t.Fatal(err)
	}
	if len(registrations) != 1 || registrations[0].DeviceID != "dev_one" || registrations[0].TokenCiphertext != "push-one" {
		t.Fatalf("party-scoped registrations = %#v", registrations)
	}
	device, err := database.PhoneDeviceBySIPUsername(context.Background(), "sip_one")
	if err != nil {
		t.Fatal(err)
	}
	if device.DeviceID != "dev_one" || device.PartyID != "pty_one" || device.SIPSecretCiphertext != "cipher-one" {
		t.Fatalf("authenticated device = %#v", device)
	}
}

func TestPhonePushRegistrationIsReplacedAndDeadHashCanBeRemoved(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	seedPhonePushDevice(t, database, "pty_one", "mem_one", "dev_one", "101", "sip_one", "cipher-one")
	now := time.Unix(1_800_000_000, 0)
	oldHash := bytes.Repeat([]byte{1}, 32)
	newHash := bytes.Repeat([]byte{2}, 32)
	for _, registration := range []PhonePushRegistration{
		{DeviceID: "dev_one", TokenHash: oldHash, TokenCiphertext: "old", Environment: "production", UpdatedAt: now},
		{DeviceID: "dev_one", TokenHash: newHash, TokenCiphertext: "new", Environment: "production", UpdatedAt: now.Add(time.Minute)},
	} {
		if err := database.SavePhonePushRegistration(context.Background(), registration); err != nil {
			t.Fatal(err)
		}
	}
	if err := database.DeletePhonePushRegistrationByHash(context.Background(), newHash); err != nil {
		t.Fatal(err)
	}
	registrations, err := database.PhonePushRegistrationsForExtension(context.Background(), "pty_one", "101")
	if err != nil || len(registrations) != 0 {
		t.Fatalf("registrations = %#v, error = %v", registrations, err)
	}
}

func seedPhonePushDevice(t *testing.T, database *Store, partyID, memberID, deviceID, extension, username, secret string) {
	t.Helper()
	now := time.Now().Unix()
	if _, err := database.db.Exec(`INSERT OR IGNORE INTO users (id, google_subject, email, name, created_at) VALUES ('usr', 'subject', 'host@example.test', 'Host', ?)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := database.db.Exec(`INSERT INTO parties (id, name, slug, host_user_id, openai_status, created_at) VALUES (?, ?, ?, 'usr', 'pending', ?)`, partyID, partyID, partyID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := database.db.Exec(`INSERT INTO members (id, party_id, display_name, extension, created_at) VALUES (?, ?, ?, ?, ?)`, memberID, partyID, memberID, extension, now); err != nil {
		t.Fatal(err)
	}
	if _, err := database.db.Exec(`INSERT INTO devices (id, member_id, label, sip_username, sip_secret_ciphertext, created_at) VALUES (?, ?, 'iPhone', ?, ?, ?)`, deviceID, memberID, username, secret, now); err != nil {
		t.Fatal(err)
	}
}
