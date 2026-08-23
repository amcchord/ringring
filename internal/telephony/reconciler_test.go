package telephony

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amcchord/ringring/internal/model"
)

type childSafetyRoutingSource struct{}

func (childSafetyRoutingSource) RoutingDevices(context.Context) ([]model.RoutingDevice, error) {
	return []model.RoutingDevice{{
		PartyID: "pty_safe", MemberID: "mem_safe", Extension: "101", DeviceID: "dev_safe",
		SIPUsername: "rrd_safe", SIPSecretCiphertext: "ciphertext",
	}}, nil
}

func (childSafetyRoutingSource) RoutingServices(context.Context) ([]model.RoutingServices, error) {
	return []model.RoutingServices{{PartyID: "pty_safe", TimeEnabled: true, AIEnabled: true}}, nil
}

type adultOnlyDecryptor struct{}

func (adultOnlyDecryptor) Decrypt(string, []byte) (string, error) { return "secret", nil }

func TestReconcilerFiltersAIWhenAdultOnlyGateIsClosed(t *testing.T) {
	directory := t.TempDir()
	reconciler := &Reconciler{
		Source: childSafetyRoutingSource{}, Cipher: adultOnlyDecryptor{}, ConfigDir: directory,
	}
	if err := reconciler.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	closed, err := os.ReadFile(filepath.Join(directory, "extensions.conf"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(closed), "exten => *14") || strings.Contains(string(closed), "AudioSocket") || !strings.Contains(string(closed), "exten => *11") {
		t.Fatalf("closed gate generated an AI route or removed an ordinary route:\n%s", closed)
	}

	reconciler.AIAdultOnlyEnabled = true
	if err := reconciler.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	approved, err := os.ReadFile(filepath.Join(directory, "extensions.conf"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(approved), "exten => *14") || !strings.Contains(string(approved), "AudioSocket") {
		t.Fatalf("approved gate did not generate the AI route:\n%s", approved)
	}
}
