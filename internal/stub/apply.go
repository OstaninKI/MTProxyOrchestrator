package stub

import (
	"fmt"
	"os"
	"path/filepath"
)

// Applier copies a stub template directory to the web root.
type Applier struct {
	WebRoot     string                                    // destination, e.g. /var/www/tgproxy-stub
	ReloadNginx func() error                             // injectable: calls "systemctl reload nginx" in prod
	MkdirAll    func(string, os.FileMode) error          // injectable; defaults to os.MkdirAll
	WriteFile   func(string, []byte, os.FileMode) error  // injectable; defaults to os.WriteFile
	ReadDir     func(string) ([]os.DirEntry, error)      // injectable; defaults to os.ReadDir
	ReadFile    func(string) ([]byte, error)              // injectable; defaults to os.ReadFile
	Remove      func(string) error                        // injectable; defaults to os.Remove
}

// DefaultApplier returns an Applier wired to real OS calls.
// reloadNginx should be: func() error { return exec.Command("systemctl", "reload", "nginx").Run() }
func DefaultApplier(webRoot string, reloadNginx func() error) Applier {
	return Applier{
		WebRoot:     webRoot,
		ReloadNginx: reloadNginx,
		MkdirAll:    os.MkdirAll,
		WriteFile:   os.WriteFile,
		ReadDir:     os.ReadDir,
		ReadFile:    os.ReadFile,
		Remove:      os.Remove,
	}
}

// Apply copies all files from srcDir to a.WebRoot, then calls ReloadNginx.
// On failure (nginx reload or any write error), it rolls back: removes what
// it wrote and restores files that existed before.
//
// Steps:
//  1. Read existing files in a.WebRoot (snapshot for rollback).
//  2. Write all files from srcDir to a.WebRoot, preserving relative paths.
//  3. Call a.ReloadNginx().
//  4. If step 2 or 3 fails: remove files written in step 2, restore snapshot.
//  5. Return nil on full success; return error on failure after rollback.
func (a Applier) Apply(srcDir string) error {
	// Step 1: snapshot existing files in WebRoot.
	snapshot, err := a.snapshotDir(a.WebRoot, "")
	if err != nil {
		return fmt.Errorf("snapshot web root: %w", err)
	}

	// Step 2: write all files from srcDir to WebRoot, tracking what was written.
	written, writeErr := a.writeDir(srcDir, "")
	if writeErr != nil {
		a.rollback(written, snapshot)
		return fmt.Errorf("write files: %w", writeErr)
	}

	// Step 3: reload nginx.
	if reloadErr := a.ReloadNginx(); reloadErr != nil {
		a.rollback(written, snapshot)
		return fmt.Errorf("reload nginx: %w", reloadErr)
	}

	return nil
}

// snapshotDir reads all regular files under root/relPath recursively into a map
// of (absolute path -> content).
func (a Applier) snapshotDir(root, relPath string) (map[string][]byte, error) {
	result := make(map[string][]byte)

	dir := root
	if relPath != "" {
		dir = filepath.Join(root, relPath)
	}

	entries, err := a.ReadDir(dir)
	if err != nil {
		// If the directory doesn't exist yet, snapshot is empty.
		if os.IsNotExist(err) {
			return result, nil
		}
		return nil, err
	}

	for _, entry := range entries {
		var childRel string
		if relPath == "" {
			childRel = entry.Name()
		} else {
			childRel = filepath.Join(relPath, entry.Name())
		}

		if entry.IsDir() {
			sub, err := a.snapshotDir(root, childRel)
			if err != nil {
				return nil, err
			}
			for k, v := range sub {
				result[k] = v
			}
			continue
		}

		if !entry.Type().IsRegular() {
			continue
		}

		absPath := filepath.Join(root, childRel)
		data, err := a.ReadFile(absPath)
		if err != nil {
			return nil, err
		}
		result[absPath] = data
	}

	return result, nil
}

// writeDir recursively copies files from srcDir/relPath to a.WebRoot/relPath.
// Returns list of absolute destination paths that were written.
func (a Applier) writeDir(srcDir, relPath string) ([]string, error) {
	var written []string

	dir := srcDir
	if relPath != "" {
		dir = filepath.Join(srcDir, relPath)
	}

	entries, err := a.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		var childRel string
		if relPath == "" {
			childRel = entry.Name()
		} else {
			childRel = filepath.Join(relPath, entry.Name())
		}

		destPath := filepath.Join(a.WebRoot, childRel)

		if entry.IsDir() {
			if err := a.MkdirAll(destPath, 0755); err != nil {
				return written, err
			}
			sub, err := a.writeDir(srcDir, childRel)
			written = append(written, sub...)
			if err != nil {
				return written, err
			}
			continue
		}

		if !entry.Type().IsRegular() {
			continue
		}

		srcPath := filepath.Join(srcDir, childRel)
		data, err := a.ReadFile(srcPath)
		if err != nil {
			return written, err
		}

		if err := a.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			return written, err
		}

		if err := a.WriteFile(destPath, data, 0644); err != nil {
			return written, err
		}

		written = append(written, destPath)
	}

	return written, nil
}

// rollback removes written files and restores the snapshot.
func (a Applier) rollback(written []string, snapshot map[string][]byte) {
	removeFn := a.Remove
	if removeFn == nil {
		removeFn = os.Remove
	}
	// Remove files written during Apply.
	for _, path := range written {
		_ = removeFn(path)
	}

	// Restore snapshot files.
	for path, data := range snapshot {
		_ = a.MkdirAll(filepath.Dir(path), 0755)
		_ = a.WriteFile(path, data, 0644)
	}
}
