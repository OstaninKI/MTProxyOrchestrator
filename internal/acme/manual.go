package acme

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"time"
)

// leStagingURL is the Let's Encrypt staging ACME directory.
const leStagingURL = "https://acme-staging-v02.api.letsencrypt.org/directory"

// CADirURL maps a provider name to its ACME directory URL.
// An empty result means lego's default (Let's Encrypt production).
func CADirURL(provider string) string {
	switch provider {
	case "staging":
		return leStagingURL
	default:
		return ""
	}
}

// ValidateManualCert parses and validates an uploaded certificate/key pair.
// tls.X509KeyPair both parses the PEM blocks and confirms the private key
// matches the leaf certificate. When domain is non-empty, the leaf must cover
// it (CN or SAN). An expired leaf (now after NotAfter) is rejected.
func ValidateManualCert(certPEM, keyPEM []byte, domain string, now time.Time) (CertInfo, error) {
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return CertInfo{}, fmt.Errorf("certificate/key pair invalid: %w", err)
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return CertInfo{}, fmt.Errorf("parse leaf certificate: %w", err)
	}
	if now.IsZero() {
		now = time.Now()
	}
	if now.After(leaf.NotAfter) {
		return CertInfo{}, fmt.Errorf("certificate expired on %s", leaf.NotAfter.Format(time.RFC3339))
	}
	if domain != "" {
		if err := leaf.VerifyHostname(domain); err != nil {
			return CertInfo{}, fmt.Errorf("certificate does not cover domain %s: %w", domain, err)
		}
	}
	name := leaf.Subject.CommonName
	if len(leaf.DNSNames) > 0 {
		name = leaf.DNSNames[0]
	}
	return CertInfo{
		Mode:      CertModeACME,
		Domain:    name,
		ExpiresAt: leaf.NotAfter,
		IssuedAt:  leaf.NotBefore,
		IsValid:   now.Before(leaf.NotAfter) && now.After(leaf.NotBefore),
	}, nil
}
