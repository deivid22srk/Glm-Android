package tests

import (
	"testing"

	"glm5.2proxy/internal/state"
)

func TestAdminSettingsThinkingAndAPIKeys(t *testing.T) {
	cfg := testConfig(t)
	store, err := state.NewAdminStore(cfg.AdminPath, 3005, state.ThinkingSettings{Enabled: true, BudgetTokens: 32000, Effort: "max"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetPort(3010); err != nil {
		t.Fatal(err)
	}
	override := state.ThinkingSettings{Enabled: true, BudgetTokens: 8000, Effort: "high"}
	if err := store.SetAccountThinking("account-1", &override); err != nil {
		t.Fatal(err)
	}
	if store.ThinkingFor("account-1") != override {
		t.Fatal("account thinking override was not applied")
	}
	key, secret, err := store.CreateAPIKey("IDE")
	if err != nil {
		t.Fatal(err)
	}
	if !store.ValidateAPIKey("Bearer "+secret) ||
		!store.ValidateAPIKey("bearer   "+secret) ||
		!store.ValidateAPIKey(secret) ||
		store.ValidateAPIKey("wrong") {
		t.Fatal("API key validation failed")
	}
	public := store.PublicSnapshot()
	if public["port"] != 3010 || public["apiKeyRequired"] != true {
		t.Fatalf("unexpected public settings: %+v", public)
	}
	if !store.DeleteAPIKey(key.ID) {
		t.Fatal("API key was not deleted")
	}
}
