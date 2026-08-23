package model

import "time"

type User struct {
	ID            string
	GoogleSubject string
	Email         string
	Name          string
	AvatarURL     string
	CreatedAt     time.Time
}

type LocalCredential struct {
	User         User
	Username     string
	PasswordHash string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Party struct {
	ID                      string
	Name                    string
	Slug                    string
	HostUserID              string
	OpenAIProjectID         string
	OpenAIServiceAccountID  string
	OpenAIAPIKeyID          string
	OpenAIKeyCiphertext     string
	OpenAIStatus            string
	OpenAISpendLimitCents   int
	OpenAISpendPendingCents int
	OpenAISpendLimitStatus  string
	CreatedAt               time.Time
}

func (p Party) OpenAIUsagePausedForSpendLimit() bool {
	return p.OpenAISpendLimitStatus == "updating" || p.OpenAISpendLimitStatus == "update-error" ||
		p.OpenAIStatus == "spend-updating" || p.OpenAIStatus == "spend-update-error"
}

type PartyServices struct {
	PartyID             string
	TimeEnabled         bool
	WeatherEnabled      bool
	WeatherSetupAllowed bool
	WeatherQuery        string
	WeatherLabel        string
	WeatherLatitude     float64
	WeatherLongitude    float64
	RadioEnabled        bool
	RadioStation        string
	AIEnabled           bool
	UpdatedAt           time.Time
}

type RoutingServices struct {
	PartyID             string
	TimeEnabled         bool
	WeatherEnabled      bool
	WeatherSetupEnabled bool
	RadioEnabled        bool
	RadioStation        string
	AIEnabled           bool
}

type Invitation struct {
	ID              string
	PartyID         string
	CreatedByUserID string
	ExpiresAt       time.Time
	UsedAt          *time.Time
	CreatedAt       time.Time
}

type Member struct {
	ID             string
	PartyID        string
	DisplayName    string
	Extension      string
	AdultExtension bool
	Weather        WeatherLocation
	CreatedAt      time.Time
	Devices        []Device
}

// WeatherLocation belongs to one member/extension. Multiple phones on that
// extension share it, while other members in the party may choose a different
// place. The resolved coordinates never leave the server-side weather flow.
type WeatherLocation struct {
	MemberID  string
	Query     string
	Label     string
	Latitude  float64
	Longitude float64
	UpdatedAt time.Time
}

type Device struct {
	ID                  string
	MemberID            string
	Label               string
	SIPUsername         string
	SIPSecretCiphertext string
	CreatedAt           time.Time
	RevokedAt           *time.Time
	Readiness           DeviceReadiness
}

// DeviceReadiness contains host-confirmed, content-free evidence that a real
// phone completed the important setup checks. Registration presence remains a
// separate live signal; these timestamps never contain call or network data.
type DeviceReadiness struct {
	EchoTestedAt         *time.Time
	OutgoingCallTestedAt *time.Time
	IncomingCallTestedAt *time.Time
}

func (r DeviceReadiness) CompletedCount() int {
	count := 0
	if r.EchoTestedAt != nil {
		count++
	}
	if r.OutgoingCallTestedAt != nil {
		count++
	}
	if r.IncomingCallTestedAt != nil {
		count++
	}
	return count
}

func (r DeviceReadiness) Complete() bool {
	return r.CompletedCount() == 3
}

type ClaimedDevice struct {
	Party     Party
	Member    Member
	Device    Device
	SIPSecret string
}

type RoutingDevice struct {
	PartyID             string
	MemberID            string
	Extension           string
	DeviceID            string
	SIPUsername         string
	SIPSecretCiphertext string
}
