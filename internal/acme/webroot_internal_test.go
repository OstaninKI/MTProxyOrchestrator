package acme

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/go-acme/lego/v4/challenge/http01"
)

// The panel runs with UMask=0077; tokens must still be world-readable or nginx
// (www-data) answers 403 and Let's Encrypt rejects the HTTP-01 challenge.
func TestWebrootProviderIgnoresRestrictiveUmask(t *testing.T) {
	old := syscall.Umask(0o077)
	t.Cleanup(func() { syscall.Umask(old) })

	root := filepath.Join(t.TempDir(), "acme")
	p := webrootProvider{dir: root}
	if err := p.Present("example.com", "tok", "keyauth"); err != nil {
		t.Fatalf("Present: %v", err)
	}

	tokenPath := filepath.Join(root, http01.ChallengePath("tok"))
	data, err := os.ReadFile(tokenPath)
	if err != nil || string(data) != "keyauth" {
		t.Fatalf("token content = %q, %v", data, err)
	}
	if mode := mustMode(t, tokenPath); mode != 0o644 {
		t.Errorf("token mode = %o, want 644", mode)
	}
	for _, dir := range []string{root, filepath.Join(root, ".well-known"), filepath.Dir(tokenPath)} {
		if mode := mustMode(t, dir); mode != 0o755 {
			t.Errorf("%s mode = %o, want 755", dir, mode)
		}
	}

	if err := p.CleanUp("example.com", "tok", "keyauth"); err != nil {
		t.Fatalf("CleanUp: %v", err)
	}
	if _, err := os.Stat(tokenPath); !os.IsNotExist(err) {
		t.Errorf("token not removed: %v", err)
	}
}

func mustMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}
