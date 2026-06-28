package proxy

import (
	"context"
	"crypto/tls"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	utls "github.com/refraction-networking/utls"
	"golang.org/x/net/proxy"

	"glm5.2proxy/internal/config"
)

func newHTTPClient(cfg config.Config) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = 100
	transport.MaxIdleConnsPerHost = 20
	transport.IdleConnTimeout = 90 * time.Second
	configureProxyTransport(transport, cfg)
	configureTLSTransport(transport, cfg)
	return &http.Client{Transport: transport}
}

func configureProxyTransport(transport *http.Transport, cfg config.Config) {
	if cfg.TransportProxyURL != "" {
		parsed, err := url.Parse(cfg.TransportProxyURL)
		if err != nil {
			log.Printf("proxy: invalid URL %s: %v", cfg.TransportProxyURL, err)
			return
		}
		transport.Proxy = http.ProxyURL(parsed)
		log.Printf("proxy: HTTP(S) enabled via %s", parsed.Host)
		return
	}
	if cfg.TransportSOCKSProxyURL == "" {
		log.Printf("proxy: no proxy configured (set HTTP_PROXY, SOCKS5_PROXY or ZCODE_PROXY)")
		return
	}
	parsed, err := url.Parse(cfg.TransportSOCKSProxyURL)
	if err != nil {
		parsed = &url.URL{Host: cfg.TransportSOCKSProxyURL}
	}
	if parsed.Scheme == "" {
		parsed.Scheme = "socks5"
	}
	dialer, err := proxy.FromURL(parsed, proxy.Direct)
	if err != nil {
		log.Printf("proxy: SOCKS5 dialer error for %s: %v", cfg.TransportSOCKSProxyURL, err)
		return
	}
	if ctxDialer, ok := dialer.(proxy.ContextDialer); ok {
		transport.DialContext = ctxDialer.DialContext
	} else {
		transport.Dial = dialer.Dial
	}
	log.Printf("proxy: SOCKS5 enabled via %s (%s)", parsed.Scheme, parsed.Host)
}

func configureTLSTransport(transport *http.Transport, cfg config.Config) {
	if !cfg.TransportUseUTLS {
		log.Printf("tls: uTLS disabled via ZCODE_DISABLE_UTLS")
		return
	}
	origDialContext := transport.DialContext
	if origDialContext == nil {
		origDialContext = new(net.Dialer).DialContext
	}
	transport.DialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		dialHost, directBackend := pickDialHost(cfg, host)
		if directBackend {
			log.Printf("tls: bypass ESA: %s -> %s", host, dialHost)
		}
		tcpConn, err := origDialContext(ctx, network, net.JoinHostPort(dialHost, port))
		if err != nil {
			return nil, err
		}
		uconn := utls.UClient(tcpConn, &utls.Config{
			ServerName:         host,
			InsecureSkipVerify: false,
			MinVersion:         tls.VersionTLS12,
		}, utls.HelloChrome_Auto)
		if err := uconn.HandshakeContext(ctx); err != nil {
			tcpConn.Close()
			return nil, err
		}
		return uconn, nil
	}
	log.Printf("tls: uTLS enabled (fingerprint: Chrome)")
	if cfg.TransportDirectBackend && len(cfg.TransportDirectIPs) > 0 {
		log.Printf("tls: direct backend enabled for %s via %v", cfg.TransportDirectHost, cfg.TransportDirectIPs)
	}
}

func pickDialHost(cfg config.Config, originalHost string) (string, bool) {
	if !cfg.TransportDirectBackend || len(cfg.TransportDirectIPs) == 0 {
		return originalHost, false
	}
	if !strings.EqualFold(strings.TrimSpace(originalHost), strings.TrimSpace(cfg.TransportDirectHost)) {
		return originalHost, false
	}
	index := time.Now().UnixMilli() % int64(len(cfg.TransportDirectIPs))
	return cfg.TransportDirectIPs[index], true
}
