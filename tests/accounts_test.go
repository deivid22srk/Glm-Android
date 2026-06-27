package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"glm5.2proxy/internal/accounts"
)

func TestAccountStoreEncryptionOrderingAndActivation(t *testing.T) {
	cfg := testConfig(t)
	store, err := accounts.NewStore(cfg.CredentialsPath, cfg.CredentialSecret)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Upsert(accounts.User{UserID: "one", Email: "one@example.test"}, "token-one", "zai-one"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Upsert(accounts.User{UserID: "two", Email: "two@example.test"}, "token-two", "zai-two"); err != nil {
		t.Fatal(err)
	}
	active, listed := store.Public()
	if active != "one" || listed[0].Label != "Conta 1" || listed[1].Label != "Conta 2" {
		t.Fatalf("unexpected account order: active=%s accounts=%+v", active, listed)
	}
	if _, err := store.Activate("two"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(cfg.CredentialsPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "token-one") || strings.Contains(string(raw), "zai-two") {
		t.Fatal("credential file leaked plaintext secrets")
	}
	reloaded, err := accounts.NewStore(cfg.CredentialsPath, cfg.CredentialSecret)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Active().ID != "two" || len(reloaded.Accounts()) != 2 {
		t.Fatalf("store did not survive reload: %+v", reloaded.Active())
	}
}

func TestAccountStoreRemoveWritesEncryptedBackup(t *testing.T) {
	cfg := testConfig(t)
	store, err := accounts.NewStore(cfg.CredentialsPath, cfg.CredentialSecret)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Upsert(accounts.User{UserID: "one", Email: "one@example.test"}, "token-one", "zai-one"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Upsert(accounts.User{UserID: "two", Email: "two@example.test"}, "token-two", "zai-two"); err != nil {
		t.Fatal(err)
	}
	removed, err := store.Remove("one")
	if err != nil {
		t.Fatal(err)
	}
	if !removed {
		t.Fatal("expected account to be removed")
	}
	backups, err := filepath.Glob(filepath.Join(filepath.Dir(cfg.CredentialsPath), "backups", filepath.Base(cfg.CredentialsPath)+".*.bak"))
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 1 {
		t.Fatalf("expected one encrypted backup, got %d: %+v", len(backups), backups)
	}
	raw, err := os.ReadFile(backups[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "token-one") || strings.Contains(string(raw), "zai-two") {
		t.Fatal("backup leaked plaintext secrets")
	}
}

func TestAccountStoreReorderPersistsQueueOrder(t *testing.T) {
	cfg := testConfig(t)
	store, err := accounts.NewStore(cfg.CredentialsPath, cfg.CredentialSecret)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"one", "two", "three"} {
		if _, err := store.Upsert(accounts.User{UserID: id}, "token-"+id, ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.Activate("two"); err != nil {
		t.Fatal(err)
	}
	if err := store.Reorder([]string{"three", "one", "two"}); err != nil {
		t.Fatal(err)
	}
	activeID, public := store.Public()
	if public[0].ID != "three" || public[0].QueuePosition != 1 || public[0].Label != "Conta 1" {
		t.Fatalf("unexpected reordered queue: %+v", public)
	}
	if activeID != "two" || public[2].ID != "two" || !public[2].Active {
		t.Fatalf("reorder must preserve manually active account: active=%s public=%+v", activeID, public)
	}
	reloaded, err := accounts.NewStore(cfg.CredentialsPath, cfg.CredentialSecret)
	if err != nil {
		t.Fatal(err)
	}
	got := reloaded.Accounts()
	if got[0].ID != "three" || got[1].ID != "one" || got[2].ID != "two" {
		t.Fatalf("reordered queue was not persisted: %+v", got)
	}
	if reloaded.Active().ID != "two" {
		t.Fatalf("active account did not survive reorder reload: %+v", reloaded.Active())
	}
}
