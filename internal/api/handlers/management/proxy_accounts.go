package management

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/proxyregistry"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/proxyutil"
)

func (h *Handler) proxyRegistry() (*proxyregistry.Registry, error) {
	if h == nil || h.cfg == nil {
		return nil, fmt.Errorf("configuration unavailable")
	}
	return proxyregistry.ConfigureForAuthDir(h.cfg.AuthDir), nil
}

// ListProxyAccounts returns reusable proxy accounts without passwords.
func (h *Handler) ListProxyAccounts(c *gin.Context) {
	registry, errRegistry := h.proxyRegistry()
	if errRegistry != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": errRegistry.Error()})
		return
	}
	accounts, errList := registry.List()
	if errList != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": errList.Error()})
		return
	}
	if h.authManager != nil {
		for i := range accounts {
			for _, auth := range h.authManager.List() {
				if auth != nil && strings.EqualFold(authProxyID(auth), accounts[i].ID) {
					accounts[i].AccountCount++
				}
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{"proxies": accounts, "proxy_accounts": accounts})
}

// CreateProxyAccount adds a reusable proxy account.
func (h *Handler) CreateProxyAccount(c *gin.Context) {
	registry, errRegistry := h.proxyRegistry()
	if errRegistry != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": errRegistry.Error()})
		return
	}
	var account proxyregistry.ProxyAccount
	if errBind := c.ShouldBindJSON(&account); errBind != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid proxy account"})
		return
	}
	created, errCreate := registry.Create(account)
	if errCreate != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": errCreate.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"proxy": created})
}

// CreateProxyAccountsBatch creates up to one hundred reusable proxy accounts.
func (h *Handler) CreateProxyAccountsBatch(c *gin.Context) {
	var request struct {
		Items []proxyregistry.ProxyAccount `json:"items"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || len(request.Items) == 0 || len(request.Items) > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "items must contain between 1 and 100 proxy accounts"})
		return
	}
	registry, errRegistry := h.proxyRegistry()
	if errRegistry != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": errRegistry.Error()})
		return
	}
	result := struct {
		SuccessIDs []string `json:"success_ids"`
		Failures   []any    `json:"failures"`
	}{SuccessIDs: []string{}, Failures: []any{}}
	for index, item := range request.Items {
		created, errCreate := registry.Create(item)
		if errCreate != nil {
			result.Failures = append(result.Failures, gin.H{"index": index, "message": errCreate.Error()})
			continue
		}
		result.SuccessIDs = append(result.SuccessIDs, created.ID)
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

// UpdateProxyAccountsBatch updates the status of selected reusable proxies.
func (h *Handler) UpdateProxyAccountsBatch(c *gin.Context) {
	var request struct {
		IDs   []string                   `json:"ids"`
		Patch map[string]json.RawMessage `json:"patch"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || len(request.IDs) == 0 || len(request.IDs) > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ids must contain between 1 and 100 proxy accounts"})
		return
	}
	registry, errRegistry := h.proxyRegistry()
	if errRegistry != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": errRegistry.Error()})
		return
	}
	var status string
	if raw, ok := request.Patch["status"]; ok {
		_ = json.Unmarshal(raw, &status)
	}
	if status != "active" && status != "inactive" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "status must be active or inactive"})
		return
	}
	result := struct {
		SuccessIDs []string `json:"success_ids"`
		Failures   []any    `json:"failures"`
	}{SuccessIDs: []string{}, Failures: []any{}}
	for _, id := range request.IDs {
		current, found, errGet := registry.Get(id)
		if errGet != nil || !found {
			result.Failures = append(result.Failures, gin.H{"id": id, "message": "proxy account not found"})
			continue
		}
		current.Status = status
		updated, errUpdate := registry.Update(id, current)
		if errUpdate != nil {
			result.Failures = append(result.Failures, gin.H{"id": id, "message": errUpdate.Error()})
			continue
		}
		if errRefresh := h.refreshAuthProxyReferences(c, id, registry); errRefresh != nil {
			result.Failures = append(result.Failures, gin.H{"id": id, "message": errRefresh.Error()})
			continue
		}
		result.SuccessIDs = append(result.SuccessIDs, updated.ID)
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

// DeleteProxyAccountsBatch removes selected reusable proxy accounts.
func (h *Handler) DeleteProxyAccountsBatch(c *gin.Context) {
	var request struct {
		IDs []string `json:"ids"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || len(request.IDs) == 0 || len(request.IDs) > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ids must contain between 1 and 100 proxy accounts"})
		return
	}
	registry, errRegistry := h.proxyRegistry()
	if errRegistry != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": errRegistry.Error()})
		return
	}
	result := struct {
		SuccessIDs []string `json:"success_ids"`
		Failures   []any    `json:"failures"`
	}{SuccessIDs: []string{}, Failures: []any{}}
	for _, id := range request.IDs {
		if errDelete := h.deleteProxyAccount(registry, c, id); errDelete != nil {
			result.Failures = append(result.Failures, gin.H{"id": id, "message": errDelete.Error()})
			continue
		}
		result.SuccessIDs = append(result.SuccessIDs, id)
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

// UpdateProxyAccount updates a reusable proxy account and refreshes referencing auths.
func (h *Handler) UpdateProxyAccount(c *gin.Context) {
	registry, errRegistry := h.proxyRegistry()
	if errRegistry != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": errRegistry.Error()})
		return
	}
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "proxy account id is required"})
		return
	}
	var account proxyregistry.ProxyAccount
	if errBind := c.ShouldBindJSON(&account); errBind != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid proxy account"})
		return
	}
	updated, errUpdate := registry.Update(id, account)
	if errUpdate != nil {
		if errors.Is(errUpdate, os.ErrNotExist) {
			c.JSON(http.StatusNotFound, gin.H{"error": "proxy account not found"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": errUpdate.Error()})
		return
	}
	if errRefresh := h.refreshAuthProxyReferences(c, id, registry); errRefresh != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": errRefresh.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"proxy": updated})
}

// GetProxyAccount returns one reusable proxy account without its password.
func (h *Handler) GetProxyAccount(c *gin.Context) {
	registry, errRegistry := h.proxyRegistry()
	if errRegistry != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": errRegistry.Error()})
		return
	}
	account, found, errGet := registry.Get(strings.TrimSpace(c.Param("id")))
	if errGet != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": errGet.Error()})
		return
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "proxy account not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"proxy": account})
}

// TestProxyAccount checks connectivity through a reusable proxy and records its egress metadata.
func (h *Handler) TestProxyAccount(c *gin.Context) {
	registry, errRegistry := h.proxyRegistry()
	if errRegistry != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": errRegistry.Error()})
		return
	}
	id := strings.TrimSpace(c.Param("id"))
	proxyURL, errResolve := registry.ResolveURL(id)
	if errResolve != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": errResolve.Error()})
		return
	}
	if proxyURL == "" || proxyURL == "direct" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "proxy account is unavailable"})
		return
	}
	transport, _, errTransport := proxyutil.BuildHTTPTransport(proxyURL)
	if errTransport != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": errTransport.Error()})
		return
	}
	client := &http.Client{Transport: transport}
	testURL := strings.TrimSpace(os.Getenv("UPSTREAM_PROXY_TEST_URL"))
	if testURL == "" {
		testURL = "https://ipinfo.io/json"
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	started := time.Now()
	req, errRequest := http.NewRequestWithContext(ctx, http.MethodGet, testURL, nil)
	if errRequest != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": errRequest.Error()})
		return
	}
	resp, errDo := client.Do(req)
	result := proxyregistry.TestResult{LatencyMs: time.Since(started).Milliseconds(), Status: "connection_failed"}
	if errDo == nil && resp != nil {
		defer resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			var payload struct {
				IP      string `json:"ip"`
				Country string `json:"country"`
				Region  string `json:"region"`
				City    string `json:"city"`
			}
			if errDecode := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); errDecode == nil {
				result.Status = "ok"
				result.Message = "ok"
				result.IP, result.Country, result.Region, result.City = payload.IP, payload.Country, payload.Region, payload.City
			} else {
				result.Status, result.Message = "invalid_probe_response", errDecode.Error()
			}
		} else {
			result.Status = fmt.Sprintf("probe_status_%d", resp.StatusCode)
			result.Message = result.Status
		}
	} else if errDo != nil {
		result.Message = errDo.Error()
	}
	if errUpdate := registry.UpdateTestState(id, result); errUpdate != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": errUpdate.Error()})
		return
	}
	status := http.StatusOK
	if result.Status != "ok" {
		status = http.StatusBadGateway
	}
	c.JSON(status, gin.H{"success": result.Status == "ok", "result": result})
}

// DeleteProxyAccount removes a reusable proxy account when no auth references it.
func (h *Handler) DeleteProxyAccount(c *gin.Context) {
	registry, errRegistry := h.proxyRegistry()
	if errRegistry != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": errRegistry.Error()})
		return
	}
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "proxy account id is required"})
		return
	}
	if errDelete := h.deleteProxyAccount(registry, c, id); errDelete != nil {
		if errors.Is(errDelete, os.ErrNotExist) {
			c.JSON(http.StatusNotFound, gin.H{"error": "proxy account not found"})
			return
		}
		if strings.Contains(errDelete.Error(), "still referenced") {
			c.JSON(http.StatusConflict, gin.H{"error": errDelete.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": errDelete.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (h *Handler) deleteProxyAccount(registry *proxyregistry.Registry, c *gin.Context, id string) error {
	if h.authManager != nil {
		for _, auth := range h.authManager.List() {
			if auth != nil && strings.EqualFold(authProxyID(auth), id) {
				return fmt.Errorf("proxy account is still referenced by an auth file")
			}
		}
	}
	return registry.Delete(id)
}

func authProxyID(auth *coreauth.Auth) string {
	if auth == nil || auth.Metadata == nil {
		return ""
	}
	value, _ := auth.Metadata["proxy_id"].(string)
	return strings.TrimSpace(value)
}

func (h *Handler) refreshAuthProxyReferences(c *gin.Context, id string, registry *proxyregistry.Registry) error {
	if h == nil || h.authManager == nil {
		return nil
	}
	proxyURL, errResolve := registry.ResolveURL(id)
	if errResolve != nil {
		return errResolve
	}
	for _, auth := range h.authManager.List() {
		if auth == nil || !strings.EqualFold(authProxyID(auth), id) {
			continue
		}
		auth.ProxyURL = proxyURL
		if auth.Metadata == nil {
			auth.Metadata = make(map[string]any)
		}
		if proxyURL == "" {
			delete(auth.Metadata, "proxy_url")
		} else {
			auth.Metadata["proxy_url"] = proxyURL
		}
		if _, errUpdate := h.authManager.Update(c.Request.Context(), auth); errUpdate != nil {
			return fmt.Errorf("refresh auth %s: %w", auth.ID, errUpdate)
		}
	}
	return nil
}
