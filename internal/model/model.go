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
	PartyID          string
	TimeEnabled      bool
	WeatherEnabled   bool
	WeatherQuery     string
	WeatherLabel     string
	WeatherLatitude  float64
	WeatherLongitude float64
	RadioEnabled     bool
	RadioStation     string
	AIEnabled        bool
	UpdatedAt        time.Time
}

type RoutingServices struct {
	PartyID        string
	TimeEnabled    bool
	WeatherEnabled bool
	RadioEnabled   bool
	RadioStation   string
	AIEnabled      bool
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
	ID          string
	PartyID     string
	DisplayName string
	Extension   string
	CreatedAt   time.Time
	Devices     []Device
}

type Device struct {
	ID                  string
	MemberID            string
	Label               string
	SIPUsername         string
	SIPSecretCiphertext string
	CreatedAt           time.Time
	RevokedAt           *time.Time
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
