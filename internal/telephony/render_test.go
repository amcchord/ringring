package telephony

import (
	"strings"
	"testing"

	"github.com/amcchord/ringring/internal/model"
	"github.com/amcchord/ringring/internal/radio"
)

func TestRenderIsolatesPartyDialplans(t *testing.T) {
	config, err := Render([]DialDevice{
		{PartyID: "pty_blue", DeviceID: "dev_a", Extension: "101", SIPUsername: "rrd_blue_a", SIPSecret: "secret-a"},
		{PartyID: "pty_blue", DeviceID: "dev_b", Extension: "102", SIPUsername: "rrd_blue_b", SIPSecret: "secret-b"},
		{PartyID: "pty_gold", DeviceID: "dev_c", Extension: "101", SIPUsername: "rrd_gold_c", SIPSecret: "secret-c"},
	}, []model.RoutingServices{
		{PartyID: "pty_blue", TimeEnabled: true, WeatherEnabled: true, AIEnabled: true},
		{PartyID: "pty_gold", TimeEnabled: true, RadioEnabled: true, RadioStation: "drone-zone"},
	})
	if err != nil {
		t.Fatal(err)
	}
	pjsip := string(config.PJSIP)
	dialplan := string(config.Dialplan)
	if !strings.Contains(pjsip, "context=rr-party-pty-blue") || !strings.Contains(pjsip, "context=rr-party-pty-gold") {
		t.Fatalf("missing party endpoint context:\n%s", pjsip)
	}
	if !strings.Contains(pjsip, "[rrd_blue_a]\ntype=aor") || !strings.Contains(pjsip, "aors=rrd_blue_a\n") || strings.Contains(pjsip, "[rrd_blue_a-aor]") {
		t.Fatalf("registrar AOR must match the SIP username:\n%s", pjsip)
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
	for name, party := range map[string]string{"blue": blue, "gold": gold} {
		if !strings.Contains(party, "exten => *10,1,Answer()\n same => n,Wait(1)\n same => n,Playback(beep)\n same => n,Echo()\n same => n,Playback(demo-echodone)") {
			t.Fatalf("%s party missing the always-available echo test:\n%s", name, party)
		}
	}
	if !strings.Contains(blue, "exten => *15,1,Answer()\n same => n,Wait(1)\n same => n,Set(AGIEXITONHANGUP=yes)\n same => n,AGI(agi://app:4573/choose-extension,pty_blue,${CHANNEL(endpoint)})") ||
		!strings.Contains(gold, "choose-extension,pty_gold,${CHANNEL(endpoint)}") {
		t.Fatalf("party extension chooser must receive the authenticated endpoint identity:\n%s", dialplan)
	}
	if strings.Contains(dialplan, "choose-extension,pty_blue,${CALLERID") {
		t.Fatal("extension chooser must not trust caller ID")
	}
	if !strings.Contains(blue, "exten => *11") {
		t.Fatal("time service should be present in each party")
	}
	if !strings.Contains(blue, "exten => *14") || !strings.Contains(blue, "ai-authorize,pty_blue") || !strings.Contains(blue, "Dial(AudioSocket/app:4574/${RINGRING_AI_CALL_ID}/c(slin))") {
		t.Fatalf("blue party missing isolated AI bridge:\n%s", blue)
	}
	if !strings.Contains(blue, "exten => *12") || strings.Contains(blue, "exten => *13") {
		t.Fatal("blue party should contain only its enabled weather service")
	}
	if strings.Contains(gold, "exten => *12") || !strings.Contains(gold, "exten => *13") {
		t.Fatal("gold party should contain only its enabled radio service")
	}
	station, _ := radio.Lookup("drone-zone")
	if !strings.Contains(gold, "MP3Player("+station.StreamURL+")") || strings.Contains(gold, "groovesalad-128-mp3") {
		t.Fatal("radio route must use only the party's catalog station")
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

func TestRenderRejectsUnknownRadioStation(t *testing.T) {
	_, err := Render([]DialDevice{{
		PartyID: "pty_safe", DeviceID: "dev_safe", Extension: "101", SIPUsername: "rrd_safe", SIPSecret: "secret",
	}}, []model.RoutingServices{{PartyID: "pty_safe", RadioEnabled: true, RadioStation: "http://169.254.169.254/latest/meta-data"}})
	if err == nil {
		t.Fatal("arbitrary radio address reached the dialplan renderer")
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
	if !strings.Contains(dialplan, "exten => *10,1,Answer()") || !strings.Contains(dialplan, "same => n,Echo()") {
		t.Fatalf("echo test must remain available when optional services are disabled:\n%s", dialplan)
	}
	if !strings.Contains(dialplan, "exten => *15,1,Answer()") || !strings.Contains(dialplan, "choose-extension,pty_quiet,${CHANNEL(endpoint)}") {
		t.Fatalf("voice extension selection must remain available when optional services are disabled:\n%s", dialplan)
	}
	for _, number := range []string{"*11", "*12", "*13", "*14"} {
		if strings.Contains(dialplan, "exten => "+number) {
			t.Fatalf("disabled service %s remained in dialplan:\n%s", number, dialplan)
		}
	}
}
