package automation

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"glm5.2proxy/internal/config"
)

//go:embed assets/**
var embeddedAssets embed.FS

type Prepared struct {
	RootDir          string
	CreatorDir       string
	SolverDir        string
	CreatorDataDir   string
	CreatorLogDir    string
	NodeCommand      string
	NPMCommand       string
	SolverAPIBase    string
	CreatorLogFile   string
	CreatorEmailFile string
}

type Manager struct {
	cfg config.Config
	mu  sync.Mutex
}

func New(cfg config.Config) *Manager {
	return &Manager{cfg: cfg}
}

func (m *Manager) Layout() Prepared {
	rootDir := filepath.Join(m.cfg.DataDir, "embedded-automation")
	return Prepared{
		RootDir:          rootDir,
		CreatorDir:       filepath.Join(rootDir, "account-creator"),
		SolverDir:        filepath.Join(rootDir, "aliyun-captcha-solver"),
		CreatorDataDir:   filepath.Join(m.cfg.DataDir, "account-creator-data"),
		CreatorLogDir:    filepath.Join(m.cfg.DataDir, "account-creator-logs"),
		SolverAPIBase:    "http://127.0.0.1:8787",
		CreatorLogFile:   filepath.Join(m.cfg.DataDir, "account-creator-logs", "run.log"),
		CreatorEmailFile: filepath.Join(m.cfg.DataDir, "account-creator-data", "emails.json"),
	}
}

func (m *Manager) Prepare(ctx context.Context) (Prepared, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	nodeCommand, err := resolveCommand("node")
	if err != nil {
		return Prepared{}, fmt.Errorf("node nao encontrado para a automacao embutida: %w", err)
	}
	npmName := "npm"
	if runtime.GOOS == "windows" {
		npmName = "npm.cmd"
	}
	npmCommand, err := resolveCommand(npmName)
	if err != nil {
		return Prepared{}, fmt.Errorf("npm nao encontrado para a automacao embutida: %w", err)
	}

	prepared := m.Layout()
	rootDir := prepared.RootDir
	if err := extractAssets(rootDir); err != nil {
		return Prepared{}, err
	}

	prepared.NodeCommand = nodeCommand
	prepared.NPMCommand = npmCommand

	if err := os.MkdirAll(prepared.CreatorDataDir, 0o700); err != nil {
		return Prepared{}, fmt.Errorf("nao foi possivel preparar dados do criador de contas: %w", err)
	}
	if err := os.MkdirAll(prepared.CreatorLogDir, 0o700); err != nil {
		return Prepared{}, fmt.Errorf("nao foi possivel preparar logs do criador de contas: %w", err)
	}

	if err := ensureDependencies(ctx, prepared.NPMCommand, prepared.CreatorDir); err != nil {
		return Prepared{}, err
	}
	if err := ensureDependencies(ctx, prepared.NPMCommand, prepared.SolverDir); err != nil {
		return Prepared{}, err
	}

	return prepared, nil
}

func resolveCommand(name string) (string, error) {
	command, err := exec.LookPath(name)
	if err != nil {
		return "", err
	}
	return command, nil
}

func extractAssets(rootDir string) error {
	return fs.WalkDir(embeddedAssets, "assets", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == "assets" {
			return nil
		}
		relative := strings.TrimPrefix(path, "assets/")
		target := filepath.Join(rootDir, filepath.FromSlash(relative))
		if entry.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		raw, err := embeddedAssets.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		existing, err := os.ReadFile(target)
		if err == nil && string(existing) == string(raw) {
			return nil
		}
		return os.WriteFile(target, raw, 0o600)
	})
}

func ensureDependencies(ctx context.Context, npmCommand, workDir string) error {
	packageFile := filepath.Join(workDir, "package.json")
	hash, err := fileHash(packageFile)
	if err != nil {
		return fmt.Errorf("nao foi possivel ler %s: %w", packageFile, err)
	}
	stampFile := filepath.Join(workDir, ".deps.sha256")
	stamp, _ := os.ReadFile(stampFile)
	if strings.TrimSpace(string(stamp)) == hash {
		if info, err := os.Stat(filepath.Join(workDir, "node_modules")); err == nil && info.IsDir() {
			return nil
		}
	}

	command := exec.CommandContext(ctx, npmCommand, "install", "--omit=dev")
	command.Dir = workDir
	command.Env = append(os.Environ(),
		"PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1",
		"PUPPETEER_SKIP_DOWNLOAD=1",
	)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("npm install falhou em %s: %w: %s", workDir, err, trimCommandOutput(string(output)))
	}
	if err := os.WriteFile(stampFile, []byte(hash), 0o600); err != nil {
		return fmt.Errorf("nao foi possivel salvar carimbo de dependencias em %s: %w", workDir, err)
	}
	return nil
}

func fileHash(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func trimCommandOutput(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 4000 {
		return value
	}
	return value[len(value)-4000:]
}
