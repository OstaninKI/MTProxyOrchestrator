package acme

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-acme/lego/v4/challenge/http01"
)

// webrootProvider serves HTTP-01 tokens from a directory nginx reads as a
// non-root worker. Unlike lego's webroot provider it chmods every directory and
// token file explicitly, so the panel's UMask=0077 cannot make them unreadable
// to nginx (which answers 403 and fails the challenge).
type webrootProvider struct {
	dir string
}

// Present writes the key authorization to <dir>/.well-known/acme-challenge/<token>.
func (p webrootProvider) Present(_, token, keyAuth string) error {
	path := filepath.Join(p.dir, http01.ChallengePath(token))
	if err := mkdirReadable(p.dir, filepath.Dir(path)); err != nil {
		return fmt.Errorf("create webroot challenge dir: %w", err)
	}
	if err := os.WriteFile(path, []byte(keyAuth), 0o644); err != nil {
		return fmt.Errorf("write webroot challenge: %w", err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		return fmt.Errorf("chmod webroot challenge: %w", err)
	}
	return nil
}

// CleanUp removes the token file.
func (p webrootProvider) CleanUp(_, token, _ string) error {
	if err := os.Remove(filepath.Join(p.dir, http01.ChallengePath(token))); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove webroot challenge: %w", err)
	}
	return nil
}

// mkdirReadable creates root and every directory from root down to dir with
// mode 0755, chmodding each one so the process umask does not narrow it.
func mkdirReadable(root, dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	rel, err := filepath.Rel(root, dir)
	if err != nil || strings.HasPrefix(rel, "..") {
		return fmt.Errorf("%s is outside webroot %s", dir, root)
	}
	cur := root
	if err := os.Chmod(cur, 0o755); err != nil {
		return err
	}
	if rel == "." {
		return nil
	}
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		cur = filepath.Join(cur, part)
		if err := os.Chmod(cur, 0o755); err != nil {
			return err
		}
	}
	return nil
}
