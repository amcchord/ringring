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
	createAttempts  int
	createdKey      openaiadmin.ServiceAccountAPIKey
	createErr       error
	listAttempts    int
	keyIDs          []string
	listErr         error
	deleteAttempts  int
	deletedKeys     []string
	deleteFailures  int
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
	if err := database.UpdatePartyOpenAI(t.Context(), partyID, "proj_test", "svc_test", "key_old", initialPartyKey, "ready"); err != nil {
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
		keyIDs:     []string{"key_old", "key_fresh"}, deleteFailures: 1,
	}
	app.openAI = keyManager
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
	if setup.StatusCode != http.StatusOK || !strings.Contains(setupBody, "You are extension 101") || !strings.Contains(setupBody, "sip.example.test") || !strings.Contains(setupBody, "Scan it with <em>Linphone.</em>") || !strings.Contains(setupBody, "data:image/png;base64,") || !strings.Contains(setupBody, "href=\"sip-linphone:?linphone-fetch-config=http%3A%2F%2F") || strings.Contains(setupBody, "#ZgotmplZ") || !strings.Contains(setupBody, "Use Linphone’s scanner—not the regular Camera app") || !strings.Contains(setupBody, "Test both directions") || !strings.Contains(setupBody, "dial <strong>*10</strong>") || !strings.Contains(setupBody, "Pick a different extension by phone") || !strings.Contains(setupBody, "Dial <strong>*15</strong>") || !strings.Contains(setupBody, "press <strong>1</strong> to save") {
		t.Fatalf("setup response was not successful: status=%d", setup.StatusCode)
	}
	oldUsername := firstMatch(t, setupBody, `(rrd_[A-Za-z0-9_-]+)`)
	oldPassword := firstMatch(t, setupBody, `<div class="secret-value"><span>Password</span><strong>([^<]+)</strong></div>`)
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
	if !strings.Contains(provisionedBody, `<entry name="username" overwrite="true">`+oldUsername+`</entry>`) || !strings.Contains(provisionedBody, `<entry name="passwd" overwrite="true">`+oldPassword+`</entry>`) || !strings.Contains(provisionedBody, `&lt;sip:sip.example.test:5060;transport=udp&gt;`) || !strings.Contains(provisionedBody, `RingRing 101`) {
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
	rotatedProvisionURL := firstMatch(t, rotatedBody, `value="(http://[^"]+/provision/linphone/[A-Za-z0-9_-]{43})"`)
	if rotatedProvisionURL == provisionURL {
		t.Fatal("rotation reused the prior provisioning link")
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
	if got := safePath("/join/invitation-secret"); got != "/join/:token" {
		t.Fatalf("safe invitation path = %q", got)
	}
	if got := safePath("/provision/linphone/provision-secret"); got != "/provision/:kind/:token" {
		t.Fatalf("safe provisioning path = %q", got)
	}
	req := httptest.NewRequest(http.MethodGet, "/provision/linphone/provision-secret", nil)
	category, limit, window := rateCategory(req)
	if category != "provision" || limit != 20 || window != 5*time.Minute {
		t.Fatalf("provisioning rate category = %q %d %s", category, limit, window)
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
