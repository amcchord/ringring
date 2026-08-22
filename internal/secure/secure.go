package secure

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

var rawURL = base64.RawURLEncoding

type Cipher struct {
	aead cipher.AEAD
}

func NewCipher(key []byte) (*Cipher, error) {
	if len(key) != 32 {
		return nil, errors.New("encryption key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}
	return &Cipher{aead: aead}, nil
}

func Token(bytes int) (string, error) {
	if bytes < 16 {
		return "", errors.New("token entropy must be at least 16 bytes")
	}
	value := make([]byte, bytes)
	if _, err := io.ReadFull(rand.Reader, value); err != nil {
		return "", fmt.Errorf("read randomness: %w", err)
	}
	return rawURL.EncodeToString(value), nil
}

func ID(prefix string) (string, error) {
	random, err := Token(18)
	if err != nil {
		return "", err
	}
	return prefix + "_" + random, nil
}

func Hash(value string) []byte {
	digest := sha256.Sum256([]byte(value))
	return digest[:]
}

func CSRF(sessionToken string, secret []byte) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte("ringring-csrf-v1\x00"))
	_, _ = mac.Write([]byte(sessionToken))
	return rawURL.EncodeToString(mac.Sum(nil))
}

func Equal(a, b string) bool {
	return hmac.Equal([]byte(a), []byte(b))
}

func (c *Cipher) Encrypt(plaintext string, associatedData []byte) (string, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("read nonce: %w", err)
	}
	sealed := c.aead.Seal(nonce, nonce, []byte(plaintext), associatedData)
	return rawURL.EncodeToString(sealed), nil
}

func (c *Cipher) Decrypt(encoded string, associatedData []byte) (string, error) {
	sealed, err := rawURL.DecodeString(encoded)
	if err != nil {
		return "", errors.New("invalid ciphertext encoding")
	}
	if len(sealed) < c.aead.NonceSize() {
		return "", errors.New("invalid ciphertext length")
	}
	nonce, ciphertext := sealed[:c.aead.NonceSize()], sealed[c.aead.NonceSize():]
	plaintext, err := c.aead.Open(nil, nonce, ciphertext, associatedData)
	if err != nil {
		return "", errors.New("decrypt ciphertext")
	}
	return string(plaintext), nil
}
