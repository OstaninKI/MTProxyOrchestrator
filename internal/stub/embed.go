package stub

import (
	"bytes"
	_ "embed"
	"os"
)

//go:embed default/index.html
var defaultStubHTML []byte

// legacyStubHTML is the minimal placeholder written by older installers.
// Used to detect servers that have never had a real stub applied.
var legacyStubHTML = []byte("<!DOCTYPE html>\n<html lang=\"en\">\n<head><meta charset=\"UTF-8\"><title>Welcome</title></head>\n<body><h1>Welcome</h1></body>\n</html>\n")

// DefaultStubHTML returns the built-in "coming soon" page used during
// initial installation before the operator applies a custom template.
func DefaultStubHTML() []byte {
	out := make([]byte, len(defaultStubHTML))
	copy(out, defaultStubHTML)
	return out
}

// MigrateStubIfLegacy replaces the file at path with DefaultStubHTML if it
// still contains the legacy minimal placeholder from older installs.
// Returns true if the file was replaced, false if it was already customised
// or did not exist. Safe to call on every reconcile run.
func MigrateStubIfLegacy(path string) (bool, error) {
	current, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if !bytes.Equal(bytes.TrimRight(current, "\r\n"), bytes.TrimRight(legacyStubHTML, "\r\n")) {
		return false, nil
	}
	if err := os.WriteFile(path, DefaultStubHTML(), 0o644); err != nil {
		return false, err
	}
	return true, nil
}
