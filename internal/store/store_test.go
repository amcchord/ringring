package store

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/amcchord/ringring/internal/radio"
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
	if suggested, err := s.SuggestedExtension(ctx, party.ID); err != nil || suggested != "101" {
		t.Fatalf("empty-party suggested extension = %q, %v", suggested, err)
	}

	claim := NewClaim{
		TokenHash: secure.Hash(token), MemberID: "mem_1", DisplayName: "Blue phone", Extension: "101",
		DeviceID: "dev_1", DeviceLabel: "Kitchen phone", SIPUsername: "rrd_1",
		SIPSecretCiphertext: "ciphertext", Provisioning: testProvisioning("provision-1", now), Now: now,
	}
	unsafeClaim := claim
	unsafeClaim.Extension = "911"
	if _, _, _, err := s.ClaimInvitation(ctx, unsafeClaim); !errors.Is(err, ErrInvalidExtension) {
		t.Fatalf("reserved invitation extension error = %v", err)
	}
	claimedParty, member, _, err := s.ClaimInvitation(ctx, claim)
	if err != nil {
		t.Fatal(err)
	}
	if claimedParty.ID != party.ID || member.Extension != "101" {
		t.Fatalf("unexpected claim result: %#v %#v", claimedParty, member)
	}
	if suggested, err := s.SuggestedExtension(ctx, party.ID); err != nil || suggested != "102" {
		t.Fatalf("occupied-party suggested extension = %q, %v", suggested, err)
	}
	claim.MemberID = "mem_2"
	claim.DeviceID = "dev_2"
	claim.SIPUsername = "rrd_2"
	claim.Provisioning = testProvisioning("provision-2", now)
	if _, _, _, err := s.ClaimInvitation(ctx, claim); !errors.Is(err, ErrInviteUsed) {
		t.Fatalf("second claim error = %v", err)
	}
}

func TestAdultExtensionAuthorizesOnlyItsActiveAuthenticatedDevices(t *testing.T) {
	ctx := t.Context()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	host, _ := s.UpsertGoogleUser(ctx, GoogleProfile{Subject: "adult-access-host", Email: "host@example.test", Name: "Host"}, now, "usr_adult_access")
	party, _ := s.CreateParty(ctx, NewParty{ID: "pty_adult_access", Name: "Party", Slug: "adult-access", HostUserID: host.ID, CreatedAt: now})
	otherParty, _ := s.CreateParty(ctx, NewParty{ID: "pty_adult_access_other", Name: "Other", Slug: "adult-access-other", HostUserID: host.ID, CreatedAt: now})
	claim := func(invitationID, token, memberID, deviceID, username, extension, partyID string, adult bool) {
		t.Helper()
		if err := s.CreateInvitation(ctx, NewInvitation{
			ID: invitationID, PartyID: partyID, CreatedByUserID: host.ID, TokenHash: secure.Hash(token),
			ExpiresAt: now.Add(time.Hour), CreatedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
		if _, _, _, err := s.ClaimInvitation(ctx, NewClaim{
			TokenHash: secure.Hash(token), MemberID: memberID, DisplayName: "Phone", Extension: extension, AdultExtension: adult,
			DeviceID: deviceID, DeviceLabel: "ATA", SIPUsername: username, SIPSecretCiphertext: "cipher-" + deviceID,
			Provisioning: testProvisioning("provision-"+deviceID, now), Now: now,
		}); err != nil {
			t.Fatal(err)
		}
	}
	claim("inv_adult_access", "adult-access", "mem_adult_access", "dev_adult_access", "123456", "101", party.ID, true)
	claim("inv_child_access", "child-access", "mem_child_access", "dev_child_access", "234567", "102", party.ID, false)
	claim("inv_other_access", "other-access", "mem_other_access", "dev_other_access", "345678", "101", otherParty.ID, true)

	for _, test := range []struct {
		partyID  string
		username string
		want     bool
	}{
		{party.ID, "123456", true},
		{party.ID, "234567", false},
		{party.ID, "345678", false},
		{otherParty.ID, "345678", true},
		{party.ID, "456789", false},
	} {
		got, err := s.AIAdultAccessForDevice(ctx, test.partyID, test.username)
		if err != nil || got != test.want {
			t.Fatalf("adult access party=%q username=%q = %t, %v; want %t", test.partyID, test.username, got, err, test.want)
		}
	}
	members, err := s.ListMembers(ctx, party.ID)
	if err != nil || len(members) != 2 || !members[0].AdultExtension || members[1].AdultExtension {
		t.Fatalf("adult extension classification was not preserved: %#v error=%v", members, err)
	}
	if err := s.RevokeDevice(ctx, party.ID, host.ID, "dev_adult_access", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if allowed, err := s.AIAdultAccessForDevice(ctx, party.ID, "123456"); err != nil || allowed {
		t.Fatalf("revoked adult device retained access: allowed=%t error=%v", allowed, err)
	}
}

func TestOperatorDisclosureIsSharedOnlyByActiveDevicesOnOneExtension(t *testing.T) {
	ctx := t.Context()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	now := time.Date(2026, 8, 23, 18, 0, 0, 0, time.UTC)
	host, err := s.UpsertGoogleUser(ctx, GoogleProfile{Subject: "operator-host", Email: "host@example.test", Name: "Host"}, now, "usr_operator")
	if err != nil {
		t.Fatal(err)
	}
	party, err := s.CreateParty(ctx, NewParty{ID: "pty_operator", Name: "Party", Slug: "operator", HostUserID: host.ID, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateInvitation(ctx, NewInvitation{
		ID: "inv_operator", PartyID: party.ID, CreatedByUserID: host.ID, TokenHash: secure.Hash("operator-token"),
		ExpiresAt: now.Add(time.Hour), CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.ClaimInvitation(ctx, NewClaim{
		TokenHash: secure.Hash("operator-token"), MemberID: "mem_operator", DisplayName: "Phone", Extension: "101",
		DeviceID: "dev_operator_one", DeviceLabel: "ATA", SIPUsername: "123456", SIPSecretCiphertext: "cipher-one",
		Provisioning: testProvisioning("provision-operator", now), Now: now,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO devices (id, member_id, label, sip_username, sip_secret_ciphertext, created_at)
		VALUES ('dev_operator_two', 'mem_operator', 'Second phone', '234567', 'cipher-two', ?)`, unix(now)); err != nil {
		t.Fatal(err)
	}

	if disclosed, err := s.OperatorDisclosureForDevice(ctx, party.ID, "123456"); err != nil || disclosed {
		t.Fatalf("new extension disclosure = %t, %v", disclosed, err)
	}
	if err := s.MarkOperatorDisclosureForDevice(ctx, party.ID, "123456", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	for _, username := range []string{"123456", "234567"} {
		if disclosed, err := s.OperatorDisclosureForDevice(ctx, party.ID, username); err != nil || !disclosed {
			t.Fatalf("shared extension disclosure for %q = %t, %v", username, disclosed, err)
		}
	}
	if err := s.MarkOperatorDisclosureForDevice(ctx, "pty_other", "123456", now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-party disclosure mark error = %v", err)
	}
	if err := s.RevokeDevice(ctx, party.ID, host.ID, "dev_operator_two", now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.OperatorDisclosureForDevice(ctx, party.ID, "234567"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoked device disclosure error = %v", err)
	}
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM operator_disclosures`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("operator disclosure rows = %d, %v", count, err)
	}
}

func TestExistingMembersMigrateToNonAdultExtensions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.Exec(`
		CREATE TABLE members (
			id TEXT PRIMARY KEY,
			party_id TEXT NOT NULL,
			display_name TEXT NOT NULL,
			extension TEXT NOT NULL,
			created_at INTEGER NOT NULL
		);
		INSERT INTO members (id, party_id, display_name, extension, created_at)
		VALUES ('mem_legacy', 'pty_legacy', 'Legacy phone', '101', 1);`); err != nil {
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var adult int
	if err := s.db.QueryRow(`SELECT adult_extension FROM members WHERE id = 'mem_legacy'`).Scan(&adult); err != nil {
		t.Fatal(err)
	}
	if adult != 0 {
		t.Fatalf("legacy extension defaulted to adult: %d", adult)
	}
	var disclosureTable int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'operator_disclosures'`).Scan(&disclosureTable); err != nil {
		t.Fatal(err)
	}
	if disclosureTable != 1 {
		t.Fatalf("operator disclosure migration table count = %d", disclosureTable)
	}
}

func TestSIPUsernameConflictsAreRetryableWithoutPartialWrites(t *testing.T) {
	ctx := t.Context()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	now := time.Date(2026, 8, 22, 22, 0, 0, 0, time.UTC)
	host, _ := s.UpsertGoogleUser(ctx, GoogleProfile{Subject: "sip-collision-host", Email: "host@example.test", Name: "Host"}, now, "usr_sip_collision")
	party, _ := s.CreateParty(ctx, NewParty{ID: "pty_sip_collision", Name: "Party", Slug: "sip-collision", HostUserID: host.ID, CreatedAt: now})
	claim := func(invitationID, token, memberID, deviceID, extension, username string) error {
		t.Helper()
		if err := s.CreateInvitation(ctx, NewInvitation{
			ID: invitationID, PartyID: party.ID, CreatedByUserID: host.ID,
			TokenHash: secure.Hash(token), ExpiresAt: now.Add(time.Hour), CreatedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
		_, _, _, err := s.ClaimInvitation(ctx, NewClaim{
			TokenHash: secure.Hash(token), MemberID: memberID, DisplayName: memberID, Extension: extension,
			DeviceID: deviceID, DeviceLabel: "Phone", SIPUsername: username, SIPSecretCiphertext: "cipher-" + deviceID,
			Provisioning: testProvisioning("provision-"+deviceID, now), Now: now,
		})
		return err
	}

	if err := claim("inv_sip_collision_a", "sip-collision-a", "mem_sip_collision_a", "dev_sip_collision_a", "101", "123456"); err != nil {
		t.Fatal(err)
	}
	if err := claim("inv_sip_collision_b", "sip-collision-b", "mem_sip_collision_b", "dev_sip_collision_b", "102", "123456"); !errors.Is(err, ErrSIPUsernameTaken) {
		t.Fatalf("invitation collision error = %v", err)
	}
	_, secondMember, secondDevice, err := s.ClaimInvitation(ctx, NewClaim{
		TokenHash: secure.Hash("sip-collision-b"), MemberID: "mem_sip_collision_b", DisplayName: "Second", Extension: "102",
		DeviceID: "dev_sip_collision_b", DeviceLabel: "Phone", SIPUsername: "234567", SIPSecretCiphertext: "cipher-b",
		Provisioning: testProvisioning("provision-b-retry", now), Now: now,
	})
	if err != nil {
		t.Fatalf("retry invitation claim: %v", err)
	}

	if _, err := s.AddDeviceForHost(ctx, NewHostedDevice{
		PartyID: party.ID, HostUserID: host.ID, MemberID: secondMember.ID,
		DeviceID: "dev_sip_collision_c", DeviceLabel: "Tablet", SIPUsername: "123456", SIPSecretCiphertext: "cipher-c",
		Provisioning: testProvisioning("provision-c", now), Now: now,
	}); !errors.Is(err, ErrSIPUsernameTaken) {
		t.Fatalf("host-added phone collision error = %v", err)
	}
	if _, err := s.RotateDevice(ctx, party.ID, host.ID, secondDevice.ID, "123456", "rotated-cipher", testProvisioning("rotated-provision", now)); !errors.Is(err, ErrSIPUsernameTaken) {
		t.Fatalf("rotation collision error = %v", err)
	}
	_, unchanged, err := s.ActiveDeviceForHost(ctx, party.ID, host.ID, secondDevice.ID)
	if err != nil || unchanged.SIPUsername != "234567" {
		t.Fatalf("failed rotation changed the phone: username=%q error=%v", unchanged.SIPUsername, err)
	}
}

func TestHostCanCountAndCancelOnlyActiveInvitations(t *testing.T) {
	ctx := t.Context()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	now := time.Date(2026, 8, 22, 18, 30, 0, 0, time.UTC)
	host, _ := s.UpsertGoogleUser(ctx, GoogleProfile{Subject: "invite-cancel-host", Email: "host@example.test", Name: "Host"}, now, "usr_invite_cancel_host")
	otherHost, _ := s.UpsertGoogleUser(ctx, GoogleProfile{Subject: "invite-cancel-other", Email: "other@example.test", Name: "Other"}, now, "usr_invite_cancel_other")
	party, _ := s.CreateParty(ctx, NewParty{ID: "pty_invite_cancel", Name: "Party", Slug: "invite-cancel", HostUserID: host.ID, CreatedAt: now})
	otherParty, _ := s.CreateParty(ctx, NewParty{ID: "pty_invite_cancel_other", Name: "Other", Slug: "invite-cancel-other", HostUserID: otherHost.ID, CreatedAt: now})
	create := func(id, token, partyID, creatorID string, expires time.Time) {
		t.Helper()
		if err := s.CreateInvitation(ctx, NewInvitation{
			ID: id, PartyID: partyID, CreatedByUserID: creatorID, TokenHash: secure.Hash(token), ExpiresAt: expires, CreatedAt: now.Add(-time.Minute),
		}); err != nil {
			t.Fatal(err)
		}
	}
	create("inv_cancel_active", "cancel-active", party.ID, host.ID, now.Add(time.Hour))
	create("inv_cancel_boundary", "cancel-boundary", party.ID, host.ID, now)
	create("inv_cancel_expired", "cancel-expired", party.ID, host.ID, now.Add(-time.Second))
	create("inv_cancel_used", "cancel-used", party.ID, host.ID, now.Add(time.Hour))
	create("inv_cancel_other", "cancel-other", otherParty.ID, otherHost.ID, now.Add(time.Hour))
	if _, _, _, err := s.ClaimInvitation(ctx, NewClaim{
		TokenHash: secure.Hash("cancel-used"), MemberID: "mem_cancel_used", DisplayName: "Used", Extension: "101",
		DeviceID: "dev_cancel_used", DeviceLabel: "Phone", SIPUsername: "rrd_cancel_used",
		SIPSecretCiphertext: "cipher-used", Provisioning: testProvisioning("cancel-used-provision", now), Now: now,
	}); err != nil {
		t.Fatal(err)
	}

	if count, err := s.ActiveInvitationCountForHost(ctx, party.ID, host.ID, now); err != nil || count != 2 {
		t.Fatalf("active invitation count = %d, %v", count, err)
	}
	if _, err := s.ActiveInvitationCountForHost(ctx, party.ID, otherHost.ID, now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-host active invitation count error = %v", err)
	}
	if _, err := s.CancelActiveInvitationsForHost(ctx, party.ID, otherHost.ID, now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-host invitation cancellation error = %v", err)
	}
	if count, _ := s.ActiveInvitationCountForHost(ctx, party.ID, host.ID, now); count != 2 {
		t.Fatalf("cross-host cancellation changed active count to %d", count)
	}

	removed, err := s.CancelActiveInvitationsForHost(ctx, party.ID, host.ID, now)
	if err != nil || removed != 2 {
		t.Fatalf("canceled invitation count = %d, %v", removed, err)
	}
	if count, err := s.ActiveInvitationCountForHost(ctx, party.ID, host.ID, now); err != nil || count != 0 {
		t.Fatalf("post-cancel active invitation count = %d, %v", count, err)
	}
	for _, token := range []string{"cancel-active", "cancel-boundary"} {
		if _, err := s.PartyByInvitation(ctx, secure.Hash(token), now); !errors.Is(err, ErrNotFound) {
			t.Fatalf("canceled token %q error = %v", token, err)
		}
	}
	if _, err := s.PartyByInvitation(ctx, secure.Hash("cancel-expired"), now); !errors.Is(err, ErrInviteExpired) {
		t.Fatalf("expired invitation changed: %v", err)
	}
	if _, err := s.PartyByInvitation(ctx, secure.Hash("cancel-used"), now); !errors.Is(err, ErrInviteUsed) {
		t.Fatalf("used invitation changed: %v", err)
	}
	if got, err := s.PartyByInvitation(ctx, secure.Hash("cancel-other"), now); err != nil || got.ID != otherParty.ID {
		t.Fatalf("other party invitation changed: %#v, %v", got, err)
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

func TestLegacyReservedExtensionsMigrateWithinTheirParty(t *testing.T) {
	ctx := context.Background()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)
	host, _ := s.UpsertGoogleUser(ctx, GoogleProfile{Subject: "migration-host", Email: "host@example.test", Name: "Host"}, now, "usr_migration")
	first, _ := s.CreateParty(ctx, NewParty{ID: "pty_migration_one", Name: "One", Slug: "one", HostUserID: host.ID, CreatedAt: now})
	second, _ := s.CreateParty(ctx, NewParty{ID: "pty_migration_two", Name: "Two", Slug: "two", HostUserID: host.ID, CreatedAt: now})
	for index, row := range []struct {
		id, partyID, value string
	}{
		{"mem_existing", first.ID, "101"},
		{"mem_emergency", first.ID, "911"},
		{"mem_crisis", first.ID, "988"},
		{"mem_other_party", second.ID, "911"},
	} {
		if _, err := s.db.ExecContext(ctx, `INSERT INTO members (id, party_id, display_name, extension, created_at) VALUES (?, ?, ?, ?, ?)`,
			row.id, row.partyID, "Legacy", row.value, unix(now.Add(time.Duration(index)*time.Second))); err != nil {
			t.Fatal(err)
		}
	}
	if err := migrateReservedMemberExtensions(s.db); err != nil {
		t.Fatal(err)
	}
	if err := migrateReservedMemberExtensions(s.db); err != nil {
		t.Fatalf("migration was not idempotent: %v", err)
	}
	want := map[string]string{
		"mem_existing": "101", "mem_emergency": "102", "mem_crisis": "103", "mem_other_party": "101",
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, extension FROM members`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, value string
		if err := rows.Scan(&id, &value); err != nil {
			t.Fatal(err)
		}
		if value != want[id] {
			t.Errorf("member %s extension = %q, want %q", id, value, want[id])
		}
		delete(want, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(want) != 0 {
		t.Fatalf("migration rows missing: %#v", want)
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

	for _, invalid := range []string{"1", "123456", "10*", "１２３", "000", "111", "112", "911", "988", "999"} {
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
	memberForRing, deviceForRing, err := s.ActiveDeviceForHost(ctx, party.ID, host.ID, device.ID)
	if err != nil || memberForRing.Extension != "101" || deviceForRing.SIPUsername != "rrd_old" || deviceForRing.SIPSecretCiphertext != "" {
		t.Fatalf("unexpected active ring target: member=%#v device=%#v error=%v", memberForRing, deviceForRing, err)
	}
	if _, _, err := s.ActiveDeviceForHost(ctx, party.ID, other.ID, device.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("non-host active ring target error = %v", err)
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
	if _, _, err := s.ActiveDeviceForHost(ctx, party.ID, host.ID, device.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoked device remained ringable: %v", err)
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
	if _, reconnected, err := s.ActiveDeviceForHost(ctx, party.ID, host.ID, device.ID); err != nil || reconnected.SIPUsername != "rrd_fresh" {
		t.Fatalf("reconnected device was not ringable: %#v error=%v", reconnected, err)
	}
}

func TestHostCanAddIndependentPhonesToExistingMember(t *testing.T) {
	ctx := t.Context()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Date(2026, 8, 22, 21, 0, 0, 0, time.UTC)
	host, _ := s.UpsertGoogleUser(ctx, GoogleProfile{Subject: "add-device-host", Email: "host@example.test", Name: "Host"}, now, "usr_add_device_host")
	other, _ := s.UpsertGoogleUser(ctx, GoogleProfile{Subject: "add-device-other", Email: "other@example.test", Name: "Other"}, now, "usr_add_device_other")
	party, _ := s.CreateParty(ctx, NewParty{ID: "pty_add_device", Name: "Party", Slug: "add-device", HostUserID: host.ID, CreatedAt: now})
	otherParty, _ := s.CreateParty(ctx, NewParty{ID: "pty_add_device_other", Name: "Other", Slug: "add-device-other", HostUserID: host.ID, CreatedAt: now})
	if err := s.CreateInvitation(ctx, NewInvitation{
		ID: "inv_add_device", PartyID: party.ID, CreatedByUserID: host.ID,
		TokenHash: secure.Hash("add-device-invite"), ExpiresAt: now.Add(time.Hour), CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	_, member, first, err := s.ClaimInvitation(ctx, NewClaim{
		TokenHash: secure.Hash("add-device-invite"), MemberID: "mem_add_device", DisplayName: "Kitchen", Extension: "101",
		DeviceID: "dev_add_device_first", DeviceLabel: "ATA", SIPUsername: "rrd_add_device_first", SIPSecretCiphertext: "cipher-first",
		Provisioning: testProvisioning("add-device-first-provision", now), Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}

	base := NewHostedDevice{
		PartyID: party.ID, HostUserID: host.ID, MemberID: member.ID,
		DeviceID: "dev_add_device_second", DeviceLabel: "Tablet", SIPUsername: "rrd_add_device_second",
		SIPSecretCiphertext: "cipher-second", Provisioning: testProvisioning("add-device-second-provision", now), Now: now,
	}
	wrongHost := base
	wrongHost.HostUserID = other.ID
	if _, err := s.AddDeviceForHost(ctx, wrongHost); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-host device creation error = %v", err)
	}
	wrongParty := base
	wrongParty.PartyID = otherParty.ID
	if _, err := s.AddDeviceForHost(ctx, wrongParty); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-party device creation error = %v", err)
	}
	invalidProvisioning := base
	invalidProvisioning.Provisioning.ExpiresAt = invalidProvisioning.Provisioning.CreatedAt
	if _, err := s.AddDeviceForHost(ctx, invalidProvisioning); err == nil {
		t.Fatal("invalid provisioning token created a device")
	}

	created, err := s.AddDeviceForHost(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	if created.Party.ID != party.ID || created.Member.ID != member.ID || created.Member.Extension != "101" ||
		created.Device.ID != base.DeviceID || created.Device.SIPUsername != base.SIPUsername || created.Device.SIPSecretCiphertext != "cipher-second" {
		t.Fatalf("unexpected created phone: %#v", created)
	}
	routing, err := s.RoutingDevices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(routing) != 2 || routing[0].DeviceID != first.ID || routing[0].Extension != "101" || routing[1].DeviceID != base.DeviceID || routing[1].Extension != "101" {
		t.Fatalf("same-extension phones were not independently routable: %#v", routing)
	}
	provisioned, err := s.ConsumeProvisioningToken(ctx, secure.Hash("add-device-second-provision"), now.Add(time.Minute))
	if err != nil || provisioned.DeviceID != base.DeviceID || provisioned.SIPUsername != base.SIPUsername || provisioned.SIPSecretCiphertext != "cipher-second" || provisioned.Extension != "101" {
		t.Fatalf("unexpected second-phone provisioning: %#v error=%v", provisioned, err)
	}

	for index := 3; index <= MaxDevicesPerMember; index++ {
		input := base
		input.DeviceID = fmt.Sprintf("dev_add_device_%d", index)
		input.SIPUsername = fmt.Sprintf("rrd_add_device_%d", index)
		input.SIPSecretCiphertext = fmt.Sprintf("cipher-%d", index)
		input.Provisioning = testProvisioning(fmt.Sprintf("add-device-provision-%d", index), now)
		if _, err := s.AddDeviceForHost(ctx, input); err != nil {
			t.Fatalf("add device %d: %v", index, err)
		}
	}
	overLimit := base
	overLimit.DeviceID = "dev_add_device_over_limit"
	overLimit.SIPUsername = "rrd_add_device_over_limit"
	overLimit.Provisioning = testProvisioning("add-device-over-limit-provision", now)
	if _, err := s.AddDeviceForHost(ctx, overLimit); !errors.Is(err, ErrDeviceLimit) {
		t.Fatalf("device limit error = %v", err)
	}
	if routing, err := s.RoutingDevices(ctx); err != nil || len(routing) != MaxDevicesPerMember {
		t.Fatalf("device limit changed routing: count=%d error=%v", len(routing), err)
	}
}

func TestDeviceReadinessIsHostScopedResettableAndClearedByRotation(t *testing.T) {
	ctx := t.Context()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Date(2026, 8, 22, 20, 0, 0, 0, time.UTC)
	host, _ := s.UpsertGoogleUser(ctx, GoogleProfile{Subject: "readiness-host", Email: "host@example.test", Name: "Host"}, now, "usr_readiness_host")
	other, _ := s.UpsertGoogleUser(ctx, GoogleProfile{Subject: "readiness-other", Email: "other@example.test", Name: "Other"}, now, "usr_readiness_other")
	party, _ := s.CreateParty(ctx, NewParty{ID: "pty_readiness", Name: "Party", Slug: "readiness", HostUserID: host.ID, CreatedAt: now})
	if err := s.CreateInvitation(ctx, NewInvitation{
		ID: "inv_readiness", PartyID: party.ID, CreatedByUserID: host.ID,
		TokenHash: secure.Hash("readiness-invite"), ExpiresAt: now.Add(time.Hour), CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	_, member, device, err := s.ClaimInvitation(ctx, NewClaim{
		TokenHash: secure.Hash("readiness-invite"), MemberID: "mem_readiness", DisplayName: "Kitchen", Extension: "101",
		DeviceID: "dev_readiness", DeviceLabel: "ATA", SIPUsername: "rrd_readiness", SIPSecretCiphertext: "cipher",
		Provisioning: testProvisioning("readiness-provision", now), Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateDeviceReadiness(ctx, party.ID, other.ID, device.ID, DeviceReadinessInput{EchoTested: true, UpdatedAt: now}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("non-host readiness error = %v", err)
	}

	firstCheck := now.Add(time.Minute)
	if err := s.UpdateDeviceReadiness(ctx, party.ID, host.ID, device.ID, DeviceReadinessInput{
		EchoTested: true, OutgoingCallTested: true, UpdatedAt: firstCheck,
	}); err != nil {
		t.Fatal(err)
	}
	members, err := s.ListMembers(ctx, party.ID)
	if err != nil {
		t.Fatal(err)
	}
	readiness := members[0].Devices[0].Readiness
	if readiness.CompletedCount() != 2 || readiness.Complete() || readiness.EchoTestedAt == nil || readiness.OutgoingCallTestedAt == nil || readiness.IncomingCallTestedAt != nil {
		t.Fatalf("unexpected first readiness state: %#v", readiness)
	}

	secondCheck := now.Add(2 * time.Minute)
	if err := s.UpdateDeviceReadiness(ctx, party.ID, host.ID, device.ID, DeviceReadinessInput{
		EchoTested: true, IncomingCallTested: true, UpdatedAt: secondCheck,
	}); err != nil {
		t.Fatal(err)
	}
	members, err = s.ListMembers(ctx, party.ID)
	if err != nil {
		t.Fatal(err)
	}
	readiness = members[0].Devices[0].Readiness
	if readiness.CompletedCount() != 2 || readiness.OutgoingCallTestedAt != nil || readiness.IncomingCallTestedAt == nil || !readiness.EchoTestedAt.Equal(firstCheck) || !readiness.IncomingCallTestedAt.Equal(secondCheck) {
		t.Fatalf("readiness did not preserve and clear individual checks: %#v", readiness)
	}

	if err := s.RevokeDevice(ctx, party.ID, host.ID, device.ID, now.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateDeviceReadiness(ctx, party.ID, host.ID, device.ID, DeviceReadinessInput{EchoTested: true, UpdatedAt: now.Add(4 * time.Minute)}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoked-device readiness error = %v", err)
	}
	if _, err := s.RotateDevice(ctx, party.ID, host.ID, device.ID, "rrd_readiness_fresh", "fresh-cipher", testProvisioning("fresh-readiness-provision", now.Add(5*time.Minute))); err != nil {
		t.Fatal(err)
	}
	members, err = s.ListMembers(ctx, party.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := members[0].Devices[0].Readiness.CompletedCount(); got != 0 {
		t.Fatalf("credential rotation retained %d readiness checks", got)
	}
	if err := s.UpdateDeviceReadiness(ctx, party.ID, host.ID, device.ID, DeviceReadinessInput{EchoTested: true, UpdatedAt: now.Add(6 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateDeviceReadiness(ctx, party.ID, host.ID, device.ID, DeviceReadinessInput{UpdatedAt: now.Add(7 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
	var readinessRows int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM device_readiness`).Scan(&readinessRows); err != nil {
		t.Fatal(err)
	}
	if readinessRows != 0 {
		t.Fatalf("clearing every check left %d empty readiness rows", readinessRows)
	}
	if err := s.UpdateDeviceReadiness(ctx, party.ID, host.ID, device.ID, DeviceReadinessInput{
		EchoTested: true, OutgoingCallTested: true, IncomingCallTested: true, UpdatedAt: now.Add(8 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteMember(ctx, party.ID, host.ID, member.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM device_readiness`).Scan(&readinessRows); err != nil {
		t.Fatal(err)
	}
	if readinessRows != 0 {
		t.Fatalf("member deletion left %d readiness rows", readinessRows)
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

func TestListOpenAIProjectIDsIsDistinctAndOrdered(t *testing.T) {
	ctx := t.Context()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	host, err := s.UpsertGoogleUser(ctx, GoogleProfile{Subject: "retention-host", Email: "host@example.test", Name: "Host"}, now, "usr_retention_host")
	if err != nil {
		t.Fatal(err)
	}
	projects := []string{"proj_zulu", "proj_alpha", "proj_zulu"}
	for index, projectID := range projects {
		partyID := fmt.Sprintf("pty_retention_%d", index)
		party, err := s.CreateParty(ctx, NewParty{ID: partyID, Name: "Party", Slug: fmt.Sprintf("retention-%d", index), HostUserID: host.ID, CreatedAt: now})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.UpdatePartyOpenAI(ctx, party.ID, projectID, "service-account", "key", "ciphertext", "ready", 1000); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.CreateParty(ctx, NewParty{ID: "pty_retention_empty", Name: "No project", Slug: "retention-empty", HostUserID: host.ID, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	projectIDs, err := s.ListOpenAIProjectIDs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"proj_alpha", "proj_zulu"}; !reflect.DeepEqual(projectIDs, want) {
		t.Fatalf("project IDs = %#v, want %#v", projectIDs, want)
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
	if !defaults.TimeEnabled || defaults.WeatherEnabled || !defaults.WeatherSetupAllowed || defaults.RadioEnabled || defaults.RadioStation != "groove-salad" || defaults.AIEnabled {
		t.Fatalf("unexpected service defaults: %#v", defaults)
	}
	input := ServiceSettingsInput{
		TimeEnabled: false, WeatherEnabled: true, WeatherSetupAllowed: true, WeatherQuery: "Portland, Maine",
		WeatherLabel: "Portland, Maine", WeatherLatitude: 43.66, WeatherLongitude: -70.25,
		RadioEnabled: true, RadioStation: "drone-zone", AIEnabled: true, AIAdultOnlyEnabled: true, UpdatedAt: now.Add(time.Minute),
	}
	closedInput := input
	closedInput.AIAdultOnlyEnabled = false
	if _, err := s.UpdatePartyServices(ctx, party.ID, host.ID, closedInput); !errors.Is(err, ErrAIAdultOnly) {
		t.Fatalf("closed adult-only gate error = %v", err)
	}
	if _, err := s.UpdatePartyServices(ctx, party.ID, other.ID, input); !errors.Is(err, ErrNotFound) {
		t.Fatalf("non-host update error = %v", err)
	}
	updated, err := s.UpdatePartyServices(ctx, party.ID, host.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	if updated.TimeEnabled || !updated.WeatherEnabled || !updated.RadioEnabled || updated.RadioStation != "drone-zone" || !updated.AIEnabled || updated.WeatherLabel != "Portland, Maine" {
		t.Fatalf("unexpected updated settings: %#v", updated)
	}
	if _, err := s.UpdatePartyServices(ctx, party.ID, host.ID, ServiceSettingsInput{RadioEnabled: true, RadioStation: "http://example.test/live", UpdatedAt: now}); !errors.Is(err, ErrInvalidRadio) {
		t.Fatalf("arbitrary radio station error = %v", err)
	}
	if err := s.UpdatePartyOpenAI(ctx, party.ID, "project", "service-account", "key-old", "encrypted-key", "ready", 1000); err != nil {
		t.Fatal(err)
	}
	routing, err := s.RoutingServices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(routing) != 1 || routing[0].TimeEnabled || !routing[0].WeatherEnabled || !routing[0].RadioEnabled || routing[0].RadioStation != "drone-zone" || !routing[0].AIEnabled {
		t.Fatalf("unexpected routing services: %#v", routing)
	}
	if err := s.EnforceAIAdultOnlyGate(ctx, true, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	stillApproved, err := s.PartyServices(ctx, party.ID)
	if err != nil || !stillApproved.AIEnabled {
		t.Fatalf("open gate mutated the AI preference: %#v error=%v", stillApproved, err)
	}
	if err := s.EnforceAIAdultOnlyGate(ctx, false, now.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	closed, err := s.PartyServices(ctx, party.ID)
	if err != nil || closed.AIEnabled || !closed.WeatherEnabled || !closed.RadioEnabled {
		t.Fatalf("closed gate did not clear only the conversation preference: %#v error=%v", closed, err)
	}
	if _, err := s.db.Exec(`UPDATE party_services SET radio_station = '' WHERE party_id = ?`, party.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RoutingServices(ctx); err == nil || strings.Contains(err.Error(), party.ID) {
		t.Fatalf("corrupt radio routing state was not rejected generically: %v", err)
	}
}

func TestFirstActivePartyPhoneCanSetOnlyAnUnknownWeatherLocation(t *testing.T) {
	ctx := t.Context()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Date(2026, 8, 23, 20, 0, 0, 0, time.UTC)
	host, err := s.UpsertGoogleUser(ctx, GoogleProfile{Subject: "weather-host", Email: "host@example.test", Name: "Host"}, now, "usr_weather_host")
	if err != nil {
		t.Fatal(err)
	}
	party, err := s.CreateParty(ctx, NewParty{ID: "pty_weather_setup", Name: "Weather", Slug: "weather-setup", HostUserID: host.ID, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdatePartyOpenAI(ctx, party.ID, "project", "service-account", "key", "ciphertext", "ready", 1000); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateInvitation(ctx, NewInvitation{
		ID: "inv_weather_setup", PartyID: party.ID, CreatedByUserID: host.ID,
		TokenHash: secure.Hash("weather-setup-token"), ExpiresAt: now.Add(time.Hour), CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.ClaimInvitation(ctx, NewClaim{
		TokenHash: secure.Hash("weather-setup-token"), MemberID: "mem_weather_setup", DisplayName: "Phone", Extension: "101",
		DeviceID: "dev_weather_setup", DeviceLabel: "ATA", SIPUsername: "345678", SIPSecretCiphertext: "ciphertext",
		Provisioning: testProvisioning("weather-provision", now), Now: now,
	}); err != nil {
		t.Fatal(err)
	}
	routing, err := s.RoutingServices(ctx)
	if err != nil || len(routing) != 1 || routing[0].WeatherEnabled || !routing[0].WeatherSetupEnabled {
		t.Fatalf("unknown weather location did not expose only setup routing: %#v error=%v", routing, err)
	}
	valid := WeatherLocationInput{
		Query: "02138", Label: "Cambridge, Massachusetts", Latitude: 42.37, Longitude: -71.11, UpdatedAt: now.Add(time.Minute),
	}
	if _, _, err := s.SetWeatherLocationByDevice(ctx, party.ID, "bad endpoint", valid); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown endpoint weather update error = %v", err)
	}
	invalid := valid
	invalid.Label = "unsafe\nlabel"
	if _, _, err := s.SetWeatherLocationByDevice(ctx, party.ID, "345678", invalid); !errors.Is(err, ErrInvalidWeather) {
		t.Fatalf("unsafe weather location error = %v", err)
	}
	services, changed, err := s.SetWeatherLocationByDevice(ctx, party.ID, "345678", valid)
	if err != nil || !changed || !services.WeatherEnabled || services.WeatherQuery != "02138" || services.WeatherLabel != "Cambridge, Massachusetts" {
		t.Fatalf("first phone weather update = %#v changed=%t error=%v", services, changed, err)
	}
	routing, err = s.RoutingServices(ctx)
	if err != nil || !routing[0].WeatherEnabled || routing[0].WeatherSetupEnabled {
		t.Fatalf("resolved weather did not switch from setup to forecast routing: %#v error=%v", routing, err)
	}
	replacement := WeatherLocationInput{
		Query: "97205", Label: "Portland, Oregon", Latitude: 45.52, Longitude: -122.68, UpdatedAt: now.Add(2 * time.Minute),
	}
	services, changed, err = s.SetWeatherLocationByDevice(ctx, party.ID, "345678", replacement)
	if err != nil || changed || services.WeatherQuery != "02138" || services.WeatherLabel != "Cambridge, Massachusetts" {
		t.Fatalf("phone replaced an already-known weather location: %#v changed=%t error=%v", services, changed, err)
	}
	if _, err := s.UpdatePartyServices(ctx, party.ID, host.ID, ServiceSettingsInput{
		TimeEnabled: true, WeatherEnabled: false, WeatherQuery: services.WeatherQuery, WeatherLabel: services.WeatherLabel,
		WeatherLatitude: services.WeatherLatitude, WeatherLongitude: services.WeatherLongitude,
		RadioStation: radio.DefaultStationID, UpdatedAt: now.Add(3 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	routing, err = s.RoutingServices(ctx)
	if err != nil || routing[0].WeatherEnabled || routing[0].WeatherSetupEnabled {
		t.Fatalf("host-disabled known weather remained callable: %#v error=%v", routing, err)
	}
	if _, err := s.UpdatePartyServices(ctx, party.ID, host.ID, ServiceSettingsInput{
		TimeEnabled: true, WeatherEnabled: false, WeatherSetupAllowed: false,
		RadioStation: radio.DefaultStationID, UpdatedAt: now.Add(4 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	services, changed, err = s.SetWeatherLocationByDevice(ctx, party.ID, "345678", replacement)
	if err != nil || changed || services.WeatherEnabled || services.WeatherLabel != "" {
		t.Fatalf("phone bypassed the host-disabled unknown weather setting: %#v changed=%t error=%v", services, changed, err)
	}
}

func TestPartyMemberForDeviceUsesActiveAuthenticatedPartyBoundary(t *testing.T) {
	ctx := t.Context()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Date(2026, 8, 23, 21, 0, 0, 0, time.UTC)
	host, err := s.UpsertGoogleUser(ctx, GoogleProfile{Subject: "join-host", Email: "host@example.test", Name: "Host"}, now, "usr_join_host")
	if err != nil {
		t.Fatal(err)
	}
	party, err := s.CreateParty(ctx, NewParty{ID: "pty_join", Name: "Join", Slug: "join", HostUserID: host.ID, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateInvitation(ctx, NewInvitation{
		ID: "inv_join", PartyID: party.ID, CreatedByUserID: host.ID,
		TokenHash: secure.Hash("join-token"), ExpiresAt: now.Add(time.Hour), CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.ClaimInvitation(ctx, NewClaim{
		TokenHash: secure.Hash("join-token"), MemberID: "mem_join", DisplayName: "Austin", Extension: "101",
		DeviceID: "dev_join", DeviceLabel: "ATA", SIPUsername: "456789", SIPSecretCiphertext: "ciphertext",
		Provisioning: testProvisioning("join-provision", now), Now: now,
	}); err != nil {
		t.Fatal(err)
	}
	member, err := s.PartyMemberForDevice(ctx, party.ID, "456789")
	if err != nil || member.ID != "mem_join" || member.DisplayName != "Austin" || member.Extension != "101" {
		t.Fatalf("party member from phone = %#v, %v", member, err)
	}
	for _, test := range []struct{ partyID, username string }{
		{"pty_other", "456789"}, {party.ID, "000000"},
	} {
		if _, err := s.PartyMemberForDevice(ctx, test.partyID, test.username); !errors.Is(err, ErrNotFound) {
			t.Fatalf("PartyMemberForDevice(%q, %q) error = %v", test.partyID, test.username, err)
		}
	}
	if err := s.RevokeDevice(ctx, party.ID, host.ID, "dev_join", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PartyMemberForDevice(ctx, party.ID, "456789"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoked PartyMemberForDevice error = %v", err)
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
	if err := s.UpdatePartyOpenAI(ctx, party.ID, "proj_rotation", "svc_rotation", "key_old", "cipher_old", "ready", 1000); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdatePartyServices(ctx, party.ID, host.ID, ServiceSettingsInput{TimeEnabled: true, WeatherEnabled: true, WeatherQuery: "Portland", WeatherLabel: "Portland", AIEnabled: true, AIAdultOnlyEnabled: true, UpdatedAt: now}); err != nil {
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

func TestPartyOpenAISpendLimitUpdateIsHostScopedRetryableAndPausesRoutes(t *testing.T) {
	ctx := t.Context()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Date(2026, 8, 22, 19, 0, 0, 0, time.UTC)
	host, _ := s.UpsertGoogleUser(ctx, GoogleProfile{Subject: "spend-host", Email: "host@example.test", Name: "Host"}, now, "usr_spend_host")
	other, _ := s.UpsertGoogleUser(ctx, GoogleProfile{Subject: "spend-other", Email: "other@example.test", Name: "Other"}, now, "usr_spend_other")
	party, _ := s.CreateParty(ctx, NewParty{ID: "pty_spend", Name: "Party", Slug: "spend", HostUserID: host.ID, CreatedAt: now})
	if err := s.UpdatePartyOpenAI(ctx, party.ID, "proj_spend", "svc_spend", "key_spend", "cipher_spend", "ready", 1000); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdatePartyServices(ctx, party.ID, host.ID, ServiceSettingsInput{TimeEnabled: true, WeatherEnabled: true, WeatherQuery: "Portland", WeatherLabel: "Portland", AIEnabled: true, AIAdultOnlyEnabled: true, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := s.StartPartyOpenAISpendLimitUpdate(ctx, party.ID, other.ID, "proj_spend", 725); !errors.Is(err, ErrOpenAISpendLimit) {
		t.Fatalf("non-host spend update error = %v", err)
	}
	if err := s.StartPartyOpenAISpendLimitUpdate(ctx, party.ID, host.ID, "wrong_project", 725); !errors.Is(err, ErrOpenAISpendLimit) {
		t.Fatalf("wrong-project spend update error = %v", err)
	}
	if err := s.StartPartyOpenAISpendLimitUpdate(ctx, party.ID, host.ID, "proj_spend", 725); err != nil {
		t.Fatal(err)
	}
	updating, err := s.PartyForHost(ctx, party.ID, host.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updating.OpenAIStatus != "spend-updating" || updating.OpenAISpendLimitCents != 1000 || updating.OpenAISpendPendingCents != 725 || updating.OpenAISpendLimitStatus != "updating" {
		t.Fatalf("unexpected pending spend state: %#v", updating)
	}
	if routing, err := s.RoutingServices(ctx); err != nil || len(routing) != 1 || routing[0].AIEnabled || routing[0].WeatherEnabled {
		t.Fatalf("AI routes were not paused during spend update: %#v, %v", routing, err)
	}
	if err := s.StartPartyOpenAISpendLimitUpdate(ctx, party.ID, host.ID, "proj_spend", 500); !errors.Is(err, ErrOpenAISpendLimit) {
		t.Fatalf("concurrent amount error = %v", err)
	}
	if err := s.SetPartyOpenAISpendLimitError(ctx, party.ID, host.ID, "proj_spend", 725); err != nil {
		t.Fatal(err)
	}
	failed, err := s.PartyForHost(ctx, party.ID, host.ID)
	if err != nil || failed.OpenAIStatus != "spend-update-error" {
		t.Fatalf("legacy-safe error state was not recorded: %#v error=%v", failed, err)
	}
	if err := s.FinishPartyOpenAISpendLimitUpdate(ctx, party.ID, host.ID, "proj_spend", 500); !errors.Is(err, ErrOpenAISpendLimit) {
		t.Fatalf("wrong pending amount finish error = %v", err)
	}
	if err := s.FinishPartyOpenAISpendLimitUpdate(ctx, party.ID, host.ID, "proj_spend", 725); err != nil {
		t.Fatal(err)
	}
	ready, err := s.PartyForHost(ctx, party.ID, host.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ready.OpenAIStatus != "ready" || ready.OpenAISpendLimitCents != 725 || ready.OpenAISpendPendingCents != 0 || ready.OpenAISpendLimitStatus != "ready" {
		t.Fatalf("unexpected confirmed spend state: %#v", ready)
	}
	if routing, err := s.RoutingServices(ctx); err != nil || len(routing) != 1 || !routing[0].AIEnabled || !routing[0].WeatherEnabled {
		t.Fatalf("AI routes did not resume after spend update: %#v, %v", routing, err)
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
	);
	INSERT INTO parties (id, name, slug, host_user_id, openai_status, created_at)
	VALUES ('pty_legacy', 'Legacy', 'legacy', 'usr_legacy', 'ready', 1);
	INSERT INTO party_services (party_id, time_enabled, updated_at)
	VALUES ('pty_legacy', 1, 1)`)
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
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('party_services') WHERE name = 'radio_station'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("radio_station column count = %d", count)
	}
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('party_services') WHERE name = 'weather_setup_allowed'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("weather_setup_allowed column count = %d", count)
	}
	var weatherSetupAllowed int
	if err := store.db.QueryRow(`SELECT weather_setup_allowed FROM party_services WHERE party_id = 'pty_legacy'`).Scan(&weatherSetupAllowed); err != nil {
		t.Fatal(err)
	}
	if weatherSetupAllowed != 1 {
		t.Fatalf("legacy weather setup allowed = %d", weatherSetupAllowed)
	}
	var stationID string
	if err := store.db.QueryRow(`SELECT radio_station FROM party_services WHERE party_id = 'pty_legacy'`).Scan(&stationID); err != nil {
		t.Fatal(err)
	}
	if stationID != "groove-salad" {
		t.Fatalf("legacy radio station = %q", stationID)
	}
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('parties') WHERE name = 'openai_api_key_id'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("openai_api_key_id column count = %d", count)
	}
	for _, column := range []string{"openai_spend_limit_cents", "openai_spend_pending_cents", "openai_spend_limit_status"} {
		if err := store.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('parties') WHERE name = ?`, column).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("%s column count = %d", column, count)
		}
	}
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'device_readiness'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("device_readiness table count = %d", count)
	}
	var spendLimit int
	var pending sql.NullInt64
	var spendStatus string
	if err := store.db.QueryRow(`SELECT openai_spend_limit_cents, openai_spend_pending_cents, openai_spend_limit_status FROM parties WHERE id = 'pty_legacy'`).Scan(&spendLimit, &pending, &spendStatus); err != nil {
		t.Fatal(err)
	}
	if spendLimit != 0 || pending.Valid || spendStatus != "unknown" {
		t.Fatalf("legacy spend state = limit %d pending %#v status %q", spendLimit, pending, spendStatus)
	}
}
