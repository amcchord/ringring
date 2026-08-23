package store

import (
	"testing"
	"time"

	"github.com/amcchord/ringring/internal/secure"
)

func TestInvitationClaimMayReturnCredentialsWithoutCreatingAnotherBearerToken(t *testing.T) {
	ctx := t.Context()
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	now := time.Date(2026, 8, 23, 18, 0, 0, 0, time.UTC)
	host, err := database.UpsertGoogleUser(ctx, GoogleProfile{
		Subject: "native-invite-host", Email: "host@example.test", Name: "Host",
	}, now, "usr_native_invite")
	if err != nil {
		t.Fatal(err)
	}
	party, err := database.CreateParty(ctx, NewParty{
		ID: "pty_native_invite", Name: "Test party", Slug: "native-invite", HostUserID: host.ID, CreatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	token := "native-invitation-secret"
	if err := database.CreateInvitation(ctx, NewInvitation{
		ID: "inv_native_invite", PartyID: party.ID, CreatedByUserID: host.ID,
		TokenHash: secure.Hash(token), ExpiresAt: now.Add(time.Hour), CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	_, _, device, err := database.ClaimInvitation(ctx, NewClaim{
		TokenHash: secure.Hash(token), MemberID: "mem_native_invite", DisplayName: "Studio phone", Extension: "101",
		DeviceID: "dev_native_invite", DeviceLabel: "Phone app", SIPUsername: "483726",
		SIPSecretCiphertext: "ciphertext", Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	var provisioningTokens int
	if err := database.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM device_provisioning_tokens WHERE device_id = ?`, device.ID).Scan(&provisioningTokens); err != nil {
		t.Fatal(err)
	}
	if provisioningTokens != 0 {
		t.Fatalf("native invitation created %d undisclosed provisioning tokens", provisioningTokens)
	}
}
