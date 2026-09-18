package v1

import (
	"net/http"

	"github.com/alresiainc/alresia-voltpanel/internal/metrics"
	"github.com/gin-gonic/gin"
)

func systemMetrics(c *gin.Context) {
	m, err := metrics.Collect()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, m)
}
