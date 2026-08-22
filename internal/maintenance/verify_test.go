package maintenance

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amcchord/ringring/internal/secure"
	"github.com/amcchord/ringring/internal/store"
)

func TestVerifyStateChecksStructureAndEncryptedCredentials(t *testing.T) {
	path, key, partyCiphertext, deviceCiphertext := verificationFixture(t)
	report, err := VerifyState(context.Background(), path, key)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "ok" || report.Integrity != "ok" || report.ForeignKeys != "ok" {
		t.Fatalf("unexpected verification status: %+v", report)
	}
	if report.Users != 1 || report.Parties != 1 || report.Members != 1 || report.Devices != 1 || report.Invitations != 1 || report.Sessions != 1 || report.RecoveryCodes != 1 || report.Provisioning != 1 || report.PhoneChecks != 1 {
		t.Fatalf("unexpected restored counts: %+v", report)
	}
	if report.PartyKeys != 1 || report.DeviceSecrets != 1 {
		t.Fatalf("encrypted credentials were not all verified: %+v", report)
	}
	formatted := strings.Join([]string{report.Status, report.Integrity, report.ForeignKeys}, " ")
	if strings.Contains(formatted, partyCiphertext) || strings.Contains(formatted, deviceCiphertext) {
		t.Fatal("verification report exposed a ciphertext")
	}
}

func TestVerifyStateRejectsWrongMasterKeyWithoutSecretDetails(t *testing.T) {
	path, _, partyCiphertext, deviceCiphertext := verificationFixture(t)
	_, err := VerifyState(context.Background(), path, make([]byte, 32))
	if err == nil {
		t.Fatal("expected the wrong master key to fail")
	}
	if strings.Contains(err.Error(), partyCiphertext) || strings.Contains(err.Error(), deviceCiphertext) || strings.Contains(err.Error(), "party-key") || strings.Contains(err.Error(), "sip-password") {
		t.Fatalf("verification error exposed secret material: %v", err)
	}
}

func TestVerifyStateRejectsCorruptDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ringring.db")
	if err := os.WriteFile(path, []byte("not a sqlite database"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyState(context.Background(), path, make([]byte, 32)); err == nil {
		t.Fatal("expected corrupt state to fail verification")
	}
}

func TestVerifyStateRejectsUnknownRadioStationWithoutEchoingIt(t *testing.T) {
	path, key, _, _ := verificationFixture(t)
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	const unsupported = "http://private.example.test/radio"
	if _, err := database.Exec(`UPDATE party_services SET radio_station = ?`, unsupported); err != nil {
		database.Close()
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = VerifyState(context.Background(), path, key)
	if err == nil || strings.Contains(err.Error(), unsupported) {
		t.Fatalf("unexpected radio verification error: %v", err)
	}
}

func TestVerifyStateReadsRequiredSchemaFromLiveWAL(t *testing.T) {
	path, key, _, _ := verificationFixture(t)
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.Exec(`PRAGMA wal_autocheckpoint = 0`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`DROP TABLE device_readiness`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		CREATE TABLE device_readiness (
			device_id TEXT PRIMARY KEY REFERENCES devices(id) ON DELETE CASCADE,
			echo_tested_at INTEGER CHECK(echo_tested_at IS NULL OR echo_tested_at > 0),
			outgoing_call_tested_at INTEGER CHECK(outgoing_call_tested_at IS NULL OR outgoing_call_tested_at > 0),
			incoming_call_tested_at INTEGER CHECK(incoming_call_tested_at IS NULL OR incoming_call_tested_at > 0),
			updated_at INTEGER NOT NULL CHECK(updated_at > 0)
		)`); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path + "-wal"); err != nil || info.Size() == 0 {
		t.Fatalf("fixture did not retain a live WAL: info=%v error=%v", info, err)
	}

	report, err := VerifyState(t.Context(), path, key)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "ok" || report.PhoneChecks != 0 || report.Devices != 1 {
		t.Fatalf("unexpected WAL-aware verification report: %+v", report)
	}
}

func TestVerifyStateReadsCheckpointedBackupFromReadOnlyDirectory(t *testing.T) {
	path, key, _, _ := verificationFixture(t)
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(path + suffix); !os.IsNotExist(err) {
			t.Fatalf("checkpointed fixture retained %s sidecar: %v", suffix, err)
		}
	}
	directory := filepath.Dir(path)
	if err := os.Chmod(directory, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(directory, 0o700); err != nil {
			t.Errorf("restore fixture directory permissions: %v", err)
		}
	})

	report, err := VerifyState(t.Context(), path, key)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "ok" || report.PhoneChecks != 1 || report.Devices != 1 {
		t.Fatalf("unexpected immutable backup report: %+v", report)
	}
}

func verificationFixture(t *testing.T) (string, []byte, string, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ringring.db")
	database, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.August, 22, 4, 0, 0, 0, time.UTC)
	if _, err := database.CreateLocalUser(context.Background(), store.NewLocalUser{
		ID: "usr_backup", Name: "Backup Host", Username: "backup-host", PasswordHash: "password-hash",
		RecoveryCodeHashes: [][]byte{secure.Hash("recovery")}, SessionTokenHash: secure.Hash("session"),
		SessionExpiresAt: now.Add(time.Hour), CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateParty(context.Background(), store.NewParty{
		ID: "pty_backup", Name: "Backup Party", Slug: "backup-party", HostUserID: "usr_backup", CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	key := []byte("0123456789abcdef0123456789abcdef")
	cipher, err := secure.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	partyCiphertext, err := cipher.Encrypt("party-key", []byte("pty_backup"))
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdatePartyOpenAI(context.Background(), "pty_backup", "project", "service", "key", partyCiphertext, "ready", 1000); err != nil {
		t.Fatal(err)
	}
	if _, err := database.UpdatePartyServices(context.Background(), "pty_backup", "usr_backup", store.ServiceSettingsInput{TimeEnabled: true, RadioStation: "groove-salad", UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := database.CreateInvitation(context.Background(), store.NewInvitation{
		ID: "inv_backup", PartyID: "pty_backup", CreatedByUserID: "usr_backup",
		TokenHash: secure.Hash("invite"), ExpiresAt: now.Add(time.Hour), CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	deviceCiphertext, err := cipher.Encrypt("sip-password", []byte("dev_backup"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := database.ClaimInvitation(context.Background(), store.NewClaim{
		TokenHash: secure.Hash("invite"), MemberID: "mem_backup", DisplayName: "Member", Extension: "101",
		DeviceID: "dev_backup", DeviceLabel: "Phone", SIPUsername: "rrd_backup",
		SIPSecretCiphertext: deviceCiphertext,
		Provisioning: store.NewProvisioningToken{
			TokenHash: secure.Hash("verify-provision"), ExpiresAt: now.Add(time.Hour), CreatedAt: now,
		},
		Now: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateDeviceReadiness(context.Background(), "pty_backup", "usr_backup", "dev_backup", store.DeviceReadinessInput{
		EchoTested: true, OutgoingCallTested: true, IncomingCallTested: true, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	return path, key, partyCiphertext, deviceCiphertext
}
