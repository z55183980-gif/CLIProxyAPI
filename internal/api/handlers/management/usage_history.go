package management

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/usagehistory"
)

func usageHistoryStore(c *gin.Context) *usagehistory.Store {
	s := usagehistory.Active()
	if s == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "usage history is unavailable"})
	}
	return s
}
func historyFilter(c *gin.Context) (usagehistory.Filter, error) {
	f := usagehistory.Filter{Start: c.Query("start"), End: c.Query("end"), Provider: c.Query("provider"), Model: c.Query("model"), AccountID: c.Query("account_id"), APIKeyID: c.Query("api_key_id"), Status: c.Query("status"), RequestType: c.Query("request_type"), Search: c.Query("search"), Sort: c.Query("sort"), Order: c.Query("order")}
	for _, p := range []struct {
		name   string
		target *int
	}{{"page", &f.Page}, {"page_size", &f.PageSize}, {"status_code", &f.StatusCode}} {
		if raw := c.Query(p.name); raw != "" {
			v, err := strconv.Atoi(raw)
			if err != nil {
				return f, err
			}
			*p.target = v
		}
	}
	if raw := c.Query("snapshot"); raw != "" {
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return f, err
		}
		f.Snapshot = v
	}
	return f, f.Validate()
}
func (h *Handler) GetUsageHistory(c *gin.Context) {
	s := usageHistoryStore(c)
	if s == nil {
		return
	}
	f, err := historyFilter(c)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	result, err := s.Query(c.Request.Context(), f)
	if err != nil {
		c.JSON(500, gin.H{"error": "could not query usage history"})
		return
	}
	c.JSON(200, result)
}

func (h *Handler) PostAccountTodayStats(c *gin.Context) {
	s := usageHistoryStore(c)
	if s == nil {
		return
	}
	var body struct {
		AccountIDs []string `json:"account_ids"`
		Start      string   `json:"start"`
		End        string   `json:"end"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 256<<10)
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "invalid account statistics request"})
		return
	}
	f := usagehistory.Filter{Start: body.Start, End: body.End}
	if err := f.Validate(); err != nil || body.Start == "" || body.End == "" || len(body.AccountIDs) > 200 {
		c.JSON(400, gin.H{"error": "valid date range and at most 200 account IDs are required"})
		return
	}
	for _, id := range body.AccountIDs {
		if strings.TrimSpace(id) == "" || len(id) > 1024 {
			c.JSON(400, gin.H{"error": "invalid account ID"})
			return
		}
	}
	stats, err := s.AccountWindowStats(c.Request.Context(), body.AccountIDs, body.Start, body.End)
	if err != nil {
		c.JSON(500, gin.H{"error": "could not query account statistics"})
		return
	}
	c.JSON(200, gin.H{"stats": stats})
}
func (h *Handler) GetUsageHistoryOptions(c *gin.Context) {
	s := usageHistoryStore(c)
	if s == nil {
		return
	}
	result, err := s.Options(c.Request.Context())
	if err != nil {
		c.JSON(500, gin.H{"error": "could not query usage options"})
		return
	}
	c.JSON(200, result)
}
func (h *Handler) GetUsagePrices(c *gin.Context) {
	s := usageHistoryStore(c)
	if s == nil {
		return
	}
	result, err := s.Prices(c.Request.Context())
	if err != nil {
		c.JSON(500, gin.H{"error": "could not query prices"})
		return
	}
	c.JSON(200, gin.H{"prices": result})
}
func (h *Handler) PutUsagePrices(c *gin.Context) {
	s := usageHistoryStore(c)
	if s == nil {
		return
	}
	var body struct {
		Prices []usagehistory.Price `json:"prices"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "invalid prices"})
		return
	}
	if err := s.SavePrices(c.Request.Context(), body.Prices); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"success": true})
}
func (h *Handler) DeleteUsageHistory(c *gin.Context) {
	s := usageHistoryStore(c)
	if s == nil {
		return
	}
	var body struct {
		Filter  usagehistory.Filter `json:"filter"`
		Confirm bool                `json:"confirm"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || !body.Confirm {
		c.JSON(400, gin.H{"error": "explicit cleanup confirmation required"})
		return
	}
	if err := body.Filter.Validate(); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	n, err := s.Delete(c.Request.Context(), body.Filter)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"deleted": n})
}
