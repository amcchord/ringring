package provisioning

import (
	"encoding/xml"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
)

const linphoneNamespace = "http://www.linphone.org/xsds/lpconfig.xsd"

var (
	sipIdentityPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
	extensionPattern   = regexp.MustCompile(`^[0-9]{2,5}$`)
)

type LinphoneConfig struct {
	Server    string
	Username  string
	Password  string
	Extension string
}

type linphoneDocument struct {
	XMLName  xml.Name          `xml:"config"`
	XMLNS    string            `xml:"xmlns,attr"`
	Sections []linphoneSection `xml:"section"`
}

type linphoneSection struct {
	Name      string          `xml:"name,attr"`
	Overwrite string          `xml:"overwrite,attr,omitempty"`
	Entries   []linphoneEntry `xml:"entry"`
}

type linphoneEntry struct {
	Name      string `xml:"name,attr"`
	Overwrite string `xml:"overwrite,attr,omitempty"`
	Value     string `xml:",chardata"`
}

// LinphoneXML produces a transient remote-provisioning document for one
// RingRing SIP account. XML escaping is handled by encoding/xml and every value
// used inside a SIP URI is constrained before it is interpolated.
func LinphoneXML(input LinphoneConfig) ([]byte, error) {
	host, err := sipHost(input.Server)
	if err != nil {
		return nil, err
	}
	if !sipIdentityPattern.MatchString(input.Username) {
		return nil, errors.New("invalid SIP username")
	}
	if input.Password == "" || len(input.Password) > 256 || strings.IndexFunc(input.Password, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 {
		return nil, errors.New("invalid SIP password")
	}
	if !extensionPattern.MatchString(input.Extension) {
		return nil, errors.New("invalid extension")
	}

	proxy := "<sip:" + host + ":5061;transport=tls>"
	identity := fmt.Sprintf(`"RingRing %s" <sip:%s@%s>`, input.Extension, input.Username, host)
	document := linphoneDocument{
		XMLNS: linphoneNamespace,
		Sections: []linphoneSection{
			{Name: "sip", Entries: []linphoneEntry{overwriteEntry("default_proxy", "0")}},
			{Name: "misc", Entries: []linphoneEntry{overwriteEntry("transient_provisioning", "1")}},
			{Name: "auth_info_0", Overwrite: "true", Entries: []linphoneEntry{
				overwriteEntry("username", input.Username),
				overwriteEntry("userid", input.Username),
				overwriteEntry("passwd", input.Password),
				overwriteEntry("domain", host),
			}},
			{Name: "proxy_0", Overwrite: "true", Entries: []linphoneEntry{
				overwriteEntry("reg_proxy", proxy),
				overwriteEntry("reg_route", proxy),
				overwriteEntry("reg_identity", identity),
				overwriteEntry("reg_expires", "600"),
				overwriteEntry("reg_sendregister", "1"),
				overwriteEntry("publish", "0"),
				overwriteEntry("dial_escape_plus", "0"),
			}},
		},
	}
	encoded, err := xml.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode Linphone provisioning: %w", err)
	}
	return append([]byte(xml.Header), encoded...), nil
}

func overwriteEntry(name, value string) linphoneEntry {
	return linphoneEntry{Name: name, Overwrite: "true", Value: value}
}

func sipHost(value string) (string, error) {
	if value == "" || len(value) > 253 || strings.TrimSpace(value) != value {
		return "", errors.New("invalid SIP host")
	}
	if ip := net.ParseIP(value); ip != nil {
		if strings.Contains(value, ":") {
			return "[" + value + "]", nil
		}
		return value, nil
	}
	for _, label := range strings.Split(value, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", errors.New("invalid SIP host")
		}
		for _, char := range label {
			if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') && char != '-' {
				return "", errors.New("invalid SIP host")
			}
		}
	}
	return strings.ToLower(value), nil
}
