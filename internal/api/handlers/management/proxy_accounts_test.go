package management

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/proxyregistry"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func TestProxyBindingPatchAndUnbind(t *testing.T) {
	manager := coreauth.NewManager(&memoryAuthStore{}, nil, nil)
	_, errRegister := manager.Register(context.Background(), &coreauth.Auth{ID: "test.json", FileName: "test.json", Provider: "claude", Metadata: map[string]any{"type": "claude"}})
	if errRegister != nil {
		t.Fatal(errRegister)
	}
	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: t.TempDir()}, manager)
	registry, errRegistry := h.proxyRegistry()
	if errRegistry != nil {
		t.Fatal(errRegistry)
	}
	_, errCreate := registry.Create(proxyregistry.ProxyAccount{ID: "p1", Name: "test", Protocol: "http", Host: "localhost", Port: 8080})
	if errCreate != nil {
		t.Fatal(errCreate)
	}
	patch := func(body string, status int) {
		t.Helper()
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodPatch, "/auth-files/fields", strings.NewReader(body))
		h.PatchAuthFileFields(ctx)
		if recorder.Code != status {
			t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
		}
	}
	patch(`{"name":"test.json","proxy_id":"missing"}`, http.StatusBadRequest)
	patch(`{"name":"test.json","proxy_id":42}`, http.StatusBadRequest)
	patch(`{"name":"test.json","proxy_id":"p1"}`, http.StatusOK)
	auth, _ := manager.GetByID("test.json")
	if auth.ProxyURL != "http://localhost:8080" {
		t.Fatalf("bound URL=%s", auth.ProxyURL)
	}
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	if errDelete := h.deleteProxyAccount(registry, ctx, "p1"); errDelete == nil {
		t.Fatal("deleted bound proxy")
	}
	patch(`{"name":"test.json","proxy_id":"","proxy_url":"http://manual:8081"}`, http.StatusOK)
	auth, _ = manager.GetByID("test.json")
	if auth.ProxyURL != "http://manual:8081" {
		t.Fatalf("manual URL=%s", auth.ProxyURL)
	}
}
