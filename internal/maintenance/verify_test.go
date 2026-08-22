package maintenance

import (
	"context"
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
	if report.Users != 1 || report.Parties != 1 || report.Members != 1 || report.Devices != 1 || report.Invitations != 1 || report.Sessions != 1 || report.RecoveryCodes != 1 {
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
	if err := database.UpdatePartyOpenAI(context.Background(), "pty_backup", "project", "service", partyCiphertext, "ready"); err != nil {
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
		SIPSecretCiphertext: deviceCiphertext, Now: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	return path, key, partyCiphertext, deviceCiphertext
}
