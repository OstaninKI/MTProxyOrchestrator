package panel

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
)

const (
	csrfTokenLen   = 32
	csrfFormField  = "_csrf"
	csrfCookieName = "csrf_token"
)

// NewCSRFToken generates a cryptographically random hex token.
func NewCSRFToken() (string, error) {
	b := make([]byte, csrfTokenLen)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// CSRFField returns the form field name for CSRF tokens.
func CSRFField() string { return csrfFormField }

// SetCSRFCookie writes the CSRF token to a response cookie scoped to path.
// Pass the panel path so the cookie does not leak to unrelated paths.
func SetCSRFCookie(w http.ResponseWriter, token string, secure bool, path string) {
	if path == "" {
		path = "/"
	}
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    token,
		Path:     path,
		HttpOnly: false,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	})
}

// ValidateCSRF checks that the request form field matches the CSRF cookie.
func ValidateCSRF(r *http.Request) bool {
	cookie, err := r.Cookie(csrfCookieName)
	if err != nil || cookie.Value == "" {
		return false
	}
	formVal := r.FormValue(csrfFormField)
	if formVal == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(formVal)) == 1
}
