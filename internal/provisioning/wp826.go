package provisioning

import (
	"encoding/xml"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

type WP826Config struct {
	Server       string
	Username     string
	Password     string
	Extension    string
	AssetBaseURL string
	PhonebookURL string
}

type grandstreamDocument struct {
	XMLName xml.Name          `xml:"gs_provision"`
	Config  grandstreamConfig `xml:"config"`
}

type grandstreamConfig struct {
	Version int               `xml:"version,attr"`
	Items   []grandstreamItem `xml:"item"`
}

type grandstreamItem struct {
	Name  string            `xml:"name,attr"`
	Parts []grandstreamPart `xml:"part"`
}

type grandstreamPart struct {
	Name  string `xml:"name,attr"`
	Value string `xml:",chardata"`
}

// WP826XML produces a partial Grandstream alias configuration for Account 1.
// It deliberately leaves Wi-Fi, networking, administrator access, and every
// other account untouched so a household setup file cannot lock out the owner.
func WP826XML(input WP826Config) ([]byte, error) {
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

	assetBase, firmwareProtocol, firmwarePath, err := wp826AssetLocation(input.AssetBaseURL)
	if err != nil {
		return nil, err
	}
	phonebookMode, phonebookServer, err := wp826PhonebookLocation(input.PhonebookURL)
	if err != nil {
		return nil, err
	}
	displayName := "RingRing " + input.Extension
	document := grandstreamDocument{Config: grandstreamConfig{
		Version: 2,
		Items: []grandstreamItem{
			item("account.1", part("enable", "Yes"), part("name", displayName)),
			item("account.1.codec.choice", part("1", "PCMU")),
			item("account.1.dtmf", part("sendInAudio", "No"), part("sendInRtp", "Yes"), part("sendInSip", "No")),
			item("account.1.ring", part("ringtone", "1")),
			item("account.1.sip",
				part("registerExpiration", "5"),
				part("registration", "Yes"),
				part("transport", "Tls Or Tcp"),
				part("uriSchemeWhenUsingTls", "sips"),
				part("userid", input.Username),
			),
			item("account.1.sip.outboundProxy.1", part("address", "")),
			item("account.1.sip.server.1", part("address", host+":5061")),
			item("account.1.sip.server.2", part("address", "")),
			item("account.1.sip.server.3", part("address", "")),
			item("account.1.sip.subscriber",
				part("name", displayName),
				part("password", input.Password),
				part("userid", input.Username),
			),
			item("account.1.sip.validate", part("certificationChain", "Yes"), part("domainCertificates", "Yes")),
			item("audio.ring", part("numberOfRingtone", "4")),
			item("lcd.wallpaper",
				part("serverPath", assetBase+"/wallpapers/ringring-memphis-day.png"),
				part("source", "Download"),
			),
			item("phonebook.download",
				part("interval", "5"),
				part("mode", phonebookMode),
				part("password", input.Password),
				part("removeEditedEntries", "Yes"),
				part("server", phonebookServer),
				part("username", input.Username),
			),
			item("provisioning", part("validateHostnameInCertificate", "Yes")),
			item("provisioning.auto", part("mode", "No")),
			item("provisioning.firmware",
				part("protocol", firmwareProtocol),
				part("serverPath", firmwarePath+"/ringtones"),
			),
			item("security.validate", part("serverCertificate", "Yes")),
			item("softkey.idleLayout", part("enable", "Yes")),
			item("softkeys.state.idle", part("hideSystemKeys", "Custom-Contacts,Custom-History,Custom-Menu")),
		},
	}}
	encoded, err := xml.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode WP826 provisioning: %w", err)
	}
	return append([]byte(xml.Header), encoded...), nil
}

func wp826PhonebookLocation(value string) (mode, server string, err error) {
	parsed, parseErr := url.Parse(value)
	if parseErr != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path == "" {
		return "", "", errors.New("invalid WP826 phonebook URL")
	}
	switch parsed.Scheme {
	case "https":
		mode = "Enabled Use HTTPS"
	case "http":
		mode = "Enabled Use HTTP"
	default:
		return "", "", errors.New("invalid WP826 phonebook URL")
	}
	return mode, parsed.Host + parsed.EscapedPath(), nil
}

func wp826AssetLocation(value string) (base, protocol, firmwarePath string, err error) {
	base = strings.TrimRight(value, "/")
	parsed, parseErr := url.Parse(base)
	if parseErr != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", "", "", errors.New("invalid WP826 asset base URL")
	}
	switch parsed.Scheme {
	case "https":
		protocol = "HTTPS"
	case "http":
		protocol = "HTTP"
	default:
		return "", "", "", errors.New("invalid WP826 asset base URL")
	}
	firmwarePath = parsed.Host + strings.TrimRight(parsed.EscapedPath(), "/")
	return base, protocol, firmwarePath, nil
}

func item(name string, parts ...grandstreamPart) grandstreamItem {
	return grandstreamItem{Name: name, Parts: parts}
}

func part(name, value string) grandstreamPart {
	return grandstreamPart{Name: name, Value: value}
}
