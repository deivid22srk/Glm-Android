package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Host                     string
	DefaultPort              int
	DataDir                  string
	CredentialsPath          string
	AdminPath                string
	ModelIODir               string
	UpstreamURL              string
	ZAICodingPlanUpstreamURL string
	BillingBaseURL           string
	ZAIAPIBaseURL            string
	ZAIUsageQuotaURL         string
	OAuthBaseURL             string
	OAuthProvider            string
	OAuthAuthorizeURL        string
	OAuthTokenURL            string
	OAuthUserInfoURL         string
	OAuthClientID            string
	OAuthRedirectURI         string
	OAuthFlowTimeout         time.Duration
	AppVersion               string
	Platform                 string
	Authorization            string
	CredentialSecret         string
	CaptchaEnabled           bool
	CaptchaTimeout           time.Duration
	CaptchaPreferredClient   string
	HeadlessEnabled          bool
	HeadlessExecutable       string
	HeadlessProfileDir       string
	HeadlessRestartDelay     time.Duration
	TransportProxyURL        string
	TransportSOCKSProxyURL   string
	TransportUseUTLS         bool
	TransportDirectBackend   bool
	TransportDirectHost      string
	TransportDirectIPs       []string
	UpstreamTimeout          time.Duration
	RetryMaxAttempts         int
	RetryBaseDelay           time.Duration
	RetryMaxDelay            time.Duration
	AccountRotation          bool
	AccountMinAvailable      int64
	AccountRetryCooldown     time.Duration
	AccountCreatorEnabled    bool
	AccountCreatorDir        string
	AccountCreatorCommand    string
	AccountCreatorTimeout    time.Duration
	AccountCreatorCooldown   time.Duration
	QuotaLog                 bool
	QuotaRefreshDelay        time.Duration
	QuotaRefreshAttempts     int
	DefaultMaxTokens         int
	DefaultThinkingEnabled   bool
	DefaultThinkingBudget    int
	DefaultEffort            string
}

func Load() Config {
	home, _ := os.UserHomeDir()
	defaultDataDir := filepath.Join(home, ".glm5.2proxy")
	dataDir := env("ZCODE_PROXY_DATA_DIR", defaultDataDir)
	if os.Getenv("ZCODE_PROXY_DATA_DIR") == "" && os.Getenv("ZCODE_PROXY_CREDENTIALS_PATH") == "" {
		migrateLegacyDataDir(home, dataDir)
	}
	defaultMaxTokens := envInt("ZCODE_MAX_TOKENS", 64000)
	defaultThinkingEnabled := enabled("ZCODE_THINKING", true)
	defaultThinkingBudget := envInt("ZCODE_THINKING_BUDGET", 32000)
	defaultMinAvailable := defaultMaxTokens
	if defaultThinkingEnabled {
		defaultMinAvailable += defaultThinkingBudget
	}
	creatorDir := strings.TrimSpace(os.Getenv("ZCODE_ACCOUNT_CREATOR_DIR"))
	directBackendValue := strings.TrimSpace(os.Getenv("ZCODE_DIRECT_BACKEND"))
	return Config{
		Host:                     env("ZCODE_PROXY_HOST", "127.0.0.1"),
		DefaultPort:              envInt("PORT", envInt("ZCODE_PROXY_PORT", 3005)),
		DataDir:                  dataDir,
		CredentialsPath:          env("ZCODE_PROXY_CREDENTIALS_PATH", filepath.Join(dataDir, "zcode-accounts.enc.json")),
		AdminPath:                env("ZCODE_PROXY_ADMIN_PATH", filepath.Join(dataDir, "admin.json")),
		ModelIODir:               env("ZCODE_MODEL_IO_DIR", filepath.Join(home, ".zcode", "cli", "rollout")),
		UpstreamURL:              env("ZCODE_UPSTREAM_URL", "https://zcode.z.ai/api/v1/zcode-plan/anthropic/v1/messages"),
		ZAICodingPlanUpstreamURL: env("ZCODE_ZAI_CODING_PLAN_UPSTREAM_URL", "https://api.z.ai/api/anthropic/v1/messages"),
		BillingBaseURL:           env("ZCODE_BILLING_BASE_URL", "https://zcode.z.ai/api/v1/zcode-plan/billing"),
		ZAIAPIBaseURL:            env("ZCODE_ZAI_API_BASE_URL", "https://api.z.ai"),
		ZAIUsageQuotaURL:         env("ZCODE_ZAI_USAGE_QUOTA_URL", "https://api.z.ai/api/monitor/usage/quota/limit"),
		OAuthBaseURL:             env("ZCODE_OAUTH_BASE_URL", "https://zcode.z.ai/api/v1"),
		OAuthProvider:            env("ZCODE_OAUTH_PROVIDER", "zai"),
		OAuthAuthorizeURL:        env("ZCODE_OAUTH_AUTHORIZE_URL", "https://chat.z.ai/api/oauth/authorize"),
		OAuthTokenURL:            env("ZCODE_OAUTH_TOKEN_URL", "https://zcode.z.ai/api/v1/oauth/token"),
		OAuthUserInfoURL:         env("ZCODE_OAUTH_USERINFO_URL", "https://chat.z.ai/api/oauth/userinfo"),
		OAuthClientID:            env("ZCODE_OAUTH_CLIENT_ID", "client_P8X5CMWmlaRO9gyO-KSqtg"),
		OAuthRedirectURI:         os.Getenv("ZCODE_OAUTH_REDIRECT_URI"),
		OAuthFlowTimeout:         envDurationMS("ZCODE_OAUTH_FLOW_TIMEOUT_MS", 300000),
		AppVersion:               env("ZCODE_APP_VERSION", "3.1.8"),
		Platform:                 env("ZCODE_PLATFORM", platform()),
		Authorization:            os.Getenv("ZCODE_AUTHORIZATION"),
		CredentialSecret:         os.Getenv("ZCODE_PROXY_CREDENTIAL_SECRET"),
		CaptchaEnabled:           enabled("ZCODE_CAPTCHA_BRIDGE", true),
		CaptchaTimeout:           envDurationMS("ZCODE_CAPTCHA_BRIDGE_TIMEOUT_MS", 120000),
		CaptchaPreferredClient:   env("ZCODE_CAPTCHA_CLIENT_PREFERENCE", "standalone-browser"),
		HeadlessEnabled:          enabled("ZCODE_CAPTCHA_HEADLESS", true),
		HeadlessExecutable:       os.Getenv("ZCODE_CAPTCHA_HEADLESS_EXECUTABLE"),
		HeadlessProfileDir:       env("ZCODE_CAPTCHA_HEADLESS_PROFILE_DIR", filepath.Join(dataDir, "captcha-headless-profile")),
		HeadlessRestartDelay:     envDurationMS("ZCODE_CAPTCHA_HEADLESS_RESTART_DELAY_MS", 3000),
		TransportProxyURL:        firstNonEmptyEnv("ZCODE_PROXY", "HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy"),
		TransportSOCKSProxyURL:   firstNonEmptyEnv("SOCKS5_PROXY", "socks5_proxy", "ALL_PROXY", "all_proxy"),
		TransportUseUTLS:         strings.TrimSpace(os.Getenv("ZCODE_DISABLE_UTLS")) == "",
		TransportDirectBackend:   !strings.EqualFold(directBackendValue, "false"),
		TransportDirectHost:      env("ZCODE_DIRECT_BACKEND_HOST", "zcode.z.ai"),
		TransportDirectIPs:       envList("ZCODE_DIRECT_BACKEND_IPS", []string{"8.216.131.99", "8.216.131.225", "8.216.131.83"}),
		UpstreamTimeout:          envDurationMS("ZCODE_UPSTREAM_TIMEOUT_MS", 180000),
		RetryMaxAttempts:         envInt("ZCODE_RETRY_MAX_ATTEMPTS", 11),
		RetryBaseDelay:           envDurationMS("ZCODE_RETRY_BASE_DELAY_MS", 1000),
		RetryMaxDelay:            envDurationMS("ZCODE_RETRY_MAX_DELAY_MS", 60000),
		AccountRotation:          enabled("ZCODE_ACCOUNT_ROTATION", true),
		AccountMinAvailable:      int64(envInt("ZCODE_ACCOUNT_MIN_AVAILABLE_UNITS", defaultMinAvailable)),
		AccountRetryCooldown:     envDurationMS("ZCODE_ACCOUNT_RETRY_COOLDOWN_MS", 300000),
		AccountCreatorEnabled:    accountCreatorEnabled(creatorDir),
		AccountCreatorDir:        creatorDir,
		AccountCreatorCommand:    env("ZCODE_ACCOUNT_CREATOR_COMMAND", "node"),
		AccountCreatorTimeout:    envDurationMS("ZCODE_ACCOUNT_CREATOR_TIMEOUT_MS", 600000),
		AccountCreatorCooldown:   envDurationMS("ZCODE_ACCOUNT_CREATOR_COOLDOWN_MS", 30000),
		QuotaLog:                 enabled("ZCODE_QUOTA_LOG", true),
		QuotaRefreshDelay:        envDurationMS("ZCODE_QUOTA_REFRESH_DELAY_MS", 1500),
		QuotaRefreshAttempts:     envInt("ZCODE_QUOTA_REFRESH_ATTEMPTS", 3),
		DefaultMaxTokens:         defaultMaxTokens,
		DefaultThinkingEnabled:   defaultThinkingEnabled,
		DefaultThinkingBudget:    defaultThinkingBudget,
		DefaultEffort:            env("ZCODE_EFFORT", "max"),
	}
}

func accountCreatorEnabled(dir string) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv("ZCODE_ACCOUNT_CREATOR_ENABLED")))
	if value != "" {
		return value != "disabled" && value != "false" && value != "0"
	}
	if strings.TrimSpace(dir) == "" {
		return true
	}
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		return true
	}
	return true
}

func migrateLegacyDataDir(home, dataDir string) {
	if home == "" || dataDir == "" {
		return
	}
	legacyDir := filepath.Join(home, ".kimiproxyplus")
	if samePath(legacyDir, dataDir) {
		return
	}
	copyIfMissing(filepath.Join(legacyDir, "zcode-accounts.enc.json"), filepath.Join(dataDir, "zcode-accounts.enc.json"), 0o600)
	copyIfMissing(filepath.Join(legacyDir, "admin.json"), filepath.Join(dataDir, "admin.json"), 0o600)
}

func copyIfMissing(src, dst string, mode os.FileMode) {
	if _, err := os.Stat(dst); err == nil {
		return
	}
	raw, err := os.ReadFile(src)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return
	}
	_ = os.WriteFile(dst, raw, mode)
}

func samePath(left, right string) bool {
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	if leftErr == nil {
		left = leftAbs
	}
	if rightErr == nil {
		right = rightAbs
	}
	return strings.EqualFold(filepath.Clean(left), filepath.Clean(right))
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func firstNonEmptyEnv(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func envList(key string, fallback []string) []string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return append([]string(nil), fallback...)
	}
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ';'
	})
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	if len(out) == 0 {
		return append([]string(nil), fallback...)
	}
	return out
}

func envInt(key string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key)))
	if err != nil {
		return fallback
	}
	return value
}

func envDurationMS(key string, fallback int) time.Duration {
	return time.Duration(envInt(key, fallback)) * time.Millisecond
}

func enabled(key string, fallback bool) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if value == "" {
		return fallback
	}
	return value != "disabled" && value != "false" && value != "0"
}

func platform() string {
	if os.PathSeparator == '\\' {
		return "win32-x64"
	}
	return "linux-x64"
}
