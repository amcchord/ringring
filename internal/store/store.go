package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	extensionrules "github.com/amcchord/ringring/internal/extension"
	"github.com/amcchord/ringring/internal/model"
	"github.com/amcchord/ringring/internal/radio"
	_ "modernc.org/sqlite"
)

var (
	ErrNotFound         = errors.New("not found")
	ErrInviteUsed       = errors.New("invitation has already been used")
	ErrInviteExpired    = errors.New("invitation has expired")
	ErrInvalidExtension = errors.New("extension must contain 2 to 5 digits and not be a reserved public safety number")
	ErrExtensionTaken   = errors.New("extension is already in use")
	ErrSIPUsernameTaken = errors.New("SIP username is already in use")
	ErrUsernameTaken    = errors.New("username is already in use")
	ErrRecoveryCode     = errors.New("invalid recovery code")
	ErrPartiesRemain    = errors.New("host still owns parties")
	ErrProvisionUsed    = errors.New("provisioning link has already been used")
	ErrProvisionExpired = errors.New("provisioning link has expired")
	ErrDeviceLimit      = errors.New("member device limit reached")
	ErrOpenAIRotation   = errors.New("OpenAI key rotation state changed")
	ErrOpenAISpendLimit = errors.New("OpenAI spend limit state changed")
	ErrInvalidRadio     = errors.New("radio station is not in the catalog")
	ErrInvalidWeather   = errors.New("weather location is invalid")
)

const MaxDevicesPerMember = 8

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

type NewHostedDevice struct {
	PartyID             string
	HostUserID          string
	MemberID            string
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
	PartyID             string
	MemberID            string
	SIPUsername         string
	SIPSecretCiphertext string
	Extension           string
}

type RotatedDevice struct {
	Party  model.Party
	Member model.Member
	Device model.Device
}

type CreatedDevice struct {
	Party  model.Party
	Member model.Member
	Device model.Device
}

type ServiceSettingsInput struct {
	TimeEnabled         bool
	WeatherEnabled      bool
	WeatherSetupAllowed bool
	WeatherQuery        string
	WeatherLabel        string
	WeatherLatitude     float64
	WeatherLongitude    float64
	RadioEnabled        bool
	RadioStation        string
	UpdatedAt           time.Time
}

type WeatherLocationInput struct {
	Query     string
	Label     string
	Latitude  float64
	Longitude float64
	UpdatedAt time.Time
}

type DeviceReadinessInput struct {
	EchoTested         bool
	OutgoingCallTested bool
	IncomingCallTested bool
	UpdatedAt          time.Time
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
	if err := ensureLegacyPartyServicesAIColumn(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("preserve legacy AI service column: %w", err)
	}
	if err := ensurePartyServicesRadioStationColumn(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate radio station setting: %w", err)
	}
	if err := ensurePartyServicesWeatherSetupColumn(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate weather setup setting: %w", err)
	}
	if err := ensurePartyOpenAIAPIKeyColumn(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate OpenAI key identifier: %w", err)
	}
	if err := ensurePartyOpenAISpendLimitColumns(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate OpenAI spend limit state: %w", err)
	}
	if err := ensureLegacyMemberAdultExtensionColumn(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("preserve legacy adult extension column: %w", err)
	}
	if err := ensureMemberWeatherColumns(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate member weather locations: %w", err)
	}
	if err := ensurePhonePushTable(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate phone push registrations: %w", err)
	}
	if err := migrateReservedMemberExtensions(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate reserved member extensions: %w", err)
	}
	return &Store{db: db}, nil
}

func ensureMemberWeatherColumns(db *sql.DB) error {
	columns := []struct {
		name       string
		definition string
	}{
		{"weather_query", "TEXT NOT NULL DEFAULT ''"},
		{"weather_label", "TEXT NOT NULL DEFAULT ''"},
		{"weather_latitude", "REAL NOT NULL DEFAULT 0"},
		{"weather_longitude", "REAL NOT NULL DEFAULT 0"},
		{"weather_updated_at", "INTEGER"},
	}
	missingQuery := false
	for _, column := range columns {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('members') WHERE name = ?`, column.name).Scan(&count); err != nil {
			return err
		}
		if count == 0 {
			if column.name == "weather_query" {
				missingQuery = true
			}
			if _, err := db.Exec(`ALTER TABLE members ADD COLUMN ` + column.name + ` ` + column.definition); err != nil {
				return err
			}
		}
	}
	if !missingQuery {
		return nil
	}
	// Preserve an existing party-wide location for members present at upgrade
	// time. New members start empty and are prompted for their own ZIP.
	_, err := db.Exec(`
		UPDATE members SET
			weather_query = COALESCE((SELECT weather_query FROM party_services WHERE party_id = members.party_id), ''),
			weather_label = COALESCE((SELECT weather_label FROM party_services WHERE party_id = members.party_id), ''),
			weather_latitude = COALESCE((SELECT weather_latitude FROM party_services WHERE party_id = members.party_id), 0),
			weather_longitude = COALESCE((SELECT weather_longitude FROM party_services WHERE party_id = members.party_id), 0),
			weather_updated_at = (SELECT updated_at FROM party_services WHERE party_id = members.party_id)
		WHERE EXISTS (
			SELECT 1 FROM party_services
			WHERE party_id = members.party_id AND TRIM(weather_label) <> ''
		)`)
	return err
}

func ensureLegacyMemberAdultExtensionColumn(db *sql.DB) error {
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('members') WHERE name = 'adult_extension'`).Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return nil
	}
	_, err := db.Exec(`ALTER TABLE members ADD COLUMN adult_extension INTEGER NOT NULL DEFAULT 0 CHECK(adult_extension IN (0, 1))`)
	return err
}

// migrateReservedMemberExtensions repairs values accepted by preview builds
// before the public-safety reservation was centralized. It changes no schema,
// is idempotent, and assigns only an unoccupied ordinary number in that party.
func migrateReservedMemberExtensions(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.Query(`SELECT id, party_id, extension FROM members ORDER BY party_id, created_at, id`)
	if err != nil {
		return err
	}
	type memberExtension struct {
		id, partyID, value string
	}
	var members []memberExtension
	usedByParty := make(map[string][]string)
	for rows.Next() {
		var member memberExtension
		if err := rows.Scan(&member.id, &member.partyID, &member.value); err != nil {
			_ = rows.Close()
			return err
		}
		members = append(members, member)
		usedByParty[member.partyID] = append(usedByParty[member.partyID], member.value)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, member := range members {
		if !extensionrules.Reserved(member.value) {
			continue
		}
		replacement := extensionrules.Suggest(usedByParty[member.partyID])
		if replacement == "" {
			return errors.New("no extension remains for reserved-number migration")
		}
		if _, err := tx.Exec(`UPDATE members SET extension = ? WHERE id = ? AND party_id = ?`, replacement, member.id, member.partyID); err != nil {
			return err
		}
		usedByParty[member.partyID] = append(usedByParty[member.partyID], replacement)
	}
	return tx.Commit()
}

func ensurePartyOpenAISpendLimitColumns(db *sql.DB) error {
	columns := []struct {
		name       string
		definition string
	}{
		{"openai_spend_limit_cents", "INTEGER NOT NULL DEFAULT 0 CHECK(openai_spend_limit_cents >= 0)"},
		{"openai_spend_pending_cents", "INTEGER CHECK(openai_spend_pending_cents IS NULL OR openai_spend_pending_cents > 0)"},
		{"openai_spend_limit_status", "TEXT NOT NULL DEFAULT 'unknown'"},
	}
	for _, column := range columns {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('parties') WHERE name = ?`, column.name).Scan(&count); err != nil {
			return err
		}
		if count == 0 {
			if _, err := db.Exec(`ALTER TABLE parties ADD COLUMN ` + column.name + ` ` + column.definition); err != nil {
				return err
			}
		}
	}
	return nil
}

func ensurePartyServicesRadioStationColumn(db *sql.DB) error {
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
		if name == "radio_station" {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if found {
		return nil
	}
	_, err = db.Exec(`ALTER TABLE party_services ADD COLUMN radio_station TEXT NOT NULL DEFAULT 'groove-salad'`)
	return err
}

func ensurePartyServicesWeatherSetupColumn(db *sql.DB) error {
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('party_services') WHERE name = 'weather_setup_allowed'`).Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return nil
	}
	_, err := db.Exec(`ALTER TABLE party_services ADD COLUMN weather_setup_allowed INTEGER NOT NULL DEFAULT 1 CHECK(weather_setup_allowed IN (0, 1))`)
	return err
}

func ensureLegacyPartyServicesAIColumn(db *sql.DB) error {
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

func (s *Store) UpdatePartyOpenAI(ctx context.Context, partyID, projectID, serviceAccountID, apiKeyID, keyCiphertext, status string, spendLimitCents int) error {
	spendStatus := "unknown"
	if status == "ready" && spendLimitCents > 0 {
		spendStatus = "ready"
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE parties SET openai_project_id = ?, openai_service_account_id = ?,
			openai_api_key_id = ?, openai_key_ciphertext = ?, openai_status = ?,
			openai_spend_limit_cents = ?, openai_spend_pending_cents = NULL,
			openai_spend_limit_status = ? WHERE id = ?`,
		projectID, serviceAccountID, apiKeyID, keyCiphertext, status, spendLimitCents, spendStatus, partyID,
	)
	if err != nil {
		return fmt.Errorf("update party OpenAI configuration: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ErrNotFound
	}
	return nil
}

// StartPartyOpenAISpendLimitUpdate records one immutable pending amount and
// pauses AI-powered routing before any provider mutation is attempted.
func (s *Store) StartPartyOpenAISpendLimitUpdate(ctx context.Context, partyID, hostUserID, projectID string, cents int) error {
	if cents < 1 {
		return errors.New("OpenAI spend limit must be positive")
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE parties SET openai_spend_pending_cents = ?, openai_spend_limit_status = 'updating',
			openai_status = 'spend-updating'
		WHERE id = ? AND host_user_id = ? AND openai_project_id = ? AND openai_status = 'ready'
			AND openai_spend_pending_cents IS NULL AND openai_spend_limit_status IN ('ready', 'unknown')`,
		cents, partyID, hostUserID, projectID,
	)
	if err != nil {
		return fmt.Errorf("start party OpenAI spend limit update: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ErrOpenAISpendLimit
	}
	return nil
}

func (s *Store) SetPartyOpenAISpendLimitError(ctx context.Context, partyID, hostUserID, projectID string, cents int) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE parties SET openai_spend_limit_status = 'update-error', openai_status = 'spend-update-error'
		WHERE id = ? AND host_user_id = ? AND openai_project_id = ?
			AND openai_spend_pending_cents = ? AND openai_spend_limit_status IN ('updating', 'update-error')
			AND openai_status IN ('spend-updating', 'spend-update-error')`,
		partyID, hostUserID, projectID, cents,
	)
	if err != nil {
		return fmt.Errorf("record party OpenAI spend limit error: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ErrOpenAISpendLimit
	}
	return nil
}

func (s *Store) FinishPartyOpenAISpendLimitUpdate(ctx context.Context, partyID, hostUserID, projectID string, cents int) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE parties SET openai_spend_limit_cents = ?, openai_spend_pending_cents = NULL,
			openai_spend_limit_status = 'ready', openai_status = 'ready'
		WHERE id = ? AND host_user_id = ? AND openai_project_id = ?
			AND openai_spend_pending_cents = ? AND openai_spend_limit_status IN ('updating', 'update-error')
			AND openai_status IN ('spend-updating', 'spend-update-error')`,
		cents, partyID, hostUserID, projectID, cents,
	)
	if err != nil {
		return fmt.Errorf("finish party OpenAI spend limit update: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ErrOpenAISpendLimit
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
	var timeEnabled, weatherEnabled, weatherSetupAllowed, radioEnabled int
	var updated int64
	err := s.db.QueryRowContext(ctx, `
		SELECT party_id, time_enabled, weather_enabled, weather_setup_allowed, weather_query, weather_label,
			weather_latitude, weather_longitude, radio_enabled, radio_station, updated_at
		FROM party_services WHERE party_id = ?`, partyID).Scan(
		&services.PartyID, &timeEnabled, &weatherEnabled, &weatherSetupAllowed, &services.WeatherQuery, &services.WeatherLabel,
		&services.WeatherLatitude, &services.WeatherLongitude, &radioEnabled, &services.RadioStation, &updated,
	)
	if errors.Is(err, sql.ErrNoRows) {
		if _, err := s.partyByID(ctx, partyID); err != nil {
			return model.PartyServices{}, err
		}
		return model.PartyServices{PartyID: partyID, TimeEnabled: true, WeatherSetupAllowed: true, RadioStation: radio.DefaultStationID}, nil
	}
	if err != nil {
		return model.PartyServices{}, fmt.Errorf("load party services: %w", err)
	}
	services.TimeEnabled = timeEnabled != 0
	services.WeatherEnabled = weatherEnabled != 0
	services.WeatherSetupAllowed = weatherSetupAllowed != 0
	services.RadioEnabled = radioEnabled != 0
	services.UpdatedAt = fromUnix(updated)
	return services, nil
}

func (s *Store) UpdatePartyServices(ctx context.Context, partyID, hostUserID string, input ServiceSettingsInput) (model.PartyServices, error) {
	station, ok := radio.Resolve(input.RadioStation)
	if !ok {
		return model.PartyServices{}, ErrInvalidRadio
	}
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO party_services (
			party_id, time_enabled, weather_enabled, weather_setup_allowed, weather_query, weather_label,
			weather_latitude, weather_longitude, radio_enabled, radio_station, ai_enabled, updated_at
		)
		SELECT id, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ? FROM parties WHERE id = ? AND host_user_id = ?
		ON CONFLICT(party_id) DO UPDATE SET
			time_enabled = excluded.time_enabled,
			weather_enabled = excluded.weather_enabled,
			weather_setup_allowed = excluded.weather_setup_allowed,
			weather_query = excluded.weather_query,
			weather_label = excluded.weather_label,
			weather_latitude = excluded.weather_latitude,
			weather_longitude = excluded.weather_longitude,
			radio_enabled = excluded.radio_enabled,
			radio_station = excluded.radio_station,
			ai_enabled = excluded.ai_enabled,
			updated_at = excluded.updated_at`,
		boolInt(input.TimeEnabled), boolInt(input.WeatherEnabled), boolInt(input.WeatherSetupAllowed), input.WeatherQuery, input.WeatherLabel,
		input.WeatherLatitude, input.WeatherLongitude, boolInt(input.RadioEnabled), station.ID, 0, unix(input.UpdatedAt), partyID, hostUserID,
	)
	if err != nil {
		return model.PartyServices{}, fmt.Errorf("update party services: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return model.PartyServices{}, ErrNotFound
	}
	return model.PartyServices{
		PartyID: partyID, TimeEnabled: input.TimeEnabled, WeatherEnabled: input.WeatherEnabled, WeatherSetupAllowed: input.WeatherSetupAllowed,
		WeatherQuery: input.WeatherQuery, WeatherLabel: input.WeatherLabel,
		WeatherLatitude: input.WeatherLatitude, WeatherLongitude: input.WeatherLongitude,
		RadioEnabled: input.RadioEnabled, RadioStation: station.ID, UpdatedAt: input.UpdatedAt.UTC(),
	}, nil
}

// WeatherLocationForDevice resolves an active authenticated SIP endpoint to
// its member-scoped weather location. Caller ID is deliberately not trusted.
func (s *Store) WeatherLocationForDevice(ctx context.Context, partyID, sipUsername string) (model.WeatherLocation, error) {
	var location model.WeatherLocation
	var updated sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
		SELECT m.id, m.weather_query, m.weather_label, m.weather_latitude,
			m.weather_longitude, m.weather_updated_at
		FROM devices d JOIN members m ON m.id = d.member_id
		WHERE m.party_id = ? AND d.sip_username = ? AND d.revoked_at IS NULL`, partyID, sipUsername).Scan(
		&location.MemberID, &location.Query, &location.Label, &location.Latitude, &location.Longitude, &updated,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return model.WeatherLocation{}, ErrNotFound
	}
	if err != nil {
		return model.WeatherLocation{}, fmt.Errorf("load member weather location from phone: %w", err)
	}
	if updated.Valid {
		location.UpdatedAt = fromUnix(updated.Int64)
	}
	return location, nil
}

// SetWeatherLocationByDevice lets an active phone fill its extension's
// otherwise unknown weather location. It never replaces an already-resolved
// location, and it cannot affect another member in the party.
func (s *Store) SetWeatherLocationByDevice(ctx context.Context, partyID, sipUsername string, input WeatherLocationInput) (model.WeatherLocation, bool, error) {
	if !validWeatherLocation(input) {
		return model.WeatherLocation{}, false, ErrInvalidWeather
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE members SET weather_query = ?, weather_label = ?, weather_latitude = ?,
			weather_longitude = ?, weather_updated_at = ?
		WHERE party_id = ? AND TRIM(weather_label) = '' AND id = (
			SELECT member_id FROM devices WHERE sip_username = ? AND revoked_at IS NULL
		) AND EXISTS (
			SELECT 1 FROM party_services
			WHERE party_id = ? AND weather_enabled = 1 AND weather_setup_allowed = 1
		)`,
		input.Query, input.Label, input.Latitude, input.Longitude, unix(input.UpdatedAt), partyID, sipUsername, partyID,
	)
	if err != nil {
		return model.WeatherLocation{}, false, fmt.Errorf("set member weather location from phone: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return model.WeatherLocation{}, false, fmt.Errorf("count member weather location update: %w", err)
	}
	location, err := s.WeatherLocationForDevice(ctx, partyID, sipUsername)
	if err != nil {
		return model.WeatherLocation{}, false, err
	}
	return location, rows == 1, nil
}

// UpdateMemberWeatherLocationForHost replaces or clears one member's location
// after proving both party ownership and member scope in the write itself.
func (s *Store) UpdateMemberWeatherLocationForHost(ctx context.Context, partyID, hostUserID, memberID string, input WeatherLocationInput) error {
	clearing := input.Query == "" && input.Label == ""
	if !clearing && !validWeatherLocation(input) {
		return ErrInvalidWeather
	}
	var updated any
	if !clearing {
		updated = unix(input.UpdatedAt)
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE members SET weather_query = ?, weather_label = ?, weather_latitude = ?,
			weather_longitude = ?, weather_updated_at = ?
		WHERE id = ? AND party_id = ? AND EXISTS (
			SELECT 1 FROM parties WHERE id = ? AND host_user_id = ?
		)`, input.Query, input.Label, input.Latitude, input.Longitude, updated,
		memberID, partyID, partyID, hostUserID)
	if err != nil {
		return fmt.Errorf("update hosted member weather location: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ErrNotFound
	}
	return nil
}

func validWeatherLocation(input WeatherLocationInput) bool {
	query := strings.TrimSpace(input.Query)
	label := strings.TrimSpace(input.Label)
	return query == input.Query && label == input.Label && len(query) >= 2 && len(query) <= 80 && len(label) >= 2 && len(label) <= 120 &&
		strings.IndexFunc(query, unicode.IsControl) == -1 && strings.IndexFunc(label, unicode.IsControl) == -1 &&
		!math.IsNaN(input.Latitude) && !math.IsInf(input.Latitude, 0) && !math.IsNaN(input.Longitude) && !math.IsInf(input.Longitude, 0) &&
		input.Latitude >= -90 && input.Latitude <= 90 && input.Longitude >= -180 && input.Longitude <= 180 && !input.UpdatedAt.IsZero()
}

// DisableLegacyAIConversation clears the retired conversation-line preference.
// The column remains only for forward-only SQLite and rollback compatibility.
func (s *Store) DisableLegacyAIConversation(ctx context.Context, now time.Time) error {
	if _, err := s.db.ExecContext(ctx, `
		UPDATE party_services SET ai_enabled = 0, updated_at = ? WHERE ai_enabled = 1`, unix(now)); err != nil {
		return fmt.Errorf("disable legacy AI conversation: %w", err)
	}
	return nil
}

// PartyMemberForDevice resolves the friendly name used for an active-call
// join announcement from Asterisk's authenticated endpoint. Revoked,
// cross-party, and unknown devices receive the same not-found result.
func (s *Store) PartyMemberForDevice(ctx context.Context, partyID, sipUsername string) (model.Member, error) {
	var member model.Member
	var created int64
	err := s.db.QueryRowContext(ctx, `
		SELECT m.id, m.party_id, m.display_name, m.extension, m.created_at
		FROM devices d
		JOIN members m ON m.id = d.member_id
		WHERE m.party_id = ? AND d.sip_username = ? AND d.revoked_at IS NULL`,
		partyID, sipUsername,
	).Scan(&member.ID, &member.PartyID, &member.DisplayName, &member.Extension, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Member{}, ErrNotFound
	}
	if err != nil {
		return model.Member{}, fmt.Errorf("load party member from phone: %w", err)
	}
	member.CreatedAt = fromUnix(created)
	return member, nil
}

// ListOpenAIProjectIDs returns the provider projects that could supply a party
// model call. The explicit retention audit uses this narrow list; identifiers
// are never logged or rendered.
func (s *Store) ListOpenAIProjectIDs(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT openai_project_id FROM parties
		WHERE openai_project_id IS NOT NULL AND openai_project_id <> ''
		ORDER BY openai_project_id`)
	if err != nil {
		return nil, fmt.Errorf("list OpenAI project IDs: %w", err)
	}
	defer rows.Close()
	var projectIDs []string
	for rows.Next() {
		var projectID string
		if err := rows.Scan(&projectID); err != nil {
			return nil, fmt.Errorf("scan OpenAI project ID: %w", err)
		}
		projectIDs = append(projectIDs, projectID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list OpenAI project IDs: %w", err)
	}
	return projectIDs, nil
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
			openai_api_key_id, openai_key_ciphertext, openai_status,
			openai_spend_limit_cents, openai_spend_pending_cents, openai_spend_limit_status, created_at
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
			openai_api_key_id, openai_key_ciphertext, openai_status,
			openai_spend_limit_cents, openai_spend_pending_cents, openai_spend_limit_status, created_at
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

// ActiveInvitationCountForHost returns only unclaimed links that have not yet
// expired. The host join is part of the query so a party identifier alone
// cannot reveal even an aggregate invitation count.
func (s *Store) ActiveInvitationCountForHost(ctx context.Context, partyID, hostUserID string, now time.Time) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(i.id)
		FROM parties p
		LEFT JOIN invitations i ON i.party_id = p.id AND i.used_at IS NULL AND i.expires_at >= ?
		WHERE p.id = ? AND p.host_user_id = ?
		GROUP BY p.id`, unix(now), partyID, hostUserID).Scan(&count)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("count active host invitations: %w", err)
	}
	return count, nil
}

// CancelActiveInvitationsForHost atomically verifies party ownership and
// removes every unclaimed, unexpired bearer link. Used and expired records are
// retained for their existing lifecycle semantics.
func (s *Store) CancelActiveInvitationsForHost(ctx context.Context, partyID, hostUserID string, now time.Time) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin active invitation cancellation: %w", err)
	}
	defer tx.Rollback()

	var exists int
	err = tx.QueryRowContext(ctx, `SELECT 1 FROM parties WHERE id = ? AND host_user_id = ?`, partyID, hostUserID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("load invitation cancellation party: %w", err)
	}
	result, err := tx.ExecContext(ctx, `
		DELETE FROM invitations
		WHERE party_id = ? AND used_at IS NULL AND expires_at >= ?`, partyID, unix(now))
	if err != nil {
		return 0, fmt.Errorf("cancel active invitations: %w", err)
	}
	removed, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count canceled active invitations: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit active invitation cancellation: %w", err)
	}
	return removed, nil
}

func (s *Store) PartyByInvitation(ctx context.Context, tokenHash []byte, now time.Time) (model.Party, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT p.id, p.name, p.slug, p.host_user_id, p.openai_project_id,
			p.openai_service_account_id, p.openai_api_key_id, p.openai_key_ciphertext, p.openai_status,
			p.openai_spend_limit_cents, p.openai_spend_pending_cents, p.openai_spend_limit_status, p.created_at,
			i.expires_at, i.used_at
		FROM invitations i JOIN parties p ON p.id = i.party_id
		WHERE i.token_hash = ?`, tokenHash)
	var party model.Party
	var projectID, serviceID, apiKeyID, keyCipher, status, spendStatus sql.NullString
	var spendLimit int
	var spendPending sql.NullInt64
	var created, expires int64
	var used sql.NullInt64
	err := row.Scan(&party.ID, &party.Name, &party.Slug, &party.HostUserID, &projectID, &serviceID,
		&apiKeyID, &keyCipher, &status, &spendLimit, &spendPending, &spendStatus, &created, &expires, &used)
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
	party.OpenAISpendLimitCents = spendLimit
	party.OpenAISpendPendingCents = int(spendPending.Int64)
	party.OpenAISpendLimitStatus = spendStatus.String
	party.CreatedAt = fromUnix(created)
	return party, nil
}

func (s *Store) ClaimInvitation(ctx context.Context, input NewClaim) (model.Party, model.Member, model.Device, error) {
	if !extensionrules.Valid(input.Extension) {
		return model.Party{}, model.Member{}, model.Device{}, ErrInvalidExtension
	}
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
		INSERT INTO members (id, party_id, display_name, extension, adult_extension, created_at)
		VALUES (?, ?, ?, ?, 0, ?)`, input.MemberID, partyID, input.DisplayName, input.Extension, unix(input.Now))
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
		if isSIPUsernameConflict(err) {
			return model.Party{}, model.Member{}, model.Device{}, ErrSIPUsernameTaken
		}
		return model.Party{}, model.Member{}, model.Device{}, fmt.Errorf("create device: %w", err)
	}
	// Native phone apps receive their SIP credentials in the successful claim
	// response, so they do not need a second, undisclosed provisioning token.
	// Web claims still pass a token and retain the existing setup-card flow.
	if len(input.Provisioning.TokenHash) != 0 {
		if err := replaceProvisioningTokenTx(ctx, tx, input.DeviceID, input.Provisioning); err != nil {
			return model.Party{}, model.Member{}, model.Device{}, err
		}
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

// SuggestedExtension returns one unoccupied, non-emergency-like number for a
// validated party invitation page. It is only a convenience; ClaimInvitation's
// transaction and unique constraint resolve concurrent claims authoritatively.
func (s *Store) SuggestedExtension(ctx context.Context, partyID string) (string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT extension FROM members WHERE party_id = ?`, partyID)
	if err != nil {
		return "", fmt.Errorf("list extensions for suggestion: %w", err)
	}
	defer rows.Close()
	var used []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return "", fmt.Errorf("scan extension for suggestion: %w", err)
		}
		used = append(used, value)
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("list extensions for suggestion: %w", err)
	}
	return extensionrules.Suggest(used), nil
}

func (s *Store) ListMembers(ctx context.Context, partyID string) ([]model.Member, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.id, m.party_id, m.display_name, m.extension,
			m.weather_query, m.weather_label, m.weather_latitude, m.weather_longitude, m.weather_updated_at, m.created_at,
			d.id, d.label, d.sip_username, d.created_at, d.revoked_at,
			r.echo_tested_at, r.outgoing_call_tested_at, r.incoming_call_tested_at
		FROM members m
		LEFT JOIN devices d ON d.member_id = m.id
		LEFT JOIN device_readiness r ON r.device_id = d.id
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
		var weatherUpdated sql.NullInt64
		var deviceID, deviceLabel, sipUsername sql.NullString
		var deviceCreated, revoked, echoTested, outgoingTested, incomingTested sql.NullInt64
		if err := rows.Scan(&member.ID, &member.PartyID, &member.DisplayName, &member.Extension,
			&member.Weather.Query, &member.Weather.Label, &member.Weather.Latitude, &member.Weather.Longitude, &weatherUpdated, &memberCreated,
			&deviceID, &deviceLabel, &sipUsername, &deviceCreated, &revoked,
			&echoTested, &outgoingTested, &incomingTested); err != nil {
			return nil, fmt.Errorf("scan member: %w", err)
		}
		index, ok := byID[member.ID]
		if !ok {
			member.Weather.MemberID = member.ID
			if weatherUpdated.Valid {
				member.Weather.UpdatedAt = fromUnix(weatherUpdated.Int64)
			}
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
			device.Readiness = readinessFromNulls(echoTested, outgoingTested, incomingTested)
			members[index].Devices = append(members[index].Devices, device)
		}
	}
	return members, rows.Err()
}

// UpdateDeviceReadiness records only the host's yes/no setup confirmations.
// It deliberately stores no call peer, audio, address, contact, or user-agent
// data. An unchecked item is cleared; an already-checked item's original
// confirmation time is retained.
func (s *Store) UpdateDeviceReadiness(ctx context.Context, partyID, hostUserID, deviceID string, input DeviceReadinessInput) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin device readiness update: %w", err)
	}
	defer tx.Rollback()

	_, _, device, err := deviceForHostTx(ctx, tx, partyID, hostUserID, deviceID)
	if err != nil {
		return err
	}
	if device.RevokedAt != nil {
		return ErrNotFound
	}
	if !input.EchoTested && !input.OutgoingCallTested && !input.IncomingCallTested {
		if _, err := tx.ExecContext(ctx, `DELETE FROM device_readiness WHERE device_id = ?`, deviceID); err != nil {
			return fmt.Errorf("clear device readiness: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit cleared device readiness: %w", err)
		}
		return nil
	}
	now := unix(input.UpdatedAt)
	_, err = tx.ExecContext(ctx, `
		INSERT INTO device_readiness (
			device_id, echo_tested_at, outgoing_call_tested_at, incoming_call_tested_at, updated_at
		) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(device_id) DO UPDATE SET
			echo_tested_at = CASE WHEN excluded.echo_tested_at IS NULL THEN NULL ELSE COALESCE(device_readiness.echo_tested_at, excluded.echo_tested_at) END,
			outgoing_call_tested_at = CASE WHEN excluded.outgoing_call_tested_at IS NULL THEN NULL ELSE COALESCE(device_readiness.outgoing_call_tested_at, excluded.outgoing_call_tested_at) END,
			incoming_call_tested_at = CASE WHEN excluded.incoming_call_tested_at IS NULL THEN NULL ELSE COALESCE(device_readiness.incoming_call_tested_at, excluded.incoming_call_tested_at) END,
			updated_at = excluded.updated_at`,
		deviceID, nullableUnix(input.EchoTested, now), nullableUnix(input.OutgoingCallTested, now),
		nullableUnix(input.IncomingCallTested, now), now)
	if err != nil {
		return fmt.Errorf("update device readiness: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit device readiness update: %w", err)
	}
	return nil
}

// ChangeMemberExtensionByDevice lets an authenticated, active SIP endpoint
// change the extension of its own member inside the supplied party. The
// endpoint identity comes from Asterisk's authenticated channel, not caller ID.
func (s *Store) ChangeMemberExtensionByDevice(ctx context.Context, partyID, sipUsername, extension string) error {
	if !extensionrules.Valid(extension) {
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

// ActiveDeviceForHost returns only the non-secret fields needed for a host to
// send one setup ring to an active phone inside their own party.
func (s *Store) ActiveDeviceForHost(ctx context.Context, partyID, hostUserID, deviceID string) (model.Member, model.Device, error) {
	var member model.Member
	var device model.Device
	var memberCreated, deviceCreated int64
	err := s.db.QueryRowContext(ctx, `
		SELECT m.id, m.party_id, m.display_name, m.extension, m.created_at,
			d.id, d.member_id, d.label, d.sip_username, d.created_at
		FROM parties p
		JOIN members m ON m.party_id = p.id
		JOIN devices d ON d.member_id = m.id
		WHERE p.id = ? AND p.host_user_id = ? AND d.id = ? AND d.revoked_at IS NULL`,
		partyID, hostUserID, deviceID,
	).Scan(
		&member.ID, &member.PartyID, &member.DisplayName, &member.Extension, &memberCreated,
		&device.ID, &device.MemberID, &device.Label, &device.SIPUsername, &deviceCreated,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Member{}, model.Device{}, ErrNotFound
	}
	if err != nil {
		return model.Member{}, model.Device{}, fmt.Errorf("load active hosted device: %w", err)
	}
	member.CreatedAt = fromUnix(memberCreated)
	device.CreatedAt = fromUnix(deviceCreated)
	return member, device, nil
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

// AddDeviceForHost gives an existing member another independently revocable
// phone credential. The party ownership check, member lookup, bounded device
// count, credential insert, and one-time provisioning token are one transaction.
func (s *Store) AddDeviceForHost(ctx context.Context, input NewHostedDevice) (CreatedDevice, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return CreatedDevice{}, fmt.Errorf("begin hosted device creation: %w", err)
	}
	defer tx.Rollback()

	party, err := scanParty(tx.QueryRowContext(ctx, `
		SELECT id, name, slug, host_user_id, openai_project_id,
			openai_service_account_id, openai_api_key_id, openai_key_ciphertext, openai_status,
			openai_spend_limit_cents, openai_spend_pending_cents, openai_spend_limit_status, created_at
		FROM parties WHERE id = ? AND host_user_id = ?`, input.PartyID, input.HostUserID))
	if errors.Is(err, sql.ErrNoRows) {
		return CreatedDevice{}, ErrNotFound
	}
	if err != nil {
		return CreatedDevice{}, fmt.Errorf("load hosted device party: %w", err)
	}

	var member model.Member
	var memberCreated int64
	err = tx.QueryRowContext(ctx, `
		SELECT id, party_id, display_name, extension, created_at
		FROM members WHERE id = ? AND party_id = ?`, input.MemberID, input.PartyID).Scan(
		&member.ID, &member.PartyID, &member.DisplayName, &member.Extension, &memberCreated,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return CreatedDevice{}, ErrNotFound
	}
	if err != nil {
		return CreatedDevice{}, fmt.Errorf("load hosted device member: %w", err)
	}
	member.CreatedAt = fromUnix(memberCreated)

	var deviceCount int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM devices WHERE member_id = ?`, input.MemberID).Scan(&deviceCount); err != nil {
		return CreatedDevice{}, fmt.Errorf("count hosted member devices: %w", err)
	}
	if deviceCount >= MaxDevicesPerMember {
		return CreatedDevice{}, ErrDeviceLimit
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO devices (id, member_id, label, sip_username, sip_secret_ciphertext, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`, input.DeviceID, input.MemberID, input.DeviceLabel,
		input.SIPUsername, input.SIPSecretCiphertext, unix(input.Now))
	if err != nil {
		if isSIPUsernameConflict(err) {
			return CreatedDevice{}, ErrSIPUsernameTaken
		}
		return CreatedDevice{}, fmt.Errorf("create hosted member device: %w", err)
	}
	if err := replaceProvisioningTokenTx(ctx, tx, input.DeviceID, input.Provisioning); err != nil {
		return CreatedDevice{}, err
	}
	if err := tx.Commit(); err != nil {
		return CreatedDevice{}, fmt.Errorf("commit hosted device creation: %w", err)
	}

	device := model.Device{
		ID: input.DeviceID, MemberID: input.MemberID, Label: input.DeviceLabel,
		SIPUsername: input.SIPUsername, SIPSecretCiphertext: input.SIPSecretCiphertext, CreatedAt: input.Now.UTC(),
	}
	return CreatedDevice{Party: party, Member: member, Device: device}, nil
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
		if isSIPUsernameConflict(err) {
			return RotatedDevice{}, ErrSIPUsernameTaken
		}
		return RotatedDevice{}, fmt.Errorf("rotate device credentials: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return RotatedDevice{}, ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM device_readiness WHERE device_id = ?`, deviceID); err != nil {
		return RotatedDevice{}, fmt.Errorf("clear device readiness after credential rotation: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM phone_push_registrations WHERE device_id = ?`, deviceID); err != nil {
		return RotatedDevice{}, fmt.Errorf("clear phone push registration after credential rotation: %w", err)
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
	if _, err := tx.ExecContext(ctx, `DELETE FROM phone_push_registrations WHERE device_id = ?`, deviceID); err != nil {
		return fmt.Errorf("remove revoked device push registration: %w", err)
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
		SELECT d.id, m.party_id, m.id, d.sip_username, d.sip_secret_ciphertext, m.extension,
			t.expires_at, t.used_at, d.revoked_at
		FROM device_provisioning_tokens t
		JOIN devices d ON d.id = t.device_id
		JOIN members m ON m.id = d.member_id
		WHERE t.token_hash = ?`, tokenHash).Scan(
		&provision.DeviceID, &provision.PartyID, &provision.MemberID, &provision.SIPUsername, &provision.SIPSecretCiphertext,
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
			CASE WHEN COALESCE(s.weather_enabled, 0) = 1 AND p.openai_status = 'ready'
				AND p.openai_spend_limit_status NOT IN ('updating', 'update-error') THEN 1 ELSE 0 END,
			CASE WHEN COALESCE(s.weather_enabled, 0) = 1 AND COALESCE(s.weather_setup_allowed, 1) = 1 AND p.openai_status = 'ready'
				AND p.openai_spend_limit_status NOT IN ('updating', 'update-error') THEN 1 ELSE 0 END,
			COALESCE(s.radio_enabled, 0),
			COALESCE(s.radio_station, 'groove-salad')
		FROM parties p LEFT JOIN party_services s ON s.party_id = p.id
		ORDER BY p.id`)
	if err != nil {
		return nil, fmt.Errorf("list routing services: %w", err)
	}
	defer rows.Close()
	var services []model.RoutingServices
	for rows.Next() {
		var item model.RoutingServices
		var timeEnabled, weatherEnabled, weatherSetupEnabled, radioEnabled int
		if err := rows.Scan(&item.PartyID, &timeEnabled, &weatherEnabled, &weatherSetupEnabled, &radioEnabled, &item.RadioStation); err != nil {
			return nil, fmt.Errorf("scan routing services: %w", err)
		}
		item.TimeEnabled = timeEnabled != 0
		item.WeatherEnabled = weatherEnabled != 0
		item.WeatherSetupEnabled = weatherSetupEnabled != 0
		item.RadioEnabled = radioEnabled != 0
		if _, ok := radio.Lookup(item.RadioStation); !ok {
			return nil, errors.New("routing state contains an unsupported radio station")
		}
		services = append(services, item)
	}
	return services, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanParty(row scanner) (model.Party, error) {
	var party model.Party
	var projectID, serviceID, apiKeyID, keyCipher, status, spendStatus sql.NullString
	var spendLimit int
	var spendPending sql.NullInt64
	var created int64
	if err := row.Scan(&party.ID, &party.Name, &party.Slug, &party.HostUserID, &projectID,
		&serviceID, &apiKeyID, &keyCipher, &status, &spendLimit, &spendPending, &spendStatus, &created); err != nil {
		return model.Party{}, err
	}
	party.OpenAIProjectID = projectID.String
	party.OpenAIServiceAccountID = serviceID.String
	party.OpenAIAPIKeyID = apiKeyID.String
	party.OpenAIKeyCiphertext = keyCipher.String
	party.OpenAIStatus = status.String
	party.OpenAISpendLimitCents = spendLimit
	party.OpenAISpendPendingCents = int(spendPending.Int64)
	party.OpenAISpendLimitStatus = spendStatus.String
	party.CreatedAt = fromUnix(created)
	return party, nil
}

func partyByIDTx(ctx context.Context, tx *sql.Tx, partyID string) (model.Party, error) {
	return scanParty(tx.QueryRowContext(ctx, `
		SELECT id, name, slug, host_user_id, openai_project_id, openai_service_account_id,
			openai_api_key_id, openai_key_ciphertext, openai_status,
			openai_spend_limit_cents, openai_spend_pending_cents, openai_spend_limit_status, created_at
		FROM parties WHERE id = ?`, partyID))
}

func (s *Store) partyByID(ctx context.Context, partyID string) (model.Party, error) {
	party, err := scanParty(s.db.QueryRowContext(ctx, `
		SELECT id, name, slug, host_user_id, openai_project_id, openai_service_account_id,
			openai_api_key_id, openai_key_ciphertext, openai_status,
			openai_spend_limit_cents, openai_spend_pending_cents, openai_spend_limit_status, created_at
		FROM parties WHERE id = ?`, partyID))
	if errors.Is(err, sql.ErrNoRows) {
		return model.Party{}, ErrNotFound
	}
	return party, err
}

func deviceForHostTx(ctx context.Context, tx *sql.Tx, partyID, hostUserID, deviceID string) (model.Party, model.Member, model.Device, error) {
	party, err := scanParty(tx.QueryRowContext(ctx, `
		SELECT id, name, slug, host_user_id, openai_project_id,
			openai_service_account_id, openai_api_key_id, openai_key_ciphertext, openai_status,
			openai_spend_limit_cents, openai_spend_pending_cents, openai_spend_limit_status, created_at
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

func isSIPUsernameConflict(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unique") && strings.Contains(message, "devices.sip_username")
}

func unix(value time.Time) int64     { return value.UTC().Unix() }
func fromUnix(value int64) time.Time { return time.Unix(value, 0).UTC() }
func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func nullableUnix(enabled bool, value int64) any {
	if !enabled {
		return nil
	}
	return value
}

func readinessFromNulls(echo, outgoing, incoming sql.NullInt64) model.DeviceReadiness {
	readiness := model.DeviceReadiness{}
	if echo.Valid {
		value := fromUnix(echo.Int64)
		readiness.EchoTestedAt = &value
	}
	if outgoing.Valid {
		value := fromUnix(outgoing.Int64)
		readiness.OutgoingCallTestedAt = &value
	}
	if incoming.Valid {
		value := fromUnix(incoming.Int64)
		readiness.IncomingCallTestedAt = &value
	}
	return readiness
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
	openai_spend_limit_cents INTEGER NOT NULL DEFAULT 0 CHECK(openai_spend_limit_cents >= 0),
	openai_spend_pending_cents INTEGER CHECK(openai_spend_pending_cents IS NULL OR openai_spend_pending_cents > 0),
	openai_spend_limit_status TEXT NOT NULL DEFAULT 'unknown',
    created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS parties_host ON parties(host_user_id);

CREATE TABLE IF NOT EXISTS party_services (
    party_id TEXT PRIMARY KEY REFERENCES parties(id) ON DELETE CASCADE,
    time_enabled INTEGER NOT NULL DEFAULT 1 CHECK(time_enabled IN (0, 1)),
    weather_enabled INTEGER NOT NULL DEFAULT 0 CHECK(weather_enabled IN (0, 1)),
    weather_setup_allowed INTEGER NOT NULL DEFAULT 1 CHECK(weather_setup_allowed IN (0, 1)),
    weather_query TEXT NOT NULL DEFAULT '',
    weather_label TEXT NOT NULL DEFAULT '',
    weather_latitude REAL NOT NULL DEFAULT 0,
    weather_longitude REAL NOT NULL DEFAULT 0,
	radio_enabled INTEGER NOT NULL DEFAULT 0 CHECK(radio_enabled IN (0, 1)),
	radio_station TEXT NOT NULL DEFAULT 'groove-salad',
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
    adult_extension INTEGER NOT NULL DEFAULT 0 CHECK(adult_extension IN (0, 1)),
	weather_query TEXT NOT NULL DEFAULT '',
	weather_label TEXT NOT NULL DEFAULT '',
	weather_latitude REAL NOT NULL DEFAULT 0,
	weather_longitude REAL NOT NULL DEFAULT 0,
	weather_updated_at INTEGER,
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

CREATE TABLE IF NOT EXISTS device_readiness (
    device_id TEXT PRIMARY KEY REFERENCES devices(id) ON DELETE CASCADE,
    echo_tested_at INTEGER CHECK(echo_tested_at IS NULL OR echo_tested_at > 0),
    outgoing_call_tested_at INTEGER CHECK(outgoing_call_tested_at IS NULL OR outgoing_call_tested_at > 0),
    incoming_call_tested_at INTEGER CHECK(incoming_call_tested_at IS NULL OR incoming_call_tested_at > 0),
    updated_at INTEGER NOT NULL CHECK(updated_at > 0)
);

CREATE TABLE IF NOT EXISTS device_provisioning_tokens (
    token_hash BLOB PRIMARY KEY,
    device_id TEXT NOT NULL UNIQUE REFERENCES devices(id) ON DELETE CASCADE,
    expires_at INTEGER NOT NULL,
    used_at INTEGER,
    created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS device_provisioning_expires ON device_provisioning_tokens(expires_at);
`
