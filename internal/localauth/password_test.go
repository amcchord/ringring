package localauth

import (
	"strings"
	"testing"
)

func TestPasswordHashRoundTrip(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(hash, "correct horse") {
		t.Fatal("password hash exposed the password")
	}
	valid, err := VerifyPassword(hash, "correct horse battery staple")
	if err != nil || !valid {
		t.Fatalf("valid password rejected: valid=%t err=%v", valid, err)
	}
	valid, err = VerifyPassword(hash, "wrong password")
	if err != nil || valid {
		t.Fatalf("wrong password accepted: valid=%t err=%v", valid, err)
	}
}

func TestPasswordHashRejectsUnsafeParameters(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	hash = strings.Replace(hash, "m=19456", "m=999999", 1)
	if _, err := VerifyPassword(hash, "correct horse battery staple"); err == nil {
		t.Fatal("unsafe memory setting was accepted")
	}
}

func TestRecoveryCodeIsReadableAndNormalizes(t *testing.T) {
	code, err := NewRecoveryCode()
	if err != nil {
		t.Fatal(err)
	}
	if len(NormalizeRecoveryCode(code)) != 26 || !strings.Contains(code, "-") {
		t.Fatalf("unexpected recovery code %q", code)
	}
	if NormalizeRecoveryCode(strings.ToLower(code)) != NormalizeRecoveryCode(code) {
		t.Fatal("recovery code normalization must ignore case")
	}
}
