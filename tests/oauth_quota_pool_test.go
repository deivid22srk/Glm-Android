package tests

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"glm5.2proxy/internal/accountpool"
	"glm5.2proxy/internal/accounts"
	"glm5.2proxy/internal/auth"
	"glm5.2proxy/internal/models"
	"glm5.2proxy/internal/quota"
	"glm5.2proxy/internal/upstream"
)

func TestOAuthQuotaAndAccountRotation(t *testing.T) {
	var chatToken string
	var tokenBody map[string]any
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/oauth/token":
			if err := json.NewDecoder(r.Body).Decode(&tokenBody); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"token": "token-one", "user": map[string]any{"user_id": "one", "email": "one@example.test"}, "zai": map[string]any{"access_token": "access-one"}}})
		case r.URL.Path == "/api/auth/z/login":
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"access_token": "biz-one"}})
		case r.URL.Path == "/billing/current":
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"plans": []any{}}})
		case r.URL.Path == "/billing/balance":
			chatToken = r.Header.Get("Authorization")
			available := 0
			if chatToken == "Bearer token-two" {
				available = 500
			}
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"balances": []any{map[string]any{"show_name": "GLM-5.2", "total_units": 1000, "used_units": 1000 - available, "remaining_units": available, "available_units": available}}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer mock.Close()

	cfg := testConfig(t)
	cfg.OAuthAuthorizeURL = mock.URL + "/authorize"
	cfg.OAuthTokenURL = mock.URL + "/oauth/token"
	cfg.OAuthUserInfoURL = mock.URL + "/userinfo"
	cfg.OAuthClientID = "client-test"
	cfg.ZAIAPIBaseURL = mock.URL
	cfg.BillingBaseURL = mock.URL + "/billing"
	store, _ := accounts.NewStore(cfg.CredentialsPath, cfg.CredentialSecret)
	oauth := auth.New(cfg, store)
	flow, err := oauth.Start(context.Background(), "http://127.0.0.1:3005")
	if err != nil || flow.FlowID == "" {
		t.Fatalf("OAuth start failed: %+v %v", flow, err)
	}
	authorizeURL, err := url.Parse(flow.AuthorizeURL)
	if err != nil {
		t.Fatal(err)
	}
	if authorizeURL.Query().Get("client_id") != "client-test" || authorizeURL.Query().Get("response_type") != "code" {
		t.Fatalf("unexpected authorize URL: %s", flow.AuthorizeURL)
	}
	if _, err := oauth.Complete(context.Background(), flow.FlowID, "code-one", ""); err != nil {
		t.Fatalf("OAuth callback failed: %v", err)
	}
	result, err := oauth.Poll(context.Background(), flow.FlowID)
	if err != nil || result["status"] != "ready" {
		t.Fatalf("OAuth poll failed: %+v %v", result, err)
	}
	if tokenBody["provider"] != "zai" || tokenBody["code"] != "code-one" || tokenBody["state"] != flow.FlowID {
		t.Fatalf("unexpected token body: %+v", tokenBody)
	}
	if _, err := store.Upsert(accounts.User{UserID: "two"}, "token-two", ""); err != nil {
		t.Fatal(err)
	}
	loader := upstream.NewLoader(cfg, store)
	quotaService := quota.New(cfg)
	pool := accountpool.New(cfg, store, loader, quotaService)
	model, _ := models.Resolve("glm-5.2")
	selection := pool.Select(context.Background(), model)
	if selection.Account == nil || selection.Account.ID != "two" || !selection.Rotated {
		t.Fatalf("account did not rotate: %+v", selection)
	}
	if store.Active().ID != "two" || chatToken != "Bearer token-two" {
		t.Fatalf("wrong active account/token: active=%+v token=%s", store.Active(), chatToken)
	}
}

func TestOAuthCallbackReportsHTTPStatusInsteadOfEOF(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer mock.Close()

	cfg := testConfig(t)
	cfg.OAuthAuthorizeURL = mock.URL + "/authorize"
	cfg.OAuthTokenURL = mock.URL + "/oauth/token"
	cfg.OAuthClientID = "client-test"
	store, _ := accounts.NewStore(cfg.CredentialsPath, cfg.CredentialSecret)
	oauth := auth.New(cfg, store)

	flow, err := oauth.Start(context.Background(), "http://127.0.0.1:3005")
	if err != nil {
		t.Fatal(err)
	}
	_, err = oauth.Complete(context.Background(), flow.FlowID, "code-one", "")
	if err == nil {
		t.Fatal("expected OAuth callback error")
	}
	message := err.Error()
	if !strings.Contains(message, "HTTP 429") || strings.Contains(message, "EOF") {
		t.Fatalf("unexpected OAuth error message: %q", message)
	}
}

func TestOAuthPollExpiresPendingFlow(t *testing.T) {
	cfg := testConfig(t)
	cfg.OAuthAuthorizeURL = "https://example.test/authorize"
	cfg.OAuthClientID = "client-test"
	cfg.OAuthFlowTimeout = time.Millisecond
	store, _ := accounts.NewStore(cfg.CredentialsPath, cfg.CredentialSecret)
	oauth := auth.New(cfg, store)

	flow, err := oauth.Start(context.Background(), "http://127.0.0.1:3005")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	result, err := oauth.Poll(context.Background(), flow.FlowID)
	if err != nil {
		t.Fatal(err)
	}
	if result["status"] != "failed" || !strings.Contains(result["error"].(string), "expired") {
		t.Fatalf("expected expired flow to fail, got %+v", result)
	}
}

func TestOAuthInvalidCallbackStateFailsPendingFlow(t *testing.T) {
	cfg := testConfig(t)
	cfg.OAuthAuthorizeURL = "https://example.test/authorize"
	cfg.OAuthClientID = "client-test"
	store, _ := accounts.NewStore(cfg.CredentialsPath, cfg.CredentialSecret)
	oauth := auth.New(cfg, store)

	flow, err := oauth.Start(context.Background(), "http://127.0.0.1:3005")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := oauth.Complete(context.Background(), "wrong-state", "code-one", ""); err == nil {
		t.Fatal("expected invalid callback state error")
	}
	result, err := oauth.Poll(context.Background(), flow.FlowID)
	if err != nil {
		t.Fatal(err)
	}
	if result["status"] != "failed" || !strings.Contains(result["error"].(string), "state") {
		t.Fatalf("expected invalid callback to fail pending flow, got %+v", result)
	}
}

func TestAccountPoolRotatesBeforeChatWhenActiveQuotaIsExhausted(t *testing.T) {
	var checkedTokens []string
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if writeEmptyBillingCurrent(w, r) {
			return
		}
		if r.URL.Path != "/billing/balance" {
			http.NotFound(w, r)
			return
		}
		token := r.Header.Get("Authorization")
		checkedTokens = append(checkedTokens, token)
		available := 0
		if token == "Bearer token-two" {
			available = 1000
		}
		json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"balances": []any{map[string]any{"show_name": "GLM-5.2", "total_units": 1000, "used_units": 1000 - available, "remaining_units": available, "available_units": available}}}})
	}))
	defer mock.Close()

	cfg := testConfig(t)
	cfg.BillingBaseURL = mock.URL + "/billing"
	store, _ := accounts.NewStore(cfg.CredentialsPath, cfg.CredentialSecret)
	_, _ = store.Upsert(accounts.User{UserID: "one"}, "token-one", "")
	_, _ = store.Upsert(accounts.User{UserID: "two"}, "token-two", "")

	loader := upstream.NewLoader(cfg, store)
	pool := accountpool.New(cfg, store, loader, quota.New(cfg))
	model, _ := models.Resolve("glm-5.2")
	selection := pool.Select(context.Background(), model)
	if selection.Account == nil || selection.Account.ID != "two" || !selection.Rotated {
		t.Fatalf("expected exhausted active account to rotate to account two: %+v", selection)
	}
	if store.Active().ID != "two" {
		t.Fatalf("rotated account was not persisted active: %+v", store.Active())
	}
	if len(checkedTokens) != 2 || checkedTokens[0] != "Bearer token-one" || checkedTokens[1] != "Bearer token-two" {
		t.Fatalf("unexpected quota check order: %+v", checkedTokens)
	}
}

func TestAccountPoolRotatesWhenAvailableIsBelowRequestReserve(t *testing.T) {
	var checkedTokens []string
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if writeEmptyBillingCurrent(w, r) {
			return
		}
		if r.URL.Path != "/billing/balance" {
			http.NotFound(w, r)
			return
		}
		token := r.Header.Get("Authorization")
		checkedTokens = append(checkedTokens, token)
		available := 56125
		if token == "Bearer fresh-token" {
			available = 3000000
		}
		json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"balances": []any{map[string]any{"show_name": "GLM-5.2", "total_units": 3000000, "used_units": 3000000 - available, "remaining_units": available, "available_units": available}}}})
	}))
	defer mock.Close()

	cfg := testConfig(t)
	cfg.BillingBaseURL = mock.URL + "/billing"
	cfg.AccountMinAvailable = 96000
	store, _ := accounts.NewStore(cfg.CredentialsPath, cfg.CredentialSecret)
	_, _ = store.Upsert(accounts.User{UserID: "low"}, "low-token", "")
	_, _ = store.Upsert(accounts.User{UserID: "fresh"}, "fresh-token", "")

	loader := upstream.NewLoader(cfg, store)
	pool := accountpool.New(cfg, store, loader, quota.New(cfg))
	model, _ := models.Resolve("glm-5.2")
	selection := pool.Select(context.Background(), model)
	if selection.Account == nil || selection.Account.User.UserID != "fresh" || !selection.Rotated {
		t.Fatalf("expected low-reserve account to rotate to fresh account: %+v", selection)
	}
	if len(checkedTokens) != 2 || checkedTokens[0] != "Bearer low-token" || checkedTokens[1] != "Bearer fresh-token" {
		t.Fatalf("unexpected quota check order: %+v", checkedTokens)
	}
}

func TestAccountPoolPrefersLessUsedAccountBeforeHigherTokenAccount(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if writeEmptyBillingCurrent(w, r) {
			return
		}
		if r.URL.Path != "/billing/balance" {
			http.NotFound(w, r)
			return
		}
		token := r.Header.Get("Authorization")
		available := 900
		if token == "Bearer used-token" {
			available = 3000000
		}
		json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"balances": []any{map[string]any{"show_name": "GLM-5.2", "total_units": 3000000, "used_units": 3000000 - available, "remaining_units": available, "available_units": available}}}})
	}))
	defer mock.Close()

	cfg := testConfig(t)
	cfg.BillingBaseURL = mock.URL + "/billing"
	store, _ := accounts.NewStore(cfg.CredentialsPath, cfg.CredentialSecret)
	_, _ = store.Upsert(accounts.User{UserID: "used"}, "used-token", "")
	_, _ = store.Upsert(accounts.User{UserID: "new"}, "new-token", "")

	loader := upstream.NewLoader(cfg, store)
	pool := accountpool.New(cfg, store, loader, quota.New(cfg))
	pool.MarkRequest("used")
	model, _ := models.Resolve("glm-5.2")
	selection := pool.Select(context.Background(), model)
	if selection.Account == nil || selection.Account.ID != "new" {
		t.Fatalf("expected less-used new account to win balanced selection: %+v", selection)
	}
	if selection.RequestCount != 0 || selection.Available == nil || *selection.Available != 900 {
		t.Fatalf("selection stats were not exposed correctly: %+v", selection)
	}
	if !strings.Contains(selection.Reason, "menos requests") {
		t.Fatalf("selection reason should explain stats: %q", selection.Reason)
	}
}

func TestAccountPoolSelectForRequestPicksHighestQuotaThatCoversRequest(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if writeEmptyBillingCurrent(w, r) {
			return
		}
		if r.URL.Path != "/billing/balance" {
			http.NotFound(w, r)
			return
		}
		availableByToken := map[string]int{
			"Bearer low-token":  400000,
			"Bearer high-token": 900000,
			"Bearer max-token":  700000,
		}
		available := availableByToken[r.Header.Get("Authorization")]
		json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"balances": []any{map[string]any{"show_name": "GLM-5.2", "total_units": 3000000, "used_units": 3000000 - available, "remaining_units": available, "available_units": available}}}})
	}))
	defer mock.Close()

	cfg := testConfig(t)
	cfg.BillingBaseURL = mock.URL + "/billing"
	cfg.AccountMinAvailable = 1
	store, _ := accounts.NewStore(cfg.CredentialsPath, cfg.CredentialSecret)
	_, _ = store.Upsert(accounts.User{UserID: "low"}, "low-token", "")
	_, _ = store.Upsert(accounts.User{UserID: "high"}, "high-token", "")
	_, _ = store.Upsert(accounts.User{UserID: "max"}, "max-token", "")

	loader := upstream.NewLoader(cfg, store)
	pool := accountpool.New(cfg, store, loader, quota.New(cfg))
	model, _ := models.Resolve("glm-5.2")
	pool.MarkRequest("high")
	selection := pool.SelectForRequest(context.Background(), model, 500000, nil)
	if selection.Account == nil || selection.Account.ID != "high" {
		t.Fatalf("expected highest quota account to cover large request, got %+v", selection)
	}
	if selection.Available == nil || *selection.Available != 900000 {
		t.Fatalf("selection did not expose highest available quota: %+v", selection)
	}
	if !strings.Contains(selection.Reason, "maior cota") {
		t.Fatalf("selection reason should explain quota-first choice: %q", selection.Reason)
	}
}

func TestAccountPoolSelectBestEffortIgnoresPreventiveQuotaCutoff(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if writeEmptyBillingCurrent(w, r) {
			return
		}
		if r.URL.Path != "/billing/balance" {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"balances": []any{map[string]any{"show_name": "GLM-5.2", "total_units": 3000000, "used_units": 2999999, "remaining_units": 1, "available_units": 1}}}})
	}))
	defer mock.Close()

	cfg := testConfig(t)
	cfg.BillingBaseURL = mock.URL + "/billing"
	cfg.AccountMinAvailable = 96000
	store, _ := accounts.NewStore(cfg.CredentialsPath, cfg.CredentialSecret)
	_, _ = store.Upsert(accounts.User{UserID: "low"}, "low-token", "")

	loader := upstream.NewLoader(cfg, store)
	pool := accountpool.New(cfg, store, loader, quota.New(cfg))
	model, _ := models.Resolve("glm-5.2")
	selection := pool.SelectBestEffort(context.Background(), model, nil)
	if selection.Account == nil || selection.Account.ID != "low" || selection.AllExhausted {
		t.Fatalf("expected best-effort mode to try saved account below cutoff: %+v", selection)
	}
}

func TestAccountPoolDoesNotSelectAccountWithoutBalanceOverHealthyAccount(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if writeEmptyBillingCurrent(w, r) {
			return
		}
		if r.URL.Path != "/billing/balance" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") == "Bearer broken-token" {
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"balances": []any{}}})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"balances": []any{map[string]any{"show_name": "GLM-5.2", "total_units": 3000000, "used_units": 0, "remaining_units": 3000000, "available_units": 3000000}}}})
	}))
	defer mock.Close()

	cfg := testConfig(t)
	cfg.BillingBaseURL = mock.URL + "/billing"
	store, _ := accounts.NewStore(cfg.CredentialsPath, cfg.CredentialSecret)
	_, _ = store.Upsert(accounts.User{UserID: "broken"}, "broken-token", "")
	_, _ = store.Upsert(accounts.User{UserID: "healthy"}, "healthy-token", "")

	loader := upstream.NewLoader(cfg, store)
	pool := accountpool.New(cfg, store, loader, quota.New(cfg))
	pool.MarkRequest("healthy")
	model, _ := models.Resolve("glm-5.2")
	selection := pool.Select(context.Background(), model)
	if selection.Account == nil || selection.Account.ID != "healthy" {
		t.Fatalf("expected account with real Start Plan balance to win, got %+v", selection)
	}
}
