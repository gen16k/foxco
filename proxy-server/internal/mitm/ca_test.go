package mitm

import (
	"crypto/tls"
	"crypto/x509"
	"path/filepath"
	"testing"
)

func newTestCA(t *testing.T) (*CA, string, string) {
	t.Helper()
	dir := t.TempDir()
	certPath := filepath.Join(dir, "ca", "ca.crt")
	keyPath := filepath.Join(dir, "ca", "ca.key")
	ca, err := EnsureCA(certPath, keyPath, []string{"anthropic.com"})
	if err != nil {
		t.Fatalf("EnsureCA: %v", err)
	}
	return ca, certPath, keyPath
}

func TestEnsureCAGeneratesAndReuses(t *testing.T) {
	ca1, certPath, keyPath := newTestCA(t)
	if !fileExists(certPath) || !fileExists(keyPath) {
		t.Fatal("EnsureCA did not persist cert/key")
	}
	// Second call must load the SAME CA (identical DER), not regenerate.
	ca2, err := EnsureCA(certPath, keyPath, []string{"anthropic.com"})
	if err != nil {
		t.Fatalf("EnsureCA reload: %v", err)
	}
	if string(ca1.certDER) != string(ca2.certDER) {
		t.Error("EnsureCA regenerated the CA instead of reusing the persisted one")
	}
}

func TestCAHasNameConstraint(t *testing.T) {
	ca, _, _ := newTestCA(t)
	if !ca.cert.IsCA {
		t.Error("CA cert IsCA = false")
	}
	found := false
	for _, d := range ca.cert.PermittedDNSDomains {
		if d == "anthropic.com" {
			found = true
		}
	}
	if !found {
		t.Errorf("PermittedDNSDomains = %v, want to include anthropic.com", ca.cert.PermittedDNSDomains)
	}
}

func TestLeafForInterceptedHostValidates(t *testing.T) {
	ca, _, _ := newTestCA(t)
	roots := x509.NewCertPool()
	roots.AddCert(ca.cert)

	leaf, err := ca.LeafFor("api.anthropic.com")
	if err != nil {
		t.Fatalf("LeafFor: %v", err)
	}
	if _, err := leaf.Leaf.Verify(x509.VerifyOptions{
		Roots:     roots,
		DNSName:   "api.anthropic.com",
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}); err != nil {
		t.Errorf("leaf for api.anthropic.com failed verification: %v", err)
	}
}

func TestLeafForUnconstrainedHostRejected(t *testing.T) {
	ca, _, _ := newTestCA(t)
	roots := x509.NewCertPool()
	roots.AddCert(ca.cert)

	// The CA will happily *sign* a leaf for evil.com, but the Name Constraint
	// must make it INVALID — this is the whole point of constraining the CA.
	leaf, err := ca.LeafFor("evil.com")
	if err != nil {
		t.Fatalf("LeafFor(evil.com) minting failed unexpectedly: %v", err)
	}
	if _, err := leaf.Leaf.Verify(x509.VerifyOptions{
		Roots:     roots,
		DNSName:   "evil.com",
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}); err == nil {
		t.Error("leaf for evil.com verified, but the Name Constraint should have rejected it")
	}
}

func TestGetCertificateCachesAndRequiresSNI(t *testing.T) {
	ca, _, _ := newTestCA(t)
	c1, err := ca.GetCertificate(&tls.ClientHelloInfo{ServerName: "api.anthropic.com"})
	if err != nil {
		t.Fatalf("GetCertificate: %v", err)
	}
	c2, _ := ca.GetCertificate(&tls.ClientHelloInfo{ServerName: "api.anthropic.com"})
	if c1 != c2 {
		t.Error("GetCertificate did not cache the leaf for a repeated SNI")
	}
	if _, err := ca.GetCertificate(&tls.ClientHelloInfo{ServerName: ""}); err == nil {
		t.Error("GetCertificate with empty SNI should error")
	}
}
