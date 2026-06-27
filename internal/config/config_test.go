package config

import (
	"reflect"
	"testing"
)

func TestLoadUsesCurrentZCodeAnthropicEndpointByDefault(t *testing.T) {
	t.Setenv("ZCODE_UPSTREAM_URL", "")
	t.Setenv("ZCODE_APP_VERSION", "")
	t.Setenv("ZCODE_PROXY", "")
	t.Setenv("HTTPS_PROXY", "")
	t.Setenv("https_proxy", "")
	t.Setenv("HTTP_PROXY", "")
	t.Setenv("http_proxy", "")
	t.Setenv("SOCKS5_PROXY", "")
	t.Setenv("socks5_proxy", "")
	t.Setenv("ALL_PROXY", "")
	t.Setenv("all_proxy", "")
	t.Setenv("ZCODE_DISABLE_UTLS", "")
	t.Setenv("ZCODE_DIRECT_BACKEND", "")
	t.Setenv("ZCODE_DIRECT_BACKEND_HOST", "")
	t.Setenv("ZCODE_DIRECT_BACKEND_IPS", "")

	cfg := Load()
	if cfg.UpstreamURL != "https://zcode.z.ai/api/v1/zcode-plan/anthropic/v1/messages" {
		t.Fatalf("unexpected upstream URL: %s", cfg.UpstreamURL)
	}
	if cfg.AppVersion != "3.1.8" {
		t.Fatalf("unexpected app version: %s", cfg.AppVersion)
	}
	if cfg.TransportProxyURL != "" || cfg.TransportSOCKSProxyURL != "" {
		t.Fatalf("expected no transport proxy by default, got http=%q socks=%q", cfg.TransportProxyURL, cfg.TransportSOCKSProxyURL)
	}
	if !cfg.TransportUseUTLS {
		t.Fatal("expected uTLS enabled by default")
	}
	if !cfg.TransportDirectBackend {
		t.Fatal("expected direct backend enabled by default")
	}
	if cfg.TransportDirectHost != "zcode.z.ai" {
		t.Fatalf("unexpected direct backend host: %s", cfg.TransportDirectHost)
	}
	expectedIPs := []string{"8.216.131.99", "8.216.131.225", "8.216.131.83"}
	if !reflect.DeepEqual(cfg.TransportDirectIPs, expectedIPs) {
		t.Fatalf("unexpected direct backend IPs: %+v", cfg.TransportDirectIPs)
	}
}

func TestLoadTransportOverrides(t *testing.T) {
	t.Setenv("ZCODE_PROXY", "http://127.0.0.1:8080")
	t.Setenv("SOCKS5_PROXY", "socks5://127.0.0.1:1080")
	t.Setenv("ZCODE_DISABLE_UTLS", "1")
	t.Setenv("ZCODE_DIRECT_BACKEND", "false")
	t.Setenv("ZCODE_DIRECT_BACKEND_HOST", "chat.z.ai")
	t.Setenv("ZCODE_DIRECT_BACKEND_IPS", "1.1.1.1, 2.2.2.2;3.3.3.3")

	cfg := Load()
	if cfg.TransportProxyURL != "http://127.0.0.1:8080" {
		t.Fatalf("unexpected HTTP proxy URL: %q", cfg.TransportProxyURL)
	}
	if cfg.TransportSOCKSProxyURL != "socks5://127.0.0.1:1080" {
		t.Fatalf("unexpected SOCKS proxy URL: %q", cfg.TransportSOCKSProxyURL)
	}
	if cfg.TransportUseUTLS {
		t.Fatal("expected uTLS disabled override")
	}
	if cfg.TransportDirectBackend {
		t.Fatal("expected direct backend disabled override")
	}
	if cfg.TransportDirectHost != "chat.z.ai" {
		t.Fatalf("unexpected direct backend host override: %q", cfg.TransportDirectHost)
	}
	expectedIPs := []string{"1.1.1.1", "2.2.2.2", "3.3.3.3"}
	if !reflect.DeepEqual(cfg.TransportDirectIPs, expectedIPs) {
		t.Fatalf("unexpected direct backend IP overrides: %+v", cfg.TransportDirectIPs)
	}
}
