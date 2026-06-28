package quota

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"glm5.2proxy/internal/config"
	"glm5.2proxy/internal/models"
	"glm5.2proxy/internal/upstream"
)

func TestSnapshotReportsEmptyHTTPStatusInsteadOfEOF(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	cfg := config.Load()
	cfg.BillingBaseURL = server.URL
	service := New(cfg)

	_, err := service.Snapshot(context.Background(), upstream.Config{
		BaseHeaders: map[string]string{
			"authorization":       "Bearer test-token",
			"x-zcode-app-version": "3.0.1",
		},
		HasAuthorization: true,
	})
	if err == nil {
		t.Fatal("expected quota snapshot error")
	}
	message := err.Error()
	if !strings.Contains(message, "HTTP 429") || strings.Contains(message, "EOF") {
		t.Fatalf("unexpected quota error message: %q", message)
	}
}

func TestBeginRequestHonorsContextWhileGateIsBusy(t *testing.T) {
	service := New(config.Config{})
	release, err := service.beginRequest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err = service.beginRequest(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context deadline while waiting for billing gate, got %v", err)
	}
}

func TestSnapshotCachedReusesFreshSnapshot(t *testing.T) {
	currentCalls := 0
	balanceCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/current":
			currentCalls++
			_, _ = w.Write([]byte(`{"code":0,"data":{"plans":[]}}`))
		case "/balance":
			balanceCalls++
			_, _ = w.Write([]byte(`{"code":0,"data":{"balances":[]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := config.Config{BillingBaseURL: server.URL, AppVersion: "3.0.1"}
	service := New(cfg)
	upstreamConfig := upstream.Config{
		BaseHeaders: map[string]string{
			"authorization":       "Bearer test-token",
			"x-zcode-app-version": "3.0.1",
		},
		HasAuthorization: true,
	}

	if _, err := service.SnapshotCached(context.Background(), upstreamConfig, time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SnapshotCached(context.Background(), upstreamConfig, time.Minute); err != nil {
		t.Fatal(err)
	}
	if currentCalls != 1 || balanceCalls != 1 {
		t.Fatalf("expected cached quota snapshot, current=%d balance=%d", currentCalls, balanceCalls)
	}
}

func TestModelBalanceCachedCoalescesConcurrentRequests(t *testing.T) {
	var balanceCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/current" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"data":{"plans":[]}}`))
			return
		}
		if r.URL.Path != "/balance" {
			http.NotFound(w, r)
			return
		}
		balanceCalls.Add(1)
		time.Sleep(25 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"balances":[{"show_name":"GLM-5.2","available_units":123}]}}`))
	}))
	defer server.Close()

	cfg := config.Config{BillingBaseURL: server.URL, AppVersion: "3.1.2"}
	service := New(cfg)
	upstreamConfig := upstream.Config{
		BaseHeaders: map[string]string{
			"authorization":       "Bearer test-token",
			"x-zcode-app-version": "3.1.2",
		},
		HasAuthorization: true,
	}
	model := models.Model{ID: "glm-5.2", UpstreamID: "GLM-5.2"}

	var wait sync.WaitGroup
	errs := make(chan error, 5)
	for range 5 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			balance, err := service.ModelBalanceCached(context.Background(), upstreamConfig, model, time.Minute)
			if err != nil {
				errs <- err
				return
			}
			if balance == nil || balance.Available == nil || *balance.Available != 123 {
				errs <- errors.New("unexpected cached balance")
			}
		}()
	}
	wait.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if balanceCalls.Load() != 1 {
		t.Fatalf("expected one coalesced balance request, got %d", balanceCalls.Load())
	}
}

func TestModelBalanceCachedUsesUsageQuotaEndpointWhenConfigured(t *testing.T) {
	var quotaCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/usage/quota/limit" {
			t.Errorf("unexpected billing endpoint call: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "coding-key.secret" {
			t.Errorf("unexpected quota authorization: %s", r.Header.Get("Authorization"))
		}
		quotaCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"data":{"limits":[{"type":"model_usage","number":3000000,"remaining":123,"usageDetails":[{"modelCode":"GLM-5.2","usage":42}]}]}}`))
	}))
	defer server.Close()

	service := New(config.Config{BillingBaseURL: "http://127.0.0.1/should-not-be-used", AppVersion: "3.1.2"})
	model := models.Model{ID: "glm-5.2", UpstreamID: "GLM-5.2"}
	balance, err := service.ModelBalanceCached(context.Background(), upstream.Config{
		QuotaEndpoint:      server.URL + "/usage/quota/limit",
		QuotaAuthorization: "coding-key.secret",
		BaseHeaders:        map[string]string{"user-agent": "ZCode/3.1.2"},
		HasAuthorization:   true,
	}, model, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if balance == nil || balance.Available == nil || *balance.Available != 123 {
		t.Fatalf("unexpected usage quota balance: %+v", balance)
	}
	if quotaCalls.Load() != 1 {
		t.Fatalf("expected one usage quota request, got %d", quotaCalls.Load())
	}
}
