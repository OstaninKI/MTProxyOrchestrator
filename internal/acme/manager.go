package acme

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/go-acme/lego/v4/certcrypto"
	"github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/challenge/http01"
	"github.com/go-acme/lego/v4/lego"
	"github.com/go-acme/lego/v4/providers/http/webroot"
	"github.com/go-acme/lego/v4/registration"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/db"
)

// CertMode describes whether ACME or self-signed certs are in use.
type CertMode int

const (
	CertModeNone       CertMode = iota
	CertModeACME                // Let's Encrypt
	CertModeSelfSigned          // self-signed
)

// ChallengeMode selects which HTTP-01 provider lego uses.
type ChallengeMode int

const (
	// ChallengeStandalone starts a temporary HTTP server on port 80.
	// Use during install when nginx is not yet running.
	ChallengeStandalone ChallengeMode = iota
	// ChallengeWebroot writes challenge tokens to WebRootDir served by nginx.
	// Use for renewals when nginx already holds port 80.
	ChallengeWebroot
)

// CertInfo describes the current certificate state.
type CertInfo struct {
	Mode      CertMode
	Domain    string
	ExpiresAt time.Time
	IssuedAt  time.Time
	IsValid   bool // true if not expired and chain is trusted
}

// DNSChecker lets tests inject a fake DNS lookup.
type DNSChecker func(domain string) (addrs []string, err error)

// Manager handles certificate lifecycle.
type Manager struct {
	DB              *db.DB
	CertDir         string     // where to store certs, e.g. /etc/tgproxy/certs
	AccountKeyPath  string     // persisted ACME account key, e.g. /etc/tgproxy/certs/account.key
	CADirURL        string     // ACME directory URL; empty = Let's Encrypt production
	ServerIP        string     // host's public IP for A record check
	DNSCheck        DNSChecker // defaults to net.LookupHost
	Now             func() time.Time
	RenewBeforeDays int // days before expiry to trigger renewal; 0 = default (30)
}

// DefaultManager returns a Manager with real DNS and real time.
func DefaultManager(database *db.DB, certDir, serverIP string) Manager {
	return Manager{
		DB:             database,
		CertDir:        certDir,
		AccountKeyPath: filepath.Join(certDir, "account.key"),
		ServerIP:       serverIP,
		DNSCheck:       net.LookupHost,
		Now:            time.Now,
	}
}

// NeedsRenewal returns true when the cert expires within the configured days (default 30).
func (m Manager) NeedsRenewal(info CertInfo) bool {
	days := m.RenewBeforeDays
	if days <= 0 {
		days = 30
	}
	return info.ExpiresAt.Before(m.Now().Add(time.Duration(days) * 24 * time.Hour))
}

// CheckDNS verifies the domain's A record resolves to m.ServerIP.
// Returns an error if the record is missing or points elsewhere.
func (m Manager) CheckDNS(domain string) error {
	checker := m.DNSCheck
	if checker == nil {
		checker = net.LookupHost
	}
	addrs, err := checker(domain)
	if err != nil {
		return fmt.Errorf("dns lookup %s: %w", domain, err)
	}
	for _, addr := range addrs {
		if addr == m.ServerIP {
			return nil
		}
	}
	return fmt.Errorf("domain %s does not point to %s (got %v)", domain, m.ServerIP, addrs)
}

// LoadOrCreateAccountKey loads the ACME account key from AccountKeyPath.
// If the file does not exist, a new ECDSA P-256 key is generated and saved.
// The file is written with mode 0600.
func (m Manager) LoadOrCreateAccountKey() (*ecdsa.PrivateKey, error) {
	if m.AccountKeyPath == "" {
		return ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	}
	data, err := os.ReadFile(m.AccountKeyPath)
	if err == nil {
		block, _ := pem.Decode(data)
		if block != nil {
			key, err := x509.ParseECPrivateKey(block.Bytes)
			if err == nil {
				return key, nil
			}
		}
	}
	// File missing or unparseable — generate a new key and persist it.
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate account key: %w", err)
	}
	if saveErr := m.saveAccountKey(key); saveErr != nil {
		return nil, saveErr
	}
	return key, nil
}

func (m Manager) saveAccountKey(key *ecdsa.PrivateKey) error {
	if m.AccountKeyPath == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(m.AccountKeyPath), 0o700); err != nil {
		return fmt.Errorf("create account key dir: %w", err)
	}
	keyBytes, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return fmt.Errorf("marshal account key: %w", err)
	}
	f, err := os.OpenFile(m.AccountKeyPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open account key file: %w", err)
	}
	defer f.Close()
	if err := pem.Encode(f, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes}); err != nil {
		return fmt.Errorf("encode account key PEM: %w", err)
	}
	return nil
}

// acmeUser implements registration.User for lego.
type acmeUser struct {
	email        string
	registration *registration.Resource
	key          *ecdsa.PrivateKey
}

func (u *acmeUser) GetEmail() string                        { return u.email }
func (u *acmeUser) GetRegistration() *registration.Resource { return u.registration }
func (u *acmeUser) GetPrivateKey() crypto.PrivateKey        { return u.key }

// ObtainACME uses lego to obtain a Let's Encrypt certificate for domain.
// mode selects the HTTP-01 challenge provider; webrootDir is only used for ChallengeWebroot.
// Stores cert files under m.CertDir/<domain>/ with mode 0600.
// Records the attempt in cert_renewals regardless of success.
// Returns the cert path and key path.
func (m Manager) ObtainACME(ctx context.Context, domain, email string, mode ChallengeMode, webrootDir string) (certPath, keyPath string, err error) {
	recordErr := func(e error) {
		if m.DB == nil {
			return
		}
		errMsg := ""
		success := 1
		if e != nil {
			errMsg = e.Error()
			success = 0
		}
		_, _ = m.DB.ExecContext(ctx,
			`INSERT INTO cert_renewals(domain, success, error_msg) VALUES(?,?,?)`,
			domain, success, errMsg,
		)
	}

	accountKey, err := m.LoadOrCreateAccountKey()
	if err != nil {
		recordErr(err)
		return "", "", err
	}

	user := &acmeUser{email: email, key: accountKey}
	cfg := lego.NewConfig(user)
	cfg.Certificate.KeyType = certcrypto.RSA2048
	if m.CADirURL != "" {
		cfg.CADirURL = m.CADirURL
	}

	client, err := lego.NewClient(cfg)
	if err != nil {
		recordErr(err)
		return "", "", fmt.Errorf("create lego client: %w", err)
	}

	switch mode {
	case ChallengeStandalone:
		// Binds port 80 directly; no external web server required.
		if err := client.Challenge.SetHTTP01Provider(http01.NewProviderServer("", "80")); err != nil {
			recordErr(err)
			return "", "", fmt.Errorf("set standalone http-01 provider: %w", err)
		}
	case ChallengeWebroot:
		if err := os.MkdirAll(webrootDir, 0o700); err != nil {
			recordErr(err)
			return "", "", fmt.Errorf("create webroot dir: %w", err)
		}
		provider, err := webroot.NewHTTPProvider(webrootDir)
		if err != nil {
			recordErr(err)
			return "", "", fmt.Errorf("create webroot provider: %w", err)
		}
		if err := client.Challenge.SetHTTP01Provider(provider); err != nil {
			recordErr(err)
			return "", "", fmt.Errorf("set webroot http-01 provider: %w", err)
		}
	default:
		err := fmt.Errorf("unknown challenge mode: %d", mode)
		recordErr(err)
		return "", "", err
	}

	reg, err := client.Registration.Register(registration.RegisterOptions{TermsOfServiceAgreed: true})
	if err != nil {
		recordErr(err)
		return "", "", fmt.Errorf("register account: %w", err)
	}
	user.registration = reg

	certs, err := client.Certificate.Obtain(certificate.ObtainRequest{
		Domains: []string{domain},
		Bundle:  true,
	})
	if err != nil {
		recordErr(err)
		return "", "", fmt.Errorf("obtain certificate: %w", err)
	}

	domainDir := filepath.Join(m.CertDir, domain)
	if err := os.MkdirAll(domainDir, 0o700); err != nil {
		recordErr(err)
		return "", "", fmt.Errorf("create cert dir: %w", err)
	}

	certPath = filepath.Join(domainDir, "cert.pem")
	keyPath = filepath.Join(domainDir, "key.pem")

	if err := os.WriteFile(certPath, certs.Certificate, 0o600); err != nil {
		recordErr(err)
		return "", "", fmt.Errorf("write cert: %w", err)
	}
	if err := os.WriteFile(keyPath, certs.PrivateKey, 0o600); err != nil {
		recordErr(err)
		return "", "", fmt.Errorf("write key: %w", err)
	}

	recordErr(nil)
	return certPath, keyPath, nil
}

// GenerateSelfSigned generates a self-signed certificate for the given
// common name (IP or domain). Stores files in m.CertDir/self-signed/.
// Valid for 10 years.
func (m Manager) GenerateSelfSigned(commonName string) (certPath, keyPath string, err error) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("generate key: %w", err)
	}

	now := m.Now()
	if now.IsZero() {
		now = time.Now()
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return "", "", fmt.Errorf("generate serial: %w", err)
	}

	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName: commonName,
		},
		NotBefore:             now,
		NotAfter:              now.Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	// Add SAN for IP or DNS.
	if ip := net.ParseIP(commonName); ip != nil {
		template.IPAddresses = []net.IP{ip}
	} else {
		template.DNSNames = []string{commonName}
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return "", "", fmt.Errorf("create certificate: %w", err)
	}

	selfSignedDir := filepath.Join(m.CertDir, "self-signed")
	if err := os.MkdirAll(selfSignedDir, 0o700); err != nil {
		return "", "", fmt.Errorf("create self-signed dir: %w", err)
	}

	certPath = filepath.Join(selfSignedDir, "cert.pem")
	keyPath = filepath.Join(selfSignedDir, "key.pem")

	certFile, err := os.OpenFile(certPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", "", fmt.Errorf("open cert file: %w", err)
	}
	defer certFile.Close()
	if err := pem.Encode(certFile, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
		return "", "", fmt.Errorf("encode cert PEM: %w", err)
	}

	keyBytes, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		return "", "", fmt.Errorf("marshal key: %w", err)
	}

	keyFile, err := os.OpenFile(keyPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", "", fmt.Errorf("open key file: %w", err)
	}
	defer keyFile.Close()
	if err := pem.Encode(keyFile, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes}); err != nil {
		return "", "", fmt.Errorf("encode key PEM: %w", err)
	}

	return certPath, keyPath, nil
}

// ReadCertInfo reads and parses the certificate at certPath.
func (m Manager) ReadCertInfo(certPath string, mode CertMode) (CertInfo, error) {
	data, err := os.ReadFile(certPath)
	if err != nil {
		return CertInfo{}, fmt.Errorf("read cert: %w", err)
	}

	block, _ := pem.Decode(data)
	if block == nil {
		return CertInfo{}, fmt.Errorf("no PEM block in %s", certPath)
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return CertInfo{}, fmt.Errorf("parse cert: %w", err)
	}

	now := m.Now()
	if now.IsZero() {
		now = time.Now()
	}

	isValid := now.Before(cert.NotAfter) && now.After(cert.NotBefore)

	domain := cert.Subject.CommonName
	if len(cert.DNSNames) > 0 {
		domain = cert.DNSNames[0]
	}

	return CertInfo{
		Mode:      mode,
		Domain:    domain,
		ExpiresAt: cert.NotAfter,
		IssuedAt:  cert.NotBefore,
		IsValid:   isValid,
	}, nil
}
