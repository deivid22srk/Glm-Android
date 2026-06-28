package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"glm5.2proxy/internal/accounts"
	"glm5.2proxy/internal/config"
)

type Flow struct {
	FlowID          string     `json:"flowId"`
	AuthorizeURL    string     `json:"authorizeUrl"`
	ExpiresAt       *time.Time `json:"expiresAt"`
	PollIntervalSec int        `json:"pollIntervalSec"`
	Status          string     `json:"status"`
	Error           string     `json:"error,omitempty"`
	redirectURI     string
	account         *accounts.PublicAccount
}

type Service struct {
	cfg      config.Config
	accounts *accounts.Store
	client   *http.Client
	mu       sync.RWMutex
	flows    map[string]*Flow
}

func New(cfg config.Config, store *accounts.Store) *Service {
	// The OAuth backend (zcode.z.ai) closes idle connections aggressively, so
	// reusing a pooled connection on the second login stalls until the socket
	// times out or gets reset. Disable keep-alives to force a fresh connection
	// on every OAuth request, which keeps repeated logins snappy.
	transport := &http.Transport{
		DisableKeepAlives: true,
		IdleConnTimeout:   30 * time.Second,
	}
	return &Service{cfg: cfg, accounts: store, client: &http.Client{Timeout: 15 * time.Second, Transport: transport}, flows: map[string]*Flow{}}
}

func (s *Service) Start(ctx context.Context, callbackBaseURL string) (Flow, error) {
	_ = ctx
	state := randomHex(32)
	redirectURI := strings.TrimSpace(s.cfg.OAuthRedirectURI)
	if redirectURI == "" {
		baseURL := strings.TrimRight(callbackBaseURL, "/")
		if baseURL == "" {
			return Flow{}, errors.New("OAuth callback base URL is required")
		}
		redirectURI = baseURL + "/api/admin/auth/login/callback"
	}
	authorizeURL, err := s.buildAuthorizeURL(state, redirectURI)
	if err != nil {
		return Flow{}, err
	}
	timeout := s.cfg.OAuthFlowTimeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	expires := time.Now().UTC().Add(timeout)
	flow := Flow{FlowID: state, AuthorizeURL: authorizeURL, ExpiresAt: &expires, PollIntervalSec: 1, Status: "pending", redirectURI: redirectURI}
	s.mu.Lock()
	s.flows[flow.FlowID] = &flow
	s.mu.Unlock()
	return publicFlow(flow), nil
}

func (s *Service) Poll(ctx context.Context, flowID string) (map[string]any, error) {
	_ = ctx
	s.mu.Lock()
	flow := s.flows[flowID]
	if flow == nil {
		s.mu.Unlock()
		return nil, errors.New("unknown OAuth flow; start login again")
	}
	if flow.Status == "pending" && flow.ExpiresAt != nil && !time.Now().Before(*flow.ExpiresAt) {
		flow.Status = "failed"
		flow.Error = "OAuth login expired; start login again"
	}
	flowCopy := *flow
	if flowCopy.Status == "ready" || flowCopy.Status == "failed" {
		delete(s.flows, flowID)
	}
	s.mu.Unlock()
	result := map[string]any{"flowId": flowCopy.FlowID, "authorizeUrl": flowCopy.AuthorizeURL, "expiresAt": flowCopy.ExpiresAt, "pollIntervalSec": flowCopy.PollIntervalSec, "status": flowCopy.Status}
	if flowCopy.Error != "" {
		result["error"] = flowCopy.Error
	}
	if flowCopy.account != nil {
		result["account"] = *flowCopy.account
	}
	return result, nil
}

func (s *Service) Complete(ctx context.Context, state, code, callbackError string) (map[string]any, error) {
	state = strings.TrimSpace(state)
	code = strings.TrimSpace(code)
	s.mu.RLock()
	flow := s.flows[state]
	s.mu.RUnlock()
	if flow == nil {
		err := "OAuth callback state is unknown or expired; start login again"
		s.failPendingFlows(err)
		return nil, errors.New(err)
	}
	if callbackError != "" {
		s.failFlow(flow, callbackError)
		return nil, errors.New(callbackError)
	}
	if code == "" {
		err := "OAuth callback did not include code"
		s.failFlow(flow, err)
		return nil, errors.New(err)
	}
	tokenSet, err := s.exchangeCode(ctx, *flow, code)
	if err != nil {
		s.failFlow(flow, err.Error())
		return nil, err
	}
	user := tokenSet.User
	if first(user.UserID, user.ID) == "" {
		if fetched, err := s.fetchUserInfo(ctx, tokenSet.ZAIAccessToken); err == nil {
			user = fetched
		}
	}
	if first(user.UserID, user.ID) == "" {
		err := errors.New("OAuth response did not include user profile")
		s.failFlow(flow, err.Error())
		return nil, err
	}
	account, err := s.accounts.Upsert(user, tokenSet.ZCodeJWTToken, tokenSet.ZAIAccessToken)
	if err != nil {
		s.failFlow(flow, err.Error())
		return nil, err
	}
	s.mu.Lock()
	flow.Status = "ready"
	flow.Error = ""
	flow.account = &account
	s.mu.Unlock()
	return map[string]any{"flowId": flow.FlowID, "status": "ready", "account": account}, nil
}

type tokenSet struct {
	ZCodeJWTToken  string
	ZAIAccessToken string
	User           accounts.User
}

func (s *Service) buildAuthorizeURL(state, redirectURI string) (string, error) {
	base := strings.TrimSpace(s.cfg.OAuthAuthorizeURL)
	if base == "" {
		return "", errors.New("OAuth authorize URL is not configured")
	}
	clientID := strings.TrimSpace(s.cfg.OAuthClientID)
	if clientID == "" {
		return "", errors.New("OAuth client ID is not configured")
	}
	parsed, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	values := parsed.Query()
	values.Set("redirect_uri", redirectURI)
	values.Set("response_type", "code")
	values.Set("client_id", clientID)
	values.Set("state", state)
	parsed.RawQuery = values.Encode()
	return parsed.String(), nil
}

func (s *Service) exchangeCode(ctx context.Context, flow Flow, code string) (tokenSet, error) {
	var payload struct {
		Code    int    `json:"code"`
		Msg     string `json:"msg"`
		Message string `json:"message"`
		Data    struct {
			Token     string          `json:"token"`
			ExpiresIn int64           `json:"expires_in"`
			User      json.RawMessage `json:"user"`
			ZAI       struct {
				AccessToken  string `json:"access_token"`
				AccessToken2 string `json:"accessToken"`
			} `json:"zai"`
		} `json:"data"`
	}
	body := map[string]any{"provider": first(s.cfg.OAuthProvider, "zai"), "code": code, "redirect_uri": flow.redirectURI, "state": flow.FlowID}
	if err := s.requestJSON(ctx, http.MethodPost, s.cfg.OAuthTokenURL, "", body, &payload); err != nil {
		return tokenSet{}, err
	}
	if payload.Code != 0 {
		return tokenSet{}, fmt.Errorf("OAuth token exchange failed: %s", first(payload.Msg, payload.Message, fmt.Sprintf("business code %d", payload.Code)))
	}
	jwt := strings.TrimSpace(payload.Data.Token)
	oauthAccessToken := strings.TrimSpace(first(payload.Data.ZAI.AccessToken, payload.Data.ZAI.AccessToken2))
	if jwt == "" {
		return tokenSet{}, errors.New("OAuth token response is missing data.token")
	}
	if oauthAccessToken == "" {
		return tokenSet{}, errors.New("OAuth token response is missing data.zai.access_token")
	}
	businessToken, err := s.resolveZAIBusinessToken(ctx, oauthAccessToken)
	if err != nil {
		return tokenSet{}, err
	}
	return tokenSet{ZCodeJWTToken: jwt, ZAIAccessToken: businessToken, User: parseUser(payload.Data.User)}, nil
}

func (s *Service) fetchUserInfo(ctx context.Context, accessToken string) (accounts.User, error) {
	if strings.TrimSpace(accessToken) == "" || strings.TrimSpace(s.cfg.OAuthUserInfoURL) == "" {
		return accounts.User{}, errors.New("OAuth userinfo token or URL is missing")
	}
	var payload json.RawMessage
	if err := s.requestJSON(ctx, http.MethodGet, s.cfg.OAuthUserInfoURL, "Bearer "+accessToken, nil, &payload); err != nil {
		return accounts.User{}, err
	}
	user := parseUser(payload)
	if first(user.UserID, user.ID) == "" {
		var envelope struct {
			Data json.RawMessage `json:"data"`
		}
		if json.Unmarshal(payload, &envelope) == nil && len(bytes.TrimSpace(envelope.Data)) > 0 {
			user = parseUser(envelope.Data)
		}
	}
	if first(user.UserID, user.ID) == "" {
		return accounts.User{}, errors.New("OAuth userinfo response did not include user id")
	}
	return user, nil
}

func (s *Service) resolveZAIBusinessToken(ctx context.Context, oauthAccessToken string) (string, error) {
	var payload struct {
		Code    any    `json:"code"`
		Success *bool  `json:"success"`
		Msg     string `json:"msg"`
		Message string `json:"message"`
		Data    struct {
			AccessToken  string `json:"access_token"`
			AccessToken2 string `json:"accessToken"`
		} `json:"data"`
	}
	targetURL := strings.TrimRight(s.cfg.ZAIAPIBaseURL, "/") + "/api/auth/z/login"
	if err := s.requestJSON(ctx, http.MethodPost, targetURL, "", map[string]any{"token": oauthAccessToken}, &payload); err != nil {
		return "", err
	}
	if payload.Success != nil && !*payload.Success {
		return "", fmt.Errorf("Z.AI business login failed: %s", first(payload.Msg, payload.Message, "success=false"))
	}
	if !successfulCode(payload.Code) {
		return "", fmt.Errorf("Z.AI business login failed: %s", first(payload.Msg, payload.Message, fmt.Sprintf("business code %v", payload.Code)))
	}
	token := strings.TrimSpace(first(payload.Data.AccessToken, payload.Data.AccessToken2))
	if token == "" {
		return "", errors.New("Z.AI business login response is missing access_token")
	}
	return token, nil
}

func parseUser(raw json.RawMessage) accounts.User {
	if len(bytes.TrimSpace(raw)) == 0 || string(raw) == "null" {
		return accounts.User{}
	}
	var value struct {
		UserID            string `json:"user_id"`
		ID                string `json:"id"`
		Sub               string `json:"sub"`
		CustomerNumber    string `json:"customerNumber"`
		Email             string `json:"email"`
		Name              string `json:"name"`
		DisplayName       string `json:"displayName"`
		PreferredUsername string `json:"preferred_username"`
		Nickname          string `json:"nickname"`
		NickName          string `json:"nickName"`
		CustomerName      string `json:"customerName"`
		Avatar            string `json:"avatar"`
		AvatarURL         string `json:"avatar_url"`
		AvatarURL2        string `json:"avatarUrl"`
		Picture           string `json:"picture"`
	}
	_ = json.Unmarshal(raw, &value)
	return accounts.User{
		UserID:    first(value.UserID, value.ID, value.Sub, value.CustomerNumber),
		ID:        first(value.ID, value.UserID, value.Sub, value.CustomerNumber),
		Email:     value.Email,
		Name:      first(value.Name, value.DisplayName, value.PreferredUsername, value.NickName, value.CustomerName),
		Nickname:  first(value.Nickname, value.NickName, value.PreferredUsername),
		Avatar:    first(value.Avatar, value.AvatarURL, value.AvatarURL2, value.Picture),
		AvatarURL: first(value.AvatarURL, value.AvatarURL2, value.Picture),
	}
}

func (s *Service) failFlow(flow *Flow, message string) {
	s.mu.Lock()
	flow.Status = "failed"
	flow.Error = message
	s.mu.Unlock()
}

func (s *Service) failPendingFlows(message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, flow := range s.flows {
		if flow.Status == "pending" {
			flow.Status = "failed"
			flow.Error = message
		}
	}
}

func (s *Service) requestJSON(ctx context.Context, method, targetURL, bearer string, body any, target any) error {
	var content io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		content = bytes.NewReader(raw)
	}
	request, err := http.NewRequestWithContext(ctx, method, strings.TrimSpace(targetURL), content)
	if err != nil {
		return err
	}
	if bearer != "" {
		request.Header.Set("Authorization", bearer)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set("Accept", "application/json")
	response, err := s.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	rawBody, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("OAuth request failed reading HTTP %s response: %w", response.Status, err)
	}
	var status struct {
		Code    int    `json:"code"`
		Msg     string `json:"msg"`
		Message string `json:"message"`
	}
	if len(bytes.TrimSpace(rawBody)) > 0 {
		_ = json.Unmarshal(rawBody, &status)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message := first(status.Msg, status.Message, strings.TrimSpace(string(rawBody)), response.Status)
		return fmt.Errorf("OAuth request failed: HTTP %d %s", response.StatusCode, message)
	}
	if len(bytes.TrimSpace(rawBody)) == 0 {
		return fmt.Errorf("OAuth request returned an empty HTTP %d response", response.StatusCode)
	}
	if target == nil {
		return nil
	}
	if err := json.Unmarshal(rawBody, target); err != nil {
		return fmt.Errorf("OAuth request returned invalid JSON from HTTP %d: %w", response.StatusCode, err)
	}
	return nil
}

func successfulCode(value any) bool {
	if value == nil {
		return true
	}
	switch typed := value.(type) {
	case float64:
		return typed == 0 || typed == 200
	case int:
		return typed == 0 || typed == 200
	case string:
		return typed == "" || typed == "0" || typed == "200"
	default:
		return false
	}
}

func (s *Service) Status() []Flow {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]Flow, 0, len(s.flows))
	for _, flow := range s.flows {
		result = append(result, publicFlow(*flow))
	}
	return result
}

func publicFlow(flow Flow) Flow {
	flow.redirectURI = ""
	flow.account = nil
	return flow
}

func randomHex(size int) string {
	value := make([]byte, size)
	_, _ = rand.Read(value)
	return hex.EncodeToString(value)
}

func first(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
