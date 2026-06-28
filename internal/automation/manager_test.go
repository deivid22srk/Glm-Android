package automation

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"glm5.2proxy/internal/config"
)

func TestPrepareAndSolverHealth(t *testing.T) {
	if os.Getenv("GLM52PROXY_AUTOMATION_SMOKE") != "1" {
		t.Skip("set GLM52PROXY_AUTOMATION_SMOKE=1 to run automation bootstrap smoke")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	prepared, err := New(config.Load()).Prepare(ctx)
	if err != nil {
		t.Fatalf("prepare failed: %v", err)
	}

	assertFileExists(t, filepath.Join(prepared.CreatorDir, "src", "main.js"))
	assertFileExists(t, filepath.Join(prepared.SolverDir, "server.js"))

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	serverCtx, serverCancel := context.WithCancel(context.Background())
	defer serverCancel()

	command := exec.CommandContext(serverCtx, prepared.NodeCommand, "server.js")
	command.Dir = prepared.SolverDir
	command.Env = append(os.Environ(),
		"API_HOST=127.0.0.1",
		fmt.Sprintf("API_PORT=%d", port),
	)
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Start(); err != nil {
		t.Fatalf("start solver server: %v", err)
	}
	defer func() {
		serverCancel()
		_ = command.Wait()
	}()

	healthURL := fmt.Sprintf("http://127.0.0.1:%d/health", port)
	deadline := time.Now().Add(25 * time.Second)
	for time.Now().Before(deadline) {
		response, err := http.Get(healthURL)
		if err == nil {
			raw, _ := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK && strings.Contains(string(raw), `"ok": true`) {
				return
			}
		}
		time.Sleep(500 * time.Millisecond)
	}

	t.Fatalf("solver health endpoint did not come up: %s", output.String())
}

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file %s: %v", path, err)
	}
}
