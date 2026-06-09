package panel

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/acme"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/audit"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/stub"
)

const nginxReloadTimeout = 10 * time.Second

// reloadNginx executes "systemctl reload nginx" with a bounded timeout.
// Overridable in tests.
var reloadNginx = func() error {
	ctx, cancel := context.WithTimeout(context.Background(), nginxReloadTimeout)
	defer cancel()
	return exec.CommandContext(ctx, "systemctl", "reload", "nginx").Run()
}

// maxUploadBytes is the request body size limit for stub ZIP uploads (5 MB).
const maxUploadBytes = stub.MaxZipSize

// certRenewDays returns the configured certificate renewal threshold in days (default 30).
func (s *Server) certRenewDays() int {
	v := s.DB.GetSetting(settingCertRenewDays, "30")
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 || n > 89 {
		return 30
	}
	return n
}

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
	ACMEEmail        string // email used for Let's Encrypt; empty if ACME not configured
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
	applier := stub.DefaultApplier(webRoot, reloadNginx)
	return applier.Apply(srcDir)
}

func validateExtractedStubTemplate(srcDir string) []stub.ValidationError {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	walkErr := filepath.WalkDir(srcDir, func(filePath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcDir, filePath)
		if err != nil {
			return err
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(rel)
		header.Method = zip.Store
		writer, err := zw.CreateHeader(header)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		src, err := os.Open(filePath)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(writer, src)
		closeErr := src.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	closeErr := zw.Close()
	if walkErr != nil {
		return []stub.ValidationError{{Reason: fmt.Sprintf("failed to inspect template: %v", walkErr)}}
	}
	if closeErr != nil {
		return []stub.ValidationError{{Reason: fmt.Sprintf("failed to inspect template: %v", closeErr)}}
	}
	return stub.Validate(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
}

func formatValidationErrors(errs []stub.ValidationError) string {
	msgs := make([]string, 0, len(errs))
	for _, ve := range errs {
		msgs = append(msgs, ve.Error())
	}
	return strings.Join(msgs, "; ")
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
	SetCSRFCookie(w, tok, s.Secure, s.PanelPath)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	settingsStubListPage(w, settingsStubListData{
		CSRFField: CSRFField(),
		CSRFToken: tok,
		Templates: templates,
		Domain:    cfg.Domain,
		HasDomain: cfg.Domain != "",
		PanelPath: s.PanelPath,
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
		SetCSRFCookie(w, tok, s.Secure, s.PanelPath)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		settingsStubListPage(w, settingsStubListData{
			CSRFField:  CSRFField(),
			CSRFToken:  tok,
			Templates:  templates,
			ApplyError: fmt.Sprintf("apply failed (rolled back): %v", err),
			PanelPath:  s.PanelPath,
		})
		return
	}

	audit.Log(s.DB, s.sessionAdminID(r), "stub.apply", name, "", clientIP(r)) //nolint:errcheck
	tok, _ := NewCSRFToken()
	SetCSRFCookie(w, tok, s.Secure, s.PanelPath)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	settingsStubListPage(w, settingsStubListData{
		CSRFField:    CSRFField(),
		CSRFToken:    tok,
		Templates:    templates,
		ApplySuccess: name,
		PanelPath:    s.PanelPath,
	})
}

// handleSettingsStubUpload accepts a custom ZIP upload, validates it, and applies it.
// Multipart field: stub_zip (required).
func (s *Server) handleSettingsStubUpload(w http.ResponseWriter, r *http.Request) {
	// Limit overall request size before any FormValue/ParseMultipartForm call,
	// including CSRF validation, because those helpers can parse multipart bodies.
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes+4096)

	if !ValidateCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}

	cfg := s.settingsConfig()

	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		tok, _ := NewCSRFToken()
		SetCSRFCookie(w, tok, s.Secure, s.PanelPath)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		settingsStubListPage(w, settingsStubListData{
			CSRFField:   CSRFField(),
			CSRFToken:   tok,
			Templates:   builtinTemplates(cfg.StubTemplatesDir),
			UploadError: "upload too large or invalid (max 5 MB)",
			PanelPath:   s.PanelPath,
		})
		return
	}

	file, _, err := r.FormFile("stub_zip")
	if err != nil {
		tok, _ := NewCSRFToken()
		SetCSRFCookie(w, tok, s.Secure, s.PanelPath)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		settingsStubListPage(w, settingsStubListData{
			CSRFField:   CSRFField(),
			CSRFToken:   tok,
			Templates:   builtinTemplates(cfg.StubTemplatesDir),
			UploadError: "no file uploaded",
			PanelPath:   s.PanelPath,
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
		SetCSRFCookie(w, tok, s.Secure, s.PanelPath)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		settingsStubListPage(w, settingsStubListData{
			CSRFField:   CSRFField(),
			CSRFToken:   tok,
			Templates:   builtinTemplates(cfg.StubTemplatesDir),
			UploadError: fmt.Sprintf("file too large (max %d bytes)", maxUploadBytes),
			PanelPath:   s.PanelPath,
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
		SetCSRFCookie(w, tok, s.Secure, s.PanelPath)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		settingsStubListPage(w, settingsStubListData{
			CSRFField:        CSRFField(),
			CSRFToken:        tok,
			Templates:        builtinTemplates(cfg.StubTemplatesDir),
			UploadValidation: msgs,
			PanelPath:        s.PanelPath,
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
		SetCSRFCookie(w, tok, s.Secure, s.PanelPath)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		settingsStubListPage(w, settingsStubListData{
			CSRFField:   CSRFField(),
			CSRFToken:   tok,
			Templates:   builtinTemplates(cfg.StubTemplatesDir),
			UploadError: fmt.Sprintf("apply failed (rolled back): %v", err),
			PanelPath:   s.PanelPath,
		})
		return
	}

	audit.Log(s.DB, s.sessionAdminID(r), "stub.upload", "custom", "", clientIP(r)) //nolint:errcheck
	tok, _ := NewCSRFToken()
	SetCSRFCookie(w, tok, s.Secure, s.PanelPath)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	settingsStubListPage(w, settingsStubListData{
		CSRFField:    CSRFField(),
		CSRFToken:    tok,
		Templates:    builtinTemplates(cfg.StubTemplatesDir),
		ApplySuccess: "custom upload",
		PanelPath:    s.PanelPath,
	})
}

// handleSettingsStubDownload downloads the current stub web root as a ZIP.
func (s *Server) handleSettingsStubDownload(w http.ResponseWriter, r *http.Request) {
	cfg := s.settingsConfig()
	webRoot := cfg.WebRoot

	if webRoot == "" {
		http.Error(w, "no stub web root configured", http.StatusNotFound)
		return
	}

	info, err := os.Stat(webRoot)
	if err != nil || !info.IsDir() {
		http.Error(w, "stub web root not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="stub.zip"`)

	zw := zip.NewWriter(w)
	defer zw.Close()

	err = filepath.Walk(webRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip files with errors
		}

		if info.IsDir() {
			return nil // skip directories
		}

		rel, err := filepath.Rel(webRoot, path)
		if err != nil {
			return nil
		}

		f, err := zw.Create(filepath.ToSlash(rel))
		if err != nil {
			return nil
		}

		src, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer src.Close()

		_, err = io.Copy(f, src)
		return nil
	})

	if err == nil {
		audit.Log(s.DB, s.sessionAdminID(r), "settings.stub_download", "", "", clientIP(r)) //nolint:errcheck
	}
}

// handleSettingsCertificates renders the certificate state page.
func (s *Server) handleSettingsCertificates(w http.ResponseWriter, r *http.Request) {
	cfg := s.settingsConfig()
	tok, err := NewCSRFToken()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	SetCSRFCookie(w, tok, s.Secure, s.PanelPath)

	data := settingsCertData{
		CSRFField: CSRFField(),
		CSRFToken: tok,
		HasDomain: cfg.Domain != "",
		Domain:    cfg.Domain,
		ServerIP:  cfg.ServerIP,
		PanelPath: s.PanelPath,
	}

	// Load cert info if we have a cert directory.
	if cfg.CertDir != "" {
		mgr := acme.Manager{
			DB:      s.DB,
			CertDir: cfg.CertDir,
			Now:     time.Now,
		}
		mgr.RenewBeforeDays = s.certRenewDays()

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

	data.RenewDays = s.certRenewDays()
	data.Notice = r.URL.Query().Get("notice")

	setStrictPanelCSP(w)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
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

// handleSettingsCertRenewalConfig updates the renewal threshold, ACME provider,
// and auto-renew toggle in one form submission.
func (s *Server) handleSettingsCertRenewalConfig(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}
	redirect := func(msg string) {
		http.Redirect(w, r, s.PanelPath+"settings/certificates?notice="+url.QueryEscape(msg), http.StatusSeeOther)
	}

	n, err := strconv.Atoi(strings.TrimSpace(r.FormValue("renew_days")))
	if err != nil || n < 1 || n > 89 {
		redirect("Threshold must be 1–89 days.")
		return
	}
	provider := strings.TrimSpace(r.FormValue("acme_provider"))
	if provider != "production" && provider != "staging" {
		redirect("Unknown ACME provider.")
		return
	}
	autoRenew := "0"
	if r.FormValue("auto_renew") != "" {
		autoRenew = "1"
	}

	if err := s.DB.SetSetting(settingCertRenewDays, strconv.Itoa(n)); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := s.DB.SetSetting(settingCertACMEProvider, provider); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := s.DB.SetSetting(settingCertAutoRenew, autoRenew); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	audit.Log(s.DB, s.sessionAdminID(r), "settings.cert_renewal_config", "", provider+"/"+autoRenew+"/"+strconv.Itoa(n), clientIP(r)) //nolint:errcheck
	redirect("Renewal settings saved.")
}

// maxCertUploadBytes caps an uploaded PEM file (cert or key) at 256 KB.
const maxCertUploadBytes = 256 * 1024

// handleSettingsCertUpload installs an admin-supplied cert/key pair, overwriting
// the files nginx serves for the configured domain. Manual upload is only
// available for domain installs; it disables auto-renew so ACME does not later
// overwrite the manual certificate.
func (s *Server) handleSettingsCertUpload(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}
	cfg := s.settingsConfig()
	redirect := func(msg string) {
		http.Redirect(w, r, s.PanelPath+"settings/certificates?notice="+url.QueryEscape(msg), http.StatusSeeOther)
	}
	if cfg.Domain == "" {
		redirect("Manual upload requires a domain install.")
		return
	}
	if err := r.ParseMultipartForm(2 * maxCertUploadBytes); err != nil {
		redirect("Could not read upload.")
		return
	}
	certPEM, err := readUploadField(r, "cert")
	if err != nil {
		redirect("Certificate file missing or too large.")
		return
	}
	keyPEM, err := readUploadField(r, "key")
	if err != nil {
		redirect("Key file missing or too large.")
		return
	}
	if _, err := acme.ValidateManualCert(certPEM, keyPEM, cfg.Domain, time.Now()); err != nil {
		redirect("Invalid certificate: " + err.Error())
		return
	}

	domainDir := filepath.Join(cfg.CertDir, cfg.Domain)
	if err := os.MkdirAll(domainDir, 0o700); err != nil {
		redirect("Could not write certificate.")
		return
	}
	if err := os.WriteFile(filepath.Join(domainDir, "cert.pem"), certPEM, 0o600); err != nil {
		redirect("Could not write certificate.")
		return
	}
	if err := os.WriteFile(filepath.Join(domainDir, "key.pem"), keyPEM, 0o600); err != nil {
		redirect("Could not write key.")
		return
	}
	_ = s.DB.SetSetting(settingCertManual, "1")
	_ = s.DB.SetSetting(settingCertAutoRenew, "0")
	audit.Log(s.DB, s.sessionAdminID(r), "settings.cert_manual_upload", "", cfg.Domain, clientIP(r)) //nolint:errcheck

	if err := reloadNginx(); err != nil {
		redirect("Certificate installed but nginx reload failed: " + err.Error())
		return
	}
	redirect("Manual certificate installed. Auto-renew disabled.")
}

// readUploadField reads a single multipart file field, rejecting empty or
// oversized content.
func readUploadField(r *http.Request, field string) ([]byte, error) {
	f, _, err := r.FormFile(field)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxCertUploadBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || len(data) > maxCertUploadBytes {
		return nil, fmt.Errorf("field %s empty or too large", field)
	}
	return data, nil
}

// handleSettingsCertManualClear reverts a manual override: it clears the manual
// flag and re-enables auto-renew. The next renewal tick (or a manual "Renew
// now") obtains a fresh ACME certificate.
func (s *Server) handleSettingsCertManualClear(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}
	_ = s.DB.SetSetting(settingCertManual, "0")
	_ = s.DB.SetSetting(settingCertAutoRenew, "1")
	audit.Log(s.DB, s.sessionAdminID(r), "settings.cert_manual_clear", "", "", clientIP(r)) //nolint:errcheck
	http.Redirect(w, r, s.PanelPath+"settings/certificates?notice="+url.QueryEscape("Reverted to ACME. Auto-renew enabled."), http.StatusSeeOther)
}

// handleSettingsCertRenew forces a Let's Encrypt renewal regardless of expiry.
// Only available when ACME is configured (domain + email present).
func (s *Server) handleSettingsCertRenew(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}

	cfg := s.settingsConfig()
	if cfg.Domain == "" || cfg.ACMEEmail == "" {
		http.Error(w, "ACME not configured", http.StatusBadRequest)
		return
	}
	mgr := acme.DefaultManager(s.DB, cfg.CertDir, "")
	webRootDir := filepath.Join(cfg.CertDir, ".well-known-webroot")
	runner := acme.DefaultRunner(mgr, webRootDir)
	if err := runner.Renew(r.Context(), cfg.Domain, cfg.ACMEEmail); err != nil {
		http.Error(w, "renewal failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, s.PanelPath+"settings/certificates", http.StatusSeeOther)
}

// handleSettingsStubRemote renders the remote GitHub templates page.
func (s *Server) handleSettingsStubRemote(w http.ResponseWriter, r *http.Request) {
	templates, err := fetchRemoteTemplateList(remoteHTTPClient)
	var remoteErr string
	if err != nil {
		remoteErr = "Failed to load templates from GitHub: " + err.Error()
	}

	applied := r.URL.Query().Get("applied")

	tok, _ := NewCSRFToken()
	SetCSRFCookie(w, tok, s.Secure, s.PanelPath)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	settingsStubRemotePage(w, settingsStubRemoteData{
		CSRFField:    CSRFField(),
		CSRFToken:    tok,
		Templates:    templates,
		Error:        remoteErr,
		ApplySuccess: applied,
		PanelPath:    s.PanelPath,
	})
}

// handleSettingsStubRemoteApply downloads a template from GitHub and applies it.
func (s *Server) handleSettingsStubRemoteApply(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}

	name := strings.TrimSpace(r.FormValue("template"))
	if name == "" || strings.Contains(name, "/") || strings.Contains(name, "..") ||
		strings.Contains(name, string(os.PathSeparator)) {
		http.Error(w, "invalid template name", http.StatusBadRequest)
		return
	}

	cfg := s.settingsConfig()

	tmpDir, err := os.MkdirTemp("", "tgproxy-stub-remote-*")
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer os.RemoveAll(tmpDir)

	renderRemoteErr := func(msg string) {
		templates, _ := fetchRemoteTemplateList(remoteHTTPClient)
		tok, _ := NewCSRFToken()
		SetCSRFCookie(w, tok, s.Secure, s.PanelPath)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		settingsStubRemotePage(w, settingsStubRemoteData{
			CSRFField: CSRFField(),
			CSRFToken: tok,
			Templates: templates,
			Error:     msg,
			PanelPath: s.PanelPath,
		})
	}

	if err := downloadRemoteTemplate(remoteHTTPClient, name, tmpDir); err != nil {
		renderRemoteErr(fmt.Sprintf("Download failed: %v", err))
		return
	}
	if validationErrs := validateExtractedStubTemplate(tmpDir); len(validationErrs) > 0 {
		renderRemoteErr("Validation failed: " + formatValidationErrors(validationErrs))
		return
	}

	if err := applyStubTemplate(cfg.WebRoot, tmpDir); err != nil {
		renderRemoteErr(fmt.Sprintf("Apply failed (rolled back): %v", err))
		return
	}

	audit.Log(s.DB, s.sessionAdminID(r), "stub.remote", name, "", clientIP(r)) //nolint:errcheck
	http.Redirect(w, r, s.PanelPath+"settings/stubs/remote?applied="+name, http.StatusSeeOther)
}
