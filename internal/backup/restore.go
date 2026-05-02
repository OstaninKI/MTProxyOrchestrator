package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// sensitiveExtensions lists file extensions that must be capped at mode 0600.
var sensitiveExtensions = map[string]struct{}{
	".toml": {},
	".json": {},
	".db":   {},
}

const secureDirPerm = 0700

// Resource limits for restore operations.
const (
	// MaxEncryptedArchiveBytes is the maximum size of the encrypted archive file (100 MB).
	MaxEncryptedArchiveBytes = 100 * 1024 * 1024
	// MaxDecryptedArchiveBytes is the maximum size of the decrypted tar.gz data (200 MB).
	MaxDecryptedArchiveBytes = 200 * 1024 * 1024
	// MaxExtractedFileBytes is the maximum size of a single extracted file (50 MB).
	MaxExtractedFileBytes = 50 * 1024 * 1024
	// MaxExtractedTotalBytes is the maximum total size of all extracted files (200 MB).
	MaxExtractedTotalBytes = 200 * 1024 * 1024
	// MaxExtractedFileCount is the maximum number of files that may be extracted (1000).
	MaxExtractedFileCount = 1000
)

// RestoreOptions configures a restore operation.
type RestoreOptions struct {
	// ArchivePath is the path to the encrypted .tar.gz.enc file.
	ArchivePath string
	// TargetDir is the directory into which files are restored (e.g. /etc/tgproxy).
	TargetDir string
	// Passphrase is the decryption passphrase.
	Passphrase string
}

// Restore decrypts the archive at opts.ArchivePath and extracts it to opts.TargetDir.
//
// It validates every tar entry path before writing:
//   - Rejects entries with ".." components (path traversal).
//   - Rejects absolute paths in tar entries.
//   - Rejects symlinks.
//   - All entries must land inside TargetDir after joining.
//
// File modes from the archive are preserved, but capped at 0600 for sensitive
// extensions (.toml, .json, .db).
//
// Services are not started or stopped; the caller is responsible for that.
func Restore(opts RestoreOptions) error {
	// Enforce encrypted archive size limit before reading fully into memory.
	fi, err := os.Stat(opts.ArchivePath)
	if err != nil {
		return fmt.Errorf("restore: stat archive: %w", err)
	}
	if fi.Size() > MaxEncryptedArchiveBytes {
		return fmt.Errorf("restore: encrypted archive size %d exceeds limit of %d bytes", fi.Size(), MaxEncryptedArchiveBytes)
	}

	// Read encrypted archive.
	raw, err := os.ReadFile(opts.ArchivePath)
	if err != nil {
		return fmt.Errorf("restore: read archive: %w", err)
	}

	minLen := saltLen + nonceLen + 1 // at least 1 byte of ciphertext
	if len(raw) < minLen {
		return fmt.Errorf("restore: archive too short (%d bytes)", len(raw))
	}

	salt := raw[:saltLen]
	nonce := raw[saltLen : saltLen+nonceLen]
	ciphertext := raw[saltLen+nonceLen:]

	// Derive key.
	key, err := deriveKey(opts.Passphrase, salt)
	if err != nil {
		return fmt.Errorf("restore: derive key: %w", err)
	}

	// Decrypt.
	block, err := aes.NewCipher(key)
	if err != nil {
		return fmt.Errorf("restore: create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return fmt.Errorf("restore: create GCM: %w", err)
	}
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return fmt.Errorf("restore: decrypt: %w", err)
	}

	// Enforce decrypted archive size limit.
	if int64(len(plaintext)) > MaxDecryptedArchiveBytes {
		return fmt.Errorf("restore: decrypted archive size %d exceeds limit of %d bytes", len(plaintext), MaxDecryptedArchiveBytes)
	}

	// Extract tar.gz from plaintext.
	if err := extractTarGz(plaintext, opts.TargetDir); err != nil {
		return fmt.Errorf("restore: extract: %w", err)
	}
	return nil
}

// extractTarGz decompresses and extracts the tar.gz bytes into targetDir.
// It enforces MaxExtractedFileCount, MaxExtractedFileBytes per file, and
// MaxExtractedTotalBytes total.
func extractTarGz(data []byte, targetDir string) error {
	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("open gzip: %w", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	var fileCount int
	var totalExtracted int64

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar: %w", err)
		}

		// Security: reject symlinks.
		if hdr.Typeflag == tar.TypeSymlink || hdr.Typeflag == tar.TypeLink {
			return fmt.Errorf("restore: rejected symlink entry: %s", hdr.Name)
		}

		// Security: reject absolute paths in the archive.
		if filepath.IsAbs(hdr.Name) {
			return fmt.Errorf("restore: rejected absolute path: %s", hdr.Name)
		}

		// Security: reject ".." components.
		clean := filepath.Clean(hdr.Name)
		if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return fmt.Errorf("restore: rejected path traversal: %s", hdr.Name)
		}
		for _, part := range strings.Split(clean, string(filepath.Separator)) {
			if part == ".." {
				return fmt.Errorf("restore: rejected path traversal in component: %s", hdr.Name)
			}
		}

		// Compute destination path.
		destPath := filepath.Join(targetDir, clean)

		// Security: ensure the final path is still inside targetDir.
		if !strings.HasPrefix(destPath, filepath.Clean(targetDir)+string(filepath.Separator)) {
			return fmt.Errorf("restore: path escapes target dir: %s", hdr.Name)
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(destPath, secureDirPerm); err != nil {
				return fmt.Errorf("restore: mkdir %s: %w", destPath, err)
			}
		case tar.TypeReg, 0: // regular file (0 is the default for old-style tars)
			// Enforce file count limit.
			fileCount++
			if fileCount > MaxExtractedFileCount {
				return fmt.Errorf("restore: archive exceeds maximum file count of %d", MaxExtractedFileCount)
			}
			// Enforce per-file size limit based on declared header size.
			if hdr.Size > MaxExtractedFileBytes {
				return fmt.Errorf("restore: file %s size %d exceeds per-file limit of %d bytes", hdr.Name, hdr.Size, MaxExtractedFileBytes)
			}
			// Enforce total extracted size limit (pre-check using declared header size).
			totalExtracted += hdr.Size
			if totalExtracted > MaxExtractedTotalBytes {
				return fmt.Errorf("restore: total extracted size exceeds limit of %d bytes", MaxExtractedTotalBytes)
			}
			written, err := extractFile(tr, hdr, destPath)
			if err != nil {
				return err
			}
			// If the actual written bytes exceed the declared size (sparse/decompression bomb),
			// update totalExtracted and re-check.
			if written > hdr.Size {
				totalExtracted += written - hdr.Size
				if totalExtracted > MaxExtractedTotalBytes {
					return fmt.Errorf("restore: total extracted size exceeds limit of %d bytes", MaxExtractedTotalBytes)
				}
			}
		default:
			// Skip unsupported entry types (devices, fifos, etc.).
		}
	}
	return nil
}

// extractFile writes a single tar entry to destPath with appropriate permissions.
// It returns the number of bytes written and any error.
func extractFile(tr *tar.Reader, hdr *tar.Header, destPath string) (int64, error) {
	// Ensure parent directory exists.
	if err := os.MkdirAll(filepath.Dir(destPath), secureDirPerm); err != nil {
		return 0, fmt.Errorf("restore: mkdir parent for %s: %w", destPath, err)
	}

	// Determine file mode: cap sensitive extensions at 0600.
	mode := os.FileMode(hdr.Mode) & 0777
	ext := strings.ToLower(filepath.Ext(destPath))
	if _, sensitive := sensitiveExtensions[ext]; sensitive {
		if mode > 0600 {
			mode = 0600
		}
	}

	f, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return 0, fmt.Errorf("restore: create file %s: %w", destPath, err)
	}
	defer f.Close()

	// Use LimitReader to enforce per-file size during actual write (defense in depth).
	n, err := io.Copy(f, io.LimitReader(tr, MaxExtractedFileBytes+1))
	if err != nil {
		return n, fmt.Errorf("restore: write file %s: %w", destPath, err)
	}
	if n > MaxExtractedFileBytes {
		return n, fmt.Errorf("restore: file %s actual size exceeds per-file limit of %d bytes", destPath, MaxExtractedFileBytes)
	}
	return n, nil
}
