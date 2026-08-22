package telephony

import (
	"regexp"
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
	if !strings.Contains(pjsip, "context=rr-party-pty_blue") || !strings.Contains(pjsip, "context=rr-party-pty_gold") {
		t.Fatalf("missing party endpoint context:\n%s", pjsip)
	}
	if !strings.Contains(pjsip, "[rrd_blue_a]\ntype=aor") || !strings.Contains(pjsip, "aors=rrd_blue_a\n") || strings.Contains(pjsip, "[rrd_blue_a-aor]") {
		t.Fatalf("registrar AOR must match the SIP username:\n%s", pjsip)
	}
	blueStart := strings.Index(dialplan, "[rr-party-pty_blue]")
	goldStart := strings.Index(dialplan, "[rr-party-pty_gold]")
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

func TestRenderPartyContextMappingIsInjective(t *testing.T) {
	devices := []DialDevice{
		{PartyID: "pty_family_a", DeviceID: "dev_underscore", Extension: "101", SIPUsername: "rrd_underscore", SIPSecret: "secret-a"},
		{PartyID: "pty-family-a", DeviceID: "dev_hyphen", Extension: "101", SIPUsername: "rrd_hyphen", SIPSecret: "secret-b"},
		{PartyID: "pty_123456789012345678901234567890_a", DeviceID: "dev_long_a", Extension: "101", SIPUsername: "rrd_long_a", SIPSecret: "secret-c"},
		{PartyID: "pty_123456789012345678901234567890_b", DeviceID: "dev_long_b", Extension: "101", SIPUsername: "rrd_long_b", SIPSecret: "secret-d"},
	}
	config, err := Render(devices, nil)
	if err != nil {
		t.Fatal(err)
	}
	dialplan := string(config.Dialplan)
	pjsip := string(config.PJSIP)
	for _, device := range devices {
		contextName := "rr-party-" + device.PartyID
		if strings.Count(dialplan, "["+contextName+"]") != 1 ||
			!strings.Contains(pjsip, "["+device.SIPUsername+"]\ntype=endpoint") ||
			!strings.Contains(pjsip, "context="+contextName+"\n") {
			t.Fatalf("party %q did not retain its unique context:\n%s\n%s", device.PartyID, pjsip, dialplan)
		}
		contextStart := strings.Index(dialplan, "["+contextName+"]")
		contextEnd := strings.Index(dialplan[contextStart+1:], "\n[")
		context := dialplan[contextStart:]
		if contextEnd >= 0 {
			context = dialplan[contextStart : contextStart+1+contextEnd]
		}
		if !strings.Contains(context, "PJSIP/"+device.SIPUsername) {
			t.Fatalf("party %q lost its endpoint:\n%s", device.PartyID, context)
		}
		for _, other := range devices {
			if other.PartyID != device.PartyID && strings.Contains(context, "PJSIP/"+other.SIPUsername) {
				t.Fatalf("party %q context contains %q endpoint:\n%s", device.PartyID, other.PartyID, context)
			}
		}
	}
}

func TestRenderRejectsDuplicateGlobalRoutingIdentities(t *testing.T) {
	base := DialDevice{PartyID: "pty_one", DeviceID: "dev_one", Extension: "101", SIPUsername: "rrd_one", SIPSecret: "secret-a"}
	for name, duplicate := range map[string]DialDevice{
		"device":   {PartyID: "pty_two", DeviceID: base.DeviceID, Extension: "102", SIPUsername: "rrd_two", SIPSecret: "secret-b"},
		"username": {PartyID: "pty_two", DeviceID: "dev_two", Extension: "102", SIPUsername: base.SIPUsername, SIPSecret: "secret-b"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Render([]DialDevice{base, duplicate}, nil); err == nil {
				t.Fatal("duplicate global routing identity was accepted")
			}
		})
	}
}

func TestRenderRejectsOverlongIdentifiers(t *testing.T) {
	_, err := Render([]DialDevice{{
		PartyID: strings.Repeat("p", 49), DeviceID: "dev_safe", Extension: "101",
		SIPUsername: "rrd_safe", SIPSecret: "secret",
	}}, nil)
	if err == nil {
		t.Fatal("overlong party ID reached Asterisk configuration")
	}
}

func TestRenderHasNoPSTNOrCrossContextDialPrimitive(t *testing.T) {
	config, err := Render([]DialDevice{
		{PartyID: "pty_one", DeviceID: "dev_one", Extension: "101", SIPUsername: "rrd_one", SIPSecret: "secret-a"},
		{PartyID: "pty_one", DeviceID: "dev_two", Extension: "102", SIPUsername: "rrd_two", SIPSecret: "secret-b"},
	}, []model.RoutingServices{{PartyID: "pty_one", AIEnabled: true}})
	if err != nil {
		t.Fatal(err)
	}
	dialplan := string(config.Dialplan)
	allowedPartyDial := regexp.MustCompile(`Dial\(PJSIP/rrd_(one|two)(?:&PJSIP/rrd_(one|two))*,30\)`)
	for _, line := range strings.Split(dialplan, "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, "Dial(") {
			continue
		}
		if allowedPartyDial.MatchString(line) || strings.Contains(line, "Dial(AudioSocket/app:4574/") {
			continue
		}
		t.Fatalf("generated dialplan contains a non-party destination: %q", line)
	}
	for _, forbidden := range []string{"Dial(SIP/", "Dial(IAX2/", "Dial(DAHDI/", "Dial(Local/", "PJSIP_DIAL_CONTACTS", "Goto("} {
		if strings.Contains(dialplan, forbidden) {
			t.Fatalf("generated dialplan contains global/outbound primitive %q", forbidden)
		}
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
