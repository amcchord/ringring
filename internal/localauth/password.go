package localauth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	argonMemory      = 19 * 1024
	argonIterations  = 2
	argonParallelism = 1
	argonSaltBytes   = 16
	argonKeyBytes    = 32
)

var recoveryEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("read password salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, argonIterations, argonMemory, argonParallelism, argonKeyBytes)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonIterations, argonParallelism,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

func VerifyPassword(encoded, password string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return false, errors.New("invalid password hash format")
	}
	version, err := strconv.Atoi(strings.TrimPrefix(parts[2], "v="))
	if err != nil || version != argon2.Version {
		return false, errors.New("unsupported password hash version")
	}
	var memory, iterations uint32
	var parallelism uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &parallelism); err != nil {
		return false, errors.New("invalid password hash parameters")
	}
	// Keep corrupted or malicious database values from turning login into a
	// resource-exhaustion attack. These bounds also leave room for future tuning.
	if memory < 7*1024 || memory > 128*1024 || iterations < 1 || iterations > 8 || parallelism < 1 || parallelism > 4 {
		return false, errors.New("unsafe password hash parameters")
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) < 16 || len(salt) > 64 {
		return false, errors.New("invalid password hash salt")
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(want) < 16 || len(want) > 64 {
		return false, errors.New("invalid password hash value")
	}
	got := argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

func NewRecoveryCode() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("read recovery code randomness: %w", err)
	}
	encoded := recoveryEncoding.EncodeToString(raw)
	groups := make([]string, 0, (len(encoded)+3)/4)
	for len(encoded) > 4 {
		groups = append(groups, encoded[:4])
		encoded = encoded[4:]
	}
	groups = append(groups, encoded)
	return strings.Join(groups, "-"), nil
}

func NormalizeRecoveryCode(value string) string {
	value = strings.ToUpper(value)
	return strings.Map(func(r rune) rune {
		if r == '-' || r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, value)
}

func RecoveryCodeHash(value string) []byte {
	digest := sha256.Sum256([]byte("ringring-recovery-v1\x00" + NormalizeRecoveryCode(value)))
	return digest[:]
}
