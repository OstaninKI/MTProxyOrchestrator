package stub

import (
	"fmt"
	"io/fs"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeFS implements a minimal in-memory filesystem for testing.
type fakeFS struct {
	files map[string][]byte
	mu    sync.Mutex
}

func newFakeFS() *fakeFS { return &fakeFS{files: make(map[string][]byte)} }

func (f *fakeFS) writeFile(path string, data []byte, _ os.FileMode) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.files[path] = data
	return nil
}

func (f *fakeFS) readFile(path string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if d, ok := f.files[path]; ok {
		return d, nil
	}
	return nil, fmt.Errorf("not found: %s", path)
}

func (f *fakeFS) remove(path string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.files, path)
	return nil
}

func (f *fakeFS) readDir(path string) ([]os.DirEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	prefix := path + "/"
	seen := make(map[string]bool)
	var entries []os.DirEntry

	for filePath := range f.files {
		if !strings.HasPrefix(filePath, prefix) {
			continue
		}

		rest := filePath[len(prefix):]
		slashIdx := strings.Index(rest, "/")

		if slashIdx == -1 {
			// Direct file child.
			name := rest
			if !seen[name] {
				seen[name] = true
				entries = append(entries, fakeDirEntry{name: name, isDir: false})
			}
		} else {
			// Subdirectory.
			dirName := rest[:slashIdx]
			if !seen[dirName] {
				seen[dirName] = true
				entries = append(entries, fakeDirEntry{name: dirName, isDir: true})
			}
		}
	}

	return entries, nil
}

// fakeDirEntry implements os.DirEntry for testing.
type fakeDirEntry struct {
	name  string
	isDir bool
}

func (e fakeDirEntry) Name() string { return e.name }
func (e fakeDirEntry) IsDir() bool  { return e.isDir }
func (e fakeDirEntry) Type() fs.FileMode {
	if e.isDir {
		return fs.ModeDir
	}
	return 0
}
func (e fakeDirEntry) Info() (fs.FileInfo, error) {
	return fakeFI{name: e.name, isDir: e.isDir}, nil
}

// fakeFI implements fs.FileInfo.
type fakeFI struct {
	name  string
	isDir bool
}

func (fi fakeFI) Name() string      { return fi.name }
func (fi fakeFI) Size() int64       { return 0 }
func (fi fakeFI) Mode() fs.FileMode { return 0644 }
func (fi fakeFI) ModTime() time.Time { return time.Time{} }
func (fi fakeFI) IsDir() bool        { return fi.isDir }
func (fi fakeFI) Sys() any           { return nil }

// mkdirAllNoop is a no-op MkdirAll for fakeFS (directories are implicit).
func mkdirAllNoop(_ string, _ os.FileMode) error { return nil }

// makeApplier builds an Applier backed by two fakeFSes: src and dst.
func makeApplier(src, dst *fakeFS, webRoot string, reloadNginx func() error) Applier {
	return Applier{
		WebRoot:     webRoot,
		ReloadNginx: reloadNginx,
		MkdirAll:    mkdirAllNoop,
		WriteFile: func(path string, data []byte, mode os.FileMode) error {
			if strings.HasPrefix(path, webRoot) {
				return dst.writeFile(path, data, mode)
			}
			return src.writeFile(path, data, mode)
		},
		ReadDir: func(path string) ([]os.DirEntry, error) {
			if strings.HasPrefix(path, webRoot) {
				return dst.readDir(path)
			}
			return src.readDir(path)
		},
		ReadFile: func(path string) ([]byte, error) {
			if strings.HasPrefix(path, webRoot) {
				return dst.readFile(path)
			}
			return src.readFile(path)
		},
		Remove: dst.remove,
	}
}

// TestApplyWritesFiles verifies that both source files appear in the WebRoot.
func TestApplyWritesFiles(t *testing.T) {
	src := newFakeFS()
	dst := newFakeFS()

	srcDir := "/src"
	webRoot := "/webroot"

	src.files["/src/index.html"] = []byte("<html></html>")
	src.files["/src/style.css"] = []byte("body{}")

	reloaded := false
	a := makeApplier(src, dst, webRoot, func() error {
		reloaded = true
		return nil
	})

	if err := a.Apply(srcDir); err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}

	if !reloaded {
		t.Error("expected nginx reload to be called")
	}

	for _, name := range []string{"index.html", "style.css"} {
		key := webRoot + "/" + name
		if _, ok := dst.files[key]; !ok {
			t.Errorf("expected %s in WebRoot", name)
		}
	}
}

// TestApplyCallsNginxReload asserts ReloadNginx is called exactly once.
func TestApplyCallsNginxReload(t *testing.T) {
	src := newFakeFS()
	dst := newFakeFS()

	srcDir := "/src"
	webRoot := "/webroot"

	src.files["/src/index.html"] = []byte("<html></html>")

	callCount := 0
	a := makeApplier(src, dst, webRoot, func() error {
		callCount++
		return nil
	})

	if err := a.Apply(srcDir); err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}

	if callCount != 1 {
		t.Errorf("expected ReloadNginx called once, got %d", callCount)
	}
}

// TestApplyRollbackOnNginxError verifies rollback when nginx reload fails.
func TestApplyRollbackOnNginxError(t *testing.T) {
	src := newFakeFS()
	dst := newFakeFS()

	srcDir := "/src"
	webRoot := "/webroot"

	// Pre-populate WebRoot with old.html.
	dst.files["/webroot/old.html"] = []byte("old content")

	// Source has index.html.
	src.files["/src/index.html"] = []byte("<html>new</html>")

	a := makeApplier(src, dst, webRoot, func() error {
		return fmt.Errorf("nginx reload failed")
	})

	err := a.Apply(srcDir)
	if err == nil {
		t.Fatal("expected error from Apply, got nil")
	}

	// New file must be rolled back (removed).
	if _, ok := dst.files["/webroot/index.html"]; ok {
		t.Error("expected index.html to be rolled back (removed)")
	}

	// Old file must be restored.
	if _, ok := dst.files["/webroot/old.html"]; !ok {
		t.Error("expected old.html to be restored after rollback")
	}
}

// TestApplyRollbackOnWriteError verifies rollback when a write fails mid-apply.
func TestApplyRollbackOnWriteError(t *testing.T) {
	src := newFakeFS()
	dst := newFakeFS()

	srcDir := "/src"
	webRoot := "/webroot"

	// Pre-populate WebRoot with original.html.
	dst.files["/webroot/original.html"] = []byte("original")

	// Source has two files.
	src.files["/src/first.html"] = []byte("<html>first</html>")
	src.files["/src/second.html"] = []byte("<html>second</html>")

	writeCount := 0
	nginxCalled := false

	a := Applier{
		WebRoot: webRoot,
		ReloadNginx: func() error {
			nginxCalled = true
			return nil
		},
		MkdirAll: mkdirAllNoop,
		WriteFile: func(path string, data []byte, mode os.FileMode) error {
			if strings.HasPrefix(path, webRoot) {
				writeCount++
				if writeCount == 2 {
					// Fail on the second write to WebRoot.
					return fmt.Errorf("disk full")
				}
				return dst.writeFile(path, data, mode)
			}
			return src.writeFile(path, data, mode)
		},
		ReadDir: func(path string) ([]os.DirEntry, error) {
			if strings.HasPrefix(path, webRoot) {
				return dst.readDir(path)
			}
			return src.readDir(path)
		},
		ReadFile: func(path string) ([]byte, error) {
			if strings.HasPrefix(path, webRoot) {
				return dst.readFile(path)
			}
			return src.readFile(path)
		},
		Remove: dst.remove,
	}

	err := a.Apply(srcDir)
	if err == nil {
		t.Fatal("expected error from Apply, got nil")
	}

	if nginxCalled {
		t.Error("nginx should not be called after write error")
	}

	// Original file must be restored.
	if _, ok := dst.files["/webroot/original.html"]; !ok {
		t.Error("expected original.html to be restored after rollback")
	}
}

// TestApplySubdirectory verifies that files in subdirectories are copied correctly.
func TestApplySubdirectory(t *testing.T) {
	src := newFakeFS()
	dst := newFakeFS()

	srcDir := "/src"
	webRoot := "/webroot"

	src.files["/src/img/logo.svg"] = []byte("<svg/>")

	a := makeApplier(src, dst, webRoot, func() error { return nil })

	if err := a.Apply(srcDir); err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}

	key := "/webroot/img/logo.svg"
	if _, ok := dst.files[key]; !ok {
		t.Errorf("expected %s in WebRoot after apply", key)
	}
}
