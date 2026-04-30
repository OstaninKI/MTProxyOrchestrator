package install

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSystemExecutorInstallFileSamePathKeepsContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tgproxy-cli")
	original := []byte("binary-data")
	if err := os.WriteFile(path, original, 0o755); err != nil {
		t.Fatal(err)
	}

	exec := NewSystemExecutor()
	if err := exec.InstallFile(path, path, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Fatalf("file content changed: got %q want %q", got, original)
	}
}
