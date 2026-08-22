package store

import (
	"context"
	"errors"
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
