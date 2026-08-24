package webapp

import (
	"bytes"
	"context"
	"encoding/json"
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
	"github.com/amcchord/ringring/internal/provisioning"
	"github.com/amcchord/ringring/internal/secure"
	"github.com/amcchord/ringring/internal/sipcredentials"
	"github.com/amcchord/ringring/internal/store"
	"github.com/amcchord/ringring/internal/telephony"
	"github.com/amcchord/ringring/internal/weather"
)

type fakeWeatherGeocoder struct {
	query string
}

type fakeOpenAIProjects struct {
	provisionAttempts          int
	provisioned                openaiadmin.ProvisionedProject
	provisionErr               error
	organizationRetentionCalls int
	organizationRetentionErr   error
	retentionAttempts          int
	retentionProject           string
	retentionErr               error
	archiveAttempts            int
	archived                   []string
	archiveErr                 error
	createAttempts             int
	createdKey                 openaiadmin.ServiceAccountAPIKey
	createErr                  error
	listAttempts               int
	keyIDs                     []string
	listErr                    error
	deleteAttempts             int
	deletedKeys                []string
	deleteFailures             int
	spendAttempts              int
	spendAmounts               []int
	spendFailures              int
}

type fakeContactPresence struct {
	statuses map[string]telephony.ContactState
	err      error
}

type fakeActiveConferences struct {
	calls []telephony.ActiveConference
	err   error
	count int
}

func (f *fakeActiveConferences) ActiveConferenceCalls(context.Context) ([]telephony.ActiveConference, error) {
	f.count++
	return f.calls, f.err
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

func (f *fakeOpenAIProjects) Provision(context.Context, string, string) (openaiadmin.ProvisionedProject, error) {
	f.provisionAttempts++
	return f.provisioned, f.provisionErr
}

func (f *fakeOpenAIProjects) VerifyOrganizationZeroDataRetention(context.Context) (openaiadmin.OrganizationDataRetention, error) {
	f.organizationRetentionCalls++
	if f.organizationRetentionErr != nil {
		return openaiadmin.OrganizationDataRetention{}, f.organizationRetentionErr
	}
	return openaiadmin.OrganizationDataRetention{Type: "zero_data_retention"}, nil
}

func (f *fakeOpenAIProjects) VerifyProjectZeroDataRetention(_ context.Context, projectID string) (openaiadmin.ProjectDataRetention, error) {
	f.retentionAttempts++
	f.retentionProject = projectID
	if f.retentionErr != nil {
		return openaiadmin.ProjectDataRetention{}, f.retentionErr
	}
	return openaiadmin.ProjectDataRetention{Type: "organization_default"}, nil
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

func TestNewSIPCredentialsAreNumericGroupedAndEncrypted(t *testing.T) {
	cipher, err := secure.NewCipher(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	app := &App{cipher: cipher}
	username, password, ciphertext, err := app.newSIPCredentials("dev_numeric")
	if err != nil {
		t.Fatal(err)
	}
	if !sipcredentials.ValidUsername(username) || !sipcredentials.ValidPassword(password) {
		t.Fatalf("generated credentials were not keypad friendly: username=%q password_length=%d", username, len(password))
	}
	decrypted, err := cipher.Decrypt(ciphertext, []byte("dev_numeric"))
	if err != nil || decrypted != password {
		t.Fatalf("generated password was not encrypted with the device boundary: match=%t error=%v", decrypted == password, err)
	}
	if grouped := groupSetupDigits(username, 3); len(strings.Fields(grouped)) != 2 || strings.ReplaceAll(grouped, " ", "") != username {
		t.Fatalf("username grouping changed the credential: %q", grouped)
	}
	if grouped := groupSetupDigits(password, 4); len(strings.Fields(grouped)) != 3 || strings.ReplaceAll(grouped, " ", "") != password {
		t.Fatalf("password grouping changed the credential: %q", grouped)
	}
}

func TestWP826ProvisioningDownloadIsOneTimeAndPrivate(t *testing.T) {
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
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	host, err := database.UpsertGoogleUser(ctx, store.GoogleProfile{Subject: "wp826-host", Email: "host@example.test", Name: "Host"}, now, "usr_wp826")
	if err != nil {
		t.Fatal(err)
	}
	party, err := database.CreateParty(ctx, store.NewParty{ID: "pty_wp826", Name: "Private party", Slug: "private-party", HostUserID: host.ID, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	invitationToken := strings.Repeat("i", 43)
	if err := database.CreateInvitation(ctx, store.NewInvitation{
		ID: "inv_wp826", PartyID: party.ID, CreatedByUserID: host.ID, TokenHash: secure.Hash(invitationToken), ExpiresAt: now.Add(time.Hour), CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	password := "123456789012"
	ciphertext, err := cipher.Encrypt(password, []byte("dev_wp826"))
	if err != nil {
		t.Fatal(err)
	}
	provisioningToken := strings.Repeat("w", 43)
	if _, _, _, err := database.ClaimInvitation(ctx, store.NewClaim{
		TokenHash: secure.Hash(invitationToken), MemberID: "mem_wp826", DisplayName: "Private member", Extension: "103",
		DeviceID: "dev_wp826", DeviceLabel: "Private handset", SIPUsername: "654321", SIPSecretCiphertext: ciphertext,
		Provisioning: store.NewProvisioningToken{TokenHash: secure.Hash(provisioningToken), ExpiresAt: now.Add(time.Hour), CreatedAt: now}, Now: now,
	}); err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{
		Environment: "development", HTTPAddr: ":0", BaseURL: "https://ringring.live", DatabasePath: ":memory:",
		MasterKey: make([]byte, 32), SessionSecret: make([]byte, 32), SIPPublicHost: "ringring.live", InviteTTL: time.Hour,
	}
	app, err := New(cfg, database, cipher, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	app.now = func() time.Time { return now }
	server := httptest.NewServer(app)
	defer server.Close()
	downloadURL := server.URL + "/provision/wp826/" + provisioningToken

	headRequest, err := http.NewRequest(http.MethodHead, downloadURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	headResponse, err := server.Client().Do(headRequest)
	if err != nil {
		t.Fatal(err)
	}
	_ = headResponse.Body.Close()
	if headResponse.StatusCode != http.StatusMethodNotAllowed || headResponse.Header.Get("Allow") != http.MethodGet {
		t.Fatalf("HEAD WP826 provisioning consumed or accepted the link: status=%d", headResponse.StatusCode)
	}

	download := get(t, server.Client(), downloadURL)
	body := readBody(t, download)
	if download.StatusCode != http.StatusOK || !strings.HasPrefix(download.Header.Get("Content-Type"), "application/xml") ||
		download.Header.Get("Content-Disposition") != `attachment; filename="ringring-wp826.xml"` ||
		download.Header.Get("Cache-Control") != "no-store, max-age=0" || download.Header.Get("Referrer-Policy") != "no-referrer" ||
		download.Header.Get("Cross-Origin-Resource-Policy") != "same-origin" || !strings.Contains(download.Header.Get("X-Robots-Tag"), "noindex") {
		t.Fatalf("WP826 provisioning headers/status were unsafe: status=%d headers=%v", download.StatusCode, download.Header)
	}
	for _, required := range []string{
		`<config version="2">`, `<item name="account.1.sip.server.1">`, `<part name="address">ringring.live:5061</part>`,
		`<part name="userid">654321</part>`, `<part name="password">` + password + `</part>`,
		`https://ringring.live/static/wp826/wallpapers/ringring-memphis-day.png`, `ringring.live/static/wp826/ringtones`,
		`<item name="phonebook.download">`, `<part name="interval">5</part>`, `<part name="mode">Enabled Use HTTPS</part>`,
		`<part name="server">ringring.live/api/v1/phone/grandstream-phonebook.xml</part>`,
	} {
		if !strings.Contains(body, required) {
			t.Errorf("WP826 download omitted %q", required)
		}
	}
	for _, privateValue := range []string{"Private party", "Private member", "Private handset", "host@example.test"} {
		if strings.Contains(body, privateValue) {
			t.Errorf("WP826 download exposed unrelated private data %q", privateValue)
		}
	}
	if reused := get(t, server.Client(), downloadURL); reused.StatusCode != http.StatusGone || !strings.Contains(readBody(t, reused), "used, expired, replaced, or disconnected") {
		t.Fatalf("WP826 provisioning link was reusable: status=%d", reused.StatusCode)
	}
}

func TestSavingSIPCredentialsRetriesOnlyUsernameCollisions(t *testing.T) {
	cipher, err := secure.NewCipher(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	app := &App{cipher: cipher}
	attempts := 0
	username, password, err := app.saveWithNewSIPCredentials("dev_collision", func(candidate, ciphertext string) error {
		attempts++
		if candidate == "" || ciphertext == "" {
			t.Fatal("retry received an empty credential")
		}
		if attempts < 4 {
			return store.ErrSIPUsernameTaken
		}
		return nil
	})
	if err != nil || attempts != 4 || !sipcredentials.ValidUsername(username) || !sipcredentials.ValidPassword(password) {
		t.Fatalf("collision retry result: attempts=%d username_valid=%t password_valid=%t error=%v", attempts, sipcredentials.ValidUsername(username), sipcredentials.ValidPassword(password), err)
	}

	attempts = 0
	_, _, err = app.saveWithNewSIPCredentials("dev_store_error", func(string, string) error {
		attempts++
		return store.ErrNotFound
	})
	if !errors.Is(err, store.ErrNotFound) || attempts != 1 {
		t.Fatalf("non-collision error was retried: attempts=%d error=%v", attempts, err)
	}
}

func TestProvisionOpenAIStoresPartyCredentialsWithoutChangingProviderRetention(t *testing.T) {
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
	now := time.Date(2026, 8, 22, 14, 0, 0, 0, time.UTC)
	host, err := database.UpsertGoogleUser(ctx, store.GoogleProfile{Subject: "retention-provision-host", Email: "host@example.test", Name: "Host"}, now, "usr_retention_provision")
	if err != nil {
		t.Fatal(err)
	}
	for _, adultOnlyEnabled := range []bool{false, true} {
		partyID := "pty_provision_closed"
		projectID := "proj_provision_closed"
		if adultOnlyEnabled {
			partyID = "pty_provision_open"
			projectID = "proj_provision_open"
		}
		party, err := database.CreateParty(ctx, store.NewParty{ID: partyID, Name: "Provision test", Slug: partyID, HostUserID: host.ID, CreatedAt: now})
		if err != nil {
			t.Fatal(err)
		}
		manager := &fakeOpenAIProjects{provisioned: openaiadmin.ProvisionedProject{
			ProjectID: projectID, ServiceAccountID: "svc_test", APIKeyID: "key_test",
			APIKey: "private-runtime-key", SpendLimitCents: 1000,
		}}
		app := &App{
			cfg: config.Config{AIAdultOnlyEnabled: adultOnlyEnabled}, store: database, cipher: cipher, openAI: manager,
			logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		}
		if err := app.provisionOpenAI(ctx, party); err != nil {
			t.Fatal(err)
		}
		if manager.organizationRetentionCalls != 0 || manager.retentionAttempts != 0 || manager.archiveAttempts != 0 {
			t.Fatalf("adult-only provisioning changed or audited provider retention: %#v", manager)
		}
		stored, err := database.PartyForHost(ctx, party.ID, host.ID)
		if err != nil || stored.OpenAIProjectID != projectID || stored.OpenAIStatus != "ready" || stored.OpenAIKeyCiphertext == "" {
			t.Fatalf("party project was not stored normally: party=%#v error=%v", stored, err)
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
	if got := numbers(availableFirstCallLines(party, services, false, true)); got != "*10,*15,*11,*12,*13" {
		t.Fatalf("closed-gate first-call lines = %q", got)
	}
	if got := numbers(availableFirstCallLines(party, services, true, false)); got != "*10,*15,*11,*12,*13" {
		t.Fatalf("non-adult first-call lines = %q", got)
	}
	if got := numbers(availableFirstCallLines(party, services, true, true)); got != "*10,*15,*11,*12,*13,*14" {
		t.Fatalf("adult-extension first-call lines = %q", got)
	}
	setupServices := model.PartyServices{TimeEnabled: true, WeatherSetupAllowed: true}
	setupLines := availableFirstCallLines(party, setupServices, false, false)
	if got := numbers(setupLines); got != "*10,*15,*11,*12" || setupLines[len(setupLines)-1].Description != "Enter a ZIP once, then hear the local forecast." {
		t.Fatalf("unknown-location first-call lines = %#v", setupLines)
	}
	disabledKnown := model.PartyServices{TimeEnabled: true, WeatherLabel: "Portland, Maine"}
	if got := numbers(availableFirstCallLines(party, disabledKnown, false, false)); got != "*10,*15,*11" {
		t.Fatalf("host-disabled known weather lines = %q", got)
	}
	party.OpenAISpendLimitStatus = "update-error"
	if got := numbers(availableFirstCallLines(party, services, true, true)); got != "*10,*15,*11,*13" {
		t.Fatalf("spend-paused first-call lines = %q", got)
	}
}

func TestPhoneCallDestinationsHideOwnNumberAndIncludeOnlyRoutableChoices(t *testing.T) {
	revokedAt := time.Date(2026, 8, 22, 1, 0, 0, 0, time.UTC)
	destinations := phoneCallDestinations("101", []model.Member{
		{DisplayName: "Blue phone", Extension: "101", Devices: []model.Device{{ID: "dev_blue"}}},
		{DisplayName: "Green phone", Extension: "102", Devices: []model.Device{{ID: "dev_green"}}},
		{DisplayName: "Quiet phone", Extension: "103", Devices: []model.Device{{ID: "dev_quiet", RevokedAt: &revokedAt}}},
	}, []firstCallLine{{Number: "*10", Title: "Echo test", Description: "Hear your own voice come back."}})

	if len(destinations) != 2 {
		t.Fatalf("iOS call destinations = %#v", destinations)
	}
	if got := destinations[0]; got.Kind != "person" || got.Label != "Green phone" || got.Dial != "102" || got.Detail != "" {
		t.Fatalf("person destination = %#v", got)
	}
	if got := destinations[1]; got.Kind != "service" || got.Label != "Echo test" || got.Dial != "*10" || got.Detail == "" {
		t.Fatalf("service destination = %#v", got)
	}
}

func TestActivePartyCallsExposeOnlyKnownSamePartyPhones(t *testing.T) {
	calls := &fakeActiveConferences{calls: []telephony.ActiveConference{
		{Name: "rrc-pty_family-102", PartyID: "pty_family", JoinExtension: "102", Endpoints: []string{"111111", "222222"}},
		{Name: "rrc-pty_other-102", PartyID: "pty_other", JoinExtension: "102", Endpoints: []string{"111111", "222222"}},
		{Name: "rrc-pty_family-103", PartyID: "pty_family", JoinExtension: "103", Endpoints: []string{"111111", "unknown"}},
	}}
	app := &App{calls: calls, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	members := []model.Member{
		{ID: "mem_a", PartyID: "pty_family", DisplayName: "Austin", Extension: "101", Devices: []model.Device{{ID: "dev_a", SIPUsername: "111111"}}},
		{ID: "mem_b", PartyID: "pty_family", DisplayName: "Bea", Extension: "102", Devices: []model.Device{{ID: "dev_b", SIPUsername: "222222"}}},
		{ID: "mem_c", PartyID: "pty_family", DisplayName: "Casey", Extension: "103", Devices: []model.Device{{ID: "dev_c", SIPUsername: "333333"}}},
	}

	active, memberStatus, notice := app.activePartyCalls(t.Context(), "pty_family", members)
	if notice != "" || len(active) != 1 {
		t.Fatalf("same-party active calls = %#v notice=%q", active, notice)
	}
	if got := active[0]; got.JoinNumber != "*16102" || got.JoinExtension != "102" || got.PhoneCount != 2 || strings.Join(got.Participants, ",") != "Austin,Bea" {
		t.Fatalf("active call view = %#v", got)
	}
	if memberStatus["mem_a"] != "On a call with Bea" || memberStatus["mem_b"] != "On a call with Austin" || memberStatus["mem_c"] != "" {
		t.Fatalf("member call status = %#v", memberStatus)
	}
}

func TestPhoneOpenAPIIsPublicEmbeddedDocumentation(t *testing.T) {
	database, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	cipher, err := secure.NewCipher(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	app, err := New(config.Config{Environment: "development", BaseURL: "http://ringring.test"}, database, cipher, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(app)
	defer server.Close()

	response := get(t, server.Client(), server.URL+"/openapi.yaml")
	body := readBody(t, response)
	if response.StatusCode != http.StatusOK || !strings.HasPrefix(response.Header.Get("Content-Type"), "application/yaml") || response.Header.Get("Access-Control-Allow-Origin") != "*" || response.Header.Get("Cross-Origin-Resource-Policy") != "cross-origin" || !strings.Contains(response.Header.Get("Cache-Control"), "public") {
		t.Fatalf("OpenAPI response was not safely public: status=%d headers=%v", response.StatusCode, response.Header)
	}
	if !strings.Contains(body, "openapi: 3.1.2") || !strings.Contains(body, "/api/v1/phone-invitations/{token}:") || !strings.Contains(body, "/api/v1/phone-provisioning/{token}:") || strings.Contains(body, "ringring_session") {
		t.Fatal("served OpenAPI contract was missing, stale, or contained an application session field")
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
		RadioEnabled: true, RadioStation: "drone-zone", AIEnabled: true, AIAdultOnlyEnabled: true, UpdatedAt: now,
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
		t.Fatal("successful claim advertised the AI line while the operator adults-only gate was closed")
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
	settingsPage := get(t, client, created.Request.URL.String()+"/settings")
	settingsBody := readBody(t, settingsPage)
	for _, stationID := range []string{"groove-salad", "drone-zone", "deep-space-one"} {
		if !strings.Contains(settingsBody, `value="`+stationID+`"`) {
			t.Fatalf("party settings missing catalog station %q", stationID)
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
		"csrf": {csrf}, "ai_enabled": {"1"},
	})
	if lockedAI.StatusCode != http.StatusConflict || !strings.Contains(readBody(t, lockedAI), "server operator") {
		t.Fatal("AI line was not held behind the operator adult-only gate")
	}
	lockedPartyPage := get(t, client, server.URL+"/parties/"+partyID+"/settings")
	lockedPartyBody := readBody(t, lockedPartyPage)
	if !strings.Contains(lockedPartyBody, "server operator has not opened the adults-only preview") || !strings.Contains(lockedPartyBody, `name="ai_enabled" value="1"  disabled`) {
		t.Fatal("party page did not explain or disable the closed AI conversation gate")
	}
	app.cfg.AIAdultOnlyEnabled = true
	noAdultAI := postForm(t, client, server.URL+"/parties/"+partyID+"/services", url.Values{
		"csrf": {csrf}, "ai_enabled": {"1"},
	})
	if noAdultAI.StatusCode != http.StatusConflict || !strings.Contains(readBody(t, noAdultAI), "adult extension") {
		t.Fatal("AI line was enabled without an adult extension")
	}
	devHost, err := database.UpsertGoogleUser(t.Context(), store.GoogleProfile{Subject: "dev:host@example.test", Email: "host@example.test", Name: "Local Host"}, fixedNow, "unused")
	if err != nil {
		t.Fatal(err)
	}
	adultToken := strings.Repeat("a", 43)
	if err := database.CreateInvitation(t.Context(), store.NewInvitation{
		ID: "inv_adult_service", PartyID: partyID, CreatedByUserID: devHost.ID, TokenHash: secure.Hash(adultToken),
		ExpiresAt: fixedNow.Add(time.Hour), CreatedAt: fixedNow,
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := database.ClaimInvitation(t.Context(), store.NewClaim{
		TokenHash: secure.Hash(adultToken), MemberID: "mem_adult_service", DisplayName: "Adult phone", Extension: "700", AdultExtension: true,
		DeviceID: "dev_adult_service", DeviceLabel: "Adult ATA", SIPUsername: "654321", SIPSecretCiphertext: "encrypted",
		Provisioning: store.NewProvisioningToken{TokenHash: secure.Hash("adult-provision"), ExpiresAt: fixedNow.Add(time.Hour), CreatedAt: fixedNow}, Now: fixedNow,
	}); err != nil {
		t.Fatal(err)
	}
	geocoder := &fakeWeatherGeocoder{}
	app.weather = geocoder
	setupPage := postForm(t, client, server.URL+"/parties/"+partyID+"/services", url.Values{
		"csrf": {csrf}, "time_enabled": {"1"}, "weather_enabled": {"1"}, "radio_station": {"groove-salad"},
	})
	weatherSetupBody := readBody(t, setupPage)
	services, err := database.PartyServices(t.Context(), partyID)
	if setupPage.StatusCode != http.StatusOK || !strings.Contains(weatherSetupBody, "five-digit ZIP") || err != nil || !services.WeatherEnabled || !services.WeatherSetupAllowed {
		t.Fatalf("blank enabled weather did not preserve phone setup: status=%d services=%#v error=%v", setupPage.StatusCode, services, err)
	}
	routingServices, err := database.RoutingServices(t.Context())
	if err != nil || len(routingServices) != 1 || !routingServices[0].WeatherEnabled || !routingServices[0].WeatherSetupEnabled {
		t.Fatalf("enabled member weather did not expose authenticated routing: %#v error=%v", routingServices, err)
	}
	servicePage := postForm(t, client, server.URL+"/parties/"+partyID+"/services", url.Values{
		"csrf": {csrf}, "time_enabled": {"1"}, "weather_enabled": {"1"},
		"radio_enabled": {"1"}, "radio_station": {"drone-zone"}, "ai_enabled": {"1"},
	})
	serviceBody := readBody(t, servicePage)
	if servicePage.StatusCode != http.StatusOK || !strings.Contains(serviceBody, "Personal weather") || geocoder.query != "" {
		t.Fatalf("service settings were not saved independently of member weather: status=%d query=%q", servicePage.StatusCode, geocoder.query)
	}
	services, err = database.PartyServices(t.Context(), partyID)
	if err != nil {
		t.Fatal(err)
	}
	if !services.TimeEnabled || !services.WeatherEnabled || !services.RadioEnabled || services.RadioStation != "drone-zone" || !services.AIEnabled {
		t.Fatalf("unexpected service settings: %#v", services)
	}
	if !strings.Contains(serviceBody, `value="drone-zone" selected`) {
		t.Fatal("party page did not keep the selected radio station")
	}
	memberWeather := postForm(t, client, server.URL+"/parties/"+partyID+"/members/mem_adult_service/weather", url.Values{
		"csrf": {csrf}, "weather_query": {" Portland,   Maine "},
	})
	memberWeatherBody := readBody(t, memberWeather)
	if memberWeather.StatusCode != http.StatusOK || geocoder.query != "Portland, Maine" || !strings.Contains(memberWeatherBody, "Using Portland, Maine") {
		t.Fatalf("member weather was not resolved and rendered: status=%d query=%q", memberWeather.StatusCode, geocoder.query)
	}
	membersWithWeather, err := database.ListMembers(t.Context(), partyID)
	if err != nil || len(membersWithWeather) != 1 || membersWithWeather[0].Weather.Label != "Portland, Maine" {
		t.Fatalf("member weather location = %#v error=%v", membersWithWeather, err)
	}
	if err := database.DeleteMember(t.Context(), partyID, devHost.ID, "mem_adult_service"); err != nil {
		t.Fatal(err)
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
	routingServices, err = database.RoutingServices(t.Context())
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
	spendPage := get(t, client, server.URL+"/parties/"+partyID+"/settings")
	spendPageBody := readBody(t, spendPage)
	if spendPage.StatusCode != http.StatusOK || !strings.Contains(spendPageBody, "Monthly AI guardrail") || !strings.Contains(spendPageBody, "$10.00 each month") || !strings.Contains(spendPageBody, `max="10.00"`) {
		t.Fatal("party settings did not show the bounded existing spend limit")
	}
	cleanPhonebook := get(t, client, server.URL+"/parties/"+partyID)
	cleanPhonebookBody := readBody(t, cleanPhonebook)
	if cleanPhonebook.StatusCode != http.StatusOK || !strings.Contains(cleanPhonebookBody, "Party settings") || strings.Contains(cleanPhonebookBody, "Monthly AI guardrail") || strings.Contains(cleanPhonebookBody, "/openai-key/rotate") || strings.Contains(cleanPhonebookBody, `name="ai_enabled"`) {
		t.Fatal("AI administration remained mixed into the phone book")
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
	spendRetryPage := get(t, client, server.URL+"/parties/"+partyID+"/settings")
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
	retryPage := get(t, client, server.URL+"/parties/"+partyID+"/settings")
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
	if !strings.Contains(joinBody, `id="extension" name="extension" value="101"`) || !strings.Contains(joinBody, "RingRing suggested an available number") || !strings.Contains(joinBody, "Public emergency and crisis numbers are unavailable") || !strings.Contains(joinBody, "Adult extension (18+)") {
		t.Fatal("empty-party invitation did not prefill and explain a safe extension")
	}
	joinCSRF := firstMatch(t, joinBody, `name="csrf" value="([^"]+)"`)
	setup := postForm(t, client, inviteURL, url.Values{
		"csrf": {joinCSRF}, "display_name": {"Blue phone"}, "extension": {"101"}, "device_label": {"ATA"}, "adult_extension": {"1"},
	})
	setupBody := readBody(t, setup)
	if setup.StatusCode != http.StatusOK || !strings.Contains(setupBody, "You are extension 101") || !strings.Contains(setupBody, "sip.example.test") || !strings.Contains(setupBody, "Scan it with the <em>RingRing app.</em>") || !strings.Contains(setupBody, "Scan it with <em>Linphone.</em>") || !strings.Contains(setupBody, "One file. <em>RingRing ready.</em>") || !strings.Contains(setupBody, "/provision/wp826/") || !strings.Contains(setupBody, "Download WP826 setup file") || !strings.Contains(setupBody, "Maintenance → Upgrade and Provisioning → Config File") || !strings.Contains(setupBody, "data:image/png;base64,") || !strings.Contains(setupBody, `href="ringring://join?provision=`) || !strings.Contains(setupBody, `id="ios-provision-url"`) || !strings.Contains(setupBody, `id="phone-provision-url"`) || !strings.Contains(setupBody, `/api/v1/phone-provisioning/`) || !strings.Contains(setupBody, `href="/openapi.yaml"`) || !strings.Contains(setupBody, "Use only one setup URL") || !strings.Contains(setupBody, "href=\"sip-linphone:?linphone-fetch-config=http%3A%2F%2F") || strings.Contains(setupBody, "#ZgotmplZ") || !strings.Contains(setupBody, "Use the scanner inside RingRing") || !strings.Contains(setupBody, "Use Linphone’s scanner—not the regular Camera app") || !strings.Contains(setupBody, "TLS · port 5061") || !strings.Contains(setupBody, "UDP · port 5060") || !strings.Contains(setupBody, "TLS protects phone sign-in and call setup") || !strings.Contains(setupBody, "voice audio is still server-relayed RTP, not encrypted media") || !strings.Contains(setupBody, "Test both directions") || !strings.Contains(setupBody, "dial <strong>*10</strong>") || !strings.Contains(setupBody, "Pick a different extension by phone") || !strings.Contains(setupBody, "Dial <strong>*15</strong>") || !strings.Contains(setupBody, "press <strong>1</strong> to save") || !strings.Contains(setupBody, `data-copy-setup`) || !strings.Contains(setupBody, `data-copy-target="setup-password"`) || !strings.Contains(setupBody, "This copies the password too") || !strings.Contains(setupBody, "SIP user, user ID, authentication ID") || !strings.Contains(setupBody, "Do not forward router ports") || !strings.Contains(setupBody, "Digits only") || !strings.Contains(setupBody, "Copy uses the exact unspaced value") {
		t.Fatalf("setup response was not successful: status=%d", setup.StatusCode)
	}
	if !strings.Contains(setupBody, ">*14<") {
		t.Fatal("adult extension setup did not advertise the enabled adults-only AI line")
	}
	oldUsername := firstMatch(t, setupBody, `id="setup-username" data-setup-value="([1-9][0-9]{5})"`)
	oldPassword := firstMatch(t, setupBody, `id="setup-password" data-setup-value="([1-9][0-9]{11})"`)
	for _, guidance := range []string{
		"6 digits · no spaces", "12 digits · no spaces", "Phone keeps showing 401?", "The first 401 is the normal sign-in challenge",
		"for both SIP User ID and Authentication ID", "Use your extension only as the display number",
		"Grandstream HT801 V2", "Port Settings → FXS PORT → General Settings", "sip.example.test:5061",
		"SIP Authenticate ID", "SIP Authentication Password", "NAT Traversal", "Keep-alive", "SIP Registration",
		"Register Expiration", "5 minutes", "reconnect promptly after a RingRing server restart",
		"PCMU / G.711 μ-law", "via RTP (RFC2833)", "Authenticate Server Certificate chain",
		"Status → Port Status", "saved password as blank afterward", "HT80x V2 administration guide",
	} {
		if !strings.Contains(setupBody, guidance) {
			t.Errorf("setup card omitted authentication guidance %q", guidance)
		}
	}
	if !strings.Contains(setupBody, ">"+groupSetupDigits(oldUsername, 3)+"</strong>") || !strings.Contains(setupBody, ">"+groupSetupDigits(oldPassword, 4)+"</strong>") {
		t.Fatal("setup did not visually group the exact numeric credentials")
	}
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
	if !strings.Contains(hostPartyBody, "Quick dial") || !strings.Contains(hostPartyBody, "sound check") || !strings.Contains(hostPartyBody, "choose extension") || !strings.Contains(hostPartyBody, "*15") || !strings.Contains(hostPartyBody, "Dial 0 or *0") {
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
	callCounter := &fakeActiveConferences{}
	app.calls = callCounter
	hostLive := get(t, client, server.URL+"/parties/"+partyID+"/live")
	hostLiveBody := readBody(t, hostLive)
	if hostLive.StatusCode != http.StatusOK || !strings.HasPrefix(hostLive.Header.Get("Content-Type"), "text/html") ||
		!strings.Contains(hostLive.Header.Get("Cache-Control"), "no-store") || !strings.Contains(hostLiveBody, `id="phonebook-live"`) ||
		!strings.Contains(hostLiveBody, "Blue phone") || !strings.Contains(hostLiveBody, "No party calls right now") || strings.Contains(hostLiveBody, "site-header") || callCounter.count != 1 {
		t.Fatalf("authenticated live phonebook response was wrong: status=%d calls=%d\n%s", hostLive.StatusCode, callCounter.count, hostLiveBody)
	}
	outsiderLive := get(t, outsiderClient, server.URL+"/parties/"+partyID+"/live")
	if outsiderLive.StatusCode != http.StatusNotFound || callCounter.count != 1 {
		t.Fatalf("another host reached live call state: status=%d calls=%d", outsiderLive.StatusCode, callCounter.count)
	}
	_ = readBody(t, outsiderLive)
	outsiderSettings := get(t, outsiderClient, server.URL+"/parties/"+partyID+"/settings")
	if outsiderSettings.StatusCode != http.StatusNotFound {
		t.Fatalf("another host reached party settings: status=%d", outsiderSettings.StatusCode)
	}
	_ = readBody(t, outsiderSettings)
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
	onlineRendered := strings.Contains(hostPartyBody, `class="phone-chip presence-online"`) && strings.Contains(hostPartyBody, `class="device-status-pill presence-online"`)
	memberLabelRendered := strings.Contains(hostPartyBody, `class="member-status-pill presence-online"`) && strings.Contains(hostPartyBody, "At least one phone is online")
	unavailableRendered := strings.Contains(hostPartyBody, "Live phone status is temporarily unavailable")
	checklistRemoved := !strings.Contains(hostPartyBody, "Setup checklist") && !strings.Contains(hostPartyBody, "/readiness")
	phonebookHierarchyRendered := strings.Contains(hostPartyBody, `class="phonebook-heading"`) && strings.Contains(hostPartyBody, `class="phonebook-count"`) && strings.Contains(hostPartyBody, `class="member-card compact-member-card"`) && strings.Contains(hostPartyBody, `class="extension-tile"`) && strings.Contains(hostPartyBody, `class="member-settings"`) && strings.Contains(hostPartyBody, `Personal weather`) && !strings.Contains(hostPartyBody, `class="member-row"`)
	if presenceCounter.calls != 1 || !onlineRendered || !memberLabelRendered || unavailableRendered || !checklistRemoved || !phonebookHierarchyRendered {
		t.Fatalf("party live presence calls=%d online=%t member_label=%t unavailable=%t checklist_removed=%t hierarchy=%t", presenceCounter.calls, onlineRendered, memberLabelRendered, unavailableRendered, checklistRemoved, phonebookHierarchyRendered)
	}
	memberID := firstMatch(t, hostPartyBody, `/members/([^/]+)/devices`)
	addDevicePath := server.URL + "/parties/" + partyID + "/members/" + memberID + "/devices"
	if !strings.Contains(hostPartyBody, "Add another phone") ||
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
	secondUsername := firstMatch(t, addedPhoneBody, `id="setup-username" data-setup-value="([1-9][0-9]{5})"`)
	if secondUsername == oldUsername {
		t.Fatal("second phone reused the first phone's SIP username")
	}
	secondProvisionURL := firstMatch(t, addedPhoneBody, `value="(http://[^"]+/provision/linphone/[A-Za-z0-9_-]{43})"`)
	secondIOSProvisionURL := firstMatch(t, addedPhoneBody, `value="(http://[^"]+/provision/ios/[A-Za-z0-9_-]{43})"`)
	compatibilityProvision := get(t, client, secondIOSProvisionURL)
	compatibilityBody := readBody(t, compatibilityProvision)
	if compatibilityProvision.StatusCode != http.StatusOK || !strings.HasPrefix(compatibilityProvision.Header.Get("Content-Type"), "application/json") || !strings.Contains(compatibilityBody, `"version":1`) || !strings.Contains(compatibilityBody, `"extension":"101"`) {
		t.Fatalf("released iOS compatibility route failed: status=%d", compatibilityProvision.StatusCode)
	}
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
	callCounter.calls = []telephony.ActiveConference{{
		Name: "rrc-" + partyID + "-101", PartyID: partyID, JoinExtension: "101", Endpoints: []string{oldUsername, secondUsername},
	}}
	liveCallPhonebook := get(t, client, server.URL+"/parties/"+partyID+"/live")
	liveCallBody := readBody(t, liveCallPhonebook)
	if liveCallPhonebook.StatusCode != http.StatusOK || !strings.Contains(liveCallBody, "Party calls") ||
		!strings.Contains(liveCallBody, "Blue phone") || !strings.Contains(liveCallBody, "2 phones in this call") ||
		!strings.Contains(liveCallBody, "*16101") || !strings.Contains(liveCallBody, "joining member’s display name—not the call") ||
		!strings.Contains(liveCallBody, "No call audio, transcript, or call history is saved") {
		t.Fatalf("live conference was not rendered as a private joinable party call:\n%s", liveCallBody)
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
	if !strings.Contains(hostPartyBody, `action="/parties/`+partyID+`/devices/`+deviceID+`/ring-test"`) || !strings.Contains(hostPartyBody, `<button class="text-button" type="submit"`) || !strings.Contains(hostPartyBody, "Ring this phone</button>") || !strings.Contains(hostPartyBody, `aria-label="Settings for Blue phone"`) {
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
	if checkedPhone.StatusCode != http.StatusOK || !strings.Contains(checkedPhoneBody, "host-confirmed real-phone checks were saved") || strings.Contains(checkedPhoneBody, "Setup checklist") || strings.Contains(checkedPhoneBody, "3 of 3 complete") {
		t.Fatalf("phone readiness response was not successful: status=%d", checkedPhone.StatusCode)
	}
	rotated := postForm(t, client, server.URL+"/parties/"+partyID+"/devices/"+deviceID+"/rotate", url.Values{"csrf": {csrf}})
	rotatedBody := readBody(t, rotated)
	if rotated.StatusCode != http.StatusOK || !strings.Contains(rotatedBody, "Fresh phone settings") ||
		!strings.Contains(rotatedBody, "old username and password no longer work") || strings.Contains(rotatedBody, "first-call-card") {
		t.Fatalf("rotation setup response was not successful: status=%d", rotated.StatusCode)
	}
	freshUsername := firstMatch(t, rotatedBody, `id="setup-username" data-setup-value="([1-9][0-9]{5})"`)
	if freshUsername == oldUsername {
		t.Fatal("rotation did not replace the SIP username")
	}
	rotatedProvisionURL := firstMatch(t, rotatedBody, `value="(http://[^"]+/provision/linphone/[A-Za-z0-9_-]{43})"`)
	rotatedIOSProvisionURL := firstMatch(t, rotatedBody, `value="(http://[^"]+/provision/ios/[A-Za-z0-9_-]{43})"`)
	rotatedPhoneProvisionURL := firstMatch(t, rotatedBody, `value="(http://[^"]+/api/v1/phone-provisioning/[A-Za-z0-9_-]{43})"`)
	if rotatedProvisionURL == provisionURL {
		t.Fatal("rotation reused the prior provisioning link")
	}
	headPhoneRequest, err := http.NewRequest(http.MethodHead, rotatedPhoneProvisionURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	headPhoneProvision, err := client.Do(headPhoneRequest)
	if err != nil {
		t.Fatal(err)
	}
	_ = headPhoneProvision.Body.Close()
	if headPhoneProvision.StatusCode != http.StatusMethodNotAllowed || headPhoneProvision.Header.Get("Allow") != http.MethodGet {
		t.Fatalf("HEAD phone provisioning check consumed or accepted the link: status=%d", headPhoneProvision.StatusCode)
	}
	phoneProvisioned := get(t, client, rotatedPhoneProvisionURL)
	phoneProvisionedBody := readBody(t, phoneProvisioned)
	if phoneProvisioned.StatusCode != http.StatusOK || !strings.HasPrefix(phoneProvisioned.Header.Get("Content-Type"), "application/json") || phoneProvisioned.Header.Get("Cache-Control") != "no-store, max-age=0" || phoneProvisioned.Header.Get("Referrer-Policy") != "no-referrer" || phoneProvisioned.Header.Get("Cross-Origin-Resource-Policy") != "same-origin" || phoneProvisioned.Header.Get("Access-Control-Allow-Origin") != "" || !strings.Contains(phoneProvisioned.Header.Get("X-Robots-Tag"), "noindex") {
		t.Fatalf("phone API provisioning headers/status were unsafe: status=%d headers=%v", phoneProvisioned.StatusCode, phoneProvisioned.Header)
	}
	var phoneConfig provisioning.PhoneProvisioningDocument
	if err := json.Unmarshal([]byte(phoneProvisionedBody), &phoneConfig); err != nil {
		t.Fatal(err)
	}
	if phoneConfig.Version != provisioning.PhoneProvisioningVersion || phoneConfig.SIP.Server != "sip.example.test" || phoneConfig.SIP.Port != 5061 || phoneConfig.SIP.Transport != "tls" || phoneConfig.SIP.Username != freshUsername || phoneConfig.SIP.Extension != "101" || phoneConfig.SIP.Password == "" {
		t.Fatalf("unexpected phone provisioning document: %#v", phoneConfig)
	}
	if len(phoneConfig.Destinations) != 6 || phoneConfig.Destinations[0].Kind != "service" || phoneConfig.Destinations[0].Label != "Echo test" || phoneConfig.Destinations[0].Dial != "*10" {
		t.Fatalf("unexpected phone call menu: %#v", phoneConfig.Destinations)
	}
	for _, privateValue := range []string{"Blue phone", "Cousins Club", "ATA", "host@example.test", "proj_test"} {
		if strings.Contains(phoneProvisionedBody, privateValue) {
			t.Fatalf("phone provisioning exposed unrelated private data: %q", privateValue)
		}
	}
	if reusedIOSProvision := get(t, client, rotatedIOSProvisionURL); reusedIOSProvision.StatusCode != http.StatusGone {
		t.Fatalf("iOS compatibility route did not share one-time consumption: status=%d", reusedIOSProvision.StatusCode)
	} else {
		_ = readBody(t, reusedIOSProvision)
	}
	if reusedPhoneProvision := get(t, client, rotatedPhoneProvisionURL); reusedPhoneProvision.StatusCode != http.StatusGone || !strings.HasPrefix(reusedPhoneProvision.Header.Get("Content-Type"), "application/problem+json") {
		t.Fatalf("phone API provisioning link was reusable: status=%d content-type=%q", reusedPhoneProvision.StatusCode, reusedPhoneProvision.Header.Get("Content-Type"))
	} else {
		var problem apiProblem
		if err := json.Unmarshal([]byte(readBody(t, reusedPhoneProvision)), &problem); err != nil || problem.Status != http.StatusGone || problem.Type != "about:blank" || problem.Detail == "" {
			t.Fatalf("phone API did not return a bounded problem document: %#v error=%v", problem, err)
		}
	}
	if again := get(t, client, rotated.Request.URL.String()); again.StatusCode != http.StatusGone {
		t.Fatalf("setup reveal could be read twice: status=%d", again.StatusCode)
	} else {
		_ = readBody(t, again)
	}
	app.presence = fakeContactPresence{statuses: map[string]telephony.ContactState{freshUsername: telephony.ContactReachable}}
	hostParty = get(t, client, server.URL+"/parties/"+partyID)
	if strings.Contains(readBody(t, hostParty), "Setup checklist") {
		t.Fatal("removed setup checklist returned after credential rotation")
	}
	membersAfterRotation, err := database.ListMembers(t.Context(), partyID)
	rotationChecksCleared := false
	if err == nil && len(membersAfterRotation) == 1 {
		for _, device := range membersAfterRotation[0].Devices {
			if device.ID == deviceID {
				rotationChecksCleared = device.Readiness.CompletedCount() == 0
			}
		}
	}
	if !rotationChecksCleared {
		t.Fatal("credential rotation did not clear host-confirmed phone checks")
	}

	app.presence = fakeContactPresence{statuses: map[string]telephony.ContactState{freshUsername: telephony.ContactUnreachable}}
	hostParty = get(t, client, server.URL+"/parties/"+partyID)
	hostPartyBody = readBody(t, hostParty)
	if !strings.Contains(hostPartyBody, `class="device-status-pill presence-trouble"`) || !strings.Contains(hostPartyBody, "not reachable") {
		t.Fatal("party page did not distinguish an unreachable registered phone")
	}
	app.presence = fakeContactPresence{statuses: map[string]telephony.ContactState{freshUsername: telephony.ContactUnknown}}
	hostParty = get(t, client, server.URL+"/parties/"+partyID)
	checkingBody := readBody(t, hostParty)
	if !strings.Contains(checkingBody, `class="device-status-pill presence-checking"`) || !strings.Contains(checkingBody, "checking") {
		t.Fatal("party page did not distinguish a newly registered phone being checked")
	}
	app.presence = fakeContactPresence{statuses: map[string]telephony.ContactState{}}
	hostParty = get(t, client, server.URL+"/parties/"+partyID)
	waitingBody := readBody(t, hostParty)
	if !strings.Contains(waitingBody, `class="device-status-pill presence-waiting"`) || !strings.Contains(waitingBody, "not registered") {
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
	if revoked.StatusCode != http.StatusOK || !strings.Contains(revokedBody, `class="device-status-pill presence-off"`) || !strings.Contains(revokedBody, "disconnected") || strings.Contains(revokedBody, `class="device-status-pill presence-online"`) {
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

func TestInvitationAllowsOpaqueOriginOnlyWithMatchingCSRF(t *testing.T) {
	app := &App{cfg: config.Config{Environment: "production", BaseURL: "https://ringring.live"}}
	req := httptest.NewRequest(http.MethodPost, "https://ringring.live/join/example", nil)
	req.Header.Set("Origin", "null")
	if !app.invitationOriginOK(req, true) {
		t.Fatal("an opaque invitation form with its exact double-submit token must be accepted")
	}
	if app.invitationOriginOK(req, false) {
		t.Fatal("an opaque invitation form without its exact double-submit token must be rejected")
	}
	req.Header.Set("Origin", "https://attacker.example")
	if app.invitationOriginOK(req, true) {
		t.Fatal("a non-opaque cross-site invitation form must be rejected")
	}
	if app.sameOrigin(req) {
		t.Fatal("the host/admin same-origin policy must remain unchanged")
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
	iosProvisioning := httptest.NewRequest(http.MethodGet, "/provision/ios/provision-secret", nil)
	iosProvisioning.Pattern = "GET /provision/ios/{token}"
	if got := safeRoute(iosProvisioning); got != "/provision/ios/{token}" || requestSurface(iosProvisioning) != "provisioning" {
		t.Fatalf("safe iOS provisioning route = %q surface=%q", got, requestSurface(iosProvisioning))
	}
	wp826Provisioning := httptest.NewRequest(http.MethodGet, "/provision/wp826/provision-secret", nil)
	wp826Provisioning.Pattern = "GET /provision/wp826/{token}"
	if got := safeRoute(wp826Provisioning); got != "/provision/wp826/{token}" || requestSurface(wp826Provisioning) != "provisioning" {
		t.Fatalf("safe WP826 provisioning route = %q surface=%q", got, requestSurface(wp826Provisioning))
	}
	phoneProvisioning := httptest.NewRequest(http.MethodGet, "/api/v1/phone-provisioning/provision-secret", nil)
	phoneProvisioning.Pattern = "GET /api/v1/phone-provisioning/{token}"
	if got := safeRoute(phoneProvisioning); got != "/api/v1/phone-provisioning/{token}" || requestSurface(phoneProvisioning) != "provisioning" {
		t.Fatalf("safe phone API route = %q surface=%q", got, requestSurface(phoneProvisioning))
	}
	phoneInvitation := httptest.NewRequest(http.MethodGet, "/api/v1/phone-invitations/invitation-secret", nil)
	phoneInvitation.Pattern = "GET /api/v1/phone-invitations/{token}"
	if got := safeRoute(phoneInvitation); got != "/api/v1/phone-invitations/{token}" || requestSurface(phoneInvitation) != "invitation" {
		t.Fatalf("safe phone invitation API route = %q surface=%q", got, requestSurface(phoneInvitation))
	}
	openAPI := httptest.NewRequest(http.MethodGet, "/openapi.yaml", nil)
	openAPI.Pattern = "GET /openapi.yaml"
	if got := safeRoute(openAPI); got != "/openapi.yaml" || requestSurface(openAPI) != "documentation" {
		t.Fatalf("safe OpenAPI route = %q surface=%q", got, requestSurface(openAPI))
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
	phoneRequest := httptest.NewRequest(http.MethodGet, "/api/v1/phone-provisioning/provision-secret", nil)
	category, limit, window = rateCategory(phoneRequest)
	if category != "provision" || limit != 20 || window != 5*time.Minute {
		t.Fatalf("phone API rate category = %q %d %s", category, limit, window)
	}
	invitationRequest := httptest.NewRequest(http.MethodPost, "/api/v1/phone-invitations/invitation-secret", nil)
	category, limit, window = rateCategory(invitationRequest)
	if category != "join" || limit != 60 || window != 5*time.Minute {
		t.Fatalf("phone invitation API rate category = %q %d %s", category, limit, window)
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
