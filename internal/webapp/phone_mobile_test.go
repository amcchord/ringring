package webapp

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/amcchord/ringring/internal/config"
	"github.com/amcchord/ringring/internal/secure"
	"github.com/amcchord/ringring/internal/store"
	"github.com/amcchord/ringring/internal/telephony"
)

func TestAuthenticatedPhoneStateIncludesFreshDirectoryAndLiveCallButtons(t *testing.T) {
	app, database, username, password := newPhoneMobileTestApp(t)
	app.calls = &fakeActiveConferences{calls: []telephony.ActiveConference{{
		Name: "rrc-pty_mobile-102", PartyID: "pty_mobile", JoinExtension: "102",
		Endpoints: []string{"sip_mobile", "sip_friend"},
	}}}
	server := httptest.NewServer(app)
	defer server.Close()

	request, _ := http.NewRequest(http.MethodGet, server.URL+"/api/v1/phone/state", nil)
	request.SetBasicAuth(username, password)
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body := readBody(t, response)
	if response.StatusCode != http.StatusOK || response.Header.Get("Cache-Control") != "no-store, max-age=0" || response.Header.Get("Vary") != "Authorization" {
		t.Fatalf("state response status=%d headers=%v body=%s", response.StatusCode, response.Header, body)
	}
	var state phoneStateDocument
	if err := json.Unmarshal([]byte(body), &state); err != nil {
		t.Fatal(err)
	}
	if state.Version != 1 || state.Extension != "101" {
		t.Fatalf("state = %#v", state)
	}
	foundFriend, foundCall := false, false
	for _, destination := range state.Destinations {
		if destination.Kind == "person" && destination.Dial == "102" && destination.Label == "Friend phone" {
			foundFriend = true
		}
		if destination.Kind == "call" && destination.Dial == "*16102" && strings.Contains(destination.Label, "Mobile phone") && strings.Contains(destination.Label, "Friend phone") {
			foundCall = true
		}
	}
	if !foundFriend || !foundCall {
		t.Fatalf("refreshed destinations = %#v", state.Destinations)
	}

	bad, _ := http.NewRequest(http.MethodGet, server.URL+"/api/v1/phone/state", nil)
	bad.SetBasicAuth(username, "wrong-password")
	badResponse, err := server.Client().Do(bad)
	if err != nil {
		t.Fatal(err)
	}
	defer badResponse.Body.Close()
	if badResponse.StatusCode != http.StatusUnauthorized || badResponse.Header.Get("WWW-Authenticate") == "" {
		t.Fatalf("bad credential response = %d %v", badResponse.StatusCode, badResponse.Header)
	}
	_ = database
}

func TestPhoneCanRegisterReplaceAndDeleteItsOwnVoIPToken(t *testing.T) {
	app, database, username, password := newPhoneMobileTestApp(t)
	server := httptest.NewServer(app)
	defer server.Close()

	token := strings.Repeat("ab", 32)
	payload := `{"token":"` + token + `","environment":"production"}`
	request, _ := http.NewRequest(http.MethodPut, server.URL+"/api/v1/phone/push", strings.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	request.SetBasicAuth(username, password)
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("push registration status=%d body=%s", response.StatusCode, body)
	}
	registrations, err := database.PhonePushRegistrationsForExtension(t.Context(), "pty_mobile", "101")
	if err != nil {
		t.Fatal(err)
	}
	if len(registrations) != 1 || registrations[0].TokenCiphertext == token || registrations[0].TokenCiphertext == "" {
		t.Fatalf("push registration was absent or plaintext: %#v", registrations)
	}

	remove, _ := http.NewRequest(http.MethodDelete, server.URL+"/api/v1/phone/push", nil)
	remove.SetBasicAuth(username, password)
	removed, err := server.Client().Do(remove)
	if err != nil {
		t.Fatal(err)
	}
	defer removed.Body.Close()
	if removed.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status = %d", removed.StatusCode)
	}
	registrations, err = database.PhonePushRegistrationsForExtension(t.Context(), "pty_mobile", "101")
	if err != nil || len(registrations) != 0 {
		t.Fatalf("registrations after delete = %#v, error = %v", registrations, err)
	}
}

func newPhoneMobileTestApp(t *testing.T) (*App, *store.Store, string, string) {
	t.Helper()
	database, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	cipher, err := secure.NewCipher(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 23, 18, 0, 0, 0, time.UTC)
	host, err := database.UpsertGoogleUser(t.Context(), store.GoogleProfile{Subject: "mobile-host", Email: "host@example.test", Name: "Host"}, now, "usr_mobile")
	if err != nil {
		t.Fatal(err)
	}
	party, err := database.CreateParty(t.Context(), store.NewParty{ID: "pty_mobile", Name: "Mobile", Slug: "mobile", HostUserID: host.ID, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	seed := func(tokenCharacter, memberID, name, extension, deviceID, username, password string) {
		token := strings.Repeat(tokenCharacter, 43)
		if err := database.CreateInvitation(t.Context(), store.NewInvitation{ID: "inv_" + memberID, PartyID: party.ID, CreatedByUserID: host.ID, TokenHash: secure.Hash(token), ExpiresAt: now.Add(time.Hour), CreatedAt: now}); err != nil {
			t.Fatal(err)
		}
		secret, err := cipher.Encrypt(password, []byte(deviceID))
		if err != nil {
			t.Fatal(err)
		}
		if _, _, _, err := database.ClaimInvitation(t.Context(), store.NewClaim{
			TokenHash: secure.Hash(token), MemberID: memberID, DisplayName: name, Extension: extension,
			DeviceID: deviceID, DeviceLabel: "iPhone", SIPUsername: username, SIPSecretCiphertext: secret,
			Provisioning: store.NewProvisioningToken{TokenHash: secure.Hash("provision-" + memberID), ExpiresAt: now.Add(time.Hour), CreatedAt: now}, Now: now,
		}); err != nil {
			t.Fatal(err)
		}
	}
	password := "123456789012"
	seed("m", "mem_mobile", "Mobile phone", "101", "dev_mobile", "sip_mobile", password)
	seed("f", "mem_friend", "Friend phone", "102", "dev_friend", "sip_friend", "210987654321")
	cfg := config.Config{
		Environment: "development", BaseURL: "http://ringring.test", SIPPublicHost: "sip.example.test",
		MasterKey: make([]byte, 32), SessionSecret: make([]byte, 32),
		APNSTeamID: "7PTN7E8EDS", APNSKeyID: "ABC123DEFG", APNSPrivateKeyFile: "/unused/test/key",
		APNSBundleID: "com.mcchord.ringring", APNSEnvironment: "production",
	}
	app, err := New(cfg, database, cipher, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	app.now = func() time.Time { return now }
	return app, database, "sip_mobile", password
}
