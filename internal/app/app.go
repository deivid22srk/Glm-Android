package app

import (
        "context"
        "fmt"
        "io/fs"
        "os"
        "time"

        "glm5.2proxy/internal/accountpool"
        "glm5.2proxy/internal/accounts"
        "glm5.2proxy/internal/api"
        "glm5.2proxy/internal/auth"
        "glm5.2proxy/internal/captcha"
        "glm5.2proxy/internal/config"
        "glm5.2proxy/internal/proxy"
        "glm5.2proxy/internal/quota"
        "glm5.2proxy/internal/state"
        "glm5.2proxy/internal/upstream"
)

type Service struct {
        cfg     config.Config
        admin   *state.AdminStore
        browser *captcha.BrowserManager
        server  *api.Server
}

// New creates the service without serving any embedded frontend.
// Kept for backward compatibility (desktop Wails entrypoint and tests).
func New() (*Service, error) {
        return NewWithFrontend(nil)
}

// NewWithFrontend creates the service and optionally attaches an embedded
// frontend filesystem (fs.FS) that the HTTP server will serve at "/".
// Pass nil to keep the headless behavior (no panel served).
func NewWithFrontend(frontend fs.FS) (*Service, error) {
        cfg := config.Load()
        admin, err := state.NewAdminStore(cfg.AdminPath, cfg.DefaultPort, state.ThinkingSettings{Enabled: cfg.DefaultThinkingEnabled, BudgetTokens: cfg.DefaultThinkingBudget, Effort: cfg.DefaultEffort})
        if err != nil {
                return nil, err
        }
        port := admin.Snapshot().Port
        if os.Getenv("PORT") != "" || os.Getenv("ZCODE_PROXY_PORT") != "" {
                port = cfg.DefaultPort
        }
        accountStore, err := accounts.NewStore(cfg.CredentialsPath, cfg.CredentialSecret)
        if err != nil {
                return nil, err
        }
        loader := upstream.NewLoader(cfg, accountStore)
        quotaService := quota.New(cfg)
        accountPool := accountpool.New(cfg, accountStore, loader, quotaService)
        oauthService := auth.New(cfg, accountStore)
        bridge := captcha.NewBridge(cfg)
        browser := captcha.NewBrowserManager(cfg, port)
        proxyService := proxy.New(cfg, bridge)
        server := api.New(cfg, port, admin, accountStore, oauthService, quotaService, accountPool, loader, bridge, browser, proxyService)
        if frontend != nil {
                server.SetFrontend(frontend)
        }
        return &Service{cfg: cfg, admin: admin, browser: browser, server: server}, nil
}

func (s *Service) Run(ctx context.Context) error {
        listener, err := s.server.Listen()
        if err != nil {
                return fmt.Errorf("open proxy listener on 127.0.0.1:%d: %w", s.server.Port(), err)
        }
        s.browser.Start(ctx)
        errorChannel := make(chan error, 1)
        go func() { errorChannel <- s.server.Serve(listener) }()
        go func() {
                startupCtx, cancel := context.WithTimeout(ctx, 4*time.Minute)
                defer cancel()
                migration := s.server.RemoveLegacyAuthAccountsOnce(startupCtx)
                s.server.SelectStartupAccount(startupCtx)
                if migration.Trigger == "startup_auth_migration" && migration.Total > 0 {
                        return
                }
                s.server.RepairBrokenAccounts(startupCtx, "startup")
        }()
        select {
        case err := <-errorChannel:
                s.browser.Stop()
                return err
        case <-ctx.Done():
                s.browser.Stop()
                shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
                defer cancel()
                if err := s.server.Shutdown(shutdownCtx); err != nil {
                        return fmt.Errorf("HTTP shutdown failed: %w", err)
                }
                return nil
        }
}

func (s *Service) Port() int { return s.server.Port() }
