package provisioning

import (
	"encoding/xml"
	"strings"
	"testing"
)

func TestGrandstreamPhonebookXMLContainsPeopleAndServices(t *testing.T) {
	encoded, err := GrandstreamPhonebookXML([]PhoneDestination{
		{Kind: "person", Label: "Alex & Sam", Dial: "102"},
		{Kind: "service", Label: "Echo test", Detail: "Hear your voice.", Dial: "*10"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "Alex & Sam") || !strings.Contains(string(encoded), "Alex &amp; Sam") {
		t.Fatal("contact label was not safely XML escaped")
	}
	var document GrandstreamPhonebook
	if err := xml.Unmarshal(encoded, &document); err != nil {
		t.Fatal(err)
	}
	if document.XMLName.Local != "AddressBook" || document.Version != 1 || len(document.Contacts) != 2 {
		t.Fatalf("unexpected Grandstream phonebook: %#v", document)
	}
	if got := document.Contacts[0]; got.FirstName != "Alex & Sam" || len(got.Phones) != 1 || got.Phones[0].Type != "Work" || got.Phones[0].Number != "102" || got.Phones[0].AccountIndex != 0 {
		t.Fatalf("person contact = %#v", got)
	}
	if got := document.Contacts[1]; got.FirstName != "Echo test" || len(got.Phones) != 1 || got.Phones[0].Number != "*10" {
		t.Fatalf("service contact = %#v", got)
	}
}

func TestGrandstreamPhonebookXMLRejectsUnsafeOrStaleEntries(t *testing.T) {
	tests := [][]PhoneDestination{
		{{Kind: "person", Label: "Unsafe\nname", Dial: "102"}},
		{{Kind: "person", Label: "Friend", Dial: "102"}, {Kind: "service", Label: "Duplicate", Dial: "102"}},
		{{Kind: "call", Label: "Join a live call", Dial: "*16102"}},
	}
	for _, entries := range tests {
		if _, err := GrandstreamPhonebookXML(entries); err == nil {
			t.Fatalf("unsafe phonebook was accepted: %#v", entries)
		}
	}
}
