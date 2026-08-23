package telephony

import (
	"errors"
	"strings"

	extensionrules "github.com/amcchord/ringring/internal/extension"
)

const conferencePrefix = "rrc-"

// ActiveConference contains only the identifiers RingRing needs to render a
// party-scoped live call. Asterisk channel IDs, caller ID, addresses, and call
// timing never leave the private AMI boundary.
type ActiveConference struct {
	Name          string
	PartyID       string
	JoinExtension string
	Endpoints     []string
}

func ConferenceName(partyID, joinExtension string) (string, error) {
	if !safeIdentifier.MatchString(partyID) || !extensionrules.Valid(joinExtension) {
		return "", errors.New("conference identity is invalid")
	}
	return conferencePrefix + partyID + "-" + joinExtension, nil
}

func ParseConferenceName(name string) (partyID, joinExtension string, ok bool) {
	if !strings.HasPrefix(name, conferencePrefix) || !amiObjectPattern.MatchString(name) {
		return "", "", false
	}
	body := strings.TrimPrefix(name, conferencePrefix)
	separator := strings.LastIndexByte(body, '-')
	if separator <= 0 || separator == len(body)-1 {
		return "", "", false
	}
	partyID, joinExtension = body[:separator], body[separator+1:]
	if !safeIdentifier.MatchString(partyID) || !extensionrules.Valid(joinExtension) {
		return "", "", false
	}
	return partyID, joinExtension, true
}

func JoinNumber(extension string) string {
	return "*16" + extension
}
