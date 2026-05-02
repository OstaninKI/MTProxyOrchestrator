package panel

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/acme"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/audit"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/stub"
)

// maxUploadBytes is the request body size limit for stub ZIP uploads (5 MB).
const maxUploadBytes = stub.MaxZipSize

// BuiltinStubTemplate describes a built-in stub template available for selection.
type BuiltinStubTemplate struct {
	Name        string
	Description string
	Dir         string // path to the template directory on disk
}

// SettingsConfig holds panel-level settings for stub and certificate management.
// Populated from installation config; injected via Server.SettingsCfg.
type SettingsConfig struct {
	StubTemplatesDir string // directory containing built-in stub templates
	WebRoot          string // nginx web root for stub pages
	CertDir          string // directory where certs are stored
	ServerIP         string // public IP of the server
	Domain           string // configured domain (empty for IP installs)
}

// settingsConfig returns SettingsConfig from Server.SettingsCfg if set, or zero values.
func (s *Server) settingsConfig() SettingsConfig {
	if s.SettingsCfg != nil {
		return *s.SettingsCfg
	}
	return SettingsConfig{}
}

// builtinTemplates returns the list of built-in stub templates, loading names
// from the StubTemplatesDir. Returns static descriptions for known template names.
func builtinTemplates(templatesDir string) []BuiltinStubTemplate {
	knownDescriptions := map[string]string{
		"coming-soon":       "Simple \"coming soon\" page with countdown.",
		"corporate-landing": "Clean corporate landing page.",
		"dev-portfolio":     "Developer portfolio page.",
		"maintenance":       "Site maintenance page.",
		"personal-blog":     "Minimal personal blog front page.",
	}

	entries, err := os.ReadDir(templatesDir)
	if err != nil {
		return nil
	}

	var out []BuiltinStubTemplate
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		desc := knownDescriptions[name]
		if desc == "" {
			desc = name
		}
		out = append(out, BuiltinStubTemplate{
			Name:        name,
			Description: desc,
			Dir:         filepath.Join(templatesDir, name),
		})
	}
	return out
}

// --- zip extractor hook (injectable for tests) ---

// extractZip extracts all files from a ZIP reader into destDir.
// It is a var so tests can replace it without writing real ZIPs.
var extractZip = func(r io.ReaderAt, size int64, destDir string) error {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	if len(zr.File) > stub.MaxZipEntries {
		return fmt.Errorf("archive contains too many files (max %d)", stub.MaxZipEntries)
	}
	var totalExtracted uint64
	for _, f := range zr.File {
		// Sanitise path: reject absolute paths and traversal sequences.
		rel := filepath.Clean(f.Name)
		if filepath.IsAbs(rel) || strings.HasPrefix(rel, "..") {
			return fmt.Errorf("zip entry with unsafe path: %s", f.Name)
		}
		dest := filepath.Join(destDir, rel)
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(dest, 0o700); err != nil {
				return err
			}
			continue
		}
		if f.UncompressedSize64 > stub.MaxExtractedFileBytes {
			return fmt.Errorf("zip entry %s exceeds maximum extracted size of %d bytes", f.Name, stub.MaxExtractedFileBytes)
		}
		totalExtracted += f.UncompressedSize64
		if totalExtracted > stub.MaxExtractedTotalBytes {
			return fmt.Errorf("zip archive exceeds maximum extracted size of %d bytes", stub.MaxExtractedTotalBytes)
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		dst, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			rc.Close()
			return err
		}
		written, err := io.Copy(dst, io.LimitReader(rc, int64(stub.MaxExtractedFileBytes)+1))
		rc.Close()
		closeErr := dst.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		if written > int64(stub.MaxExtractedFileBytes) {
			return fmt.Errorf("zip entry %s exceeds maximum extracted size of %d bytes", f.Name, stub.MaxExtractedFileBytes)
		}
	}
	return nil
}

// --- stub applier hook (injectable for tests) ---

// applyStubTemplate applies a stub template directory using a default Applier.
// In tests this can be replaced with a stub.
var applyStubTemplate = func(webRoot, srcDir string) error {
	applier := stub.DefaultApplier(webRoot, func() error {
		return nil // nginx reload is best-effort in panel context
	})
	return applier.Apply(srcDir)
}

// --- handlers ---

// handleSettingsStubList renders the stub templates list page.
func (s *Server) handleSettingsStubList(w http.ResponseWriter, r *http.Request) {
	cfg := s.settingsConfig()
	templates := builtinTemplates(cfg.StubTemplatesDir)

	tok, err := NewCSRFToken()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	SetCSRFCookie(w, tok, s.Secure)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	settingsStubListPage(w, settingsStubListData{
		CSRFField: CSRFField(),
		CSRFToken: tok,
		Templates: templates,
	})
}

// handleSettingsStubApply applies a selected built-in stub template.
// Form fields: template (required) — name of the built-in template.
func (s *Server) handleSettingsStubApply(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}

	cfg := s.settingsConfig()
	name := strings.TrimSpace(r.FormValue("template"))
	if name == "" {
		http.Error(w, "template name is required", http.StatusBadRequest)
		return
	}

	// Reject path traversal attempts.
	if strings.Contains(name, "/") || strings.Contains(name, "..") || strings.Contains(name, string(os.PathSeparator)) {
		http.Error(w, "invalid template name", http.StatusBadRequest)
		return
	}

	templates := builtinTemplates(cfg.StubTemplatesDir)
	var srcDir string
	for _, t := range templates {
		if t.Name == name {
			srcDir = t.Dir
			break
		}
	}
	if srcDir == "" {
		http.Error(w, "template not found", http.StatusNotFound)
		return
	}

	if err := applyStubTemplate(cfg.WebRoot, srcDir); err != nil {
		tok, _ := NewCSRFToken()
		SetCSRFCookie(w, tok, s.Secure)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		settingsStubListPage(w, settingsStubListData{
			CSRFField:  CSRFField(),
			CSRFToken:  tok,
			Templates:  templates,
			ApplyError: fmt.Sprintf("apply failed (rolled back): %v", err),
		})
		return
	}

	audit.Log(s.DB, s.sessionAdminID(r), "stub.apply", name, "", clientIP(r)) //nolint:errcheck
	tok, _ := NewCSRFToken()
	SetCSRFCookie(w, tok, s.Secure)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	settingsStubListPage(w, settingsStubListData{
		CSRFField:    CSRFField(),
		CSRFToken:    tok,
		Templates:    templates,
		ApplySuccess: name,
	})
}

// handleSettingsStubUpload accepts a custom ZIP upload, validates it, and applies it.
// Multipart field: stub_zip (required).
func (s *Server) handleSettingsStubUpload(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}

	cfg := s.settingsConfig()

	// Limit overall request size to prevent large uploads before parsing multipart.
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes+4096)

	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		tok, _ := NewCSRFToken()
		SetCSRFCookie(w, tok, s.Secure)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		settingsStubListPage(w, settingsStubListData{
			CSRFField:   CSRFField(),
			CSRFToken:   tok,
			Templates:   builtinTemplates(cfg.StubTemplatesDir),
			UploadError: "upload too large or invalid (max 5 MB)",
		})
		return
	}

	file, _, err := r.FormFile("stub_zip")
	if err != nil {
		tok, _ := NewCSRFToken()
		SetCSRFCookie(w, tok, s.Secure)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		settingsStubListPage(w, settingsStubListData{
			CSRFField:   CSRFField(),
			CSRFToken:   tok,
			Templates:   builtinTemplates(cfg.StubTemplatesDir),
			UploadError: "no file uploaded",
		})
		return
	}
	defer file.Close()

	// Read file into buffer to allow ReaderAt access required by stub.Validate.
	buf := &bytes.Buffer{}
	n, err := io.Copy(buf, io.LimitReader(file, maxUploadBytes+1))
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if n > maxUploadBytes {
		tok, _ := NewCSRFToken()
		SetCSRFCookie(w, tok, s.Secure)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		settingsStubListPage(w, settingsStubListData{
			CSRFField:   CSRFField(),
			CSRFToken:   tok,
			Templates:   builtinTemplates(cfg.StubTemplatesDir),
			UploadError: fmt.Sprintf("file too large (max %d bytes)", maxUploadBytes),
		})
		return
	}

	data := buf.Bytes()
	reader := bytes.NewReader(data)
	validationErrs := stub.Validate(reader, int64(len(data)))
	if len(validationErrs) > 0 {
		var msgs []string
		for _, ve := range validationErrs {
			// Use the structured error message (file:reason) — never echo raw HTML content.
			msgs = append(msgs, ve.Error())
		}
		tok, _ := NewCSRFToken()
		SetCSRFCookie(w, tok, s.Secure)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		settingsStubListPage(w, settingsStubListData{
			CSRFField:        CSRFField(),
			CSRFToken:        tok,
			Templates:        builtinTemplates(cfg.StubTemplatesDir),
			UploadValidation: msgs,
		})
		return
	}

	// Extract ZIP to a temp directory and apply.
	tmpDir, err := os.MkdirTemp("", "tgproxy-stub-upload-*")
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer os.RemoveAll(tmpDir)

	if err := extractZip(reader, int64(len(data)), tmpDir); err != nil {
		http.Error(w, "internal error: extract failed", http.StatusInternalServerError)
		return
	}

	if err := applyStubTemplate(cfg.WebRoot, tmpDir); err != nil {
		tok, _ := NewCSRFToken()
		SetCSRFCookie(w, tok, s.Secure)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		settingsStubListPage(w, settingsStubListData{
			CSRFField:   CSRFField(),
			CSRFToken:   tok,
			Templates:   builtinTemplates(cfg.StubTemplatesDir),
			UploadError: fmt.Sprintf("apply failed (rolled back): %v", err),
		})
		return
	}

	audit.Log(s.DB, s.sessionAdminID(r), "stub.upload", "custom", "", clientIP(r)) //nolint:errcheck
	tok, _ := NewCSRFToken()
	SetCSRFCookie(w, tok, s.Secure)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	settingsStubListPage(w, settingsStubListData{
		CSRFField:    CSRFField(),
		CSRFToken:    tok,
		Templates:    builtinTemplates(cfg.StubTemplatesDir),
		ApplySuccess: "custom upload",
	})
}

// handleSettingsCertificates renders the certificate state page.
func (s *Server) handleSettingsCertificates(w http.ResponseWriter, r *http.Request) {
	cfg := s.settingsConfig()

	data := settingsCertData{
		HasDomain: cfg.Domain != "",
		Domain:    cfg.Domain,
		ServerIP:  cfg.ServerIP,
	}

	// Load cert info if we have a cert directory.
	if cfg.CertDir != "" {
		mgr := acme.Manager{
			DB:      s.DB,
			CertDir: cfg.CertDir,
			Now:     time.Now,
		}

		// Try ACME cert first (domain-based), then self-signed.
		if cfg.Domain != "" {
			certPath := filepath.Join(cfg.CertDir, cfg.Domain, "cert.pem")
			if info, err := mgr.ReadCertInfo(certPath, acme.CertModeACME); err == nil {
				data.CertMode = "ACME (Let's Encrypt)"
				data.ExpiresAt = info.ExpiresAt
				data.IssuedAt = info.IssuedAt
				data.IsValid = info.IsValid
				data.NeedsRenewal = mgr.NeedsRenewal(info)
			}
		}

		// Fall back to self-signed if ACME cert not loaded.
		if data.CertMode == "" {
			certPath := filepath.Join(cfg.CertDir, "self-signed", "cert.pem")
			if info, err := mgr.ReadCertInfo(certPath, acme.CertModeSelfSigned); err == nil {
				data.CertMode = "Self-signed"
				data.ExpiresAt = info.ExpiresAt
				data.IssuedAt = info.IssuedAt
				data.IsValid = info.IsValid
				data.NeedsRenewal = mgr.NeedsRenewal(info)
			}
		}

		if data.CertMode == "" {
			data.CertMode = "none"
		}

		// Load recent renewal attempts.
		data.Renewals = loadRecentRenewals(s, cfg.Domain)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	settingsCertPage(w, data)
}

// RenewalAttempt is a single row from cert_renewals.
type RenewalAttempt struct {
	Domain    string
	Success   bool
	ErrorMsg  string
	CreatedAt string
}

// loadRecentRenewals returns the last 10 renewal attempts for the given domain.
// Returns nil on any error.
func loadRecentRenewals(s *Server, domain string) []RenewalAttempt {
	if s.DB == nil {
		return nil
	}
	rows, err := s.DB.Query(
		`SELECT domain, success, error_msg, created_at FROM cert_renewals
		 WHERE domain=? ORDER BY created_at DESC LIMIT 10`,
		domain,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var out []RenewalAttempt
	for rows.Next() {
		var ra RenewalAttempt
		var success int
		if err := rows.Scan(&ra.Domain, &success, &ra.ErrorMsg, &ra.CreatedAt); err != nil {
			continue
		}
		ra.Success = success == 1
		out = append(out, ra)
	}
	return out
}
