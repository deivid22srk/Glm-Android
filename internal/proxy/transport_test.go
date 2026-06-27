package proxy

import (
	"testing"

	"glm5.2proxy/internal/config"
)

func TestPickDialHostUsesDirectBackendForConfiguredHost(t *testing.T) {
	cfg := config.Config{
		TransportDirectBackend: true,
		TransportDirectHost:    "zcode.z.ai",
		TransportDirectIPs:     []string{"8.8.8.8"},
	}

	host, direct := pickDialHost(cfg, "zcode.z.ai")
	if !direct {
		t.Fatal("expected direct backend selection")
	}
	if host != "8.8.8.8" {
		t.Fatalf("unexpected direct backend host: %s", host)
	}
}

func TestPickDialHostLeavesOtherHostsUntouched(t *testing.T) {
	cfg := config.Config{
		TransportDirectBackend: true,
		TransportDirectHost:    "zcode.z.ai",
		TransportDirectIPs:     []string{"8.8.8.8"},
	}

	host, direct := pickDialHost(cfg, "api.z.ai")
	if direct {
		t.Fatal("did not expect direct backend for another host")
	}
	if host != "api.z.ai" {
		t.Fatalf("unexpected dial host: %s", host)
	}
}

func TestPickDialHostRespectsDisabledFlag(t *testing.T) {
	cfg := config.Config{
		TransportDirectBackend: false,
		TransportDirectHost:    "zcode.z.ai",
		TransportDirectIPs:     []string{"8.8.8.8"},
	}

	host, direct := pickDialHost(cfg, "zcode.z.ai")
	if direct {
		t.Fatal("did not expect direct backend when disabled")
	}
	if host != "zcode.z.ai" {
		t.Fatalf("unexpected dial host: %s", host)
	}
}
