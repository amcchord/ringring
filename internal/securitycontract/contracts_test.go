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
		"write=system,command,call",
	} {
		if !strings.Contains(manager, required) {
			t.Fatalf("AMI boundary lost required rule %q", required)
		}
	}
	if strings.Contains(manager, "permit=0.0.0.0") {
		t.Fatal("AMI permits every source")
	}
	for _, forbidden := range []string{"read=all", "write=all", "originate", "config"} {
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
	for _, required := range []string{"navigator.clipboard.writeText", `document.execCommand("copy")`, `helper.remove()`, `data-copy-setup`, `data-setup-field`} {
		if !strings.Contains(javascript, required) {
			t.Fatalf("the credential copy helper is missing bounded behavior %q", required)
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
