package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/amcchord/ringring/internal/model"
	_ "modernc.org/sqlite"
)

var (
	ErrNotFound       = errors.New("not found")
	ErrInviteUsed     = errors.New("invitation has already been used")
	ErrInviteExpired  = errors.New("invitation has expired")
	ErrExtensionTaken = errors.New("extension is already in use")
)

type Store struct {
	db *sql.DB
}

type GoogleProfile struct {
	Subject   string
	Email     string
	Name      string
	AvatarURL string
}

type NewParty struct {
	ID         string
	Name       string
	Slug       string
	HostUserID string
	CreatedAt  time.Time
}

type NewInvitation struct {
	ID              string
	PartyID         string
	CreatedByUserID string
	TokenHash       []byte
	ExpiresAt       time.Time
	CreatedAt       time.Time
}

type NewClaim struct {
	TokenHash           []byte
	MemberID            string
	DisplayName         string
	Extension           string
	DeviceID            string
	DeviceLabel         string
	SIPUsername         string
	SIPSecretCiphertext string
	Now                 time.Time
}

func Open(path string) (*Store, error) {
	if path != ":memory:" && !strings.HasPrefix(path, "file:") {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA foreign_keys = ON; PRAGMA journal_mode = WAL; PRAGMA busy_timeout = 5000;`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("configure database: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate database: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *Store) UpsertGoogleUser(ctx context.Context, profile GoogleProfile, now time.Time, id string) (model.User, error) {
	const query = `
		INSERT INTO users (id, google_subject, email, name, avatar_url, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(google_subject) DO UPDATE SET
			email = excluded.email,
			name = excluded.name,
			avatar_url = excluded.avatar_url
		RETURNING id, google_subject, email, name, avatar_url, created_at`

	var user model.User
	var created int64
	err := s.db.QueryRowContext(ctx, query, id, profile.Subject, profile.Email, profile.Name, profile.AvatarURL, unix(now)).Scan(
		&user.ID, &user.GoogleSubject, &user.Email, &user.Name, &user.AvatarURL, &created,
	)
	if err != nil {
		return model.User{}, fmt.Errorf("upsert Google user: %w", err)
	}
	user.CreatedAt = fromUnix(created)
	return user, nil
}

func (s *Store) CreateSession(ctx context.Context, tokenHash []byte, userID string, expiresAt, now time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (token_hash, user_id, expires_at, created_at) VALUES (?, ?, ?, ?)`,
		tokenHash, userID, unix(expiresAt), unix(now),
	)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

func (s *Store) UserBySession(ctx context.Context, tokenHash []byte, now time.Time) (model.User, error) {
	const query = `
		SELECT u.id, u.google_subject, u.email, u.name, u.avatar_url, u.created_at
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = ? AND s.expires_at > ?`
	var user model.User
	var created int64
	err := s.db.QueryRowContext(ctx, query, tokenHash, unix(now)).Scan(
		&user.ID, &user.GoogleSubject, &user.Email, &user.Name, &user.AvatarURL, &created,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return model.User{}, ErrNotFound
	}
	if err != nil {
		return model.User{}, fmt.Errorf("load session: %w", err)
	}
	user.CreatedAt = fromUnix(created)
	return user, nil
}

func (s *Store) DeleteSession(ctx context.Context, tokenHash []byte) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, tokenHash); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

func (s *Store) CreateParty(ctx context.Context, input NewParty) (model.Party, error) {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO parties (id, name, slug, host_user_id, openai_status, created_at)
		VALUES (?, ?, ?, ?, 'pending', ?)`,
		input.ID, input.Name, input.Slug, input.HostUserID, unix(input.CreatedAt),
	)
	if err != nil {
		return model.Party{}, fmt.Errorf("create party: %w", err)
	}
	return model.Party{
		ID: input.ID, Name: input.Name, Slug: input.Slug, HostUserID: input.HostUserID,
		OpenAIStatus: "pending", CreatedAt: input.CreatedAt.UTC(),
	}, nil
}

func (s *Store) UpdatePartyOpenAI(ctx context.Context, partyID, projectID, serviceAccountID, keyCiphertext, status string) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE parties SET openai_project_id = ?, openai_service_account_id = ?,
			openai_key_ciphertext = ?, openai_status = ? WHERE id = ?`,
		projectID, serviceAccountID, keyCiphertext, status, partyID,
	)
	if err != nil {
		return fmt.Errorf("update party OpenAI configuration: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ListPartiesByHost(ctx context.Context, hostUserID string) ([]model.Party, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, slug, host_user_id, openai_project_id, openai_service_account_id,
			openai_key_ciphertext, openai_status, created_at
		FROM parties WHERE host_user_id = ? ORDER BY created_at DESC`, hostUserID)
	if err != nil {
		return nil, fmt.Errorf("list parties: %w", err)
	}
	defer rows.Close()

	var parties []model.Party
	for rows.Next() {
		party, err := scanParty(rows)
		if err != nil {
			return nil, err
		}
		parties = append(parties, party)
	}
	return parties, rows.Err()
}

func (s *Store) PartyForHost(ctx context.Context, partyID, hostUserID string) (model.Party, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, slug, host_user_id, openai_project_id, openai_service_account_id,
			openai_key_ciphertext, openai_status, created_at
		FROM parties WHERE id = ? AND host_user_id = ?`, partyID, hostUserID)
	party, err := scanParty(row)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Party{}, ErrNotFound
	}
	return party, err
}

func (s *Store) CreateInvitation(ctx context.Context, input NewInvitation) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO invitations (id, party_id, created_by_user_id, token_hash, expires_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		input.ID, input.PartyID, input.CreatedByUserID, input.TokenHash, unix(input.ExpiresAt), unix(input.CreatedAt),
	)
	if err != nil {
		return fmt.Errorf("create invitation: %w", err)
	}
	return nil
}

func (s *Store) PartyByInvitation(ctx context.Context, tokenHash []byte, now time.Time) (model.Party, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT p.id, p.name, p.slug, p.host_user_id, p.openai_project_id,
			p.openai_service_account_id, p.openai_key_ciphertext, p.openai_status, p.created_at,
			i.expires_at, i.used_at
		FROM invitations i JOIN parties p ON p.id = i.party_id
		WHERE i.token_hash = ?`, tokenHash)
	var party model.Party
	var projectID, serviceID, keyCipher, status sql.NullString
	var created, expires int64
	var used sql.NullInt64
	err := row.Scan(&party.ID, &party.Name, &party.Slug, &party.HostUserID, &projectID, &serviceID,
		&keyCipher, &status, &created, &expires, &used)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Party{}, ErrNotFound
	}
	if err != nil {
		return model.Party{}, fmt.Errorf("load invitation: %w", err)
	}
	if used.Valid {
		return model.Party{}, ErrInviteUsed
	}
	if fromUnix(expires).Before(now) {
		return model.Party{}, ErrInviteExpired
	}
	party.OpenAIProjectID = projectID.String
	party.OpenAIServiceAccountID = serviceID.String
	party.OpenAIKeyCiphertext = keyCipher.String
	party.OpenAIStatus = status.String
	party.CreatedAt = fromUnix(created)
	return party, nil
}

func (s *Store) ClaimInvitation(ctx context.Context, input NewClaim) (model.Party, model.Member, model.Device, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.Party{}, model.Member{}, model.Device{}, fmt.Errorf("begin invitation claim: %w", err)
	}
	defer tx.Rollback()

	var invitationID, partyID string
	var expires int64
	var used sql.NullInt64
	err = tx.QueryRowContext(ctx,
		`SELECT id, party_id, expires_at, used_at FROM invitations WHERE token_hash = ?`, input.TokenHash,
	).Scan(&invitationID, &partyID, &expires, &used)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Party{}, model.Member{}, model.Device{}, ErrNotFound
	}
	if err != nil {
		return model.Party{}, model.Member{}, model.Device{}, fmt.Errorf("load invitation claim: %w", err)
	}
	if used.Valid {
		return model.Party{}, model.Member{}, model.Device{}, ErrInviteUsed
	}
	if fromUnix(expires).Before(input.Now) {
		return model.Party{}, model.Member{}, model.Device{}, ErrInviteExpired
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO members (id, party_id, display_name, extension, created_at)
		VALUES (?, ?, ?, ?, ?)`, input.MemberID, partyID, input.DisplayName, input.Extension, unix(input.Now))
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return model.Party{}, model.Member{}, model.Device{}, ErrExtensionTaken
		}
		return model.Party{}, model.Member{}, model.Device{}, fmt.Errorf("create member: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO devices (id, member_id, label, sip_username, sip_secret_ciphertext, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`, input.DeviceID, input.MemberID, input.DeviceLabel,
		input.SIPUsername, input.SIPSecretCiphertext, unix(input.Now))
	if err != nil {
		return model.Party{}, model.Member{}, model.Device{}, fmt.Errorf("create device: %w", err)
	}

	result, err := tx.ExecContext(ctx, `UPDATE invitations SET used_at = ? WHERE id = ? AND used_at IS NULL`, unix(input.Now), invitationID)
	if err != nil {
		return model.Party{}, model.Member{}, model.Device{}, fmt.Errorf("consume invitation: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return model.Party{}, model.Member{}, model.Device{}, ErrInviteUsed
	}

	party, err := partyByIDTx(ctx, tx, partyID)
	if err != nil {
		return model.Party{}, model.Member{}, model.Device{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.Party{}, model.Member{}, model.Device{}, fmt.Errorf("commit invitation claim: %w", err)
	}

	member := model.Member{ID: input.MemberID, PartyID: partyID, DisplayName: input.DisplayName, Extension: input.Extension, CreatedAt: input.Now.UTC()}
	device := model.Device{ID: input.DeviceID, MemberID: input.MemberID, Label: input.DeviceLabel, SIPUsername: input.SIPUsername, SIPSecretCiphertext: input.SIPSecretCiphertext, CreatedAt: input.Now.UTC()}
	return party, member, device, nil
}

func (s *Store) ListMembers(ctx context.Context, partyID string) ([]model.Member, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.id, m.party_id, m.display_name, m.extension, m.created_at,
			d.id, d.label, d.sip_username, d.created_at, d.revoked_at
		FROM members m
		LEFT JOIN devices d ON d.member_id = m.id
		WHERE m.party_id = ?
		ORDER BY CAST(m.extension AS INTEGER), m.extension, d.created_at`, partyID)
	if err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}
	defer rows.Close()

	var members []model.Member
	byID := make(map[string]int)
	for rows.Next() {
		var member model.Member
		var memberCreated int64
		var deviceID, deviceLabel, sipUsername sql.NullString
		var deviceCreated, revoked sql.NullInt64
		if err := rows.Scan(&member.ID, &member.PartyID, &member.DisplayName, &member.Extension, &memberCreated,
			&deviceID, &deviceLabel, &sipUsername, &deviceCreated, &revoked); err != nil {
			return nil, fmt.Errorf("scan member: %w", err)
		}
		index, ok := byID[member.ID]
		if !ok {
			member.CreatedAt = fromUnix(memberCreated)
			members = append(members, member)
			index = len(members) - 1
			byID[member.ID] = index
		}
		if deviceID.Valid {
			device := model.Device{ID: deviceID.String, MemberID: member.ID, Label: deviceLabel.String, SIPUsername: sipUsername.String, CreatedAt: fromUnix(deviceCreated.Int64)}
			if revoked.Valid {
				value := fromUnix(revoked.Int64)
				device.RevokedAt = &value
			}
			members[index].Devices = append(members[index].Devices, device)
		}
	}
	return members, rows.Err()
}

func (s *Store) RoutingDevices(ctx context.Context) ([]model.RoutingDevice, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.party_id, m.id, m.extension, d.id, d.sip_username, d.sip_secret_ciphertext
		FROM devices d JOIN members m ON m.id = d.member_id
		WHERE d.revoked_at IS NULL
		ORDER BY m.party_id, CAST(m.extension AS INTEGER), d.id`)
	if err != nil {
		return nil, fmt.Errorf("list routing devices: %w", err)
	}
	defer rows.Close()
	var devices []model.RoutingDevice
	for rows.Next() {
		var device model.RoutingDevice
		if err := rows.Scan(&device.PartyID, &device.MemberID, &device.Extension, &device.DeviceID,
			&device.SIPUsername, &device.SIPSecretCiphertext); err != nil {
			return nil, fmt.Errorf("scan routing device: %w", err)
		}
		devices = append(devices, device)
	}
	return devices, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanParty(row scanner) (model.Party, error) {
	var party model.Party
	var projectID, serviceID, keyCipher, status sql.NullString
	var created int64
	if err := row.Scan(&party.ID, &party.Name, &party.Slug, &party.HostUserID, &projectID,
		&serviceID, &keyCipher, &status, &created); err != nil {
		return model.Party{}, err
	}
	party.OpenAIProjectID = projectID.String
	party.OpenAIServiceAccountID = serviceID.String
	party.OpenAIKeyCiphertext = keyCipher.String
	party.OpenAIStatus = status.String
	party.CreatedAt = fromUnix(created)
	return party, nil
}

func partyByIDTx(ctx context.Context, tx *sql.Tx, partyID string) (model.Party, error) {
	return scanParty(tx.QueryRowContext(ctx, `
		SELECT id, name, slug, host_user_id, openai_project_id, openai_service_account_id,
			openai_key_ciphertext, openai_status, created_at FROM parties WHERE id = ?`, partyID))
}

func unix(value time.Time) int64     { return value.UTC().Unix() }
func fromUnix(value int64) time.Time { return time.Unix(value, 0).UTC() }

const schema = `
CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    google_subject TEXT NOT NULL UNIQUE,
    email TEXT NOT NULL,
    name TEXT NOT NULL,
    avatar_url TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS sessions (
    token_hash BLOB PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at INTEGER NOT NULL,
    created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS sessions_expires_at ON sessions(expires_at);

CREATE TABLE IF NOT EXISTS parties (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    slug TEXT NOT NULL UNIQUE,
    host_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    openai_project_id TEXT,
    openai_service_account_id TEXT,
    openai_key_ciphertext TEXT,
    openai_status TEXT NOT NULL DEFAULT 'pending',
    created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS parties_host ON parties(host_user_id);

CREATE TABLE IF NOT EXISTS invitations (
    id TEXT PRIMARY KEY,
    party_id TEXT NOT NULL REFERENCES parties(id) ON DELETE CASCADE,
    created_by_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    token_hash BLOB NOT NULL UNIQUE,
    expires_at INTEGER NOT NULL,
    used_at INTEGER,
    created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS invitations_party ON invitations(party_id);

CREATE TABLE IF NOT EXISTS members (
    id TEXT PRIMARY KEY,
    party_id TEXT NOT NULL REFERENCES parties(id) ON DELETE CASCADE,
    display_name TEXT NOT NULL,
    extension TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    UNIQUE(party_id, extension)
);
CREATE INDEX IF NOT EXISTS members_party ON members(party_id);

CREATE TABLE IF NOT EXISTS devices (
    id TEXT PRIMARY KEY,
    member_id TEXT NOT NULL REFERENCES members(id) ON DELETE CASCADE,
    label TEXT NOT NULL,
    sip_username TEXT NOT NULL UNIQUE,
    sip_secret_ciphertext TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    revoked_at INTEGER
);
CREATE INDEX IF NOT EXISTS devices_member ON devices(member_id);
`
