package webapp

import (
	"fmt"
	"io/fs"
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"

	webassets "github.com/amcchord/ringring/web"
)

func TestSharedAccessibilityContract(t *testing.T) {
	base := embeddedText(t, "templates/base.html")
	for _, want := range []string{
		`class="skip-link" href="#main-content"`,
		`<main id="main-content" tabindex="-1">`,
		`<html lang="en">`,
	} {
		if !strings.Contains(base, want) {
			t.Fatalf("base template is missing %q", want)
		}
	}

	css := embeddedText(t, "static/site.css")
	for _, want := range []string{
		`.skip-link { min-height: 44px;`,
		`:where(a, button, input, select, summary):focus-visible { outline: 3px solid var(--ink);`,
		`.site-header nav > a { min-height: 44px;`,
		`.text-button { min-height: 44px;`,
		`.device-card-tools summary { min-height: 58px;`,
		`.member-card-footer > .member-add-device > summary { width: fit-content; min-height: 44px;`,
		`.readiness-check { min-height: 44px;`,
		`.device-ring-button:disabled { color: #625a77;`,
		`.add-device-card input { min-height: 48px;`,
		`.member-card-footer > .member-add-device > summary, .member-remove-link { width: 100%;`,
		`.invite-actions summary { min-height: 44px;`,
		`.call-list li { min-height: 44px;`,
		`.device-guide-jump { min-height: 70px;`,
		`.device-guide-fallback summary { min-height: 52px;`,
		`.device-guide-source a { min-height: 44px;`,
		`.linphone-card, .first-call-grid { grid-template-columns: 1fr; }`,
		`.kicker.light { color: var(--white); }`,
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("shared focus/touch contract is missing %q", want)
		}
	}
	if regexp.MustCompile(`(?s)\.ticker[^{]*\{[^}]*animation\s*:`).MatchString(css) {
		t.Fatal("the highlights strip must stay static unless it gains an explicit pause/stop control")
	}

	paths, err := fs.Glob(webassets.Files, "templates/*.html")
	if err != nil {
		t.Fatal(err)
	}
	decorativeIcon := regexp.MustCompile(`<span class="(?:panel-icon|step-icon)"[^>]*>`)
	for _, path := range paths {
		contents := embeddedText(t, path)
		for _, icon := range decorativeIcon.FindAllString(contents, -1) {
			if !strings.Contains(icon, `aria-hidden="true"`) {
				t.Errorf("%s has a decorative icon exposed to assistive technology: %s", path, icon)
			}
		}
	}
}

func TestFormInstructionsAreVisibleAndAssociated(t *testing.T) {
	tests := []struct {
		path  string
		wants []string
	}{
		{
			path: "templates/signup.html",
			wants: []string{
				`id="signup-username-help"`,
				`aria-describedby="signup-username-help`,
				`id="signup-name" name="name" value="{{.FormName}}" maxlength="40" autocomplete="name" placeholder="Austin" {{if index .FormInvalid "signup-name"}}aria-describedby="signup-error" aria-invalid="true"{{end}}`,
				`id="signup-error" role="alert"`,
			},
		},
		{
			path: "templates/recover.html",
			wants: []string{
				`id="recovery-code-help"`,
				`aria-describedby="recovery-code-help`,
				`id="recover-error" role="alert"`,
			},
		},
		{
			path: "templates/join.html",
			wants: []string{
				`id="extension-help"`,
				`aria-describedby="extension-help{{if index .FormInvalid "extension"}} join-error{{end}}"`,
				`id="join-error" role="alert"`,
				`aria-invalid="true"`,
				`<small>optional</small>`,
			},
		},
		{
			path: "templates/party.html",
			wants: []string{
				`<label for="invite-url">`,
				`id="invite-url-help"`,
				`aria-describedby="invite-url-help"`,
				`alt="One-time RingRing invitation QR code"`,
				`aria-describedby="invite-qr-help"`,
				`id="invite-qr-help"`,
				`action="/parties/{{.Party.ID}}/invites/cancel"`,
				`Used invitations and members are not changed.`,
				`aria-describedby="openai-spend-help"`,
				`aria-label="Ring {{.Label}}"`,
				`aria-describedby="ring-test-help-{{.ID}}"`,
				`id="ring-test-help-{{.ID}}"`,
				`aria-label="Setup checklist for {{.Label}}, {{.Readiness.CompletedCount}} of 3 complete"`,
				`aria-label="{{if .RevokedAt}}Reconnect {{.Label}}{{else}}Phone settings for {{.Label}}{{end}}"`,
				`aria-label="Add another phone for {{$member.DisplayName}} on extension {{$member.Extension}}"`,
				`aria-label="Remove {{.DisplayName}} from this party"`,
				`id="new-device-label-{{$member.ID}}"`,
				`aria-describedby="new-device-help-{{$member.ID}}"`,
				`id="new-device-help-{{$member.ID}}"`,
			},
		},
		{
			path: "templates/setup.html",
			wants: []string{
				`<dl class="setup-card">`,
				`<dt>Password{{if .SimpleSIPCredentials}}<span class="credential-format">12 digits · no spaces</span>{{end}}</dt>`,
				`aria-describedby="linphone-provision-help"`,
				`id="setup-copy-status" class="visually-hidden" role="status" aria-live="polite"`,
				`aria-label="Copy password"`,
				`aria-controls="setup-password setup-copy-status"`,
				`aria-labelledby="field-translator-title"`,
				`href="#grandstream-ht801-v2"`,
				`id="grandstream-ht801-v2" aria-labelledby="grandstream-guide-title"`,
				`aria-label="Grandstream HT801 V2 field values"`,
				`aria-labelledby="first-call-title"`,
				`id="call-people-title"`,
				`id="call-lines-title"`,
				`Private snapshot.`,
			},
		},
	}
	for _, test := range tests {
		contents := embeddedText(t, test.path)
		for _, want := range test.wants {
			if !strings.Contains(contents, want) {
				t.Errorf("%s is missing %q", test.path, want)
			}
		}
	}
}

func TestCorePaletteContrast(t *testing.T) {
	css := embeddedText(t, "static/site.css")
	colors := cssColors(t, css)
	tests := []struct {
		name       string
		foreground string
		background string
		minimum    float64
	}{
		{name: "body text", foreground: "ink", background: "paper", minimum: 4.5},
		{name: "muted text", foreground: "muted", background: "paper", minimum: 4.5},
		{name: "primary button", foreground: "white", background: "purple", minimum: 4.5},
		{name: "small coral accent text", foreground: "coral-text", background: "white", minimum: 4.5},
		{name: "large coral heading", foreground: "coral", background: "paper", minimum: 3},
	}
	for _, test := range tests {
		ratio := contrastRatio(colors[test.foreground], colors[test.background])
		if ratio < test.minimum {
			t.Errorf("%s contrast %.2f:1 is below %.1f:1", test.name, ratio, test.minimum)
		}
	}
}

func TestPhonebookStatusPillsMeetTextContrast(t *testing.T) {
	for _, test := range []struct {
		name       string
		foreground string
		background string
	}{
		{name: "online", foreground: "#11583e", background: "#dcf8ea"},
		{name: "checking", foreground: "#155677", background: "#e3f5ff"},
		{name: "trouble", foreground: "#8a202d", background: "#ffeaeb"},
		{name: "waiting", foreground: "#6d520a", background: "#fff5c7"},
		{name: "unknown", foreground: "#565064", background: "#eeebf2"},
	} {
		if ratio := contrastRatio(test.foreground, test.background); ratio < 4.5 {
			t.Errorf("%s phone status contrast %.2f:1 is below 4.5:1", test.name, ratio)
		}
	}
}

func embeddedText(t *testing.T, path string) string {
	t.Helper()
	contents, err := webassets.Files.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(contents)
}

func cssColors(t *testing.T, css string) map[string]string {
	t.Helper()
	matches := regexp.MustCompile(`--([a-z-]+):\s*(#[0-9a-fA-F]{6});`).FindAllStringSubmatch(css, -1)
	colors := make(map[string]string, len(matches))
	for _, match := range matches {
		colors[match[1]] = match[2]
	}
	for _, name := range []string{"ink", "paper", "white", "purple", "coral", "coral-text", "muted"} {
		if colors[name] == "" {
			t.Fatalf("CSS color token --%s is missing", name)
		}
	}
	return colors
}

func contrastRatio(first, second string) float64 {
	bright, dark := relativeLuminance(first), relativeLuminance(second)
	if bright < dark {
		bright, dark = dark, bright
	}
	return (bright + 0.05) / (dark + 0.05)
}

func relativeLuminance(hex string) float64 {
	component := func(offset int) float64 {
		value, err := strconv.ParseUint(hex[offset:offset+2], 16, 8)
		if err != nil {
			panic(fmt.Sprintf("invalid tested CSS color %q", hex))
		}
		sRGB := float64(value) / 255
		if sRGB <= 0.04045 {
			return sRGB / 12.92
		}
		return math.Pow((sRGB+0.055)/1.055, 2.4)
	}
	return 0.2126*component(1) + 0.7152*component(3) + 0.0722*component(5)
}
