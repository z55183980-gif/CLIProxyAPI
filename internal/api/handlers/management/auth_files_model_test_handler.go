package management

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
	"github.com/tidwall/gjson"
)

// TestAuthFileModel executes a short generation pinned to the selected credential.
func (h *Handler) TestAuthFileModel(c *gin.Context) {
	var body struct {
		Name      string `json:"name"`
		AuthIndex string `json:"auth_index"`
		Model     string `json:"model"`
	}
	if errBind := c.ShouldBindJSON(&body); errBind != nil || strings.TrimSpace(body.Name) == "" || strings.TrimSpace(body.AuthIndex) == "" || strings.TrimSpace(body.Model) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name, auth_index and model are required"})
		return
	}
	if h == nil || h.authManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "auth manager unavailable"})
		return
	}
	auth, found := h.lookupAuthFile(body.Name, body.AuthIndex)
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "auth file not found"})
		return
	}
	if auth.Disabled || auth.Status == coreauth.StatusDisabled {
		c.JSON(http.StatusConflict, gin.H{"error": "auth file is disabled"})
		return
	}
	body.Model = strings.TrimSpace(body.Model)
	allowed := false
	for _, model := range registry.GetGlobalRegistry().GetModelsForClient(auth.ID) {
		if model != nil && model.ID == body.Model {
			allowed = true
			break
		}
	}
	if !allowed {
		c.JSON(http.StatusBadRequest, gin.H{"error": "model is not available for this auth file"})
		return
	}
	payload, errMarshal := json.Marshal(gin.H{
		"model": body.Model, "stream": false, "max_tokens": 64,
		"messages": []gin.H{{"role": "user", "content": "Reply with OK."}},
	})
	if errMarshal != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to build model test"})
		return
	}
	started := time.Now()
	response, errExecute := h.authManager.Execute(c.Request.Context(), []string{auth.Provider}, executor.Request{
		Model: body.Model, Payload: payload, Format: translator.FormatOpenAI,
	}, executor.Options{
		SourceFormat: translator.FormatOpenAI, OriginalRequest: payload,
		Metadata: map[string]any{executor.PinnedAuthMetadataKey: auth.ID},
	})
	result := gin.H{"model": body.Model, "latency_ms": time.Since(started).Milliseconds(), "success": false}
	if errExecute != nil {
		// Upstream authorization failures must not trigger management logout.
		status := http.StatusBadGateway
		var statusError interface{ StatusCode() int }
		if errors.As(errExecute, &statusError) && statusError.StatusCode() > 0 {
			status = statusError.StatusCode()
		}
		result["status_code"] = status
		result["error"] = http.StatusText(status)
		if result["error"] == "" {
			result["error"] = "Model request failed"
		}
		c.JSON(http.StatusOK, result)
		return
	}
	if !gjson.ValidBytes(response.Payload) || !gjson.GetBytes(response.Payload, "choices.0").Exists() || gjson.GetBytes(response.Payload, "error").Exists() {
		result["error"] = "No valid model completion received"
		c.JSON(http.StatusOK, result)
		return
	}
	result["success"] = true
	c.JSON(http.StatusOK, result)
}
