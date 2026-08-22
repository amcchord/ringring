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

type Party struct {
	ID                     string
	Name                   string
	Slug                   string
	HostUserID             string
	OpenAIProjectID        string
	OpenAIServiceAccountID string
	OpenAIKeyCiphertext    string
	OpenAIStatus           string
	CreatedAt              time.Time
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
