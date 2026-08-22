package provisioning

import (
	"encoding/xml"
	"strings"
	"testing"
)

func TestLinphoneXMLContainsTransientSIPAccount(t *testing.T) {
	encoded, err := LinphoneXML(LinphoneConfig{
		Server: "RingRing.Live", Username: "rrd_phone_1", Password: `secret<&"value`, Extension: "101",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), `secret<&"value`) || !strings.Contains(string(encoded), "secret&lt;&amp;&#34;value") {
		t.Fatal("password was not safely XML escaped")
	}
	var document linphoneDocument
	if err := xml.Unmarshal(encoded, &document); err != nil {
		t.Fatal(err)
	}
	if document.XMLName.Local != "config" || document.XMLName.Space != linphoneNamespace {
		t.Fatalf("unexpected root: %#v", document.XMLName)
	}
	sections := sectionValues(document)
	want := map[string]string{
		"sip/default_proxy": "0", "misc/transient_provisioning": "1",
		"auth_info_0/username": "rrd_phone_1", "auth_info_0/userid": "rrd_phone_1",
		"auth_info_0/passwd": `secret<&"value`, "auth_info_0/domain": "ringring.live",
		"proxy_0/reg_proxy":    "<sip:ringring.live:5060;transport=udp>",
		"proxy_0/reg_route":    "<sip:ringring.live:5060;transport=udp>",
		"proxy_0/reg_identity": `"RingRing 101" <sip:rrd_phone_1@ringring.live>`,
		"proxy_0/reg_expires":  "600", "proxy_0/reg_sendregister": "1",
		"proxy_0/publish": "0", "proxy_0/dial_escape_plus": "0",
	}
	for key, value := range want {
		if sections[key] != value {
			t.Errorf("%s = %q, want %q", key, sections[key], value)
		}
	}
	for _, section := range document.Sections {
		for _, entry := range section.Entries {
			if entry.Overwrite != "true" {
				t.Errorf("%s/%s did not opt into overwrite", section.Name, entry.Name)
			}
		}
	}
}

func TestLinphoneXMLRejectsUnsafeURIInputs(t *testing.T) {
	tests := []LinphoneConfig{
		{Server: "ringring.live;transport=tcp", Username: "rrd_phone", Password: "secret", Extension: "101"},
		{Server: "ringring.live", Username: "rrd_phone@elsewhere", Password: "secret", Extension: "101"},
		{Server: "ringring.live", Username: "rrd_phone", Password: "secret\nentry", Extension: "101"},
		{Server: "ringring.live", Username: "rrd_phone", Password: "secret", Extension: "*10"},
	}
	for _, test := range tests {
		if _, err := LinphoneXML(test); err == nil {
			t.Fatalf("unsafe config was accepted: %#v", test)
		}
	}
}

func sectionValues(document linphoneDocument) map[string]string {
	values := make(map[string]string)
	for _, section := range document.Sections {
		for _, entry := range section.Entries {
			values[section.Name+"/"+entry.Name] = entry.Value
		}
	}
	return values
}
