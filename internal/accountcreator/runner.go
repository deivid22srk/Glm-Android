package accountcreator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"glm5.2proxy/internal/automation"
	"glm5.2proxy/internal/config"
)

type Runner struct {
	cfg        config.Config
	automation *automation.Manager
	runMu      sync.Mutex
	stateMu    sync.RWMutex
	lastRun    time.Time
	running    bool
	lastResult *Result
	lastError  string
	progress   Progress
}

type Result struct {
	Enabled   bool   `json:"enabled"`
	Started   bool   `json:"started"`
	Command   string `json:"command,omitempty"`
	WorkDir   string `json:"workDir,omitempty"`
	Output    string `json:"output,omitempty"`
	Duration  string `json:"duration,omitempty"`
	Username  string `json:"username,omitempty"`
	Email     string `json:"email,omitempty"`
	AccountID string `json:"accountId,omitempty"`
	Label     string `json:"label,omitempty"`
}

type Progress struct {
	Stage       string `json:"stage"`
	Message     string `json:"message"`
	Percent     int    `json:"percent"`
	Detail      string `json:"detail,omitempty"`
	StartedAt   string `json:"startedAt,omitempty"`
	UpdatedAt   string `json:"updatedAt,omitempty"`
	LastLogLine string `json:"lastLogLine,omitempty"`
}

type Status struct {
	Enabled             bool     `json:"enabled"`
	Busy                bool     `json:"busy"`
	Mode                string   `json:"mode"`
	WorkDir             string   `json:"workDir"`
	EmbeddedRootDir     string   `json:"embeddedRootDir"`
	CreatorDataDir      string   `json:"creatorDataDir"`
	CreatorLogDir       string   `json:"creatorLogDir"`
	CreatorLogFile      string   `json:"creatorLogFile"`
	CreatorEmailFile    string   `json:"creatorEmailFile"`
	SolverDir           string   `json:"solverDir"`
	SolverAPIBase       string   `json:"solverApiBase"`
	LastRunAt           string   `json:"lastRunAt,omitempty"`
	CooldownRemainingMs int64    `json:"cooldownRemainingMs"`
	LastError           string   `json:"lastError,omitempty"`
	LastResult          *Result  `json:"lastResult,omitempty"`
	Progress            Progress `json:"progress"`
}

func New(cfg config.Config) *Runner {
	return &Runner{cfg: cfg, automation: automation.New(cfg)}
}

func (r *Runner) Enabled() bool {
	return r != nil && r.cfg.AccountCreatorEnabled
}

func (r *Runner) Status() Status {
	if r == nil {
		return Status{}
	}
	r.stateMu.RLock()
	defer r.stateMu.RUnlock()

	layout := r.automation.Layout()
	workDir := layout.CreatorDir
	mode := "embedded"
	if externalDir := r.externalWorkDir(); externalDir != "" {
		workDir = externalDir
		mode = "external"
	}

	status := Status{
		Enabled:             r.Enabled(),
		Busy:                r.running,
		Mode:                mode,
		WorkDir:             workDir,
		EmbeddedRootDir:     layout.RootDir,
		CreatorDataDir:      layout.CreatorDataDir,
		CreatorLogDir:       layout.CreatorLogDir,
		CreatorLogFile:      layout.CreatorLogFile,
		CreatorEmailFile:    layout.CreatorEmailFile,
		SolverDir:           layout.SolverDir,
		SolverAPIBase:       layout.SolverAPIBase,
		CooldownRemainingMs: r.cooldownRemainingLocked().Milliseconds(),
		LastError:           r.lastError,
		LastResult:          cloneResult(r.lastResult),
		Progress:            r.progress,
	}
	if !r.lastRun.IsZero() {
		status.LastRunAt = r.lastRun.Format(time.RFC3339)
	}
	return status
}

func (r *Runner) Run(ctx context.Context, proxyBaseURL string) (Result, error) {
	result := Result{Enabled: r.Enabled()}
	if !r.Enabled() {
		return result, errors.New("criacao automatica de contas desativada")
	}
	r.runMu.Lock()
	defer r.runMu.Unlock()
	r.setProgress("queued", "Aguardando janela de execucao", 4, "")
	r.stateMu.Lock()
	r.running = true
	r.stateMu.Unlock()
	defer func() {
		r.stateMu.Lock()
		r.running = false
		r.stateMu.Unlock()
	}()
	if wait := r.cooldownRemaining(); wait > 0 {
		r.setProgress("cooldown", "Aguardando cooldown da ultima criacao", 6, wait.Round(time.Second).String())
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return result, ctx.Err()
		case <-timer.C:
		}
	}
	started := time.Now()
	runCtx := ctx
	cancel := func() {}
	if r.cfg.AccountCreatorTimeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, r.cfg.AccountCreatorTimeout)
	}
	defer cancel()
	r.setProgress("prepare", "Preparando automacao e dependencias locais", 10, "")
	prepared, err := r.automation.Prepare(runCtx)
	if err != nil {
		r.setProgress("failed", "Falha ao preparar a automacao", 100, err.Error())
		return result, err
	}
	workDir := prepared.CreatorDir
	if externalDir := r.externalWorkDir(); externalDir != "" {
		workDir = externalDir
	}
	result.WorkDir = workDir

	command := strings.TrimSpace(r.cfg.AccountCreatorCommand)
	if command == "" || strings.EqualFold(command, "node") {
		command = prepared.NodeCommand
	}
	cmd := exec.CommandContext(runCtx, command, "src/main.js", "1")
	cmd.Dir = workDir
	cmd.Env = append(os.Environ(),
		"PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1",
		"PUPPETEER_SKIP_DOWNLOAD=1",
		"ZCODE_ACCOUNT_CREATOR_HEADLESS=1",
		"ZCODE_PROXY_AUTO_LINK=1",
		"ZCODE_PROXY_BASE_URL="+proxyBaseURL,
		"ZCODE_ACCOUNT_CREATOR_DATA_DIR="+prepared.CreatorDataDir,
		"ZCODE_ACCOUNT_CREATOR_LOG_DIR="+prepared.CreatorLogDir,
		"ZCODE_ACCOUNT_CREATOR_EMAILS_FILE="+prepared.CreatorEmailFile,
		"ZCODE_ACCOUNT_CREATOR_LOG_FILE="+prepared.CreatorLogFile,
		"ZCODE_ACCOUNT_CREATOR_CAPTCHA_API="+prepared.SolverAPIBase,
		"CAPTCHA_API_WORKDIR="+prepared.SolverDir,
	)
	var output bytes.Buffer
	progressWriter := newProgressLineWriter(func(line string) {
		r.updateProgressFromLine(line)
	})
	cmd.Stdout = io.MultiWriter(&output, progressWriter)
	cmd.Stderr = io.MultiWriter(&output, progressWriter)
	r.setProgress("launch", "Iniciando navegador headless e fluxo de cadastro", 18, workDir)
	err = cmd.Run()
	result.Started = true
	result.Command = command + " src/main.js 1"
	result.Output = trimOutput(output.String())
	result.Duration = time.Since(started).Round(time.Millisecond).String()
	applyAutomationSummary(&result, output.String())
	r.stateMu.Lock()
	r.lastRun = time.Now()
	r.lastResult = cloneResult(&result)
	if err != nil {
		r.lastError = err.Error()
		r.progress = withProgressUpdate(r.progress, "failed", "Falha ao executar a automacao", 100, err.Error())
		r.stateMu.Unlock()
		if runCtx.Err() != nil {
			return result, fmt.Errorf("criacao automatica de conta excedeu o timeout: %w", runCtx.Err())
		}
		return result, fmt.Errorf("criacao automatica de conta falhou: %w", err)
	}
	r.lastError = ""
	r.progress = withProgressUpdate(r.progress, "completed", "Conta criada e vinculada ao proxy", 100, accountCreatorResultDetail(result))
	r.stateMu.Unlock()
	return result, nil
}

func (r *Runner) setProgress(stage, message string, percent int, detail string) {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	r.progress = withProgressUpdate(r.progress, stage, message, percent, detail)
}

func (r *Runner) updateProgressFromLine(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	stage, message, percent := progressForLogLine(line)
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	current := r.progress
	if percent < current.Percent {
		percent = current.Percent
	}
	if stage == "" {
		stage = current.Stage
	}
	if message == "" {
		message = current.Message
	}
	r.progress = withProgressUpdate(current, stage, message, percent, "")
	r.progress.LastLogLine = line
}

func progressForLogLine(line string) (string, string, int) {
	value := strings.ToLower(line)
	switch {
	case strings.Contains(value, "etapa 1"):
		return "email", "Obtendo email temporario limpo", 24
	case strings.Contains(value, "email novo disponivel") || strings.Contains(value, "email marcado"):
		return "email", "Email temporario reservado", 30
	case strings.Contains(value, "preenchendo formulario") || strings.Contains(value, "tela de login"):
		return "register", "Preenchendo cadastro no Z.ai", 38
	case strings.Contains(value, "captcha"):
		if strings.Contains(value, "resolvido") {
			return "captcha", "Captcha resolvido", 58
		}
		return "captcha", "Resolvendo captcha Aliyun", 48
	case strings.Contains(value, "verificacao por email") || strings.Contains(value, "aguardando email"):
		return "verify_email", "Aguardando email de verificacao", 64
	case strings.Contains(value, "link de verificacao") || strings.Contains(value, "clicou no email"):
		return "verify_email", "Confirmando email da conta", 70
	case strings.Contains(value, "vincular conta criada") || strings.Contains(value, "poll do vinculo"):
		return "link_proxy", "Vinculando conta ao proxy", 80
	case strings.Contains(value, "conta vinculada ao proxy"):
		return "link_proxy", "Conta vinculada ao proxy", 88
	case strings.Contains(value, "coding plan"):
		return "coding_plan", "Atualizando Coding Plan e cota", 94
	case strings.Contains(value, "conta criada com sucesso"):
		return "completed", "Conta criada com sucesso", 98
	case strings.Contains(value, "falha") || strings.Contains(value, "[err"):
		return "failed", "Falha durante a criacao", 100
	default:
		return "", "", 0
	}
}

func withProgressUpdate(current Progress, stage, message string, percent int, detail string) Progress {
	now := time.Now().Format(time.RFC3339)
	if current.StartedAt == "" {
		current.StartedAt = now
	}
	current.Stage = stage
	current.Message = message
	current.Percent = clampPercent(percent)
	current.Detail = detail
	current.UpdatedAt = now
	return current
}

func clampPercent(value int) int {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}

func accountCreatorResultDetail(result Result) string {
	parts := []string{}
	if result.Label != "" {
		parts = append(parts, result.Label)
	}
	if result.Email != "" {
		parts = append(parts, result.Email)
	}
	if result.Duration != "" {
		parts = append(parts, result.Duration)
	}
	return strings.Join(parts, " - ")
}

type progressLineWriter struct {
	mu     sync.Mutex
	buffer string
	onLine func(string)
}

func newProgressLineWriter(onLine func(string)) *progressLineWriter {
	return &progressLineWriter{onLine: onLine}
}

func (w *progressLineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buffer += string(p)
	for {
		index := strings.IndexAny(w.buffer, "\r\n")
		if index < 0 {
			break
		}
		line := strings.TrimSpace(w.buffer[:index])
		w.buffer = strings.TrimLeft(w.buffer[index+1:], "\r\n")
		if line != "" && w.onLine != nil {
			w.onLine(line)
		}
	}
	return len(p), nil
}

func applyAutomationSummary(result *Result, output string) {
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "=== Conta criada com sucesso ===") {
			var payload struct {
				Username string `json:"username"`
				Email    string `json:"email"`
			}
			if decodeLogPayload(line, &payload) == nil {
				result.Username = payload.Username
				result.Email = payload.Email
			}
		}
		if strings.Contains(line, "Conta vinculada ao proxy GLM5.2") {
			var payload struct {
				Email     string `json:"email"`
				AccountID string `json:"accountId"`
				Label     string `json:"label"`
			}
			if decodeLogPayload(line, &payload) == nil {
				if payload.Email != "" {
					result.Email = payload.Email
				}
				result.AccountID = payload.AccountID
				result.Label = payload.Label
			}
		}
	}
}

func decodeLogPayload(line string, target any) error {
	start := strings.LastIndex(line, "{")
	if start < 0 {
		return errors.New("log line has no JSON payload")
	}
	return json.Unmarshal([]byte(strings.TrimSpace(line[start:])), target)
}

func (r *Runner) cooldownRemaining() time.Duration {
	r.stateMu.RLock()
	defer r.stateMu.RUnlock()
	return r.cooldownRemainingLocked()
}

func (r *Runner) cooldownRemainingLocked() time.Duration {
	if r.cfg.AccountCreatorCooldown <= 0 || r.lastRun.IsZero() {
		return 0
	}
	wait := r.cfg.AccountCreatorCooldown - time.Since(r.lastRun)
	if wait < 0 {
		return 0
	}
	return wait
}

func validateWorkDir(dir string) error {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return errors.New("pasta da automacao nao configurada")
	}
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("pasta da automacao de contas indisponivel %q: %w", dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("pasta da automacao de contas nao e diretorio: %s", dir)
	}
	entry := filepath.Join(dir, "src", "main.js")
	if _, err := os.Stat(entry); err != nil {
		return fmt.Errorf("entrada da automacao nao encontrada %q: %w", entry, err)
	}
	return nil
}

func trimOutput(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 4000 {
		return value
	}
	return value[len(value)-4000:]
}

func cloneResult(value *Result) *Result {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func (r *Runner) externalWorkDir() string {
	dir := strings.TrimSpace(r.cfg.AccountCreatorDir)
	if dir == "" {
		return ""
	}
	if validateWorkDir(dir) != nil {
		return ""
	}
	return dir
}
