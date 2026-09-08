package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

// getBillingUsage returns token and monetary totals grouped by account. The
// pricing plugin updates the process-wide read model after each successful,
// idempotently recorded charge. A provider callback can replace this with a
// durable SQL query when the server is embedded with a billing database.
func (s *Server) getBillingUsage(c *gin.Context) {
	if s == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "billing is unavailable"})
		return
	}
	if s.billingUsageProvider != nil {
		totals, err := s.billingUsageProvider(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"accounts": totals})
		return
	}
	c.JSON(http.StatusOK, gin.H{"accounts": usage.DefaultBillingTotals().Snapshot()})
}
