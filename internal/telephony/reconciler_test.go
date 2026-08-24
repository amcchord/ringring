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
	return []model.RoutingServices{{PartyID: "pty_safe", TimeEnabled: true}}, nil
}

type adultOnlyDecryptor struct{}

func (adultOnlyDecryptor) Decrypt(string, []byte) (string, error) { return "secret", nil }

func TestReconcilerAlwaysOmitsRemovedAIConversationRoute(t *testing.T) {
	directory := t.TempDir()
	reconciler := &Reconciler{
		Source: childSafetyRoutingSource{}, Cipher: adultOnlyDecryptor{}, ConfigDir: directory,
	}
	if err := reconciler.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	generated, err := os.ReadFile(filepath.Join(directory, "extensions.conf"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(generated), "exten => *14") || strings.Contains(string(generated), "AudioSocket") || !strings.Contains(string(generated), "exten => *11") {
		t.Fatalf("removed AI route was generated or an ordinary route disappeared:\n%s", generated)
	}
}
