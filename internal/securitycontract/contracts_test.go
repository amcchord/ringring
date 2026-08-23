package securitycontract

import (
	"bufio"
	"crypto/sha256"
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate security contract test")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
}

func readRepositoryFile(t *testing.T, name string) string {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join(repositoryRoot(t), name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(contents)
}

// composePublishedPorts intentionally reads only the small, source-controlled
// Compose shape RingRing uses. It distinguishes host-published `ports` from
// private `expose` entries so a future public control-plane port fails CI.
func composePublishedPorts(t *testing.T, contents string) map[string][]string {
	t.Helper()
	result := make(map[string][]string)
	scanner := bufio.NewScanner(strings.NewReader(contents))
	service := ""
	inServices := false
	inPorts := false
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if indent == 0 {
			inServices = trimmed == "services:"
			service = ""
			inPorts = false
			continue
		}
		if !inServices {
			continue
		}
		if indent == 2 && strings.HasSuffix(trimmed, ":") {
			service = strings.TrimSuffix(trimmed, ":")
			result[service] = nil
			inPorts = false
			continue
		}
		if indent == 4 {
			inPorts = trimmed == "ports:"
			continue
		}
		if service != "" && inPorts && indent == 6 && strings.HasPrefix(trimmed, "- ") {
			result[service] = append(result[service], strings.Trim(strings.TrimPrefix(trimmed, "- "), "\"'"))
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestOnlyWebSIPAndRTPPortsArePublished(t *testing.T) {
	compose := readRepositoryFile(t, "compose.yaml")
	ports := composePublishedPorts(t, compose)
	want := map[string][]string{
		"app":      nil,
		"asterisk": {"5060:5060/udp", "5061:5061/tcp", "10000-10199:10000-10199/udp"},
		"caddy":    {"80:80", "443:443", "443:443/udp"},
	}
	if len(ports) != len(want) {
		t.Fatalf("Compose services = %#v, want exactly %#v", ports, want)
	}
	for service, expected := range want {
		got, ok := ports[service]
		if !ok {
			t.Fatalf("missing Compose service %q", service)
		}
		sort.Strings(got)
		sort.Strings(expected)
		if strings.Join(got, "\n") != strings.Join(expected, "\n") {
			t.Fatalf("published ports for %s = %#v, want %#v", service, got, expected)
		}
	}
	for _, forbidden := range []string{"network_mode: host", "privileged: true", "/var/run/docker.sock"} {
		if strings.Contains(compose, forbidden) {
			t.Fatalf("Compose contains forbidden privilege or host-boundary escape %q", forbidden)
		}
	}
}

func TestWebProxyCannotReachPrivateMetricsOrControlServices(t *testing.T) {
	caddy := readRepositoryFile(t, "deploy/Caddyfile")
	if strings.Count(caddy, "reverse_proxy") != 1 || !strings.Contains(caddy, "reverse_proxy app:8080") {
		t.Fatalf("Caddy must proxy only the public app listener:\n%s", caddy)
	}
	for _, forbidden := range []string{"9090", "4573", "4574", "5038", "asterisk:", "/metrics"} {
		if strings.Contains(caddy, forbidden) {
			t.Fatalf("Caddy exposes a private service marker %q", forbidden)
		}
	}

	manager := readRepositoryFile(t, "deploy/asterisk/config/manager.conf.in")
	for _, required := range []string{
		"webenabled=no",
		"deny=0.0.0.0/0.0.0.0",
		"permit=172.31.88.10/255.255.255.255",
		"read=system,command",
		"write=system,command,originate",
	} {
		if !strings.Contains(manager, required) {
			t.Fatalf("AMI boundary lost required rule %q", required)
		}
	}
	if strings.Contains(manager, "permit=0.0.0.0") {
		t.Fatal("AMI permits every source")
	}
	for _, forbidden := range []string{"read=all", "write=all", "config"} {
		if strings.Contains(strings.ToLower(manager), forbidden) {
			t.Fatalf("AMI grants an unnecessary broad permission %q", forbidden)
		}
	}
}

func TestCredentialCopyHelperIsIntegrityPinnedAndLocalOnly(t *testing.T) {
	javascript := readRepositoryFile(t, "web/static/site.js")
	digest := sha256.Sum256([]byte(javascript))
	integrity := "sha256-" + base64.StdEncoding.EncodeToString(digest[:])
	liveJavascript := readRepositoryFile(t, "web/static/party-live.js")
	liveDigest := sha256.Sum256([]byte(liveJavascript))
	liveIntegrity := "sha256-" + base64.StdEncoding.EncodeToString(liveDigest[:])
	base := readRepositoryFile(t, "web/templates/base.html")
	if strings.Count(base, "<script") != 2 || !strings.Contains(base, `src="/static/site.js" integrity="`+integrity+`"`) ||
		!strings.Contains(base, `{{if .PartyLiveURL}}<script defer src="/static/party-live.js" integrity="`+liveIntegrity+`"`) {
		t.Fatalf("browser helpers must be local, conditional, and integrity pinned: setup=%s live=%s", integrity, liveIntegrity)
	}
	app := readRepositoryFile(t, "internal/webapp/app.go")
	if !strings.Contains(app, `setupScriptSHA256`) || !strings.Contains(app, `"`+integrity+`"`) ||
		!strings.Contains(app, `partyLiveScriptSHA256`) || !strings.Contains(app, `"`+liveIntegrity+`"`) ||
		!strings.Contains(app, `script-src '"+setupScriptSHA256+"' '"+partyLiveScriptSHA256+"'`) {
		t.Fatal("the CSP must permit only the exact browser-helper digests")
	}
	for _, forbidden := range []string{
		"fetch(", "XMLHttpRequest", "sendBeacon", "WebSocket", "EventSource",
		"localStorage", "sessionStorage", "indexedDB", "document.cookie",
		"window.location", "console.",
	} {
		if strings.Contains(javascript, forbidden) {
			t.Fatalf("the credential copy helper contains transmission or persistence primitive %q", forbidden)
		}
	}
	for _, required := range []string{"navigator.clipboard.writeText", `document.execCommand("copy")`, `helper.remove()`, `data-copy-setup`, `data-setup-field`, `getAttribute("data-setup-value")`, "exactValue(value)"} {
		if !strings.Contains(javascript, required) {
			t.Fatalf("the credential copy helper is missing bounded behavior %q", required)
		}
	}
	for _, forbidden := range []string{
		"XMLHttpRequest", "sendBeacon", "WebSocket", "EventSource", "localStorage", "sessionStorage",
		"indexedDB", "document.cookie", "Authorization", "body:", `method: "POST"`, "console.",
	} {
		if strings.Contains(liveJavascript, forbidden) {
			t.Fatalf("the live phonebook helper contains an unnecessary data primitive %q", forbidden)
		}
	}
	for _, required := range []string{
		`document.getElementById("phonebook-live")`, `liveURL.startsWith("/parties/")`, `liveURL.endsWith("/live")`,
		"fetch(liveURL", `method: "GET"`, `credentials: "same-origin"`, `cache: "no-store"`, `redirect: "error"`,
		`headers: { Accept: "text/html" }`, `phonebook.contains(document.activeElement)`, `phonebook.querySelector("details[open]")`,
		"nextPhonebook.dataset.partyLiveUrl !== liveURL", "phonebook.replaceWith(nextPhonebook)", "window.setInterval(refresh, 3000)",
	} {
		if !strings.Contains(liveJavascript, required) {
			t.Fatalf("the live phonebook helper is missing bounded behavior %q", required)
		}
	}
}

func TestGeneratedSIPCredentialsStayKeypadFriendlyAndCollisionSafe(t *testing.T) {
	credentials := readRepositoryFile(t, "internal/sipcredentials/credentials.go")
	for _, required := range []string{
		"crypto/rand", "UsernameDigits = 6", "PasswordDigits = 12",
		"about 39.7 bits", "rand.Int(reader, span)",
	} {
		if !strings.Contains(credentials, required) {
			t.Fatalf("the generated SIP credential policy is missing %q", required)
		}
	}
	app := readRepositoryFile(t, "internal/webapp/app.go")
	for _, required := range []string{"sipcredentials.Generate()", "sipCredentialAttempts = 16", "store.ErrSIPUsernameTaken", "saveWithNewSIPCredentials"} {
		if !strings.Contains(app, required) {
			t.Fatalf("the web credential path is missing collision-safe behavior %q", required)
		}
	}
	if strings.Contains(app, `return "rrd_" + suffix`) {
		t.Fatal("the web credential path is not using the digits-only generator")
	}
	storeSource := readRepositoryFile(t, "internal/store/store.go")
	for _, required := range []string{"ErrSIPUsernameTaken", "isSIPUsernameConflict", "devices.sip_username"} {
		if !strings.Contains(storeSource, required) {
			t.Fatalf("the SIP username uniqueness boundary is missing %q", required)
		}
	}
	setup := readRepositoryFile(t, "web/templates/setup.html")
	for _, required := range []string{`data-setup-value="{{.Claim.Device.SIPUsername}}"`, `data-setup-value="{{.Claim.SIPSecret}}"`, "6 digits · no spaces", "12 digits · no spaces", "Digits only—no spaces", "The first 401 is the normal sign-in challenge", "for both SIP User ID and Authentication ID"} {
		if !strings.Contains(setup, required) {
			t.Fatalf("the one-time setup card is missing exact-value guidance %q", required)
		}
	}
	smoke := readRepositoryFile(t, "scripts/sip-smoke.sh")
	for _, required := range []string{`grep -Eq '^[1-9][0-9]{5}$'`, `grep -Eq '^[1-9][0-9]{11}$'`} {
		if !strings.Contains(smoke, required) {
			t.Fatalf("the SIP gate is not authenticating the current numeric format %q", required)
		}
	}
}

func TestGrandstreamGuidePreservesCredentialAndRoutingBoundaries(t *testing.T) {
	setup := readRepositoryFile(t, "web/templates/setup.html")
	start := strings.Index(setup, `<section class="device-specific-guide"`)
	end := strings.Index(setup, `<section class="field-translator"`)
	if start < 0 || end <= start {
		t.Fatal("the credential-aware Grandstream guide is missing or misplaced")
	}
	guide := setup[start:end]
	for _, required := range []string{
		`{{.SIPPublicHost}}:5061`, `{{.SIPPublicHost}}:5060`, "SIP Authenticate ID", "SIP Authentication Password",
		"The same <strong>Username / auth ID</strong>", "Outbound Proxy", "Leave blank", "NAT Traversal", "Keep-alive",
		"SIP Registration", "TLS", "PCMU / G.711 μ-law", "via RTP (RFC2833)", "Authenticate Server Certificate chain",
		"One initial 401 is normal", "Save and Apply", "Status → Port Status", "dial <strong>*10</strong>",
	} {
		if !strings.Contains(guide, required) {
			t.Errorf("the Grandstream guide is missing safe setup marker %q", required)
		}
	}
	for _, forbidden := range []string{`{{.Claim.Device.SIPUsername}}`, `{{.Claim.SIPSecret}}`, "port forward", "factory reset"} {
		if strings.Contains(guide, forbidden) {
			t.Errorf("the Grandstream guide duplicates a secret or unsafe operation %q", forbidden)
		}
	}
}

func TestWP826DownloadKeepsTheOneTimeMinimalConfigBoundary(t *testing.T) {
	required := map[string][]string{
		"internal/webapp/app.go": {
			`GET /provision/wp826/{token}`, "ConsumeProvisioningToken", "provisioning.WP826XML",
			`attachment; filename="ringring-wp826.xml"`, `AssetBaseURL: a.cfg.BaseURL + "/static/wp826"`,
		},
		"internal/provisioning/wp826.go": {
			`Version: 2`, `item("account.1"`, `part("transport", "Tls Or Tcp")`,
			`part("certificationChain", "Yes")`, `part("domainCertificates", "Yes")`,
			`part("numberOfRingtone", "4")`, `ringring-memphis-day.png`,
			`Custom-Contacts,Custom-History,Custom-Menu`,
		},
		"web/templates/setup.html": {
			`href="{{.WP826ProvisionURL}}"`, "Download WP826 setup file", "Use only one setup link.",
			"The XML contains this phone’s SIP password", "then delete the file", "Upload Device Configuration",
		},
		"docs/SECURITY.md": {
			"downloadable WP826 XML", "cannot carry Wi-Fi, network, administrator", "delete it afterward",
		},
	}
	for filename, markers := range required {
		contents := readRepositoryFile(t, filename)
		for _, marker := range markers {
			if !strings.Contains(contents, marker) {
				t.Errorf("%s is missing WP826 boundary %q", filename, marker)
			}
		}
	}

	renderer := readRepositoryFile(t, "internal/provisioning/wp826.go")
	for _, forbidden := range []string{"wifi", "administrator.password", "network.ipv4", "account.2", "party.Name", "MemberID"} {
		if strings.Contains(renderer, forbidden) {
			t.Errorf("WP826 renderer contains out-of-scope setting or private field %q", forbidden)
		}
	}
}

func TestAsteriskHasNoPSTNOrGlobalOutboundRoute(t *testing.T) {
	pjsip := readRepositoryFile(t, "deploy/asterisk/config/pjsip.conf.in")
	if !strings.Contains(pjsip, "endpoint_identifier_order=auth_username,username,ip,anonymous") {
		t.Fatal("PJSIP must identify an authenticated username before address-based fallbacks")
	}
	if strings.Count(pjsip, "#include") != 1 || !strings.Contains(pjsip, "#include /var/lib/ringring/asterisk/pjsip.conf") {
		t.Fatal("base PJSIP configuration must include only RingRing-generated endpoints")
	}
	for _, forbidden := range []string{"type=registration", "outbound_auth=", "outbound_proxy=", "type=identify", "trunk"} {
		if strings.Contains(strings.ToLower(pjsip), forbidden) {
			t.Fatalf("base PJSIP configuration contains outbound-provider marker %q", forbidden)
		}
	}

	dialplan := readRepositoryFile(t, "deploy/asterisk/config/extensions.conf")
	if strings.Count(dialplan, "#include") != 1 || !strings.Contains(dialplan, "#include /var/lib/ringring/asterisk/extensions.conf") {
		t.Fatal("base dialplan must include only the RingRing-generated party dialplan")
	}
	for _, forbidden := range []string{"Dial(", "SIP/", "IAX2/", "DAHDI/", "Local/", "Goto("} {
		if strings.Contains(dialplan, forbidden) {
			t.Fatalf("base dialplan contains routing primitive %q", forbidden)
		}
	}
}

func TestPartyCallConferencesAreScopedEphemeralAndNeverRecorded(t *testing.T) {
	confbridge := readRepositoryFile(t, "deploy/asterisk/config/confbridge.conf")
	for _, required := range []string{
		"[ringring_bridge]", "max_members=20", "record_conference=no", "[ringring_initial]", "marked=yes",
		"end_marked_any=yes", "[ringring_joiner]", "marked=no", "[ringring_announcer]", "quiet=yes", "jitterbuffer=yes",
	} {
		if !strings.Contains(confbridge, required) {
			t.Errorf("ConfBridge policy is missing %q", required)
		}
	}
	for _, forbidden := range []string{"record_file", "record_command", "menu=", "dsp_drop_silence=yes"} {
		if strings.Contains(confbridge, forbidden) {
			t.Errorf("ConfBridge policy contains retention or unexpected behavior %q", forbidden)
		}
	}

	dialplan := readRepositoryFile(t, "deploy/asterisk/config/extensions.conf")
	for _, required := range []string{
		"[rr-party-bridge]", "ConfBridge(${RINGRING_CONFERENCE},ringring_bridge,ringring_initial)",
		"[rr-party-announcement]", "Set(TIMEOUT(absolute)=15)", "ConfBridge(${RINGRING_CONFERENCE},ringring_bridge,ringring_announcer)",
	} {
		if !strings.Contains(dialplan, required) {
			t.Errorf("fixed conference contexts are missing %q", required)
		}
	}
	for _, forbidden := range []string{"Dial(", "AGI(", "AudioSocket", "System("} {
		if strings.Contains(dialplan, forbidden) {
			t.Errorf("fixed base conference dialplan contains unsafe primitive %q", forbidden)
		}
	}

	ami := readRepositoryFile(t, "internal/telephony/ami.go")
	for _, required := range []string{
		"ParseConferenceName(conference)", `"Channel", "Local/s@rr-party-announcement/n"`, `"Application", "Playback"`,
		`"Variable", "RINGRING_CONFERENCE="+conference`, "ConfbridgeListRooms", "pjsipEndpoint", "len(room.Endpoints) >= 2",
	} {
		if !strings.Contains(ami, required) {
			t.Errorf("AMI conference boundary is missing %q", required)
		}
	}
	voice := readRepositoryFile(t, "internal/voice/join.go")
	for _, required := range []string{
		"PartyMemberForDevice", "conferenceParty != partyID", "Ring ring! %s is joining the party.",
		"secure.Token(18)", "defaultJoinAudioTTL", "os.Remove(audioPath)", `playback = "beep"`,
	} {
		if !strings.Contains(voice, required) {
			t.Errorf("party join announcement boundary is missing %q", required)
		}
	}
}

func TestAIConversationRequiresAdultExtensionAtEveryBoundary(t *testing.T) {
	required := map[string][]string{
		".env.example":                      {"AI_ADULT_ONLY_ENABLED=false"},
		"ringringctl":                       {"AI_ADULT_ONLY_ENABLED=false", "AI_ADULT_ONLY_ENABLED must be true or false", "ringring verify-openai-retention"},
		"cmd/ringring/main.go":              {"EnforceAIAdultOnlyGate", "AIAdultAccess: database", "AIAdultOnlyEnabled: cfg.AIAdultOnlyEnabled"},
		"internal/config/config.go":         {`envStrictBool("AI_ADULT_ONLY_ENABLED", false)`},
		"internal/openaiadmin/client.go":    {"/organization/data_retention", "/data_retention", "organization_default", "none", "zero_data_retention", "enhanced_zero_data_retention", "has not enabled Zero Data Retention"},
		"internal/store/store.go":           {"ErrAIAdultOnly", "input.AIEnabled && !input.AIAdultOnlyEnabled", "EnforceAIAdultOnlyGate", "m.adult_extension = 1", "d.revoked_at IS NULL"},
		"internal/telephony/reconciler.go":  {"!r.AIAdultOnlyEnabled", "services[index].AIEnabled = false"},
		"internal/telephony/render.go":      {"${CHANNEL(endpoint)}", "RINGRING_AI_DENIED"},
		"internal/voice/ai.go":              {"!s.AIAdultOnlyEnabled", "AI conversation adult-only gate is closed", "AIAdultAccessForDevice"},
		"internal/webapp/app.go":            {"aiEnabled && !a.cfg.AIAdultOnlyEnabled", `r.FormValue("adult_extension")`, "AdultExtension: adultExtension"},
		"scripts/restore-drill.sh":          {"--env AI_ADULT_ONLY_ENABLED=false"},
		"web/templates/join.html":           {`name="adult_extension"`, "Adult extension (18+)"},
		"web/templates/party_settings.html": {"$conversationReady", "Only adult extensions can call", "provider retention may apply"},
	}
	for filename, markers := range required {
		contents := readRepositoryFile(t, filename)
		for _, marker := range markers {
			if !strings.Contains(contents, marker) {
				t.Errorf("%s is missing adult-extension boundary %q", filename, marker)
			}
		}
	}
	if strings.Contains(readRepositoryFile(t, "internal/voice/ai.go"), "WAIT FOR DIGIT 8000") {
		t.Fatal("adult extension authorization must not add a repeated keypad confirmation")
	}
}

func TestHostPhoneRingHasFixedScopedBoundaries(t *testing.T) {
	dialplan := readRepositoryFile(t, "deploy/asterisk/config/extensions.conf")
	for _, marker := range []string{
		"[rr-phone-check]", "Set(CDR_PROP(disable)=1)", "Set(TIMEOUT(absolute)=20)",
		"Playback(hello)", "Playback(your)", "Playback(extension)", "Playback(is)",
		"SayDigits(${RINGRING_EXTENSION})", "Playback(auth-thankyou)",
	} {
		if !strings.Contains(dialplan, marker) {
			t.Errorf("internal phone-check context is missing %q", marker)
		}
	}
	for _, forbidden := range []string{"Dial(", "Goto(", "AGI(", "AudioSocket", "System("} {
		if strings.Contains(dialplan, forbidden) {
			t.Errorf("base phone-check dialplan contains unsafe primitive %q", forbidden)
		}
	}
	required := map[string][]string{
		"deploy/asterisk/Dockerfile": {
			"find /etc/asterisk -maxdepth 1 -type f -exec chown asterisk:ringring",
			"find /etc/asterisk -maxdepth 1 -type f -exec chmod 0640",
		},
		"deploy/asterisk/entrypoint.sh": {
			"chmod 0640 /etc/asterisk/manager.conf /etc/asterisk/pjsip.conf /etc/asterisk/extensions.conf /etc/asterisk/confbridge.conf",
			"chown asterisk:ringring /etc/asterisk/manager.conf /etc/asterisk/pjsip.conf /etc/asterisk/extensions.conf /etc/asterisk/confbridge.conf",
		},
		"internal/telephony/ami.go": {
			`amiObjectPattern.MatchString(sipUsername)`, `extensionrules.Valid(extension)`,
			`"Channel", "PJSIP/"+sipUsername`, `"Context", "rr-phone-check"`, `"Async", "true"`,
		},
		"internal/store/store.go": {
			"ActiveDeviceForHost", "p.host_user_id = ?", "d.revoked_at IS NULL",
		},
		"internal/webapp/app.go": {
			"ActiveDeviceForHost", `a.phoneRings.allow("device:"+device.ID`,
			"state != telephony.ContactReachable", "a.ringer.RingDevice",
		},
		"web/templates/party.html": {`<span aria-hidden="true">📳</span> Ring this phone`, "/ring-test"},
		"ringringctl":              {"dialplan show s@rr-phone-check", "fixed incoming phone-check context is unavailable"},
	}
	for filename, markers := range required {
		contents := readRepositoryFile(t, filename)
		for _, marker := range markers {
			if !strings.Contains(contents, marker) {
				t.Errorf("%s is missing scoped phone-ring marker %q", filename, marker)
			}
		}
	}
}

func TestHostAddedPhonesStayBoundedToOnePartyMember(t *testing.T) {
	required := map[string][]string{
		"internal/store/store.go": {
			"const MaxDevicesPerMember = 8", "AddDeviceForHost", "WHERE id = ? AND host_user_id = ?",
			"FROM members WHERE id = ? AND party_id = ?", "SELECT COUNT(*) FROM devices WHERE member_id = ?",
			"replaceProvisioningTokenTx(ctx, tx, input.DeviceID, input.Provisioning)",
		},
		"internal/webapp/app.go": {
			`POST /parties/{partyID}/members/{memberID}/devices`, "addMemberDevice",
			"a.parseSmallForm(w, r)", "a.validCSRF(r, session)", "store.NewHostedDevice{",
			"telephony reconcile after device creation", "NewDevice: true",
		},
		"internal/telephony/render.go": {
			`parties[contextName][device.Extension] = append`, `strings.Join(endpoints, "&")`,
		},
		"web/templates/party.html": {
			"Add another phone", "/members/{{$member.ID}}/devices", "calls to this extension ring them together",
		},
		"web/templates/setup.html": {"Another phone ready", "Existing phones stay connected"},
	}
	for filename, markers := range required {
		contents := readRepositoryFile(t, filename)
		for _, marker := range markers {
			if !strings.Contains(contents, marker) {
				t.Errorf("%s is missing same-extension phone boundary %q", filename, marker)
			}
		}
	}
}

func TestMemberWeatherStaysBoundedToAuthenticatedExtensionAndHostParty(t *testing.T) {
	required := map[string][]string{
		"internal/store/store.go": {
			"WeatherLocationForDevice", "d.sip_username = ? AND d.revoked_at IS NULL",
			"TRIM(weather_label) = ''", "weather_enabled = 1 AND weather_setup_allowed = 1",
			"UpdateMemberWeatherLocationForHost", "host_user_id = ?",
		},
		"internal/voice/server.go": {
			"WeatherLocationForDevice(ctx, partyID, endpoint)", "location.MemberID",
			"weather-v2-", "save the place for this extension",
		},
		"internal/webapp/app.go": {
			`POST /parties/{partyID}/members/{memberID}/weather`, "updateMemberWeather",
			"a.parseSmallForm(w, r)", "a.validCSRF(r, session)", "UpdateMemberWeatherLocationForHost",
		},
		"internal/webapp/ratelimit.go": {`return "member-weather", 30, 5 * time.Minute`},
		"web/templates/party.html": {
			"Personal weather", "/members/{{.ID}}/weather", "uses this place only for",
		},
	}
	for filename, markers := range required {
		contents := readRepositoryFile(t, filename)
		for _, marker := range markers {
			if !strings.Contains(contents, marker) {
				t.Errorf("%s is missing member-weather boundary %q", filename, marker)
			}
		}
	}
}

func TestInvitationQRCodeStaysInsideTheOneTimeHostReveal(t *testing.T) {
	required := map[string][]string{
		"internal/webapp/app.go": {
			"data.InviteURL = a.readInviteFlash(w, r, party.ID)",
			"provisioning.QRCodeDataURI(data.InviteURL)",
			"data.InviteQR = template.URL(inviteQR)",
		},
		"web/templates/party.html": {
			`class="invite-qr"`, `src="{{.InviteQR}}"`, "Or scan it.",
			"Anyone with the link or code can claim this invitation",
		},
		"internal/provisioning/qr.go": {
			"deliberately has no network writer or external rendering service",
			`return "data:image/png;base64,"`,
		},
	}
	for filename, markers := range required {
		contents := readRepositoryFile(t, filename)
		for _, marker := range markers {
			if !strings.Contains(contents, marker) {
				t.Errorf("%s is missing local invitation QR boundary %q", filename, marker)
			}
		}
	}
	qr := readRepositoryFile(t, "internal/provisioning/qr.go")
	for _, forbidden := range []string{"net/http", "http.Get", "http.Post", "qrserver", "quickchart", "googleapis"} {
		if strings.Contains(qr, forbidden) {
			t.Errorf("local QR renderer contains network/provider primitive %q", forbidden)
		}
	}
}

func TestPhoneProvisioningAPIKeepsTheOneTimePartyBoundary(t *testing.T) {
	required := map[string][]string{
		"internal/webapp/app.go": {
			`GET /openapi.yaml`, `GET /api/v1/phone-provisioning/{token}`, `GET /provision/ios/{token}`,
			"phoneProvisioningDocument", "ConsumeProvisioningToken", "device.PartyID", "device.MemberID",
			`w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")`, `Type: "about:blank"`,
		},
		"internal/webapp/ratelimit.go": {
			`strings.HasPrefix(r.URL.Path, "/api/v1/phone-provisioning/")`, `return "provision", 20, 5 * time.Minute`,
		},
		"internal/provisioning/phone.go": {
			"PhoneProvisioningVersion = 1", "len(destinations) > 128", "seenDialTargets",
			"validatePhoneDestination", "serviceExtensionPattern", "PhoneOpenAPI",
		},
		"internal/provisioning/phone-provisioning.openapi.yaml": {
			"openapi: 3.1.2", "/api/v1/phone-provisioning/{token}:", "application/problem+json:",
			"no-store, max-age=0", "maxItems: 128", "never provides a PSTN route",
		},
		"web/templates/setup.html": {
			`id="phone-provision-url"`, "Use only one setup URL", `href="/openapi.yaml"`,
			"Never test or preview a real URL in a browser",
		},
		"docs/PHONE_API.md": {
			"disable redirects", "cap the body at 256 KiB", "device-protected secret storage",
			"do not add PSTN, emergency, trunk", "setup-time snapshot, not live presence",
		},
	}
	for filename, markers := range required {
		contents := readRepositoryFile(t, filename)
		for _, marker := range markers {
			if !strings.Contains(contents, marker) {
				t.Errorf("%s is missing phone API boundary %q", filename, marker)
			}
		}
	}

	app := readRepositoryFile(t, "internal/webapp/app.go")
	credentialStart := strings.Index(app, "func (a *App) phoneProvisionAPI")
	credentialEnd := strings.Index(app, "func (a *App) iosProvisionCompatibility")
	if credentialStart < 0 || credentialEnd <= credentialStart {
		t.Fatal("could not isolate the canonical phone provisioning handler")
	}
	if strings.Contains(app[credentialStart:credentialEnd], "Access-Control-Allow-Origin") {
		t.Fatal("credential responses must not opt into browser CORS")
	}

	spec := readRepositoryFile(t, "internal/provisioning/phone-provisioning.openapi.yaml")
	for _, forbidden := range []string{"email:", "party_id", "member_id", "device_id", "openai", "presence:"} {
		if strings.Contains(strings.ToLower(spec), forbidden) {
			t.Errorf("OpenAPI contract exposes forbidden private field marker %q", forbidden)
		}
	}
}

func TestHostCanCancelOnlyActiveInvitationsInsideOwnedParty(t *testing.T) {
	required := map[string][]string{
		"internal/store/store.go": {
			"ActiveInvitationCountForHost", "CancelActiveInvitationsForHost",
			"WHERE p.id = ? AND p.host_user_id = ?", "SELECT 1 FROM parties WHERE id = ? AND host_user_id = ?",
			"WHERE party_id = ? AND used_at IS NULL AND expires_at >= ?",
		},
		"internal/webapp/app.go": {
			`POST /parties/{partyID}/invites/cancel`, "cancelInvitations",
			"a.parseSmallForm(w, r)", "a.validCSRF(r, session)",
			"CancelActiveInvitationsForHost", "a.clearCookie(w, inviteFlashCookie",
		},
		"internal/webapp/ratelimit.go": {`strings.Contains(r.URL.Path, "/invites/")`},
		"web/templates/party.html": {
			"Manage unused", "/invites/cancel", "Cancel unused", "Used invitations and members are not changed.",
		},
	}
	for filename, markers := range required {
		contents := readRepositoryFile(t, filename)
		for _, marker := range markers {
			if !strings.Contains(contents, marker) {
				t.Errorf("%s is missing active-invitation cancellation boundary %q", filename, marker)
			}
		}
	}
}

func TestPrivateFirstCallCardStaysInsideSuccessfulMemberSetup(t *testing.T) {
	app := readRepositoryFile(t, "internal/webapp/app.go")
	for _, marker := range []string{
		"type callDirectoryEntry struct", "DisplayName string", "Extension   string",
		"directoryMembers, err := a.store.ListMembers", "data.CallDirectory = privateCallDirectory(directoryMembers)",
		"device.RevokedAt == nil", "availableFirstCallLines(party, services, a.cfg.AIAdultOnlyEnabled, member.AdultExtension)",
		`services.AIEnabled && voiceReady && adultOnlyEnabled && memberAdultAllowed`,
	} {
		if !strings.Contains(app, marker) {
			t.Errorf("internal/webapp/app.go is missing private first-call boundary %q", marker)
		}
	}

	setup := readRepositoryFile(t, "web/templates/setup.html")
	for _, marker := range []string{
		`{{if not .SetupForHost}}`, `class="first-call-card"`, `{{range .CallDirectory}}`,
		`{{.Extension}}`, `{{.DisplayName}}`, `{{range .FirstCallLines}}`, `Private snapshot.`,
	} {
		if !strings.Contains(setup, marker) {
			t.Errorf("web/templates/setup.html is missing private first-call boundary %q", marker)
		}
	}
}

func TestNATSmokeWaitsForBothDisposableHouseholdPaths(t *testing.T) {
	smoke := readRepositoryFile(t, "scripts/nat-smoke.sh")
	for _, marker := range []string{
		`chmod 0555 "$work_directory/state"`, "pjsip show endpoint rr_smoke_b", "paths_ready=0",
		"ip netns exec home-a ping", "ip netns exec home-b ping",
		"The isolated household paths did not reach Asterisk.",
	} {
		if !strings.Contains(smoke, marker) {
			t.Errorf("scripts/nat-smoke.sh is missing readiness boundary %q", marker)
		}
	}
	linphone := readRepositoryFile(t, "scripts/linphone-smoke.sh")
	for _, marker := range []string{
		`chmod 0555 "$work_directory/state" "$work_directory/provision" "$work_directory/certs"`,
		`chmod 0400 "$work_directory/certs/ca.key" "$work_directory/certs/private-key.pem"`,
	} {
		if !strings.Contains(linphone, marker) {
			t.Errorf("scripts/linphone-smoke.sh is missing dropped-UID bind boundary %q", marker)
		}
	}
}
