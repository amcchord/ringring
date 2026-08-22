package secure

import "testing"

func TestCipherRoundTrip(t *testing.T) {
	c, err := NewCipher(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}

	encoded, err := c.Encrypt("quiet secret", []byte("device-1"))
	if err != nil {
		t.Fatal(err)
	}
	if encoded == "quiet secret" {
		t.Fatal("ciphertext leaked plaintext")
	}
	got, err := c.Decrypt(encoded, []byte("device-1"))
	if err != nil {
		t.Fatal(err)
	}
	if got != "quiet secret" {
		t.Fatalf("got %q", got)
	}
	if _, err := c.Decrypt(encoded, []byte("device-2")); err == nil {
		t.Fatal("expected associated-data mismatch to fail")
	}
}

func TestTokenAndCSRF(t *testing.T) {
	a, err := Token(32)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Token(32)
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("random tokens unexpectedly match")
	}
	if CSRF(a, make([]byte, 32)) == CSRF(b, make([]byte, 32)) {
		t.Fatal("CSRF token must be bound to session")
	}
}

func TestIDUsesSafePrefixAndEnoughEntropy(t *testing.T) {
	id, err := ID("pty")
	if err != nil {
		t.Fatal(err)
	}
	if len(id) < len("pty_")+22 || id[:4] != "pty_" {
		t.Fatalf("unexpected ID %q", id)
	}
}
