package upstream

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"glm5.2proxy/internal/accounts"
	"glm5.2proxy/internal/config"
)

type Config struct {
	Endpoint           string
	BaseHeaders        map[string]string
	BodyTemplate       map[string]any
	Source             string
	AccountID          string
	QuotaEndpoint      string
	QuotaAuthorization string
	ActiveAccount      *accounts.PublicAccount
	HasAuthorization   bool
	HasCaptcha         bool
}

type Loader struct {
	cfg          config.Config
	accounts     *accounts.Store
	mu           sync.Mutex
	cachedRecord map[string]any
	cachedSource string
	cachedUntil  time.Time
}

func NewLoader(cfg config.Config, store *accounts.Store) *Loader {
	return &Loader{cfg: cfg, accounts: store}
}

func (l *Loader) Load(account *accounts.Account) Config {
	record, source := l.cachedModelIO()
	headers := map[string]string{}
	template := nativeBodyTemplate()
	endpoint := l.cfg.UpstreamURL
	if request, ok := record["request"].(map[string]any); ok {
		if captured, ok := request["headers"].(map[string]any); ok {
			for key, value := range captured {
				if text, ok := value.(string); ok {
					headers[key] = text
				}
			}
		}
		if provider, ok := request["providerOptions"].(map[string]any); ok {
			if endpoints, ok := provider["endpoints"].(map[string]any); ok && os.Getenv("ZCODE_UPSTREAM_URL") == "" {
				base, _ := endpoints["baseURL"].(string)
				paths, _ := endpoints["paths"].(map[string]any)
				anthropic, _ := paths["anthropic"].(string)
				if base != "" && anthropic != "" {
					endpoint = strings.TrimRight(base, "/") + anthropic
				}
			}
		}
	}
	if account == nil {
		account = l.accounts.Active()
	}
	authorization := l.cfg.Authorization
	activeSource := ""
	var public *accounts.PublicAccount
	if authorization == "" && account != nil {
		authorization = accounts.Authorization(account)
		item := accounts.Sanitize(*account)
		public = &item
		if activeSource == "" {
			activeSource = "proxy-account:" + account.ID
		}
	}
	if authorization == "" {
		authorization = header(headers, "Authorization")
	}
	if activeSource == "" {
		if l.cfg.Authorization != "" {
			activeSource = "environment"
		} else if authorization != "" && source != "" {
			activeSource = source
		} else {
			activeSource = "builtin:zai-start-plan"
		}
	}
	captcha := os.Getenv("ZCODE_CAPTCHA_VERIFY_PARAM")
	acceptLanguage := first(os.Getenv("ZCODE_ACCEPT_LANGUAGE"), header(headers, "Accept-Language"), "pt-BR,pt;q=0.9,en;q=0.8")
	baseHeaders := map[string]string{
		"authorization":       authorization,
		"Referer":             first(os.Getenv("ZCODE_HTTP_REFERER"), header(headers, "HTTP-Referer"), "https://zcode.z.ai"),
		"user-agent":          first(os.Getenv("ZCODE_USER_AGENT"), header(headers, "User-Agent"), "ZCode/"+l.cfg.AppVersion),
		"x-zcode-app-version": first(os.Getenv("ZCODE_APP_VERSION"), header(headers, "X-ZCode-App-Version"), l.cfg.AppVersion),
		"x-title":             first(os.Getenv("ZCODE_TITLE"), header(headers, "X-Title"), "Z Code@electron"),
		"x-zcode-agent":       first(os.Getenv("ZCODE_AGENT"), header(headers, "X-ZCode-Agent"), "glm"),
		"x-session-id":        first(os.Getenv("ZCODE_SESSION_ID"), header(headers, "X-Session-ID")),
		"Accept-Language":     acceptLanguage,
	}
	if captcha != "" {
		baseHeaders["x-aliyun-captcha-verify-param"] = captcha
		baseHeaders["x-aliyun-captcha-verify-region"] = os.Getenv("ZCODE_CAPTCHA_VERIFY_REGION")
	}
	for key, value := range baseHeaders {
		if value == "" {
			delete(baseHeaders, key)
		}
	}
	accountID := ""
	quotaEndpoint := ""
	quotaAuthorization := ""
	if account != nil {
		accountID = account.ID
		if strings.TrimSpace(account.CodingPlanAPIKey) != "" {
			quotaEndpoint = l.cfg.ZAIUsageQuotaURL
			quotaAuthorization = strings.TrimSpace(account.CodingPlanAPIKey)
		}
	}
	return Config{
		Endpoint: endpoint, BaseHeaders: baseHeaders, BodyTemplate: template, Source: activeSource, AccountID: accountID,
		QuotaEndpoint: quotaEndpoint, QuotaAuthorization: quotaAuthorization,
		ActiveAccount: public, HasAuthorization: authorization != "", HasCaptcha: captcha != "",
	}
}

func (l *Loader) LoadStartPlanQuota(account *accounts.Account) Config {
	if account == nil {
		account = l.accounts.Active()
	}
	if account == nil {
		return l.Load(nil)
	}
	startPlanAccount := *account
	startPlanAccount.CodingPlanAPIKey = ""
	startPlanAccount.ZAIAcccessToken = ""
	return l.Load(&startPlanAccount)
}

func bearerAuthorization(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(strings.ToLower(value), "bearer ") {
		return value
	}
	return "Bearer " + value
}

func (l *Loader) cachedModelIO() (map[string]any, string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if time.Now().Before(l.cachedUntil) {
		return l.cachedRecord, l.cachedSource
	}
	record, source := latestModelIO(l.cfg.ModelIODir)
	l.cachedRecord = record
	l.cachedSource = source
	l.cachedUntil = time.Now().Add(10 * time.Second)
	return record, source
}

func nativeBodyTemplate() map[string]any {
	return map[string]any{
		"max_tokens": float64(64000),
		"thinking": map[string]any{
			"type":          "enabled",
			"budget_tokens": float64(32000),
		},
		"output_config": map[string]any{
			"effort": "max",
		},
		"stream": true,
		"system": []any{
			map[string]any{
				"type": "text",
				"text": "You are ZCode, an interactive coding agent",
				"cache_control": map[string]any{
					"type": "ephemeral",
				},
			},
			map[string]any{
				"type": "text",
				"text": "\nYou are an interactive ZCode agent that helps users with software engineering tasks.\n\nIMPORTANT: Assist with authorized security testing, defensive security, CTF challenges, and educational contexts. Refuse requests for destructive techniques, DoS attacks, mass targeting, supply chain compromise, or detection evasion for malicious purposes. Dual-use security tools (C2 frameworks, credential testing, exploit development) require clear authorization context: pentesting engagements, CTF competitions, security research, or defensive use cases.\n\n# Harness\n- Text you output outside of tool use is displayed to the user as Github-flavored markdown in a terminal.\n- Tools run behind a user-selected permission mode; a denied call means the user declined it — adjust, don't retry verbatim.\n- `<system-reminder>` tags in messages and tool results are injected by the harness, not the user. Hooks may intercept tool calls; treat hook output as user feedback.\n- Prefer the dedicated file/search tools over shell commands when one fits. Independent tool calls can run in parallel in one response.\n- Reference code as `file_path:line_number` — it's clickable.",
				"cache_control": map[string]any{
					"type": "ephemeral",
				},
			},
			map[string]any{
				"type": "text",
				"text": "Write code that reads like the surrounding code: match its comment density, naming, and idiom.\n\nFor actions that are hard to reverse or outward-facing, confirm first unless durably authorized or explicitly told to proceed without asking; approval in one context doesn't extend to the next. Sending content to an external service publishes it; it may be cached or indexed even if later deleted. Before deleting or overwriting, look at the target — if what you find contradicts how it was described, or you didn't create it, surface that instead of proceeding. Report outcomes faithfully: if tests fail, say so with the output; if a step was skipped, say that; when something is done and verified, state it plainly without hedging.\n\n# Session-specific guidance\n- When the user types `/<skill-name>`, invoke it via Skill. Only use skills listed in the user-invocable skills section — don't guess.\n\n# Environment\nYou have been invoked in the following environment:\n- Primary working directory: C:\\Users\\maicon2\\ZCodeProject\n- Is a git repository: no\n- Platform: win32\n- Shell: cmd.exe\n- OS Version: win32 10.0.26200 x64\n- You are powered by the model named builtin:zai-start-plan/GLM-5.2.\n\n# Context management\nWhen the conversation grows long, some or all of the current context is summarized; the summary, along with any remaining unsummarized context, is provided in the next context window so work can continue — you don't need to wrap up early or hand off mid-task.",
				"cache_control": map[string]any{
					"type": "ephemeral",
				},
			},
		},
	}
}

func latestModelIO(directory string) (map[string]any, string) {
	files, _ := filepath.Glob(filepath.Join(directory, "model-io-*.jsonl"))
	sort.Slice(files, func(i, j int) bool {
		left, _ := os.Stat(files[i])
		right, _ := os.Stat(files[j])
		return left != nil && right != nil && left.ModTime().After(right.ModTime())
	})
	for _, file := range files {
		handle, err := os.Open(file)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(handle)
		buffer := make([]byte, 64*1024)
		scanner.Buffer(buffer, 32*1024*1024)
		for scanner.Scan() {
			var record map[string]any
			if json.Unmarshal(scanner.Bytes(), &record) != nil {
				continue
			}
			request, _ := record["request"].(map[string]any)
			headers, _ := request["headers"].(map[string]any)
			body, _ := request["body"].(map[string]any)
			if headerAny(headers, "Authorization") != "" && validTemplateBody(body) {
				handle.Close()
				return record, file
			}
		}
		handle.Close()
	}
	return map[string]any{}, ""
}

func validTemplateBody(body map[string]any) bool {
	if text(body["model"]) == "" {
		return false
	}
	if len(arrayAny(body["messages"])) == 0 {
		return false
	}
	return len(arrayAny(body["system"])) > 0
}

func text(value any) string {
	result, _ := value.(string)
	return result
}

func arrayAny(value any) []any {
	result, _ := value.([]any)
	return result
}

func header(values map[string]string, target string) string {
	for key, value := range values {
		if strings.EqualFold(key, target) {
			return value
		}
	}
	return ""
}

func headerAny(values map[string]any, target string) string {
	for key, value := range values {
		if strings.EqualFold(key, target) {
			text, _ := value.(string)
			return text
		}
	}
	return ""
}

func first(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
