package config

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestMetricsAddressDefaultsAndCanBeOverridden(t *testing.T) {
	for _, name := range []string{
		"APP_ENV", "APP_BASE_URL", "METRICS_ADDR", "RINGRING_MASTER_KEY", "SESSION_SECRET",
		"AI_CALL_MAX_DURATION", "AI_MAX_CONCURRENT", "AI_CHILD_SAFETY_APPROVED",
	} {
		t.Setenv(name, "")
	}
	defaultConfig, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if defaultConfig.MetricsAddr != "127.0.0.1:9090" {
		t.Fatalf("default metrics address = %q", defaultConfig.MetricsAddr)
	}

	t.Setenv("METRICS_ADDR", "127.0.0.1:19090")
	overridden, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if overridden.MetricsAddr != "127.0.0.1:19090" {
		t.Fatalf("overridden metrics address = %q", overridden.MetricsAddr)
	}
}

func TestAIChildSafetyApprovalIsExplicitAndStrict(t *testing.T) {
	for _, name := range []string{"APP_ENV", "APP_BASE_URL", "RINGRING_MASTER_KEY", "SESSION_SECRET", "AI_CHILD_SAFETY_APPROVED"} {
		t.Setenv(name, "")
	}
	closed, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if closed.AIChildSafetyApproved {
		t.Fatal("AI child-safety gate defaulted open")
	}

	t.Setenv("AI_CHILD_SAFETY_APPROVED", "true")
	approved, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !approved.AIChildSafetyApproved {
		t.Fatal("explicit AI child-safety approval was ignored")
	}

	t.Setenv("AI_CHILD_SAFETY_APPROVED", "TRUE")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "AI_CHILD_SAFETY_APPROVED") {
		t.Fatalf("malformed approval did not fail closed: %v", err)
	}
}

func TestProductionMetricsAddressMustRemainLoopbackOnly(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("APP_BASE_URL", "https://ringring.test")
	t.Setenv("METRICS_ADDR", "0.0.0.0:9090")
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	t.Setenv("RINGRING_MASTER_KEY", key)
	t.Setenv("SESSION_SECRET", key)
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "METRICS_ADDR") {
		t.Fatalf("production accepted a non-loopback metrics listener: %v", err)
	}
}
