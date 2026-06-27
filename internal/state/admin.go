package state

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type ThinkingSettings struct {
	Enabled      bool   `json:"enabled"`
	BudgetTokens int    `json:"budgetTokens"`
	Effort       string `json:"effort"`
}

type APIKey struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Prefix    string    `json:"prefix"`
	Hash      string    `json:"hash"`
	CreatedAt time.Time `json:"createdAt"`
}

type PublicAPIKey struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Prefix    string    `json:"prefix"`
	CreatedAt time.Time `json:"createdAt"`
}

type AdminSettings struct {
	Version         int                         `json:"version"`
	Port            int                         `json:"port"`
	APIEnabled      bool                        `json:"apiEnabled"`
	GlobalThinking  ThinkingSettings            `json:"globalThinking"`
	AccountThinking map[string]ThinkingSettings `json:"accountThinking"`
	APIKeys         []APIKey                    `json:"apiKeys"`
	Migrations      map[string]string           `json:"migrations,omitempty"`
}

type AdminStore struct {
	path string
	mu   sync.RWMutex
	data AdminSettings
}

func NewAdminStore(path string, defaultPort int, defaults ThinkingSettings) (*AdminStore, error) {
	store := &AdminStore{path: path}
	store.data = AdminSettings{Version: 2, Port: defaultPort, APIEnabled: true, GlobalThinking: defaults, AccountThinking: map[string]ThinkingSettings{}, APIKeys: []APIKey{}, Migrations: map[string]string{}}
	if raw, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(raw, &store.data); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if store.data.Port == 0 {
		store.data.Port = defaultPort
	}
	if store.data.Version < 2 {
		store.data.Version = 2
		store.data.APIEnabled = true
		if err := store.saveLocked(); err != nil {
			return nil, err
		}
	}
	if store.data.AccountThinking == nil {
		store.data.AccountThinking = map[string]ThinkingSettings{}
	}
	if store.data.Migrations == nil {
		store.data.Migrations = map[string]string{}
	}
	return store, nil
}

func (s *AdminStore) Snapshot() AdminSettings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return clone(s.data)
}

func (s *AdminStore) PublicSnapshot() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	keys := make([]PublicAPIKey, 0, len(s.data.APIKeys))
	for _, key := range s.data.APIKeys {
		keys = append(keys, PublicAPIKey{ID: key.ID, Name: key.Name, Prefix: key.Prefix, CreatedAt: key.CreatedAt})
	}
	return map[string]any{"version": s.data.Version, "port": s.data.Port, "apiEnabled": s.data.APIEnabled, "globalThinking": s.data.GlobalThinking, "accountThinking": s.data.AccountThinking, "apiKeys": keys, "apiKeyRequired": len(keys) > 0}
}

func (s *AdminStore) MigrationCompleted(key string) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.data.Migrations[key]
	return ok
}

func (s *AdminStore) MarkMigrationCompleted(key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("migration key is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data.Migrations == nil {
		s.data.Migrations = map[string]string{}
	}
	s.data.Migrations[key] = time.Now().UTC().Format(time.RFC3339)
	return s.saveLocked()
}

func (s *AdminStore) SetPort(port int) error {
	if port < 1 || port > 65535 {
		return errors.New("port must be between 1 and 65535")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Port = port
	return s.saveLocked()
}

func (s *AdminStore) SetAPIEnabled(enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.APIEnabled = enabled
	return s.saveLocked()
}

func (s *AdminStore) SetGlobalThinking(value ThinkingSettings) error {
	if err := validateThinking(value); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.GlobalThinking = value
	return s.saveLocked()
}

func (s *AdminStore) SetAccountThinking(accountID string, value *ThinkingSettings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if value == nil {
		delete(s.data.AccountThinking, accountID)
		return s.saveLocked()
	}
	if err := validateThinking(*value); err != nil {
		return err
	}
	s.data.AccountThinking[accountID] = *value
	return s.saveLocked()
}

func (s *AdminStore) ThinkingFor(accountID string) ThinkingSettings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if value, ok := s.data.AccountThinking[accountID]; ok {
		return value
	}
	return s.data.GlobalThinking
}

func (s *AdminStore) CreateAPIKey(name string) (APIKey, string, error) {
	secretBytes := make([]byte, 32)
	if _, err := rand.Read(secretBytes); err != nil {
		return APIKey{}, "", err
	}
	secret := "zkp_" + hex.EncodeToString(secretBytes)
	key := APIKey{ID: randomID(), Name: strings.TrimSpace(name), Prefix: secret[:12], Hash: hash(secret), CreatedAt: time.Now().UTC()}
	if key.Name == "" {
		key.Name = "API key"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.APIKeys = append(s.data.APIKeys, key)
	return key, secret, s.saveLocked()
}

func PublicKey(key APIKey) PublicAPIKey {
	return PublicAPIKey{ID: key.ID, Name: key.Name, Prefix: key.Prefix, CreatedAt: key.CreatedAt}
}

func (s *AdminStore) DeleteAPIKey(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	before := len(s.data.APIKeys)
	filtered := s.data.APIKeys[:0]
	for _, key := range s.data.APIKeys {
		if key.ID != id {
			filtered = append(filtered, key)
		}
	}
	s.data.APIKeys = filtered
	if len(filtered) == before {
		return false
	}
	_ = s.saveLocked()
	return true
}

func (s *AdminStore) ValidateAPIKey(secret string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.data.APIKeys) == 0 {
		return true
	}
	value := hash(apiKeySecret(secret))
	for _, key := range s.data.APIKeys {
		if key.Hash == value {
			return true
		}
	}
	return false
}

func apiKeySecret(header string) string {
	value := strings.TrimSpace(header)
	fields := strings.Fields(value)
	if len(fields) == 2 && strings.EqualFold(fields[0], "Bearer") {
		return fields[1]
	}
	return value
}

func (s *AdminStore) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func validateThinking(value ThinkingSettings) error {
	if value.BudgetTokens < 0 || value.BudgetTokens > 64000 {
		return errors.New("budgetTokens must be between 0 and 64000")
	}
	switch value.Effort {
	case "none", "low", "medium", "high", "max":
		return nil
	default:
		return errors.New("effort must be one of none, low, medium, high, max")
	}
}

func hash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func randomID() string {
	value := make([]byte, 12)
	_, _ = rand.Read(value)
	return hex.EncodeToString(value)
}

func clone(value AdminSettings) AdminSettings {
	raw, _ := json.Marshal(value)
	var out AdminSettings
	_ = json.Unmarshal(raw, &out)
	return out
}
