package panel

import (
	"crypto/rand"
	"encoding/hex"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	bcryptCost      = 12
	sessionIDLen    = 32
	sessionDuration = 24 * time.Hour
)

// HashPassword returns a bcrypt hash of password at cost 12.
func HashPassword(password string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return "", err
	}
	return string(h), nil
}

// CheckPassword reports whether password matches the stored hash.
func CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// NewSessionID generates a cryptographically random hex session identifier.
func NewSessionID() (string, error) {
	b := make([]byte, sessionIDLen)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// SessionExpiry returns the absolute expiry time for a new session.
func SessionExpiry() time.Time {
	return time.Now().Add(sessionDuration)
}
