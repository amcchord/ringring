package provisioning

import (
	"encoding/xml"
	"errors"
	"fmt"
)

const GrandstreamPhonebookVersion = 1

type GrandstreamPhonebook struct {
	XMLName  xml.Name                  `xml:"AddressBook"`
	Version  int                       `xml:"version"`
	Contacts []GrandstreamPhoneContact `xml:"Contact"`
}

type GrandstreamPhoneContact struct {
	FirstName string                   `xml:"FirstName"`
	Phones    []GrandstreamPhoneNumber `xml:"Phone"`
}

type GrandstreamPhoneNumber struct {
	Type         string `xml:"type,attr"`
	Number       string `xml:"phonenumber"`
	AccountIndex int    `xml:"accountindex"`
}

// GrandstreamPhonebookXML converts a party-scoped RingRing directory into the
// centralized XML phonebook understood by Grandstream handsets. Live-call
// buttons are intentionally excluded because a handset may cache this document.
func GrandstreamPhonebookXML(destinations []PhoneDestination) ([]byte, error) {
	if len(destinations) > 128 {
		return nil, errors.New("too many Grandstream phonebook contacts")
	}
	seenDialTargets := make(map[string]struct{}, len(destinations))
	contacts := make([]GrandstreamPhoneContact, 0, len(destinations))
	for _, destination := range destinations {
		if err := validatePhoneDestination(destination); err != nil {
			return nil, err
		}
		if destination.Kind == "call" {
			return nil, errors.New("live calls cannot be cached in a Grandstream phonebook")
		}
		if _, exists := seenDialTargets[destination.Dial]; exists {
			return nil, errors.New("duplicate Grandstream phonebook contact")
		}
		seenDialTargets[destination.Dial] = struct{}{}
		contacts = append(contacts, GrandstreamPhoneContact{
			FirstName: destination.Label,
			Phones: []GrandstreamPhoneNumber{{
				Type: "Work", Number: destination.Dial, AccountIndex: 0,
			}},
		})
	}
	document := GrandstreamPhonebook{Version: GrandstreamPhonebookVersion, Contacts: contacts}
	encoded, err := xml.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode Grandstream phonebook: %w", err)
	}
	return append(append([]byte(xml.Header), encoded...), '\n'), nil
}
