package maintenance

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"github.com/amcchord/ringring/internal/radio"
	"github.com/amcchord/ringring/internal/secure"
	_ "modernc.org/sqlite"
)

type StateReport struct {
	Status        string `json:"status"`
	Integrity     string `json:"integrity"`
	ForeignKeys   string `json:"foreign_keys"`
	Users         int    `json:"users"`
	Parties       int    `json:"parties"`
	Members       int    `json:"members"`
	Devices       int    `json:"devices"`
	Invitations   int    `json:"invitations"`
	Sessions      int    `json:"sessions"`
	RecoveryCodes int    `json:"recovery_codes"`
	Provisioning  int    `json:"provisioning_tokens"`
	PartyKeys     int    `json:"party_keys_verified"`
	DeviceSecrets int    `json:"device_secrets_verified"`
}

// VerifyState opens a restored database without migrating it, checks SQLite
// integrity and current schema requirements, and proves that the supplied
// master key can decrypt every persisted service and SIP credential. It returns
// counts only and never returns decrypted values or record identifiers.
func VerifyState(ctx context.Context, databasePath string, masterKey []byte) (StateReport, error) {
	if databasePath == "" || databasePath == ":memory:" {
		return StateReport{}, errors.New("database path must name a backup file")
	}
	info, err := os.Stat(databasePath)
	if err != nil {
		return StateReport{}, fmt.Errorf("inspect database: %w", err)
	}
	if !info.Mode().IsRegular() {
		return StateReport{}, errors.New("database path is not a regular file")
	}
	absolutePath, err := filepath.Abs(databasePath)
	if err != nil {
		return StateReport{}, fmt.Errorf("resolve database path: %w", err)
	}
	databaseURL := url.URL{Scheme: "file", Path: filepath.ToSlash(absolutePath)}
	query := databaseURL.Query()
	// Backup creation requires a cleanly closed snapshot with no WAL or SHM
	// sidecars, so immutable mode is both safe and keeps verification read-only.
	query.Set("immutable", "1")
	query.Set("mode", "ro")
	databaseURL.RawQuery = query.Encode()
	database, err := sql.Open("sqlite", databaseURL.String())
	if err != nil {
		return StateReport{}, fmt.Errorf("open database: %w", err)
	}
	defer database.Close()
	database.SetMaxOpenConns(1)
	if _, err := database.ExecContext(ctx, `PRAGMA query_only = ON; PRAGMA foreign_keys = ON; PRAGMA busy_timeout = 5000;`); err != nil {
		return StateReport{}, fmt.Errorf("configure verification connection: %w", err)
	}

	report := StateReport{Status: "ok", Integrity: "ok", ForeignKeys: "ok"}
	var integrity string
	if err := database.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
		return StateReport{}, fmt.Errorf("check database integrity: %w", err)
	}
	if integrity != "ok" {
		return StateReport{}, errors.New("database integrity check failed")
	}
	foreignKeyRows, err := database.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return StateReport{}, fmt.Errorf("check foreign keys: %w", err)
	}
	if foreignKeyRows.Next() {
		foreignKeyRows.Close()
		return StateReport{}, errors.New("database foreign-key check failed")
	}
	if err := foreignKeyRows.Err(); err != nil {
		foreignKeyRows.Close()
		return StateReport{}, fmt.Errorf("read foreign-key check: %w", err)
	}
	if err := foreignKeyRows.Close(); err != nil {
		return StateReport{}, fmt.Errorf("close foreign-key check: %w", err)
	}

	currentSchema, err := database.QueryContext(ctx, `
		SELECT p.openai_api_key_id, p.openai_spend_limit_cents, p.openai_spend_pending_cents,
			p.openai_spend_limit_status, s.ai_enabled, s.radio_station,
			t.token_hash, t.device_id, t.expires_at, t.used_at, t.created_at
		FROM parties p CROSS JOIN party_services s CROSS JOIN device_provisioning_tokens t LIMIT 0`)
	if err != nil {
		return StateReport{}, errors.New("database schema is not current")
	}
	if err := currentSchema.Close(); err != nil {
		return StateReport{}, fmt.Errorf("close schema check: %w", err)
	}
	stationRows, err := database.QueryContext(ctx, `SELECT radio_station FROM party_services`)
	if err != nil {
		return StateReport{}, errors.New("verify radio station catalog")
	}
	for stationRows.Next() {
		var stationID string
		if err := stationRows.Scan(&stationID); err != nil {
			stationRows.Close()
			return StateReport{}, errors.New("verify radio station catalog")
		}
		if _, ok := radio.Lookup(stationID); !ok {
			stationRows.Close()
			return StateReport{}, errors.New("database contains an unsupported radio station")
		}
	}
	if err := stationRows.Err(); err != nil {
		stationRows.Close()
		return StateReport{}, errors.New("verify radio station catalog")
	}
	if err := stationRows.Close(); err != nil {
		return StateReport{}, errors.New("verify radio station catalog")
	}
	counts := []struct {
		table string
		value *int
	}{
		{"users", &report.Users},
		{"parties", &report.Parties},
		{"members", &report.Members},
		{"devices", &report.Devices},
		{"invitations", &report.Invitations},
		{"sessions", &report.Sessions},
		{"recovery_codes", &report.RecoveryCodes},
		{"device_provisioning_tokens", &report.Provisioning},
	}
	for _, count := range counts {
		if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+count.table).Scan(count.value); err != nil {
			return StateReport{}, fmt.Errorf("count required table %s: %w", count.table, err)
		}
	}

	cipher, err := secure.NewCipher(masterKey)
	if err != nil {
		return StateReport{}, fmt.Errorf("prepare master key: %w", err)
	}
	report.DeviceSecrets, err = verifyCiphertexts(ctx, database, cipher,
		`SELECT id, sip_secret_ciphertext FROM devices WHERE sip_secret_ciphertext <> ''`)
	if err != nil {
		return StateReport{}, errors.New("verify encrypted device credentials")
	}
	report.PartyKeys, err = verifyCiphertexts(ctx, database, cipher,
		`SELECT id, openai_key_ciphertext FROM parties WHERE openai_key_ciphertext <> ''`)
	if err != nil {
		return StateReport{}, errors.New("verify encrypted party credentials")
	}
	return report, nil
}

func verifyCiphertexts(ctx context.Context, database *sql.DB, cipher *secure.Cipher, query string) (int, error) {
	rows, err := database.QueryContext(ctx, query)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var id, ciphertext string
		if err := rows.Scan(&id, &ciphertext); err != nil {
			return 0, err
		}
		plaintext, err := cipher.Decrypt(ciphertext, []byte(id))
		if err != nil || plaintext == "" {
			return 0, errors.New("credential decryption failed")
		}
		count++
	}
	return count, rows.Err()
}
