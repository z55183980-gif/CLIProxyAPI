package proxyregistry

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRegistryCreateResolveAndReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".proxy-accounts")
	registry := New(path)
	created, errCreate := registry.Create(ProxyAccount{
		ID:       "proxy-main",
		Name:     "Main",
		Protocol: "http",
		Host:     "127.0.0.1",
		Port:     8080,
		Username: "user",
		Password: "secret",
	})
	if errCreate != nil {
		t.Fatalf("Create() error = %v", errCreate)
	}
	if created.Password != "" {
		t.Fatal("Create() returned a password in the public record")
	}
	resolved, errResolve := registry.ResolveURL("proxy-main")
	if errResolve != nil || resolved != "http://user:secret@127.0.0.1:8080" {
		t.Fatalf("ResolveURL() = %q, %v", resolved, errResolve)
	}

	reloaded := New(path)
	loaded, found, errGet := reloaded.Get("proxy-main")
	if errGet != nil || !found || loaded.Name != "Main" {
		t.Fatalf("Get() = %#v, found=%t, err=%v", loaded, found, errGet)
	}
	if loaded.Password != "" {
		t.Fatal("Get() returned a password in the public record")
	}
}

func TestRegistryFallbackAndReferenceValidation(t *testing.T) {
	registry := New(filepath.Join(t.TempDir(), ".proxy-accounts"))
	if _, errCreate := registry.Create(ProxyAccount{ID: "backup", Name: "Backup", Protocol: "socks5", Host: "localhost", Port: 1080}); errCreate != nil {
		t.Fatalf("create backup: %v", errCreate)
	}
	if _, errCreate := registry.Create(ProxyAccount{ID: "main", Name: "Main", Protocol: "http", Host: "localhost", Port: 8080, FallbackMode: "proxy", BackupProxyID: "backup"}); errCreate != nil {
		t.Fatalf("create main: %v", errCreate)
	}
	resolved, errResolve := registry.ResolveURL("main")
	if errResolve != nil || resolved != "http://localhost:8080" {
		t.Fatalf("ResolveURL() = %q, %v", resolved, errResolve)
	}
	if _, errCreate := registry.Create(ProxyAccount{ID: "cycle", Name: "Cycle", Protocol: "http", Host: "localhost", Port: 8081, FallbackMode: "proxy", BackupProxyID: "cycle"}); errCreate == nil {
		t.Fatal("expected self-reference validation error")
	}
	if errDelete := registry.Delete("missing"); !os.IsNotExist(errDelete) {
		t.Fatalf("Delete(missing) error = %v, want os.ErrNotExist", errDelete)
	}
	if errDelete := registry.Delete("backup"); errDelete == nil {
		t.Fatal("expected referenced backup deletion to fail")
	}
}
