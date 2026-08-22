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
	VoiceAudioDir              string
	VoicePlaybackDir           string
	InviteTTL                  time.Duration
	DevAuth                    bool
}

func Load() (Config, error) {
	cfg := Config{
		Environment:                env("APP_ENV", "development"),
		HTTPAddr:                   env("HTTP_ADDR", ":8080"),
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
		VoiceAudioDir:              env("VOICE_AUDIO_DIR", "/asterisk/audio"),
		VoicePlaybackDir:           env("VOICE_PLAYBACK_DIR", "/var/lib/ringring/asterisk/audio"),
		InviteTTL:                  48 * time.Hour,
		DevAuth:                    envBool("DEV_AUTH", false),
	}

	if _, err := url.ParseRequestURI(cfg.BaseURL); err != nil {
		return Config{}, fmt.Errorf("APP_BASE_URL: %w", err)
	}

	var err error
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
