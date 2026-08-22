package webapp

import (
	"context"
	"errors"
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
	"github.com/amcchord/ringring/internal/openaiadmin"
	"github.com/amcchord/ringring/internal/secure"
	"github.com/amcchord/ringring/internal/store"
	"github.com/amcchord/ringring/internal/telephony"
	"github.com/amcchord/ringring/internal/weather"
)

type fakeWeatherGeocoder struct {
	query string
}

type fakeOpenAIProjects struct {
	archiveAttempts int
	archived        []string
	archiveErr      error
}

type fakeContactPresence struct {
	statuses map[string]telephony.ContactState
	err      error
}

func (f fakeContactPresence) ContactStatuses(context.Context) (map[string]telephony.ContactState, error) {
	return f.statuses, f.err
}

type countingContactPresence struct {
	calls    int
	statuses map[string]telephony.ContactState
}

func (f *countingContactPresence) ContactStatuses(context.Context) (map[string]telephony.ContactState, error) {
	f.calls++
	return f.statuses, nil
}

func (*fakeOpenAIProjects) Provision(context.Context, string, string) (openaiadmin.ProvisionedProject, error) {
	return openaiadmin.ProvisionedProject{}, errors.New("not used")
}

func (f *fakeOpenAIProjects) ArchiveProject(_ context.Context, projectID string) error {
	f.archiveAttempts++
	if f.archiveErr != nil {
		return f.archiveErr
	}
	f.archived = append(f.archived, projectID)
	return nil
}

func (f *fakeWeatherGeocoder) Geocode(_ context.Context, query string) (weather.Location, error) {
	f.query = query
	return weather.Location{Query: query, Label: "Portland, Maine", Latitude: 43.66, Longitude: -70.25}, nil
}

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
	homeBody := readBody(t, home)
	if home.StatusCode != http.StatusOK || !strings.Contains(homeBody, "Pick up") || !strings.Contains(homeBody, "test my phone") {
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
	if err := database.UpdatePartyOpenAI(t.Context(), partyID, "proj_test", "svc_test", "encrypted-key", "ready"); err != nil {
		t.Fatal(err)
	}
	unconfirmedAI := postForm(t, client, server.URL+"/parties/"+partyID+"/services", url.Values{
		"csrf": {csrf}, "ai_enabled": {"1"},
	})
	if unconfirmedAI.StatusCode != http.StatusBadRequest || !strings.Contains(readBody(t, unconfirmedAI), "adult host") {
		t.Fatal("AI line was enabled without the adult safety confirmation")
	}
	geocoder := &fakeWeatherGeocoder{}
	app.weather = geocoder
	servicePage := postForm(t, client, server.URL+"/parties/"+partyID+"/services", url.Values{
		"csrf": {csrf}, "time_enabled": {"1"}, "weather_enabled": {"1"},
		"weather_query": {" Portland,   Maine "}, "radio_enabled": {"1"},
		"ai_enabled": {"1"}, "ai_safety_confirmed": {"1"},
	})
	serviceBody := readBody(t, servicePage)
	if servicePage.StatusCode != http.StatusOK || !strings.Contains(serviceBody, "Using Portland, Maine") || geocoder.query != "Portland, Maine" {
		t.Fatalf("service settings were not saved: status=%d query=%q", servicePage.StatusCode, geocoder.query)
	}
	services, err := database.PartyServices(t.Context(), partyID)
	if err != nil {
		t.Fatal(err)
	}
	if !services.TimeEnabled || !services.WeatherEnabled || !services.RadioEnabled || !services.AIEnabled || services.WeatherLatitude != 43.66 {
		t.Fatalf("unexpected service settings: %#v", services)
	}
	routingServices, err := database.RoutingServices(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(routingServices) != 1 || !routingServices[0].WeatherEnabled || !routingServices[0].AIEnabled {
		t.Fatalf("AI-powered services were not routable: %#v", routingServices)
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
	if setup.StatusCode != http.StatusOK || !strings.Contains(setupBody, "You are extension 101") || !strings.Contains(setupBody, "sip.example.test") || !strings.Contains(setupBody, "Test both directions") || !strings.Contains(setupBody, "dial <strong>*10</strong>") {
		t.Fatalf("setup response was not successful: status=%d", setup.StatusCode)
	}
	oldUsername := firstMatch(t, setupBody, `(rrd_[A-Za-z0-9_-]+)`)
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

	hostParty := get(t, client, server.URL+"/parties/"+partyID)
	hostPartyBody := readBody(t, hostParty)
	if !strings.Contains(hostPartyBody, "Phone echo test") || !strings.Contains(hostPartyBody, "Always ready") {
		t.Fatal("party page omitted the always-available echo test")
	}
	if !strings.Contains(hostPartyBody, "status unavailable") || !strings.Contains(hostPartyBody, "Live phone status is temporarily unavailable") {
		t.Fatal("party page did not fail safely when live presence was unconfigured")
	}
	presenceCounter := &countingContactPresence{statuses: map[string]telephony.ContactState{oldUsername: telephony.ContactReachable}}
	app.presence = presenceCounter
	outsiderJar, _ := cookiejar.New(nil)
	outsiderClient := &http.Client{Transport: server.Client().Transport, Jar: outsiderJar}
	outsiderLogin := postForm(t, outsiderClient, server.URL+"/auth/dev", url.Values{"email": {"another-host@example.test"}})
	_ = readBody(t, outsiderLogin)
	outsiderParty := get(t, outsiderClient, server.URL+"/parties/"+partyID)
	if outsiderParty.StatusCode != http.StatusNotFound || presenceCounter.calls != 0 {
		t.Fatalf("another host reached party presence: status=%d calls=%d", outsiderParty.StatusCode, presenceCounter.calls)
	}
	_ = readBody(t, outsiderParty)
	hostParty = get(t, client, server.URL+"/parties/"+partyID)
	hostPartyBody = readBody(t, hostParty)
	onlineRendered := strings.Contains(hostPartyBody, "presence-online\">online")
	memberLabelRendered := strings.Contains(hostPartyBody, `aria-label="At least one phone is online"`)
	unavailableRendered := strings.Contains(hostPartyBody, "Live phone status is temporarily unavailable")
	if presenceCounter.calls != 1 || !onlineRendered || !memberLabelRendered || unavailableRendered {
		t.Fatalf("party live presence calls=%d online=%t member_label=%t unavailable=%t", presenceCounter.calls, onlineRendered, memberLabelRendered, unavailableRendered)
	}
	deviceID := firstMatch(t, hostPartyBody, `/devices/([^/]+)/rotate`)
	rotated := postForm(t, client, server.URL+"/parties/"+partyID+"/devices/"+deviceID+"/rotate", url.Values{"csrf": {csrf}})
	rotatedBody := readBody(t, rotated)
	if rotated.StatusCode != http.StatusOK || !strings.Contains(rotatedBody, "Fresh phone settings") || !strings.Contains(rotatedBody, "old username and password no longer work") {
		t.Fatalf("rotation setup response was not successful: status=%d", rotated.StatusCode)
	}
	freshUsername := firstMatch(t, rotatedBody, `(rrd_[A-Za-z0-9_-]+)`)
	if freshUsername == oldUsername {
		t.Fatal("rotation did not replace the SIP username")
	}
	if again := get(t, client, rotated.Request.URL.String()); again.StatusCode != http.StatusGone {
		t.Fatalf("setup reveal could be read twice: status=%d", again.StatusCode)
	} else {
		_ = readBody(t, again)
	}

	app.presence = fakeContactPresence{statuses: map[string]telephony.ContactState{freshUsername: telephony.ContactUnreachable}}
	hostParty = get(t, client, server.URL+"/parties/"+partyID)
	hostPartyBody = readBody(t, hostParty)
	if !strings.Contains(hostPartyBody, "presence-trouble\">not reachable") {
		t.Fatal("party page did not distinguish an unreachable registered phone")
	}
	app.presence = fakeContactPresence{statuses: map[string]telephony.ContactState{freshUsername: telephony.ContactUnknown}}
	hostParty = get(t, client, server.URL+"/parties/"+partyID)
	if !strings.Contains(readBody(t, hostParty), "presence-checking\">checking") {
		t.Fatal("party page did not distinguish a newly registered phone being checked")
	}
	app.presence = fakeContactPresence{statuses: map[string]telephony.ContactState{}}
	hostParty = get(t, client, server.URL+"/parties/"+partyID)
	if !strings.Contains(readBody(t, hostParty), "presence-waiting\">not registered") {
		t.Fatal("party page did not identify an unregistered phone")
	}
	app.presence = fakeContactPresence{err: errors.New("temporary AMI failure")}
	hostParty = get(t, client, server.URL+"/parties/"+partyID)
	if hostParty.StatusCode != http.StatusOK || !strings.Contains(readBody(t, hostParty), "Live phone status is temporarily unavailable") {
		t.Fatal("AMI failure blocked the authenticated party page")
	}
	app.presence = fakeContactPresence{statuses: map[string]telephony.ContactState{freshUsername: telephony.ContactReachable}}
	hostParty = get(t, client, server.URL+"/parties/"+partyID)
	revokeDeviceID := firstMatch(t, readBody(t, hostParty), `/devices/([^/]+)/revoke`)
	revoked := postForm(t, client, server.URL+"/parties/"+partyID+"/devices/"+revokeDeviceID+"/revoke", url.Values{"csrf": {csrf}})
	revokedBody := readBody(t, revoked)
	if revoked.StatusCode != http.StatusOK || !strings.Contains(revokedBody, "presence-off\">disconnected") || strings.Contains(revokedBody, "presence-online\">online") {
		t.Fatalf("revocation response was not successful: status=%d", revoked.StatusCode)
	}
	routing, err = database.RoutingDevices(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(routing) != 0 {
		t.Fatalf("revoked device remained routable: %#v", routing)
	}

	hostParty = get(t, client, server.URL+"/parties/"+partyID)
	memberDeletePath := firstMatch(t, readBody(t, hostParty), `(/parties/[^/]+/members/[^/]+/delete)`)
	memberConfirmation := get(t, client, server.URL+memberDeletePath)
	memberConfirmationBody := readBody(t, memberConfirmation)
	if memberConfirmation.StatusCode != http.StatusOK || !strings.Contains(memberConfirmationBody, "Delete member and phones") || !strings.Contains(memberConfirmationBody, "extension <strong>101</strong>") {
		t.Fatalf("member confirmation status=%d", memberConfirmation.StatusCode)
	}
	wrongMember := postForm(t, client, server.URL+memberDeletePath, url.Values{"csrf": {csrf}, "confirmation": {"102"}})
	if wrongMember.StatusCode != http.StatusBadRequest || !strings.Contains(readBody(t, wrongMember), "extension did not match") {
		t.Fatal("member deletion accepted the wrong extension")
	}
	deletedMember := postForm(t, client, server.URL+memberDeletePath, url.Values{"csrf": {csrf}, "confirmation": {"101"}})
	if deletedMember.StatusCode != http.StatusOK || !strings.Contains(readBody(t, deletedMember), "member and every phone credential") {
		t.Fatalf("member deletion status=%d", deletedMember.StatusCode)
	}
	if members, err := database.ListMembers(t.Context(), partyID); err != nil || len(members) != 0 {
		t.Fatalf("member deletion left records: %#v, %v", members, err)
	}

	projects := &fakeOpenAIProjects{archiveErr: errors.New("temporary admin failure")}
	app.openAI = projects
	partyDeletePath := "/parties/" + partyID + "/delete"
	partyConfirmation := get(t, client, server.URL+partyDeletePath)
	if partyConfirmation.StatusCode != http.StatusOK || !strings.Contains(readBody(t, partyConfirmation), "archive this party's OpenAI project") {
		t.Fatalf("party confirmation status=%d", partyConfirmation.StatusCode)
	}
	failedParty := postForm(t, client, server.URL+partyDeletePath, url.Values{"csrf": {csrf}, "confirmation": {"Cousins Club"}})
	if failedParty.StatusCode != http.StatusBadGateway || !strings.Contains(readBody(t, failedParty), "all local party data was kept") {
		t.Fatal("party deletion did not fail closed when project archival failed")
	}
	if existing := get(t, client, server.URL+"/parties/"+partyID); existing.StatusCode != http.StatusOK {
		t.Fatalf("archive failure removed the party: status=%d", existing.StatusCode)
	} else {
		_ = readBody(t, existing)
	}
	projects.archiveErr = nil
	wrongParty := postForm(t, client, server.URL+partyDeletePath, url.Values{"csrf": {csrf}, "confirmation": {"Cousins"}})
	if wrongParty.StatusCode != http.StatusBadRequest || !strings.Contains(readBody(t, wrongParty), "party name did not match") {
		t.Fatal("party deletion accepted the wrong party name")
	}
	deletedParty := postForm(t, client, server.URL+partyDeletePath, url.Values{"csrf": {csrf}, "confirmation": {"Cousins Club"}})
	if deletedParty.StatusCode != http.StatusOK || deletedParty.Request.URL.Path != "/app" || !strings.Contains(readBody(t, deletedParty), "party, its invites, members, and phone credentials") {
		t.Fatalf("party deletion ended at %s with %d", deletedParty.Request.URL, deletedParty.StatusCode)
	}
	if projects.archiveAttempts != 2 || len(projects.archived) != 1 || projects.archived[0] != "proj_test" {
		t.Fatalf("unexpected project archival: attempts=%d archived=%#v", projects.archiveAttempts, projects.archived)
	}

	accountConfirmation := get(t, client, server.URL+"/account/delete")
	if accountConfirmation.StatusCode != http.StatusOK || !strings.Contains(readBody(t, accountConfirmation), "Delete my host account") {
		t.Fatalf("account confirmation status=%d", accountConfirmation.StatusCode)
	}
	wrongAccount := postForm(t, client, server.URL+"/account/delete", url.Values{"csrf": {csrf}, "confirmation": {"delete"}})
	if wrongAccount.StatusCode != http.StatusBadRequest || !strings.Contains(readBody(t, wrongAccount), "confirmation did not match") {
		t.Fatal("account deletion accepted a lowercase confirmation")
	}
	deletedAccount := postForm(t, client, server.URL+"/account/delete", url.Values{"csrf": {csrf}, "confirmation": {"DELETE"}})
	if deletedAccount.StatusCode != http.StatusOK || deletedAccount.Request.URL.Path != "/" || !strings.Contains(readBody(t, deletedAccount), "account and its sign-in data were deleted") {
		t.Fatalf("account deletion ended at %s with %d", deletedAccount.Request.URL, deletedAccount.StatusCode)
	}
	if afterDelete := get(t, client, server.URL+"/app"); afterDelete.Request.URL.Path != "/login" {
		t.Fatalf("deleted account retained its session: ended at %s", afterDelete.Request.URL)
	} else {
		_ = readBody(t, afterDelete)
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

func TestNativeSignupLoginAndOfflineRecovery(t *testing.T) {
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
		Environment: "development", BaseURL: "http://" + server.Listener.Addr().String(),
		MasterKey: make([]byte, 32), SessionSecret: make([]byte, 32),
		HostSignupCode: "family-door-code", InviteTTL: time.Hour,
	}
	app, err := New(cfg, database, cipher, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	fixedNow := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	app.now = func() time.Time { return fixedNow }
	server.Config.Handler = app
	server.Start()
	defer server.Close()

	jar, _ := cookiejar.New(nil)
	client := server.Client()
	client.Jar = jar
	signupPage := get(t, client, server.URL+"/signup")
	signupBody := readBody(t, signupPage)
	if !strings.Contains(signupBody, "No email runaround") || !strings.Contains(signupBody, "Family access code") {
		t.Fatal("native signup form was not rendered")
	}
	signupCSRF := firstMatch(t, signupBody, `name="csrf" value="([^"]+)"`)
	created := postForm(t, client, server.URL+"/signup", url.Values{
		"csrf": {signupCSRF}, "name": {"Austin Host"}, "username": {"Austin.Rings"},
		"signup_code": {"family-door-code"}, "password": {"a colorful phone party"},
		"password_confirm": {"a colorful phone party"},
	})
	createdBody := readBody(t, created)
	if created.StatusCode != http.StatusOK || created.Request.URL.Path != "/account/recovery-codes" || !strings.Contains(createdBody, "Save your recovery codes") {
		t.Fatalf("signup ended at %s with %d", created.Request.URL, created.StatusCode)
	}
	recoveryCode := firstMatch(t, createdBody, `<code>([A-Z2-7-]+)</code>`)
	if secondReveal := get(t, client, server.URL+"/account/recovery-codes"); secondReveal.StatusCode != http.StatusGone {
		t.Fatalf("recovery codes could be revealed twice: status=%d", secondReveal.StatusCode)
	} else {
		_ = readBody(t, secondReveal)
	}
	if dashboard := get(t, client, server.URL+"/app"); dashboard.StatusCode != http.StatusOK {
		t.Fatalf("new account was not signed in: status=%d", dashboard.StatusCode)
	} else {
		_ = readBody(t, dashboard)
	}

	secondJar, _ := cookiejar.New(nil)
	secondClient := server.Client()
	secondClient.Jar = secondJar
	duplicateForm := get(t, secondClient, server.URL+"/signup")
	duplicateCSRF := firstMatch(t, readBody(t, duplicateForm), `name="csrf" value="([^"]+)"`)
	duplicate := postForm(t, secondClient, server.URL+"/signup", url.Values{
		"csrf": {duplicateCSRF}, "name": {"Someone Else"}, "username": {"austin.rings"},
		"signup_code": {"family-door-code"}, "password": {"another colorful phrase"},
		"password_confirm": {"another colorful phrase"},
	})
	if duplicate.StatusCode != http.StatusConflict || !strings.Contains(readBody(t, duplicate), "not available") {
		t.Fatalf("duplicate username status=%d", duplicate.StatusCode)
	}

	recoverPage := get(t, client, server.URL+"/recover")
	recoverCSRF := firstMatch(t, readBody(t, recoverPage), `name="csrf" value="([^"]+)"`)
	recovered := postForm(t, client, server.URL+"/recover", url.Values{
		"csrf": {recoverCSRF}, "username": {"AUSTIN.RINGS"}, "recovery_code": {strings.ToLower(recoveryCode)},
		"password": {"the fresh colorful phrase"}, "password_confirm": {"the fresh colorful phrase"},
	})
	recoveredBody := readBody(t, recovered)
	if recovered.StatusCode != http.StatusOK || recovered.Request.URL.Path != "/account/recovery-codes" || !strings.Contains(recoveredBody, "All old sessions") {
		t.Fatalf("recovery ended at %s with %d", recovered.Request.URL, recovered.StatusCode)
	}
	if appPage := get(t, client, server.URL+"/app"); appPage.Request.URL.Path != "/login" {
		t.Fatalf("recovery did not invalidate the current session: ended at %s", appPage.Request.URL)
	} else {
		_ = readBody(t, appPage)
	}

	loginPage := get(t, client, server.URL+"/login")
	loginCSRF := firstMatch(t, readBody(t, loginPage), `name="csrf" value="([^"]+)"`)
	oldLogin := postForm(t, client, server.URL+"/login", url.Values{
		"csrf": {loginCSRF}, "username": {"austin.rings"}, "password": {"a colorful phone party"},
	})
	if oldLogin.StatusCode != http.StatusUnauthorized || !strings.Contains(readBody(t, oldLogin), "did not match") {
		t.Fatalf("old password remained valid: status=%d", oldLogin.StatusCode)
	}
	loginPage = get(t, client, server.URL+"/login")
	loginCSRF = firstMatch(t, readBody(t, loginPage), `name="csrf" value="([^"]+)"`)
	newLogin := postForm(t, client, server.URL+"/login", url.Values{
		"csrf": {loginCSRF}, "username": {"austin.rings"}, "password": {"the fresh colorful phrase"},
	})
	if newLogin.StatusCode != http.StatusOK || newLogin.Request.URL.Path != "/app" {
		t.Fatalf("new password did not sign in: ended at %s with %d", newLogin.Request.URL, newLogin.StatusCode)
	}
}

func TestProductionSignupNeedsFamilyCode(t *testing.T) {
	if (config.Config{Environment: "production"}).HostSignupEnabled() {
		t.Fatal("production signup must stay closed without a family access code")
	}
	if !(config.Config{Environment: "production", HostSignupCode: "secret"}).HostSignupEnabled() {
		t.Fatal("family access code must enable production signup")
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
