package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type PhoneDevice struct {
	DeviceID            string
	PartyID             string
	MemberID            string
	Extension           string
	AdultExtension      bool
	SIPUsername         string
	SIPSecretCiphertext string
}

type PhonePushRegistration struct {
	DeviceID        string
	TokenHash       []byte
	TokenCiphertext string
	Environment     string
	UpdatedAt       time.Time
}

func ensurePhonePushTable(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS phone_push_registrations (
			device_id TEXT PRIMARY KEY REFERENCES devices(id) ON DELETE CASCADE,
			token_hash BLOB NOT NULL UNIQUE,
			token_ciphertext TEXT NOT NULL,
			environment TEXT NOT NULL CHECK(environment IN ('production', 'development')),
			updated_at INTEGER NOT NULL CHECK(updated_at > 0)
		);
		CREATE INDEX IF NOT EXISTS phone_push_updated_at ON phone_push_registrations(updated_at);`)
	return err
}

func (s *Store) PhoneDeviceBySIPUsername(ctx context.Context, username string) (PhoneDevice, error) {
	var device PhoneDevice
	var adult int
	err := s.db.QueryRowContext(ctx, `
		SELECT d.id, m.party_id, m.id, m.extension, m.adult_extension,
			d.sip_username, d.sip_secret_ciphertext
		FROM devices d JOIN members m ON m.id = d.member_id
		WHERE d.sip_username = ? AND d.revoked_at IS NULL`, username).Scan(
		&device.DeviceID, &device.PartyID, &device.MemberID, &device.Extension, &adult,
		&device.SIPUsername, &device.SIPSecretCiphertext,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return PhoneDevice{}, ErrNotFound
	}
	if err != nil {
		return PhoneDevice{}, fmt.Errorf("load phone API device: %w", err)
	}
	device.AdultExtension = adult == 1
	return device, nil
}

func (s *Store) SavePhonePushRegistration(ctx context.Context, registration PhonePushRegistration) error {
	if registration.DeviceID == "" || len(registration.TokenHash) != 32 || registration.TokenCiphertext == "" ||
		(registration.Environment != "production" && registration.Environment != "development") || registration.UpdatedAt.IsZero() {
		return errors.New("invalid phone push registration")
	}
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO phone_push_registrations (device_id, token_hash, token_ciphertext, environment, updated_at)
		SELECT id, ?, ?, ?, ? FROM devices WHERE id = ? AND revoked_at IS NULL
		ON CONFLICT(device_id) DO UPDATE SET token_hash = excluded.token_hash,
			token_ciphertext = excluded.token_ciphertext, environment = excluded.environment,
			updated_at = excluded.updated_at`,
		registration.TokenHash, registration.TokenCiphertext, registration.Environment,
		unix(registration.UpdatedAt), registration.DeviceID,
	)
	if err != nil {
		return fmt.Errorf("save phone push registration: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeletePhonePushRegistration(ctx context.Context, deviceID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM phone_push_registrations WHERE device_id = ?`, deviceID)
	if err != nil {
		return fmt.Errorf("delete phone push registration: %w", err)
	}
	return nil
}

func (s *Store) DeletePhonePushRegistrationByHash(ctx context.Context, tokenHash []byte) error {
	if len(tokenHash) != 32 {
		return errors.New("invalid phone push token hash")
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM phone_push_registrations WHERE token_hash = ?`, tokenHash)
	if err != nil {
		return fmt.Errorf("delete invalid phone push registration: %w", err)
	}
	return nil
}

func (s *Store) PhonePushRegistrationsForExtension(ctx context.Context, partyID, extension string) ([]PhonePushRegistration, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.device_id, p.token_hash, p.token_ciphertext, p.environment, p.updated_at
		FROM phone_push_registrations p
		JOIN devices d ON d.id = p.device_id
		JOIN members m ON m.id = d.member_id
		WHERE m.party_id = ? AND m.extension = ? AND d.revoked_at IS NULL
		ORDER BY p.device_id`, partyID, extension)
	if err != nil {
		return nil, fmt.Errorf("list phone push registrations: %w", err)
	}
	defer rows.Close()
	var registrations []PhonePushRegistration
	for rows.Next() {
		var registration PhonePushRegistration
		var updated int64
		if err := rows.Scan(&registration.DeviceID, &registration.TokenHash, &registration.TokenCiphertext, &registration.Environment, &updated); err != nil {
			return nil, fmt.Errorf("scan phone push registration: %w", err)
		}
		registration.UpdatedAt = fromUnix(updated)
		registrations = append(registrations, registration)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read phone push registrations: %w", err)
	}
	return registrations, nil
}
