package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// --- fake FS helpers ---------------------------------------------------------

// fakeFile represents a file in the fake filesystem.
type fakeFile struct {
	name    string
	content []byte
	mode    fs.FileMode
}

// fakeFS implements FileReader backed by an in-memory map.
type fakeFS struct {
	files map[string]*fakeFile // abs path → file
	dirs  map[string][]string  // abs dir path → children (file names)
}

func newFakeFS() *fakeFS {
	return &fakeFS{
		files: make(map[string]*fakeFile),
		dirs:  make(map[string][]string),
	}
}

func (f *fakeFS) addFile(absPath string, content []byte, mode fs.FileMode) {
	f.files[absPath] = &fakeFile{name: filepath.Base(absPath), content: content, mode: mode}
	dir := filepath.Dir(absPath)
	// Ensure the directory entry exists and contains this file name (avoid duplicates).
	for _, existing := range f.dirs[dir] {
		if existing == filepath.Base(absPath) {
			return
		}
	}
	f.dirs[dir] = append(f.dirs[dir], filepath.Base(absPath))
	// Ensure each ancestor directory is registered in its parent as well.
	f.ensureAncestors(dir)
}

// ensureAncestors registers dir as an entry in its parent directory (recursively).
// This allows Stat and ReadDir to work on intermediate directories.
func (f *fakeFS) ensureAncestors(dir string) {
	parent := filepath.Dir(dir)
	if parent == dir {
		// Reached filesystem root.
		return
	}
	base := filepath.Base(dir)
	for _, existing := range f.dirs[parent] {
		if existing == base {
			return
		}
	}
	f.dirs[parent] = append(f.dirs[parent], base)
	// Ensure the dir itself has an entry in dirs (even if empty) so Stat works.
	if _, ok := f.dirs[dir]; !ok {
		f.dirs[dir] = nil
	}
	f.ensureAncestors(parent)
}

// fakeFileInfo implements fs.FileInfo for a fakeFile.
type fakeFileInfo struct {
	ff *fakeFile
}

func (fi fakeFileInfo) Name() string       { return fi.ff.name }
func (fi fakeFileInfo) Size() int64        { return int64(len(fi.ff.content)) }
func (fi fakeFileInfo) Mode() fs.FileMode  { return fi.ff.mode }
func (fi fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (fi fakeFileInfo) IsDir() bool        { return false }
func (fi fakeFileInfo) Sys() interface{}   { return nil }

// fakeDirInfo implements fs.FileInfo for a directory.
type fakeDirInfo struct{ name string }

func (di fakeDirInfo) Name() string       { return di.name }
func (di fakeDirInfo) Size() int64        { return 0 }
func (di fakeDirInfo) Mode() fs.FileMode  { return 0755 }
func (di fakeDirInfo) ModTime() time.Time { return time.Time{} }
func (di fakeDirInfo) IsDir() bool        { return true }
func (di fakeDirInfo) Sys() interface{}   { return nil }

func (f *fakeFS) Stat(name string) (fs.FileInfo, error) {
	if ff, ok := f.files[name]; ok {
		return fakeFileInfo{ff}, nil
	}
	if _, ok := f.dirs[name]; ok {
		return fakeDirInfo{filepath.Base(name)}, nil
	}
	return nil, &os.PathError{Op: "stat", Path: name, Err: os.ErrNotExist}
}

func (f *fakeFS) Open(name string) (io.ReadCloser, error) {
	ff, ok := f.files[name]
	if !ok {
		return nil, &os.PathError{Op: "open", Path: name, Err: os.ErrNotExist}
	}
	return io.NopCloser(bytes.NewReader(ff.content)), nil
}

// fakeDirEntry implements fs.DirEntry.
type fakeDirEntry struct {
	name  string
	isDir bool
}

func (de fakeDirEntry) Name() string { return de.name }
func (de fakeDirEntry) IsDir() bool  { return de.isDir }
func (de fakeDirEntry) Type() fs.FileMode {
	if de.isDir {
		return fs.ModeDir
	}
	return 0
}
func (de fakeDirEntry) Info() (fs.FileInfo, error) { return nil, nil }

func (f *fakeFS) ReadDir(name string) ([]fs.DirEntry, error) {
	children, ok := f.dirs[name]
	if !ok {
		return nil, &os.PathError{Op: "readdir", Path: name, Err: os.ErrNotExist}
	}
	var out []fs.DirEntry
	for _, child := range children {
		full := filepath.Join(name, child)
		_, isFile := f.files[full]
		_, isDir := f.dirs[full]
		out = append(out, fakeDirEntry{name: child, isDir: isDir && !isFile})
	}
	return out, nil
}

// --- tests -------------------------------------------------------------------

// TestRoundTrip verifies that a backup+restore cycle preserves file content.
func TestRoundTrip(t *testing.T) {
	ffs := newFakeFS()
	configDir := "/etc/tgproxy"

	// Mandatory files.
	ffs.addFile(configDir+"/config.toml", []byte("mode = single"), 0600)
	ffs.addFile(configDir+"/teleproxy.toml", []byte("port = 443"), 0600)
	ffs.addFile(configDir+"/secrets/users.json", []byte(`{"users":[]}`), 0600)
	ffs.addFile(configDir+"/panel.db", []byte("sqlite data"), 0600)

	// Optional files.
	ffs.addFile(configDir+"/sing-box.json", []byte(`{"inbounds":[]}`), 0600)
	ffs.addFile(configDir+"/nodes/outbounds.json", []byte(`{"outbounds":[]}`), 0600)

	// stub-templates directory with one file.
	ffs.addFile(configDir+"/stub-templates/index.html", []byte("<html>stub</html>"), 0644)

	destFile := filepath.Join(t.TempDir(), "backup.tar.gz.enc")
	restoreDir := t.TempDir()

	// Create backup.
	err := Create(BackupOptions{
		ConfigDir:  configDir,
		PanelDB:    configDir + "/panel.db",
		Passphrase: "test-passphrase-123",
		DestPath:   destFile,
		Reader:     ffs,
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Verify archive exists and is mode 0600.
	fi, err := os.Stat(destFile)
	if err != nil {
		t.Fatalf("archive not created: %v", err)
	}
	if fi.Mode().Perm() != 0600 {
		t.Errorf("archive mode = %o, want 0600", fi.Mode().Perm())
	}

	// Restore backup.
	err = Restore(RestoreOptions{
		ArchivePath: destFile,
		TargetDir:   restoreDir,
		Passphrase:  "test-passphrase-123",
	})
	if err != nil {
		t.Fatalf("Restore failed: %v", err)
	}

	// Verify restored files.
	checks := map[string]string{
		"config.toml":               "mode = single",
		"teleproxy.toml":            "port = 443",
		"secrets/users.json":        `{"users":[]}`,
		"panel.db":                  "sqlite data",
		"sing-box.json":             `{"inbounds":[]}`,
		"nodes/outbounds.json":      `{"outbounds":[]}`,
		"stub-templates/index.html": "<html>stub</html>",
	}
	for rel, want := range checks {
		full := filepath.Join(restoreDir, rel)
		got, err := os.ReadFile(full)
		if err != nil {
			t.Errorf("missing restored file %s: %v", rel, err)
			continue
		}
		if string(got) != want {
			t.Errorf("file %s: got %q, want %q", rel, got, want)
		}
	}
}

// TestPassphraseMismatch verifies that restore with the wrong passphrase fails.
func TestPassphraseMismatch(t *testing.T) {
	ffs := newFakeFS()
	configDir := "/etc/tgproxy"
	ffs.addFile(configDir+"/config.toml", []byte("mode = single"), 0600)
	ffs.addFile(configDir+"/teleproxy.toml", []byte("port = 443"), 0600)
	ffs.addFile(configDir+"/secrets/users.json", []byte(`{}`), 0600)
	ffs.addFile(configDir+"/panel.db", []byte("db"), 0600)

	destFile := filepath.Join(t.TempDir(), "backup.tar.gz.enc")
	err := Create(BackupOptions{
		ConfigDir:  configDir,
		PanelDB:    configDir + "/panel.db",
		Passphrase: "correct-passphrase",
		DestPath:   destFile,
		Reader:     ffs,
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	err = Restore(RestoreOptions{
		ArchivePath: destFile,
		TargetDir:   t.TempDir(),
		Passphrase:  "wrong-passphrase",
	})
	if err == nil {
		t.Fatal("expected restore to fail with wrong passphrase, but it succeeded")
	}
}

// TestPathTraversalRejected verifies that tar entries with ".." are rejected.
func TestPathTraversalRejected(t *testing.T) {
	// Build a malicious tar.gz.enc archive by hand.
	malArchive := buildMaliciousTarGzEnc(t, "../etc/passwd", []byte("malicious"))

	err := Restore(RestoreOptions{
		ArchivePath: malArchive,
		TargetDir:   t.TempDir(),
		Passphrase:  "passphrase",
	})
	if err == nil {
		t.Fatal("expected restore to reject path traversal, but it succeeded")
	}
}

// TestMissingOptionalFilesSkipped verifies that optional missing files don't cause errors.
func TestMissingOptionalFilesSkipped(t *testing.T) {
	ffs := newFakeFS()
	configDir := "/etc/tgproxy"

	// Only the mandatory files; no optional files.
	ffs.addFile(configDir+"/config.toml", []byte("mode = single"), 0600)
	ffs.addFile(configDir+"/teleproxy.toml", []byte("port = 443"), 0600)
	ffs.addFile(configDir+"/secrets/users.json", []byte(`{}`), 0600)
	ffs.addFile(configDir+"/panel.db", []byte("db"), 0600)
	// sing-box.json, outbounds.json, stub-templates — all absent.

	destFile := filepath.Join(t.TempDir(), "backup.tar.gz.enc")
	err := Create(BackupOptions{
		ConfigDir:  configDir,
		PanelDB:    configDir + "/panel.db",
		Passphrase: "pass",
		DestPath:   destFile,
		Reader:     ffs,
	})
	if err != nil {
		t.Fatalf("Create with missing optional files failed: %v", err)
	}

	restoreDir := t.TempDir()
	err = Restore(RestoreOptions{
		ArchivePath: destFile,
		TargetDir:   restoreDir,
		Passphrase:  "pass",
	})
	if err != nil {
		t.Fatalf("Restore failed: %v", err)
	}

	// Mandatory files must be present.
	for _, rel := range []string{"config.toml", "teleproxy.toml", "secrets/users.json", "panel.db"} {
		if _, err := os.Stat(filepath.Join(restoreDir, rel)); err != nil {
			t.Errorf("expected mandatory file %s to be restored: %v", rel, err)
		}
	}

	// Optional files must be absent.
	for _, rel := range []string{"sing-box.json", "nodes/outbounds.json"} {
		if _, err := os.Stat(filepath.Join(restoreDir, rel)); err == nil {
			t.Errorf("optional file %s should not exist in restore dir", rel)
		}
	}
}

// buildMaliciousTarGzEnc builds a tar.gz.enc archive that contains a single
// entry with the given (malicious) tarName and the given content.
// It uses passphrase "passphrase" to encrypt.
func buildMaliciousTarGzEnc(t *testing.T, tarName string, content []byte) string {
	t.Helper()

	// Build tar.gz in memory.
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	hdr := &tar.Header{
		Name:     tarName,
		Size:     int64(len(content)),
		Mode:     0644,
		Typeflag: tar.TypeReg,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("write header: %v", err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatalf("write content: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}

	plaintext := buf.Bytes()

	// Encrypt.
	salt := make([]byte, saltLen)
	for i := range salt {
		salt[i] = byte(i) // deterministic for test
	}
	key, err := deriveKey("passphrase", salt)
	if err != nil {
		t.Fatalf("derive key: %v", err)
	}

	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	nonce := make([]byte, nonceLen) // all-zero nonce for test
	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)

	outPath := filepath.Join(t.TempDir(), "malicious.tar.gz.enc")
	var out bytes.Buffer
	out.Write(salt)
	out.Write(nonce)
	out.Write(ciphertext)
	if err := os.WriteFile(outPath, out.Bytes(), 0600); err != nil {
		t.Fatalf("write malicious archive: %v", err)
	}
	return outPath
}
