// Package localca implements providers.SSLProvider (§7/§14 of the
// implementation plan) as a local, mkcert-style certificate authority
// built entirely on Go's standard library (crypto/x509, crypto/ecdsa) --
// no external `mkcert` binary is required.
//
// EnsureCA generates a root CA key+cert once and persists it under the
// daemon's config dir (never in SQLite -- §9.7/§23: private keys don't
// belong in the structured-state DB). IssueCertificate signs a leaf
// certificate for one hostname off that CA. Renew re-issues a leaf in
// place; Revoke destroys its private key and marks it revoked.
//
// TrustCA -- the one operation here that would modify the real OS/browser
// trust store -- lives in trust.go, gated so it only ever runs with an
// explicit confirmed=true, and never invoked by anything else in this
// package.
package localca

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/alresiainc/alresia-voltpanel/internal/providers"
	"github.com/google/uuid"
)

// ErrCertificateNotFound is returned by Renew/Revoke when certID doesn't
// match a certificate this CA issued.
var ErrCertificateNotFound = errors.New("localca: certificate not found")

// caValidity is the root CA's own lifetime. Long-lived by design (like
// mkcert's root, which is valid for years) -- it's a local development
// convenience, not a publicly trusted CA subject to the shorter lifetimes
// the public Web PKI enforces.
const caValidity = 10 * 365 * 24 * time.Hour

// leafValidity mirrors mkcert's own leaf lifetime convention (825 days --
// the Apple/Chrome-enforced maximum for publicly trusted certs; there's no
// such cap for a locally-trusted CA, but matching it avoids surprising
// anyone used to mkcert's behavior).
const leafValidity = 825 * 24 * time.Hour

// Provider implements providers.SSLProvider, storing CA and issued-leaf
// material under Dir.
type Provider struct {
	// Dir is the directory the CA key/cert and every issued leaf
	// cert/key live under. 0700, never SQLite (§9.7).
	Dir string

	runner commandRunner // see trust.go; real exec.Command by default
	mu     sync.Mutex
}

// New returns a Provider rooted at <cfgDir>/ssl -- the production
// constructor, called from internal/server with the daemon's real config
// dir (internal/storage.EnsureDirs()).
func New(cfgDir string) *Provider {
	return &Provider{Dir: filepath.Join(cfgDir, "ssl"), runner: realRunner{}}
}

// NewWithDir returns a Provider rooted at an explicit directory -- used by
// this package's own tests (always a t.TempDir()) and available to any
// caller that wants a non-default location.
func NewWithDir(dir string) *Provider {
	return &Provider{Dir: dir, runner: realRunner{}}
}

// Compile-time check that Provider satisfies the shared contract.
var _ providers.SSLProvider = (*Provider)(nil)

func (p *Provider) caCertPath() string        { return filepath.Join(p.Dir, "ca-cert.pem") }
func (p *Provider) caKeyPath() string         { return filepath.Join(p.Dir, "ca-key.pem") }
func (p *Provider) trustedMarkerPath() string { return filepath.Join(p.Dir, "trusted") }
func (p *Provider) certsDir() string          { return filepath.Join(p.Dir, "certs") }
func (p *Provider) certDir(id string) string  { return filepath.Join(p.certsDir(), id) }
func (p *Provider) certPath(id string) string { return filepath.Join(p.certDir(id), "cert.pem") }
func (p *Provider) keyPath(id string) string  { return filepath.Join(p.certDir(id), "key.pem") }
func (p *Provider) metaPath(id string) string { return filepath.Join(p.certDir(id), "meta.json") }

type certMeta struct {
	Hostname  string    `json:"hostname"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
}

func caCommonName() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "local"
	}
	return fmt.Sprintf("VoltPanel Local Development CA (%s)", host)
}

func newSerial() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	return rand.Int(rand.Reader, limit)
}

// EnsureCA creates the root CA once (persisting its key+cert under Dir)
// and is a no-op returning the existing CA's info on every later call.
// Never touches the OS/browser trust store -- that's TrustCA's job alone.
func (p *Provider) EnsureCA(_ context.Context) (providers.CAInfo, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.ensureCALocked()
}

func (p *Provider) ensureCALocked() (providers.CAInfo, error) {
	if cert, _, err := p.loadCA(); err == nil {
		return providers.CAInfo{
			CommonName: cert.Subject.CommonName,
			NotAfter:   cert.NotAfter.UTC().Format(time.RFC3339),
			Trusted:    p.isTrusted(),
		}, nil
	}

	if err := os.MkdirAll(p.Dir, 0o700); err != nil {
		return providers.CAInfo{}, fmt.Errorf("localca: create %s: %w", p.Dir, err)
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return providers.CAInfo{}, fmt.Errorf("localca: generate CA key: %w", err)
	}
	serial, err := newSerial()
	if err != nil {
		return providers.CAInfo{}, fmt.Errorf("localca: generate CA serial: %w", err)
	}
	now := time.Now().UTC()
	cn := caCommonName()
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: cn, Organization: []string{"VoltPanel"}},
		NotBefore:             now.Add(-1 * time.Hour),
		NotAfter:              now.Add(caValidity),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return providers.CAInfo{}, fmt.Errorf("localca: self-sign CA: %w", err)
	}
	if err := writeKeyPEM(p.caKeyPath(), key); err != nil {
		return providers.CAInfo{}, err
	}
	if err := writeCertPEM(p.caCertPath(), der); err != nil {
		return providers.CAInfo{}, err
	}
	return providers.CAInfo{CommonName: cn, NotAfter: tmpl.NotAfter.UTC().Format(time.RFC3339), Trusted: false}, nil
}

func (p *Provider) isTrusted() bool {
	_, err := os.Stat(p.trustedMarkerPath())
	return err == nil
}

// loadCA reads and parses the persisted CA key+cert. Returns an error
// (never generates anything) if EnsureCA hasn't run yet.
func (p *Provider) loadCA() (*x509.Certificate, *ecdsa.PrivateKey, error) {
	certPEM, err := os.ReadFile(p.caCertPath())
	if err != nil {
		return nil, nil, fmt.Errorf("localca: no CA yet (call EnsureCA first): %w", err)
	}
	keyPEM, err := os.ReadFile(p.caKeyPath())
	if err != nil {
		return nil, nil, fmt.Errorf("localca: CA cert exists but key is missing: %w", err)
	}
	cert, err := parseCertPEM(certPEM)
	if err != nil {
		return nil, nil, fmt.Errorf("localca: parse CA cert: %w", err)
	}
	key, err := parseECDSAKeyPEM(keyPEM)
	if err != nil {
		return nil, nil, fmt.Errorf("localca: parse CA key: %w", err)
	}
	return cert, key, nil
}

// IssueCertificate signs a fresh leaf certificate for domain off the local
// CA, generating the CA first if it doesn't exist yet. The returned
// Certificate.ID also names the on-disk directory (under Dir/certs/<id>)
// holding the leaf's cert.pem/key.pem -- CertificatePEM retrieves them.
func (p *Provider) IssueCertificate(ctx context.Context, domain string) (providers.Certificate, error) {
	if domain == "" {
		return providers.Certificate{}, fmt.Errorf("localca: domain must not be empty")
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	if _, err := p.ensureCALocked(); err != nil {
		return providers.Certificate{}, err
	}
	caCert, caKey, err := p.loadCA()
	if err != nil {
		return providers.Certificate{}, err
	}

	id := uuid.NewString()
	cert, err := p.signLeaf(id, domain, caCert, caKey)
	if err != nil {
		return providers.Certificate{}, err
	}
	if err := p.writeMeta(id, certMeta{Hostname: domain, Status: "valid", CreatedAt: time.Now().UTC()}); err != nil {
		return providers.Certificate{}, err
	}
	return providerCertificate(id, domain, cert), nil
}

func (p *Provider) signLeaf(id, domain string, caCert *x509.Certificate, caKey *ecdsa.PrivateKey) (*x509.Certificate, error) {
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("localca: generate leaf key: %w", err)
	}
	serial, err := newSerial()
	if err != nil {
		return nil, fmt.Errorf("localca: generate leaf serial: %w", err)
	}
	now := time.Now().UTC()
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: domain},
		DNSNames:              []string{domain},
		NotBefore:             now.Add(-1 * time.Hour),
		NotAfter:              now.Add(leafValidity),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, &leafKey.PublicKey, caKey)
	if err != nil {
		return nil, fmt.Errorf("localca: sign leaf for %s: %w", domain, err)
	}
	if err := os.MkdirAll(p.certDir(id), 0o700); err != nil {
		return nil, err
	}
	if err := writeKeyPEM(p.keyPath(id), leafKey); err != nil {
		return nil, err
	}
	if err := writeCertPEM(p.certPath(id), der); err != nil {
		return nil, err
	}
	return parseCertPEM(mustPEMFromDER(der))
}

func (p *Provider) writeMeta(id string, m certMeta) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p.metaPath(id), b, 0o600)
}

func (p *Provider) readMeta(id string) (certMeta, error) {
	b, err := os.ReadFile(p.metaPath(id))
	if err != nil {
		if os.IsNotExist(err) {
			return certMeta{}, ErrCertificateNotFound
		}
		return certMeta{}, err
	}
	var m certMeta
	if err := json.Unmarshal(b, &m); err != nil {
		return certMeta{}, err
	}
	return m, nil
}

func providerCertificate(id, domain string, cert *x509.Certificate) providers.Certificate {
	return providers.Certificate{
		ID:        id,
		DomainID:  domain,
		NotBefore: cert.NotBefore.UTC().Format(time.RFC3339),
		NotAfter:  cert.NotAfter.UTC().Format(time.RFC3339),
		Status:    "valid",
	}
}

// Renew re-signs certID's leaf for the same hostname, in place (same ID,
// fresh key+cert). Fails with ErrCertificateNotFound for an unknown or
// already-revoked certID.
func (p *Provider) Renew(_ context.Context, certID string) (providers.Certificate, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	meta, err := p.readMeta(certID)
	if err != nil {
		return providers.Certificate{}, err
	}
	if meta.Status == "revoked" {
		return providers.Certificate{}, fmt.Errorf("localca: cannot renew a revoked certificate (%s)", certID)
	}
	caCert, caKey, err := p.loadCA()
	if err != nil {
		return providers.Certificate{}, err
	}
	cert, err := p.signLeaf(certID, meta.Hostname, caCert, caKey)
	if err != nil {
		return providers.Certificate{}, err
	}
	meta.CreatedAt = time.Now().UTC()
	meta.Status = "valid"
	if err := p.writeMeta(certID, meta); err != nil {
		return providers.Certificate{}, err
	}
	return providerCertificate(certID, meta.Hostname, cert), nil
}

// Revoke destroys certID's private key (so it can never be used to
// terminate TLS again) and marks it revoked. Idempotent: revoking an
// already-revoked certificate succeeds without error.
func (p *Provider) Revoke(_ context.Context, certID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	meta, err := p.readMeta(certID)
	if err != nil {
		return err
	}
	if meta.Status == "revoked" {
		return nil
	}
	if err := os.Remove(p.keyPath(certID)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("localca: destroy leaf key for %s: %w", certID, err)
	}
	meta.Status = "revoked"
	return p.writeMeta(certID, meta)
}

// CACertPEM returns the persisted CA certificate's PEM bytes. Exported for
// callers (a future WebServerProvider, or a "download the CA cert" UI
// action) that need the raw bytes rather than the CAInfo summary --
// EnsureCA must have been called at least once already.
func (p *Provider) CACertPEM() ([]byte, error) {
	return os.ReadFile(p.caCertPath())
}

// CertificatePEM returns the leaf certificate and private key PEM bytes
// issued under certID. Exported for the same reason as CACertPEM.
func (p *Provider) CertificatePEM(certID string) (certPEM, keyPEM []byte, err error) {
	certPEM, err = os.ReadFile(p.certPath(certID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, ErrCertificateNotFound
		}
		return nil, nil, err
	}
	keyPEM, err = os.ReadFile(p.keyPath(certID))
	if err != nil {
		if os.IsNotExist(err) {
			// Revoked certs have no key left; that's expected, not an error.
			return certPEM, nil, nil
		}
		return nil, nil, err
	}
	return certPEM, keyPEM, nil
}

// --- PEM helpers ------------------------------------------------------------

func writeCertPEM(path string, der []byte) error {
	b := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	return os.WriteFile(path, b, 0o644)
}

func writeKeyPEM(path string, key *ecdsa.PrivateKey) error {
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return fmt.Errorf("localca: marshal private key: %w", err)
	}
	b := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	return os.WriteFile(path, b, 0o600)
}

func mustPEMFromDER(der []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func parseCertPEM(b []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(b)
	if block == nil {
		return nil, fmt.Errorf("localca: no PEM block found")
	}
	return x509.ParseCertificate(block.Bytes)
}

func parseECDSAKeyPEM(b []byte) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode(b)
	if block == nil {
		return nil, fmt.Errorf("localca: no PEM block found")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	ecKey, ok := key.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("localca: private key is not ECDSA")
	}
	return ecKey, nil
}
