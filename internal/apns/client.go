package apns

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	productionEndpoint   = "https://api.push.apple.com"
	developmentEndpoint  = "https://api.sandbox.push.apple.com"
	providerTokenTTL     = 50 * time.Minute
	maximumResponseBytes = 8 << 10
)

var (
	identifierPattern  = regexp.MustCompile(`^[A-Z0-9]{10}$`)
	bundleIDPattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]{2,254}$`)
	deviceTokenPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

// Config contains only provider-side APNs settings. The private key stays in a
// root-readable file outside the repository and is read once at startup.
type Config struct {
	TeamID         string
	KeyID          string
	PrivateKeyFile string
	BundleID       string
	Environment    string
	HTTPClient     *http.Client
	Now            func() time.Time
}

type Client struct {
	teamID      string
	keyID       string
	topic       string
	endpoint    string
	privateKey  *ecdsa.PrivateKey
	httpClient  *http.Client
	now         func() time.Time
	mu          sync.Mutex
	cachedToken string
	tokenMadeAt time.Time
}

// SendResult lets callers forget registrations APNs has declared permanently
// invalid without exposing the device token in logs or errors.
type SendResult struct {
	Unregistered bool
}

type responseBody struct {
	Reason string `json:"reason"`
}

func New(config Config) (*Client, error) {
	if !identifierPattern.MatchString(config.TeamID) || !identifierPattern.MatchString(config.KeyID) {
		return nil, errors.New("APNs team and key identifiers must be 10 uppercase letters or digits")
	}
	if !bundleIDPattern.MatchString(config.BundleID) {
		return nil, errors.New("APNs bundle identifier is invalid")
	}
	endpoint := productionEndpoint
	switch config.Environment {
	case "production":
	case "development":
		endpoint = developmentEndpoint
	default:
		return nil, errors.New("APNs environment must be production or development")
	}
	encoded, err := os.ReadFile(config.PrivateKeyFile)
	if err != nil {
		return nil, fmt.Errorf("read APNs private key: %w", err)
	}
	block, remainder := pem.Decode(encoded)
	if block == nil || len(bytes.TrimSpace(remainder)) != 0 || block.Type != "PRIVATE KEY" {
		return nil, errors.New("APNs private key must contain one PKCS#8 private key")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse APNs private key: %w", err)
	}
	privateKey, ok := parsed.(*ecdsa.PrivateKey)
	if !ok || privateKey.Curve != elliptic.P256() {
		return nil, errors.New("APNs private key must be an EC P-256 key")
	}
	client := config.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	return &Client{
		teamID: config.TeamID, keyID: config.KeyID, topic: config.BundleID + ".voip",
		endpoint: endpoint, privateKey: privateKey, httpClient: client, now: now,
	}, nil
}

// SendVoIP sends a content-minimized wake-up. Party names, caller names,
// extensions, SIP identities, and credentials never enter the APNs payload.
func (c *Client) SendVoIP(ctx context.Context, deviceToken, callID string) (SendResult, error) {
	deviceToken = strings.ToLower(strings.TrimSpace(deviceToken))
	if !deviceTokenPattern.MatchString(deviceToken) {
		return SendResult{}, errors.New("APNs device token is invalid")
	}
	if _, err := parseUUID(callID); err != nil {
		return SendResult{}, err
	}
	providerToken, err := c.providerToken()
	if err != nil {
		return SendResult{}, err
	}
	payload, err := json.Marshal(struct {
		APS    struct{} `json:"aps"`
		CallID string   `json:"call_id"`
	}{CallID: callID})
	if err != nil {
		return SendResult{}, fmt.Errorf("encode APNs VoIP payload: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/3/device/"+deviceToken, bytes.NewReader(payload))
	if err != nil {
		return SendResult{}, fmt.Errorf("create APNs request: %w", err)
	}
	request.Header.Set("authorization", "bearer "+providerToken)
	request.Header.Set("apns-push-type", "voip")
	request.Header.Set("apns-topic", c.topic)
	request.Header.Set("apns-priority", "10")
	request.Header.Set("apns-expiration", "0")
	request.Header.Set("content-type", "application/json")

	response, err := c.httpClient.Do(request)
	if err != nil {
		return SendResult{}, fmt.Errorf("send APNs VoIP notification: %w", err)
	}
	defer response.Body.Close()
	limited := io.LimitReader(response.Body, maximumResponseBytes)
	body, err := io.ReadAll(limited)
	if err != nil {
		return SendResult{}, fmt.Errorf("read APNs response: %w", err)
	}
	if response.StatusCode == http.StatusOK {
		return SendResult{}, nil
	}
	var failure responseBody
	_ = json.Unmarshal(body, &failure)
	if failure.Reason == "Unregistered" || failure.Reason == "BadDeviceToken" || failure.Reason == "DeviceTokenNotForTopic" {
		return SendResult{Unregistered: true}, nil
	}
	if failure.Reason == "ExpiredProviderToken" || failure.Reason == "InvalidProviderToken" {
		c.clearProviderToken()
	}
	return SendResult{}, fmt.Errorf("APNs rejected VoIP notification with status %d (%s)", response.StatusCode, safeReason(failure.Reason))
}

func (c *Client) providerToken() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now().UTC()
	if c.cachedToken != "" && now.Sub(c.tokenMadeAt) < providerTokenTTL {
		return c.cachedToken, nil
	}
	header, _ := json.Marshal(map[string]string{"alg": "ES256", "kid": c.keyID})
	claims, _ := json.Marshal(map[string]any{"iss": c.teamID, "iat": now.Unix()})
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(claims)
	digest := sha256.Sum256([]byte(unsigned))
	r, s, err := ecdsa.Sign(rand.Reader, c.privateKey, digest[:])
	if err != nil {
		return "", fmt.Errorf("sign APNs provider token: %w", err)
	}
	signature := make([]byte, 64)
	writePadded(signature[:32], r)
	writePadded(signature[32:], s)
	c.cachedToken = unsigned + "." + base64.RawURLEncoding.EncodeToString(signature)
	c.tokenMadeAt = now
	return c.cachedToken, nil
}

func (c *Client) clearProviderToken() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cachedToken = ""
	c.tokenMadeAt = time.Time{}
}

func writePadded(destination []byte, value *big.Int) {
	encoded := value.Bytes()
	copy(destination[len(destination)-len(encoded):], encoded)
}

func parseUUID(value string) ([16]byte, error) {
	var result [16]byte
	compact := strings.ReplaceAll(strings.ToLower(value), "-", "")
	if len(compact) != 32 {
		return result, errors.New("call identifier is invalid")
	}
	decoded, err := hex.DecodeString(compact)
	if err != nil {
		return result, errors.New("call identifier is invalid")
	}
	copy(result[:], decoded)
	return result, nil
}

func safeReason(reason string) string {
	if reason == "" {
		return "unknown"
	}
	for _, character := range reason {
		if (character < 'A' || character > 'Z') && (character < 'a' || character > 'z') {
			return "unknown"
		}
	}
	return reason
}
