package secrets_test

import (
	"strings"
	"testing"
	"unicode"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/secrets"
)

func TestGenerateMTProtoSecret_Length(t *testing.T) {
	s, err := secrets.GenerateMTProtoSecret()
	if err != nil {
		t.Fatal(err)
	}
	// 16 bytes encoded as 32 hex characters
	if len(s.Hex()) != 32 {
		t.Errorf("hex length = %d, want 32", len(s.Hex()))
	}
}

func TestGenerateMTProtoSecret_Uniqueness(t *testing.T) {
	a, err := secrets.GenerateMTProtoSecret()
	if err != nil {
		t.Fatal(err)
	}
	b, err := secrets.GenerateMTProtoSecret()
	if err != nil {
		t.Fatal(err)
	}
	if a.Hex() == b.Hex() {
		t.Error("two consecutive secrets are identical")
	}
}

func TestGenerateAdminLogin_Length(t *testing.T) {
	login, err := secrets.GenerateAdminLogin()
	if err != nil {
		t.Fatal(err)
	}
	if len(login) != 8 {
		t.Errorf("login length = %d, want 8", len(login))
	}
}

func TestGenerateAdminLogin_Alphabet(t *testing.T) {
	for range 20 {
		login, err := secrets.GenerateAdminLogin()
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range login {
			if !unicode.IsLower(r) && !unicode.IsDigit(r) {
				t.Errorf("login %q contains invalid character %q", login, r)
			}
		}
	}
}

func TestGenerateAdminPassword_MinLength(t *testing.T) {
	for range 20 {
		pass, err := secrets.GenerateAdminPassword()
		if err != nil {
			t.Fatal(err)
		}
		if len(pass) < 16 {
			t.Errorf("password length = %d, want >= 16", len(pass))
		}
	}
}

func TestGenerateAdminPassword_ContainsLetterAndDigit(t *testing.T) {
	for range 50 {
		pass, err := secrets.GenerateAdminPassword()
		if err != nil {
			t.Fatal(err)
		}
		hasLetter := strings.IndexFunc(pass, unicode.IsLetter) >= 0
		hasDigit := strings.IndexFunc(pass, unicode.IsDigit) >= 0
		if !hasLetter || !hasDigit {
			t.Errorf("password %q must contain at least one letter and one digit", pass)
		}
	}
}

func TestValidateUserLabel_Valid(t *testing.T) {
	valid := []string{"alice", "bob_1", "user123", strings.Repeat("a", 32)}
	for _, l := range valid {
		if err := secrets.ValidateUserLabel(l); err != nil {
			t.Errorf("ValidateUserLabel(%q) = %v, want nil", l, err)
		}
	}
}

func TestValidateUserLabel_Invalid(t *testing.T) {
	invalid := []string{
		"",
		"Alice",                 // uppercase
		"user-1",                // hyphen
		"user 1",                // space
		strings.Repeat("a", 33), // too long
		"user!",                 // special char
	}
	for _, l := range invalid {
		if err := secrets.ValidateUserLabel(l); err == nil {
			t.Errorf("ValidateUserLabel(%q) = nil, want error", l)
		}
	}
}

func TestMaxActiveSecrets(t *testing.T) {
	if secrets.MaxActiveSecrets != 16 {
		t.Errorf("MaxActiveSecrets = %d, want 16", secrets.MaxActiveSecrets)
	}
}
