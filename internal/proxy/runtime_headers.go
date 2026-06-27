package proxy

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"strings"
	"sync"

	"glm5.2proxy/internal/captcha"
	"glm5.2proxy/internal/config"
	"glm5.2proxy/internal/upstream"
)

type runtimeHeaderManager struct {
	cfg     config.Config
	captcha *captcha.Bridge
	mu      sync.Mutex
	session map[string]string
}

func newRuntimeHeaderManager(cfg config.Config, bridge *captcha.Bridge) *runtimeHeaderManager {
	return &runtimeHeaderManager{cfg: cfg, captcha: bridge, session: map[string]string{}}
}

func (m *runtimeHeaderManager) Prepare(ctx context.Context, value upstream.Config) (upstream.Config, error) {
	return m.prepare(ctx, value, false)
}

func (m *runtimeHeaderManager) PrepareWithCaptcha(ctx context.Context, value upstream.Config) (upstream.Config, error) {
	return m.prepare(ctx, value, true)
}

func (m *runtimeHeaderManager) prepare(ctx context.Context, value upstream.Config, forceCaptcha bool) (upstream.Config, error) {
	headers := make(map[string]string, len(value.BaseHeaders)+4)
	for key, headerValue := range value.BaseHeaders {
		headers[key] = headerValue
	}
	if strings.TrimSpace(headers["x-session-id"]) == "" {
		headers["x-session-id"] = m.ensureSessionID(value)
	}
	if forceCaptcha {
		challenge, err := m.captcha.FreshChallenge(ctx)
		if err != nil {
			return value, err
		}
		headers["x-aliyun-captcha-verify-param"] = challenge.Token
		if strings.TrimSpace(challenge.Region) != "" {
			headers["x-aliyun-captcha-verify-region"] = challenge.Region
		}
	}
	value.BaseHeaders = headers
	return value, nil
}

func (m *runtimeHeaderManager) ensureSessionID(value upstream.Config) string {
	key := runtimeSessionKey(value)
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing := m.session[key]; existing != "" {
		return existing
	}
	id := randomID()
	m.session[key] = id
	return id
}

func runtimeSessionKey(value upstream.Config) string {
	if accountID := strings.TrimSpace(value.AccountID); accountID != "" {
		return "account:" + accountID
	}
	if source := strings.TrimSpace(value.Source); source != "" {
		return "source:" + source
	}
	authorization := strings.TrimSpace(value.BaseHeaders["authorization"])
	if authorization == "" {
		return "default"
	}
	hash := sha1.Sum([]byte(authorization))
	return "auth:" + hex.EncodeToString(hash[:8])
}
