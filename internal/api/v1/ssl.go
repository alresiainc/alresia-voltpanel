// ssl.go implements POST /api/v1/ssl/ca/ensure, POST /api/v1/ssl/certificates
// and POST /api/v1/ssl/ca/trust (§17 Phase 4). Handlers glue Deps.SSL (the
// real local-CA SSLProvider) together with the internal/domain/domainname
// repository, which persists certificate metadata (never key material --
// that stays on disk under the provider's own directory, §9.7).
//
// trustCA is the one handler in this whole file that would modify the
// real OS/browser trust store if actually invoked with confirmed:true by
// a real user on their own machine -- it mirrors SSLProvider.TrustCA's own
// confirmed-gate rather than hardcoding true, so the provider's refusal
// stays authoritative even if this handler had a bug.
package v1

import (
	"errors"
	"net/http"
	"time"

	"github.com/alresiainc/alresia-voltpanel/internal/domain/domainname"
	"github.com/alresiainc/alresia-voltpanel/internal/providers/ssl/localca"
	"github.com/gin-gonic/gin"
)

// caInfoView/certificateView are the JSON shapes this API returns --
// providers.CAInfo/Certificate have no json tags (they're shared,
// interface-defined placeholder types per internal/providers/contract.go),
// so these views give the wire format proper camelCase without touching
// that shared file.
type caInfoView struct {
	CommonName string `json:"commonName"`
	NotAfter   string `json:"notAfter"`
	Trusted    bool   `json:"trusted"`
}

func ensureCA(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		if d.SSL == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "SSL provider not available"})
			return
		}
		info, err := d.SSL.EnsureCA(c.Request.Context())
		audit(d.DB(), "ssl.ca.ensure", "ca", info.CommonName, resultOf(err))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, caInfoView{CommonName: info.CommonName, NotAfter: info.NotAfter, Trusted: info.Trusted})
	}
}

// issueCertificate issues a leaf certificate for an already-registered
// domain, persists the resulting certificate row (reusing the provider's
// own certificate ID so the DB row and the on-disk PEM material stay
// addressable by the same ID), and flips the domain's ssl_enabled flag.
func issueCertificate(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		if d.SSL == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "SSL provider not available"})
			return
		}
		var body struct {
			DomainID string `json:"domainId"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if body.DomainID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "domainId must not be empty"})
			return
		}

		repo := domainRepo(d)
		domain, err := repo.GetDomain(body.DomainID)
		if err != nil {
			writeDomainError(c, err)
			return
		}

		providerCert, err := d.SSL.IssueCertificate(c.Request.Context(), domain.Hostname)
		if err != nil {
			audit(d.DB(), "ssl.certificate.issue", "domain", domain.ID, "error")
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		notBefore, notAfter, err := parseCertWindow(providerCert.NotBefore, providerCert.NotAfter)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		cert, err := repo.CreateCertificate(providerCert.ID, domain.ID, "local-ca", notBefore, notAfter, providerCert.Status)
		if err != nil {
			audit(d.DB(), "ssl.certificate.issue", "domain", domain.ID, "error")
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if err := repo.SetSSLEnabled(domain.ID, true); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		audit(d.DB(), "ssl.certificate.issue", "domain", domain.ID, "ok")
		c.JSON(http.StatusCreated, cert)
	}
}

func parseCertWindow(notBefore, notAfter string) (time.Time, time.Time, error) {
	nb, err := time.Parse(time.RFC3339, notBefore)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	na, err := time.Parse(time.RFC3339, notAfter)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	return nb, na, nil
}

// trustCA is the one endpoint in this whole task that would touch the real
// OS/browser trust store if actually invoked with confirmed:true by a real
// user on their own machine (§9.6/§22: TrustCA never runs implicitly).
// This handler adds no confirmation logic of its own beyond forwarding the
// request body's value straight through to SSLProvider.TrustCA, which is
// the actual, authoritative gate -- confirmed must never be hardcoded here.
func trustCA(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		if d.SSL == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "SSL provider not available"})
			return
		}
		var body struct {
			Confirmed bool `json:"confirmed"`
		}
		// A missing/unparseable body is treated as confirmed=false (the
		// safe default), not an error -- ShouldBindJSON leaves Confirmed
		// at its zero value either way.
		_ = c.ShouldBindJSON(&body)

		err := d.SSL.TrustCA(c.Request.Context(), body.Confirmed)
		audit(d.DB(), "ssl.ca.trust", "ca", "local", resultOf(err))
		if err != nil {
			if errors.Is(err, localca.ErrTrustNotConfirmed) {
				c.JSON(http.StatusBadRequest, gin.H{"error": "trusting the local CA requires \"confirmed\": true in the request body -- this modifies your OS/browser trust store"})
				return
			}
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}

var _ = domainname.ErrNotFound // keep the import even if unused directly here in future edits
