// Package proxyregistry stores reusable upstream proxy accounts.
package proxyregistry

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/util"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/proxyutil"
)

const storageFileName = ".proxy-accounts"

// ProxyAccount is a reusable upstream proxy definition.
type ProxyAccount struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Protocol      string     `json:"protocol"`
	Host          string     `json:"host"`
	Port          int        `json:"port"`
	Username      string     `json:"username,omitempty"`
	Password      string     `json:"password,omitempty"`
	Status        string     `json:"status"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
	FallbackMode  string     `json:"fallback_mode,omitempty"`
	BackupProxyID string     `json:"backup_proxy_id,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// Public returns the account fields safe to send to the management UI.
func (p ProxyAccount) Public() ProxyAccount {
	p.Password = ""
	return p
}

// URL builds the proxy URL consumed by the existing transport implementation.
func (p ProxyAccount) URL() (string, error) {
	if err := validateAccount(&p); err != nil {
		return "", err
	}
	u := &url.URL{Scheme: strings.ToLower(strings.TrimSpace(p.Protocol)), Host: net.JoinHostPort(p.Host, strconv.Itoa(p.Port))}
	if p.Username != "" {
		if p.Password != "" {
			u.User = url.UserPassword(p.Username, p.Password)
		} else {
			u.User = url.User(p.Username)
		}
	}
	return u.String(), nil
}

// Registry is a concurrency-safe, file-backed proxy account registry.
type Registry struct {
	mu       sync.RWMutex
	path     string
	loaded   bool
	loadErr  error
	accounts map[string]ProxyAccount
}

// New creates a registry backed by path.
func New(path string) *Registry {
	return &Registry{path: strings.TrimSpace(path), accounts: make(map[string]ProxyAccount)}
}

func (r *Registry) ensureLoadedLocked() error {
	if r.loaded {
		return r.loadErr
	}
	r.loaded = true
	if r.path == "" {
		return nil
	}
	raw, errRead := os.ReadFile(r.path)
	if errors.Is(errRead, os.ErrNotExist) {
		return nil
	}
	if errRead != nil {
		r.loadErr = fmt.Errorf("read proxy registry: %w", errRead)
		return r.loadErr
	}
	var records []ProxyAccount
	if errUnmarshal := json.Unmarshal(raw, &records); errUnmarshal != nil {
		r.loadErr = fmt.Errorf("decode proxy registry: %w", errUnmarshal)
		return r.loadErr
	}
	for i := range records {
		if strings.TrimSpace(records[i].ID) == "" {
			continue
		}
		if errNormalize := normalizeAccount(&records[i]); errNormalize != nil {
			return fmt.Errorf("invalid proxy account %s: %w", records[i].ID, errNormalize)
		}
		r.accounts[records[i].ID] = records[i]
	}
	return nil
}

func (r *Registry) saveLocked() error {
	if r.path == "" {
		return fmt.Errorf("proxy registry path is empty")
	}
	items := make([]ProxyAccount, 0, len(r.accounts))
	for _, item := range r.accounts {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	raw, errMarshal := json.MarshalIndent(items, "", "  ")
	if errMarshal != nil {
		return fmt.Errorf("encode proxy registry: %w", errMarshal)
	}
	if errMkdir := os.MkdirAll(filepath.Dir(r.path), 0o700); errMkdir != nil {
		return fmt.Errorf("create proxy registry directory: %w", errMkdir)
	}
	tmp, errTemp := os.CreateTemp(filepath.Dir(r.path), ".proxy-accounts-*.tmp")
	if errTemp != nil {
		return fmt.Errorf("create proxy registry temporary file: %w", errTemp)
	}
	tmpName := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}()
	if errChmod := tmp.Chmod(0o600); errChmod != nil {
		return fmt.Errorf("set proxy registry permissions: %w", errChmod)
	}
	if _, errWrite := tmp.Write(raw); errWrite != nil {
		return fmt.Errorf("write proxy registry: %w", errWrite)
	}
	if errClose := tmp.Close(); errClose != nil {
		return fmt.Errorf("close proxy registry: %w", errClose)
	}
	if errRename := os.Rename(tmpName, r.path); errRename != nil {
		return fmt.Errorf("replace proxy registry: %w", errRename)
	}
	return nil
}

// List returns public proxy account records sorted by name.
func (r *Registry) List() ([]ProxyAccount, error) {
	if r == nil {
		return nil, fmt.Errorf("proxy registry is nil")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.ensureLoadedLocked(); err != nil {
		return nil, err
	}
	items := make([]ProxyAccount, 0, len(r.accounts))
	for _, account := range r.accounts {
		items = append(items, account.Public())
	}
	sort.Slice(items, func(i, j int) bool {
		if strings.EqualFold(items[i].Name, items[j].Name) {
			return items[i].ID < items[j].ID
		}
		return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
	})
	return items, nil
}

// Get returns a public account by ID.
func (r *Registry) Get(id string) (ProxyAccount, bool, error) {
	if r == nil {
		return ProxyAccount{}, false, fmt.Errorf("proxy registry is nil")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.ensureLoadedLocked(); err != nil {
		return ProxyAccount{}, false, err
	}
	account, ok := r.accounts[strings.TrimSpace(id)]
	if !ok {
		return ProxyAccount{}, false, nil
	}
	return account.Public(), true, nil
}

// Create validates and persists a new proxy account.
func (r *Registry) Create(account ProxyAccount) (ProxyAccount, error) {
	if r == nil {
		return ProxyAccount{}, fmt.Errorf("proxy registry is nil")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.ensureLoadedLocked(); err != nil {
		return ProxyAccount{}, err
	}
	if strings.TrimSpace(account.ID) == "" {
		account.ID = uuid.NewString()
	}
	if _, exists := r.accounts[account.ID]; exists {
		return ProxyAccount{}, fmt.Errorf("proxy account %s already exists", account.ID)
	}
	now := time.Now().UTC()
	if account.CreatedAt.IsZero() {
		account.CreatedAt = now
	}
	account.UpdatedAt = now
	if err := normalizeAccount(&account); err != nil {
		return ProxyAccount{}, err
	}
	if err := r.validateReferencesLocked(account); err != nil {
		return ProxyAccount{}, err
	}
	r.accounts[account.ID] = account
	if err := r.saveLocked(); err != nil {
		delete(r.accounts, account.ID)
		return ProxyAccount{}, err
	}
	return account.Public(), nil
}

// Update validates and persists an existing proxy account.
func (r *Registry) Update(id string, account ProxyAccount) (ProxyAccount, error) {
	if r == nil {
		return ProxyAccount{}, fmt.Errorf("proxy registry is nil")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.ensureLoadedLocked(); err != nil {
		return ProxyAccount{}, err
	}
	id = strings.TrimSpace(id)
	old, ok := r.accounts[id]
	if !ok {
		return ProxyAccount{}, os.ErrNotExist
	}
	account.ID = id
	account.CreatedAt = old.CreatedAt
	if account.Password == "" {
		account.Password = old.Password
	}
	account.UpdatedAt = time.Now().UTC()
	if err := normalizeAccount(&account); err != nil {
		return ProxyAccount{}, err
	}
	if err := r.validateReferencesLocked(account); err != nil {
		return ProxyAccount{}, err
	}
	r.accounts[id] = account
	if err := r.saveLocked(); err != nil {
		r.accounts[id] = old
		return ProxyAccount{}, err
	}
	return account.Public(), nil
}

// Delete removes an account by ID.
func (r *Registry) Delete(id string) error {
	if r == nil {
		return fmt.Errorf("proxy registry is nil")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.ensureLoadedLocked(); err != nil {
		return err
	}
	id = strings.TrimSpace(id)
	old, ok := r.accounts[id]
	if !ok {
		return os.ErrNotExist
	}
	for _, account := range r.accounts {
		if account.ID != id && account.FallbackMode == "proxy" && strings.EqualFold(strings.TrimSpace(account.BackupProxyID), id) {
			return fmt.Errorf("proxy account is referenced as backup by %s", account.ID)
		}
	}
	delete(r.accounts, id)
	if err := r.saveLocked(); err != nil {
		r.accounts[id] = old
		return err
	}
	return nil
}

// ResolveURL resolves an account and its fallback chain to a transport URL.
func (r *Registry) ResolveURL(id string) (string, error) {
	if r == nil {
		return "", fmt.Errorf("proxy registry is nil")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.ensureLoadedLocked(); err != nil {
		return "", err
	}
	visited := make(map[string]struct{})
	for id = strings.TrimSpace(id); id != ""; {
		if _, exists := visited[id]; exists {
			return "", fmt.Errorf("proxy fallback cycle detected at %s", id)
		}
		visited[id] = struct{}{}
		account, ok := r.accounts[id]
		if !ok {
			return "", fmt.Errorf("proxy account %s not found", id)
		}
		active := strings.EqualFold(strings.TrimSpace(account.Status), "active") || strings.TrimSpace(account.Status) == ""
		expired := account.ExpiresAt != nil && !account.ExpiresAt.IsZero() && !time.Now().Before(account.ExpiresAt.UTC())
		if active && !expired {
			return account.URL()
		}
		if strings.EqualFold(strings.TrimSpace(account.FallbackMode), "direct") {
			return "direct", nil
		}
		if !strings.EqualFold(strings.TrimSpace(account.FallbackMode), "proxy") {
			return "", nil
		}
		id = account.BackupProxyID
	}
	return "", nil
}

// ResolveURLFromID resolves an ID using the configured global registry.
func ResolveURLFromID(id string) (string, error) {
	return Global().ResolveURL(id)
}

// FindByURL finds an account whose generated URL matches raw.
func (r *Registry) FindByURL(raw string) (string, bool, error) {
	want := strings.TrimSpace(raw)
	if want == "" || r == nil {
		return "", false, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.ensureLoadedLocked(); err != nil {
		return "", false, err
	}
	for id, account := range r.accounts {
		got, errURL := account.URL()
		if errURL == nil && got == want {
			return id, true, nil
		}
	}
	return "", false, nil
}

// ResolveMetadataProxy resolves the proxy_id metadata used by auth files.
// The second return value reports whether proxy_id was present, even when the
// selected account is disabled or has no usable fallback.
func ResolveMetadataProxy(metadata map[string]any) (string, bool) {
	if metadata == nil {
		return "", false
	}
	id, _ := metadata["proxy_id"].(string)
	id = strings.TrimSpace(id)
	if id == "" {
		return "", false
	}
	url, errResolve := Global().ResolveURL(id)
	if errResolve != nil {
		return "", true
	}
	return url, true
}

func normalizeAccount(account *ProxyAccount) error {
	if account == nil {
		return fmt.Errorf("proxy account is nil")
	}
	account.ID = strings.TrimSpace(account.ID)
	account.Name = strings.TrimSpace(account.Name)
	account.Protocol = strings.ToLower(strings.TrimSpace(account.Protocol))
	account.Host = strings.TrimSpace(account.Host)
	account.Username = strings.TrimSpace(account.Username)
	account.Status = strings.ToLower(strings.TrimSpace(account.Status))
	account.FallbackMode = strings.ToLower(strings.TrimSpace(account.FallbackMode))
	if account.Status == "" {
		account.Status = "active"
	}
	if account.FallbackMode == "" {
		account.FallbackMode = "none"
	}
	return validateAccount(account)
}

func validateAccount(account *ProxyAccount) error {
	if account == nil || strings.TrimSpace(account.ID) == "" {
		return fmt.Errorf("id is required")
	}
	if strings.TrimSpace(account.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if account.Protocol != "http" && account.Protocol != "https" && account.Protocol != "socks5" && account.Protocol != "socks5h" {
		return fmt.Errorf("unsupported protocol %q", account.Protocol)
	}
	if account.Host == "" || strings.ContainsAny(account.Host, "/?#@") {
		return fmt.Errorf("invalid host")
	}
	if account.Port < 1 || account.Port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535")
	}
	if account.Status != "active" && account.Status != "disabled" {
		return fmt.Errorf("status must be active or disabled")
	}
	if account.FallbackMode != "none" && account.FallbackMode != "direct" && account.FallbackMode != "proxy" {
		return fmt.Errorf("fallback_mode must be none, direct, or proxy")
	}
	if account.FallbackMode == "proxy" && strings.TrimSpace(account.BackupProxyID) == "" {
		return fmt.Errorf("backup_proxy_id is required for proxy fallback")
	}
	if _, errParse := proxyutil.Parse(fmt.Sprintf("%s://%s", account.Protocol, net.JoinHostPort(account.Host, strconv.Itoa(account.Port)))); errParse != nil {
		return fmt.Errorf("invalid proxy endpoint: %w", errParse)
	}
	return nil
}

func (r *Registry) validateReferencesLocked(account ProxyAccount) error {
	if account.FallbackMode != "proxy" {
		return nil
	}
	if account.BackupProxyID == account.ID {
		return fmt.Errorf("backup_proxy_id cannot reference itself")
	}
	visited := map[string]struct{}{account.ID: {}}
	for id := strings.TrimSpace(account.BackupProxyID); id != ""; {
		if _, exists := visited[id]; exists {
			return fmt.Errorf("proxy fallback cycle detected at %s", id)
		}
		visited[id] = struct{}{}
		next, exists := r.accounts[id]
		if !exists {
			return fmt.Errorf("backup proxy account %s not found", id)
		}
		if next.FallbackMode != "proxy" {
			return nil
		}
		id = strings.TrimSpace(next.BackupProxyID)
	}
	return nil
}

var globalState = struct {
	sync.RWMutex
	registry *Registry
	path     string
}{registry: New("")}

// ConfigureForAuthDir selects the registry associated with an auth directory.
func ConfigureForAuthDir(authDir string) *Registry {
	resolved, errResolve := util.ResolveAuthDir(authDir)
	if errResolve != nil || strings.TrimSpace(resolved) == "" {
		resolved = strings.TrimSpace(authDir)
	}
	path := filepath.Join(resolved, storageFileName)
	globalState.Lock()
	defer globalState.Unlock()
	if globalState.registry == nil || globalState.path != path {
		globalState.registry = New(path)
		globalState.path = path
	}
	return globalState.registry
}

// Global returns the currently configured registry.
func Global() *Registry {
	globalState.RLock()
	defer globalState.RUnlock()
	return globalState.registry
}
