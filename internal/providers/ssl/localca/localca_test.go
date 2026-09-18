package localca

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestProvider(t *testing.T) *Provider {
	t.Helper()
	return NewWithDir(filepath.Join(t.TempDir(), "ssl"))
}

func TestEnsureCAGeneratesAndPersists(t *testing.T) {
	p := newTestProvider(t)
	info, err := p.EnsureCA(context.Background())
	if err != nil {
		t.Fatalf("EnsureCA: %v", err)
	}
	if info.CommonName == "" {
		t.Fatal("expected a non-empty CA common name")
	}
	if info.Trusted {
		t.Fatal("a freshly generated CA must never report as trusted")
	}
	if _, err := os.Stat(p.caCertPath()); err != nil {
		t.Fatalf("expected CA cert to be persisted: %v", err)
	}
	if _, err := os.Stat(p.caKeyPath()); err != nil {
		t.Fatalf("expected CA key to be persisted: %v", err)
	}

	// Second call must be idempotent (same CA, not regenerated).
	info2, err := p.EnsureCA(context.Background())
	if err != nil {
		t.Fatalf("second EnsureCA: %v", err)
	}
	if info2.CommonName != info.CommonName || info2.NotAfter != info.NotAfter {
		t.Fatalf("expected EnsureCA to be idempotent, got %+v then %+v", info, info2)
	}
}

func TestCAKeyFilePermissionsAreOwnerOnly(t *testing.T) {
	p := newTestProvider(t)
	if _, err := p.EnsureCA(context.Background()); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(p.caKeyPath())
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm&0o077 != 0 {
		t.Fatalf("expected CA private key to be readable only by its owner, got mode %o", perm)
	}
}

func TestIssueCertificateIsSignedByCAAndHasCorrectSAN(t *testing.T) {
	p := newTestProvider(t)
	cert, err := p.IssueCertificate(context.Background(), "myapp.test")
	if err != nil {
		t.Fatalf("IssueCertificate: %v", err)
	}
	if cert.ID == "" {
		t.Fatal("expected a non-empty certificate id")
	}
	if cert.Status != "valid" {
		t.Fatalf("expected status valid, got %q", cert.Status)
	}

	caPEM, err := p.CACertPEM()
	if err != nil {
		t.Fatal(err)
	}
	leafPEM, _, err := p.CertificatePEM(cert.ID)
	if err != nil {
		t.Fatal(err)
	}

	caBlock, _ := pem.Decode(caPEM)
	caCert, err := x509.ParseCertificate(caBlock.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	leafBlock, _ := pem.Decode(leafPEM)
	leafCert, err := x509.ParseCertificate(leafBlock.Bytes)
	if err != nil {
		t.Fatal(err)
	}

	if len(leafCert.DNSNames) != 1 || leafCert.DNSNames[0] != "myapp.test" {
		t.Fatalf("expected SAN DNSNames=[myapp.test], got %v", leafCert.DNSNames)
	}

	pool := x509.NewCertPool()
	pool.AddCert(caCert)
	chains, err := leafCert.Verify(x509.VerifyOptions{
		Roots:       pool,
		DNSName:     "myapp.test",
		CurrentTime: leafCert.NotBefore.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("expected the leaf certificate to verify against the local CA: %v", err)
	}
	if len(chains) == 0 {
		t.Fatal("expected at least one verified chain")
	}
}

func TestIssueCertificateAutoGeneratesCAIfMissing(t *testing.T) {
	p := newTestProvider(t)
	// Deliberately skip calling EnsureCA -- IssueCertificate must ensure it.
	if _, err := p.IssueCertificate(context.Background(), "auto-ca.test"); err != nil {
		t.Fatalf("expected IssueCertificate to auto-create the CA, got: %v", err)
	}
	if _, err := os.Stat(p.caCertPath()); err != nil {
		t.Fatalf("expected CA to have been created as a side effect: %v", err)
	}
}

func TestIssueCertificateRejectsEmptyDomain(t *testing.T) {
	p := newTestProvider(t)
	if _, err := p.IssueCertificate(context.Background(), ""); err == nil {
		t.Fatal("expected an error for an empty domain")
	}
}

func TestRenewReissuesSameHostnameWithNewValidity(t *testing.T) {
	p := newTestProvider(t)
	first, err := p.IssueCertificate(context.Background(), "renew.test")
	if err != nil {
		t.Fatal(err)
	}

	renewed, err := p.Renew(context.Background(), first.ID)
	if err != nil {
		t.Fatalf("Renew: %v", err)
	}
	if renewed.ID != first.ID {
		t.Fatalf("expected Renew to keep the same certificate id, got %q vs %q", renewed.ID, first.ID)
	}
	if renewed.DomainID != "renew.test" {
		t.Fatalf("expected the renewed cert to still be for renew.test, got %q", renewed.DomainID)
	}

	leafPEM, _, err := p.CertificatePEM(first.ID)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(leafPEM)
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if len(cert.DNSNames) != 1 || cert.DNSNames[0] != "renew.test" {
		t.Fatalf("expected the renewed leaf to still carry the right SAN, got %v", cert.DNSNames)
	}
}

func TestRenewUnknownCertificateReturnsErrCertificateNotFound(t *testing.T) {
	p := newTestProvider(t)
	if _, err := p.Renew(context.Background(), "does-not-exist"); err != ErrCertificateNotFound {
		t.Fatalf("expected ErrCertificateNotFound, got %v", err)
	}
}

func TestRevokeDestroysKeyAndMarksStatus(t *testing.T) {
	p := newTestProvider(t)
	cert, err := p.IssueCertificate(context.Background(), "revoke.test")
	if err != nil {
		t.Fatal(err)
	}

	if err := p.Revoke(context.Background(), cert.ID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if _, err := os.Stat(p.keyPath(cert.ID)); !os.IsNotExist(err) {
		t.Fatalf("expected the private key file to be gone after revoke, stat err: %v", err)
	}

	// Revoke is idempotent.
	if err := p.Revoke(context.Background(), cert.ID); err != nil {
		t.Fatalf("expected re-revoking to be a no-op, got: %v", err)
	}

	// Renewing a revoked cert must fail.
	if _, err := p.Renew(context.Background(), cert.ID); err == nil {
		t.Fatal("expected Renew to refuse a revoked certificate")
	}
}

func TestRevokeUnknownCertificateReturnsErrCertificateNotFound(t *testing.T) {
	p := newTestProvider(t)
	if err := p.Revoke(context.Background(), "does-not-exist"); err != ErrCertificateNotFound {
		t.Fatalf("expected ErrCertificateNotFound, got %v", err)
	}
}

func TestIssueCertificateForDifferentHostnamesAreIndependentlyVerifiable(t *testing.T) {
	p := newTestProvider(t)
	a, err := p.IssueCertificate(context.Background(), "a.test")
	if err != nil {
		t.Fatal(err)
	}
	b, err := p.IssueCertificate(context.Background(), "b.test")
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == b.ID {
		t.Fatal("expected distinct certificate ids for distinct hostnames")
	}

	caPEM, _ := p.CACertPEM()
	caBlock, _ := pem.Decode(caPEM)
	caCert, _ := x509.ParseCertificate(caBlock.Bytes)
	pool := x509.NewCertPool()
	pool.AddCert(caCert)

	for domain, id := range map[string]string{"a.test": a.ID, "b.test": b.ID} {
		leafPEM, _, err := p.CertificatePEM(id)
		if err != nil {
			t.Fatal(err)
		}
		block, _ := pem.Decode(leafPEM)
		leaf, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := leaf.Verify(x509.VerifyOptions{Roots: pool, DNSName: domain, CurrentTime: leaf.NotBefore.Add(time.Hour)}); err != nil {
			t.Fatalf("expected %s's cert to verify: %v", domain, err)
		}
	}
}
