package management

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/proxyregistry"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
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
	if h.authManager != nil {
		for _, auth := range h.authManager.List() {
			if auth != nil && strings.EqualFold(authProxyID(auth), id) {
				c.JSON(http.StatusConflict, gin.H{"error": "proxy account is still referenced by an auth file"})
				return
			}
		}
	}
	if errDelete := registry.Delete(id); errDelete != nil {
		if errors.Is(errDelete, os.ErrNotExist) {
			c.JSON(http.StatusNotFound, gin.H{"error": "proxy account not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": errDelete.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
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
