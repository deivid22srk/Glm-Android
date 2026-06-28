package tests

import (
	"os"
	"path/filepath"
	"testing"

	"glm5.2proxy/internal/accounts"
	"glm5.2proxy/internal/upstream"
)

func TestUpstreamLoaderUsesNativeTemplateWithoutModelIO(t *testing.T) {
	cfg := testConfig(t)
	cfg.Authorization = "Bearer test"
	store, err := accounts.NewStore(cfg.CredentialsPath, cfg.CredentialSecret)
	if err != nil {
		t.Fatal(err)
	}

	loaded := upstream.NewLoader(cfg, store).Load(nil)
	if loaded.Source != "environment" {
		t.Fatalf("unexpected source without model-io: %s", loaded.Source)
	}
	if loaded.Endpoint != cfg.UpstreamURL {
		t.Fatalf("unexpected endpoint: %s", loaded.Endpoint)
	}
	system := loaded.BodyTemplate["system"].([]any)
	if len(system) != 3 {
		t.Fatalf("native body template must ship the ZCode system prompt: %+v", loaded.BodyTemplate)
	}
	if loaded.BaseHeaders["x-title"] != "Z Code@electron" || loaded.BaseHeaders["x-zcode-agent"] != "glm" {
		t.Fatalf("native ZCode identity headers missing: %+v", loaded.BaseHeaders)
	}
}

func TestUpstreamLoaderFallsBackToCapturedAuthorizationAndPreservesCapturedSession(t *testing.T) {
	cfg := testConfig(t)
	if err := os.MkdirAll(cfg.ModelIODir, 0o700); err != nil {
		t.Fatal(err)
	}
	record := `{"request":{"headers":{"Authorization":"Bearer captured","X-Title":"Z Code@electron","X-ZCode-Agent":"glm","x-session-id":"official-session"},"body":{"model":"GLM-5.2","stream":true,"system":[{"type":"text","text":"You are ZCode"}],"messages":[{"role":"user","content":[{"type":"text","text":"oi"}]}]}}}`
	if err := os.WriteFile(filepath.Join(cfg.ModelIODir, "model-io-test.jsonl"), []byte(record+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := accounts.NewStore(cfg.CredentialsPath, cfg.CredentialSecret)
	if err != nil {
		t.Fatal(err)
	}

	loaded := upstream.NewLoader(cfg, store).Load(nil)
	if loaded.BaseHeaders["authorization"] != "Bearer captured" {
		t.Fatalf("captured authorization should be a fallback when no account/env auth exists: %+v", loaded.BaseHeaders)
	}
	if loaded.BaseHeaders["x-title"] != "Z Code@electron" || loaded.BaseHeaders["x-zcode-agent"] != "glm" {
		t.Fatalf("official ZCode identity headers were not preserved: %+v", loaded.BaseHeaders)
	}
	if loaded.BaseHeaders["x-session-id"] != "official-session" {
		t.Fatalf("captured official session must be preserved for captcha-bound requests: %+v", loaded.BaseHeaders)
	}
	system := loaded.BodyTemplate["system"].([]any)
	if len(system) != 3 {
		t.Fatalf("native template should stay authoritative even when model-io exists: %+v", loaded.BodyTemplate)
	}
}

func TestUpstreamLoaderUsesStartPlanJWTForChatWhenAuxiliaryTokensExist(t *testing.T) {
	cfg := testConfig(t)
	store, err := accounts.NewStore(cfg.CredentialsPath, cfg.CredentialSecret)
	if err != nil {
		t.Fatal(err)
	}
	public, err := store.Upsert(accounts.User{UserID: "one", Email: "one@example.test"}, "jwt-token", "business-token")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateCodingPlanAPIKey(public.ID, "coding-key.secret"); err != nil {
		t.Fatal(err)
	}

	loaded := upstream.NewLoader(cfg, store).Load(store.Active())
	if loaded.Endpoint != cfg.UpstreamURL {
		t.Fatalf("chat should use Start Plan endpoint: %s", loaded.Endpoint)
	}
	if loaded.BaseHeaders["authorization"] != "Bearer jwt-token" {
		t.Fatalf("chat should use ZCode JWT bearer auth: %+v", loaded.BaseHeaders)
	}
}

func TestUpstreamLoaderDoesNotUseCodingPlanAPIKeyForStartPlanChat(t *testing.T) {
	cfg := testConfig(t)
	store, err := accounts.NewStore(cfg.CredentialsPath, cfg.CredentialSecret)
	if err != nil {
		t.Fatal(err)
	}
	public, err := store.Upsert(accounts.User{UserID: "one", Email: "one@example.test"}, "jwt-token", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateCodingPlanAPIKey(public.ID, "coding-key.secret"); err != nil {
		t.Fatal(err)
	}

	loaded := upstream.NewLoader(cfg, store).Load(store.Active())
	if loaded.BaseHeaders["authorization"] != "Bearer jwt-token" {
		t.Fatalf("chat should keep Start Plan JWT instead of Coding Plan API key: %+v", loaded.BaseHeaders)
	}
}

func TestUpstreamLoaderUsesStartPlanAuthorizationForQuota(t *testing.T) {
	cfg := testConfig(t)
	store, err := accounts.NewStore(cfg.CredentialsPath, cfg.CredentialSecret)
	if err != nil {
		t.Fatal(err)
	}
	public, err := store.Upsert(accounts.User{UserID: "one", Email: "one@example.test"}, "jwt-token", "business-token")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateCodingPlanAPIKey(public.ID, "coding-key.secret"); err != nil {
		t.Fatal(err)
	}

	loader := upstream.NewLoader(cfg, store)
	chat := loader.Load(store.Active())
	quota := loader.LoadStartPlanQuota(store.Active())
	if chat.BaseHeaders["authorization"] != "Bearer jwt-token" {
		t.Fatalf("chat config should use Start Plan JWT: %+v", chat.BaseHeaders)
	}
	if quota.BaseHeaders["authorization"] != "Bearer jwt-token" {
		t.Fatalf("quota config should use start plan auth: %+v", quota.BaseHeaders)
	}
	if quota.Endpoint == cfg.ZAICodingPlanUpstreamURL {
		t.Fatalf("quota config should not switch to coding plan endpoint: %s", quota.Endpoint)
	}
}
