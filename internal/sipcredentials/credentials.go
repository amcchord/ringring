// Package sipcredentials generates the human-enterable identities used only
// for SIP device authentication. These are deliberately numeric so an ATA or
// desk phone can accept them without switching keypad input modes.
package sipcredentials

import (
	"crypto/rand"
	"fmt"
	"io"
	"math/big"
)

const (
	// UsernameDigits gives each phone a short keypad-friendly identity. The
	// database's unique constraint and bounded retry handle the 900,000-value
	// space without allowing two phones to share an identity.
	UsernameDigits = 6
	// PasswordDigits gives each phone about 39.7 bits of random secret entropy
	// across 9×10^11 values while remaining practical to enter by keypad.
	PasswordDigits = 12
)

type Pair struct {
	Username string
	Password string
}

func Generate() (Pair, error) {
	username, err := randomDecimal(rand.Reader, UsernameDigits)
	if err != nil {
		return Pair{}, fmt.Errorf("generate SIP username: %w", err)
	}
	password, err := randomDecimal(rand.Reader, PasswordDigits)
	if err != nil {
		return Pair{}, fmt.Errorf("generate SIP password: %w", err)
	}
	return Pair{Username: username, Password: password}, nil
}

func ValidUsername(value string) bool {
	return validDecimal(value, UsernameDigits)
}

func ValidPassword(value string) bool {
	return validDecimal(value, PasswordDigits)
}

func randomDecimal(reader io.Reader, digits int) (string, error) {
	if digits < 2 {
		return "", fmt.Errorf("decimal credential must have at least two digits")
	}
	minimum := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(digits-1)), nil)
	span := new(big.Int).Mul(new(big.Int).Set(minimum), big.NewInt(9))
	value, err := rand.Int(reader, span)
	if err != nil {
		return "", err
	}
	return value.Add(value, minimum).Text(10), nil
}

func validDecimal(value string, digits int) bool {
	if len(value) != digits || value[0] < '1' || value[0] > '9' {
		return false
	}
	for index := 1; index < len(value); index++ {
		if value[index] < '0' || value[index] > '9' {
			return false
		}
	}
	return true
}
