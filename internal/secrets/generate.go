package secrets

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"regexp"
)

var labelRe = regexp.MustCompile(`^[a-z0-9_]{1,32}$`)

// GenerateMTProtoSecret generates a cryptographically random 16-byte MTProto secret.
func GenerateMTProtoSecret() (MTProtoSecret, error) {
	var s MTProtoSecret
	if _, err := rand.Read(s[:]); err != nil {
		return s, fmt.Errorf("generate mtproto secret: %w", err)
	}
	return s, nil
}

const loginAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
const passwordAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// GenerateAdminLogin generates an 8-character lowercase alphanumeric login.
func GenerateAdminLogin() (string, error) {
	return randomString(loginAlphabet, 8)
}

// GenerateAdminPassword generates a password of at least 16 characters
// guaranteed to contain at least one letter and one digit.
func GenerateAdminPassword() (string, error) {
	const length = 20
	for {
		s, err := randomString(passwordAlphabet, length)
		if err != nil {
			return "", err
		}
		hasLetter, hasDigit := false, false
		for _, r := range s {
			if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' {
				hasLetter = true
			}
			if r >= '0' && r <= '9' {
				hasDigit = true
			}
		}
		if hasLetter && hasDigit {
			return s, nil
		}
	}
}

// ValidateUserLabel returns an error if the label does not match [a-z0-9_]{1,32}.
func ValidateUserLabel(label string) error {
	if !labelRe.MatchString(label) {
		return errors.New("label must match [a-z0-9_]{1,32}")
	}
	return nil
}

func randomString(alphabet string, length int) (string, error) {
	b := make([]byte, length)
	max := big.NewInt(int64(len(alphabet)))
	for i := range b {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", fmt.Errorf("random string: %w", err)
		}
		b[i] = alphabet[n.Int64()]
	}
	return string(b), nil
}
