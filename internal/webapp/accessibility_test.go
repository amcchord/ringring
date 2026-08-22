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
		`.device-actions summary { width: fit-content; min-height: 44px;`,
		`.device-readiness summary { width: fit-content; min-height: 44px;`,
		`.readiness-check { min-height: 44px;`,
		`.device-ring-test .text-button:disabled { color: var(--muted);`,
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
				`aria-describedby="openai-spend-help"`,
				`aria-describedby="ring-test-help-{{.ID}}"`,
				`id="ring-test-help-{{.ID}}"`,
			},
		},
		{
			path: "templates/setup.html",
			wants: []string{
				`<dl class="setup-card">`,
				`<dt>Password</dt>`,
				`aria-describedby="linphone-provision-help"`,
				`id="setup-copy-status" class="visually-hidden" role="status" aria-live="polite"`,
				`aria-label="Copy password"`,
				`aria-controls="setup-password setup-copy-status"`,
				`aria-labelledby="field-translator-title"`,
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
