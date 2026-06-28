package accounts

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type User struct {
	UserID    string `json:"user_id,omitempty"`
	ID        string `json:"id,omitempty"`
	Email     string `json:"email,omitempty"`
	Name      string `json:"name,omitempty"`
	Nickname  string `json:"nickname,omitempty"`
	Avatar    string `json:"avatar,omitempty"`
	AvatarURL string `json:"avatar_url,omitempty"`
}

type Account struct {
	ID                string     `json:"id"`
	RegistrationOrder int        `json:"registrationOrder"`
	User              User       `json:"user"`
	ZCodeJWTToken     string     `json:"zcodeJwtToken"`
	ZAIAcccessToken   string     `json:"zaiAccessToken,omitempty"`
	CodingPlanAPIKey  string     `json:"codingPlanApiKey,omitempty"`
	TokenExpiresAt    *time.Time `json:"tokenExpiresAt,omitempty"`
	CreatedAt         time.Time  `json:"createdAt"`
	UpdatedAt         time.Time  `json:"updatedAt"`
}

type storeData struct {
	Version         int       `json:"version"`
	ActiveAccountID string    `json:"activeAccountId,omitempty"`
	Accounts        []Account `json:"accounts"`
}

type envelope struct {
	Version   int    `json:"version"`
	Algorithm string `json:"algorithm"`
	IV        string `json:"iv"`
	Tag       string `json:"tag"`
	Data      string `json:"data"`
}

type PublicAccount struct {
	ID                  string     `json:"id"`
	RegistrationOrder   int        `json:"registrationOrder"`
	Label               string     `json:"label"`
	User                PublicUser `json:"user"`
	QueuePosition       int        `json:"queuePosition,omitempty"`
	Active              bool       `json:"active"`
	CreatedAt           time.Time  `json:"createdAt"`
	UpdatedAt           time.Time  `json:"updatedAt"`
	HasZCodeJWTToken    bool       `json:"hasZcodeJwtToken"`
	HasZAIAccessToken   bool       `json:"hasZaiAccessToken"`
	HasCodingPlanAPIKey bool       `json:"hasCodingPlanApiKey"`
	TokenExpiresAt      *time.Time `json:"tokenExpiresAt"`
	TokenExpired        *bool      `json:"tokenExpired"`
}

type PublicUser struct {
	ID     string `json:"id,omitempty"`
	Email  string `json:"email,omitempty"`
	Name   string `json:"name,omitempty"`
	Avatar string `json:"avatar,omitempty"`
}

type Store struct {
	path   string
	secret string
	mu     sync.RWMutex
	data   storeData
}

func NewStore(path, secret string) (*Store, error) {
	store := &Store{path: path, secret: secret, data: storeData{Version: 1, Accounts: []Account{}}}
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Store) Accounts() []Account {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Account, len(s.data.Accounts))
	copy(out, s.data.Accounts)
	sort.SliceStable(out, func(i, j int) bool { return out[i].RegistrationOrder < out[j].RegistrationOrder })
	return out
}

func (s *Store) Active() *Account {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, account := range s.data.Accounts {
		if account.ID == s.data.ActiveAccountID {
			copy := account
			return &copy
		}
	}
	return nil
}

func (s *Store) Get(id string) *Account {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, account := range s.data.Accounts {
		if account.ID == id {
			copy := account
			return &copy
		}
	}
	return nil
}

func (s *Store) Public() (string, []PublicAccount) {
	accounts := s.Accounts()
	s.mu.RLock()
	activeID := s.data.ActiveAccountID
	s.mu.RUnlock()
	out := make([]PublicAccount, 0, len(accounts))
	for index, account := range accounts {
		item := Sanitize(account)
		item.QueuePosition = index + 1
		item.Active = account.ID == activeID
		out = append(out, item)
	}
	return activeID, out
}

func (s *Store) Upsert(userData User, jwt, accessToken string) (PublicAccount, error) {
	id := first(userData.UserID, userData.ID)
	if id == "" || strings.TrimSpace(jwt) == "" {
		return PublicAccount{}, errors.New("OAuth response did not include user_id and ZCode token")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	order := 0
	index := -1
	for current, account := range s.data.Accounts {
		if account.RegistrationOrder > order {
			order = account.RegistrationOrder
		}
		if account.ID == id {
			index = current
		}
	}
	account := Account{ID: id, RegistrationOrder: order + 1, User: userData, ZCodeJWTToken: jwt, ZAIAcccessToken: accessToken, TokenExpiresAt: jwtExpiry(jwt), CreatedAt: now, UpdatedAt: now}
	if index >= 0 {
		existing := s.data.Accounts[index]
		account.RegistrationOrder = existing.RegistrationOrder
		account.CreatedAt = existing.CreatedAt
		if account.ZAIAcccessToken == "" {
			account.ZAIAcccessToken = existing.ZAIAcccessToken
		}
		if account.CodingPlanAPIKey == "" {
			account.CodingPlanAPIKey = existing.CodingPlanAPIKey
		}
		s.data.Accounts[index] = account
	} else {
		s.data.Accounts = append(s.data.Accounts, account)
	}
	if s.data.ActiveAccountID == "" {
		s.data.ActiveAccountID = id
	}
	if err := s.saveLocked(); err != nil {
		return PublicAccount{}, err
	}
	return Sanitize(account), nil
}

func (s *Store) UpdateCodingPlanAPIKey(id, apiKey string) (*Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for index := range s.data.Accounts {
		if s.data.Accounts[index].ID != id {
			continue
		}
		s.data.Accounts[index].CodingPlanAPIKey = strings.TrimSpace(apiKey)
		s.data.Accounts[index].UpdatedAt = time.Now().UTC()
		if err := s.saveLocked(); err != nil {
			return nil, err
		}
		account := s.data.Accounts[index]
		return &account, nil
	}
	return nil, nil
}

func (s *Store) Activate(id string) (*PublicAccount, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, account := range s.data.Accounts {
		if account.ID == id {
			s.data.ActiveAccountID = id
			if err := s.saveLocked(); err != nil {
				return nil, err
			}
			public := Sanitize(account)
			public.Active = true
			return &public, nil
		}
	}
	return nil, nil
}

func (s *Store) Reorder(ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(ids) != len(s.data.Accounts) {
		return errors.New("account order must include every saved account")
	}
	positions := make(map[string]int, len(ids))
	for index, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			return errors.New("account order contains an empty id")
		}
		if _, exists := positions[id]; exists {
			return errors.New("account order contains duplicate ids")
		}
		positions[id] = index + 1
	}
	for index := range s.data.Accounts {
		position, exists := positions[s.data.Accounts[index].ID]
		if !exists {
			return errors.New("account order contains an unknown set of accounts")
		}
		s.data.Accounts[index].RegistrationOrder = position
	}
	sort.SliceStable(s.data.Accounts, func(i, j int) bool {
		return s.data.Accounts[i].RegistrationOrder < s.data.Accounts[j].RegistrationOrder
	})
	return s.saveLocked()
}

func (s *Store) Remove(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	found := false
	for _, account := range s.data.Accounts {
		if account.ID == id {
			found = true
			break
		}
	}
	if !found {
		return false, nil
	}
	if err := s.backupLocked(); err != nil {
		return false, err
	}
	filtered := s.data.Accounts[:0]
	for _, account := range s.data.Accounts {
		if account.ID != id {
			filtered = append(filtered, account)
		}
	}
	s.data.Accounts = filtered
	if s.data.ActiveAccountID == id {
		s.data.ActiveAccountID = ""
		if len(filtered) > 0 {
			s.data.ActiveAccountID = filtered[0].ID
		}
	}
	return true, s.saveLocked()
}

func (s *Store) backupLocked() error {
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	dir := filepath.Join(filepath.Dir(s.path), "backups")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	name := fmt.Sprintf("%s.%s.bak", filepath.Base(s.path), time.Now().UTC().Format("20060102T150405.000000000Z"))
	return os.WriteFile(filepath.Join(dir, name), raw, 0o600)
}

func Sanitize(account Account) PublicAccount {
	var expired *bool
	if account.TokenExpiresAt != nil {
		value := !account.TokenExpiresAt.After(time.Now())
		expired = &value
	}
	return PublicAccount{
		ID: account.ID, RegistrationOrder: account.RegistrationOrder, Label: fmt.Sprintf("Conta %d", account.RegistrationOrder),
		User:      PublicUser{ID: first(account.User.UserID, account.User.ID), Email: account.User.Email, Name: first(account.User.Name, account.User.Nickname), Avatar: first(account.User.Avatar, account.User.AvatarURL)},
		CreatedAt: account.CreatedAt, UpdatedAt: account.UpdatedAt, HasZCodeJWTToken: account.ZCodeJWTToken != "",
		HasZAIAccessToken: account.ZAIAcccessToken != "", HasCodingPlanAPIKey: account.CodingPlanAPIKey != "",
		TokenExpiresAt: account.TokenExpiresAt, TokenExpired: expired,
	}
}

func Authorization(account *Account) string {
	if account == nil || account.ZCodeJWTToken == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(account.ZCodeJWTToken), "bearer ") {
		return account.ZCodeJWTToken
	}
	return "Bearer " + account.ZCodeJWTToken
}

func (s *Store) load() error {
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var envelope envelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return err
	}
	plaintext, err := s.decrypt(envelope)
	needsResave := false
	if err != nil {
		if s.secret != "" {
			return fmt.Errorf("could not decrypt credential store %q: %w", s.path, err)
		}
		plaintext, err = s.decryptWithKey(envelope, defaultKey("kimiproxyplus"))
		if err != nil {
			return fmt.Errorf("could not decrypt credential store %q: %w", s.path, err)
		}
		needsResave = true
	}
	if err := json.Unmarshal(plaintext, &s.data); err != nil {
		return err
	}
	next := 0
	for index := range s.data.Accounts {
		if s.data.Accounts[index].RegistrationOrder == 0 {
			next++
			s.data.Accounts[index].RegistrationOrder = next
			needsResave = true
		} else if s.data.Accounts[index].RegistrationOrder > next {
			next = s.data.Accounts[index].RegistrationOrder
		}
	}
	if needsResave {
		return s.saveLocked()
	}
	return nil
}

func (s *Store) saveLocked() error {
	raw, err := json.Marshal(s.data)
	if err != nil {
		return err
	}
	envelope, err := s.encrypt(raw)
	if err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.%d.tmp", s.path, os.Getpid())
	if err := os.WriteFile(tmp, append(encoded, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *Store) encrypt(plaintext []byte) (envelope, error) {
	block, err := aes.NewCipher(s.key())
	if err != nil {
		return envelope{}, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return envelope{}, err
	}
	iv := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(iv); err != nil {
		return envelope{}, err
	}
	sealed := gcm.Seal(nil, iv, plaintext, nil)
	tagSize := gcm.Overhead()
	return envelope{Version: 1, Algorithm: "aes-256-gcm", IV: base64.StdEncoding.EncodeToString(iv), Tag: base64.StdEncoding.EncodeToString(sealed[len(sealed)-tagSize:]), Data: base64.StdEncoding.EncodeToString(sealed[:len(sealed)-tagSize])}, nil
}

func (s *Store) decrypt(value envelope) ([]byte, error) {
	return s.decryptWithKey(value, s.key())
}

func (s *Store) decryptWithKey(value envelope, key []byte) ([]byte, error) {
	iv, err := base64.StdEncoding.DecodeString(value.IV)
	if err != nil {
		return nil, err
	}
	tag, err := base64.StdEncoding.DecodeString(value.Tag)
	if err != nil {
		return nil, err
	}
	data, err := base64.StdEncoding.DecodeString(value.Data)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return gcm.Open(nil, iv, append(data, tag...), nil)
}

func (s *Store) key() []byte {
	if s.secret != "" {
		sum := sha256.Sum256([]byte(s.secret))
		return sum[:]
	}
	return defaultKey("glm5.2proxy")
}

func defaultKey(namespace string) []byte {
	host, _ := os.Hostname()
	current, _ := user.Current()
	home, _ := os.UserHomeDir()
	username := ""
	if current != nil {
		username = current.Username
		if separator := strings.LastIndexAny(username, `\/`); separator >= 0 {
			username = username[separator+1:]
		}
	}
	secret := fmt.Sprintf("%s:%s:%s:%s", namespace, host, username, home)
	sum := sha256.Sum256([]byte(secret))
	return sum[:]
}

func jwtExpiry(token string) *time.Time {
	raw := strings.TrimSpace(strings.TrimPrefix(token, "Bearer "))
	parts := strings.Split(raw, ".")
	if len(parts) < 2 {
		return nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil
	}
	var value struct {
		Exp int64 `json:"exp"`
	}
	if json.Unmarshal(payload, &value) != nil || value.Exp <= 0 {
		return nil
	}
	expiry := time.Unix(value.Exp, 0).UTC()
	return &expiry
}

func first(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
