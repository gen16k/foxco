// Package mitm provides the proxy-issued root CA and on-the-fly leaf
// certificate minting used for transparent HTTPS interception. The root CA is
// constrained (X.509 Name Constraints) to the intercepted DNS domains so that,
// even if its private key leaks, it cannot be used to impersonate unrelated
// sites. Leaf certificates for each SNI are minted on demand, signed by the CA,
// and cached.
//
// SECURITY: installing the root CA into the OS trust store is what makes the
// minted leaves trusted. That step is performed by install.ps1 (elevated), not
// by this package. The CA private key must be protected by filesystem ACLs.
package mitm

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	caCommonName    = "PromptGate CA"
	caOrg           = "PromptGate"
	caValidity      = 10 * 365 * 24 * time.Hour
	leafValidity    = 90 * 24 * time.Hour
	leafRenewBefore = 24 * time.Hour
)

// CA is a loaded (or freshly generated) root CA that mints leaf certificates.
type CA struct {
	cert    *x509.Certificate
	key     *ecdsa.PrivateKey
	certDER []byte

	mu    sync.Mutex
	cache map[string]*tls.Certificate
}

// EnsureCA loads the CA from certPath/keyPath, generating and persisting a new
// Name-Constrained root CA (PEM) if either file is missing. constraints is the
// permitted DNS-domain subtree (e.g. ["anthropic.com"]); an empty list yields
// an unconstrained CA (not recommended).
func EnsureCA(certPath, keyPath string, constraints []string) (*CA, error) {
	if certPath == "" || keyPath == "" {
		return nil, fmt.Errorf("mitm: ca_cert_path and ca_key_path must be set")
	}
	if fileExists(certPath) && fileExists(keyPath) {
		return loadCA(certPath, keyPath)
	}
	return generateCA(certPath, keyPath, constraints)
}

// CertPEM returns the PEM-encoded root CA certificate (for trust-store install).
func (c *CA) CertPEM() []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.certDER})
}

// GetCertificate is a tls.Config.GetCertificate callback: it returns a leaf
// certificate for the requested SNI, minting and caching one if needed.
func (c *CA) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	name := hello.ServerName
	if name == "" {
		return nil, fmt.Errorf("mitm: TLS ClientHello has no SNI; cannot mint a leaf")
	}
	return c.LeafFor(name)
}

// LeafFor returns a cached or freshly minted leaf certificate for host.
func (c *CA) LeafFor(host string) (*tls.Certificate, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cur, ok := c.cache[host]; ok && cur.Leaf != nil &&
		time.Now().Before(cur.Leaf.NotAfter.Add(-leafRenewBefore)) {
		return cur, nil
	}
	leaf, err := c.mintLeaf(host)
	if err != nil {
		return nil, err
	}
	if c.cache == nil {
		c.cache = make(map[string]*tls.Certificate)
	}
	c.cache[host] = leaf
	return leaf, nil
}

func (c *CA) mintLeaf(host string) (*tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("mitm: leaf key: %w", err)
	}
	serial, err := randSerial()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: host},
		NotBefore:    now.Add(-1 * time.Hour),
		NotAfter:     now.Add(leafValidity),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{host},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, c.cert, &key.PublicKey, c.key)
	if err != nil {
		return nil, fmt.Errorf("mitm: sign leaf: %w", err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("mitm: parse leaf: %w", err)
	}
	return &tls.Certificate{
		Certificate: [][]byte{der}, // leaf only; the root is trusted via the OS store
		PrivateKey:  key,
		Leaf:        leaf,
	}, nil
}

func generateCA(certPath, keyPath string, constraints []string) (*CA, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("mitm: ca key: %w", err)
	}
	serial, err := randSerial()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: caCommonName, Organization: []string{caOrg}},
		NotBefore:             now.Add(-1 * time.Hour),
		NotAfter:              now.Add(caValidity),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            0, // may sign leaves only, never sub-CAs
		MaxPathLenZero:        true,
	}
	if len(constraints) > 0 {
		tmpl.PermittedDNSDomains = constraints
		tmpl.PermittedDNSDomainsCritical = true
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, fmt.Errorf("mitm: create ca: %w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("mitm: parse ca: %w", err)
	}
	if err := persistCA(certPath, keyPath, der, key); err != nil {
		return nil, err
	}
	return &CA{cert: cert, key: key, certDER: der, cache: map[string]*tls.Certificate{}}, nil
}

func persistCA(certPath, keyPath string, certDER []byte, key *ecdsa.PrivateKey) error {
	if err := os.MkdirAll(filepath.Dir(certPath), 0o700); err != nil {
		return fmt.Errorf("mitm: mkdir ca dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(keyPath), 0o700); err != nil {
		return fmt.Errorf("mitm: mkdir key dir: %w", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	if err := os.WriteFile(certPath, certPEM, 0o644); err != nil {
		return fmt.Errorf("mitm: write ca cert: %w", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return fmt.Errorf("mitm: marshal ca key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	// 0600: defence in depth; the authoritative protection is the NTFS ACL set
	// by install.ps1 (SYSTEM + Administrators only).
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		return fmt.Errorf("mitm: write ca key: %w", err)
	}
	return nil
}

func loadCA(certPath, keyPath string) (*CA, error) {
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, fmt.Errorf("mitm: read ca cert: %w", err)
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("mitm: read ca key: %w", err)
	}
	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil || certBlock.Type != "CERTIFICATE" {
		return nil, fmt.Errorf("mitm: ca cert is not a PEM CERTIFICATE")
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("mitm: parse ca cert: %w", err)
	}
	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return nil, fmt.Errorf("mitm: ca key is not PEM")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("mitm: parse ca key: %w", err)
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("mitm: ca key is not ECDSA")
	}
	return &CA{cert: cert, key: key, certDER: certBlock.Bytes, cache: map[string]*tls.Certificate{}}, nil
}

func randSerial() (*big.Int, error) {
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("mitm: serial: %w", err)
	}
	return serial, nil
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}
