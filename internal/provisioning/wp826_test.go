package provisioning

import (
	"encoding/xml"
	"strings"
	"testing"
)

func TestWP826XMLContainsSIPAccountAndTheme(t *testing.T) {
	encoded, err := WP826XML(WP826Config{
		Server:       "RingRing.Live",
		Username:     "654321",
		Password:     `secret<&"value`,
		Extension:    "103",
		AssetBaseURL: "https://ringring.live/static/wp826/",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), `secret<&"value`) || !strings.Contains(string(encoded), "secret&lt;&amp;&#34;value") {
		t.Fatal("password was not safely XML escaped")
	}
	var document grandstreamDocument
	if err := xml.Unmarshal(encoded, &document); err != nil {
		t.Fatal(err)
	}
	if document.XMLName.Local != "gs_provision" || document.Config.Version != 2 {
		t.Fatalf("unexpected root or config version: %#v", document)
	}

	values := grandstreamValues(document)
	want := map[string]string{
		"account.1/enable":                           "Yes",
		"account.1/name":                             "RingRing 103",
		"account.1.codec.choice/1":                   "PCMU",
		"account.1.dtmf/sendInAudio":                 "No",
		"account.1.dtmf/sendInRtp":                   "Yes",
		"account.1.dtmf/sendInSip":                   "No",
		"account.1.ring/ringtone":                    "1",
		"account.1.sip/registerExpiration":           "5",
		"account.1.sip/registration":                 "Yes",
		"account.1.sip/transport":                    "Tls Or Tcp",
		"account.1.sip/uriSchemeWhenUsingTls":        "sips",
		"account.1.sip/userid":                       "654321",
		"account.1.sip.outboundProxy.1/address":      "",
		"account.1.sip.server.1/address":             "ringring.live:5061",
		"account.1.sip.server.2/address":             "",
		"account.1.sip.server.3/address":             "",
		"account.1.sip.subscriber/name":              "RingRing 103",
		"account.1.sip.subscriber/password":          `secret<&"value`,
		"account.1.sip.subscriber/userid":            "654321",
		"account.1.sip.validate/certificationChain":  "Yes",
		"account.1.sip.validate/domainCertificates":  "Yes",
		"audio.ring/numberOfRingtone":                "4",
		"lcd.wallpaper/serverPath":                   "https://ringring.live/static/wp826/wallpapers/ringring-memphis-day.png",
		"lcd.wallpaper/source":                       "Download",
		"provisioning/validateHostnameInCertificate": "Yes",
		"provisioning.auto/mode":                     "No",
		"provisioning.firmware/protocol":             "HTTPS",
		"provisioning.firmware/serverPath":           "ringring.live/static/wp826/ringtones",
		"security.validate/serverCertificate":        "Yes",
		"softkey.idleLayout/enable":                  "Yes",
		"softkeys.state.idle/hideSystemKeys":         "Custom-Contacts,Custom-History,Custom-Menu",
	}
	for key, value := range want {
		if values[key] != value {
			t.Errorf("%s = %q, want %q", key, values[key], value)
		}
	}
	if len(values) != len(want) {
		t.Fatalf("WP826 output has %d values, want %d: %#v", len(values), len(want), values)
	}
	for _, forbidden := range []string{"wifi", "admin", "party", "member", "device"} {
		if strings.Contains(strings.ToLower(string(encoded)), forbidden) {
			t.Errorf("WP826 output included forbidden unrelated setting %q", forbidden)
		}
	}
}

func TestWP826XMLRejectsUnsafeInputs(t *testing.T) {
	valid := WP826Config{
		Server: "ringring.live", Username: "654321", Password: "123456789012", Extension: "103",
		AssetBaseURL: "https://ringring.live/static/wp826",
	}
	tests := []WP826Config{
		func() WP826Config { value := valid; value.Server = "ringring.live;transport=udp"; return value }(),
		func() WP826Config { value := valid; value.Username = "user@elsewhere"; return value }(),
		func() WP826Config { value := valid; value.Password = "secret\nvalue"; return value }(),
		func() WP826Config { value := valid; value.Extension = "*10"; return value }(),
		func() WP826Config { value := valid; value.AssetBaseURL = "ftp://ringring.live/assets"; return value }(),
		func() WP826Config {
			value := valid
			value.AssetBaseURL = "https://user@ringring.live/assets"
			return value
		}(),
		func() WP826Config {
			value := valid
			value.AssetBaseURL = "https://ringring.live/assets?secret=1"
			return value
		}(),
	}
	for _, test := range tests {
		if _, err := WP826XML(test); err == nil {
			t.Fatalf("unsafe config was accepted: %#v", test)
		}
	}
}

func grandstreamValues(document grandstreamDocument) map[string]string {
	values := make(map[string]string)
	for _, item := range document.Config.Items {
		for _, part := range item.Parts {
			values[item.Name+"/"+part.Name] = part.Value
		}
	}
	return values
}
