package provisioning

import (
	_ "embed"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const PhoneProvisioningVersion = 1

var serviceExtensionPattern = regexp.MustCompile(`^\*[0-9]{2}$`)
var conferenceJoinPattern = regexp.MustCompile(`^\*16[0-9]{2,5}$`)

//go:embed phone-provisioning.openapi.yaml
var phoneOpenAPIDocument []byte

// PhoneProvisioningDocument is the deliberately small, vendor-neutral payload
// consumed by RingRing and compatible third-party phone apps. Alongside one SIP
// endpoint it carries only the names and dial targets that this party member may
// call; host, party, and device labels never enter it.
type PhoneProvisioningDocument struct {
	Version      int                `json:"version"`
	SIP          PhoneSIPAccount    `json:"sip"`
	Destinations []PhoneDestination `json:"destinations"`
}

type PhoneSIPAccount struct {
	Server    string `json:"server"`
	Port      int    `json:"port"`
	Transport string `json:"transport"`
	Username  string `json:"username"`
	Password  string `json:"password"`
	Extension string `json:"extension"`
}

type PhoneDestination struct {
	Kind   string `json:"kind"`
	Label  string `json:"label"`
	Detail string `json:"detail,omitempty"`
	Dial   string `json:"dial"`
}

// PhoneJSON returns a transient phone-app provisioning document. The same
// strict host, identity, password, and extension validation used for Linphone
// runs before any credential is serialized.
func PhoneJSON(input LinphoneConfig, destinations []PhoneDestination) ([]byte, error) {
	host, err := sipHost(input.Server)
	if err != nil {
		return nil, err
	}
	if !sipIdentityPattern.MatchString(input.Username) {
		return nil, errors.New("invalid SIP username")
	}
	if err := validatePassword(input.Password); err != nil {
		return nil, err
	}
	if !extensionPattern.MatchString(input.Extension) {
		return nil, errors.New("invalid extension")
	}
	if len(destinations) > 128 {
		return nil, errors.New("too many phone call destinations")
	}
	seenDialTargets := make(map[string]struct{}, len(destinations))
	for _, destination := range destinations {
		if err := validatePhoneDestination(destination); err != nil {
			return nil, err
		}
		if _, exists := seenDialTargets[destination.Dial]; exists {
			return nil, errors.New("duplicate phone call destination")
		}
		seenDialTargets[destination.Dial] = struct{}{}
	}

	document := PhoneProvisioningDocument{
		Version: PhoneProvisioningVersion,
		SIP: PhoneSIPAccount{
			Server: host, Port: 5061, Transport: "tls",
			Username: input.Username, Password: input.Password, Extension: input.Extension,
		},
		Destinations: append([]PhoneDestination(nil), destinations...),
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

// PhoneOpenAPI returns a copy of the embedded OpenAPI contract served by every
// RingRing deployment. Returning a copy prevents a caller from changing the
// process-wide document for later requests.
func PhoneOpenAPI() []byte {
	return append([]byte(nil), phoneOpenAPIDocument...)
}

func validatePhoneDestination(destination PhoneDestination) error {
	if destination.Kind != "person" && destination.Kind != "service" && destination.Kind != "call" {
		return errors.New("invalid phone call destination kind")
	}
	if !validPhoneDestinationText(destination.Label, 80, false) || !validPhoneDestinationText(destination.Detail, 160, true) {
		return errors.New("invalid phone call destination label")
	}
	if !extensionPattern.MatchString(destination.Dial) && !serviceExtensionPattern.MatchString(destination.Dial) && !conferenceJoinPattern.MatchString(destination.Dial) {
		return errors.New("invalid phone call destination dial target")
	}
	if destination.Kind == "call" && !conferenceJoinPattern.MatchString(destination.Dial) {
		return errors.New("live call destination must use a conference join target")
	}
	return nil
}

func validPhoneDestinationText(value string, maximum int, optional bool) bool {
	if value == "" {
		return optional
	}
	if !utf8.ValidString(value) || strings.TrimSpace(value) != value || utf8.RuneCountInString(value) > maximum {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}
