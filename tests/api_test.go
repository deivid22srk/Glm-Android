package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"glm5.2proxy/internal/accountpool"
	"glm5.2proxy/internal/accounts"
	"glm5.2proxy/internal/api"
	"glm5.2proxy/internal/auth"
	"glm5.2proxy/internal/captcha"
	"glm5.2proxy/internal/proxy"
	"glm5.2proxy/internal/quota"
	"glm5.2proxy/internal/state"
	"glm5.2proxy/internal/upstream"
)

func TestAdministrativeAPIAndAPIKeyProtection(t *testing.T) {
	cfg := testConfig(t)
	admin, _ := state.NewAdminStore(cfg.AdminPath, 3005, state.ThinkingSettings{Enabled: true, BudgetTokens: 32000, Effort: "max"})
	accountStore, _ := accounts.NewStore(cfg.CredentialsPath, cfg.CredentialSecret)
	loader := upstream.NewLoader(cfg, accountStore)
	quotaService := quota.New(cfg)
	bridge := captcha.NewBridge(cfg)
	browser := captcha.NewBrowserManager(cfg, 3005)
	server := api.New(cfg, 3005, admin, accountStore, auth.New(cfg, accountStore), quotaService, accountpool.New(cfg, accountStore, loader, quotaService), loader, bridge, browser, proxy.New(cfg, bridge))
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	response, err := http.Get(httpServer.URL + "/api/admin/models/capabilities")
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("capabilities endpoint failed: %v status=%v", err, response.StatusCode)
	}
	var capabilities map[string]any
	json.NewDecoder(response.Body).Decode(&capabilities)
	response.Body.Close()
	if len(capabilities["data"].([]any)) != 2 {
		t.Fatalf("unexpected capabilities: %+v", capabilities)
	}

	create, _ := http.Post(httpServer.URL+"/api/admin/api-keys", "application/json", strings.NewReader(`{"name":"IDE"}`))
	var created map[string]any
	json.NewDecoder(create.Body).Decode(&created)
	create.Body.Close()
	secret, _ := created["secret"].(string)
	if secret == "" {
		t.Fatalf("API key secret missing: %+v", created)
	}
	unauthorized, _ := http.Get(httpServer.URL + "/v1/models")
	if unauthorized.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected API key enforcement, got %d", unauthorized.StatusCode)
	}
	unauthorized.Body.Close()
	request, _ := http.NewRequest(http.MethodGet, httpServer.URL+"/v1/models", nil)
	request.Header.Set("Authorization", "Bearer "+secret)
	authorized, _ := http.DefaultClient.Do(request)
	if authorized.StatusCode != http.StatusOK {
		t.Fatalf("valid API key rejected: %d", authorized.StatusCode)
	}
	authorized.Body.Close()
}

func TestChatWithoutZCodeAccountReturnsFriendlyAuthError(t *testing.T) {
	cfg := testConfig(t)
	cfg.AccountCreatorEnabled = false
	admin, _ := state.NewAdminStore(cfg.AdminPath, 3005, state.ThinkingSettings{Enabled: true, BudgetTokens: 32000, Effort: "max"})
	accountStore, _ := accounts.NewStore(cfg.CredentialsPath, cfg.CredentialSecret)
	loader := upstream.NewLoader(cfg, accountStore)
	quotaService := quota.New(cfg)
	bridge := captcha.NewBridge(cfg)
	browser := captcha.NewBrowserManager(cfg, 3005)
	server := api.New(cfg, 3005, admin, accountStore, auth.New(cfg, accountStore), quotaService, accountpool.New(cfg, accountStore, loader, quotaService), loader, bridge, browser, proxy.New(cfg, bridge))
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	body := []byte(`{"model":"glm-5.2","messages":[{"role":"user","content":"oi"}],"stream":false}`)
	response, err := http.Post(httpServer.URL+"/v1/chat/completions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var payload map[string]map[string]any
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusUnauthorized || payload["error"]["type"] != "zcode_auth_missing" {
		t.Fatalf("expected friendly auth error, status=%d payload=%+v", response.StatusCode, payload)
	}
}

func TestCaptchaBrowserUnavailableIsLoggedWithActionableEvent(t *testing.T) {
	fakeUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"message":"captcha verification required","type":"zcode_upstream_error"}}`))
	}))
	defer fakeUpstream.Close()

	cfg := testConfig(t)
	cfg.Authorization = "Bearer test-token"
	cfg.CaptchaEnabled = true
	cfg.HeadlessEnabled = false
	cfg.UpstreamURL = fakeUpstream.URL
	admin, _ := state.NewAdminStore(cfg.AdminPath, 3005, state.ThinkingSettings{Enabled: true, BudgetTokens: 32000, Effort: "max"})
	accountStore, _ := accounts.NewStore(cfg.CredentialsPath, cfg.CredentialSecret)
	loader := upstream.NewLoader(cfg, accountStore)
	quotaService := quota.New(cfg)
	bridge := captcha.NewBridge(cfg)
	browser := captcha.NewBrowserManager(cfg, 3005)
	server := api.New(cfg, 3005, admin, accountStore, auth.New(cfg, accountStore), quotaService, accountpool.New(cfg, accountStore, loader, quotaService), loader, bridge, browser, proxy.New(cfg, bridge))
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	body := []byte(`{"model":"glm-5.2","messages":[{"role":"user","content":"oi"}],"stream":false}`)
	response, err := http.Post(httpServer.URL+"/v1/chat/completions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]map[string]any
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusBadGateway || payload["error"]["type"] != "zcode_captcha_browser_unavailable" {
		t.Fatalf("expected captcha browser error, status=%d payload=%+v", response.StatusCode, payload)
	}

	logsResponse, err := http.Get(httpServer.URL + "/api/admin/logs")
	if err != nil {
		t.Fatal(err)
	}
	var logs map[string]any
	if err := json.NewDecoder(logsResponse.Body).Decode(&logs); err != nil {
		t.Fatal(err)
	}
	logsResponse.Body.Close()
	entries := logs["data"].([]any)
	if len(entries) == 0 {
		t.Fatal("expected captcha log entry")
	}
	last := entries[len(entries)-1].(map[string]any)
	if last["event"] != "captcha.browser_disabled" || !strings.Contains(last["message"].(string), "/zcode/captcha/browser") {
		t.Fatalf("unexpected captcha log: %+v", last)
	}
}

func TestChatDefaultsToNonStreamingOpenAIResponse(t *testing.T) {
	var upstreamBody map[string]any
	fakeUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&upstreamBody); err != nil {
			t.Errorf("decode upstream body: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer fakeUpstream.Close()

	cfg := testConfig(t)
	cfg.Authorization = "Bearer test-token"
	cfg.UpstreamURL = fakeUpstream.URL
	admin, _ := state.NewAdminStore(cfg.AdminPath, 3005, state.ThinkingSettings{Enabled: true, BudgetTokens: 32000, Effort: "max"})
	accountStore, _ := accounts.NewStore(cfg.CredentialsPath, cfg.CredentialSecret)
	loader := upstream.NewLoader(cfg, accountStore)
	quotaService := quota.New(cfg)
	bridge := captcha.NewBridge(cfg)
	browser := captcha.NewBrowserManager(cfg, 3005)
	server := api.New(cfg, 3005, admin, accountStore, auth.New(cfg, accountStore), quotaService, accountpool.New(cfg, accountStore, loader, quotaService), loader, bridge, browser, proxy.New(cfg, bridge))
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	body := []byte(`{"model":"glm-5.2","messages":[{"role":"user","content":"oi"}]}`)
	response, err := http.Post(httpServer.URL+"/v1/chat/completions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var payload map[string]any
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || payload["object"] != "chat.completion" {
		t.Fatalf("expected non-streaming chat completion, status=%d payload=%+v", response.StatusCode, payload)
	}
	choices := payload["choices"].([]any)
	message := choices[0].(map[string]any)["message"].(map[string]any)
	if message["content"] != "ok" {
		t.Fatalf("unexpected completion content: %+v", payload)
	}
	if upstreamBody["stream"] != true {
		t.Fatalf("upstream body must remain streaming for ZCode SSE parser: %+v", upstreamBody)
	}
}

func TestParameterErrorLogsSanitizedPayloadDiagnostic(t *testing.T) {
	fakeUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/messages":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"code":"3001","message":"parameter error","type":"zcode_upstream_error"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer fakeUpstream.Close()

	cfg := testConfig(t)
	cfg.Authorization = "Bearer test-token"
	cfg.UpstreamURL = fakeUpstream.URL + "/messages"
	admin, _ := state.NewAdminStore(cfg.AdminPath, 3005, state.ThinkingSettings{Enabled: true, BudgetTokens: 32000, Effort: "max"})
	accountStore, _ := accounts.NewStore(cfg.CredentialsPath, cfg.CredentialSecret)
	loader := upstream.NewLoader(cfg, accountStore)
	quotaService := quota.New(cfg)
	bridge := captcha.NewBridge(cfg)
	browser := captcha.NewBrowserManager(cfg, 3005)
	server := api.New(cfg, 3005, admin, accountStore, auth.New(cfg, accountStore), quotaService, accountpool.New(cfg, accountStore, loader, quotaService), loader, bridge, browser, proxy.New(cfg, bridge))
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	body := []byte(`{"model":"glm-5.2","messages":[{"role":"user","content":"texto muito secreto do usuario"}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}],"stream":false}`)
	response, err := http.Post(httpServer.URL+"/v1/chat/completions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var errorPayload map[string]map[string]any
	if err := json.NewDecoder(response.Body).Decode(&errorPayload); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected upstream parameter status, got %d", response.StatusCode)
	}
	if _, ok := errorPayload["error"]["request_diagnostic"].(map[string]any); !ok {
		t.Fatalf("expected request diagnostic in response: %+v", errorPayload)
	}

	logsResponse, err := http.Get(httpServer.URL + "/api/admin/logs")
	if err != nil {
		t.Fatal(err)
	}
	var logs map[string]any
	if err := json.NewDecoder(logsResponse.Body).Decode(&logs); err != nil {
		t.Fatal(err)
	}
	logsResponse.Body.Close()
	entries := logs["data"].([]any)
	last := entries[len(entries)-1].(map[string]any)
	message := last["message"].(string)
	if last["event"] != "upstream.parameter_error" ||
		!strings.Contains(message, "translated_body") ||
		!strings.Contains(message, "tool_choice") ||
		strings.Contains(message, "texto muito secreto do usuario") {
		t.Fatalf("unexpected parameter diagnostic log: %+v", last)
	}
}

func TestChatRequestsAreQueuedPerDefaultAccountAndModel(t *testing.T) {
	var active int32
	var maxActive int32
	fakeUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/messages":
			current := atomic.AddInt32(&active, 1)
			for {
				previous := atomic.LoadInt32(&maxActive)
				if current <= previous || atomic.CompareAndSwapInt32(&maxActive, previous, current) {
					break
				}
			}
			time.Sleep(50 * time.Millisecond)
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\n"))
			_, _ = w.Write([]byte("data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
			atomic.AddInt32(&active, -1)
		case r.URL.Path == "/billing/current":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"data":{"plans":[]}}`))
		case r.URL.Path == "/billing/balance":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"data":{"balances":[{"show_name":"GLM-5.2","total_units":3000000,"used_units":0,"remaining_units":3000000,"available_units":3000000}]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer fakeUpstream.Close()

	cfg := testConfig(t)
	cfg.Authorization = "Bearer test-token"
	cfg.UpstreamURL = fakeUpstream.URL + "/messages"
	cfg.BillingBaseURL = fakeUpstream.URL + "/billing"
	admin, _ := state.NewAdminStore(cfg.AdminPath, 3005, state.ThinkingSettings{Enabled: true, BudgetTokens: 32000, Effort: "max"})
	accountStore, _ := accounts.NewStore(cfg.CredentialsPath, cfg.CredentialSecret)
	loader := upstream.NewLoader(cfg, accountStore)
	quotaService := quota.New(cfg)
	bridge := captcha.NewBridge(cfg)
	browser := captcha.NewBrowserManager(cfg, 3005)
	server := api.New(cfg, 3005, admin, accountStore, auth.New(cfg, accountStore), quotaService, accountpool.New(cfg, accountStore, loader, quotaService), loader, bridge, browser, proxy.New(cfg, bridge))
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	body := []byte(`{"model":"glm-5.2","messages":[{"role":"user","content":"oi"}],"stream":false}`)
	var wait sync.WaitGroup
	statuses := make(chan int, 2)
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			response, err := http.Post(httpServer.URL+"/v1/chat/completions", "application/json", bytes.NewReader(body))
			if err != nil {
				t.Errorf("chat request failed: %v", err)
				return
			}
			defer response.Body.Close()
			statuses <- response.StatusCode
		}()
	}
	wait.Wait()
	close(statuses)
	for status := range statuses {
		if status != http.StatusOK {
			t.Fatalf("expected chat status 200, got %d", status)
		}
	}
	if maxActive != 1 {
		t.Fatalf("expected upstream serialization, got max active %d", maxActive)
	}
}

func TestChatRotatesAccountWhenUpstreamReportsQuotaExhausted(t *testing.T) {
	var chatTokens []string
	fakeUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/messages":
			token := r.Header.Get("Authorization")
			chatTokens = append(chatTokens, token)
			if token == "Bearer token-one" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"error":{"message":"quota exhausted for model","type":"quota_exhausted","code":"quota_exhausted"}}`))
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\n"))
			_, _ = w.Write([]byte("data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		case r.URL.Path == "/billing/current":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"data":{"plans":[]}}`))
		case r.URL.Path == "/billing/balance":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"data":{"balances":[{"show_name":"GLM-5.2","total_units":3000000,"used_units":0,"remaining_units":3000000,"available_units":3000000}]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer fakeUpstream.Close()

	cfg := testConfig(t)
	cfg.UpstreamURL = fakeUpstream.URL + "/messages"
	cfg.BillingBaseURL = fakeUpstream.URL + "/billing"
	admin, _ := state.NewAdminStore(cfg.AdminPath, 3005, state.ThinkingSettings{Enabled: true, BudgetTokens: 32000, Effort: "max"})
	accountStore, _ := accounts.NewStore(cfg.CredentialsPath, cfg.CredentialSecret)
	_, _ = accountStore.Upsert(accounts.User{UserID: "one"}, "token-one", "")
	_, _ = accountStore.Upsert(accounts.User{UserID: "two"}, "token-two", "")
	loader := upstream.NewLoader(cfg, accountStore)
	quotaService := quota.New(cfg)
	bridge := captcha.NewBridge(cfg)
	browser := captcha.NewBrowserManager(cfg, 3005)
	server := api.New(cfg, 3005, admin, accountStore, auth.New(cfg, accountStore), quotaService, accountpool.New(cfg, accountStore, loader, quotaService), loader, bridge, browser, proxy.New(cfg, bridge))
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	body := []byte(`{"model":"glm-5.2","messages":[{"role":"user","content":"oi"}],"stream":false}`)
	response, err := http.Post(httpServer.URL+"/v1/chat/completions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected rotated request to succeed, got %d", response.StatusCode)
	}
	if strings.Join(chatTokens, ",") != "Bearer token-one,Bearer token-two" {
		t.Fatalf("request did not rotate across accounts: %+v", chatTokens)
	}
	if active := accountStore.Active(); active == nil || active.ID != "two" {
		t.Fatalf("rotated account was not persisted as active: %+v", active)
	}
}

func TestChatRotatesAccountWhenUpstreamReportsExpiredAuth(t *testing.T) {
	var chatTokens []string
	fakeUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/messages":
			token := r.Header.Get("Authorization")
			chatTokens = append(chatTokens, token)
			if token == "Bearer token-one" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":{"message":"invalid or expired auth token","type":"auth_failed","code":"auth_failed"}}`))
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\n"))
			_, _ = w.Write([]byte("data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		case r.URL.Path == "/billing/current":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"data":{"plans":[]}}`))
		case r.URL.Path == "/billing/balance":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"data":{"balances":[{"show_name":"GLM-5.2","total_units":3000000,"used_units":0,"remaining_units":3000000,"available_units":3000000}]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer fakeUpstream.Close()

	cfg := testConfig(t)
	cfg.UpstreamURL = fakeUpstream.URL + "/messages"
	cfg.BillingBaseURL = fakeUpstream.URL + "/billing"
	admin, _ := state.NewAdminStore(cfg.AdminPath, 3005, state.ThinkingSettings{Enabled: true, BudgetTokens: 32000, Effort: "max"})
	accountStore, _ := accounts.NewStore(cfg.CredentialsPath, cfg.CredentialSecret)
	_, _ = accountStore.Upsert(accounts.User{UserID: "one"}, "token-one", "")
	_, _ = accountStore.Upsert(accounts.User{UserID: "two"}, "token-two", "")
	loader := upstream.NewLoader(cfg, accountStore)
	quotaService := quota.New(cfg)
	bridge := captcha.NewBridge(cfg)
	browser := captcha.NewBrowserManager(cfg, 3005)
	server := api.New(cfg, 3005, admin, accountStore, auth.New(cfg, accountStore), quotaService, accountpool.New(cfg, accountStore, loader, quotaService), loader, bridge, browser, proxy.New(cfg, bridge))
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	body := []byte(`{"model":"glm-5.2","messages":[{"role":"user","content":"oi"}],"stream":false}`)
	response, err := http.Post(httpServer.URL+"/v1/chat/completions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected auth-rotated request to succeed, got %d", response.StatusCode)
	}
	if strings.Join(chatTokens, ",") != "Bearer token-one,Bearer token-two" {
		t.Fatalf("request did not rotate on auth failure: %+v", chatTokens)
	}
}

func TestChatStopsImmediatelyWhenNetworkIsWAFBlocked(t *testing.T) {
	var chatTokens []string
	fakeUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/messages":
			chatTokens = append(chatTokens, r.Header.Get("Authorization"))
			w.Header().Set("Server", "ESA")
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusMethodNotAllowed)
			_, _ = w.Write([]byte(`<!doctypehtml><html><body>The requested URL may cause potential security risks. errors.aliyun.com</body></html>`))
		case r.URL.Path == "/billing/current":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"data":{"plans":[]}}`))
		case r.URL.Path == "/billing/balance":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"data":{"balances":[{"show_name":"GLM-5.2","total_units":3000000,"used_units":0,"remaining_units":3000000,"available_units":3000000}]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer fakeUpstream.Close()

	cfg := testConfig(t)
	cfg.UpstreamURL = fakeUpstream.URL + "/messages"
	cfg.BillingBaseURL = fakeUpstream.URL + "/billing"
	cfg.AccountCreatorEnabled = false
	admin, _ := state.NewAdminStore(cfg.AdminPath, 3005, state.ThinkingSettings{Enabled: true, BudgetTokens: 32000, Effort: "max"})
	accountStore, _ := accounts.NewStore(cfg.CredentialsPath, cfg.CredentialSecret)
	_, _ = accountStore.Upsert(accounts.User{UserID: "one"}, "token-one", "")
	_, _ = accountStore.Upsert(accounts.User{UserID: "two"}, "token-two", "")
	loader := upstream.NewLoader(cfg, accountStore)
	quotaService := quota.New(cfg)
	bridge := captcha.NewBridge(cfg)
	browser := captcha.NewBrowserManager(cfg, 3005)
	server := api.New(cfg, 3005, admin, accountStore, auth.New(cfg, accountStore), quotaService, accountpool.New(cfg, accountStore, loader, quotaService), loader, bridge, browser, proxy.New(cfg, bridge))
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	body := []byte(`{"model":"glm-5.2","messages":[{"role":"user","content":"oi"}],"stream":false}`)
	response, err := http.Post(httpServer.URL+"/v1/chat/completions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var payload map[string]map[string]any
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 WAF status, got %d", response.StatusCode)
	}
	if payload["error"]["type"] != "zcode_waf_block" || payload["error"]["code"] != "esa_405" {
		t.Fatalf("expected WAF payload, got %+v", payload)
	}
	if len(chatTokens) != 1 || chatTokens[0] != "Bearer token-one" {
		t.Fatalf("WAF block should stop before rotating accounts: %+v", chatTokens)
	}

	logsResponse, err := http.Get(httpServer.URL + "/api/admin/logs")
	if err != nil {
		t.Fatal(err)
	}
	var logs map[string]any
	if err := json.NewDecoder(logsResponse.Body).Decode(&logs); err != nil {
		t.Fatal(err)
	}
	logsResponse.Body.Close()
	found := false
	for _, raw := range logs["data"].([]any) {
		entry := raw.(map[string]any)
		if entry["event"] == "network.waf_blocked" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected network.waf_blocked log, got %+v", logs)
	}
}

func TestChatRotatesAccountAfterPerAccountStaleLimit(t *testing.T) {
	var chatTokens []string
	fakeUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/messages":
			token := r.Header.Get("Authorization")
			chatTokens = append(chatTokens, token)
			w.Header().Set("Content-Type", "text/event-stream")
			if token == "Bearer token-one" {
				_, _ = w.Write([]byte("data: [DONE]\n\n"))
				return
			}
			_, _ = w.Write([]byte("data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\n"))
			_, _ = w.Write([]byte("data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		case r.URL.Path == "/billing/current":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"data":{"plans":[]}}`))
		case r.URL.Path == "/billing/balance":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"data":{"balances":[{"show_name":"GLM-5.2","total_units":3000000,"used_units":0,"remaining_units":3000000,"available_units":3000000}]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer fakeUpstream.Close()

	cfg := testConfig(t)
	cfg.RetryMaxAttempts = 11
	cfg.UpstreamURL = fakeUpstream.URL + "/messages"
	cfg.BillingBaseURL = fakeUpstream.URL + "/billing"
	admin, _ := state.NewAdminStore(cfg.AdminPath, 3005, state.ThinkingSettings{Enabled: true, BudgetTokens: 32000, Effort: "max"})
	accountStore, _ := accounts.NewStore(cfg.CredentialsPath, cfg.CredentialSecret)
	_, _ = accountStore.Upsert(accounts.User{UserID: "one"}, "token-one", "")
	_, _ = accountStore.Upsert(accounts.User{UserID: "two"}, "token-two", "")
	loader := upstream.NewLoader(cfg, accountStore)
	quotaService := quota.New(cfg)
	bridge := captcha.NewBridge(cfg)
	browser := captcha.NewBrowserManager(cfg, 3005)
	server := api.New(cfg, 3005, admin, accountStore, auth.New(cfg, accountStore), quotaService, accountpool.New(cfg, accountStore, loader, quotaService), loader, bridge, browser, proxy.New(cfg, bridge))
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	body := []byte(`{"model":"glm-5.2","messages":[{"role":"user","content":"oi"}],"stream":false}`)
	response, err := http.Post(httpServer.URL+"/v1/chat/completions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected stale-rotated request to succeed, got %d", response.StatusCode)
	}
	if strings.Join(chatTokens, ",") != "Bearer token-one,Bearer token-one,Bearer token-one,Bearer token-one,Bearer token-two" {
		t.Fatalf("unexpected per-account retry/rotation pattern: %+v", chatTokens)
	}

	logsResponse, err := http.Get(httpServer.URL + "/api/admin/logs")
	if err != nil {
		t.Fatal(err)
	}
	var logs map[string]any
	if err := json.NewDecoder(logsResponse.Body).Decode(&logs); err != nil {
		t.Fatal(err)
	}
	logsResponse.Body.Close()
	found := false
	for _, raw := range logs["data"].([]any) {
		entry := raw.(map[string]any)
		if entry["event"] == "account.stale_rotated" && strings.Contains(entry["message"].(string), "4 tentativa") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected account.stale_rotated log, got %+v", logs)
	}
}

func TestChatRotatesAccountAfterUnknownUpstreamError(t *testing.T) {
	var chatTokens []string
	fakeUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/messages":
			token := r.Header.Get("Authorization")
			chatTokens = append(chatTokens, token)
			if token == "Bearer token-one" {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\n"))
			_, _ = w.Write([]byte("data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		case r.URL.Path == "/billing/current":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"data":{"plans":[]}}`))
		case r.URL.Path == "/billing/balance":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"data":{"balances":[{"show_name":"GLM-5.2","total_units":3000000,"used_units":0,"remaining_units":3000000,"available_units":3000000}]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer fakeUpstream.Close()

	cfg := testConfig(t)
	cfg.UpstreamURL = fakeUpstream.URL + "/messages"
	cfg.BillingBaseURL = fakeUpstream.URL + "/billing"
	admin, _ := state.NewAdminStore(cfg.AdminPath, 3005, state.ThinkingSettings{Enabled: true, BudgetTokens: 32000, Effort: "max"})
	accountStore, _ := accounts.NewStore(cfg.CredentialsPath, cfg.CredentialSecret)
	_, _ = accountStore.Upsert(accounts.User{UserID: "one"}, "token-one", "")
	_, _ = accountStore.Upsert(accounts.User{UserID: "two"}, "token-two", "")
	loader := upstream.NewLoader(cfg, accountStore)
	quotaService := quota.New(cfg)
	bridge := captcha.NewBridge(cfg)
	browser := captcha.NewBrowserManager(cfg, 3005)
	server := api.New(cfg, 3005, admin, accountStore, auth.New(cfg, accountStore), quotaService, accountpool.New(cfg, accountStore, loader, quotaService), loader, bridge, browser, proxy.New(cfg, bridge))
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	body := []byte(`{"model":"glm-5.2","messages":[{"role":"user","content":"oi"}],"stream":false}`)
	response, err := http.Post(httpServer.URL+"/v1/chat/completions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected unknown-upstream rotation to succeed, got %d", response.StatusCode)
	}
	if strings.Join(chatTokens, ",") != "Bearer token-one,Bearer token-two" {
		t.Fatalf("unexpected account rotation pattern: %+v", chatTokens)
	}

	logsResponse, err := http.Get(httpServer.URL + "/api/admin/logs")
	if err != nil {
		t.Fatal(err)
	}
	var logs map[string]any
	if err := json.NewDecoder(logsResponse.Body).Decode(&logs); err != nil {
		t.Fatal(err)
	}
	logsResponse.Body.Close()
	found := false
	for _, raw := range logs["data"].([]any) {
		entry := raw.(map[string]any)
		if entry["event"] == "account.unknown_upstream_rotated" && strings.Contains(entry["message"].(string), "status=403") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected unknown upstream rotation log, got %+v", logs)
	}
}

func TestChatRetriesStaleAccountAfterAllAccountsCooldown(t *testing.T) {
	var chatTokens []string
	fakeUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/messages":
			token := r.Header.Get("Authorization")
			chatTokens = append(chatTokens, token)
			w.Header().Set("Content-Type", "text/event-stream")
			if len(chatTokens) <= 8 {
				_, _ = w.Write([]byte("data: [DONE]\n\n"))
				return
			}
			_, _ = w.Write([]byte("data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\n"))
			_, _ = w.Write([]byte("data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		case r.URL.Path == "/billing/current":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"data":{"plans":[]}}`))
		case r.URL.Path == "/billing/balance":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"data":{"balances":[{"show_name":"GLM-5.2","total_units":3000000,"used_units":0,"remaining_units":3000000,"available_units":3000000}]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer fakeUpstream.Close()

	cfg := testConfig(t)
	cfg.RetryMaxAttempts = 11
	cfg.AccountRetryCooldown = time.Millisecond
	cfg.UpstreamURL = fakeUpstream.URL + "/messages"
	cfg.BillingBaseURL = fakeUpstream.URL + "/billing"
	admin, _ := state.NewAdminStore(cfg.AdminPath, 3005, state.ThinkingSettings{Enabled: true, BudgetTokens: 32000, Effort: "max"})
	accountStore, _ := accounts.NewStore(cfg.CredentialsPath, cfg.CredentialSecret)
	_, _ = accountStore.Upsert(accounts.User{UserID: "one"}, "token-one", "")
	_, _ = accountStore.Upsert(accounts.User{UserID: "two"}, "token-two", "")
	loader := upstream.NewLoader(cfg, accountStore)
	quotaService := quota.New(cfg)
	bridge := captcha.NewBridge(cfg)
	browser := captcha.NewBrowserManager(cfg, 3005)
	server := api.New(cfg, 3005, admin, accountStore, auth.New(cfg, accountStore), quotaService, accountpool.New(cfg, accountStore, loader, quotaService), loader, bridge, browser, proxy.New(cfg, bridge))
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	body := []byte(`{"model":"glm-5.2","messages":[{"role":"user","content":"oi"}],"stream":false}`)
	response, err := http.Post(httpServer.URL+"/v1/chat/completions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected cooldown retry to succeed, got %d", response.StatusCode)
	}
	pattern := strings.Join(chatTokens, ",")
	if pattern != "Bearer token-one,Bearer token-one,Bearer token-one,Bearer token-one,Bearer token-two,Bearer token-two,Bearer token-two,Bearer token-two,Bearer token-one" {
		t.Fatalf("unexpected cooldown retry pattern: %s", pattern)
	}

	logsResponse, err := http.Get(httpServer.URL + "/api/admin/logs")
	if err != nil {
		t.Fatal(err)
	}
	var logs map[string]any
	if err := json.NewDecoder(logsResponse.Body).Decode(&logs); err != nil {
		t.Fatal(err)
	}
	logsResponse.Body.Close()
	releaseFound := false
	for _, raw := range logs["data"].([]any) {
		entry := raw.(map[string]any)
		if entry["event"] == "account.retry_cooldown_released" {
			releaseFound = true
		}
	}
	if !releaseFound {
		t.Fatalf("expected cooldown release log, got %+v", logs)
	}
}

func TestAdmissionConcurrencyErrorUsesFriendlyMessage(t *testing.T) {
	fakeUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/messages":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"message":"model admission concurrency limit exceeded","type":"zcode_upstream_error","code":"3001"}}`))
		case r.URL.Path == "/billing/current":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"data":{"plans":[]}}`))
		case r.URL.Path == "/billing/balance":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"data":{"balances":[{"show_name":"GLM-5.2","total_units":3000000,"used_units":0,"remaining_units":3000000,"available_units":3000000}]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer fakeUpstream.Close()

	cfg := testConfig(t)
	cfg.Authorization = "Bearer test-token"
	cfg.UpstreamURL = fakeUpstream.URL + "/messages"
	cfg.BillingBaseURL = fakeUpstream.URL + "/billing"
	cfg.RetryMaxAttempts = 1
	admin, _ := state.NewAdminStore(cfg.AdminPath, 3005, state.ThinkingSettings{Enabled: true, BudgetTokens: 32000, Effort: "max"})
	accountStore, _ := accounts.NewStore(cfg.CredentialsPath, cfg.CredentialSecret)
	loader := upstream.NewLoader(cfg, accountStore)
	quotaService := quota.New(cfg)
	bridge := captcha.NewBridge(cfg)
	browser := captcha.NewBrowserManager(cfg, 3005)
	server := api.New(cfg, 3005, admin, accountStore, auth.New(cfg, accountStore), quotaService, accountpool.New(cfg, accountStore, loader, quotaService), loader, bridge, browser, proxy.New(cfg, bridge))
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	body := []byte(`{"model":"glm-5.2","messages":[{"role":"user","content":"oi"}],"stream":false}`)
	response, err := http.Post(httpServer.URL+"/v1/chat/completions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var payload map[string]map[string]any
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	errorPayload := payload["error"]
	if response.StatusCode != http.StatusTooManyRequests || errorPayload["message"] != "Opa, os servidores da Z.ai estao cheios no momento. Tente novamente em instantes." || errorPayload["technical_message"] != "model admission concurrency limit exceeded" {
		t.Fatalf("unexpected friendly error: status=%d payload=%+v", response.StatusCode, errorPayload)
	}
}

func TestAPIStateReorderAndLogs(t *testing.T) {
	cfg := testConfig(t)
	admin, _ := state.NewAdminStore(cfg.AdminPath, 3005, state.ThinkingSettings{Enabled: true, BudgetTokens: 32000, Effort: "max"})
	accountStore, _ := accounts.NewStore(cfg.CredentialsPath, cfg.CredentialSecret)
	_, _ = accountStore.Upsert(accounts.User{UserID: "one"}, "token-one", "")
	_, _ = accountStore.Upsert(accounts.User{UserID: "two"}, "token-two", "")
	loader := upstream.NewLoader(cfg, accountStore)
	quotaService := quota.New(cfg)
	bridge := captcha.NewBridge(cfg)
	browser := captcha.NewBrowserManager(cfg, 3005)
	server := api.New(cfg, 3005, admin, accountStore, auth.New(cfg, accountStore), quotaService, accountpool.New(cfg, accountStore, loader, quotaService), loader, bridge, browser, proxy.New(cfg, bridge))
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	activate, _ := http.NewRequest(http.MethodPost, httpServer.URL+"/api/admin/accounts/two/activate", nil)
	response, err := http.DefaultClient.Do(activate)
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("manual activation failed: %v status=%v", err, response.StatusCode)
	}
	var activatePayload map[string]any
	if err := json.NewDecoder(response.Body).Decode(&activatePayload); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if _, ok := activatePayload["zcode"]; ok {
		t.Fatalf("manual activation must not apply or report ZCode sync: %+v", activatePayload)
	}
	if active := accountStore.Active(); active == nil || active.ID != "two" {
		t.Fatalf("manual activation was not persisted: %+v", active)
	}

	reorder, _ := http.NewRequest(http.MethodPut, httpServer.URL+"/api/admin/accounts/order", strings.NewReader(`{"accountIds":["two","one"]}`))
	reorder.Header.Set("Content-Type", "application/json")
	response, err = http.DefaultClient.Do(reorder)
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("reorder failed: %v status=%v", err, response.StatusCode)
	}
	response.Body.Close()
	if accountStore.Accounts()[0].ID != "two" {
		t.Fatalf("account order was not updated: %+v", accountStore.Accounts())
	}
	if active := accountStore.Active(); active == nil || active.ID != "two" {
		t.Fatalf("reorder changed active account unexpectedly: %+v", active)
	}

	stop, _ := http.NewRequest(http.MethodPatch, httpServer.URL+"/api/admin/settings", strings.NewReader(`{"apiEnabled":false}`))
	stop.Header.Set("Content-Type", "application/json")
	response, _ = http.DefaultClient.Do(stop)
	response.Body.Close()
	modelsResponse, _ := http.Get(httpServer.URL + "/v1/models")
	if modelsResponse.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected stopped API to return 503, got %d", modelsResponse.StatusCode)
	}
	modelsResponse.Body.Close()

	start, _ := http.NewRequest(http.MethodPatch, httpServer.URL+"/api/admin/settings", strings.NewReader(`{"apiEnabled":true}`))
	start.Header.Set("Content-Type", "application/json")
	response, _ = http.DefaultClient.Do(start)
	response.Body.Close()
	modelsResponse, _ = http.Get(httpServer.URL + "/v1/models")
	if modelsResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected restarted API to return 200, got %d", modelsResponse.StatusCode)
	}
	modelsResponse.Body.Close()

	logsResponse, _ := http.Get(httpServer.URL + "/api/admin/logs")
	var logs map[string]any
	_ = json.NewDecoder(logsResponse.Body).Decode(&logs)
	logsResponse.Body.Close()
	if len(logs["data"].([]any)) < 3 {
		t.Fatalf("expected real administrative logs, got %+v", logs)
	}
}

func TestStartupLegacyAuthMigrationRemovesOnlyAccountsWithoutCodingPlanKey(t *testing.T) {
	cfg := testConfig(t)
	admin, _ := state.NewAdminStore(cfg.AdminPath, 3005, state.ThinkingSettings{Enabled: true, BudgetTokens: 32000, Effort: "max"})
	accountStore, _ := accounts.NewStore(cfg.CredentialsPath, cfg.CredentialSecret)
	_, _ = accountStore.Upsert(accounts.User{UserID: "legacy", Email: "legacy@example.test"}, "legacy-token", "legacy-zai-token")
	_, _ = accountStore.Upsert(accounts.User{UserID: "current", Email: "current@example.test"}, "current-token", "current-business-token")
	_, _ = accountStore.UpdateCodingPlanAPIKey("current", "coding-plan-key")
	loader := upstream.NewLoader(cfg, accountStore)
	quotaService := quota.New(cfg)
	bridge := captcha.NewBridge(cfg)
	browser := captcha.NewBrowserManager(cfg, 3005)
	server := api.New(cfg, 3005, admin, accountStore, auth.New(cfg, accountStore), quotaService, accountpool.New(cfg, accountStore, loader, quotaService), loader, bridge, browser, proxy.New(cfg, bridge))

	report := server.RemoveLegacyAuthAccountsOnce(context.Background())
	if report.Total != 1 || report.Removed != 1 || report.Failed != 0 {
		t.Fatalf("expected one legacy account removed, got %+v", report)
	}
	if accountStore.Get("legacy") != nil {
		t.Fatal("legacy account should have been removed")
	}
	if accountStore.Get("current") == nil {
		t.Fatal("current account with codingPlanApiKey should have been kept")
	}

	second := server.RemoveLegacyAuthAccountsOnce(context.Background())
	if second.Total != 0 || second.Removed != 0 || second.Skipped != 1 {
		t.Fatalf("expected completed migration marker to skip second run, got %+v", second)
	}
}

func TestAccountRepairRemovesBrokenAccountWithEmptyStartPlan(t *testing.T) {
	fakeUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/billing/current":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"data":{"plans":[]}}`))
		case "/billing/balance":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"data":{"balances":[]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer fakeUpstream.Close()

	cfg := testConfig(t)
	cfg.BillingBaseURL = fakeUpstream.URL + "/billing"
	admin, _ := state.NewAdminStore(cfg.AdminPath, 3005, state.ThinkingSettings{Enabled: true, BudgetTokens: 32000, Effort: "max"})
	accountStore, _ := accounts.NewStore(cfg.CredentialsPath, cfg.CredentialSecret)
	_, _ = accountStore.Upsert(accounts.User{UserID: "broken", Email: "broken@example.test"}, "broken-token", "")
	loader := upstream.NewLoader(cfg, accountStore)
	quotaService := quota.New(cfg)
	bridge := captcha.NewBridge(cfg)
	browser := captcha.NewBrowserManager(cfg, 3005)
	server := api.New(cfg, 3005, admin, accountStore, auth.New(cfg, accountStore), quotaService, accountpool.New(cfg, accountStore, loader, quotaService), loader, bridge, browser, proxy.New(cfg, bridge))
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	response, err := http.Post(httpServer.URL+"/api/admin/accounts/repair", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var report map[string]any
	if err := json.NewDecoder(response.Body).Decode(&report); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || int(report["skipped"].(float64)) != 0 || int(report["removed"].(float64)) != 1 {
		t.Fatalf("expected one removed account, status=%d report=%+v", response.StatusCode, report)
	}
	if accountStore.Get("broken") != nil {
		t.Fatal("broken account should have been removed")
	}
	logsResponse, _ := http.Get(httpServer.URL + "/api/admin/logs")
	var logs map[string]any
	_ = json.NewDecoder(logsResponse.Body).Decode(&logs)
	logsResponse.Body.Close()
	found := false
	for _, raw := range logs["data"].([]any) {
		entry := raw.(map[string]any)
		if entry["event"] == "account_repair.account_removed" && strings.Contains(entry["message"].(string), "Start Plan") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected account repair removal log, got %+v", logs)
	}

	statusResponse, _ := http.Get(httpServer.URL + "/api/admin/accounts/repair/status")
	var status map[string]any
	_ = json.NewDecoder(statusResponse.Body).Decode(&status)
	statusResponse.Body.Close()
	if status["active"].(bool) || int(status["processed"].(float64)) != 1 || int(status["removed"].(float64)) != 1 {
		t.Fatalf("expected completed repair progress with removal, got %+v", status)
	}
}

func TestAccountRepairKeepsExhaustedAccountWithRealBalance(t *testing.T) {
	fakeUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/billing/current":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"data":{"plans":[]}}`))
		case "/billing/balance":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"data":{"balances":[{"show_name":"GLM-5.2","total_units":3000000,"used_units":3000000,"remaining_units":0,"available_units":0}]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer fakeUpstream.Close()

	cfg := testConfig(t)
	cfg.BillingBaseURL = fakeUpstream.URL + "/billing"
	admin, _ := state.NewAdminStore(cfg.AdminPath, 3005, state.ThinkingSettings{Enabled: true, BudgetTokens: 32000, Effort: "max"})
	accountStore, _ := accounts.NewStore(cfg.CredentialsPath, cfg.CredentialSecret)
	public, _ := accountStore.Upsert(accounts.User{UserID: "exhausted", Email: "exhausted@example.test"}, "exhausted-token", "business-token")
	_, _ = accountStore.UpdateCodingPlanAPIKey(public.ID, "coding-key.secret")
	loader := upstream.NewLoader(cfg, accountStore)
	quotaService := quota.New(cfg)
	bridge := captcha.NewBridge(cfg)
	browser := captcha.NewBrowserManager(cfg, 3005)
	server := api.New(cfg, 3005, admin, accountStore, auth.New(cfg, accountStore), quotaService, accountpool.New(cfg, accountStore, loader, quotaService), loader, bridge, browser, proxy.New(cfg, bridge))
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	response, err := http.Post(httpServer.URL+"/api/admin/accounts/repair", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var report map[string]any
	if err := json.NewDecoder(response.Body).Decode(&report); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || int(report["healthy"].(float64)) != 1 || int(report["removed"].(float64)) != 0 {
		t.Fatalf("expected exhausted-but-real account to stay healthy, status=%d report=%+v", response.StatusCode, report)
	}
	if accountStore.Get("exhausted") == nil {
		t.Fatal("exhausted account with real balance should not be removed")
	}
}
