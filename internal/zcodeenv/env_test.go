package zcodeenv

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"glm5.2proxy/internal/accounts"
)

func TestCipherRoundTrip(t *testing.T) {
	t.Setenv("ZCODE_CREDENTIAL_SECRET", "test-secret")
	cipher := NewCipher(t.TempDir())
	encrypted, err := cipher.Encrypt("valor secreto")
	if err != nil {
		t.Fatal(err)
	}
	if encrypted == "valor secreto" {
		t.Fatal("expected encrypted value")
	}
	decrypted, err := cipher.Decrypt(encrypted)
	if err != nil {
		t.Fatal(err)
	}
	if decrypted != "valor secreto" {
		t.Fatalf("unexpected decrypted value: %q", decrypted)
	}
}

func TestWriteCredentialsPreservesOtherKeys(t *testing.T) {
	t.Setenv("ZCODE_CREDENTIAL_SECRET", "test-secret")
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.json")
	if err := os.WriteFile(path, []byte(`{"other":"value"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	account := accounts.Account{
		User:            accounts.User{UserID: "u1", Email: "u1@example.com", Name: "User 1"},
		ZCodeJWTToken:   "jwt-token",
		ZAIAcccessToken: "access-token",
	}
	backup, err := writeCredentials(path, NewCipher(dir), account)
	if err != nil {
		t.Fatal(err)
	}
	if backup == "" {
		t.Fatal("expected backup path")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]string
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	if saved["other"] != "value" {
		t.Fatal("other credential key was not preserved")
	}
	plain, err := NewCipher(dir).Decrypt(saved[credentialJWTToken])
	if err != nil {
		t.Fatal(err)
	}
	if plain != "jwt-token" {
		t.Fatalf("unexpected jwt: %q", plain)
	}
}

func TestWriteCredentialsAllowsJWTOnlyAccount(t *testing.T) {
	t.Setenv("ZCODE_CREDENTIAL_SECRET", "test-secret")
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.json")
	account := accounts.Account{
		User:          accounts.User{UserID: "u1", Email: "u1@example.com", Name: "User 1"},
		ZCodeJWTToken: "jwt-token",
	}
	if _, err := writeCredentials(path, NewCipher(dir), account); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]string
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	if saved[credentialAccessToken] != "" {
		t.Fatal("did not expect access token credential for jwt-only account")
	}
	if saved[credentialJWTToken] == "" {
		t.Fatal("expected jwt credential")
	}
}

func TestDetectBridgeProxyBaseURL(t *testing.T) {
	raw := []byte(`;(()=>{if(globalThis.__GLM52_PROXY_BRIDGE__)return;const u="http://127.0.0.1:34115/api/admin/zcode/bridge";})();`)
	got := detectBridgeProxyBaseURL(raw)
	if got != "http://127.0.0.1:34115" {
		t.Fatalf("unexpected bridge proxy base URL: %q", got)
	}
}

func TestUpdateConfigWritesBothZCodeCodingProviders(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{
  "provider": {
    "builtin:zai-start-plan": {
      "enabled": false,
      "systemDisabledReason": "coding_plan_not_entitled",
      "options": {
        "baseURL": "https://wrong.example.test/anthropic",
        "other": "legacy-preserved"
      }
    },
    "builtin:zai-coding-plan": {
      "enabled": false,
      "systemDisabledReason": "coding_plan_not_entitled",
      "options": {
        "baseURL": "https://api.z.ai/api/anthropic"
      }
    }
  },
  "modelProviders": {
    "builtin:zai-start-plan": {
      "enabled": false,
      "options": {
        "other": "preserved"
      }
    }
  }
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, err := updateConfig(path, "jwt-token")
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected first config repair to change the file")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]any
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	providers := saved["modelProviders"].(map[string]any)
	for _, providerID := range codingPlanProviderIDs {
		providerConfig := codingPlanProviderConfigByID[providerID]
		provider := providers[providerID].(map[string]any)
		if provider["enabled"] != true {
			t.Fatalf("%s was not enabled", providerID)
		}
		options := provider["options"].(map[string]any)
		if options["apiKey"] != "jwt-token" {
			t.Fatalf("%s apiKey was not updated", providerID)
		}
		if options["baseURL"] != providerConfig.ModelBaseURL {
			t.Fatalf("%s baseURL mismatch: %#v", providerID, options["baseURL"])
		}
	}
	startOptions := providers["builtin:zai-start-plan"].(map[string]any)["options"].(map[string]any)
	if startOptions["other"] != "preserved" {
		t.Fatal("existing provider options were not preserved")
	}

	legacyProviders := saved["provider"].(map[string]any)
	for _, providerID := range codingPlanProviderIDs {
		providerConfig := codingPlanProviderConfigByID[providerID]
		provider := legacyProviders[providerID].(map[string]any)
		if provider["enabled"] != true {
			t.Fatalf("legacy %s was not enabled", providerID)
		}
		if _, ok := provider["systemDisabledReason"]; ok {
			t.Fatalf("legacy %s still has systemDisabledReason", providerID)
		}
		if _, ok := provider["apiKeyRequired"]; ok {
			t.Fatalf("legacy %s still has apiKeyRequired at the top level", providerID)
		}
		options := provider["options"].(map[string]any)
		if options["apiKey"] != "jwt-token" {
			t.Fatalf("legacy %s apiKey was not updated", providerID)
		}
		if options["apiKeyRequired"] != true {
			t.Fatalf("legacy %s options.apiKeyRequired was not enabled", providerID)
		}
		if options["baseURL"] != providerConfig.LegacyBaseURL {
			t.Fatalf("legacy %s baseURL mismatch: %#v", providerID, options["baseURL"])
		}
	}
	legacyStartOptions := legacyProviders["builtin:zai-start-plan"].(map[string]any)["options"].(map[string]any)
	if legacyStartOptions["other"] != "legacy-preserved" {
		t.Fatal("legacy provider options were not preserved")
	}
	changed, err = updateConfig(path, "jwt-token")
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("expected second config repair to be a no-op")
	}
}

func TestClearCodingPlanCacheRemovesStaleEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "coding-plan-cache.json")
	if err := os.WriteFile(path, []byte(`{
  "version": 1,
  "entryStatus": {
    "updatedAt": 123,
    "items": {
      "builtin:zai-start-plan": {
        "status": "unavailable",
        "reason": "coding_plan_not_entitled"
      },
      "builtin:zai-coding-plan": {
        "status": "unavailable",
        "reason": "coding_plan_not_entitled"
      },
      "builtin:bigmodel-start-plan": {
        "status": "unavailable",
        "reason": "coding_plan_not_authenticated"
      }
    }
  }
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, err := clearCodingPlanCache(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected coding plan cache to change")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]any
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	entryStatus := saved["entryStatus"].(map[string]any)
	items := entryStatus["items"].(map[string]any)
	for _, providerID := range codingPlanProviderIDs {
		if _, ok := items[providerID]; ok {
			t.Fatalf("%s entry was not cleared", providerID)
		}
	}
	if _, ok := items["builtin:bigmodel-start-plan"]; !ok {
		t.Fatal("unrelated coding plan cache entry should have been preserved")
	}
	if entryStatus["updatedAt"].(float64) <= 123 {
		t.Fatal("updatedAt was not refreshed")
	}
}

func TestClearCodingPlanCacheDoesNotRewriteAvailableEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "coding-plan-cache.json")
	content := `{
  "version": 1,
  "entryStatus": {
    "updatedAt": 123,
    "items": {
      "builtin:zai-start-plan": {
        "status": "available"
      },
      "builtin:zai-coding-plan": {
        "status": "available"
      }
    }
  }
}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, err := clearCodingPlanCache(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("available coding plan entries should not trigger cache repair")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != content {
		t.Fatal("cache should not be rewritten")
	}
}

func TestEnforceCodingPlanStateInEnvironmentRepairsFiles(t *testing.T) {
	dir := t.TempDir()
	env := Environment{
		HomeDir:        dir,
		DataDir:        dir,
		ConfigPath:     filepath.Join(dir, "config.json"),
		CodingPlanPath: filepath.Join(dir, "coding-plan-cache.json"),
	}
	account := accounts.Account{
		User:          accounts.User{UserID: "u1", Email: "u1@example.com", Name: "User 1"},
		ZCodeJWTToken: "jwt-token",
	}
	if err := os.WriteFile(env.ConfigPath, []byte(`{
  "provider": {
    "builtin:zai-start-plan": {
      "enabled": false,
      "systemDisabledReason": "coding_plan_not_entitled",
      "options": {}
    }
  },
  "modelProviders": {
    "builtin:zai-start-plan": {
      "enabled": false,
      "options": {}
    }
  }
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(env.CodingPlanPath, []byte(`{
  "version": 1,
  "entryStatus": {
    "updatedAt": 1,
    "items": {
      "builtin:zai-start-plan": {
        "status": "unavailable",
        "reason": "coding_plan_not_entitled"
      }
    }
  }
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, err := EnforceCodingPlanStateInEnvironment(env, account)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected enforce to repair zcode state")
	}
	raw, err := os.ReadFile(env.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]any
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	providers := saved["modelProviders"].(map[string]any)
	legacyProviders := saved["provider"].(map[string]any)
	for _, providerID := range codingPlanProviderIDs {
		providerConfig := codingPlanProviderConfigByID[providerID]
		provider := providers[providerID].(map[string]any)
		if provider["enabled"] != true {
			t.Fatalf("repaired %s was not enabled", providerID)
		}
		providerOptions := provider["options"].(map[string]any)
		if providerOptions["apiKey"] != "jwt-token" {
			t.Fatalf("repaired %s apiKey mismatch", providerID)
		}
		if providerOptions["baseURL"] != providerConfig.ModelBaseURL {
			t.Fatalf("repaired %s modelProviders baseURL mismatch: %#v", providerID, providerOptions["baseURL"])
		}
		legacyProvider := legacyProviders[providerID].(map[string]any)
		if legacyProvider["enabled"] != true {
			t.Fatalf("repaired legacy %s was not enabled", providerID)
		}
		if _, ok := legacyProvider["systemDisabledReason"]; ok {
			t.Fatalf("repaired legacy %s still has systemDisabledReason", providerID)
		}
		if _, ok := legacyProvider["apiKeyRequired"]; ok {
			t.Fatalf("repaired legacy %s still has apiKeyRequired at the top level", providerID)
		}
		legacyOptions := legacyProvider["options"].(map[string]any)
		if legacyOptions["apiKey"] != "jwt-token" {
			t.Fatalf("repaired legacy %s apiKey mismatch", providerID)
		}
		if legacyOptions["apiKeyRequired"] != true {
			t.Fatalf("repaired legacy %s options.apiKeyRequired mismatch", providerID)
		}
		if legacyOptions["baseURL"] != providerConfig.LegacyBaseURL {
			t.Fatalf("repaired legacy %s baseURL mismatch: %#v", providerID, legacyOptions["baseURL"])
		}
	}
	raw, err = os.ReadFile(env.CodingPlanPath)
	if err != nil {
		t.Fatal(err)
	}
	var cache map[string]any
	if err := json.Unmarshal(raw, &cache); err != nil {
		t.Fatal(err)
	}
	items := cache["entryStatus"].(map[string]any)["items"].(map[string]any)
	for _, providerID := range codingPlanProviderIDs {
		if _, ok := items[providerID]; ok {
			t.Fatalf("repaired cache still contains %s", providerID)
		}
	}
}

func TestEnforceCodingPlanStateNoopsWhenStartPlanAvailable(t *testing.T) {
	dir := t.TempDir()
	env := Environment{
		HomeDir:        dir,
		DataDir:        dir,
		ConfigPath:     filepath.Join(dir, "config.json"),
		CodingPlanPath: filepath.Join(dir, "coding-plan-cache.json"),
	}
	account := accounts.Account{
		User:          accounts.User{UserID: "u1", Email: "u1@example.com", Name: "User 1"},
		ZCodeJWTToken: "jwt-token",
	}
	if err := os.WriteFile(env.ConfigPath, []byte(`{
  "provider": {
    "builtin:zai-start-plan": {
      "enabled": true,
      "options": {
        "apiKey": "jwt-token",
        "baseURL": "https://zcode.z.ai/api/v1/zcode-plan/anthropic"
      }
    },
    "builtin:zai-coding-plan": {
      "enabled": true,
      "options": {
        "apiKey": "jwt-token",
        "baseURL": "https://api.z.ai/api/anthropic"
      }
    }
  },
  "modelProviders": {
    "builtin:zai-start-plan": {
      "enabled": true,
      "options": {
        "apiKey": "jwt-token",
        "baseURL": "https://zcode.z.ai/api/v1/zcode-plan/anthropic"
      }
    },
    "builtin:zai-coding-plan": {
      "enabled": true,
      "options": {
        "apiKey": "jwt-token",
        "baseURL": "https://api.z.ai/api/anthropic"
      }
    }
  }
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(env.CodingPlanPath, []byte(`{
  "version": 1,
  "entryStatus": {
    "updatedAt": 1,
    "items": {
      "builtin:zai-start-plan": {
        "status": "available"
      },
      "builtin:zai-coding-plan": {
        "status": "available"
      }
    }
  }
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, err := EnforceCodingPlanStateInEnvironment(env, account)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("available start plan should not be repaired")
	}
}
