package store

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/amcchord/ringring/internal/secure"
)

func testProvisioning(token string, now time.Time) NewProvisioningToken {
	return NewProvisioningToken{
		TokenHash: secure.Hash(token), ExpiresAt: now.Add(30 * time.Minute), CreatedAt: now,
	}
}

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
		SIPSecretCiphertext: "ciphertext", Provisioning: testProvisioning("provision-1", now), Now: now,
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
	claim.Provisioning = testProvisioning("provision-2", now)
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
			SIPSecretCiphertext: "cipher", Provisioning: testProvisioning("provision-"+token, now), Now: now.Add(time.Duration(i) * time.Second),
		})
		if i == 0 && err != nil {
			t.Fatal(err)
		}
		if i == 1 && !errors.Is(err, ErrExtensionTaken) {
			t.Fatalf("duplicate extension error = %v", err)
		}
	}
}

func TestActiveDeviceCanChangeOnlyItsPartyMemberExtension(t *testing.T) {
	ctx := context.Background()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Date(2026, 8, 22, 14, 0, 0, 0, time.UTC)
	host, _ := s.UpsertGoogleUser(ctx, GoogleProfile{Subject: "extension-host", Email: "host@example.test", Name: "Host"}, now, "usr_extension")
	party, _ := s.CreateParty(ctx, NewParty{ID: "pty_extension", Name: "Extension party", Slug: "extension-party", HostUserID: host.ID, CreatedAt: now})
	otherParty, _ := s.CreateParty(ctx, NewParty{ID: "pty_other_extension", Name: "Other party", Slug: "other-extension-party", HostUserID: host.ID, CreatedAt: now})

	claim := func(inviteID, token, memberID, deviceID, sipUsername, extension, partyID string) {
		t.Helper()
		if err := s.CreateInvitation(ctx, NewInvitation{
			ID: inviteID, PartyID: partyID, CreatedByUserID: host.ID,
			TokenHash: secure.Hash(token), ExpiresAt: now.Add(time.Hour), CreatedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
		if _, _, _, err := s.ClaimInvitation(ctx, NewClaim{
			TokenHash: secure.Hash(token), MemberID: memberID, DisplayName: "Member", Extension: extension,
			DeviceID: deviceID, DeviceLabel: "Phone", SIPUsername: sipUsername,
			SIPSecretCiphertext: "cipher-" + deviceID, Provisioning: testProvisioning("provision-"+deviceID, now), Now: now,
		}); err != nil {
			t.Fatal(err)
		}
	}
	claim("inv_extension_a", "invite-extension-a", "mem_extension_a", "dev_extension_a", "rrd_extension_a", "101", party.ID)
	claim("inv_extension_b", "invite-extension-b", "mem_extension_b", "dev_extension_b", "rrd_extension_b", "102", party.ID)
	claim("inv_extension_other", "invite-extension-other", "mem_extension_other", "dev_extension_other", "rrd_extension_other", "103", otherParty.ID)

	for _, invalid := range []string{"1", "123456", "10*", "１２３"} {
		if err := s.ChangeMemberExtensionByDevice(ctx, party.ID, "rrd_extension_a", invalid); !errors.Is(err, ErrInvalidExtension) {
			t.Fatalf("invalid extension %q error = %v", invalid, err)
		}
	}
	if err := s.ChangeMemberExtensionByDevice(ctx, otherParty.ID, "rrd_extension_a", "104"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-party endpoint error = %v", err)
	}
	if err := s.ChangeMemberExtensionByDevice(ctx, party.ID, "rrd_unknown", "104"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown endpoint error = %v", err)
	}
	if err := s.ChangeMemberExtensionByDevice(ctx, party.ID, "rrd_extension_a", "103"); err != nil {
		t.Fatal(err)
	}
	if err := s.ChangeMemberExtensionByDevice(ctx, party.ID, "rrd_extension_a", "102"); !errors.Is(err, ErrExtensionTaken) {
		t.Fatalf("occupied extension error = %v", err)
	}

	routing, err := s.RoutingDevices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	extensions := make(map[string]string, len(routing))
	for _, device := range routing {
		extensions[device.SIPUsername] = device.Extension
	}
	if extensions["rrd_extension_a"] != "103" || extensions["rrd_extension_b"] != "102" || extensions["rrd_extension_other"] != "103" {
		t.Fatalf("unexpected party-scoped extensions: %#v", extensions)
	}

	if err := s.RevokeDevice(ctx, party.ID, host.ID, "dev_extension_a", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := s.ChangeMemberExtensionByDevice(ctx, party.ID, "rrd_extension_a", "104"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoked endpoint error = %v", err)
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
		DeviceID: "dev_devices", DeviceLabel: "ATA", SIPUsername: "rrd_old", SIPSecretCiphertext: "old-cipher",
		Provisioning: testProvisioning("old-provision", now), Now: now,
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

	rotated, err := s.RotateDevice(ctx, party.ID, host.ID, device.ID, "rrd_fresh", "fresh-cipher", testProvisioning("fresh-provision", now.Add(time.Minute)))
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

func TestProvisioningTokenIsHashedOneTimeExpiringAndRevocable(t *testing.T) {
	ctx := context.Background()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	host, _ := s.UpsertGoogleUser(ctx, GoogleProfile{Subject: "provision-host", Email: "host@example.test", Name: "Host"}, now, "usr_provision")
	party, _ := s.CreateParty(ctx, NewParty{ID: "pty_provision", Name: "Party", Slug: "provision", HostUserID: host.ID, CreatedAt: now})
	if err := s.CreateInvitation(ctx, NewInvitation{
		ID: "inv_provision", PartyID: party.ID, CreatedByUserID: host.ID,
		TokenHash: secure.Hash("provision-invite"), ExpiresAt: now.Add(time.Hour), CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	const initialToken = "initial-provision"
	_, _, device, err := s.ClaimInvitation(ctx, NewClaim{
		TokenHash: secure.Hash("provision-invite"), MemberID: "mem_provision", DisplayName: "Kitchen", Extension: "101",
		DeviceID: "dev_provision", DeviceLabel: "Softphone", SIPUsername: "rrd_initial", SIPSecretCiphertext: "initial-cipher",
		Provisioning: testProvisioning(initialToken, now), Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}

	var storedHash []byte
	if err := s.db.QueryRow(`SELECT token_hash FROM device_provisioning_tokens WHERE device_id = ?`, device.ID).Scan(&storedHash); err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(storedHash, []byte(initialToken)) || !bytes.Equal(storedHash, secure.Hash(initialToken)) {
		t.Fatal("provisioning token was not stored only as its SHA-256 digest")
	}
	if _, err := s.ConsumeProvisioningToken(ctx, secure.Hash("wrong-provision"), now.Add(time.Minute)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown provisioning error = %v", err)
	}
	provisioned, err := s.ConsumeProvisioningToken(ctx, secure.Hash(initialToken), now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if provisioned.DeviceID != device.ID || provisioned.SIPUsername != "rrd_initial" || provisioned.SIPSecretCiphertext != "initial-cipher" || provisioned.Extension != "101" {
		t.Fatalf("unexpected provisioning device: %#v", provisioned)
	}
	if _, err := s.ConsumeProvisioningToken(ctx, secure.Hash(initialToken), now.Add(2*time.Minute)); !errors.Is(err, ErrProvisionUsed) {
		t.Fatalf("reused provisioning error = %v", err)
	}

	if _, err := s.RotateDevice(ctx, party.ID, host.ID, device.ID, "rrd_expired", "expired-cipher", testProvisioning("expired-provision", now)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ConsumeProvisioningToken(ctx, secure.Hash(initialToken), now.Add(3*time.Minute)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("replaced provisioning error = %v", err)
	}
	if _, err := s.ConsumeProvisioningToken(ctx, secure.Hash("expired-provision"), now.Add(31*time.Minute)); !errors.Is(err, ErrProvisionExpired) {
		t.Fatalf("expired provisioning error = %v", err)
	}

	revocable := testProvisioning("revocable-provision", now.Add(32*time.Minute))
	if _, err := s.RotateDevice(ctx, party.ID, host.ID, device.ID, "rrd_revocable", "revocable-cipher", revocable); err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeDevice(ctx, party.ID, host.ID, device.ID, now.Add(33*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ConsumeProvisioningToken(ctx, revocable.TokenHash, now.Add(34*time.Minute)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoked provisioning error = %v", err)
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
	if err := s.UpdatePartyOpenAI(ctx, party.ID, "project", "service-account", "key-old", "encrypted-key", "ready"); err != nil {
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

func TestPartyOpenAIKeyRotationIsHostScopedAndCompareAndSwap(t *testing.T) {
	ctx := t.Context()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Date(2026, 8, 22, 18, 0, 0, 0, time.UTC)
	host, _ := s.UpsertGoogleUser(ctx, GoogleProfile{Subject: "rotation-host", Email: "host@example.test", Name: "Host"}, now, "usr_rotation_host")
	other, _ := s.UpsertGoogleUser(ctx, GoogleProfile{Subject: "rotation-other", Email: "other@example.test", Name: "Other"}, now, "usr_rotation_other")
	party, _ := s.CreateParty(ctx, NewParty{ID: "pty_rotation", Name: "Party", Slug: "rotation", HostUserID: host.ID, CreatedAt: now})
	if err := s.UpdatePartyOpenAI(ctx, party.ID, "proj_rotation", "svc_rotation", "key_old", "cipher_old", "ready"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdatePartyServices(ctx, party.ID, host.ID, ServiceSettingsInput{TimeEnabled: true, WeatherEnabled: true, WeatherQuery: "Portland", WeatherLabel: "Portland", AIEnabled: true, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := s.StartPartyOpenAIKeyRotation(ctx, party.ID, other.ID, "key_old", "key_outsider", "cipher_outsider"); !errors.Is(err, ErrOpenAIRotation) {
		t.Fatalf("non-host rotation error = %v", err)
	}
	if err := s.StartPartyOpenAIKeyRotation(ctx, party.ID, host.ID, "wrong_key", "key_wrong", "cipher_wrong"); !errors.Is(err, ErrOpenAIRotation) {
		t.Fatalf("stale rotation error = %v", err)
	}
	if err := s.StartPartyOpenAIKeyRotation(ctx, party.ID, host.ID, "key_old", "key_fresh", "cipher_fresh"); err != nil {
		t.Fatal(err)
	}
	rotating, err := s.PartyForHost(ctx, party.ID, host.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rotating.OpenAIAPIKeyID != "key_fresh" || rotating.OpenAIKeyCiphertext != "cipher_fresh" || rotating.OpenAIStatus != "rotating" {
		t.Fatalf("unexpected rotating state: %#v", rotating)
	}
	if routing, err := s.RoutingServices(ctx); err != nil || len(routing) != 1 || routing[0].AIEnabled || routing[0].WeatherEnabled {
		t.Fatalf("AI routes were not paused during rotation: %#v, %v", routing, err)
	}
	if err := s.StartPartyOpenAIKeyRotation(ctx, party.ID, host.ID, "key_old", "key_second", "cipher_second"); !errors.Is(err, ErrOpenAIRotation) {
		t.Fatalf("concurrent rotation error = %v", err)
	}
	if err := s.SetPartyOpenAIKeyRotationStatus(ctx, party.ID, host.ID, "key_fresh", "rotation-error"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPartyOpenAIKeyRotationStatus(ctx, party.ID, host.ID, "key_fresh", "ready"); err != nil {
		t.Fatal(err)
	}
	ready, err := s.PartyForHost(ctx, party.ID, host.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ready.OpenAIAPIKeyID != "key_fresh" || ready.OpenAIStatus != "ready" {
		t.Fatalf("unexpected completed state: %#v", ready)
	}
	if err := s.SetPartyOpenAIKeyRotationStatus(ctx, party.ID, host.ID, "key_fresh", "unknown"); err == nil {
		t.Fatal("accepted an invalid rotation status")
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
		DeviceID: "dev_delete", DeviceLabel: "ATA", SIPUsername: "rrd_delete", SIPSecretCiphertext: "cipher",
		Provisioning: testProvisioning("delete-provision", now), Now: now,
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

func TestOpenAddsCurrentColumnsToLegacyDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE parties (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		slug TEXT NOT NULL UNIQUE,
		host_user_id TEXT NOT NULL,
		openai_project_id TEXT,
		openai_service_account_id TEXT,
		openai_key_ciphertext TEXT,
		openai_status TEXT NOT NULL DEFAULT 'pending',
		created_at INTEGER NOT NULL
	);
	CREATE TABLE party_services (
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
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('parties') WHERE name = 'openai_api_key_id'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("openai_api_key_id column count = %d", count)
	}
}
