package tests

import (
	"path/filepath"
	"testing"

	"glm5.2proxy/internal/config"
)

func TestConfigDefaultMinAvailableReservesOneThinkingRequest(t *testing.T) {
	t.Setenv("ZCODE_PROXY_DATA_DIR", filepath.Join(t.TempDir(), "data"))
	t.Setenv("ZCODE_MAX_TOKENS", "64000")
	t.Setenv("ZCODE_THINKING", "true")
	t.Setenv("ZCODE_THINKING_BUDGET", "32000")
	t.Setenv("ZCODE_ACCOUNT_MIN_AVAILABLE_UNITS", "")

	cfg := config.Load()
	if cfg.AccountMinAvailable != 96000 {
		t.Fatalf("expected min available to reserve max output plus thinking budget, got %d", cfg.AccountMinAvailable)
	}
}

func TestConfigMinAvailableEnvOverride(t *testing.T) {
	t.Setenv("ZCODE_PROXY_DATA_DIR", filepath.Join(t.TempDir(), "data"))
	t.Setenv("ZCODE_ACCOUNT_MIN_AVAILABLE_UNITS", "120000")

	cfg := config.Load()
	if cfg.AccountMinAvailable != 120000 {
		t.Fatalf("expected env override, got %d", cfg.AccountMinAvailable)
	}
}
