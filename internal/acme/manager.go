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
	DB       *db.DB
	CertDir  string     // where to store certs, e.g. /etc/tgproxy/certs
	ServerIP string     // host's public IP for A record check
	DNSCheck DNSChecker // defaults to net.LookupHost
	Now      func() time.Time
}

// DefaultManager returns a Manager with real DNS and real time.
func DefaultManager(database *db.DB, certDir, serverIP string) Manager {
	return Manager{
		DB:       database,
		CertDir:  certDir,
		ServerIP: serverIP,
		DNSCheck: net.LookupHost,
		Now:      time.Now,
	}
}

// NeedsRenewal returns true when the cert expires within 30 days.
func (m Manager) NeedsRenewal(info CertInfo) bool {
	return info.ExpiresAt.Before(m.Now().Add(30 * 24 * time.Hour))
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
// Stores the cert files under m.CertDir/<domain>/.
// Records the attempt in cert_renewals regardless of success.
// Returns the cert path and key path.
func (m Manager) ObtainACME(ctx context.Context, domain, email string) (certPath, keyPath string, err error) {
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

	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		recordErr(err)
		return "", "", fmt.Errorf("generate account key: %w", err)
	}

	user := &acmeUser{email: email, key: privateKey}
	config := lego.NewConfig(user)
	config.Certificate.KeyType = certcrypto.RSA2048

	client, err := lego.NewClient(config)
	if err != nil {
		recordErr(err)
		return "", "", fmt.Errorf("create lego client: %w", err)
	}

	webrootDir := filepath.Join(m.CertDir, ".well-known-webroot")
	if err := os.MkdirAll(webrootDir, 0700); err != nil {
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
		return "", "", fmt.Errorf("set http-01 provider: %w", err)
	}

	reg, err := client.Registration.Register(registration.RegisterOptions{TermsOfServiceAgreed: true})
	if err != nil {
		recordErr(err)
		return "", "", fmt.Errorf("register account: %w", err)
	}
	user.registration = reg

	request := certificate.ObtainRequest{
		Domains: []string{domain},
		Bundle:  true,
	}

	certs, err := client.Certificate.Obtain(request)
	if err != nil {
		recordErr(err)
		return "", "", fmt.Errorf("obtain certificate: %w", err)
	}

	domainDir := filepath.Join(m.CertDir, domain)
	if err := os.MkdirAll(domainDir, 0700); err != nil {
		recordErr(err)
		return "", "", fmt.Errorf("create cert dir: %w", err)
	}

	certPath = filepath.Join(domainDir, "cert.pem")
	keyPath = filepath.Join(domainDir, "key.pem")

	if err := os.WriteFile(certPath, certs.Certificate, 0600); err != nil {
		recordErr(err)
		return "", "", fmt.Errorf("write cert: %w", err)
	}
	if err := os.WriteFile(keyPath, certs.PrivateKey, 0600); err != nil {
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

	// Add SAN for IP or DNS
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
	if err := os.MkdirAll(selfSignedDir, 0700); err != nil {
		return "", "", fmt.Errorf("create self-signed dir: %w", err)
	}

	certPath = filepath.Join(selfSignedDir, "cert.pem")
	keyPath = filepath.Join(selfSignedDir, "key.pem")

	certFile, err := os.OpenFile(certPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
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

	keyFile, err := os.OpenFile(keyPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
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
