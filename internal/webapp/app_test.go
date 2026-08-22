package webapp

import (
	"bytes"
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
	"github.com/amcchord/ringring/internal/model"
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
	createAttempts  int
	createdKey      openaiadmin.ServiceAccountAPIKey
	createErr       error
	listAttempts    int
	keyIDs          []string
	listErr         error
	deleteAttempts  int
	deletedKeys     []string
	deleteFailures  int
	spendAttempts   int
	spendAmounts    []int
	spendFailures   int
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

type fakeDeviceRinger struct {
	statuses  map[string]telephony.ContactState
	statusErr error
	ringErr   error
	calls     int
	username  string
	extension string
}

func (f *fakeDeviceRinger) ContactStatuses(context.Context) (map[string]telephony.ContactState, error) {
	return f.statuses, f.statusErr
}

func (f *fakeDeviceRinger) RingDevice(_ context.Context, username, extension string) error {
	f.calls++
	f.username = username
	f.extension = extension
	return f.ringErr
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

func (f *fakeOpenAIProjects) CreateServiceAccountAPIKey(context.Context, string, string) (openaiadmin.ServiceAccountAPIKey, error) {
	f.createAttempts++
	if f.createErr != nil {
		return openaiadmin.ServiceAccountAPIKey{}, f.createErr
	}
	return f.createdKey, nil
}

func (f *fakeOpenAIProjects) ServiceAccountAPIKeyIDs(context.Context, string, string) ([]string, error) {
	f.listAttempts++
	if f.listErr != nil {
		return nil, f.listErr
	}
	return append([]string(nil), f.keyIDs...), nil
}

func (f *fakeOpenAIProjects) DeleteProjectAPIKey(_ context.Context, _ string, keyID string) error {
	f.deleteAttempts++
	if f.deleteFailures > 0 {
		f.deleteFailures--
		return errors.New("temporary key deletion failure")
	}
	f.deletedKeys = append(f.deletedKeys, keyID)
	for index, candidate := range f.keyIDs {
		if candidate == keyID {
			f.keyIDs = append(f.keyIDs[:index], f.keyIDs[index+1:]...)
			break
		}
	}
	return nil
}

func (f *fakeOpenAIProjects) UpdateProjectSpendLimit(_ context.Context, _ string, cents int) (openaiadmin.SpendLimit, error) {
	f.spendAttempts++
	f.spendAmounts = append(f.spendAmounts, cents)
	if f.spendFailures > 0 {
		f.spendFailures--
		return openaiadmin.SpendLimit{}, errors.New("temporary spend limit failure")
	}
	return openaiadmin.SpendLimit{ThresholdAmount: cents, Currency: "USD", Interval: "month", EnforcementStatus: "enforcing"}, nil
}

func (f *fakeWeatherGeocoder) Geocode(_ context.Context, query string) (weather.Location, error) {
	f.query = query
	return weather.Location{Query: query, Label: "Portland, Maine", Latitude: 43.66, Longitude: -70.25}, nil
}

func TestParseDollarsUsesExactCents(t *testing.T) {
	tests := []struct {
		input string
		want  int
		ok    bool
	}{
		{"0.01", 1, true}, {"7.2", 720, true}, {"10", 1000, true}, {" 10.00 ", 1000, true},
		{"", 0, false}, {".50", 0, false}, {"1.001", 0, false}, {"-1", 0, false}, {"1e1", 0, false}, {"$10", 0, false},
	}
	for _, test := range tests {
		got, err := parseDollars(test.input)
		if test.ok && (err != nil || got != test.want) {
			t.Errorf("parseDollars(%q) = %d, %v; want %d", test.input, got, err, test.want)
		}
		if !test.ok && err == nil {
			t.Errorf("parseDollars(%q) unexpectedly accepted %d", test.input, got)
		}
	}
}

func TestPrivateCallDirectoryIncludesOnlyMembersWithAnActivePhone(t *testing.T) {
	revokedAt := time.Date(2026, 8, 22, 1, 0, 0, 0, time.UTC)
	directory := privateCallDirectory([]model.Member{
		{DisplayName: "Blue phone", Extension: "101", Devices: []model.Device{{ID: "dev_blue"}}},
		{DisplayName: "Quiet phone", Extension: "102", Devices: []model.Device{{ID: "dev_quiet", RevokedAt: &revokedAt}}},
		{DisplayName: "No phone", Extension: "103"},
	})
	if len(directory) != 1 || directory[0].DisplayName != "Blue phone" || directory[0].Extension != "101" {
		t.Fatalf("private call directory = %#v", directory)
	}
}

func TestFirstCallLinesMatchCurrentlyDialableServices(t *testing.T) {
	party := model.Party{OpenAIStatus: "ready", OpenAISpendLimitStatus: "ready"}
	services := model.PartyServices{TimeEnabled: true, WeatherEnabled: true, RadioEnabled: true, AIEnabled: true}
	numbers := func(lines []firstCallLine) string {
		var values []string
		for _, line := range lines {
			values = append(values, line.Number)
		}
		return strings.Join(values, ",")
	}
	if got := numbers(availableFirstCallLines(party, services, false)); got != "*10,*15,*11,*12,*13" {
		t.Fatalf("closed-gate first-call lines = %q", got)
	}
	if got := numbers(availableFirstCallLines(party, services, true)); got != "*10,*15,*11,*12,*13,*14" {
		t.Fatalf("approved first-call lines = %q", got)
	}
	party.OpenAISpendLimitStatus = "update-error"
	if got := numbers(availableFirstCallLines(party, services, true)); got != "*10,*15,*11,*13" {
		t.Fatalf("spend-paused first-call lines = %q", got)
	}
}

func TestSuccessfulClaimGetsPrivateFirstCallCard(t *testing.T) {
	database, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	cipher, err := secure.NewCipher(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}

	ctx := t.Context()
	now := time.Date(2026, 8, 22, 13, 0, 0, 0, time.UTC)
	host, err := database.UpsertGoogleUser(ctx, store.GoogleProfile{Subject: "directory-host", Email: "host@example.test", Name: "Host"}, now, "usr_directory_host")
	if err != nil {
		t.Fatal(err)
	}
	party, err := database.CreateParty(ctx, store.NewParty{ID: "pty_directory", Name: "Cousins Club", Slug: "cousins-club", HostUserID: host.ID, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	partyKey, err := cipher.Encrypt("sk-test-party", []byte(party.ID))
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdatePartyOpenAI(ctx, party.ID, "proj_directory", "svc_directory", "key_directory", partyKey, "ready", 500); err != nil {
		t.Fatal(err)
	}
	if _, err := database.UpdatePartyServices(ctx, party.ID, host.ID, store.ServiceSettingsInput{
		TimeEnabled: true, WeatherEnabled: true, WeatherQuery: "Portland, Maine", WeatherLabel: "Portland, Maine",
		RadioEnabled: true, RadioStation: "drone-zone", AIEnabled: true, AIChildSafetyApproved: true, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	seedMember := func(invitationID, token, memberID, displayName, extension, deviceID, deviceLabel, username string, revoke bool) {
		t.Helper()
		if err := database.CreateInvitation(ctx, store.NewInvitation{
			ID: invitationID, PartyID: party.ID, CreatedByUserID: host.ID, TokenHash: secure.Hash(token), ExpiresAt: now.Add(time.Hour), CreatedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
		ciphertext, err := cipher.Encrypt("seed-secret-"+extension, []byte(deviceID))
		if err != nil {
			t.Fatal(err)
		}
		_, _, device, err := database.ClaimInvitation(ctx, store.NewClaim{
			TokenHash: secure.Hash(token), MemberID: memberID, DisplayName: displayName, Extension: extension,
			DeviceID: deviceID, DeviceLabel: deviceLabel, SIPUsername: username, SIPSecretCiphertext: ciphertext,
			Provisioning: store.NewProvisioningToken{TokenHash: secure.Hash("provision-" + extension), ExpiresAt: now.Add(time.Hour), CreatedAt: now}, Now: now,
		})
		if err != nil {
			t.Fatal(err)
		}
		if revoke {
			if err := database.RevokeDevice(ctx, party.ID, host.ID, device.ID, now); err != nil {
				t.Fatal(err)
			}
		}
	}
	seedMember("inv_blue", strings.Repeat("b", 43), "mem_blue", "Blue phone", "101", "dev_blue", "Blue ATA", "rrd_blue_seed", false)
	seedMember("inv_quiet", strings.Repeat("q", 43), "mem_quiet", "Quiet phone", "102", "dev_quiet", "Quiet ATA", "rrd_quiet_seed", true)

	joinToken := strings.Repeat("j", 43)
	if err := database.CreateInvitation(ctx, store.NewInvitation{
		ID: "inv_join_directory", PartyID: party.ID, CreatedByUserID: host.ID, TokenHash: secure.Hash(joinToken), ExpiresAt: now.Add(time.Hour), CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewUnstartedServer(nil)
	cfg := config.Config{
		Environment: "development", HTTPAddr: ":0", BaseURL: "http://" + server.Listener.Addr().String(),
		DatabasePath: ":memory:", MasterKey: make([]byte, 32), SessionSecret: make([]byte, 32),
		SIPPublicHost: "sip.example.test", InviteTTL: time.Hour,
	}
	app, err := New(cfg, database, cipher, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	app.now = func() time.Time { return now }
	server.Config.Handler = app
	server.Start()
	defer server.Close()

	jar, _ := cookiejar.New(nil)
	client := server.Client()
	client.Jar = jar
	joinURL := server.URL + "/join/" + joinToken
	join := get(t, client, joinURL)
	joinBody := readBody(t, join)
	if join.StatusCode != http.StatusOK || strings.Contains(joinBody, "Blue phone") || strings.Contains(joinBody, "Quiet phone") || strings.Contains(joinBody, "first-call-card") {
		t.Fatalf("unclaimed invitation exposed the private directory: status=%d", join.StatusCode)
	}
	csrf := firstMatch(t, joinBody, `name="csrf" value="([^"]+)"`)
	setup := postForm(t, client, joinURL, url.Values{
		"csrf": {csrf}, "display_name": {"Green phone"}, "extension": {"103"}, "device_label": {"Desk phone"},
	})
	setupBody := readBody(t, setup)
	if setup.StatusCode != http.StatusOK || setup.Header.Get("Cache-Control") != "no-store" ||
		!strings.Contains(setupBody, `class="first-call-card"`) || !strings.Contains(setupBody, "Blue phone") || !strings.Contains(setupBody, ">101<") ||
		strings.Contains(setupBody, "Quiet phone") || strings.Contains(setupBody, "rrd_blue_seed") || strings.Contains(setupBody, "Blue ATA") {
		t.Fatalf("successful claim directory was incomplete or over-broad: status=%d", setup.StatusCode)
	}
	for _, number := range []string{"*10", "*15", "*11", "*12", "*13"} {
		if !strings.Contains(setupBody, ">"+number+"<") {
			t.Errorf("successful claim omitted dialable first-call line %s", number)
		}
	}
	if strings.Contains(setupBody, ">*14<") {
		t.Fatal("successful claim advertised the AI line while the operator child-safety gate was closed")
	}
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
	if got := home.Header.Get("Content-Security-Policy"); !strings.Contains(got, "frame-ancestors 'none'") || !strings.Contains(got, "script-src '"+setupScriptSHA256+"'") || strings.Contains(got, "script-src 'self'") || strings.Contains(got, "unsafe-inline") || strings.Contains(got, "unsafe-eval") {
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
	if !strings.Contains(partyPage, "Cousins Club") || !strings.Contains(partyPage, "AI voice: unavailable") {
		t.Fatal("party page missing expected details")
	}
	for _, stationID := range []string{"groove-salad", "drone-zone", "deep-space-one"} {
		if !strings.Contains(partyPage, `value="`+stationID+`"`) {
			t.Fatalf("party page missing catalog station %q", stationID)
		}
	}
	initialPartyKey, err := cipher.Encrypt("sk-old-party", []byte(partyID))
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdatePartyOpenAI(t.Context(), partyID, "proj_test", "svc_test", "key_old", initialPartyKey, "ready", 1000); err != nil {
		t.Fatal(err)
	}
	lockedAI := postForm(t, client, server.URL+"/parties/"+partyID+"/services", url.Values{
		"csrf": {csrf}, "ai_enabled": {"1"}, "ai_safety_confirmed": {"1"},
	})
	if lockedAI.StatusCode != http.StatusConflict || !strings.Contains(readBody(t, lockedAI), "server operator") {
		t.Fatal("AI line was not held behind the operator child-safety gate")
	}
	lockedPartyPage := get(t, client, server.URL+"/parties/"+partyID)
	lockedPartyBody := readBody(t, lockedPartyPage)
	if !strings.Contains(lockedPartyBody, "Locked until the server operator") || !strings.Contains(lockedPartyBody, `name="ai_enabled" value="1"  disabled`) {
		t.Fatal("party page did not explain or disable the closed AI conversation gate")
	}
	app.cfg.AIChildSafetyApproved = true
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
		"radio_station": {"drone-zone"},
		"ai_enabled":    {"1"}, "ai_safety_confirmed": {"1"},
	})
	serviceBody := readBody(t, servicePage)
	if servicePage.StatusCode != http.StatusOK || !strings.Contains(serviceBody, "Using Portland, Maine") || geocoder.query != "Portland, Maine" {
		t.Fatalf("service settings were not saved: status=%d query=%q", servicePage.StatusCode, geocoder.query)
	}
	services, err := database.PartyServices(t.Context(), partyID)
	if err != nil {
		t.Fatal(err)
	}
	if !services.TimeEnabled || !services.WeatherEnabled || !services.RadioEnabled || services.RadioStation != "drone-zone" || !services.AIEnabled || services.WeatherLatitude != 43.66 {
		t.Fatalf("unexpected service settings: %#v", services)
	}
	if !strings.Contains(serviceBody, `value="drone-zone" selected`) {
		t.Fatal("party page did not keep the selected radio station")
	}
	invalidRadio := postForm(t, client, server.URL+"/parties/"+partyID+"/services", url.Values{
		"csrf": {csrf}, "radio_enabled": {"1"}, "radio_station": {"http://169.254.169.254/latest/meta-data"},
	})
	if invalidRadio.StatusCode != http.StatusBadRequest || !strings.Contains(readBody(t, invalidRadio), "listed radio station") {
		t.Fatal("arbitrary radio URL did not receive the safe validation error")
	}
	services, err = database.PartyServices(t.Context(), partyID)
	if err != nil || services.RadioStation != "drone-zone" || !services.RadioEnabled {
		t.Fatalf("invalid radio update changed settings: %#v error=%v", services, err)
	}
	routingServices, err := database.RoutingServices(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(routingServices) != 1 || !routingServices[0].WeatherEnabled || !routingServices[0].AIEnabled {
		t.Fatalf("AI-powered services were not routable: %#v", routingServices)
	}

	keyManager := &fakeOpenAIProjects{
		createdKey: openaiadmin.ServiceAccountAPIKey{ID: "key_fresh", Value: "sk-fresh-party"},
		keyIDs:     []string{"key_old", "key_fresh"}, deleteFailures: 1, spendFailures: 1,
	}
	app.openAI = keyManager
	spendPage := get(t, client, server.URL+"/parties/"+partyID)
	spendPageBody := readBody(t, spendPage)
	if spendPage.StatusCode != http.StatusOK || !strings.Contains(spendPageBody, "Monthly AI guardrail") || !strings.Contains(spendPageBody, "$10.00 each month") || !strings.Contains(spendPageBody, `max="10.00"`) {
		t.Fatal("party page did not show the bounded existing spend limit")
	}
	missingSpendCSRF := postForm(t, client, server.URL+"/parties/"+partyID+"/openai-spend-limit", url.Values{"spend_limit_dollars": {"7.25"}})
	if missingSpendCSRF.StatusCode != http.StatusForbidden || keyManager.spendAttempts != 0 {
		t.Fatalf("spend limit accepted a missing CSRF token: status=%d attempts=%d", missingSpendCSRF.StatusCode, keyManager.spendAttempts)
	}
	_ = readBody(t, missingSpendCSRF)
	overCapSpend := postForm(t, client, server.URL+"/parties/"+partyID+"/openai-spend-limit", url.Values{"csrf": {csrf}, "spend_limit_dollars": {"10.01"}})
	if overCapSpend.StatusCode != http.StatusBadRequest || keyManager.spendAttempts != 0 || !strings.Contains(readBody(t, overCapSpend), "$10.00") {
		t.Fatal("spend limit accepted an amount above the deployment ceiling")
	}
	failedSpend := postForm(t, client, server.URL+"/parties/"+partyID+"/openai-spend-limit", url.Values{"csrf": {csrf}, "spend_limit_dollars": {"7.25"}})
	failedSpendBody := readBody(t, failedSpend)
	if failedSpend.StatusCode != http.StatusBadGateway || !strings.Contains(failedSpendBody, "needs one more try") || strings.Contains(failedSpendBody, "update-error") || strings.Contains(failedSpendBody, "proj_test") {
		t.Fatalf("ambiguous spend update did not fail privately and safely: status=%d", failedSpend.StatusCode)
	}
	pendingParty, _, err := database.PartyVoiceSettings(t.Context(), partyID)
	if err != nil {
		t.Fatal(err)
	}
	if pendingParty.OpenAIStatus != "spend-update-error" || pendingParty.OpenAISpendLimitCents != 1000 || pendingParty.OpenAISpendPendingCents != 725 || pendingParty.OpenAISpendLimitStatus != "update-error" {
		t.Fatalf("unexpected pending spend state: %#v", pendingParty)
	}
	routingServices, err = database.RoutingServices(t.Context())
	if err != nil || len(routingServices) != 1 || routingServices[0].WeatherEnabled || routingServices[0].AIEnabled {
		t.Fatalf("AI-powered routes were not paused for uncertain spend state: %#v error=%v", routingServices, err)
	}
	blockedRotation := postForm(t, client, server.URL+"/parties/"+partyID+"/openai-key/rotate", url.Values{"csrf": {csrf}})
	if blockedRotation.StatusCode != http.StatusConflict || keyManager.createAttempts != 0 || !strings.Contains(readBody(t, blockedRotation), "Finish the spend limit") {
		t.Fatal("key rotation was not held behind pending spend reconciliation")
	}
	spendRetryPage := get(t, client, server.URL+"/parties/"+partyID)
	spendRetryBody := readBody(t, spendRetryPage)
	if spendRetryPage.StatusCode != http.StatusOK || !strings.Contains(spendRetryBody, "Finish spend limit update") || !strings.Contains(spendRetryBody, "paused for limit safety") || strings.Contains(spendRetryBody, "update-error") || strings.Contains(spendRetryBody, "proj_test") {
		t.Fatal("party page did not offer a private retry for the immutable pending amount")
	}
	finishedSpend := postForm(t, client, server.URL+"/parties/"+partyID+"/openai-spend-limit", url.Values{"csrf": {csrf}, "spend_limit_dollars": {"1.00"}})
	finishedSpendBody := readBody(t, finishedSpend)
	if finishedSpend.StatusCode != http.StatusOK || !strings.Contains(finishedSpendBody, "confirmed this party’s hard monthly spend limit") || !strings.Contains(finishedSpendBody, "$7.25 each month") {
		t.Fatalf("spend retry did not complete: status=%d", finishedSpend.StatusCode)
	}
	if keyManager.spendAttempts != 2 || len(keyManager.spendAmounts) != 2 || keyManager.spendAmounts[0] != 725 || keyManager.spendAmounts[1] != 725 {
		t.Fatalf("spend retry changed or duplicated the pending amount unsafely: %#v", keyManager.spendAmounts)
	}
	confirmedParty, _, err := database.PartyVoiceSettings(t.Context(), partyID)
	if err != nil || confirmedParty.OpenAISpendLimitCents != 725 || confirmedParty.OpenAISpendPendingCents != 0 || confirmedParty.OpenAISpendLimitStatus != "ready" {
		t.Fatalf("unexpected confirmed spend state: %#v error=%v", confirmedParty, err)
	}
	routingServices, err = database.RoutingServices(t.Context())
	if err != nil || len(routingServices) != 1 || !routingServices[0].WeatherEnabled || !routingServices[0].AIEnabled {
		t.Fatalf("AI-powered routes did not resume after spend confirmation: %#v error=%v", routingServices, err)
	}
	missingRotationCSRF := postForm(t, client, server.URL+"/parties/"+partyID+"/openai-key/rotate", url.Values{})
	if missingRotationCSRF.StatusCode != http.StatusForbidden || keyManager.createAttempts != 0 {
		t.Fatalf("key rotation accepted a missing CSRF token: status=%d creates=%d", missingRotationCSRF.StatusCode, keyManager.createAttempts)
	}
	_ = readBody(t, missingRotationCSRF)
	failedRotation := postForm(t, client, server.URL+"/parties/"+partyID+"/openai-key/rotate", url.Values{"csrf": {csrf}})
	failedRotationBody := readBody(t, failedRotation)
	if failedRotation.StatusCode != http.StatusBadGateway || !strings.Contains(failedRotationBody, "needs one more try") || strings.Contains(failedRotationBody, "sk-fresh-party") {
		t.Fatalf("partial key rotation did not fail safely: status=%d", failedRotation.StatusCode)
	}
	retryPage := get(t, client, server.URL+"/parties/"+partyID)
	retryPageBody := readBody(t, retryPage)
	if retryPage.StatusCode != http.StatusOK || !strings.Contains(retryPageBody, "Finish key replacement") || !strings.Contains(retryPageBody, "paused for key safety") || strings.Contains(retryPageBody, "rotation-error") || strings.Contains(retryPageBody, "key_fresh") || strings.Contains(retryPageBody, "sk-fresh-party") {
		t.Fatal("party page did not offer a private, retryable rotation state")
	}
	pausedDashboard := get(t, client, server.URL+"/app")
	pausedDashboardBody := readBody(t, pausedDashboard)
	if pausedDashboard.StatusCode != http.StatusOK || !strings.Contains(pausedDashboardBody, "AI voice: paused for key safety") || strings.Contains(pausedDashboardBody, "rotation-error") {
		t.Fatal("dashboard exposed an internal key rotation status")
	}
	rotatingParty, _, err := database.PartyVoiceSettings(t.Context(), partyID)
	if err != nil {
		t.Fatal(err)
	}
	if rotatingParty.OpenAIStatus != "rotation-error" || rotatingParty.OpenAIAPIKeyID != "key_fresh" {
		t.Fatalf("unexpected partial rotation state: %#v", rotatingParty)
	}
	decryptedFreshKey, err := cipher.Decrypt(rotatingParty.OpenAIKeyCiphertext, []byte(partyID))
	if err != nil || decryptedFreshKey != "sk-fresh-party" {
		t.Fatal("fresh party key was not encrypted and installed")
	}
	routingServices, err = database.RoutingServices(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(routingServices) != 1 || routingServices[0].WeatherEnabled || routingServices[0].AIEnabled {
		t.Fatalf("AI-powered routes were not paused after partial rotation: %#v", routingServices)
	}
	retriedRotation := postForm(t, client, server.URL+"/parties/"+partyID+"/openai-key/rotate", url.Values{"csrf": {csrf}})
	retriedRotationBody := readBody(t, retriedRotation)
	if retriedRotation.StatusCode != http.StatusOK || !strings.Contains(retriedRotationBody, "fresh OpenAI key") || strings.Contains(retriedRotationBody, "sk-fresh-party") {
		t.Fatalf("key rotation retry did not complete safely: status=%d", retriedRotation.StatusCode)
	}
	readyParty, _, err := database.PartyVoiceSettings(t.Context(), partyID)
	if err != nil {
		t.Fatal(err)
	}
	if readyParty.OpenAIStatus != "ready" || readyParty.OpenAIAPIKeyID != "key_fresh" || keyManager.createAttempts != 1 || keyManager.listAttempts != 2 || keyManager.deleteAttempts != 2 || len(keyManager.deletedKeys) != 1 || keyManager.deletedKeys[0] != "key_old" {
		t.Fatalf("unexpected completed key rotation: party=%#v manager=%#v", readyParty, keyManager)
	}
	routingServices, err = database.RoutingServices(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(routingServices) != 1 || !routingServices[0].WeatherEnabled || !routingServices[0].AIEnabled {
		t.Fatalf("AI-powered routes did not resume after rotation: %#v", routingServices)
	}

	invite := postForm(t, client, server.URL+"/parties/"+partyID+"/invites", url.Values{"csrf": {csrf}})
	if invite.StatusCode != http.StatusOK {
		t.Fatalf("create invite status = %d", invite.StatusCode)
	}
	inviteBody := readBody(t, invite)
	if invite.Header.Get("Cache-Control") != "no-store" || !strings.Contains(inviteBody, `class="invite-qr"`) ||
		!strings.Contains(inviteBody, `src="data:image/png;base64,`) || !strings.Contains(inviteBody, "Or scan it.") ||
		!strings.Contains(inviteBody, "Anyone with the link or code can claim this invitation") ||
		!strings.Contains(inviteBody, "Manage unused link · 1 active") || !strings.Contains(inviteBody, "Cancel unused link") {
		t.Fatal("one-time invitation did not include its private local QR handoff")
	}
	inviteURL := firstMatch(t, inviteBody, `value="(http://[^"]+/join/[^"]+)"`)
	inviteHiddenAgain := get(t, client, server.URL+"/parties/"+partyID)
	inviteHiddenBody := readBody(t, inviteHiddenAgain)
	if strings.Contains(inviteHiddenBody, inviteURL) || strings.Contains(inviteHiddenBody, `class="invite-qr"`) || strings.Contains(inviteHiddenBody, `data:image/png;base64,`) {
		t.Fatal("one-time invitation link or QR was revealed on a second party view")
	}

	join := get(t, client, inviteURL)
	if join.StatusCode != http.StatusOK {
		t.Fatalf("join status = %d", join.StatusCode)
	}
	joinBody := readBody(t, join)
	if !strings.Contains(joinBody, `id="extension" name="extension" value="101"`) || !strings.Contains(joinBody, "RingRing suggested an available number") || !strings.Contains(joinBody, "Public emergency and crisis numbers are unavailable") {
		t.Fatal("empty-party invitation did not prefill and explain a safe extension")
	}
	joinCSRF := firstMatch(t, joinBody, `name="csrf" value="([^"]+)"`)
	setup := postForm(t, client, inviteURL, url.Values{
		"csrf": {joinCSRF}, "display_name": {"Blue phone"}, "extension": {"101"}, "device_label": {"ATA"},
	})
	setupBody := readBody(t, setup)
	if setup.StatusCode != http.StatusOK || !strings.Contains(setupBody, "You are extension 101") || !strings.Contains(setupBody, "sip.example.test") || !strings.Contains(setupBody, "Scan it with <em>Linphone.</em>") || !strings.Contains(setupBody, "data:image/png;base64,") || !strings.Contains(setupBody, "href=\"sip-linphone:?linphone-fetch-config=http%3A%2F%2F") || strings.Contains(setupBody, "#ZgotmplZ") || !strings.Contains(setupBody, "Use Linphone’s scanner—not the regular Camera app") || !strings.Contains(setupBody, "TLS · port 5061") || !strings.Contains(setupBody, "UDP · port 5060") || !strings.Contains(setupBody, "TLS protects phone sign-in and call setup") || !strings.Contains(setupBody, "voice audio is still server-relayed RTP, not encrypted media") || !strings.Contains(setupBody, "Test both directions") || !strings.Contains(setupBody, "dial <strong>*10</strong>") || !strings.Contains(setupBody, "Pick a different extension by phone") || !strings.Contains(setupBody, "Dial <strong>*15</strong>") || !strings.Contains(setupBody, "press <strong>1</strong> to save") || !strings.Contains(setupBody, `data-copy-setup`) || !strings.Contains(setupBody, `data-copy-target="setup-password"`) || !strings.Contains(setupBody, "This copies the password too") || !strings.Contains(setupBody, "SIP user, user ID, authentication ID") || !strings.Contains(setupBody, "Do not forward router ports") {
		t.Fatalf("setup response was not successful: status=%d", setup.StatusCode)
	}
	oldUsername := firstMatch(t, setupBody, `(rrd_[A-Za-z0-9_-]+)`)
	oldPassword := firstMatch(t, setupBody, `<strong id="setup-password" data-setup-value>([^<]+)</strong>`)
	provisionURL := firstMatch(t, setupBody, `value="(http://[^"]+/provision/linphone/[A-Za-z0-9_-]{43})"`)
	if setup.Header.Get("Cache-Control") != "no-store" || setup.Header.Get("Referrer-Policy") != "no-referrer" || !strings.Contains(setup.Header.Get("X-Robots-Tag"), "noindex") {
		t.Fatal("setup credentials must not be cached")
	}
	headRequest, err := http.NewRequest(http.MethodHead, provisionURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	headProvision, err := client.Do(headRequest)
	if err != nil {
		t.Fatal(err)
	}
	_ = headProvision.Body.Close()
	if headProvision.StatusCode != http.StatusMethodNotAllowed || headProvision.Header.Get("Allow") != http.MethodGet {
		t.Fatalf("HEAD provisioning check consumed or accepted the link: status=%d", headProvision.StatusCode)
	}
	provisioned := get(t, client, provisionURL)
	provisionedBody := readBody(t, provisioned)
	if provisioned.StatusCode != http.StatusOK || !strings.HasPrefix(provisioned.Header.Get("Content-Type"), "application/xml") || provisioned.Header.Get("Cache-Control") != "no-store, max-age=0" || provisioned.Header.Get("Referrer-Policy") != "no-referrer" || provisioned.Header.Get("Cross-Origin-Resource-Policy") != "same-origin" || !strings.Contains(provisioned.Header.Get("X-Robots-Tag"), "noindex") {
		t.Fatalf("Linphone provisioning headers/status were unsafe: status=%d headers=%v", provisioned.StatusCode, provisioned.Header)
	}
	if !strings.Contains(provisionedBody, `<entry name="username" overwrite="true">`+oldUsername+`</entry>`) || !strings.Contains(provisionedBody, `<entry name="passwd" overwrite="true">`+oldPassword+`</entry>`) || !strings.Contains(provisionedBody, `&lt;sip:sip.example.test:5061;transport=tls&gt;`) || !strings.Contains(provisionedBody, `RingRing 101`) {
		t.Fatal("Linphone provisioning document omitted the claimed account")
	}
	for _, privateValue := range []string{"Blue phone", "Cousins Club", "ATA", "host@example.test", "proj_test"} {
		if strings.Contains(provisionedBody, privateValue) {
			t.Fatalf("Linphone provisioning exposed unrelated private data: %q", privateValue)
		}
	}
	if reusedProvision := get(t, client, provisionURL); reusedProvision.StatusCode != http.StatusGone || !strings.Contains(readBody(t, reusedProvision), "used, expired, replaced, or disconnected") {
		t.Fatalf("Linphone provisioning link was reusable: status=%d", reusedProvision.StatusCode)
	}
	unknownProvision := get(t, client, server.URL+"/provision/linphone/"+strings.Repeat("x", 43))
	if unknownProvision.StatusCode != http.StatusGone || !strings.Contains(readBody(t, unknownProvision), "setup link is finished") {
		t.Fatalf("unknown provisioning link did not fail generically: status=%d", unknownProvision.StatusCode)
	}

	secondInvite := postForm(t, client, server.URL+"/parties/"+partyID+"/invites", url.Values{"csrf": {csrf}})
	if secondInvite.StatusCode != http.StatusOK {
		t.Fatalf("second invite status = %d", secondInvite.StatusCode)
	}
	secondInviteURL := firstMatch(t, readBody(t, secondInvite), `value="(http://[^"]+/join/[^"]+)"`)
	secondJoin := get(t, client, secondInviteURL)
	secondJoinBody := readBody(t, secondJoin)
	if secondJoin.StatusCode != http.StatusOK || !strings.Contains(secondJoinBody, `id="extension" name="extension" value="102"`) {
		t.Fatalf("occupied-party suggestion was not 102: status=%d", secondJoin.StatusCode)
	}
	secondJoinCSRF := firstMatch(t, secondJoinBody, `name="csrf" value="([^"]+)"`)
	oversized := postForm(t, client, secondInviteURL, url.Values{
		"csrf": {secondJoinCSRF}, "display_name": {strings.Repeat("x", 33<<10)}, "extension": {"102"}, "device_label": {"Desk phone"},
	})
	if oversized.StatusCode != http.StatusRequestEntityTooLarge || !strings.Contains(readBody(t, oversized), "form is too large") {
		t.Fatalf("oversized invitation claim was not rejected: status=%d", oversized.StatusCode)
	}
	refreshedJoin := get(t, client, secondInviteURL)
	refreshedJoinBody := readBody(t, refreshedJoin)
	if refreshedJoin.StatusCode != http.StatusOK || !strings.Contains(refreshedJoinBody, `id="extension" name="extension" value="102"`) {
		t.Fatalf("oversized claim consumed or changed invitation: status=%d", refreshedJoin.StatusCode)
	}
	secondJoinCSRF = firstMatch(t, refreshedJoinBody, `name="csrf" value="([^"]+)"`)
	reservedNumber := postForm(t, client, secondInviteURL, url.Values{
		"csrf": {secondJoinCSRF}, "display_name": {"Green phone"}, "extension": {"911"}, "device_label": {"Desk phone"},
	})
	reservedBody := readBody(t, reservedNumber)
	if reservedNumber.StatusCode != http.StatusBadRequest || !strings.Contains(reservedBody, "public emergency or crisis numbers stay unavailable") ||
		!strings.Contains(reservedBody, `value="Green phone"`) || !strings.Contains(reservedBody, `value="Desk phone"`) ||
		!strings.Contains(reservedBody, `id="extension" name="extension" value="102"`) || !strings.Contains(reservedBody, `aria-invalid="true"`) {
		t.Fatalf("reserved extension did not return a preserved, corrected join form: status=%d", reservedNumber.StatusCode)
	}
	collidingNumber := postForm(t, client, secondInviteURL, url.Values{
		"csrf": {secondJoinCSRF}, "display_name": {"Green phone"}, "extension": {"101"}, "device_label": {"Desk phone"},
	})
	collisionBody := readBody(t, collidingNumber)
	if collidingNumber.StatusCode != http.StatusConflict || !strings.Contains(collisionBody, "That number was just claimed") ||
		!strings.Contains(collisionBody, `id="extension" name="extension" value="102"`) || !strings.Contains(collisionBody, `value="Green phone"`) || !strings.Contains(collisionBody, `value="Desk phone"`) {
		t.Fatalf("extension collision did not offer a preserved fresh suggestion: status=%d", collidingNumber.StatusCode)
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
	if !strings.Contains(hostPartyBody, "Phone echo test") || !strings.Contains(hostPartyBody, "Always ready") || !strings.Contains(hostPartyBody, "Pick an extension") || !strings.Contains(hostPartyBody, "*15") || !strings.Contains(hostPartyBody, "authenticated phone") {
		t.Fatal("party page omitted an always-available phone utility")
	}
	if !strings.Contains(hostPartyBody, "status unavailable") || !strings.Contains(hostPartyBody, "Live phone status is temporarily unavailable") {
		t.Fatal("party page did not fail safely when live presence was unconfigured")
	}
	presenceCounter := &countingContactPresence{statuses: map[string]telephony.ContactState{oldUsername: telephony.ContactReachable}}
	app.presence = presenceCounter
	outsiderJar, _ := cookiejar.New(nil)
	outsiderClient := &http.Client{Transport: server.Client().Transport, Jar: outsiderJar}
	outsiderLogin := postForm(t, outsiderClient, server.URL+"/auth/dev", url.Values{"email": {"another-host@example.test"}})
	outsiderLoginBody := readBody(t, outsiderLogin)
	outsiderCSRF := firstMatch(t, outsiderLoginBody, `name="csrf" value="([^"]+)"`)
	createAttemptsBeforeOutsider := keyManager.createAttempts
	outsiderRotation := postForm(t, outsiderClient, server.URL+"/parties/"+partyID+"/openai-key/rotate", url.Values{"csrf": {outsiderCSRF}})
	if outsiderRotation.StatusCode != http.StatusNotFound || keyManager.createAttempts != createAttemptsBeforeOutsider {
		t.Fatalf("another host reached key rotation: status=%d creates=%d", outsiderRotation.StatusCode, keyManager.createAttempts)
	}
	_ = readBody(t, outsiderRotation)
	outsiderParty := get(t, outsiderClient, server.URL+"/parties/"+partyID)
	if outsiderParty.StatusCode != http.StatusNotFound || presenceCounter.calls != 0 {
		t.Fatalf("another host reached party presence: status=%d calls=%d", outsiderParty.StatusCode, presenceCounter.calls)
	}
	_ = readBody(t, outsiderParty)
	cancelInvitesPath := server.URL + "/parties/" + partyID + "/invites/cancel"
	missingCancelCSRF := postForm(t, client, cancelInvitesPath, nil)
	if missingCancelCSRF.StatusCode != http.StatusForbidden {
		t.Fatalf("invitation cancellation accepted missing CSRF: status=%d", missingCancelCSRF.StatusCode)
	}
	_ = readBody(t, missingCancelCSRF)
	outsiderCancel := postForm(t, outsiderClient, cancelInvitesPath, url.Values{"csrf": {outsiderCSRF}})
	if outsiderCancel.StatusCode != http.StatusNotFound {
		t.Fatalf("another host canceled invitations: status=%d", outsiderCancel.StatusCode)
	}
	_ = readBody(t, outsiderCancel)
	if stillActive := get(t, client, secondInviteURL); stillActive.StatusCode != http.StatusOK {
		t.Fatalf("rejected cancellation changed active invitation: status=%d", stillActive.StatusCode)
	} else {
		_ = readBody(t, stillActive)
	}
	canceledInvites := postForm(t, client, cancelInvitesPath, url.Values{"csrf": {csrf}})
	canceledInvitesBody := readBody(t, canceledInvites)
	if canceledInvites.StatusCode != http.StatusOK || !strings.Contains(canceledInvitesBody, "Unused invitation links were canceled") ||
		strings.Contains(canceledInvitesBody, "Manage unused link") || strings.Contains(canceledInvitesBody, "Cancel unused link") {
		t.Fatalf("host invitation cancellation response was wrong: status=%d", canceledInvites.StatusCode)
	}
	if canceledLink := get(t, client, secondInviteURL); canceledLink.StatusCode != http.StatusNotFound || !strings.Contains(readBody(t, canceledLink), "not in service") {
		t.Fatalf("canceled invitation remained usable: status=%d", canceledLink.StatusCode)
	}
	presenceCounter.calls = 0
	hostParty = get(t, client, server.URL+"/parties/"+partyID)
	hostPartyBody = readBody(t, hostParty)
	onlineRendered := strings.Contains(hostPartyBody, "presence-online\">online")
	memberLabelRendered := strings.Contains(hostPartyBody, `aria-label="At least one phone is online"`)
	unavailableRendered := strings.Contains(hostPartyBody, "Live phone status is temporarily unavailable")
	readinessRendered := strings.Contains(hostPartyBody, "Real phone check · 0 of 3") && strings.Contains(hostPartyBody, "different internet connection") && strings.Contains(hostPartyBody, "does not save who called, network details, call audio, or a call log")
	if presenceCounter.calls != 1 || !onlineRendered || !memberLabelRendered || unavailableRendered || !readinessRendered {
		t.Fatalf("party live presence calls=%d online=%t member_label=%t unavailable=%t", presenceCounter.calls, onlineRendered, memberLabelRendered, unavailableRendered)
	}
	memberID := firstMatch(t, hostPartyBody, `/members/([^/]+)/devices`)
	addDevicePath := server.URL + "/parties/" + partyID + "/members/" + memberID + "/devices"
	if !strings.Contains(hostPartyBody, "Add another phone") || !strings.Contains(hostPartyBody, "calls to this extension ring them together") ||
		!strings.Contains(hostPartyBody, `id="new-device-label-`+memberID+`"`) {
		t.Fatal("member row omitted the same-extension phone setup flow")
	}
	missingAddCSRF := postForm(t, client, addDevicePath, url.Values{"device_label": {"Tablet app"}})
	if missingAddCSRF.StatusCode != http.StatusForbidden {
		t.Fatalf("phone creation accepted a missing CSRF token: status=%d", missingAddCSRF.StatusCode)
	}
	_ = readBody(t, missingAddCSRF)
	outsiderAdd := postForm(t, outsiderClient, addDevicePath, url.Values{"csrf": {outsiderCSRF}, "device_label": {"Tablet app"}})
	if outsiderAdd.StatusCode != http.StatusNotFound {
		t.Fatalf("another host added a phone: status=%d", outsiderAdd.StatusCode)
	}
	_ = readBody(t, outsiderAdd)
	longLabelAdd := postForm(t, client, addDevicePath, url.Values{"csrf": {csrf}, "device_label": {strings.Repeat("x", 41)}})
	if longLabelAdd.StatusCode != http.StatusBadRequest || !strings.Contains(readBody(t, longLabelAdd), "phone name is too long") {
		t.Fatalf("overlong phone label status=%d", longLabelAdd.StatusCode)
	}
	routing, err = database.RoutingDevices(t.Context())
	if err != nil || len(routing) != 1 {
		t.Fatalf("rejected phone additions changed routing: %#v error=%v", routing, err)
	}

	addedPhone := postForm(t, client, addDevicePath, url.Values{"csrf": {csrf}, "device_label": {"Tablet app"}})
	addedPhoneBody := readBody(t, addedPhone)
	if addedPhone.StatusCode != http.StatusOK || addedPhone.Header.Get("Cache-Control") != "no-store" ||
		!strings.Contains(addedPhoneBody, "Another phone ready") || !strings.Contains(addedPhoneBody, "shares extension 101") ||
		!strings.Contains(addedPhoneBody, "Existing phones stay connected") || !strings.Contains(addedPhoneBody, "Tablet app") ||
		strings.Contains(addedPhoneBody, "first-call-card") {
		t.Fatalf("second-phone setup response was not successful: status=%d", addedPhone.StatusCode)
	}
	secondUsername := firstMatch(t, addedPhoneBody, `(rrd_[A-Za-z0-9_-]+)`)
	if secondUsername == oldUsername {
		t.Fatal("second phone reused the first phone's SIP username")
	}
	secondProvisionURL := firstMatch(t, addedPhoneBody, `value="(http://[^"]+/provision/linphone/[A-Za-z0-9_-]{43})"`)
	membersAfterAdd, err := database.ListMembers(t.Context(), partyID)
	if err != nil || len(membersAfterAdd) != 1 || len(membersAfterAdd[0].Devices) != 2 {
		t.Fatalf("second phone was not attached to one member: %#v error=%v", membersAfterAdd, err)
	}
	var secondDeviceID string
	for _, candidate := range membersAfterAdd[0].Devices {
		if candidate.Label == "Tablet app" {
			secondDeviceID = candidate.ID
		}
	}
	if secondDeviceID == "" {
		t.Fatal("could not identify the newly added phone")
	}
	routing, err = database.RoutingDevices(t.Context())
	if err != nil || len(routing) != 2 || routing[0].Extension != "101" || routing[1].Extension != "101" {
		t.Fatalf("same-extension phones were not both routable: %#v error=%v", routing, err)
	}
	dialDevices, err := telephony.FromRoutingDevices(routing, cipher.Decrypt)
	if err != nil {
		t.Fatal(err)
	}
	configuration, err := telephony.Render(dialDevices, nil)
	if err != nil {
		t.Fatal(err)
	}
	foundForkedRing := false
	for _, line := range strings.Split(string(configuration.Dialplan), "\n") {
		if strings.Contains(line, "Dial(PJSIP/") && strings.Contains(line, oldUsername) && strings.Contains(line, secondUsername) {
			foundForkedRing = true
		}
	}
	if !foundForkedRing {
		t.Fatalf("same-extension phones did not share one explicit Dial line:\n%s", configuration.Dialplan)
	}
	if again := get(t, client, addedPhone.Request.URL.String()); again.StatusCode != http.StatusGone {
		t.Fatalf("new-phone setup reveal could be read twice: status=%d", again.StatusCode)
	} else {
		_ = readBody(t, again)
	}
	revokedSecond := postForm(t, client, server.URL+"/parties/"+partyID+"/devices/"+secondDeviceID+"/revoke", url.Values{"csrf": {csrf}})
	if revokedSecond.StatusCode != http.StatusOK || !strings.Contains(readBody(t, revokedSecond), "Tablet app") {
		t.Fatalf("second phone could not be independently disconnected: status=%d", revokedSecond.StatusCode)
	}
	if staleProvision := get(t, client, secondProvisionURL); staleProvision.StatusCode != http.StatusGone {
		t.Fatalf("disconnected second phone retained provisioning: status=%d", staleProvision.StatusCode)
	} else {
		_ = readBody(t, staleProvision)
	}
	routing, err = database.RoutingDevices(t.Context())
	if err != nil || len(routing) != 1 || routing[0].SIPUsername != oldUsername {
		t.Fatalf("disconnecting the second phone changed the first: %#v error=%v", routing, err)
	}

	deviceID := firstMatch(t, hostPartyBody, `/devices/([^/]+)/rotate`)
	ringer := &fakeDeviceRinger{statuses: map[string]telephony.ContactState{oldUsername: telephony.ContactReachable}}
	app.ringer = ringer
	ringPath := server.URL + "/parties/" + partyID + "/devices/" + deviceID + "/ring-test"
	if !strings.Contains(hostPartyBody, `action="/parties/`+partyID+`/devices/`+deviceID+`/ring-test"`) || !strings.Contains(hostPartyBody, "📳 Ring this phone") || !strings.Contains(hostPartyBody, "It will say extension 101") {
		t.Fatal("online phone did not show the incoming ring test")
	}
	outsiderRing := postForm(t, outsiderClient, ringPath, url.Values{"csrf": {outsiderCSRF}})
	if outsiderRing.StatusCode != http.StatusNotFound || ringer.calls != 0 {
		t.Fatalf("another host reached the phone ring test: status=%d calls=%d", outsiderRing.StatusCode, ringer.calls)
	}
	_ = readBody(t, outsiderRing)
	ringer.statuses = map[string]telephony.ContactState{}
	offlineRing := postForm(t, client, ringPath, url.Values{"csrf": {csrf}})
	if offlineRing.StatusCode != http.StatusConflict || !strings.Contains(readBody(t, offlineRing), "not online yet") || ringer.calls != 0 {
		t.Fatalf("offline phone ring status=%d calls=%d", offlineRing.StatusCode, ringer.calls)
	}
	fixedNow = fixedNow.Add(time.Minute)
	ringer.statuses = map[string]telephony.ContactState{oldUsername: telephony.ContactReachable}
	for attempt := 1; attempt <= 2; attempt++ {
		ringSent := postForm(t, client, ringPath, url.Values{"csrf": {csrf}})
		ringSentBody := readBody(t, ringSent)
		if ringSent.StatusCode != http.StatusOK || !strings.Contains(ringSentBody, "Ring test sent") || ringer.calls != attempt || ringer.username != oldUsername || ringer.extension != "101" {
			t.Fatalf("ring attempt %d status=%d calls=%d target=%q extension=%q", attempt, ringSent.StatusCode, ringer.calls, ringer.username, ringer.extension)
		}
	}
	limitedRing := postForm(t, client, ringPath, url.Values{"csrf": {csrf}})
	if limitedRing.StatusCode != http.StatusTooManyRequests || limitedRing.Header.Get("Retry-After") != "60" || !strings.Contains(readBody(t, limitedRing), "Give that phone a moment") || ringer.calls != 2 {
		t.Fatalf("repeated ring was not limited: status=%d calls=%d", limitedRing.StatusCode, ringer.calls)
	}
	readinessPath := server.URL + "/parties/" + partyID + "/devices/" + deviceID + "/readiness"
	outsiderReadiness := postForm(t, outsiderClient, readinessPath, url.Values{
		"csrf": {outsiderCSRF}, "echo_tested": {"1"}, "outgoing_call_tested": {"1"}, "incoming_call_tested": {"1"},
	})
	if outsiderReadiness.StatusCode != http.StatusNotFound {
		t.Fatalf("another host updated phone readiness: status=%d", outsiderReadiness.StatusCode)
	}
	_ = readBody(t, outsiderReadiness)
	checkedPhone := postForm(t, client, readinessPath, url.Values{
		"csrf": {csrf}, "echo_tested": {"1"}, "outgoing_call_tested": {"1"}, "incoming_call_tested": {"1"},
	})
	checkedPhoneBody := readBody(t, checkedPhone)
	if checkedPhone.StatusCode != http.StatusOK || !strings.Contains(checkedPhoneBody, "host-confirmed real-phone checks were saved") || !strings.Contains(checkedPhoneBody, "Real phone check · 3 of 3") || !strings.Contains(checkedPhoneBody, "All three checks are host-confirmed") {
		t.Fatalf("phone readiness response was not successful: status=%d", checkedPhone.StatusCode)
	}
	rotated := postForm(t, client, server.URL+"/parties/"+partyID+"/devices/"+deviceID+"/rotate", url.Values{"csrf": {csrf}})
	rotatedBody := readBody(t, rotated)
	if rotated.StatusCode != http.StatusOK || !strings.Contains(rotatedBody, "Fresh phone settings") ||
		!strings.Contains(rotatedBody, "old username and password no longer work") || strings.Contains(rotatedBody, "first-call-card") {
		t.Fatalf("rotation setup response was not successful: status=%d", rotated.StatusCode)
	}
	freshUsername := firstMatch(t, rotatedBody, `(rrd_[A-Za-z0-9_-]+)`)
	if freshUsername == oldUsername {
		t.Fatal("rotation did not replace the SIP username")
	}
	rotatedProvisionURL := firstMatch(t, rotatedBody, `value="(http://[^"]+/provision/linphone/[A-Za-z0-9_-]{43})"`)
	if rotatedProvisionURL == provisionURL {
		t.Fatal("rotation reused the prior provisioning link")
	}
	if again := get(t, client, rotated.Request.URL.String()); again.StatusCode != http.StatusGone {
		t.Fatalf("setup reveal could be read twice: status=%d", again.StatusCode)
	} else {
		_ = readBody(t, again)
	}
	app.presence = fakeContactPresence{statuses: map[string]telephony.ContactState{freshUsername: telephony.ContactReachable}}
	hostParty = get(t, client, server.URL+"/parties/"+partyID)
	if !strings.Contains(readBody(t, hostParty), "Real phone check · 0 of 3") {
		t.Fatal("credential rotation did not clear host-confirmed phone checks")
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
	if revokedProvision := get(t, client, rotatedProvisionURL); revokedProvision.StatusCode != http.StatusGone || !strings.Contains(readBody(t, revokedProvision), "used, expired, replaced, or disconnected") {
		t.Fatalf("revocation left provisioning usable: status=%d", revokedProvision.StatusCode)
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

func TestSecretPathsAreMaskedAndProvisioningIsRateLimited(t *testing.T) {
	invitation := httptest.NewRequest(http.MethodGet, "/join/invitation-secret", nil)
	invitation.Pattern = "GET /join/{token}"
	if got := safeRoute(invitation); got != "/join/{token}" || requestSurface(invitation) != "invitation" {
		t.Fatalf("safe invitation route = %q surface=%q", got, requestSurface(invitation))
	}
	provisioning := httptest.NewRequest(http.MethodGet, "/provision/linphone/provision-secret", nil)
	provisioning.Pattern = "GET /provision/linphone/{token}"
	if got := safeRoute(provisioning); got != "/provision/linphone/{token}" || requestSurface(provisioning) != "provisioning" {
		t.Fatalf("safe provisioning route = %q surface=%q", got, requestSurface(provisioning))
	}
	unmatched := httptest.NewRequest(http.MethodGet, "/private-value", nil)
	if got := safeRoute(unmatched); got != "unmatched" || requestSurface(unmatched) != "other" {
		t.Fatalf("unmatched route = %q surface=%q", got, requestSurface(unmatched))
	}
	req := httptest.NewRequest(http.MethodGet, "/provision/linphone/provision-secret", nil)
	category, limit, window := rateCategory(req)
	if category != "provision" || limit != 20 || window != 5*time.Minute {
		t.Fatalf("provisioning rate category = %q %d %s", category, limit, window)
	}
}

func TestInternalMetricsAreAggregateAndAbsentFromPublicHandler(t *testing.T) {
	database, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	cipher, err := secure.NewCipher(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	app, err := New(config.Config{Environment: "development", BaseURL: "http://ringring.test"}, database, cipher, logger)
	if err != nil {
		t.Fatal(err)
	}
	app.presence = fakeContactPresence{statuses: map[string]telephony.ContactState{
		"private-device-one": telephony.ContactReachable,
		"private-device-two": telephony.ContactUnreachable,
	}}

	for _, path := range []string{
		"/parties/private-party-identifier",
		"/join/private-invitation-token",
		"/private-unmatched-path",
	} {
		response := httptest.NewRecorder()
		app.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
	}
	privateMethod := httptest.NewRecorder()
	app.ServeHTTP(privateMethod, httptest.NewRequest("PRIVATE-METHOD", "/private-method-path", nil))
	publicMetrics := httptest.NewRecorder()
	app.ServeHTTP(publicMetrics, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if publicMetrics.Code != http.StatusNotFound || strings.Contains(publicMetrics.Body.String(), "ringring_database_up") {
		t.Fatalf("public handler exposed metrics: status=%d body=%q", publicMetrics.Code, publicMetrics.Body.String())
	}

	internalMetrics := httptest.NewRecorder()
	app.MetricsHandler().ServeHTTP(internalMetrics, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := internalMetrics.Body.String()
	for _, expected := range []string{
		"ringring_database_up 1",
		"ringring_asterisk_ami_up 1",
		"ringring_sip_contacts{state=\"reachable\"} 1",
		"ringring_sip_contacts{state=\"unreachable\"} 1",
		"ringring_http_requests_total{surface=\"host\",method=\"GET\",status_class=\"3xx\"} 1",
		"ringring_http_requests_total{surface=\"invitation\",method=\"GET\",status_class=\"4xx\"} 1",
	} {
		if !strings.Contains(body, expected) {
			t.Errorf("internal metrics omitted %q\n%s", expected, body)
		}
	}
	combined := logs.String() + body
	for _, forbidden := range []string{
		"private-party-identifier", "private-invitation-token", "private-unmatched-path",
		"private-device-one", "private-device-two", "PRIVATE-METHOD", "private-method-path",
	} {
		if strings.Contains(combined, forbidden) {
			t.Fatalf("logs or metrics exposed record value %q\n%s", forbidden, combined)
		}
	}
	for _, expectedRoute := range []string{"route=/parties/{partyID}", "route=/join/{token}"} {
		if !strings.Contains(logs.String(), expectedRoute) {
			t.Errorf("request log omitted safe route template %q\n%s", expectedRoute, logs.String())
		}
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
	duplicateBody := readBody(t, duplicate)
	if duplicate.StatusCode != http.StatusConflict || !strings.Contains(duplicateBody, "not available") {
		t.Fatalf("duplicate username status=%d", duplicate.StatusCode)
	}
	if !regexp.MustCompile(`id="signup-username"[^>]*aria-describedby="signup-username-help signup-error"[^>]*aria-invalid="true"`).MatchString(duplicateBody) || regexp.MustCompile(`id="signup-name"[^>]*aria-invalid="true"`).MatchString(duplicateBody) {
		t.Fatal("signup error did not identify only the invalid username field")
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
	oldLoginBody := readBody(t, oldLogin)
	if oldLogin.StatusCode != http.StatusUnauthorized || !strings.Contains(oldLoginBody, "did not match") {
		t.Fatalf("old password remained valid: status=%d", oldLogin.StatusCode)
	}
	for _, field := range []string{"login-username", "login-password"} {
		if !regexp.MustCompile(`id="` + field + `"[^>]*aria-describedby="login-error"[^>]*aria-invalid="true"`).MatchString(oldLoginBody) {
			t.Fatalf("credential error did not associate %s with the visible alert", field)
		}
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
