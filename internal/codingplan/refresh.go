package codingplan

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"glm5.2proxy/internal/accounts"
	"glm5.2proxy/internal/config"
)

const apiKeyName = "zcode-api-key"

type Result struct {
	OrganizationID    string `json:"organizationId,omitempty"`
	ProjectID         string `json:"projectId,omitempty"`
	APIKeyName        string `json:"apiKeyName,omitempty"`
	APIKeyCreated     bool   `json:"apiKeyCreated"`
	SecretResolved    bool   `json:"secretResolved"`
	QuotaVerified     bool   `json:"quotaVerified"`
	QuotaError        string `json:"quotaError,omitempty"`
	StartPlanVerified bool   `json:"startPlanVerified"`
	StartPlanError    string `json:"startPlanError,omitempty"`
	Credential        string `json:"-"`
}

type Service struct {
	cfg    config.Config
	client *http.Client
}

type organization struct {
	OrganizationID   string    `json:"organizationId"`
	OrganizationName string    `json:"organizationName"`
	Projects         []project `json:"projects"`
}

type project struct {
	ProjectID   string `json:"projectId"`
	ProjectName string `json:"projectName"`
}

func New(cfg config.Config) *Service {
	return &Service{cfg: cfg, client: &http.Client{Timeout: 20 * time.Second}}
}

func (s *Service) Refresh(ctx context.Context, account accounts.Account) (Result, error) {
	accessToken := strings.TrimSpace(account.ZAIAcccessToken)
	if accessToken == "" {
		return Result{}, errors.New("conta sem zai access token; refaca o login OAuth")
	}
	keyName := apiKeyNameForAccount(account.ID)
	bizToken, err := s.resolveBizToken(ctx, accessToken)
	if err == nil {
		return s.resolveAPIKey(ctx, "Bearer "+bizToken, keyName)
	}
	result, directErr := s.resolveAPIKey(ctx, "Bearer "+accessToken, keyName)
	if directErr == nil {
		return result, nil
	}
	return Result{}, fmt.Errorf("%w; fallback com token salvo tambem falhou: %v", err, directErr)
}

func (s *Service) resolveBizToken(ctx context.Context, accessToken string) (string, error) {
	var response struct {
		AccessToken  string `json:"access_token"`
		AccessToken2 string `json:"accessToken"`
	}
	if err := s.request(ctx, http.MethodPost, "/api/auth/z/login", "", map[string]any{"token": accessToken}, &response); err != nil {
		return "", err
	}
	token := first(response.AccessToken, response.AccessToken2)
	if token == "" {
		return "", errors.New("Z.AI biz token response is missing access_token")
	}
	return token, nil
}

func (s *Service) resolveAPIKey(ctx context.Context, authorization string, preferredName string) (Result, error) {
	var customer struct {
		Organizations []organization `json:"organizations"`
	}
	if err := s.request(ctx, http.MethodGet, "/api/biz/customer/getCustomerInfo", authorization, nil, &customer); err != nil {
		return Result{}, err
	}
	orgID, projectID := pickOrgAndProject(customer.Organizations)
	if orgID == "" || projectID == "" {
		return Result{}, errors.New("nao foi possivel resolver organization/project da Z.AI")
	}
	basePath := "/api/biz/v1/organization/" + url.PathEscape(orgID) + "/projects/" + url.PathEscape(projectID) + "/api_keys"
	preferredName = strings.TrimSpace(preferredName)
	if preferredName == "" {
		preferredName = apiKeyName
	}

	var keys []struct {
		Name   string `json:"name"`
		APIKey string `json:"apiKey"`
	}
	if err := s.request(ctx, http.MethodGet, basePath, authorization, nil, &keys); err != nil {
		return Result{}, err
	}
	apiKey := ""
	selectedName := preferredName
	for _, key := range keys {
		if key.Name == preferredName || key.Name == apiKeyName {
			apiKey = strings.TrimSpace(key.APIKey)
			selectedName = key.Name
			break
		}
	}
	created := false
	if apiKey == "" {
		createdKey, err := s.createAPIKey(ctx, basePath, authorization, preferredName)
		if err != nil && duplicateAPIKeyName(err) {
			selectedName = uniqueAPIKeyName(preferredName)
			createdKey, err = s.createAPIKey(ctx, basePath, authorization, selectedName)
		}
		if err != nil {
			return Result{}, err
		}
		apiKey = strings.TrimSpace(createdKey)
		created = true
	}
	if apiKey == "" {
		return Result{}, errors.New("API key response is missing apiKey")
	}
	var copied struct {
		SecretKey string `json:"secretKey"`
	}
	if err := s.request(ctx, http.MethodGet, basePath+"/copy/"+url.PathEscape(apiKey), authorization, nil, &copied); err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(copied.SecretKey) == "" {
		return Result{}, errors.New("API key copy response is missing secretKey")
	}
	return Result{
		OrganizationID: orgID, ProjectID: projectID, APIKeyName: selectedName,
		APIKeyCreated: created, SecretResolved: true,
		Credential: apiKey + "." + strings.TrimSpace(copied.SecretKey),
	}, nil
}

func (s *Service) createAPIKey(ctx context.Context, basePath, authorization, name string) (string, error) {
	var createdKey struct {
		APIKey      string `json:"apiKey"`
		APIKeySnake string `json:"api_key"`
	}
	if err := s.request(ctx, http.MethodPost, basePath, authorization, map[string]any{"name": name}, &createdKey); err != nil {
		return "", err
	}
	return first(createdKey.APIKey, createdKey.APIKeySnake), nil
}

func apiKeyNameForAccount(accountID string) string {
	accountID = strings.ToLower(strings.TrimSpace(accountID))
	accountID = strings.NewReplacer("-", "").Replace(accountID)
	if len(accountID) > 10 {
		accountID = accountID[:10]
	}
	if accountID == "" {
		return apiKeyName
	}
	return "glm5proxy-" + accountID
}

func uniqueAPIKeyName(base string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		base = apiKeyName
	}
	if len(base) > 24 {
		base = base[:24]
	}
	return fmt.Sprintf("%s-%x", base, time.Now().UTC().UnixNano())
}

func duplicateAPIKeyName(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "duplicate")
}

func failedBusinessMessage(message string) bool {
	message = strings.ToLower(strings.TrimSpace(message))
	if message == "" {
		return false
	}
	return strings.Contains(message, "failed") ||
		strings.Contains(message, "fail") ||
		strings.Contains(message, "invalid") ||
		strings.Contains(message, "duplicate") ||
		strings.Contains(message, "denied")
}

func (s *Service) request(ctx context.Context, method, path, authorization string, body any, target any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	request, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(s.cfg.ZAIAPIBaseURL, "/")+path, reader)
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	response, err := s.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return err
	}
	var envelope struct {
		Code    any             `json:"code"`
		Msg     string          `json:"msg"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return fmt.Errorf("Z.AI business request failed: HTTP %d empty response", response.StatusCode)
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return fmt.Errorf("Z.AI business request returned invalid JSON from HTTP %d: %w", response.StatusCode, err)
	}
	message := first(envelope.Msg, envelope.Message, strings.TrimSpace(string(raw)), response.Status)
	if response.StatusCode < 200 || response.StatusCode >= 300 || !successfulCode(envelope.Code) || failedBusinessMessage(message) {
		return fmt.Errorf("Z.AI business request failed: HTTP %d %s", response.StatusCode, message)
	}
	if target == nil {
		return nil
	}
	if len(bytes.TrimSpace(envelope.Data)) == 0 || string(envelope.Data) == "null" {
		return nil
	}
	if err := json.Unmarshal(envelope.Data, target); err != nil {
		return fmt.Errorf("Z.AI business data returned invalid JSON: %w", err)
	}
	return nil
}

func pickOrgAndProject(orgs []organization) (string, string) {
	if len(orgs) == 0 {
		return "", ""
	}
	orgIndex := 0
	for index, org := range orgs {
		if strings.Contains(org.OrganizationName, "默认机构") {
			orgIndex = index
			break
		}
	}
	org := orgs[orgIndex]
	if len(org.Projects) == 0 {
		return "", ""
	}
	projectIndex := 0
	for index, project := range org.Projects {
		if strings.Contains(project.ProjectName, "默认项目") {
			projectIndex = index
			break
		}
	}
	return org.OrganizationID, org.Projects[projectIndex].ProjectID
}

func successfulCode(value any) bool {
	switch typed := value.(type) {
	case nil:
		return true
	case float64:
		return typed == 0 || typed == 200
	case string:
		return typed == "0" || typed == "200"
	default:
		return false
	}
}

func first(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
