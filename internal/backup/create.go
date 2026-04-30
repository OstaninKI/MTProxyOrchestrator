// Package backup implements encrypted backup and restore for /etc/tgproxy configuration.
package backup

import (
	"archive/tar"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"golang.org/x/crypto/scrypt"
)

const (
	scryptN     = 32768
	scryptR     = 8
	scryptP     = 1
	keyLen      = 32
	saltLen     = 32
	nonceLen    = 12
	archivePerm = 0600
)

// FileReader is an FS abstraction used to read source files during backup.
// The real implementation uses os; tests can substitute a fake.
type FileReader interface {
	// Open opens the named file for reading.
	Open(name string) (io.ReadCloser, error)
	// Stat returns file info for the named file.
	Stat(name string) (fs.FileInfo, error)
	// ReadDir reads a directory and returns its entries.
	ReadDir(name string) ([]fs.DirEntry, error)
}

// OSFileReader is the production FileReader that delegates to the real OS.
type OSFileReader struct{}

func (OSFileReader) Open(name string) (io.ReadCloser, error) {
	return os.Open(name)
}

func (OSFileReader) Stat(name string) (fs.FileInfo, error) {
	return os.Stat(name)
}

func (OSFileReader) ReadDir(name string) ([]fs.DirEntry, error) {
	return os.ReadDir(name)
}

// BackupOptions configures a backup operation.
type BackupOptions struct {
	// ConfigDir is the root of the tgproxy config tree (e.g. /etc/tgproxy).
	ConfigDir string
	// PanelDB is the path to the SQLite database file.
	PanelDB string
	// Passphrase is the encryption passphrase.
	Passphrase string
	// DestPath is the output file path (e.g. /tmp/backup.tar.gz.enc).
	DestPath string
	// Reader is the FS abstraction; nil defaults to OSFileReader.
	Reader FileReader
}

// Create builds an encrypted backup archive at opts.DestPath.
//
// Layout on disk: salt (32 bytes) | nonce (12 bytes) | AES-256-GCM ciphertext.
// The plaintext is a gzip-compressed tar archive with paths relative to ConfigDir.
func Create(opts BackupOptions) error {
	if opts.Reader == nil {
		opts.Reader = OSFileReader{}
	}

	// Collect the in-memory tar.gz.
	plaintext, err := buildTarGz(opts)
	if err != nil {
		return fmt.Errorf("backup: build archive: %w", err)
	}

	// Derive encryption key.
	salt := make([]byte, saltLen)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return fmt.Errorf("backup: generate salt: %w", err)
	}
	key, err := deriveKey(opts.Passphrase, salt)
	if err != nil {
		return fmt.Errorf("backup: derive key: %w", err)
	}

	// Encrypt with AES-256-GCM.
	block, err := aes.NewCipher(key)
	if err != nil {
		return fmt.Errorf("backup: create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return fmt.Errorf("backup: create GCM: %w", err)
	}
	nonce := make([]byte, nonceLen)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return fmt.Errorf("backup: generate nonce: %w", err)
	}
	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)

	// Write salt + nonce + ciphertext.
	f, err := os.OpenFile(opts.DestPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, archivePerm)
	if err != nil {
		return fmt.Errorf("backup: create dest file: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(salt); err != nil {
		return fmt.Errorf("backup: write salt: %w", err)
	}
	if _, err := f.Write(nonce); err != nil {
		return fmt.Errorf("backup: write nonce: %w", err)
	}
	if _, err := f.Write(ciphertext); err != nil {
		return fmt.Errorf("backup: write ciphertext: %w", err)
	}
	return nil
}

// deriveKey derives a 32-byte AES key from passphrase and salt using scrypt.
func deriveKey(passphrase string, salt []byte) ([]byte, error) {
	return scrypt.Key([]byte(passphrase), salt, scryptN, scryptR, scryptP, keyLen)
}

// buildTarGz creates the plaintext tar.gz in memory and returns its bytes.
func buildTarGz(opts BackupOptions) ([]byte, error) {
	pr, pw := io.Pipe()
	errCh := make(chan error, 1)

	go func() {
		gw := gzip.NewWriter(pw)
		tw := tar.NewWriter(gw)

		var writeErr error
		if writeErr = addBackupEntries(tw, opts); writeErr != nil {
			pw.CloseWithError(writeErr)
			errCh <- writeErr
			return
		}
		if writeErr = tw.Close(); writeErr != nil {
			writeErr = fmt.Errorf("backup: close tar: %w", writeErr)
			pw.CloseWithError(writeErr)
			errCh <- writeErr
			return
		}
		if writeErr = gw.Close(); writeErr != nil {
			writeErr = fmt.Errorf("backup: close gzip: %w", writeErr)
			pw.CloseWithError(writeErr)
			errCh <- writeErr
			return
		}
		pw.Close()
		errCh <- nil
	}()

	data, readErr := io.ReadAll(pr)
	buildErr := <-errCh
	if buildErr != nil {
		return nil, buildErr
	}
	if readErr != nil {
		return nil, fmt.Errorf("backup: read archive pipe: %w", readErr)
	}
	return data, nil
}

// addBackupEntries adds all required (and optional) config files/dirs to tw.
func addBackupEntries(tw *tar.Writer, opts BackupOptions) error {
	type entry struct {
		path     string
		optional bool
		isDir    bool
	}

	entries := []entry{
		{path: filepath.Join(opts.ConfigDir, "config.toml"), optional: false},
		{path: filepath.Join(opts.ConfigDir, "teleproxy.toml"), optional: false},
		{path: filepath.Join(opts.ConfigDir, "sing-box.json"), optional: true},
		{path: filepath.Join(opts.ConfigDir, "secrets", "users.json"), optional: false},
		{path: filepath.Join(opts.ConfigDir, "nodes", "outbounds.json"), optional: true},
		{path: filepath.Join(opts.ConfigDir, "stub-templates"), optional: true, isDir: true},
		{path: opts.PanelDB, optional: false},
	}

	for _, e := range entries {
		if e.isDir {
			if err := addDirToTar(tw, e.path, opts.ConfigDir, opts.Reader); err != nil {
				if os.IsNotExist(err) && e.optional {
					continue
				}
				return err
			}
			continue
		}
		if err := addFileToTar(tw, e.path, opts.ConfigDir, opts.Reader); err != nil {
			if os.IsNotExist(err) && e.optional {
				continue
			}
			return fmt.Errorf("backup: add %s: %w", e.path, err)
		}
	}
	return nil
}

// addFileToTar adds a single file to the tar archive.
// relBase is the prefix stripped to produce relative tar paths.
func addFileToTar(tw *tar.Writer, absPath, relBase string, r FileReader) error {
	fi, err := r.Stat(absPath)
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("backup: %s is not a regular file", absPath)
	}

	relPath, err := filepath.Rel(relBase, absPath)
	if err != nil {
		return fmt.Errorf("backup: rel path for %s: %w", absPath, err)
	}

	hdr := &tar.Header{
		Name:     relPath,
		Size:     fi.Size(),
		Mode:     int64(fi.Mode()),
		ModTime:  fi.ModTime(),
		Typeflag: tar.TypeReg,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("backup: write tar header for %s: %w", relPath, err)
	}

	f, err := r.Open(absPath)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := io.Copy(tw, f); err != nil {
		return fmt.Errorf("backup: copy %s: %w", absPath, err)
	}
	return nil
}

// addDirToTar recursively adds all regular files within dirPath to the tar archive.
func addDirToTar(tw *tar.Writer, dirPath, relBase string, r FileReader) error {
	if _, err := r.Stat(dirPath); err != nil {
		return err
	}
	return walkDir(tw, dirPath, relBase, r)
}

// walkDir is a recursive helper that walks dirPath and adds files to tw.
func walkDir(tw *tar.Writer, dirPath, relBase string, r FileReader) error {
	entries, err := r.ReadDir(dirPath)
	if err != nil {
		return fmt.Errorf("backup: read dir %s: %w", dirPath, err)
	}
	for _, de := range entries {
		fullPath := filepath.Join(dirPath, de.Name())
		if de.IsDir() {
			if err := walkDir(tw, fullPath, relBase, r); err != nil {
				return err
			}
			continue
		}
		if de.Type().IsRegular() {
			if err := addFileToTar(tw, fullPath, relBase, r); err != nil {
				return err
			}
		}
		// skip symlinks and other special files
	}
	return nil
}
