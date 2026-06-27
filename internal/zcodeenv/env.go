package zcodeenv

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"glm5.2proxy/internal/accounts"
)

const (
	credentialActiveProvider = "oauth:active_provider"
	credentialAccessToken    = "oauth:zai:access_token"
	credentialUserInfo       = "oauth:zai:user_info"
	credentialJWTToken       = "zcodejwttoken"
	startPlanBaseURL         = "https://zcode.z.ai/api/v1/zcode-plan/anthropic"
	zaiCodingPlanBaseURL     = "https://api.z.ai/api/anthropic"
)

type Environment struct {
	HomeDir             string       `json:"homeDir"`
	DataDir             string       `json:"dataDir"`
	CredentialsPath     string       `json:"credentialsPath"`
	ConfigPath          string       `json:"configPath"`
	SettingPath         string       `json:"settingPath"`
	CodingPlanPath      string       `json:"codingPlanPath"`
	InstallPath         string       `json:"installPath,omitempty"`
	AppServerScript     string       `json:"appServerScript,omitempty"`
	RunningProcesses    []Process    `json:"runningProcesses"`
	CurrentUser         *CurrentUser `json:"currentUser,omitempty"`
	CredentialsPresent  bool         `json:"credentialsPresent"`
	ConfigPresent       bool         `json:"configPresent"`
	DetectedAt          time.Time    `json:"detectedAt"`
	RestartRecommended  bool         `json:"restartRecommended"`
	LiveRefreshPossible bool         `json:"liveRefreshPossible"`
	LiveRefreshReason   string       `json:"liveRefreshReason,omitempty"`
	BridgeInstalled     bool         `json:"bridgeInstalled"`
	BridgeVersion       string       `json:"bridgeVersion,omitempty"`
	BridgeProxyBaseURL  string       `json:"bridgeProxyBaseUrl,omitempty"`
	BridgeScriptPath    string       `json:"bridgeScriptPath,omitempty"`
	Warnings            []string     `json:"warnings,omitempty"`
}

type Process struct {
	PID         int    `json:"pid"`
	Executable  string `json:"executable,omitempty"`
	CommandLine string `json:"commandLine,omitempty"`
	Role        string `json:"role"`
}

type CurrentUser struct {
	ID    string `json:"id,omitempty"`
	Email string `json:"email,omitempty"`
	Name  string `json:"name,omitempty"`
}

type ApplyResult struct {
	Environment         Environment            `json:"environment"`
	Account             accounts.PublicAccount `json:"account"`
	BackupPath          string                 `json:"backupPath,omitempty"`
	ConfigUpdated       bool                   `json:"configUpdated"`
	CredentialsUpdated  bool                   `json:"credentialsUpdated"`
	RestartRecommended  bool                   `json:"restartRecommended"`
	LiveRefreshPossible bool                   `json:"liveRefreshPossible"`
	LiveRefreshReason   string                 `json:"liveRefreshReason,omitempty"`
	LiveRefreshQueued   bool                   `json:"liveRefreshQueued"`
	BridgePatched       bool                   `json:"bridgePatched"`
	BridgePatchMessage  string                 `json:"bridgePatchMessage,omitempty"`
	BridgeRestartedApp  bool                   `json:"bridgeRestartedApp"`
}

type BridgePatchResult struct {
	Installed    bool
	Version      string
	Updated      bool
	RestartedApp bool
	Message      string
}

const (
	bridgeMarkerV1 = "__GLM52_PROXY_BRIDGE__"
	bridgeMarkerV2 = "__GLM52_PROXY_BRIDGE_RELOAD_V2__"
	bridgeMarkerV3 = "__GLM52_PROXY_BRIDGE_RELOAD_V3__"
	bridgeMarkerV5 = "__GLM52_PROXY_BRIDGE_RELOAD_V5__"
	bridgeMarkerV6 = "__GLM52_PROXY_BRIDGE_RELOAD_V6__"
)

const requiredBridgeVersion = "v6"

var codingPlanProviderIDs = []string{"builtin:zai-start-plan", "builtin:zai-coding-plan"}

type codingPlanProviderConfig struct {
	ModelBaseURL  string
	LegacyBaseURL string
}

var codingPlanProviderConfigByID = map[string]codingPlanProviderConfig{
	"builtin:zai-start-plan": {
		ModelBaseURL:  startPlanBaseURL,
		LegacyBaseURL: startPlanBaseURL,
	},
	"builtin:zai-coding-plan": {
		ModelBaseURL:  zaiCodingPlanBaseURL,
		LegacyBaseURL: zaiCodingPlanBaseURL,
	},
}

var bridgeDetectionCache struct {
	mu        sync.Mutex
	path      string
	size      int64
	modTime   time.Time
	installed bool
	version   string
	proxyURL  string
}

func Detect() Environment {
	home, _ := os.UserHomeDir()
	dataDir := filepath.Join(home, ".zcode", "v2")
	env := Environment{
		HomeDir:             home,
		DataDir:             dataDir,
		CredentialsPath:     filepath.Join(dataDir, "credentials.json"),
		ConfigPath:          filepath.Join(dataDir, "config.json"),
		SettingPath:         filepath.Join(dataDir, "setting.json"),
		CodingPlanPath:      filepath.Join(dataDir, "coding-plan-cache.json"),
		DetectedAt:          time.Now(),
		RestartRecommended:  true,
		LiveRefreshPossible: false,
		LiveRefreshReason:   "Sem o patch bridge do renderer, o refresh de conta fica atras de um RPC privado por Electron MessagePort. Com o patch instalado, o proxy enfileira o refresh e o renderer do ZCode executa internamente.",
	}
	env.RunningProcesses = runningProcesses()
	for _, process := range env.RunningProcesses {
		if env.InstallPath == "" && strings.HasSuffix(strings.ToLower(process.Executable), "zcode.exe") {
			env.InstallPath = process.Executable
		}
		if env.AppServerScript == "" && strings.Contains(strings.ToLower(process.CommandLine), `resources\glm\zcode.cjs`) {
			env.AppServerScript = filepath.Join(filepath.Dir(filepath.Dir(process.Executable)), "resources", "glm", "zcode.cjs")
			if _, err := os.Stat(env.AppServerScript); err != nil {
				env.AppServerScript = ""
			}
		}
	}
	if env.InstallPath == "" && runtime.GOOS == "windows" {
		candidate := filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "ZCode", "ZCode.exe")
		if _, err := os.Stat(candidate); err == nil {
			env.InstallPath = candidate
		}
	}
	if env.AppServerScript == "" && env.InstallPath != "" {
		candidate := filepath.Join(filepath.Dir(env.InstallPath), "resources", "glm", "zcode.cjs")
		if _, err := os.Stat(candidate); err == nil {
			env.AppServerScript = candidate
		}
	}
	env.CredentialsPresent = fileExists(env.CredentialsPath)
	env.ConfigPresent = fileExists(env.ConfigPath)
	if env.CredentialsPresent {
		if current, err := readCurrentUser(env.CredentialsPath, NewCipher(home)); err == nil {
			env.CurrentUser = current
		} else {
			env.Warnings = append(env.Warnings, "Nao foi possivel descriptografar o usuario atual do ZCode: "+err.Error())
		}
	}
	env.BridgeScriptPath = bridgeScriptPath()
	if installed, version, proxyBaseURL, err := detectBridge(env); err == nil {
		env.BridgeInstalled = installed
		env.BridgeVersion = version
		env.BridgeProxyBaseURL = proxyBaseURL
		env.LiveRefreshPossible = installed && version == requiredBridgeVersion
		if env.LiveRefreshPossible {
			env.RestartRecommended = false
			env.LiveRefreshReason = "Bridge do renderer detectado; o proxy consegue enfileirar o refresh e o ZCode recarrega a janela automaticamente."
		} else if installed {
			env.LiveRefreshReason = "Bridge do renderer detectado, mas em versao antiga (" + version + "). A proxima aplicacao de conta reinstala o patch atual."
		}
	} else if env.InstallPath != "" {
		env.Warnings = append(env.Warnings, "Nao foi possivel verificar o bridge do ZCode: "+err.Error())
	}
	sort.SliceStable(env.RunningProcesses, func(i, j int) bool { return env.RunningProcesses[i].PID < env.RunningProcesses[j].PID })
	return env
}

func Available(env Environment) bool {
	return env.InstallPath != "" || env.AppServerScript != "" || env.CredentialsPresent || env.ConfigPresent || len(env.RunningProcesses) > 0
}

func Restart(env Environment) error {
	if runtime.GOOS != "windows" {
		return errors.New("reinicio automatico do ZCode so esta implementado no Windows")
	}
	if env.InstallPath == "" {
		return errors.New("executavel do ZCode nao foi detectado")
	}
	if len(env.RunningProcesses) > 0 {
		command := exec.Command("powershell", "-NoProfile", "-Command", "Get-Process -Name 'ZCode' -ErrorAction SilentlyContinue | Stop-Process -Force")
		if output, err := command.CombinedOutput(); err != nil {
			text := strings.TrimSpace(string(output))
			if text != "" {
				return fmt.Errorf("falha ao fechar ZCode: %s", text)
			}
			return fmt.Errorf("falha ao fechar ZCode: %w", err)
		}
		time.Sleep(1200 * time.Millisecond)
	}
	if err := exec.Command(env.InstallPath).Start(); err != nil {
		return fmt.Errorf("falha ao reabrir ZCode: %w", err)
	}
	return nil
}

func ApplyAccount(account accounts.Account) (ApplyResult, error) {
	return ApplyAccountWithBridge(account, "")
}

func ApplyAccountWithBridge(account accounts.Account, proxyBaseURL string) (ApplyResult, error) {
	env := Detect()
	return ApplyAccountWithBridgeInEnvironment(env, account, proxyBaseURL)
}

func ApplyAccountWithBridgeInEnvironment(env Environment, account accounts.Account, proxyBaseURL string) (ApplyResult, error) {
	if account.ZCodeJWTToken == "" {
		return ApplyResult{}, errors.New("conta sem zcodeJwtToken salvo")
	}
	if err := os.MkdirAll(env.DataDir, 0o700); err != nil {
		return ApplyResult{}, err
	}
	cipher := NewCipher(env.HomeDir)
	backup, err := writeCredentials(env.CredentialsPath, cipher, account)
	if err != nil {
		return ApplyResult{}, err
	}
	configUpdated, err := updateConfig(env.ConfigPath, account.ZCodeJWTToken)
	if err != nil {
		return ApplyResult{}, err
	}
	if _, err := clearCodingPlanCache(env.CodingPlanPath, true); err != nil {
		return ApplyResult{}, err
	}
	env.CredentialsPresent = true
	env.ConfigPresent = true
	env.CurrentUser = &CurrentUser{
		ID:    first(account.User.UserID, account.User.ID),
		Email: account.User.Email,
		Name:  first(account.User.Name, account.User.Nickname),
	}
	patchResult, err := ensureBridgeInstalled(env, proxyBaseURL)
	if err != nil {
		return ApplyResult{}, err
	}
	if patchResult.Updated {
		env = Detect()
	}
	env.CredentialsPresent = true
	env.ConfigPresent = true
	env.CurrentUser = &CurrentUser{
		ID:    first(account.User.UserID, account.User.ID),
		Email: account.User.Email,
		Name:  first(account.User.Name, account.User.Nickname),
	}
	return ApplyResult{
		Environment:         env,
		Account:             accounts.Sanitize(account),
		BackupPath:          backup,
		ConfigUpdated:       configUpdated,
		CredentialsUpdated:  true,
		RestartRecommended:  !env.LiveRefreshPossible,
		LiveRefreshPossible: env.LiveRefreshPossible,
		LiveRefreshReason:   env.LiveRefreshReason,
		BridgePatched:       patchResult.Updated,
		BridgePatchMessage:  patchResult.Message,
		BridgeRestartedApp:  patchResult.RestartedApp,
	}, nil
}

func EnforceCodingPlanStateInEnvironment(env Environment, account accounts.Account) (bool, error) {
	if account.ZCodeJWTToken == "" {
		return false, errors.New("conta sem zcodeJwtToken salvo")
	}
	current, err := codingPlanStateCurrent(env.ConfigPath, env.CodingPlanPath, account.ZCodeJWTToken)
	if err != nil {
		return false, err
	}
	if current {
		return false, nil
	}
	configChanged, err := updateConfig(env.ConfigPath, account.ZCodeJWTToken)
	if err != nil {
		return false, err
	}
	cacheChanged, err := clearCodingPlanCache(env.CodingPlanPath, false)
	if err != nil {
		return false, err
	}
	return configChanged || cacheChanged, nil
}

func codingPlanStateCurrent(configPath, cachePath, jwt string) (bool, error) {
	configCurrent, err := codingPlanConfigCurrent(configPath, jwt)
	if err != nil || !configCurrent {
		return configCurrent, err
	}
	cacheAvailable, err := codingPlanStartPlanAvailable(cachePath)
	if err != nil {
		return false, err
	}
	return cacheAvailable, nil
}

func ensureBridgeInstalled(env Environment, proxyBaseURL string) (BridgePatchResult, error) {
	if runtime.GOOS != "windows" {
		return BridgePatchResult{}, nil
	}
	if env.InstallPath == "" {
		return BridgePatchResult{}, nil
	}
	expectedProxyBaseURL := effectiveProxyBaseURL(proxyBaseURL)
	if env.BridgeInstalled && env.BridgeVersion == requiredBridgeVersion && sameBridgeProxy(env.BridgeProxyBaseURL, expectedProxyBaseURL) {
		return BridgePatchResult{
			Installed: true,
			Version:   env.BridgeVersion,
			Message:   "Bridge " + requiredBridgeVersion + " do ZCode ja estava instalado.",
		}, nil
	}
	scriptPath := env.BridgeScriptPath
	if scriptPath == "" {
		return BridgePatchResult{}, errors.New("script de patch do ZCode nao foi encontrado no projeto")
	}
	asarPath := filepath.Join(filepath.Dir(env.InstallPath), "resources", "app.asar")
	args := []string{
		"-NoProfile",
		"-ExecutionPolicy", "Bypass",
		"-File", scriptPath,
		"-ZCodeAsarPath", asarPath,
		"-ProxyBaseUrl", effectiveProxyBaseURL(proxyBaseURL),
	}
	if len(env.RunningProcesses) > 0 {
		args = append(args, "-ForceKill")
	}
	output, err := exec.Command("powershell", args...).CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(output))
		if text != "" {
			return BridgePatchResult{}, fmt.Errorf("falha ao instalar bridge do ZCode: %s", text)
		}
		return BridgePatchResult{}, fmt.Errorf("falha ao instalar bridge do ZCode: %w", err)
	}
	restarted := false
	if len(env.RunningProcesses) > 0 {
		if err := exec.Command(env.InstallPath).Start(); err == nil {
			restarted = true
		}
	}
	verifiedEnv := Detect()
	if !verifiedEnv.BridgeInstalled || verifiedEnv.BridgeVersion != requiredBridgeVersion {
		return BridgePatchResult{}, fmt.Errorf("o patch terminou, mas o bridge %s nao foi detectado no ZCode", requiredBridgeVersion)
	}
	if !sameBridgeProxy(verifiedEnv.BridgeProxyBaseURL, expectedProxyBaseURL) {
		return BridgePatchResult{}, fmt.Errorf("o patch terminou, mas o bridge aponta para %q em vez de %q", verifiedEnv.BridgeProxyBaseURL, expectedProxyBaseURL)
	}
	message := strings.TrimSpace(string(output))
	if message == "" {
		message = "Bridge " + requiredBridgeVersion + " do ZCode instalado automaticamente."
	}
	return BridgePatchResult{
		Installed:    true,
		Version:      verifiedEnv.BridgeVersion,
		Updated:      true,
		RestartedApp: restarted,
		Message:      compactLines(message),
	}, nil
}

func detectBridge(env Environment) (bool, string, string, error) {
	if env.InstallPath == "" {
		return false, "", "", nil
	}
	asarPath := filepath.Join(filepath.Dir(env.InstallPath), "resources", "app.asar")
	info, err := os.Stat(asarPath)
	if err != nil {
		return false, "", "", err
	}
	bridgeDetectionCache.mu.Lock()
	if bridgeDetectionCache.path == asarPath && bridgeDetectionCache.size == info.Size() && bridgeDetectionCache.modTime.Equal(info.ModTime()) {
		installed := bridgeDetectionCache.installed
		version := bridgeDetectionCache.version
		proxyURL := bridgeDetectionCache.proxyURL
		bridgeDetectionCache.mu.Unlock()
		return installed, version, proxyURL, nil
	}
	bridgeDetectionCache.mu.Unlock()
	raw, err := os.ReadFile(asarPath)
	if err != nil {
		return false, "", "", err
	}
	proxyBaseURL := detectBridgeProxyBaseURL(raw)
	installed := false
	version := ""
	switch {
	case bytes.Contains(raw, []byte(bridgeMarkerV6)):
		installed = true
		version = "v6"
	case bytes.Contains(raw, []byte(bridgeMarkerV5)):
		installed = true
		version = "v5"
	case bytes.Contains(raw, []byte(bridgeMarkerV3)):
		installed = true
		version = "v3"
	case bytes.Contains(raw, []byte(bridgeMarkerV2)):
		installed = true
		version = "v2"
	case bytes.Contains(raw, []byte(bridgeMarkerV1)):
		installed = true
		version = "v1"
	}
	bridgeDetectionCache.mu.Lock()
	bridgeDetectionCache.path = asarPath
	bridgeDetectionCache.size = info.Size()
	bridgeDetectionCache.modTime = info.ModTime()
	bridgeDetectionCache.installed = installed
	bridgeDetectionCache.version = version
	bridgeDetectionCache.proxyURL = proxyBaseURL
	bridgeDetectionCache.mu.Unlock()
	return installed, version, proxyBaseURL, nil
}

func detectBridgeProxyBaseURL(raw []byte) string {
	needle := []byte("/api/admin/zcode/bridge")
	index := bytes.Index(raw, needle)
	if index < 0 {
		return ""
	}
	start := index - 1
	for start >= 0 && raw[start] != '"' && raw[start] != '\'' && raw[start] != '`' {
		start--
	}
	if start < 0 {
		return ""
	}
	return string(raw[start+1 : index])
}

func bridgeScriptPath() string {
	candidates := []string{}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(cwd, "scripts", "patch-zcode-live-refresh.ps1"))
	}
	if exe, err := os.Executable(); err == nil {
		base := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(base, "scripts", "patch-zcode-live-refresh.ps1"),
			filepath.Join(base, "..", "scripts", "patch-zcode-live-refresh.ps1"),
			filepath.Join(base, "..", "..", "scripts", "patch-zcode-live-refresh.ps1"),
		)
	}
	for _, candidate := range candidates {
		resolved, err := filepath.Abs(candidate)
		if err == nil && fileExists(resolved) {
			return resolved
		}
	}
	return ""
}

func effectiveProxyBaseURL(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "http://127.0.0.1:3005"
	}
	return strings.TrimRight(trimmed, "/")
}

func sameBridgeProxy(left, right string) bool {
	return strings.EqualFold(strings.TrimRight(strings.TrimSpace(left), "/"), strings.TrimRight(strings.TrimSpace(right), "/"))
}

func compactLines(value string) string {
	lines := strings.FieldsFunc(value, func(r rune) bool { return r == '\r' || r == '\n' })
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		filtered = append(filtered, line)
	}
	if len(filtered) == 0 {
		return ""
	}
	return strings.Join(filtered, " | ")
}

func writeCredentials(path string, cipher Cipher, account accounts.Account) (string, error) {
	credentials := map[string]string{}
	if raw, err := os.ReadFile(path); err == nil && len(bytes.TrimSpace(raw)) > 0 {
		_ = json.Unmarshal(raw, &credentials)
	}
	backup := ""
	if fileExists(path) {
		backup = path + ".glm5proxy-backup-" + time.Now().Format("20060102-150405")
		if raw, err := os.ReadFile(path); err == nil {
			if err := os.WriteFile(backup, raw, 0o600); err != nil {
				return "", err
			}
		}
	}
	userInfo, err := json.Marshal(account.User)
	if err != nil {
		return "", err
	}
	values := map[string]string{
		credentialActiveProvider: "zai",
		credentialUserInfo:       string(userInfo),
		credentialJWTToken:       account.ZCodeJWTToken,
	}
	if account.ZAIAcccessToken != "" {
		values[credentialAccessToken] = account.ZAIAcccessToken
	}
	for key, value := range values {
		encrypted, err := cipher.Encrypt(value)
		if err != nil {
			return "", err
		}
		credentials[key] = encrypted
	}
	raw, err := json.MarshalIndent(credentials, "", "  ")
	if err != nil {
		return "", err
	}
	return backup, os.WriteFile(path, append(raw, '\n'), 0o600)
}

func updateConfig(path, jwt string) (bool, error) {
	config := map[string]any{}
	existingRaw := []byte{}
	if raw, err := os.ReadFile(path); err == nil && len(bytes.TrimSpace(raw)) > 0 {
		existingRaw = raw
		if err := json.Unmarshal(raw, &config); err != nil {
			return false, err
		}
	}
	providers, _ := config["modelProviders"].(map[string]any)
	if providers == nil {
		providers = map[string]any{}
		config["modelProviders"] = providers
	}
	legacyProviders, _ := config["provider"].(map[string]any)
	if legacyProviders == nil {
		legacyProviders = map[string]any{}
		config["provider"] = legacyProviders
	}
	for _, providerID := range codingPlanProviderIDs {
		providerConfig := codingPlanProviderConfigByID[providerID]
		provider := ensureObject(providers, providerID)
		provider["enabled"] = true
		options := ensureObject(provider, "options")
		options["apiKey"] = jwt
		options["baseURL"] = providerConfig.ModelBaseURL

		legacyProvider := ensureObject(legacyProviders, providerID)
		if _, ok := legacyProvider["name"]; !ok {
			legacyProvider["name"] = "Z.ai - Coding Plan"
		}
		if _, ok := legacyProvider["kind"]; !ok {
			legacyProvider["kind"] = "anthropic"
		}
		if _, ok := legacyProvider["source"]; !ok {
			legacyProvider["source"] = "custom"
		}
		legacyProvider["enabled"] = true
		delete(legacyProvider, "systemDisabledReason")
		delete(legacyProvider, "apiKeyRequired")
		legacyOptions := ensureObject(legacyProvider, "options")
		legacyOptions["apiKey"] = jwt
		legacyOptions["apiKeyRequired"] = true
		legacyOptions["baseURL"] = providerConfig.LegacyBaseURL
	}
	raw, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return false, err
	}
	serialized := append(raw, '\n')
	if bytes.Equal(existingRaw, serialized) {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return false, err
	}
	return true, os.WriteFile(path, serialized, 0o600)
}

func clearCodingPlanCache(path string, force bool) (bool, error) {
	if !fileExists(path) {
		return false, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return false, nil
	}
	cache := map[string]any{}
	if err := json.Unmarshal(raw, &cache); err != nil {
		if err := removeIfExists(path); err != nil {
			return false, err
		}
		return true, nil
	}
	entryStatus, _ := cache["entryStatus"].(map[string]any)
	if entryStatus == nil {
		return false, nil
	}
	items, _ := entryStatus["items"].(map[string]any)
	if items == nil {
		return false, nil
	}
	changed := false
	for _, providerID := range codingPlanProviderIDs {
		if !force && !codingPlanCacheEntryBlocksPlan(items[providerID]) {
			continue
		}
		if _, ok := items[providerID]; ok {
			delete(items, providerID)
			changed = true
		}
	}
	if !changed {
		return false, nil
	}
	entryStatus["updatedAt"] = time.Now().UnixMilli()
	raw, err = json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return false, err
	}
	return true, os.WriteFile(path, append(raw, '\n'), 0o600)
}

func codingPlanConfigCurrent(path, jwt string) (bool, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	config := map[string]any{}
	if err := json.Unmarshal(raw, &config); err != nil {
		return false, err
	}
	providers, _ := config["modelProviders"].(map[string]any)
	if !codingPlanProviderCurrent(providers["builtin:zai-start-plan"], jwt, startPlanBaseURL) {
		return false, nil
	}
	legacyProviders, _ := config["provider"].(map[string]any)
	return codingPlanProviderCurrent(legacyProviders["builtin:zai-start-plan"], jwt, startPlanBaseURL), nil
}

func codingPlanProviderCurrent(value any, jwt, baseURL string) bool {
	provider, _ := value.(map[string]any)
	if provider == nil || provider["enabled"] != true {
		return false
	}
	options, _ := provider["options"].(map[string]any)
	return options != nil && options["apiKey"] == jwt && options["baseURL"] == baseURL
}

func codingPlanStartPlanAvailable(path string) (bool, error) {
	if !fileExists(path) {
		return false, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	cache := map[string]any{}
	if err := json.Unmarshal(raw, &cache); err != nil {
		return false, err
	}
	entryStatus, _ := cache["entryStatus"].(map[string]any)
	items, _ := entryStatus["items"].(map[string]any)
	entry, _ := items["builtin:zai-start-plan"].(map[string]any)
	return entry != nil && entry["status"] == "available", nil
}

func codingPlanCacheEntryBlocksPlan(value any) bool {
	entry, _ := value.(map[string]any)
	if entry == nil {
		return false
	}
	status, _ := entry["status"].(string)
	return status != "available"
}

func ensureObject(parent map[string]any, key string) map[string]any {
	value, _ := parent[key].(map[string]any)
	if value == nil {
		value = map[string]any{}
		parent[key] = value
	}
	return value
}

func removeIfExists(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func readCurrentUser(path string, cipher Cipher) (*CurrentUser, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	credentials := map[string]string{}
	if err := json.Unmarshal(raw, &credentials); err != nil {
		return nil, err
	}
	value := credentials[credentialUserInfo]
	if value == "" {
		return nil, nil
	}
	plain, err := cipher.Decrypt(value)
	if err != nil {
		return nil, err
	}
	var user accounts.User
	if err := json.Unmarshal([]byte(plain), &user); err != nil {
		return nil, err
	}
	return &CurrentUser{ID: first(user.UserID, user.ID), Email: user.Email, Name: first(user.Name, user.Nickname)}, nil
}

func runningProcesses() []Process {
	if runtime.GOOS != "windows" {
		return nil
	}
	command := exec.Command("powershell", "-NoProfile", "-Command", "Get-CimInstance Win32_Process -Filter \"name='ZCode.exe'\" | Select-Object ProcessId,ExecutablePath,CommandLine | ConvertTo-Csv -NoTypeInformation")
	output, err := command.Output()
	if err != nil {
		return nil
	}
	reader := csv.NewReader(bytes.NewReader(output))
	records, err := reader.ReadAll()
	if err != nil || len(records) < 2 {
		return nil
	}
	var processes []Process
	for _, record := range records[1:] {
		if len(record) < 3 {
			continue
		}
		pid := 0
		_, _ = fmt.Sscanf(record[0], "%d", &pid)
		commandLine := record[2]
		processes = append(processes, Process{PID: pid, Executable: record[1], CommandLine: commandLine, Role: processRole(commandLine)})
	}
	return processes
}

func processRole(commandLine string) string {
	lower := strings.ToLower(commandLine)
	switch {
	case strings.Contains(lower, "zcode.cjs app-server --stdio"):
		return "app-server"
	case strings.Contains(lower, "--type=renderer"):
		return "renderer"
	case strings.Contains(lower, "--type=utility"):
		return "utility"
	case strings.Contains(lower, "--type=gpu-process"):
		return "gpu"
	default:
		return "main"
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func first(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
