package stub

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// MaxZipSize is the maximum allowed uncompressed ZIP size in bytes (5 MB).
const MaxZipSize = 5 * 1024 * 1024

// Limits for extracted custom stub archives.
const (
	MaxZipEntries          = 256
	MaxExtractedFileBytes  = 5 * 1024 * 1024
	MaxExtractedTotalBytes = 20 * 1024 * 1024
)

// AllowedExtensions is the set of permitted file extensions in an uploaded ZIP.
var AllowedExtensions = map[string]bool{
	".html": true, ".htm": true,
	".css": true,
	".svg": true,
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".ico": true,
	".woff": true, ".woff2": true, ".ttf": true, ".eot": true, ".otf": true,
}

var eventHandlerAttrPattern = regexp.MustCompile(`(?i)\son[a-z0-9_-]+\s*=`)
var svgExternalReferencePattern = regexp.MustCompile(`(?i)\b(?:href|xlink:href|src)\s*=\s*["']?\s*(?:https?:)?//`)
var protocolRelativeURLPattern = regexp.MustCompile(`//[a-z0-9]`)
var svgSetElementPattern = regexp.MustCompile(`(?i)<set[\s>/]`)

// ValidationError is a specific validation failure.
type ValidationError struct {
	File   string // file name inside ZIP, or "" for archive-level errors
	Reason string
}

func (e ValidationError) Error() string {
	if e.File == "" {
		return e.Reason
	}
	return fmt.Sprintf("%s: %s", e.File, e.Reason)
}

// Validate reads a ZIP archive from r (size bytes) and returns all validation errors found.
// Returns nil if the archive is valid.
// Checks (in order, all checks run even if earlier ones fail):
//  1. Size <= MaxZipSize
//  2. No path traversal: reject entries where filepath.Clean(name) contains ".." or starts with "/"
//  3. No symlinks: reject entries where entry.Mode()&os.ModeSymlink != 0
//  4. Extension allowlist: reject entries not in AllowedExtensions (directories are exempt)
//  5. No active content or external URLs in .html/.css/.svg content.
func Validate(r io.ReaderAt, size int64) []ValidationError {
	var errs []ValidationError

	// Check 1: size limit
	if size > MaxZipSize {
		errs = append(errs, ValidationError{
			File:   "",
			Reason: fmt.Sprintf("archive size %d exceeds maximum allowed size %d", size, MaxZipSize),
		})
		return errs
	}

	zr, err := zip.NewReader(r, size)
	if err != nil {
		errs = append(errs, ValidationError{
			File:   "",
			Reason: fmt.Sprintf("failed to open ZIP archive: %s", err),
		})
		return errs
	}

	if len(zr.File) > MaxZipEntries {
		errs = append(errs, ValidationError{
			File:   "",
			Reason: fmt.Sprintf("archive contains too many files (max %d)", MaxZipEntries),
		})
		return errs
	}

	var totalExtracted uint64
	var hasRootHTML bool
	for _, entry := range zr.File {
		name := entry.Name

		// Check 2: path traversal
		clean := filepath.Clean(name)
		if strings.Contains(clean, "..") || strings.HasPrefix(clean, "/") {
			errs = append(errs, ValidationError{
				File:   name,
				Reason: "path traversal detected",
			})
			continue
		}

		// Track root-level HTML for entry point validation.
		// "clean" contains the cleaned path; if it has no separator it's at the root.
		if !strings.ContainsRune(clean, '/') && !strings.ContainsRune(clean, '\\') {
			if e := strings.ToLower(filepath.Ext(clean)); e == ".html" || e == ".htm" {
				hasRootHTML = true
			}
		}

		// Check 3: symlinks
		if entry.Mode()&os.ModeSymlink != 0 {
			errs = append(errs, ValidationError{
				File:   name,
				Reason: "symlinks are not allowed",
			})
			continue
		}

		// Directories are exempt from extension and content checks
		if entry.FileInfo().IsDir() {
			continue
		}
		if entry.UncompressedSize64 > MaxExtractedFileBytes {
			errs = append(errs, ValidationError{
				File:   name,
				Reason: fmt.Sprintf("file exceeds maximum extracted size of %d bytes", MaxExtractedFileBytes),
			})
			continue
		}
		totalExtracted += entry.UncompressedSize64
		if totalExtracted > MaxExtractedTotalBytes {
			errs = append(errs, ValidationError{
				File:   "",
				Reason: fmt.Sprintf("archive exceeds maximum extracted size of %d bytes", MaxExtractedTotalBytes),
			})
			return errs
		}

		// Check 4: extension allowlist
		ext := strings.ToLower(filepath.Ext(name))
		if !AllowedExtensions[ext] {
			errs = append(errs, ValidationError{
				File:   name,
				Reason: fmt.Sprintf("file extension %q is not allowed", ext),
			})
			continue
		}

		// Check 5: no active content or external URLs in text-based stub files.
		if isTextStubFile(ext) {
			rc, openErr := entry.Open()
			if openErr != nil {
				errs = append(errs, ValidationError{
					File:   name,
					Reason: fmt.Sprintf("failed to read file: %s", openErr),
				})
				continue
			}
			content, readErr := io.ReadAll(rc)
			rc.Close()
			if readErr != nil {
				errs = append(errs, ValidationError{
					File:   name,
					Reason: fmt.Sprintf("failed to read file content: %s", readErr),
				})
				continue
			}
			s := string(content)
			if containsExternalURL(ext, s) {
				errs = append(errs, ValidationError{
					File:   name,
					Reason: "external URLs are not allowed",
				})
			}
			if containsActiveContent(ext, s) {
				errs = append(errs, ValidationError{
					File:   name,
					Reason: "active content is not allowed",
				})
			}
		}
	}

	if !hasRootHTML {
		errs = append(errs, ValidationError{
			File:   "",
			Reason: "archive must contain at least one .html or .htm file at the root",
		})
	}

	return errs
}

func isTextStubFile(ext string) bool {
	switch ext {
	case ".html", ".htm", ".css", ".svg":
		return true
	default:
		return false
	}
}

func containsExternalURL(ext, content string) bool {
	lower := strings.ToLower(content)
	if ext == ".svg" {
		return svgExternalReferencePattern.MatchString(content)
	}
	return strings.Contains(lower, "http://") || strings.Contains(lower, "https://") || protocolRelativeURLPattern.MatchString(lower)
}

func containsActiveContent(ext, content string) bool {
	lower := strings.ToLower(content)
	if strings.Contains(lower, "<script") ||
		strings.Contains(lower, "javascript:") ||
		eventHandlerAttrPattern.MatchString(content) {
		return true
	}
	if ext == ".css" {
		return strings.Contains(lower, "@import") || strings.Contains(lower, "expression(")
	}
	if ext == ".svg" {
		return strings.Contains(lower, "<foreignobject") ||
			strings.Contains(lower, "<animate") ||
			svgSetElementPattern.MatchString(lower)
	}
	return false
}
