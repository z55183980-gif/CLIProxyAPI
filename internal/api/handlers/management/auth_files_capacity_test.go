package management

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	fileauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/auth"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func TestAuthFileCapacityPersistenceAndValidation(t *testing.T) {
	dir := t.TempDir()
	store := fileauth.NewFileTokenStore()
	store.SetBaseDir(dir)
	m := coreauth.NewManager(store, nil, nil)
	a := &coreauth.Auth{ID: "capacity.json", FileName: "capacity.json", Provider: "codex", Attributes: map[string]string{"path": filepath.Join(dir, "capacity.json")}, Metadata: map[string]any{"type": "codex"}}
	if _, err := m.Register(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: dir}, m)
	patch := func(body string) int {
		rec := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(rec)
		ctx.Request = httptest.NewRequest(http.MethodPatch, "/v0/management/auth-files/fields", strings.NewReader(body))
		h.PatchAuthFileFields(ctx)
		return rec.Code
	}
	if code := patch(`{"name":"capacity.json","concurrency":5,"priority":1}`); code != 200 {
		t.Fatalf("save status = %d", code)
	}
	loaded, err := store.List(context.Background())
	if err != nil || len(loaded) != 1 || coreauth.AuthCapacity(loaded[0]) != 5 {
		t.Fatalf("persisted capacity = %#v, %v", loaded, err)
	}
	current, _ := m.GetByID(a.ID)
	entry := h.buildAuthFileEntry(current)
	if entry["concurrency"] != 5 || entry["current_concurrency"] != 0 || entry["capacity_editable"] != true || entry["priority"] != 1 {
		t.Fatalf("entry = %#v", entry)
	}
	for _, value := range []string{`-1`, `1.5`, `true`, `1000001`, `{}`, `""`} {
		if code := patch(`{"name":"capacity.json","concurrency":` + value + `}`); code != 400 {
			t.Fatalf("accepted %s, status %d", value, code)
		}
	}
	if code := patch(`{"name":"capacity.json","concurrency.value":2}`); code != 400 {
		t.Fatalf("nested capacity status = %d", code)
	}
	raw, err := os.ReadFile(filepath.Join(dir, a.FileName))
	if err != nil {
		t.Fatal(err)
	}
	var metadata map[string]any
	if err := json.Unmarshal(raw, &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata["concurrency"] != float64(5) {
		t.Fatalf("invalid updates changed persisted limit: %v", metadata)
	}
	if code := patch(`{"name":"capacity.json","concurrency":0}`); code != 200 {
		t.Fatalf("unlimited status = %d", code)
	}
}
