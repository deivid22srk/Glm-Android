package codingplan

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"glm5.2proxy/internal/accounts"
	"glm5.2proxy/internal/config"
)

func TestRefreshResolvesExistingZaiAPIKey(t *testing.T) {
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/auth/z/login":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["token"] != "oauth-access" {
				t.Fatalf("unexpected oauth token: %q", body["token"])
			}
			_, _ = w.Write([]byte(`{"code":0,"data":{"access_token":"biz-token"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/biz/customer/getCustomerInfo":
			if r.Header.Get("Authorization") != "Bearer biz-token" {
				t.Fatalf("unexpected auth header: %q", r.Header.Get("Authorization"))
			}
			_, _ = w.Write([]byte(`{"code":0,"data":{"organizations":[{"organizationId":"org-1","organizationName":"默认机构","projects":[{"projectId":"project-1","projectName":"默认项目"}]}]}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/biz/v1/organization/org-1/projects/project-1/api_keys":
			_, _ = w.Write([]byte(`{"code":0,"data":[{"name":"zcode-api-key","apiKey":"key-1"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/biz/v1/organization/org-1/projects/project-1/api_keys/copy/key-1":
			_, _ = w.Write([]byte(`{"code":0,"data":{"secretKey":"secret-1"}}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	service := New(config.Config{ZAIAPIBaseURL: server.URL})
	result, err := service.Refresh(context.Background(), accounts.Account{ZAIAcccessToken: "oauth-access"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.SecretResolved || result.APIKeyCreated || result.OrganizationID != "org-1" || result.ProjectID != "project-1" || result.APIKeyName != "zcode-api-key" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(calls) != 4 {
		t.Fatalf("unexpected calls: %+v", calls)
	}
}

func TestRefreshCreatesMissingZaiAPIKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/auth/z/login":
			_, _ = w.Write([]byte(`{"code":0,"data":{"accessToken":"biz-token"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/biz/customer/getCustomerInfo":
			_, _ = w.Write([]byte(`{"code":0,"data":{"organizations":[{"organizationId":"org-1","organizationName":"Other","projects":[{"projectId":"project-1","projectName":"Other"}]}]}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/biz/v1/organization/org-1/projects/project-1/api_keys":
			_, _ = w.Write([]byte(`{"code":0,"data":[]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/biz/v1/organization/org-1/projects/project-1/api_keys":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["name"] != "zcode-api-key" {
				t.Fatalf("unexpected api key name: %q", body["name"])
			}
			_, _ = w.Write([]byte(`{"code":0,"data":{"apiKey":"key-2"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/biz/v1/organization/org-1/projects/project-1/api_keys/copy/key-2":
			_, _ = w.Write([]byte(`{"code":0,"data":{"secretKey":"secret-2"}}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	service := New(config.Config{ZAIAPIBaseURL: server.URL})
	result, err := service.Refresh(context.Background(), accounts.Account{ZAIAcccessToken: "oauth-access"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.APIKeyCreated || !result.SecretResolved {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestRefreshFallsBackWhenPreferredAPIKeyNameIsDuplicate(t *testing.T) {
	var createAttempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/auth/z/login":
			_, _ = w.Write([]byte(`{"code":0,"data":{"accessToken":"biz-token"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/biz/customer/getCustomerInfo":
			_, _ = w.Write([]byte(`{"code":0,"data":{"organizations":[{"organizationId":"org-1","projects":[{"projectId":"project-1"}]}]}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/biz/v1/organization/org-1/projects/project-1/api_keys":
			_, _ = w.Write([]byte(`{"code":0,"data":[]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/biz/v1/organization/org-1/projects/project-1/api_keys":
			createAttempts++
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if createAttempts == 1 {
				if body["name"] != "glm5proxy-account123" {
					t.Fatalf("unexpected first api key name: %q", body["name"])
				}
				_, _ = w.Write([]byte(`{"code":200,"msg":"Creation failed, apiKey name [glm5proxy-account123] is duplicate"}`))
				return
			}
			if body["name"] == "glm5proxy-account123" {
				t.Fatalf("fallback api key name was not changed: %q", body["name"])
			}
			_, _ = w.Write([]byte(`{"code":0,"data":{"apiKey":"key-fallback"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/biz/v1/organization/org-1/projects/project-1/api_keys/copy/key-fallback":
			_, _ = w.Write([]byte(`{"code":0,"data":{"secretKey":"secret-fallback"}}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	service := New(config.Config{ZAIAPIBaseURL: server.URL})
	result, err := service.Refresh(context.Background(), accounts.Account{ID: "account-123456", ZAIAcccessToken: "oauth-access"})
	if err != nil {
		t.Fatal(err)
	}
	if createAttempts != 2 || !result.APIKeyCreated || !result.SecretResolved || result.Credential != "key-fallback.secret-fallback" {
		t.Fatalf("unexpected duplicate fallback result: attempts=%d result=%+v", createAttempts, result)
	}
}

func TestRefreshAcceptsAlreadyResolvedBusinessToken(t *testing.T) {
	var loginCalled bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/auth/z/login":
			loginCalled = true
			_, _ = w.Write([]byte(`{"code":200,"msg":"User login failed"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/biz/customer/getCustomerInfo":
			if r.Header.Get("Authorization") != "Bearer business-token" {
				t.Fatalf("unexpected auth header: %q", r.Header.Get("Authorization"))
			}
			_, _ = w.Write([]byte(`{"code":0,"data":{"organizations":[{"organizationId":"org-1","projects":[{"projectId":"project-1"}]}]}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/biz/v1/organization/org-1/projects/project-1/api_keys":
			_, _ = w.Write([]byte(`{"code":0,"data":[{"name":"zcode-api-key","apiKey":"key-1"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/biz/v1/organization/org-1/projects/project-1/api_keys/copy/key-1":
			_, _ = w.Write([]byte(`{"code":0,"data":{"secretKey":"secret-1"}}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	service := New(config.Config{ZAIAPIBaseURL: server.URL})
	result, err := service.Refresh(context.Background(), accounts.Account{ZAIAcccessToken: "business-token"})
	if err != nil {
		t.Fatal(err)
	}
	if !loginCalled || !result.SecretResolved {
		t.Fatalf("unexpected result: loginCalled=%t result=%+v", loginCalled, result)
	}
}
