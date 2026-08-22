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
	ErrNotFound         = errors.New("not found")
	ErrInviteUsed       = errors.New("invitation has already been used")
	ErrInviteExpired    = errors.New("invitation has expired")
	ErrInvalidExtension = errors.New("extension must contain 2 to 5 digits")
	ErrExtensionTaken   = errors.New("extension is already in use")
	ErrUsernameTaken    = errors.New("username is already in use")
	ErrRecoveryCode     = errors.New("invalid recovery code")
	ErrPartiesRemain    = errors.New("host still owns parties")
	ErrProvisionUsed    = errors.New("provisioning link has already been used")
	ErrProvisionExpired = errors.New("provisioning link has expired")
	ErrOpenAIRotation   = errors.New("OpenAI key rotation state changed")
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

type NewLocalUser struct {
	ID                 string
	Name               string
	Username           string
	PasswordHash       string
	RecoveryCodeHashes [][]byte
	SessionTokenHash   []byte
	SessionExpiresAt   time.Time
	CreatedAt          time.Time
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
	Provisioning        NewProvisioningToken
	Now                 time.Time
}

type NewProvisioningToken struct {
	TokenHash []byte
	ExpiresAt time.Time
	CreatedAt time.Time
}

type ProvisioningDevice struct {
	DeviceID            string
	SIPUsername         string
	SIPSecretCiphertext string
	Extension           string
}

type RotatedDevice struct {
	Party  model.Party
	Member model.Member
	Device model.Device
}

type ServiceSettingsInput struct {
	TimeEnabled      bool
	WeatherEnabled   bool
	WeatherQuery     string
	WeatherLabel     string
	WeatherLatitude  float64
	WeatherLongitude float64
	RadioEnabled     bool
	AIEnabled        bool
	UpdatedAt        time.Time
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
	if err := ensurePartyServicesAIColumn(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate AI service setting: %w", err)
	}
	if err := ensurePartyOpenAIAPIKeyColumn(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate OpenAI key identifier: %w", err)
	}
	return &Store{db: db}, nil
}

func ensurePartyServicesAIColumn(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(party_services)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return err
		}
		if name == "ai_enabled" {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if found {
		return nil
	}
	_, err = db.Exec(`ALTER TABLE party_services ADD COLUMN ai_enabled INTEGER NOT NULL DEFAULT 0 CHECK(ai_enabled IN (0, 1))`)
	return err
}

func ensurePartyOpenAIAPIKeyColumn(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(parties)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return err
		}
		if name == "openai_api_key_id" {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if found {
		return nil
	}
	_, err = db.Exec(`ALTER TABLE parties ADD COLUMN openai_api_key_id TEXT`)
	return err
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

func (s *Store) CreateLocalUser(ctx context.Context, input NewLocalUser) (model.User, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.User{}, fmt.Errorf("begin local user: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO users (id, google_subject, email, name, avatar_url, created_at)
		VALUES (?, ?, '', ?, '', ?)`, input.ID, "local:"+input.ID, input.Name, unix(input.CreatedAt))
	if err != nil {
		return model.User{}, fmt.Errorf("create local user: %w", err)
	}
	result, err := tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO local_credentials (user_id, username, password_hash, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)`, input.ID, input.Username, input.PasswordHash, unix(input.CreatedAt), unix(input.CreatedAt))
	if err != nil {
		return model.User{}, fmt.Errorf("create local credential: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return model.User{}, ErrUsernameTaken
	}
	for _, hash := range input.RecoveryCodeHashes {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO recovery_codes (user_id, code_hash, created_at) VALUES (?, ?, ?)`,
			input.ID, hash, unix(input.CreatedAt)); err != nil {
			return model.User{}, fmt.Errorf("create recovery code: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO sessions (token_hash, user_id, expires_at, created_at) VALUES (?, ?, ?, ?)`,
		input.SessionTokenHash, input.ID, unix(input.SessionExpiresAt), unix(input.CreatedAt)); err != nil {
		return model.User{}, fmt.Errorf("create initial session: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return model.User{}, fmt.Errorf("commit local user: %w", err)
	}
	return model.User{ID: input.ID, Name: input.Name, CreatedAt: input.CreatedAt.UTC()}, nil
}

func (s *Store) LocalCredentialByUsername(ctx context.Context, username string) (model.LocalCredential, error) {
	const query = `
		SELECT u.id, u.google_subject, u.email, u.name, u.avatar_url, u.created_at,
			c.username, c.password_hash, c.created_at, c.updated_at
		FROM local_credentials c JOIN users u ON u.id = c.user_id
		WHERE c.username = ?`
	var credential model.LocalCredential
	var userCreated, credentialCreated, updated int64
	err := s.db.QueryRowContext(ctx, query, username).Scan(
		&credential.User.ID, &credential.User.GoogleSubject, &credential.User.Email, &credential.User.Name,
		&credential.User.AvatarURL, &userCreated, &credential.Username, &credential.PasswordHash,
		&credentialCreated, &updated,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return model.LocalCredential{}, ErrNotFound
	}
	if err != nil {
		return model.LocalCredential{}, fmt.Errorf("load local credential: %w", err)
	}
	credential.User.CreatedAt = fromUnix(userCreated)
	credential.CreatedAt = fromUnix(credentialCreated)
	credential.UpdatedAt = fromUnix(updated)
	return credential, nil
}

func (s *Store) RecoverLocalUser(ctx context.Context, username string, recoveryCodeHash []byte, passwordHash string, replacementCodeHashes [][]byte, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin local recovery: %w", err)
	}
	defer tx.Rollback()

	var userID string
	if err := tx.QueryRowContext(ctx, `SELECT user_id FROM local_credentials WHERE username = ?`, username).Scan(&userID); errors.Is(err, sql.ErrNoRows) {
		return ErrRecoveryCode
	} else if err != nil {
		return fmt.Errorf("load recovery account: %w", err)
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM recovery_codes WHERE user_id = ? AND code_hash = ?`, userID, recoveryCodeHash)
	if err != nil {
		return fmt.Errorf("consume recovery code: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ErrRecoveryCode
	}
	if _, err := tx.ExecContext(ctx, `UPDATE local_credentials SET password_hash = ?, updated_at = ? WHERE user_id = ?`, passwordHash, unix(now), userID); err != nil {
		return fmt.Errorf("replace local password: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("invalidate recovered sessions: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM recovery_codes WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("rotate recovery codes: %w", err)
	}
	for _, hash := range replacementCodeHashes {
		if _, err := tx.ExecContext(ctx, `INSERT INTO recovery_codes (user_id, code_hash, created_at) VALUES (?, ?, ?)`, userID, hash, unix(now)); err != nil {
			return fmt.Errorf("create replacement recovery code: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit local recovery: %w", err)
	}
	return nil
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

func (s *Store) UpdatePartyOpenAI(ctx context.Context, partyID, projectID, serviceAccountID, apiKeyID, keyCiphertext, status string) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE parties SET openai_project_id = ?, openai_service_account_id = ?,
			openai_api_key_id = ?, openai_key_ciphertext = ?, openai_status = ? WHERE id = ?`,
		projectID, serviceAccountID, apiKeyID, keyCiphertext, status, partyID,
	)
	if err != nil {
		return fmt.Errorf("update party OpenAI configuration: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ErrNotFound
	}
	return nil
}

// StartPartyOpenAIKeyRotation atomically installs the fresh encrypted key and
// pauses AI-powered routing. Matching the previous key ID prevents concurrent
// host requests from both becoming authoritative.
func (s *Store) StartPartyOpenAIKeyRotation(ctx context.Context, partyID, hostUserID, previousKeyID, freshKeyID, freshKeyCiphertext string) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE parties SET openai_api_key_id = ?, openai_key_ciphertext = ?, openai_status = 'rotating'
		WHERE id = ? AND host_user_id = ? AND COALESCE(openai_api_key_id, '') = ?
			AND openai_status = 'ready' AND openai_project_id <> '' AND openai_service_account_id <> ''`,
		freshKeyID, freshKeyCiphertext, partyID, hostUserID, previousKeyID,
	)
	if err != nil {
		return fmt.Errorf("start party OpenAI key rotation: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ErrOpenAIRotation
	}
	return nil
}

func (s *Store) SetPartyOpenAIKeyRotationStatus(ctx context.Context, partyID, hostUserID, currentKeyID, status string) error {
	if status != "ready" && status != "rotation-error" {
		return errors.New("invalid OpenAI key rotation status")
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE parties SET openai_status = ?
		WHERE id = ? AND host_user_id = ? AND openai_api_key_id = ?
			AND openai_status IN ('rotating', 'rotation-error')`,
		status, partyID, hostUserID, currentKeyID,
	)
	if err != nil {
		return fmt.Errorf("finish party OpenAI key rotation: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ErrOpenAIRotation
	}
	return nil
}

func (s *Store) PartyServices(ctx context.Context, partyID string) (model.PartyServices, error) {
	var services model.PartyServices
	var timeEnabled, weatherEnabled, radioEnabled, aiEnabled int
	var updated int64
	err := s.db.QueryRowContext(ctx, `
		SELECT party_id, time_enabled, weather_enabled, weather_query, weather_label,
			weather_latitude, weather_longitude, radio_enabled, ai_enabled, updated_at
		FROM party_services WHERE party_id = ?`, partyID).Scan(
		&services.PartyID, &timeEnabled, &weatherEnabled, &services.WeatherQuery, &services.WeatherLabel,
		&services.WeatherLatitude, &services.WeatherLongitude, &radioEnabled, &aiEnabled, &updated,
	)
	if errors.Is(err, sql.ErrNoRows) {
		if _, err := s.partyByID(ctx, partyID); err != nil {
			return model.PartyServices{}, err
		}
		return model.PartyServices{PartyID: partyID, TimeEnabled: true}, nil
	}
	if err != nil {
		return model.PartyServices{}, fmt.Errorf("load party services: %w", err)
	}
	services.TimeEnabled = timeEnabled != 0
	services.WeatherEnabled = weatherEnabled != 0
	services.RadioEnabled = radioEnabled != 0
	services.AIEnabled = aiEnabled != 0
	services.UpdatedAt = fromUnix(updated)
	return services, nil
}

func (s *Store) UpdatePartyServices(ctx context.Context, partyID, hostUserID string, input ServiceSettingsInput) (model.PartyServices, error) {
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO party_services (
			party_id, time_enabled, weather_enabled, weather_query, weather_label,
			weather_latitude, weather_longitude, radio_enabled, ai_enabled, updated_at
		)
		SELECT id, ?, ?, ?, ?, ?, ?, ?, ?, ? FROM parties WHERE id = ? AND host_user_id = ?
		ON CONFLICT(party_id) DO UPDATE SET
			time_enabled = excluded.time_enabled,
			weather_enabled = excluded.weather_enabled,
			weather_query = excluded.weather_query,
			weather_label = excluded.weather_label,
			weather_latitude = excluded.weather_latitude,
			weather_longitude = excluded.weather_longitude,
			radio_enabled = excluded.radio_enabled,
			ai_enabled = excluded.ai_enabled,
			updated_at = excluded.updated_at`,
		boolInt(input.TimeEnabled), boolInt(input.WeatherEnabled), input.WeatherQuery, input.WeatherLabel,
		input.WeatherLatitude, input.WeatherLongitude, boolInt(input.RadioEnabled), boolInt(input.AIEnabled), unix(input.UpdatedAt), partyID, hostUserID,
	)
	if err != nil {
		return model.PartyServices{}, fmt.Errorf("update party services: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return model.PartyServices{}, ErrNotFound
	}
	return model.PartyServices{
		PartyID: partyID, TimeEnabled: input.TimeEnabled, WeatherEnabled: input.WeatherEnabled,
		WeatherQuery: input.WeatherQuery, WeatherLabel: input.WeatherLabel,
		WeatherLatitude: input.WeatherLatitude, WeatherLongitude: input.WeatherLongitude,
		RadioEnabled: input.RadioEnabled, AIEnabled: input.AIEnabled, UpdatedAt: input.UpdatedAt.UTC(),
	}, nil
}

func (s *Store) PartyVoiceSettings(ctx context.Context, partyID string) (model.Party, model.PartyServices, error) {
	party, err := s.partyByID(ctx, partyID)
	if err != nil {
		return model.Party{}, model.PartyServices{}, err
	}
	services, err := s.PartyServices(ctx, partyID)
	return party, services, err
}

func (s *Store) ListPartiesByHost(ctx context.Context, hostUserID string) ([]model.Party, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, slug, host_user_id, openai_project_id, openai_service_account_id,
			openai_api_key_id, openai_key_ciphertext, openai_status, created_at
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
			openai_api_key_id, openai_key_ciphertext, openai_status, created_at
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
			p.openai_service_account_id, p.openai_api_key_id, p.openai_key_ciphertext, p.openai_status, p.created_at,
			i.expires_at, i.used_at
		FROM invitations i JOIN parties p ON p.id = i.party_id
		WHERE i.token_hash = ?`, tokenHash)
	var party model.Party
	var projectID, serviceID, apiKeyID, keyCipher, status sql.NullString
	var created, expires int64
	var used sql.NullInt64
	err := row.Scan(&party.ID, &party.Name, &party.Slug, &party.HostUserID, &projectID, &serviceID,
		&apiKeyID, &keyCipher, &status, &created, &expires, &used)
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
	party.OpenAIAPIKeyID = apiKeyID.String
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
	if err := replaceProvisioningTokenTx(ctx, tx, input.DeviceID, input.Provisioning); err != nil {
		return model.Party{}, model.Member{}, model.Device{}, err
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

// ChangeMemberExtensionByDevice lets an authenticated, active SIP endpoint
// change the extension of its own member inside the supplied party. The
// endpoint identity comes from Asterisk's authenticated channel, not caller ID.
func (s *Store) ChangeMemberExtensionByDevice(ctx context.Context, partyID, sipUsername, extension string) error {
	if !validExtensionValue(extension) {
		return ErrInvalidExtension
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE members SET extension = ?
		WHERE party_id = ? AND id = (
			SELECT member_id FROM devices
			WHERE sip_username = ? AND revoked_at IS NULL
		)`, extension, partyID, sipUsername)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return ErrExtensionTaken
		}
		return fmt.Errorf("change member extension by device: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ErrNotFound
	}
	return nil
}

func validExtensionValue(extension string) bool {
	if len(extension) < 2 || len(extension) > 5 {
		return false
	}
	for _, character := range extension {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func (s *Store) MemberForHost(ctx context.Context, partyID, hostUserID, memberID string) (model.Party, model.Member, error) {
	party, err := s.PartyForHost(ctx, partyID, hostUserID)
	if err != nil {
		return model.Party{}, model.Member{}, err
	}
	var member model.Member
	var created int64
	err = s.db.QueryRowContext(ctx, `
		SELECT id, party_id, display_name, extension, created_at
		FROM members WHERE id = ? AND party_id = ?`, memberID, partyID).Scan(
		&member.ID, &member.PartyID, &member.DisplayName, &member.Extension, &created,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Party{}, model.Member{}, ErrNotFound
	}
	if err != nil {
		return model.Party{}, model.Member{}, fmt.Errorf("load hosted member: %w", err)
	}
	member.CreatedAt = fromUnix(created)
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, member_id, label, sip_username, created_at, revoked_at
		FROM devices WHERE member_id = ? ORDER BY created_at`, memberID)
	if err != nil {
		return model.Party{}, model.Member{}, fmt.Errorf("list hosted member devices: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var device model.Device
		var deviceCreated int64
		var revoked sql.NullInt64
		if err := rows.Scan(&device.ID, &device.MemberID, &device.Label, &device.SIPUsername, &deviceCreated, &revoked); err != nil {
			return model.Party{}, model.Member{}, fmt.Errorf("scan hosted member device: %w", err)
		}
		device.CreatedAt = fromUnix(deviceCreated)
		if revoked.Valid {
			value := fromUnix(revoked.Int64)
			device.RevokedAt = &value
		}
		member.Devices = append(member.Devices, device)
	}
	if err := rows.Err(); err != nil {
		return model.Party{}, model.Member{}, fmt.Errorf("read hosted member devices: %w", err)
	}
	return party, member, nil
}

func (s *Store) DeleteMember(ctx context.Context, partyID, hostUserID, memberID string) error {
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM members
		WHERE id = ? AND party_id = ?
		AND EXISTS (SELECT 1 FROM parties WHERE id = ? AND host_user_id = ?)`,
		memberID, partyID, partyID, hostUserID,
	)
	if err != nil {
		return fmt.Errorf("delete hosted member: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteParty(ctx context.Context, partyID, hostUserID string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM parties WHERE id = ? AND host_user_id = ?`, partyID, hostUserID)
	if err != nil {
		return fmt.Errorf("delete hosted party: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteUserWithoutParties(ctx context.Context, userID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin user deletion: %w", err)
	}
	defer tx.Rollback()
	var parties int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM parties WHERE host_user_id = ?`, userID).Scan(&parties); err != nil {
		return fmt.Errorf("count hosted parties: %w", err)
	}
	if parties != 0 {
		return ErrPartiesRemain
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, userID)
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ErrNotFound
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit user deletion: %w", err)
	}
	return nil
}

// RotateDevice replaces a device's registration identity and re-enables it if
// it was previously revoked. Both the party and device ownership checks happen
// inside the same transaction as the update.
func (s *Store) RotateDevice(ctx context.Context, partyID, hostUserID, deviceID, sipUsername, secretCiphertext string, provisioning NewProvisioningToken) (RotatedDevice, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return RotatedDevice{}, fmt.Errorf("begin device rotation: %w", err)
	}
	defer tx.Rollback()

	party, member, device, err := deviceForHostTx(ctx, tx, partyID, hostUserID, deviceID)
	if err != nil {
		return RotatedDevice{}, err
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE devices
		SET sip_username = ?, sip_secret_ciphertext = ?, revoked_at = NULL
		WHERE id = ?`, sipUsername, secretCiphertext, deviceID)
	if err != nil {
		return RotatedDevice{}, fmt.Errorf("rotate device credentials: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return RotatedDevice{}, ErrNotFound
	}
	if err := replaceProvisioningTokenTx(ctx, tx, deviceID, provisioning); err != nil {
		return RotatedDevice{}, err
	}
	if err := tx.Commit(); err != nil {
		return RotatedDevice{}, fmt.Errorf("commit device rotation: %w", err)
	}

	device.SIPUsername = sipUsername
	device.SIPSecretCiphertext = secretCiphertext
	device.RevokedAt = nil
	return RotatedDevice{Party: party, Member: member, Device: device}, nil
}

func (s *Store) RevokeDevice(ctx context.Context, partyID, hostUserID, deviceID string, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin device revocation: %w", err)
	}
	defer tx.Rollback()

	if _, _, _, err := deviceForHostTx(ctx, tx, partyID, hostUserID, deviceID); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE devices SET revoked_at = ? WHERE id = ?`, unix(now), deviceID)
	if err != nil {
		return fmt.Errorf("revoke device: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM device_provisioning_tokens WHERE device_id = ?`, deviceID); err != nil {
		return fmt.Errorf("remove revoked device provisioning: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit device revocation: %w", err)
	}
	return nil
}

func (s *Store) ConsumeProvisioningToken(ctx context.Context, tokenHash []byte, now time.Time) (ProvisioningDevice, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ProvisioningDevice{}, fmt.Errorf("begin provisioning claim: %w", err)
	}
	defer tx.Rollback()

	var provision ProvisioningDevice
	var expires int64
	var used, revoked sql.NullInt64
	err = tx.QueryRowContext(ctx, `
		SELECT d.id, d.sip_username, d.sip_secret_ciphertext, m.extension,
			t.expires_at, t.used_at, d.revoked_at
		FROM device_provisioning_tokens t
		JOIN devices d ON d.id = t.device_id
		JOIN members m ON m.id = d.member_id
		WHERE t.token_hash = ?`, tokenHash).Scan(
		&provision.DeviceID, &provision.SIPUsername, &provision.SIPSecretCiphertext,
		&provision.Extension, &expires, &used, &revoked,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ProvisioningDevice{}, ErrNotFound
	}
	if err != nil {
		return ProvisioningDevice{}, fmt.Errorf("load provisioning claim: %w", err)
	}
	if used.Valid || revoked.Valid {
		return ProvisioningDevice{}, ErrProvisionUsed
	}
	if !fromUnix(expires).After(now) {
		return ProvisioningDevice{}, ErrProvisionExpired
	}
	result, err := tx.ExecContext(ctx, `UPDATE device_provisioning_tokens SET used_at = ? WHERE token_hash = ? AND used_at IS NULL`, unix(now), tokenHash)
	if err != nil {
		return ProvisioningDevice{}, fmt.Errorf("consume provisioning claim: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ProvisioningDevice{}, ErrProvisionUsed
	}
	if err := tx.Commit(); err != nil {
		return ProvisioningDevice{}, fmt.Errorf("commit provisioning claim: %w", err)
	}
	return provision, nil
}

func replaceProvisioningTokenTx(ctx context.Context, tx *sql.Tx, deviceID string, input NewProvisioningToken) error {
	if len(input.TokenHash) != 32 || !input.ExpiresAt.After(input.CreatedAt) {
		return errors.New("invalid provisioning token")
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM device_provisioning_tokens WHERE device_id = ?`, deviceID); err != nil {
		return fmt.Errorf("replace device provisioning: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO device_provisioning_tokens (token_hash, device_id, expires_at, created_at)
		VALUES (?, ?, ?, ?)`, input.TokenHash, deviceID, unix(input.ExpiresAt), unix(input.CreatedAt)); err != nil {
		return fmt.Errorf("create device provisioning: %w", err)
	}
	return nil
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

func (s *Store) RoutingServices(ctx context.Context) ([]model.RoutingServices, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.id,
			COALESCE(s.time_enabled, 1),
			CASE WHEN COALESCE(s.weather_enabled, 0) = 1 AND p.openai_status = 'ready' THEN 1 ELSE 0 END,
			COALESCE(s.radio_enabled, 0),
			CASE WHEN COALESCE(s.ai_enabled, 0) = 1 AND p.openai_status = 'ready' THEN 1 ELSE 0 END
		FROM parties p LEFT JOIN party_services s ON s.party_id = p.id
		ORDER BY p.id`)
	if err != nil {
		return nil, fmt.Errorf("list routing services: %w", err)
	}
	defer rows.Close()
	var services []model.RoutingServices
	for rows.Next() {
		var item model.RoutingServices
		var timeEnabled, weatherEnabled, radioEnabled, aiEnabled int
		if err := rows.Scan(&item.PartyID, &timeEnabled, &weatherEnabled, &radioEnabled, &aiEnabled); err != nil {
			return nil, fmt.Errorf("scan routing services: %w", err)
		}
		item.TimeEnabled = timeEnabled != 0
		item.WeatherEnabled = weatherEnabled != 0
		item.RadioEnabled = radioEnabled != 0
		item.AIEnabled = aiEnabled != 0
		services = append(services, item)
	}
	return services, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanParty(row scanner) (model.Party, error) {
	var party model.Party
	var projectID, serviceID, apiKeyID, keyCipher, status sql.NullString
	var created int64
	if err := row.Scan(&party.ID, &party.Name, &party.Slug, &party.HostUserID, &projectID,
		&serviceID, &apiKeyID, &keyCipher, &status, &created); err != nil {
		return model.Party{}, err
	}
	party.OpenAIProjectID = projectID.String
	party.OpenAIServiceAccountID = serviceID.String
	party.OpenAIAPIKeyID = apiKeyID.String
	party.OpenAIKeyCiphertext = keyCipher.String
	party.OpenAIStatus = status.String
	party.CreatedAt = fromUnix(created)
	return party, nil
}

func partyByIDTx(ctx context.Context, tx *sql.Tx, partyID string) (model.Party, error) {
	return scanParty(tx.QueryRowContext(ctx, `
		SELECT id, name, slug, host_user_id, openai_project_id, openai_service_account_id,
			openai_api_key_id, openai_key_ciphertext, openai_status, created_at FROM parties WHERE id = ?`, partyID))
}

func (s *Store) partyByID(ctx context.Context, partyID string) (model.Party, error) {
	party, err := scanParty(s.db.QueryRowContext(ctx, `
		SELECT id, name, slug, host_user_id, openai_project_id, openai_service_account_id,
			openai_api_key_id, openai_key_ciphertext, openai_status, created_at FROM parties WHERE id = ?`, partyID))
	if errors.Is(err, sql.ErrNoRows) {
		return model.Party{}, ErrNotFound
	}
	return party, err
}

func deviceForHostTx(ctx context.Context, tx *sql.Tx, partyID, hostUserID, deviceID string) (model.Party, model.Member, model.Device, error) {
	party, err := scanParty(tx.QueryRowContext(ctx, `
		SELECT id, name, slug, host_user_id, openai_project_id,
			openai_service_account_id, openai_api_key_id, openai_key_ciphertext, openai_status, created_at
		FROM parties WHERE id = ? AND host_user_id = ?`, partyID, hostUserID))
	if errors.Is(err, sql.ErrNoRows) {
		return model.Party{}, model.Member{}, model.Device{}, ErrNotFound
	}
	if err != nil {
		return model.Party{}, model.Member{}, model.Device{}, fmt.Errorf("load device party: %w", err)
	}

	var member model.Member
	var device model.Device
	var memberCreated, deviceCreated int64
	var revoked sql.NullInt64
	err = tx.QueryRowContext(ctx, `
		SELECT m.id, m.party_id, m.display_name, m.extension, m.created_at,
			d.id, d.member_id, d.label, d.sip_username, d.sip_secret_ciphertext, d.created_at, d.revoked_at
		FROM devices d JOIN members m ON m.id = d.member_id
		WHERE d.id = ? AND m.party_id = ?`, deviceID, partyID).Scan(
		&member.ID, &member.PartyID, &member.DisplayName, &member.Extension, &memberCreated,
		&device.ID, &device.MemberID, &device.Label, &device.SIPUsername, &device.SIPSecretCiphertext, &deviceCreated, &revoked,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Party{}, model.Member{}, model.Device{}, ErrNotFound
	}
	if err != nil {
		return model.Party{}, model.Member{}, model.Device{}, fmt.Errorf("load hosted device: %w", err)
	}
	member.CreatedAt = fromUnix(memberCreated)
	device.CreatedAt = fromUnix(deviceCreated)
	if revoked.Valid {
		value := fromUnix(revoked.Int64)
		device.RevokedAt = &value
	}
	return party, member, device, nil
}

func unix(value time.Time) int64     { return value.UTC().Unix() }
func fromUnix(value int64) time.Time { return time.Unix(value, 0).UTC() }
func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

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

CREATE TABLE IF NOT EXISTS local_credentials (
    user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    username TEXT NOT NULL UNIQUE COLLATE NOCASE,
    password_hash TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS recovery_codes (
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code_hash BLOB NOT NULL,
    created_at INTEGER NOT NULL,
    PRIMARY KEY (user_id, code_hash)
);

CREATE TABLE IF NOT EXISTS parties (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    slug TEXT NOT NULL UNIQUE,
    host_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    openai_project_id TEXT,
    openai_service_account_id TEXT,
    openai_api_key_id TEXT,
    openai_key_ciphertext TEXT,
    openai_status TEXT NOT NULL DEFAULT 'pending',
    created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS parties_host ON parties(host_user_id);

CREATE TABLE IF NOT EXISTS party_services (
    party_id TEXT PRIMARY KEY REFERENCES parties(id) ON DELETE CASCADE,
    time_enabled INTEGER NOT NULL DEFAULT 1 CHECK(time_enabled IN (0, 1)),
    weather_enabled INTEGER NOT NULL DEFAULT 0 CHECK(weather_enabled IN (0, 1)),
    weather_query TEXT NOT NULL DEFAULT '',
    weather_label TEXT NOT NULL DEFAULT '',
    weather_latitude REAL NOT NULL DEFAULT 0,
    weather_longitude REAL NOT NULL DEFAULT 0,
	radio_enabled INTEGER NOT NULL DEFAULT 0 CHECK(radio_enabled IN (0, 1)),
	ai_enabled INTEGER NOT NULL DEFAULT 0 CHECK(ai_enabled IN (0, 1)),
	updated_at INTEGER NOT NULL
);

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

CREATE TABLE IF NOT EXISTS device_provisioning_tokens (
    token_hash BLOB PRIMARY KEY,
    device_id TEXT NOT NULL UNIQUE REFERENCES devices(id) ON DELETE CASCADE,
    expires_at INTEGER NOT NULL,
    used_at INTEGER,
    created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS device_provisioning_expires ON device_provisioning_tokens(expires_at);
`
