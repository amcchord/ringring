package webapp

import (
	"strings"
	"testing"

	webassets "github.com/amcchord/ringring/web"
)

func TestOperatorSetupGuidanceStaysDiscoverable(t *testing.T) {
	setup, err := webassets.Files.ReadFile("templates/setup.html")
	if err != nil {
		t.Fatal(err)
	}
	party, err := webassets.Files.ReadFile("templates/party.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"Ask the RingRing operator",
		"Dial <strong>0</strong> or <strong>*0</strong>",
		"Off Hook Auto Dial</dt><dd><strong>0</strong>",
		"Off Hook Auto Dial Delay</dt><dd><strong>8 seconds</strong>",
		"delayed off-hook, hotline, or warmline dialing",
	} {
		if !strings.Contains(string(setup), required) {
			t.Errorf("setup page omitted operator guidance %q", required)
		}
	}
	for _, required := range []string{"RingRing operator", "Dial 0 or *0", "misdialed numbers"} {
		if !strings.Contains(string(party), required) {
			t.Errorf("party page omitted operator guidance %q", required)
		}
	}
}
