package telephony

import (
	"strings"
	"testing"

	"github.com/amcchord/ringring/internal/model"
)

func TestRenderIsolatesPartyDialplans(t *testing.T) {
	config, err := Render([]DialDevice{
		{PartyID: "pty_blue", DeviceID: "dev_a", Extension: "101", SIPUsername: "rrd_blue_a", SIPSecret: "secret-a"},
		{PartyID: "pty_blue", DeviceID: "dev_b", Extension: "102", SIPUsername: "rrd_blue_b", SIPSecret: "secret-b"},
		{PartyID: "pty_gold", DeviceID: "dev_c", Extension: "101", SIPUsername: "rrd_gold_c", SIPSecret: "secret-c"},
	}, []model.RoutingServices{
		{PartyID: "pty_blue", TimeEnabled: true, WeatherEnabled: true},
		{PartyID: "pty_gold", TimeEnabled: true, RadioEnabled: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	pjsip := string(config.PJSIP)
	dialplan := string(config.Dialplan)
	if !strings.Contains(pjsip, "context=rr-party-pty-blue") || !strings.Contains(pjsip, "context=rr-party-pty-gold") {
		t.Fatalf("missing party endpoint context:\n%s", pjsip)
	}
	blueStart := strings.Index(dialplan, "[rr-party-pty-blue]")
	goldStart := strings.Index(dialplan, "[rr-party-pty-gold]")
	if blueStart < 0 || goldStart < 0 {
		t.Fatalf("missing party contexts:\n%s", dialplan)
	}
	blue := dialplan[blueStart:goldStart]
	gold := dialplan[goldStart:]
	if strings.Contains(blue, "rrd_gold_c") || strings.Contains(gold, "rrd_blue") {
		t.Fatalf("cross-party endpoint leaked into dialplan:\n%s", dialplan)
	}
	if !strings.Contains(blue, "exten => *11") {
		t.Fatal("time service should be present in each party")
	}
	if !strings.Contains(blue, "exten => *12") || strings.Contains(blue, "exten => *13") {
		t.Fatal("blue party should contain only its enabled weather service")
	}
	if strings.Contains(gold, "exten => *12") || !strings.Contains(gold, "exten => *13") {
		t.Fatal("gold party should contain only its enabled radio service")
	}
	if strings.Contains(blue, "pty_gold") || strings.Contains(gold, "pty_blue") {
		t.Fatalf("service party ID leaked across contexts:\n%s", dialplan)
	}
}

func TestRenderRejectsConfigInjection(t *testing.T) {
	_, err := Render([]DialDevice{{
		PartyID: "pty_safe", DeviceID: "dev_safe", Extension: "101", SIPUsername: "rrd_safe",
		SIPSecret: "secret\n[evil]",
	}}, nil)
	if err == nil {
		t.Fatal("expected newline injection to be rejected")
	}
}

func TestRenderOmitsDisabledSpecialNumbers(t *testing.T) {
	config, err := Render([]DialDevice{{
		PartyID: "pty_quiet", DeviceID: "dev_quiet", Extension: "101", SIPUsername: "rrd_quiet", SIPSecret: "secret",
	}}, []model.RoutingServices{{PartyID: "pty_quiet"}})
	if err != nil {
		t.Fatal(err)
	}
	dialplan := string(config.Dialplan)
	for _, number := range []string{"*11", "*12", "*13"} {
		if strings.Contains(dialplan, "exten => "+number) {
			t.Fatalf("disabled service %s remained in dialplan:\n%s", number, dialplan)
		}
	}
}
