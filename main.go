package main

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"glm5.2proxy/internal/app"
)

//go:embed cmd/desktop/frontend/dist
var assets embed.FS

type Desktop struct {
	service *app.Service
	cancel  context.CancelFunc
	done    chan struct{}
}

type APIResponse struct {
	Status int    `json:"status"`
	Body   string `json:"body"`
}

func (d *Desktop) startup(wailsCtx context.Context) {
	ctx, cancel := context.WithCancel(context.Background())
	d.cancel = cancel
	go func() {
		defer close(d.done)
		if err := d.service.Run(ctx); err != nil {
			log.Printf("backend stopped: %v", err)
			wailsruntime.Quit(wailsCtx)
		}
	}()
}

func (d *Desktop) shutdown(_ context.Context) {
	if d.cancel != nil {
		d.cancel()
	}
	select {
	case <-d.done:
	case <-time.After(12 * time.Second):
	}
}

func (d *Desktop) Port() int {
	return d.service.Port()
}

func (d *Desktop) APIRequest(method, path, body string) (APIResponse, error) {
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		method = http.MethodGet
	}
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || strings.Contains(path, "://") {
		return APIResponse{}, errors.New("caminho de API invalido")
	}
	var lastErr error
	deadline := time.Now().Add(5 * time.Second)
	for {
		response, err := d.apiRequestOnce(method, path, body)
		if err == nil {
			return response, nil
		}
		lastErr = err
		if time.Now().After(deadline) {
			return APIResponse{}, lastErr
		}
		time.Sleep(150 * time.Millisecond)
	}
}

func (d *Desktop) apiRequestOnce(method, path, body string) (APIResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), apiRequestTimeout(path))
	defer cancel()
	requestURL := "http://127.0.0.1:" + fmt.Sprint(d.service.Port()) + path
	request, err := http.NewRequestWithContext(ctx, method, requestURL, bytes.NewBufferString(body))
	if err != nil {
		return APIResponse{}, err
	}
	request.Header.Set("Accept", "application/json")
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return APIResponse{}, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 25<<20))
	if err != nil {
		return APIResponse{}, err
	}
	return APIResponse{Status: response.StatusCode, Body: string(raw)}, nil
}

func apiRequestTimeout(path string) time.Duration {
	switch {
	case strings.HasPrefix(path, "/api/admin/account-creator/run"):
		return 12 * time.Minute
	default:
		return 45 * time.Second
	}
}

func (d *Desktop) OpenExternalURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return errInvalidURL()
	}
	switch parsed.Scheme {
	case "http", "https":
	default:
		return errInvalidURL()
	}
	return openExternalURL(rawURL)
}

func main() {
	service, err := app.New()
	if err != nil {
		log.Fatal(err)
	}
	frontend, err := fs.Sub(assets, "cmd/desktop/frontend/dist")
	if err != nil {
		log.Fatal(err)
	}
	desktop := &Desktop{service: service, done: make(chan struct{})}
	err = wails.Run(&options.App{
		Title:  "glm5.2proxy",
		Width:  1180,
		Height: 760,
		AssetServer: &assetserver.Options{
			Assets: frontend,
		},
		OnStartup:  desktop.startup,
		OnShutdown: desktop.shutdown,
		Bind:       []any{desktop},
	})
	if err != nil {
		log.Fatal(err)
	}
}

func errInvalidURL() error {
	return errors.New("URL de autenticacao invalida")
}

func openExternalURL(rawURL string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL).Start()
	case "darwin":
		return exec.Command("open", rawURL).Start()
	default:
		return exec.Command("xdg-open", rawURL).Start()
	}
}
