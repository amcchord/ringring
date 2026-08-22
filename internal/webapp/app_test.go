package webapp

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/amcchord/ringring/internal/config"
	"github.com/amcchord/ringring/internal/secure"
	"github.com/amcchord/ringring/internal/store"
)

func TestPartyInvitationAndClaimFlow(t *testing.T) {
	database, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	cipher, err := secure.NewCipher(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}

	server := httptest.NewUnstartedServer(nil)
	cfg := config.Config{
		Environment: "development", HTTPAddr: ":0",
		BaseURL: "http://" + server.Listener.Addr().String(), DatabasePath: ":memory:",
		MasterKey: make([]byte, 32), SessionSecret: make([]byte, 32),
		SIPPublicHost: "sip.example.test", InviteTTL: time.Hour, DevAuth: true,
	}
	app, err := New(cfg, database, cipher, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	fixedNow := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	app.now = func() time.Time { return fixedNow }
	server.Config.Handler = app
	server.Start()
	defer server.Close()

	jar, _ := cookiejar.New(nil)
	client := server.Client()
	client.Jar = jar

	home := get(t, client, server.URL+"/")
	if home.StatusCode != http.StatusOK || !strings.Contains(readBody(t, home), "Pick up") {
		t.Fatal("landing page did not render")
	}
	if got := home.Header.Get("Content-Security-Policy"); !strings.Contains(got, "frame-ancestors 'none'") {
		t.Fatalf("missing security policy: %q", got)
	}

	login := postForm(t, client, server.URL+"/auth/dev", url.Values{"email": {"host@example.test"}})
	if login.Request.URL.Path != "/app" || login.StatusCode != http.StatusOK {
		t.Fatalf("login ended at %s with %d", login.Request.URL, login.StatusCode)
	}
	dashboard := readBody(t, login)
	csrf := firstMatch(t, dashboard, `name="csrf" value="([^"]+)"`)

	created := postForm(t, client, server.URL+"/parties", url.Values{"csrf": {csrf}, "name": {"Cousins Club"}})
	if created.StatusCode != http.StatusOK || !strings.HasPrefix(created.Request.URL.Path, "/parties/") {
		t.Fatalf("party creation ended at %s with %d", created.Request.URL, created.StatusCode)
	}
	partyPage := readBody(t, created)
	partyID := strings.TrimPrefix(created.Request.URL.Path, "/parties/")
	if !strings.Contains(partyPage, "Cousins Club") || !strings.Contains(partyPage, "not-configured") {
		t.Fatal("party page missing expected details")
	}

	invite := postForm(t, client, server.URL+"/parties/"+partyID+"/invites", url.Values{"csrf": {csrf}})
	if invite.StatusCode != http.StatusOK {
		t.Fatalf("create invite status = %d", invite.StatusCode)
	}
	inviteURL := firstMatch(t, readBody(t, invite), `value="(http://[^"]+/join/[^"]+)"`)

	join := get(t, client, inviteURL)
	if join.StatusCode != http.StatusOK {
		t.Fatalf("join status = %d", join.StatusCode)
	}
	joinCSRF := firstMatch(t, readBody(t, join), `name="csrf" value="([^"]+)"`)
	setup := postForm(t, client, inviteURL, url.Values{
		"csrf": {joinCSRF}, "display_name": {"Blue phone"}, "extension": {"101"}, "device_label": {"ATA"},
	})
	setupBody := readBody(t, setup)
	if setup.StatusCode != http.StatusOK || !strings.Contains(setupBody, "You are extension 101") || !strings.Contains(setupBody, "sip.example.test") {
		t.Fatalf("setup response was not successful: status=%d", setup.StatusCode)
	}
	if setup.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("setup credentials must not be cached")
	}

	used := get(t, client, inviteURL)
	if used.StatusCode != http.StatusGone || !strings.Contains(readBody(t, used), "already rang") {
		t.Fatalf("used invite status = %d", used.StatusCode)
	}
	routing, err := database.RoutingDevices(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(routing) != 1 || routing[0].Extension != "101" || strings.Contains(routing[0].SIPSecretCiphertext, "Blue phone") {
		t.Fatalf("unexpected routing state: %#v", routing)
	}
}

func TestProductionRejectsNullOrigin(t *testing.T) {
	app := &App{cfg: config.Config{Environment: "production", BaseURL: "https://ringring.live"}}
	req := httptest.NewRequest(http.MethodPost, "https://ringring.live/parties", nil)
	req.Header.Set("Origin", "null")
	if app.sameOrigin(req) {
		t.Fatal("production must not trust a null origin")
	}
}

func get(t *testing.T, client *http.Client, target string) *http.Response {
	t.Helper()
	response, err := client.Get(target)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func postForm(t *testing.T, client *http.Client, target string, values url.Values) *http.Response {
	t.Helper()
	response, err := client.PostForm(target, values)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func readBody(t *testing.T, response *http.Response) string {
	t.Helper()
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func firstMatch(t *testing.T, value, pattern string) string {
	t.Helper()
	match := regexp.MustCompile(pattern).FindStringSubmatch(value)
	if len(match) != 2 {
		t.Fatalf("pattern %q not found", pattern)
	}
	return match[1]
}
