package acme

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"
)

// makePair returns PEM cert + PEM key for a self-signed leaf valid [notBefore, notAfter]
// covering dnsName.
func makePair(t *testing.T, dnsName string, notBefore, notAfter time.Time) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: dnsName},
		DNSNames:     []string{dnsName},
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	kb, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb})
	return certPEM, keyPEM
}

func TestCADirURL(t *testing.T) {
	if got := CADirURL("production"); got != "" {
		t.Errorf("production = %q, want empty", got)
	}
	if got := CADirURL(""); got != "" {
		t.Errorf("empty = %q, want empty", got)
	}
	if got := CADirURL("unknown"); got != "" {
		t.Errorf("unknown = %q, want empty", got)
	}
	if got := CADirURL("staging"); got != "https://acme-staging-v02.api.letsencrypt.org/directory" {
		t.Errorf("staging = %q, want LE staging URL", got)
	}
}

func TestValidateManualCertValid(t *testing.T) {
	now := time.Date(2026, 6, 9, 0, 0, 0, 0, time.UTC)
	certPEM, keyPEM := makePair(t, "proxy.example.com", now.Add(-time.Hour), now.Add(90*24*time.Hour))
	info, err := ValidateManualCert(certPEM, keyPEM, "proxy.example.com", now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !info.IsValid {
		t.Error("expected IsValid true")
	}
	if info.Domain != "proxy.example.com" {
		t.Errorf("Domain = %q", info.Domain)
	}
}

func TestValidateManualCertMismatchedKey(t *testing.T) {
	now := time.Date(2026, 6, 9, 0, 0, 0, 0, time.UTC)
	certPEM, _ := makePair(t, "proxy.example.com", now.Add(-time.Hour), now.Add(90*24*time.Hour))
	_, otherKey := makePair(t, "proxy.example.com", now.Add(-time.Hour), now.Add(90*24*time.Hour))
	if _, err := ValidateManualCert(certPEM, otherKey, "proxy.example.com", now); err == nil {
		t.Fatal("expected error for mismatched key")
	}
}

func TestValidateManualCertExpired(t *testing.T) {
	now := time.Date(2026, 6, 9, 0, 0, 0, 0, time.UTC)
	certPEM, keyPEM := makePair(t, "proxy.example.com", now.Add(-100*24*time.Hour), now.Add(-time.Hour))
	if _, err := ValidateManualCert(certPEM, keyPEM, "proxy.example.com", now); err == nil {
		t.Fatal("expected error for expired cert")
	}
}

func TestValidateManualCertWrongDomain(t *testing.T) {
	now := time.Date(2026, 6, 9, 0, 0, 0, 0, time.UTC)
	certPEM, keyPEM := makePair(t, "other.example.com", now.Add(-time.Hour), now.Add(90*24*time.Hour))
	if _, err := ValidateManualCert(certPEM, keyPEM, "proxy.example.com", now); err == nil {
		t.Fatal("expected error for uncovered domain")
	}
}

func TestValidateManualCertMalformed(t *testing.T) {
	now := time.Date(2026, 6, 9, 0, 0, 0, 0, time.UTC)
	if _, err := ValidateManualCert([]byte("not pem"), []byte("not pem"), "", now); err == nil {
		t.Fatal("expected error for malformed PEM")
	}
}
