package provisioning

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPhoneJSONContainsVersionedAccountAndCallDestinations(t *testing.T) {
	encoded, err := PhoneJSON(LinphoneConfig{
		Server: "RingRing.Live", Username: "rrd_phone_1", Password: `secret<&\"value`, Extension: "101",
	}, []PhoneDestination{
		{Kind: "person", Label: "Studio phone", Dial: "102"},
		{Kind: "service", Label: "Echo test", Detail: "Hear your own voice come back.", Dial: "*10"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var document PhoneProvisioningDocument
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatal(err)
	}
	if document.Version != PhoneProvisioningVersion {
		t.Fatalf("version = %d", document.Version)
	}
	want := PhoneSIPAccount{
		Server: "ringring.live", Port: 5061, Transport: "tls", Username: "rrd_phone_1",
		Password: `secret<&\"value`, Extension: "101",
	}
	if document.SIP != want {
		t.Fatalf("SIP account = %#v, want %#v", document.SIP, want)
	}
	if len(document.Destinations) != 2 || document.Destinations[0].Label != "Studio phone" || document.Destinations[0].Dial != "102" || document.Destinations[1].Dial != "*10" {
		t.Fatalf("call destinations = %#v", document.Destinations)
	}
	for _, privateField := range []string{"party", "member", "device", "display_name", "email", "host_user"} {
		if strings.Contains(string(encoded), privateField) {
			t.Fatalf("phone provisioning included private field %q", privateField)
		}
	}
}

func TestPhoneJSONRejectsUnsafeInputs(t *testing.T) {
	tests := []LinphoneConfig{
		{Server: "ringring.live;transport=udp", Username: "rrd_phone", Password: "secret", Extension: "101"},
		{Server: "ringring.live", Username: "rrd_phone@elsewhere", Password: "secret", Extension: "101"},
		{Server: "ringring.live", Username: "rrd_phone", Password: "secret\nvalue", Extension: "101"},
		{Server: "ringring.live", Username: "rrd_phone", Password: "secret", Extension: "*10"},
	}
	for _, test := range tests {
		if _, err := PhoneJSON(test, nil); err == nil {
			t.Fatalf("unsafe config was accepted: %#v", test)
		}
	}
}

func TestPhoneJSONRejectsUnsafeCallDestinations(t *testing.T) {
	account := LinphoneConfig{Server: "ringring.live", Username: "rrd_phone", Password: "secret", Extension: "101"}
	tests := [][]PhoneDestination{
		{{Kind: "outside-party", Label: "Unknown", Dial: "102"}},
		{{Kind: "person", Label: "", Dial: "102"}},
		{{Kind: "person", Label: "Studio\nphone", Dial: "102"}},
		{{Kind: "person", Label: "Studio phone", Dial: "911911"}},
		{{Kind: "service", Label: "Echo test", Dial: "*1"}},
		{{Kind: "person", Label: "Studio phone", Dial: "102"}, {Kind: "service", Label: "Duplicate", Dial: "102"}},
	}
	for _, destinations := range tests {
		if _, err := PhoneJSON(account, destinations); err == nil {
			t.Fatalf("unsafe destinations were accepted: %#v", destinations)
		}
	}
}

func TestPhoneOpenAPITracksTheRuntimeContract(t *testing.T) {
	document := string(PhoneOpenAPI())
	for _, required := range []string{
		"openapi: 3.1.2",
		"/api/v1/phone-invitations/{token}:",
		"previewPhoneInvitation",
		"claimPhoneInvitation",
		"PhoneInvitationClaim:",
		"/api/v1/phone-provisioning/{token}:",
		"/provision/ios/{token}:",
		"deprecated: true",
		"PhoneProvisioningDocument:",
		"const: 5061",
		"const: tls",
		"maxItems: 128",
		"application/problem+json:",
	} {
		if !strings.Contains(document, required) {
			t.Fatalf("OpenAPI document is missing %q", required)
		}
	}
	for _, forbidden := range []string{"real password", "host@example", "Blue phone"} {
		if strings.Contains(document, forbidden) {
			t.Fatalf("OpenAPI document contains private or child-specific example %q", forbidden)
		}
	}
}
