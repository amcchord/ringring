package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/amcchord/ringring/internal/secure"
	"github.com/amcchord/ringring/internal/store"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: sip-smoke-state <database-path>")
		os.Exit(2)
	}
	ctx := context.Background()
	database, err := store.Open(os.Args[1])
	if err != nil {
		fail(err)
	}
	defer database.Close()
	cipher, err := secure.NewCipher(make([]byte, 32))
	if err != nil {
		fail(err)
	}
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	host, err := database.UpsertGoogleUser(ctx, store.GoogleProfile{
		Subject: "dev:smoke@example.test", Email: "smoke@example.test", Name: "Smoke host",
	}, now, "usr_smoke")
	if err != nil {
		fail(err)
	}
	party, err := database.CreateParty(ctx, store.NewParty{
		ID: "pty_smoke", Name: "Smoke party", Slug: "smoke-party", HostUserID: host.ID, CreatedAt: now,
	})
	if err != nil {
		fail(err)
	}
	devices := []struct {
		inviteID, token, memberID, deviceID, extension, sipUsername, sipSecret string
	}{
		{"inv_smoke_a", "invite-smoke-a", "mem_smoke_a", "dev_smoke_a", "101", "rr_smoke_a", "smoke-only-a-7Qm4s9Vx"},
		{"inv_smoke_b", "invite-smoke-b", "mem_smoke_b", "dev_smoke_b", "102", "rr_smoke_b", "smoke-only-b-2Kp8w6Nz"},
	}
	for _, item := range devices {
		if err := database.CreateInvitation(ctx, store.NewInvitation{
			ID: item.inviteID, PartyID: party.ID, CreatedByUserID: host.ID,
			TokenHash: secure.Hash(item.token), ExpiresAt: now.Add(time.Hour), CreatedAt: now,
		}); err != nil {
			fail(err)
		}
		ciphertext, err := cipher.Encrypt(item.sipSecret, []byte(item.deviceID))
		if err != nil {
			fail(err)
		}
		_, _, _, err = database.ClaimInvitation(ctx, store.NewClaim{
			TokenHash: secure.Hash(item.token), MemberID: item.memberID, DisplayName: "Smoke phone",
			Extension: item.extension, DeviceID: item.deviceID, DeviceLabel: "Smoke client",
			SIPUsername: item.sipUsername, SIPSecretCiphertext: ciphertext,
			Provisioning: store.NewProvisioningToken{
				TokenHash: secure.Hash("provision-" + item.deviceID), ExpiresAt: now.Add(30 * time.Minute), CreatedAt: now,
			},
			Now: now,
		})
		if err != nil {
			fail(err)
		}
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
