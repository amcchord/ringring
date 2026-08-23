package apns

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// TestLiveProviderAuthentication is deliberately opt-in. It uses an invalid,
// all-zero device token so it can verify the provider key and topic without
// waking or identifying a real phone.
func TestLiveProviderAuthentication(t *testing.T) {
	if os.Getenv("RINGRING_APNS_LIVE") != "1" {
		t.Skip("set RINGRING_APNS_LIVE=1 with the APNs test configuration")
	}
	client, err := New(Config{
		TeamID:         os.Getenv("APNS_TEAM_ID"),
		KeyID:          os.Getenv("APNS_KEY_ID"),
		PrivateKeyFile: os.Getenv("APNS_PRIVATE_KEY_FILE"),
		BundleID:       os.Getenv("APNS_BUNDLE_ID"),
		Environment:    os.Getenv("APNS_ENVIRONMENT"),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := client.SendVoIP(ctx, strings.Repeat("0", 64), "00000000-0000-4000-8000-000000000000")
	if err != nil {
		t.Fatalf("provider authentication or topic validation failed: %v", err)
	}
	if !result.Unregistered {
		t.Fatal("APNs unexpectedly accepted the intentionally invalid device token")
	}
}
