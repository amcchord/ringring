package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Environment                string
	HTTPAddr                   string
	MetricsAddr                string
	BaseURL                    string
	DatabasePath               string
	MasterKey                  []byte
	SessionSecret              []byte
	GoogleClientID             string
	GoogleClientSecret         string
	HostSignupCode             string
	OpenAIAdminKey             string
	OpenAIPartySpendLimitCents int
	SIPPublicHost              string
	AsteriskConfigDir          string
	AsteriskAMIAddr            string
	AsteriskAMIUser            string
	AsteriskAMISecret          string
	FastAGIAddr                string
	AIAudioAddr                string
	AIRealtimeModel            string
	AICallMaxDuration          time.Duration
	AIMaxConcurrent            int
	AIAdultOnlyEnabled         bool
	VoiceAudioDir              string
	VoicePlaybackDir           string
	InviteTTL                  time.Duration
	DevAuth                    bool
}

func Load() (Config, error) {
	aiAdultOnlyEnabled, err := envStrictBool("AI_ADULT_ONLY_ENABLED", false)
	if err != nil {
		return Config{}, err
	}
	cfg := Config{
		Environment:                env("APP_ENV", "development"),
		HTTPAddr:                   env("HTTP_ADDR", ":8080"),
		MetricsAddr:                env("METRICS_ADDR", "127.0.0.1:9090"),
		BaseURL:                    strings.TrimRight(env("APP_BASE_URL", "http://localhost:8080"), "/"),
		DatabasePath:               env("DATABASE_PATH", "data/ringring.db"),
		GoogleClientID:             os.Getenv("GOOGLE_CLIENT_ID"),
		GoogleClientSecret:         os.Getenv("GOOGLE_CLIENT_SECRET"),
		HostSignupCode:             os.Getenv("HOST_SIGNUP_CODE"),
		OpenAIAdminKey:             os.Getenv("OPENAI_ADMIN_KEY"),
		OpenAIPartySpendLimitCents: envInt("OPENAI_PARTY_SPEND_LIMIT_CENTS", 1000),
		SIPPublicHost:              env("SIP_PUBLIC_HOST", "localhost"),
		AsteriskConfigDir:          os.Getenv("ASTERISK_CONFIG_DIR"),
		AsteriskAMIAddr:            env("ASTERISK_AMI_ADDR", "asterisk:5038"),
		AsteriskAMIUser:            env("ASTERISK_AMI_USER", "ringring"),
		AsteriskAMISecret:          os.Getenv("ASTERISK_AMI_SECRET"),
		FastAGIAddr:                env("FASTAGI_ADDR", ":4573"),
		AIAudioAddr:                env("AI_AUDIO_ADDR", ":4574"),
		AIRealtimeModel:            env("AI_REALTIME_MODEL", "gpt-realtime-2.1"),
		AICallMaxDuration:          envDuration("AI_CALL_MAX_DURATION", 3*time.Minute),
		AIMaxConcurrent:            envInt("AI_MAX_CONCURRENT", 2),
		AIAdultOnlyEnabled:         aiAdultOnlyEnabled,
		VoiceAudioDir:              env("VOICE_AUDIO_DIR", "/asterisk/audio"),
		VoicePlaybackDir:           env("VOICE_PLAYBACK_DIR", "/var/lib/ringring/asterisk/audio"),
		InviteTTL:                  48 * time.Hour,
		DevAuth:                    envBool("DEV_AUTH", false),
	}

	if _, err := url.ParseRequestURI(cfg.BaseURL); err != nil {
		return Config{}, fmt.Errorf("APP_BASE_URL: %w", err)
	}

	if cfg.MasterKey, err = decodeKey("RINGRING_MASTER_KEY"); err != nil {
		return Config{}, err
	}
	if cfg.SessionSecret, err = decodeKey("SESSION_SECRET"); err != nil {
		return Config{}, err
	}

	if cfg.Environment == "production" {
		if len(cfg.MasterKey) != 32 || len(cfg.SessionSecret) != 32 {
			return Config{}, errors.New("production requires 32-byte RINGRING_MASTER_KEY and SESSION_SECRET")
		}
		if !strings.HasPrefix(cfg.BaseURL, "https://") {
			return Config{}, errors.New("production APP_BASE_URL must use https")
		}
		if cfg.DevAuth {
			return Config{}, errors.New("DEV_AUTH cannot be enabled in production")
		}
		if cfg.MetricsAddr != "127.0.0.1:9090" {
			return Config{}, errors.New("production METRICS_ADDR must remain 127.0.0.1:9090")
		}
	}
	if cfg.AICallMaxDuration < 30*time.Second || cfg.AICallMaxDuration > 10*time.Minute {
		return Config{}, errors.New("AI_CALL_MAX_DURATION must be between 30s and 10m")
	}
	if cfg.AIMaxConcurrent < 1 || cfg.AIMaxConcurrent > 20 {
		return Config{}, errors.New("AI_MAX_CONCURRENT must be between 1 and 20")
	}

	if len(cfg.MasterKey) == 0 {
		cfg.MasterKey = make([]byte, 32)
	}
	if len(cfg.SessionSecret) == 0 {
		cfg.SessionSecret = make([]byte, 32)
	}

	return cfg, nil
}

func (c Config) GoogleAuthConfigured() bool {
	return c.GoogleClientID != "" && c.GoogleClientSecret != ""
}

func (c Config) HostSignupEnabled() bool {
	return c.Environment != "production" || c.HostSignupCode != ""
}

func (c Config) OpenAIProvisioningConfigured() bool {
	return c.OpenAIAdminKey != ""
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func envBool(name string, fallback bool) bool {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	return err == nil && parsed
}

func envStrictBool(name string, fallback bool) (bool, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	switch value {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("%s must be true or false", name)
	}
}

func envInt(name string, fallback int) int {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 {
		return fallback
	}
	return parsed
}

func envDuration(name string, fallback time.Duration) time.Duration {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func decodeKey(name string) ([]byte, error) {
	value := os.Getenv(name)
	if value == "" {
		return nil, nil
	}
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("%s must be base64: %w", name, err)
	}
	if len(decoded) != 32 {
		return nil, fmt.Errorf("%s must decode to exactly 32 bytes", name)
	}
	return decoded, nil
}
