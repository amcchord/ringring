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
	base := readRepositoryFile(t, "web/templates/base.html")
	if strings.Count(base, "<script") != 1 || !strings.Contains(base, `src="/static/site.js" integrity="`+integrity+`"`) {
		t.Fatalf("the only browser helper must be the integrity-pinned local setup script: %s", integrity)
	}
	app := readRepositoryFile(t, "internal/webapp/app.go")
	if !strings.Contains(app, `setupScriptSHA256`) || !strings.Contains(app, `"`+integrity+`"`) || !strings.Contains(app, `script-src '"+setupScriptSHA256+"'`) {
		t.Fatal("the CSP must permit only the exact setup-script digest")
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
}

func TestGeneratedSIPCredentialsStayKeypadFriendlyAndStrong(t *testing.T) {
	credentials := readRepositoryFile(t, "internal/sipcredentials/credentials.go")
	for _, required := range []string{
		"crypto/rand", "UsernameDigits = 15", "PasswordDigits = 24",
		"about 49.7 bits", "about 79.6 bits", "rand.Int(reader, span)",
	} {
		if !strings.Contains(credentials, required) {
			t.Fatalf("the generated SIP credential policy is missing %q", required)
		}
	}
	app := readRepositoryFile(t, "internal/webapp/app.go")
	if !strings.Contains(app, "sipcredentials.Generate()") || strings.Contains(app, `return "rrd_" + suffix`) {
		t.Fatal("the web credential path is not using the digits-only generator")
	}
	setup := readRepositoryFile(t, "web/templates/setup.html")
	for _, required := range []string{`data-setup-value="{{.Claim.Device.SIPUsername}}"`, `data-setup-value="{{.Claim.SIPSecret}}"`, "Digits only", "Leave the spaces out"} {
		if !strings.Contains(setup, required) {
			t.Fatalf("the one-time setup card is missing exact-value guidance %q", required)
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

func TestAIConversationRequiresOperatorChildSafetyApprovalAtEveryBoundary(t *testing.T) {
	required := map[string][]string{
		".env.example":                     {"AI_CHILD_SAFETY_APPROVED=false"},
		"ringringctl":                      {"AI_CHILD_SAFETY_APPROVED=false", "AI_CHILD_SAFETY_APPROVED must be true or false"},
		"cmd/ringring/main.go":             {"EnforceAIChildSafetyGate", "AIChildSafetyApproved: cfg.AIChildSafetyApproved"},
		"internal/config/config.go":        {`envStrictBool("AI_CHILD_SAFETY_APPROVED", false)`},
		"internal/store/store.go":          {"ErrAIChildSafety", "input.AIEnabled && !input.AIChildSafetyApproved", "EnforceAIChildSafetyGate"},
		"internal/telephony/reconciler.go": {"!r.AIChildSafetyApproved", "services[index].AIEnabled = false"},
		"internal/voice/ai.go":             {"!s.AIChildSafetyApproved", "AI conversation child-safety gate is closed"},
		"internal/webapp/app.go":           {"aiEnabled && !a.cfg.AIChildSafetyApproved", "AIChildSafetyApproved: a.cfg.AIChildSafetyApproved"},
		"web/templates/party.html":         {"$conversationReady", "Locked until the server operator"},
	}
	for filename, markers := range required {
		contents := readRepositoryFile(t, filename)
		for _, marker := range markers {
			if !strings.Contains(contents, marker) {
				t.Errorf("%s is missing child-safety gate marker %q", filename, marker)
			}
		}
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
			"chmod 0640 /etc/asterisk/manager.conf /etc/asterisk/pjsip.conf /etc/asterisk/extensions.conf",
			"chown asterisk:ringring /etc/asterisk/manager.conf /etc/asterisk/pjsip.conf /etc/asterisk/extensions.conf",
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
		"web/templates/party.html": {"📳 Ring this phone", "/ring-test"},
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
		"device.RevokedAt == nil", "availableFirstCallLines(party, services, a.cfg.AIChildSafetyApproved)",
		`services.AIEnabled && voiceReady && childSafetyApproved`,
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
