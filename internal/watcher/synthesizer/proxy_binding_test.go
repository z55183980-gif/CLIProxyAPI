package synthesizer

import (
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/proxyregistry"
	"path/filepath"
	"testing"
	"time"
)

func TestProxyBindingSurvivesFileReload(t *testing.T) {
	dir := t.TempDir()
	registry := proxyregistry.ConfigureForAuthDir(dir)
	_, errCreate := registry.Create(proxyregistry.ProxyAccount{ID: "p1", Name: "test", Protocol: "http", Host: "localhost", Port: 8080})
	if errCreate != nil {
		t.Fatal(errCreate)
	}
	ctx := &SynthesisContext{AuthDir: dir, Config: &config.Config{}, Now: time.Now(), IDGenerator: NewStableIDGenerator()}
	files, errSynthesize := synthesizeFileAuths(ctx, filepath.Join(dir, "test.json"), []byte(`{"type":"claude","proxy_id":"p1","proxy_url":"http://stale:1"}`))
	if errSynthesize != nil {
		t.Fatal(errSynthesize)
	}
	if len(files) != 1 || files[0].ProxyURL != "http://localhost:8080" {
		t.Fatalf("binding was not resolved: %+v", files)
	}
}
