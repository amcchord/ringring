package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/amcchord/ringring/internal/secure"
)

func TestInvitationCanOnlyBeClaimedOnce(t *testing.T) {
	ctx := context.Background()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	user, err := s.UpsertGoogleUser(ctx, GoogleProfile{Subject: "google-1", Email: "host@example.test", Name: "Host"}, now, "usr_1")
	if err != nil {
		t.Fatal(err)
	}
	party, err := s.CreateParty(ctx, NewParty{ID: "pty_1", Name: "Test party", Slug: "test-party", HostUserID: user.ID, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	token := "invite-secret"
	if err := s.CreateInvitation(ctx, NewInvitation{
		ID: "inv_1", PartyID: party.ID, CreatedByUserID: user.ID,
		TokenHash: secure.Hash(token), ExpiresAt: now.Add(time.Hour), CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	claim := NewClaim{
		TokenHash: secure.Hash(token), MemberID: "mem_1", DisplayName: "Blue phone", Extension: "101",
		DeviceID: "dev_1", DeviceLabel: "Kitchen phone", SIPUsername: "rrd_1",
		SIPSecretCiphertext: "ciphertext", Now: now,
	}
	claimedParty, member, _, err := s.ClaimInvitation(ctx, claim)
	if err != nil {
		t.Fatal(err)
	}
	if claimedParty.ID != party.ID || member.Extension != "101" {
		t.Fatalf("unexpected claim result: %#v %#v", claimedParty, member)
	}
	claim.MemberID = "mem_2"
	claim.DeviceID = "dev_2"
	claim.SIPUsername = "rrd_2"
	if _, _, _, err := s.ClaimInvitation(ctx, claim); !errors.Is(err, ErrInviteUsed) {
		t.Fatalf("second claim error = %v", err)
	}
}

func TestExtensionUniqueWithinParty(t *testing.T) {
	ctx := context.Background()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now().UTC()
	user, _ := s.UpsertGoogleUser(ctx, GoogleProfile{Subject: "g", Email: "e", Name: "n"}, now, "usr")
	party, _ := s.CreateParty(ctx, NewParty{ID: "pty", Name: "Party", Slug: "party", HostUserID: user.ID, CreatedAt: now})

	for i, token := range []string{"one", "two"} {
		if err := s.CreateInvitation(ctx, NewInvitation{
			ID: "inv_" + token, PartyID: party.ID, CreatedByUserID: user.ID,
			TokenHash: secure.Hash(token), ExpiresAt: now.Add(time.Hour), CreatedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
		_, _, _, err := s.ClaimInvitation(ctx, NewClaim{
			TokenHash: secure.Hash(token), MemberID: "mem_" + token, DisplayName: "Member", Extension: "101",
			DeviceID: "dev_" + token, DeviceLabel: "Phone", SIPUsername: "sip_" + token,
			SIPSecretCiphertext: "cipher", Now: now.Add(time.Duration(i) * time.Second),
		})
		if i == 0 && err != nil {
			t.Fatal(err)
		}
		if i == 1 && !errors.Is(err, ErrExtensionTaken) {
			t.Fatalf("duplicate extension error = %v", err)
		}
	}
}

func TestHostCanRevokeAndReconnectDevice(t *testing.T) {
	ctx := context.Background()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	host, _ := s.UpsertGoogleUser(ctx, GoogleProfile{Subject: "host", Email: "host@example.test", Name: "Host"}, now, "usr_host")
	other, _ := s.UpsertGoogleUser(ctx, GoogleProfile{Subject: "other", Email: "other@example.test", Name: "Other"}, now, "usr_other")
	party, _ := s.CreateParty(ctx, NewParty{ID: "pty_devices", Name: "Party", Slug: "devices", HostUserID: host.ID, CreatedAt: now})
	if err := s.CreateInvitation(ctx, NewInvitation{
		ID: "inv_devices", PartyID: party.ID, CreatedByUserID: host.ID,
		TokenHash: secure.Hash("device-invite"), ExpiresAt: now.Add(time.Hour), CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	_, _, device, err := s.ClaimInvitation(ctx, NewClaim{
		TokenHash: secure.Hash("device-invite"), MemberID: "mem_devices", DisplayName: "Kitchen", Extension: "101",
		DeviceID: "dev_devices", DeviceLabel: "ATA", SIPUsername: "rrd_old", SIPSecretCiphertext: "old-cipher", Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := s.RevokeDevice(ctx, party.ID, other.ID, device.ID, now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("non-host revocation error = %v", err)
	}
	if err := s.RevokeDevice(ctx, party.ID, host.ID, device.ID, now); err != nil {
		t.Fatal(err)
	}
	routing, err := s.RoutingDevices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(routing) != 0 {
		t.Fatalf("revoked device remained routable: %#v", routing)
	}

	rotated, err := s.RotateDevice(ctx, party.ID, host.ID, device.ID, "rrd_fresh", "fresh-cipher")
	if err != nil {
		t.Fatal(err)
	}
	if rotated.Device.SIPUsername != "rrd_fresh" || rotated.Device.RevokedAt != nil || rotated.Member.Extension != "101" {
		t.Fatalf("unexpected rotated device: %#v", rotated)
	}
	routing, err = s.RoutingDevices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(routing) != 1 || routing[0].SIPUsername != "rrd_fresh" || routing[0].SIPSecretCiphertext != "fresh-cipher" {
		t.Fatalf("unexpected routing after reconnect: %#v", routing)
	}
}

func TestPartyServiceSettingsAreHostScopedAndDefaultToTime(t *testing.T) {
	ctx := context.Background()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	host, _ := s.UpsertGoogleUser(ctx, GoogleProfile{Subject: "service-host", Email: "host@example.test", Name: "Host"}, now, "usr_service_host")
	other, _ := s.UpsertGoogleUser(ctx, GoogleProfile{Subject: "service-other", Email: "other@example.test", Name: "Other"}, now, "usr_service_other")
	party, _ := s.CreateParty(ctx, NewParty{ID: "pty_services", Name: "Party", Slug: "services", HostUserID: host.ID, CreatedAt: now})

	defaults, err := s.PartyServices(ctx, party.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !defaults.TimeEnabled || defaults.WeatherEnabled || defaults.RadioEnabled || defaults.AIEnabled {
		t.Fatalf("unexpected service defaults: %#v", defaults)
	}
	input := ServiceSettingsInput{
		TimeEnabled: false, WeatherEnabled: true, WeatherQuery: "Portland, Maine",
		WeatherLabel: "Portland, Maine", WeatherLatitude: 43.66, WeatherLongitude: -70.25,
		RadioEnabled: true, AIEnabled: true, UpdatedAt: now.Add(time.Minute),
	}
	if _, err := s.UpdatePartyServices(ctx, party.ID, other.ID, input); !errors.Is(err, ErrNotFound) {
		t.Fatalf("non-host update error = %v", err)
	}
	updated, err := s.UpdatePartyServices(ctx, party.ID, host.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	if updated.TimeEnabled || !updated.WeatherEnabled || !updated.RadioEnabled || !updated.AIEnabled || updated.WeatherLabel != "Portland, Maine" {
		t.Fatalf("unexpected updated settings: %#v", updated)
	}
	if err := s.UpdatePartyOpenAI(ctx, party.ID, "project", "service-account", "encrypted-key", "ready"); err != nil {
		t.Fatal(err)
	}
	routing, err := s.RoutingServices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(routing) != 1 || routing[0].TimeEnabled || !routing[0].WeatherEnabled || !routing[0].RadioEnabled || !routing[0].AIEnabled {
		t.Fatalf("unexpected routing services: %#v", routing)
	}
}

func TestDeletionIsHostScopedAndCascades(t *testing.T) {
	ctx := context.Background()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	host, _ := s.UpsertGoogleUser(ctx, GoogleProfile{Subject: "delete-host", Email: "host@example.test", Name: "Host"}, now, "usr_delete_host")
	other, _ := s.UpsertGoogleUser(ctx, GoogleProfile{Subject: "delete-other", Email: "other@example.test", Name: "Other"}, now, "usr_delete_other")
	sessionHash := secure.Hash("delete-session")
	if err := s.CreateSession(ctx, sessionHash, host.ID, now.Add(time.Hour), now); err != nil {
		t.Fatal(err)
	}
	party, _ := s.CreateParty(ctx, NewParty{ID: "pty_delete", Name: "Delete Party", Slug: "delete-party", HostUserID: host.ID, CreatedAt: now})
	if err := s.CreateInvitation(ctx, NewInvitation{
		ID: "inv_delete", PartyID: party.ID, CreatedByUserID: host.ID,
		TokenHash: secure.Hash("delete-invite"), ExpiresAt: now.Add(time.Hour), CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	_, member, _, err := s.ClaimInvitation(ctx, NewClaim{
		TokenHash: secure.Hash("delete-invite"), MemberID: "mem_delete", DisplayName: "Kitchen", Extension: "101",
		DeviceID: "dev_delete", DeviceLabel: "ATA", SIPUsername: "rrd_delete", SIPSecretCiphertext: "cipher", Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteMember(ctx, party.ID, other.ID, member.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("non-host member deletion error = %v", err)
	}
	if err := s.DeleteMember(ctx, party.ID, host.ID, member.ID); err != nil {
		t.Fatal(err)
	}
	if routing, err := s.RoutingDevices(ctx); err != nil || len(routing) != 0 {
		t.Fatalf("deleted member remained routable: %#v, %v", routing, err)
	}
	if _, _, err := s.MemberForHost(ctx, party.ID, host.ID, member.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted member lookup error = %v", err)
	}
	if err := s.DeleteParty(ctx, party.ID, other.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("non-host party deletion error = %v", err)
	}
	if err := s.DeleteUserWithoutParties(ctx, host.ID); !errors.Is(err, ErrPartiesRemain) {
		t.Fatalf("account deletion with party error = %v", err)
	}
	if err := s.DeleteParty(ctx, party.ID, host.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PartyForHost(ctx, party.ID, host.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted party lookup error = %v", err)
	}
	if err := s.DeleteUserWithoutParties(ctx, host.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UserBySession(ctx, sessionHash, now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted account session lookup error = %v", err)
	}
}

func TestOpenAddsAIServiceColumnToLegacyDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE party_services (
		party_id TEXT PRIMARY KEY,
		time_enabled INTEGER NOT NULL DEFAULT 1,
		weather_enabled INTEGER NOT NULL DEFAULT 0,
		weather_query TEXT NOT NULL DEFAULT '',
		weather_label TEXT NOT NULL DEFAULT '',
		weather_latitude REAL NOT NULL DEFAULT 0,
		weather_longitude REAL NOT NULL DEFAULT 0,
		radio_enabled INTEGER NOT NULL DEFAULT 0,
		updated_at INTEGER NOT NULL
	)`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var count int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('party_services') WHERE name = 'ai_enabled'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("ai_enabled column count = %d", count)
	}
}
