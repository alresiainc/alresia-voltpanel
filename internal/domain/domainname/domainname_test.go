package domainname

import (
	"testing"
	"time"

	"github.com/alresiainc/alresia-voltpanel/internal/storage"
)

func newTestRepo(t *testing.T) *Repository {
	t.Helper()
	db, err := storage.OpenSQLite(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return NewRepository(db)
}

func TestCreateAndGetDomain(t *testing.T) {
	r := newTestRepo(t)
	d, err := r.CreateDomain("myapp.test", "", 3000)
	if err != nil {
		t.Fatalf("CreateDomain: %v", err)
	}
	if d.ID == "" {
		t.Fatal("expected a non-empty id")
	}
	if d.Provider != "hosts" {
		t.Fatalf("expected default provider 'hosts', got %q", d.Provider)
	}
	if !d.Enabled {
		t.Fatal("expected a newly created domain to be enabled")
	}
	if d.SSLEnabled {
		t.Fatal("expected a newly created domain to not have SSL enabled yet")
	}

	got, err := r.GetDomain(d.ID)
	if err != nil {
		t.Fatalf("GetDomain: %v", err)
	}
	if got.Hostname != "myapp.test" || got.Port != 3000 {
		t.Fatalf("unexpected domain: %+v", got)
	}
}

func TestCreateDomainRejectsEmptyHostname(t *testing.T) {
	r := newTestRepo(t)
	if _, err := r.CreateDomain("", "", 0); err == nil {
		t.Fatal("expected an error for an empty hostname")
	}
}

func TestCreateDomainDuplicateHostnameReturnsErrHostnameTaken(t *testing.T) {
	r := newTestRepo(t)
	if _, err := r.CreateDomain("dupe.test", "", 0); err != nil {
		t.Fatal(err)
	}
	_, err := r.CreateDomain("dupe.test", "", 0)
	if err != ErrHostnameTaken {
		t.Fatalf("expected ErrHostnameTaken, got %v", err)
	}
}

func TestCreateDomainWithEmptyProjectIDStoresNull(t *testing.T) {
	// project_id has a foreign-key constraint against projects(id) and this
	// daemon runs with PRAGMA foreign_keys=ON -- an empty string would fail
	// that check where NULL does not. Successfully creating with "" proves
	// the NULL conversion happened.
	r := newTestRepo(t)
	d, err := r.CreateDomain("no-project.test", "", 0)
	if err != nil {
		t.Fatalf("expected an empty projectID to succeed (stored as NULL), got: %v", err)
	}
	got, err := r.GetDomain(d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ProjectID != "" {
		t.Fatalf("expected ProjectID to round-trip as empty, got %q", got.ProjectID)
	}
}

func TestGetUnknownDomainReturnsErrNotFound(t *testing.T) {
	r := newTestRepo(t)
	if _, err := r.GetDomain("does-not-exist"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestListDomains(t *testing.T) {
	r := newTestRepo(t)
	if _, err := r.CreateDomain("a.test", "", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := r.CreateDomain("b.test", "", 0); err != nil {
		t.Fatal(err)
	}
	list, err := r.ListDomains()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 domains, got %d", len(list))
	}
}

func TestDeleteDomain(t *testing.T) {
	r := newTestRepo(t)
	d, err := r.CreateDomain("gone.test", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.DeleteDomain(d.ID); err != nil {
		t.Fatalf("DeleteDomain: %v", err)
	}
	if _, err := r.GetDomain(d.ID); err != ErrNotFound {
		t.Fatalf("expected deleted domain to 404, got %v", err)
	}
}

func TestDeleteUnknownDomainReturnsErrNotFound(t *testing.T) {
	r := newTestRepo(t)
	if err := r.DeleteDomain("does-not-exist"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestSetSSLEnabled(t *testing.T) {
	r := newTestRepo(t)
	d, err := r.CreateDomain("ssl.test", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.SetSSLEnabled(d.ID, true); err != nil {
		t.Fatalf("SetSSLEnabled: %v", err)
	}
	got, err := r.GetDomain(d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.SSLEnabled {
		t.Fatal("expected ssl_enabled to be true after SetSSLEnabled(true)")
	}
}

func TestCreateAndGetCertificate(t *testing.T) {
	r := newTestRepo(t)
	d, err := r.CreateDomain("cert.test", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	notBefore := time.Now().UTC()
	notAfter := notBefore.AddDate(0, 0, 825)
	cert, err := r.CreateCertificate("cert-id-1", d.ID, "local-ca", notBefore, notAfter, "valid")
	if err != nil {
		t.Fatalf("CreateCertificate: %v", err)
	}
	if cert.ID != "cert-id-1" {
		t.Fatalf("expected the given id to be used, got %q", cert.ID)
	}

	got, err := r.GetCertificate("cert-id-1")
	if err != nil {
		t.Fatalf("GetCertificate: %v", err)
	}
	if got.DomainID != d.ID || got.Status != "valid" {
		t.Fatalf("unexpected certificate: %+v", got)
	}
	if !got.NotAfter.After(got.NotBefore) {
		t.Fatalf("expected NotAfter to be after NotBefore, got %+v", got)
	}
}

func TestCreateCertificateRequiresExistingDomain(t *testing.T) {
	// certificates.domain_id is NOT NULL REFERENCES domains(id), enforced
	// because this daemon runs with PRAGMA foreign_keys=ON.
	r := newTestRepo(t)
	_, err := r.CreateCertificate("orphan-cert", "does-not-exist", "local-ca", time.Now(), time.Now(), "valid")
	if err == nil {
		t.Fatal("expected creating a certificate for a nonexistent domain to fail")
	}
}

func TestListCertificatesForDomain(t *testing.T) {
	r := newTestRepo(t)
	d, err := r.CreateDomain("multi-cert.test", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := r.CreateCertificate("c1", d.ID, "local-ca", now, now.AddDate(0, 0, 825), "revoked"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.CreateCertificate("c2", d.ID, "local-ca", now.AddDate(0, 0, 1), now.AddDate(0, 0, 826), "valid"); err != nil {
		t.Fatal(err)
	}
	list, err := r.ListCertificatesForDomain(d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 certificates, got %d", len(list))
	}
}

func TestUpdateCertificateStatus(t *testing.T) {
	r := newTestRepo(t)
	d, err := r.CreateDomain("renew.test", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := r.CreateCertificate("renewable", d.ID, "local-ca", now, now.AddDate(0, 0, 825), "valid"); err != nil {
		t.Fatal(err)
	}
	newNotBefore := now.AddDate(0, 0, 800)
	newNotAfter := now.AddDate(0, 0, 1625)
	if err := r.UpdateCertificateStatus("renewable", "valid", newNotBefore, newNotAfter); err != nil {
		t.Fatalf("UpdateCertificateStatus: %v", err)
	}
	got, err := r.GetCertificate("renewable")
	if err != nil {
		t.Fatal(err)
	}
	if got.NotAfter.Unix() != newNotAfter.Unix() {
		t.Fatalf("expected NotAfter to be updated, got %v want %v", got.NotAfter, newNotAfter)
	}
}

func TestUpdateCertificateStatusUnknownReturnsErrNotFound(t *testing.T) {
	r := newTestRepo(t)
	if err := r.UpdateCertificateStatus("does-not-exist", "valid", time.Now(), time.Now()); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
