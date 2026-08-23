package config

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestMetricsAddressDefaultsAndCanBeOverridden(t *testing.T) {
	for _, name := range []string{
		"APP_ENV", "APP_BASE_URL", "METRICS_ADDR", "RINGRING_MASTER_KEY", "SESSION_SECRET",
		"AI_CALL_MAX_DURATION", "AI_MAX_CONCURRENT", "AI_ADULT_ONLY_ENABLED",
		"APNS_TEAM_ID", "APNS_KEY_ID", "APNS_PRIVATE_KEY_FILE", "APNS_BUNDLE_ID", "APNS_ENVIRONMENT",
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

func TestAPNsConfigurationIsOptionalButAtomic(t *testing.T) {
	for _, name := range []string{"APNS_TEAM_ID", "APNS_KEY_ID", "APNS_PRIVATE_KEY_FILE", "APNS_BUNDLE_ID", "APNS_ENVIRONMENT"} {
		t.Setenv(name, "")
	}
	disabled, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if disabled.APNSConfigured() || disabled.APNSEnvironment != "production" || disabled.APNSBundleID != "com.mcchord.ringring" {
		t.Fatalf("unexpected APNs defaults: %#v", disabled)
	}

	t.Setenv("APNS_TEAM_ID", "7PTN7E8EDS")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "configured together") {
		t.Fatalf("partial APNs setup was accepted: %v", err)
	}
	t.Setenv("APNS_KEY_ID", "ABC123DEFG")
	t.Setenv("APNS_PRIVATE_KEY_FILE", "/run/secrets/ringring-apns/AuthKey.p8")
	enabled, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !enabled.APNSConfigured() {
		t.Fatal("complete APNs setup was not enabled")
	}

	t.Setenv("APNS_ENVIRONMENT", "staging")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "APNS_ENVIRONMENT") {
		t.Fatalf("invalid APNs environment was accepted: %v", err)
	}
}

func TestAIAdultOnlyGateIsExplicitAndStrict(t *testing.T) {
	for _, name := range []string{"APP_ENV", "APP_BASE_URL", "RINGRING_MASTER_KEY", "SESSION_SECRET", "AI_ADULT_ONLY_ENABLED"} {
		t.Setenv(name, "")
	}
	closed, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if closed.AIAdultOnlyEnabled {
		t.Fatal("AI adult-only gate defaulted open")
	}

	t.Setenv("AI_ADULT_ONLY_ENABLED", "true")
	approved, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !approved.AIAdultOnlyEnabled {
		t.Fatal("explicit AI adult-only enablement was ignored")
	}

	t.Setenv("AI_ADULT_ONLY_ENABLED", "TRUE")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "AI_ADULT_ONLY_ENABLED") {
		t.Fatalf("malformed adult-only gate did not fail closed: %v", err)
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
