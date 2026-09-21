// Package crypto provides authenticated encryption for secrets stored in the
// database (notification provider credentials). The key comes exclusively
// from the LEM_SECRET_KEY environment variable; the server refuses to start
// without it so credentials are never written unencrypted by accident.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
)

const envKey = "LEM_SECRET_KEY"

var (
	errMissingKey = fmt.Errorf("%s is not set: notification credentials cannot be stored safely", envKey)
	errShortKey   = fmt.Errorf("%s must be at least 16 characters", envKey)
)

// Key derives a stable 32-byte AES-256 key from LEM_SECRET_KEY.
// Any string is accepted but short values are rejected to avoid weak material.
func Key() ([]byte, error) {
	raw := os.Getenv(envKey)
	if raw == "" {
		return nil, errMissingKey
	}
	if len(raw) < 16 {
		return nil, errShortKey
	}
	sum := sha256.Sum256([]byte(raw))
	return sum[:], nil
}

// MustKey is used at startup to fail fast on a missing/weak key.
func MustKey() []byte {
	key, err := Key()
	if err != nil {
		panic(err)
	}
	return key
}

// Encrypt returns base64(nonce || ciphertext) for the given plaintext.
func Encrypt(key, plaintext []byte) (string, error) {
	if len(key) == 0 {
		return "", errMissingKey
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("new gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("nonce: %w", err)
	}
	sealed := gcm.Seal(nonce, nonce, plaintext, nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// ErrDecrypt marks a ciphertext that cannot be opened with the current key.
var ErrDecrypt = errors.New("decryption failed (wrong LEM_SECRET_KEY or corrupted data)")

// Decrypt reverses Encrypt.
func Decrypt(key []byte, encoded string) ([]byte, error) {
	if len(key) == 0 {
		return nil, errMissingKey
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new gcm: %w", err)
	}
	if len(data) < gcm.NonceSize() {
		return nil, ErrDecrypt
	}
	plain, err := gcm.Open(nil, data[:gcm.NonceSize()], data[gcm.NonceSize():], nil)
	if err != nil {
		return nil, ErrDecrypt
	}
	return plain, nil
}
