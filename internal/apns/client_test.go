package apns

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestSendVoIPUsesContentMinimizedAppleContract(t *testing.T) {
	keyFile := writeTestKey(t)
	requests := 0
	client, err := New(Config{
		TeamID: "7PTN7E8EDS", KeyID: "ABC123DEFG", PrivateKeyFile: keyFile,
		BundleID: "com.mcchord.ringring", Environment: "production",
		Now: func() time.Time { return time.Unix(1_800_000_000, 0) },
		HTTPClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			requests++
			if request.URL.Scheme != "https" || request.URL.Host != "api.push.apple.com" ||
				request.Header.Get("apns-push-type") != "voip" ||
				request.Header.Get("apns-topic") != "com.mcchord.ringring.voip" ||
				request.Header.Get("apns-expiration") != "0" ||
				!strings.HasPrefix(request.Header.Get("authorization"), "bearer ") {
				t.Fatalf("unexpected APNs request: %#v", request)
			}
			body, readErr := io.ReadAll(request.Body)
			if readErr != nil {
				t.Fatal(readErr)
			}
			var payload map[string]any
			if json.Unmarshal(body, &payload) != nil || len(payload) != 2 || payload["call_id"] != "4cdb5b42-d53d-4f43-9151-bd33a5324ed7" {
				t.Fatalf("unexpected minimized payload: %s", body)
			}
			if aps, ok := payload["aps"].(map[string]any); !ok || len(aps) != 0 {
				t.Fatalf("unexpected APS body: %#v", payload["aps"])
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	deviceToken := strings.Repeat("ab", 32)
	if _, err := client.SendVoIP(context.Background(), deviceToken, "4cdb5b42-d53d-4f43-9151-bd33a5324ed7"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.SendVoIP(context.Background(), deviceToken, "4cdb5b42-d53d-4f43-9151-bd33a5324ed7"); err != nil {
		t.Fatal(err)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}
}

func TestSendVoIPMarksDeadTokensWithoutLeakingThem(t *testing.T) {
	client, err := New(Config{
		TeamID: "7PTN7E8EDS", KeyID: "ABC123DEFG", PrivateKeyFile: writeTestKey(t),
		BundleID: "com.mcchord.ringring", Environment: "development",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.URL.Host != "api.sandbox.push.apple.com" {
				t.Fatalf("host = %q", request.URL.Host)
			}
			return &http.Response{
				StatusCode: http.StatusGone,
				Body:       io.NopCloser(strings.NewReader(`{"reason":"Unregistered"}`)), Header: make(http.Header),
			}, nil
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.SendVoIP(context.Background(), strings.Repeat("12", 32), "4cdb5b42-d53d-4f43-9151-bd33a5324ed7")
	if err != nil || !result.Unregistered {
		t.Fatalf("result = %#v, error = %v", result, err)
	}
}

func TestNewAndSendRejectInvalidConfigurationAndInputs(t *testing.T) {
	keyFile := writeTestKey(t)
	if _, err := New(Config{TeamID: "short", KeyID: "ABC123DEFG", PrivateKeyFile: keyFile, BundleID: "com.mcchord.ringring", Environment: "production"}); err == nil {
		t.Fatal("invalid team ID was accepted")
	}
	client, err := New(Config{TeamID: "7PTN7E8EDS", KeyID: "ABC123DEFG", PrivateKeyFile: keyFile, BundleID: "com.mcchord.ringring", Environment: "production"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.SendVoIP(context.Background(), "not-a-token", "4cdb5b42-d53d-4f43-9151-bd33a5324ed7"); err == nil {
		t.Fatal("invalid device token was accepted")
	}
	if _, err := client.SendVoIP(context.Background(), strings.Repeat("ab", 32), "not-a-call-id"); err == nil {
		t.Fatal("invalid call ID was accepted")
	}
}

func writeTestKey(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "AuthKey_TEST.p8")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded}), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
