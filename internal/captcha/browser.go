package captcha

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"glm5.2proxy/internal/config"
)

type BrowserSnapshot struct {
	Enabled    bool           `json:"enabled"`
	Running    bool           `json:"running"`
	PID        int            `json:"pid"`
	Client     string         `json:"client"`
	Executable string         `json:"executable"`
	ProfileDir string         `json:"profileDir"`
	StartedAt  *time.Time     `json:"startedAt"`
	LastExit   map[string]any `json:"lastExit"`
}

type BrowserManager struct {
	cfg      config.Config
	port     int
	mu       sync.RWMutex
	command  *exec.Cmd
	started  *time.Time
	lastExit map[string]any
	stopping bool
	cancel   context.CancelFunc
	done     chan struct{}
}

func NewBrowserManager(cfg config.Config, port int) *BrowserManager {
	return &BrowserManager{cfg: cfg, port: port, done: make(chan struct{})}
}

func (m *BrowserManager) Start(parent context.Context) {
	if !m.cfg.HeadlessEnabled {
		close(m.done)
		return
	}
	ctx, cancel := context.WithCancel(parent)
	m.cancel = cancel
	go m.loop(ctx)
}

func (m *BrowserManager) Stop() {
	m.mu.Lock()
	m.stopping = true
	command := m.command
	m.mu.Unlock()
	if m.cancel != nil {
		m.cancel()
	}
	if command != nil && command.Process != nil {
		killProcessTree(command.Process.Pid)
	}
	select {
	case <-m.done:
	case <-time.After(10 * time.Second):
	}
}

func (m *BrowserManager) Snapshot() BrowserSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	pid := 0
	if m.command != nil && m.command.Process != nil {
		pid = m.command.Process.Pid
	}
	return BrowserSnapshot{Enabled: m.cfg.HeadlessEnabled, Running: pid != 0, PID: pid, Client: m.launchClient(), Executable: executableOf(m.command), ProfileDir: m.cfg.HeadlessProfileDir, StartedAt: m.started, LastExit: m.lastExit}
}

func (m *BrowserManager) loop(ctx context.Context) {
	defer close(m.done)
	for {
		if ctx.Err() != nil {
			return
		}
		executable := m.findExecutable()
		if executable == "" {
			m.mu.Lock()
			m.lastExit = map[string]any{"at": time.Now().UnixMilli(), "error": "no supported Chrome or Edge executable found"}
			m.mu.Unlock()
			return
		}
		_ = os.MkdirAll(m.cfg.HeadlessProfileDir, 0o700)
		clientName := m.launchClient()
		url := fmt.Sprintf("http://127.0.0.1:%d/zcode/captcha/browser?client=%s", m.port, clientName)
		args := []string{"--user-data-dir=" + m.cfg.HeadlessProfileDir, "--no-first-run", "--no-default-browser-check", "--no-proxy-server", "--disable-breakpad", "--disable-component-extensions-with-background-pages", "--disable-component-update", "--disable-default-apps", "--disable-extensions", "--disable-sync", "--disable-features=GlobalMediaControls,MediaRouter,OptimizationHints,Translate,msEdgeUpdateLaunchServicesPreferredVersion", "--metrics-recording-only", "--mute-audio", "--window-size=1100,900"}
		if clientName == "headless-browser" {
			args = append(args, "--headless=new")
		}
		args = append(args, url)
		command := exec.Command(executable, args...)
		if clientName == "headless-browser" {
			hideProcess(command)
		}
		if err := command.Start(); err != nil {
			m.mu.Lock()
			m.lastExit = map[string]any{"at": time.Now().UnixMilli(), "error": err.Error()}
			m.mu.Unlock()
			return
		}
		if err := attachProcess(command); err != nil {
			killProcessTree(command.Process.Pid)
			_ = command.Wait()
			m.mu.Lock()
			m.lastExit = map[string]any{"at": time.Now().UnixMilli(), "error": err.Error()}
			m.mu.Unlock()
			return
		}
		now := time.Now().UTC()
		m.mu.Lock()
		m.command = command
		m.started = &now
		m.mu.Unlock()
		log.Printf("captcha browser started: client=%s executable=%s pid=%d", clientName, filepath.Base(executable), command.Process.Pid)
		launchedAt := time.Now()
		err := command.Wait()
		lifetime := time.Since(launchedAt)
		m.mu.Lock()
		m.command = nil
		m.lastExit = map[string]any{"at": time.Now().UnixMilli(), "error": errorText(err)}
		stopping := m.stopping
		m.mu.Unlock()
		if stopping || ctx.Err() != nil {
			return
		}
		if lifetime < 10*time.Second {
			m.mu.Lock()
			m.lastExit = map[string]any{
				"at":       time.Now().UnixMilli(),
				"error":    "captcha browser exited immediately; automatic restart suppressed to avoid opening repeated tabs",
				"lifetime": lifetime.String(),
			}
			m.mu.Unlock()
			log.Printf("captcha browser exited after %s; restart suppressed to avoid repeated tabs", lifetime.Round(time.Millisecond))
			return
		}
		timer := time.NewTimer(m.cfg.HeadlessRestartDelay)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return
		}
	}
}

func (m *BrowserManager) launchClient() string {
	if m.cfg.CaptchaPreferredClient == "headless-browser" {
		return "headless-browser"
	}
	return "standalone-browser"
}

func (m *BrowserManager) findExecutable() string {
	if m.cfg.HeadlessExecutable != "" {
		if _, err := os.Stat(m.cfg.HeadlessExecutable); err == nil {
			return m.cfg.HeadlessExecutable
		}
	}
	var candidates []string
	switch runtime.GOOS {
	case "windows":
		candidates = []string{
			filepath.Join(os.Getenv("ProgramFiles"), "Google", "Chrome", "Application", "chrome.exe"),
			filepath.Join(os.Getenv("ProgramFiles(x86)"), "Google", "Chrome", "Application", "chrome.exe"),
			filepath.Join(os.Getenv("ProgramFiles(x86)"), "Microsoft", "Edge", "Application", "msedge.exe"),
			filepath.Join(os.Getenv("ProgramFiles"), "Microsoft", "Edge", "Application", "msedge.exe"),
			filepath.Join(os.Getenv("LOCALAPPDATA"), "Microsoft", "Edge", "Application", "msedge.exe"),
		}
	case "darwin":
		candidates = []string{"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", "/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge"}
	default:
		candidates = []string{"/usr/bin/google-chrome", "/usr/bin/google-chrome-stable", "/usr/bin/chromium", "/usr/bin/chromium-browser", "/usr/bin/microsoft-edge", "/usr/bin/microsoft-edge-stable"}
	}
	for _, candidate := range candidates {
		if candidate != "" {
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
		}
	}
	return ""
}

func executableOf(command *exec.Cmd) string {
	if command == nil {
		return ""
	}
	return command.Path
}

func errorText(err error) any {
	if err == nil {
		return nil
	}
	return err.Error()
}
