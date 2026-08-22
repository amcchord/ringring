package sipcredentials

import (
	"errors"
	"io"
	"math/big"
	"strings"
	"testing"
)

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) {
	return 0, errors.New("randomness unavailable")
}

func TestGenerateUsesFixedNumericFormats(t *testing.T) {
	passwords := make(map[string]struct{}, 500)
	for index := 0; index < 500; index++ {
		pair, err := Generate()
		if err != nil {
			t.Fatal(err)
		}
		if !ValidUsername(pair.Username) || !ValidPassword(pair.Password) {
			t.Fatalf("invalid generated format: username=%q password_length=%d", pair.Username, len(pair.Password))
		}
		if _, exists := passwords[pair.Password]; exists {
			t.Fatal("generated duplicate password in a 500-value sample")
		}
		passwords[pair.Password] = struct{}{}
	}
}

func TestRandomDecimalIsUniformlyBoundedAndFailsClosed(t *testing.T) {
	minimum, err := randomDecimal(io.LimitReader(zeroReader{}, 64), PasswordDigits)
	if err != nil {
		t.Fatal(err)
	}
	if minimum != "1"+strings.Repeat("0", PasswordDigits-1) {
		t.Fatalf("zero randomness did not map to the exact lower bound: %q", minimum)
	}
	if _, err := randomDecimal(failingReader{}, PasswordDigits); err == nil {
		t.Fatal("randomness failure was accepted")
	}
	if _, err := randomDecimal(zeroReader{}, 1); err == nil {
		t.Fatal("an undersized decimal credential was accepted")
	}

	usernameSpace := new(big.Int).Mul(new(big.Int).Exp(big.NewInt(10), big.NewInt(UsernameDigits-1), nil), big.NewInt(9))
	passwordSpace := new(big.Int).Mul(new(big.Int).Exp(big.NewInt(10), big.NewInt(PasswordDigits-1), nil), big.NewInt(9))
	if usernameSpace.BitLen() != 20 || passwordSpace.BitLen() != 40 {
		t.Fatalf("unexpected credential spaces: username_bits=%d password_bits=%d", usernameSpace.BitLen(), passwordSpace.BitLen())
	}
}

type zeroReader struct{}

func (zeroReader) Read(destination []byte) (int, error) {
	clear(destination)
	return len(destination), nil
}

func TestFormatValidatorsRejectAmbiguousOrLegacyValues(t *testing.T) {
	for _, value := range []string{"", "012345", "123 456", "12345a", "123456789012345", "rrd_123"} {
		if ValidUsername(value) {
			t.Fatalf("invalid username accepted: %q", value)
		}
	}
	for _, value := range []string{"", "012345678901", "12345678901", "12345678901a", "1234-5678-9012", "123456789012345678901234"} {
		if ValidPassword(value) {
			t.Fatalf("invalid password accepted: %q", value)
		}
	}
}
