package tests

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"glm5.2proxy/internal/captcha"
	"glm5.2proxy/internal/proxy"
	"glm5.2proxy/internal/upstream"
)

func TestCaptchaBridgeAndProxyRetry(t *testing.T) {
	cfg := testConfig(t)
	cfg.CaptchaEnabled = true
	bridge := captcha.NewBridge(cfg)
	pollRecorder := httptest.NewRecorder()
	pollRequest := httptest.NewRequest(http.MethodGet, "/zcode/captcha/poll?client=headless-browser", nil)
	pollDone := make(chan struct{})
	go func() {
		bridge.Poll(pollRecorder, pollRequest)
		close(pollDone)
	}()
	time.Sleep(20 * time.Millisecond)
	tokenResult := make(chan string, 1)
	go func() {
		token, _ := bridge.Fresh(context.Background())
		tokenResult <- token
	}()
	<-pollDone
	var captchaRequest captcha.Request
	if err := json.Unmarshal(pollRecorder.Body.Bytes(), &captchaRequest); err != nil {
		t.Fatal(err)
	}
	submit := httptest.NewRequest(http.MethodPost, "/zcode/captcha/submit", strings.NewReader(`{"id":"`+captchaRequest.ID+`","token":"proof","region":"sgp"}`))
	submit.Header.Set("Content-Type", "application/json")
	bridge.Submit(httptest.NewRecorder(), submit)
	if token := <-tokenResult; token != "proof" {
		t.Fatalf("unexpected captcha token: %q", token)
	}

	cfg.CaptchaEnabled = false
	var attempts atomic.Int32
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		if attempts.Add(1) == 1 {
			w.Write([]byte("event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"code\":\"1305\",\"message\":\"busy\"}}\n\n"))
			return
		}
		w.Write([]byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer mock.Close()
	service := proxy.New(cfg, bridge)
	completion, _, err := service.Collect(context.Background(), upstream.Config{Endpoint: mock.URL, BaseHeaders: map[string]string{"authorization": "Bearer test"}, HasAuthorization: true}, map[string]any{"model": "GLM-5.2"})
	if err != nil || completion.Text != "ok" || attempts.Load() != 2 {
		t.Fatalf("retry failed: completion=%+v attempts=%d err=%v", completion, attempts.Load(), err)
	}
}

func TestProxyRetriesAdmissionConcurrencyLimit(t *testing.T) {
	cfg := testConfig(t)
	cfg.CaptchaEnabled = false
	cfg.RetryBaseDelay = time.Millisecond
	var attempts atomic.Int32
	var sessions []string
	var requestIDs []string
	var traceIDs []string
	var queryIDs []string
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sessions = append(sessions, r.Header.Get("X-Session-ID"))
		requestIDs = append(requestIDs, r.Header.Get("X-Request-ID"))
		traceIDs = append(traceIDs, r.Header.Get("X-ZCode-Trace-ID"))
		queryIDs = append(queryIDs, r.Header.Get("X-Query-ID"))
		w.Header().Set("Content-Type", "text/event-stream")
		if attempts.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"type":"zcode_upstream_error","message":"model admission concurrency limit exceeded"}}`))
			return
		}
		_, _ = w.Write([]byte("data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer mock.Close()

	service := proxy.New(cfg, captcha.NewBridge(cfg))
	completion, _, err := service.Collect(context.Background(), upstream.Config{Endpoint: mock.URL, BaseHeaders: map[string]string{"authorization": "Bearer test"}, HasAuthorization: true}, map[string]any{"model": "GLM-5.2"})
	if err != nil || completion.Text != "ok" || attempts.Load() != 2 || sessions[0] == "" || sessions[0] != sessions[1] || requestIDs[0] == "" || requestIDs[0] != requestIDs[1] || traceIDs[0] == "" || traceIDs[0] != traceIDs[1] || queryIDs[0] == "" || queryIDs[0] != queryIDs[1] {
		t.Fatalf("admission retry failed: completion=%+v attempts=%d sessions=%v requestIDs=%v traceIDs=%v queryIDs=%v err=%v", completion, attempts.Load(), sessions, requestIDs, traceIDs, queryIDs, err)
	}
}

func TestProxyDoesNotPrefetchCaptchaForNormalRequest(t *testing.T) {
	cfg := testConfig(t)
	cfg.CaptchaEnabled = true
	bridge := captcha.NewBridge(cfg)
	var attempts atomic.Int32
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		if value := r.Header.Get("X-Aliyun-Captcha-Verify-Param"); value != "" {
			t.Fatalf("normal request should not prefetch captcha token, got %q", value)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer mock.Close()

	service := proxy.New(cfg, bridge)
	completion, streamAttempts, err := service.Collect(context.Background(), upstream.Config{Endpoint: mock.URL, BaseHeaders: map[string]string{"authorization": "Bearer test"}, HasAuthorization: true}, map[string]any{"model": "GLM-5.2"})
	if err != nil || completion.Text != "ok" || streamAttempts != 1 || attempts.Load() != 1 {
		t.Fatalf("normal request failed: completion=%+v streamAttempts=%d upstreamAttempts=%d err=%v", completion, streamAttempts, attempts.Load(), err)
	}
}

func TestProxyFetchesCaptchaOnlyAfterUpstreamChallenge(t *testing.T) {
	cfg := testConfig(t)
	cfg.CaptchaEnabled = true
	cfg.RetryBaseDelay = time.Millisecond
	bridge := captcha.NewBridge(cfg)

	pollDone := make(chan captcha.Request, 1)
	go func() {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/zcode/captcha/poll?client=headless-browser", nil)
		bridge.Poll(recorder, request)
		var captchaRequest captcha.Request
		_ = json.Unmarshal(recorder.Body.Bytes(), &captchaRequest)
		pollDone <- captchaRequest
	}()
	go func() {
		captchaRequest := <-pollDone
		submit := httptest.NewRequest(http.MethodPost, "/zcode/captcha/submit", strings.NewReader(`{"id":"`+captchaRequest.ID+`","token":"proof","region":"sgp"}`))
		submit.Header.Set("Content-Type", "application/json")
		bridge.Submit(httptest.NewRecorder(), submit)
	}()

	var attempts atomic.Int32
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := attempts.Add(1)
		if current == 1 {
			if value := r.Header.Get("X-Aliyun-Captcha-Verify-Param"); value != "" {
				t.Fatalf("first request should not include captcha token, got %q", value)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":{"message":"captcha verification required","type":"zcode_upstream_error"}}`))
			return
		}
		if value := r.Header.Get("X-Aliyun-Captcha-Verify-Param"); value != "proof" {
			t.Fatalf("second request should include fresh captcha token, got %q", value)
		}
		if value := r.Header.Get("X-Aliyun-Captcha-Verify-Region"); value != "sgp" {
			t.Fatalf("second request should include captcha region, got %q", value)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer mock.Close()

	service := proxy.New(cfg, bridge)
	completion, streamAttempts, err := service.Collect(context.Background(), upstream.Config{Endpoint: mock.URL, BaseHeaders: map[string]string{"authorization": "Bearer test"}, HasAuthorization: true}, map[string]any{"model": "GLM-5.2"})
	if err != nil || completion.Text != "ok" || streamAttempts != 2 || attempts.Load() != 2 {
		t.Fatalf("captcha retry failed: completion=%+v streamAttempts=%d upstreamAttempts=%d err=%v", completion, streamAttempts, attempts.Load(), err)
	}
}

func TestProxyStreamRetriesAdmissionConcurrencyLimit(t *testing.T) {
	cfg := testConfig(t)
	cfg.CaptchaEnabled = false
	cfg.RetryBaseDelay = time.Millisecond
	var attempts atomic.Int32
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		if attempts.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"message":"model admission concurrency limit exceeded"}}`))
			return
		}
		_, _ = w.Write([]byte("data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"stream ok\"}}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer mock.Close()

	service := proxy.New(cfg, captcha.NewBridge(cfg))
	var text string
	streamAttempts, err := service.Stream(context.Background(), upstream.Config{Endpoint: mock.URL, BaseHeaders: map[string]string{"authorization": "Bearer test"}, HasAuthorization: true}, map[string]any{"model": "GLM-5.2"}, func(event proxy.StreamEvent) error {
		if content, ok := event.Delta["content"].(string); ok {
			text += content
		}
		return nil
	})
	if err != nil || text != "stream ok" || attempts.Load() != 2 || streamAttempts != 2 {
		t.Fatalf("stream admission retry failed: text=%q attempts=%d streamAttempts=%d err=%v", text, attempts.Load(), streamAttempts, err)
	}
}

func TestProxyRetriesEmptyUpstreamStream(t *testing.T) {
	cfg := testConfig(t)
	cfg.CaptchaEnabled = false
	cfg.RetryBaseDelay = time.Millisecond
	var attempts atomic.Int32
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		if attempts.Add(1) == 1 {
			_, _ = w.Write([]byte("data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
			return
		}
		_, _ = w.Write([]byte("data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"recovered\"}}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer mock.Close()

	service := proxy.New(cfg, captcha.NewBridge(cfg))
	completion, streamAttempts, err := service.Collect(context.Background(), upstream.Config{Endpoint: mock.URL, BaseHeaders: map[string]string{"authorization": "Bearer test"}, HasAuthorization: true}, map[string]any{"model": "GLM-5.2"})
	if err != nil || completion.Text != "recovered" || attempts.Load() != 2 || streamAttempts != 2 {
		t.Fatalf("empty stream recovery failed: completion=%+v attempts=%d streamAttempts=%d err=%v", completion, attempts.Load(), streamAttempts, err)
	}
}

func TestProxyStreamingDoesNotEmitFalseStopBeforeEmptyStreamRetry(t *testing.T) {
	cfg := testConfig(t)
	cfg.CaptchaEnabled = false
	cfg.RetryBaseDelay = time.Millisecond
	var attempts atomic.Int32
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		if attempts.Add(1) == 1 {
			_, _ = w.Write([]byte("data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
			return
		}
		_, _ = w.Write([]byte("data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"recovered\"}}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer mock.Close()

	service := proxy.New(cfg, captcha.NewBridge(cfg))
	var events []proxy.StreamEvent
	streamAttempts, err := service.Stream(context.Background(), upstream.Config{Endpoint: mock.URL, BaseHeaders: map[string]string{"authorization": "Bearer test"}, HasAuthorization: true}, map[string]any{"model": "GLM-5.2"}, func(event proxy.StreamEvent) error {
		events = append(events, event)
		return nil
	})
	if err != nil || attempts.Load() != 2 || streamAttempts != 2 || len(events) != 2 || events[0].Delta["content"] != "recovered" || events[1].FinishReason != "stop" {
		t.Fatalf("stream empty recovery failed: events=%+v attempts=%d streamAttempts=%d err=%v", events, attempts.Load(), streamAttempts, err)
	}
}

func TestProxyStreamingDoesNotRetryAfterClientEmission(t *testing.T) {
	cfg := testConfig(t)
	cfg.CaptchaEnabled = false
	cfg.RetryBaseDelay = time.Millisecond
	var attempts atomic.Int32
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		if attempts.Add(1) == 1 {
			_, _ = w.Write([]byte("data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"broken\"}}\n\n"))
			return
		}
		_, _ = w.Write([]byte("data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"recovered\"}}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer mock.Close()

	service := proxy.New(cfg, captcha.NewBridge(cfg))
	var text string
	var finishes int
	streamAttempts, err := service.Stream(context.Background(), upstream.Config{Endpoint: mock.URL, BaseHeaders: map[string]string{"authorization": "Bearer test"}, HasAuthorization: true}, map[string]any{"model": "GLM-5.2"}, func(event proxy.StreamEvent) error {
		if value, ok := event.Delta["content"].(string); ok {
			text += value
		}
		if event.FinishReason != "" {
			finishes++
		}
		return nil
	})
	if !proxy.IsStaleConnection(err) || attempts.Load() != 1 || streamAttempts != 1 || text != "broken" || finishes != 0 {
		t.Fatalf("partial stream should stay on emitted attempt: text=%q finishes=%d attempts=%d streamAttempts=%d err=%v", text, finishes, attempts.Load(), streamAttempts, err)
	}
}

func TestProxyRetriesServerError(t *testing.T) {
	cfg := testConfig(t)
	cfg.CaptchaEnabled = false
	cfg.RetryBaseDelay = time.Millisecond
	var attempts atomic.Int32
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":{"message":"temporarily unavailable"}}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":100,\"cache_read_input_tokens\":25}}}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":7}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer mock.Close()

	service := proxy.New(cfg, captcha.NewBridge(cfg))
	completion, _, err := service.Collect(context.Background(), upstream.Config{Endpoint: mock.URL, BaseHeaders: map[string]string{"authorization": "Bearer test"}, HasAuthorization: true}, map[string]any{"model": "GLM-5.2"})
	if err != nil || completion.Text != "ok" || attempts.Load() != 2 || completion.Usage["prompt_tokens"] != 125 || completion.Usage["completion_tokens"] != 7 || completion.Usage["total_tokens"] != 132 {
		t.Fatalf("server error recovery failed: completion=%+v attempts=%d err=%v", completion, attempts.Load(), err)
	}
}

func TestAdmissionConcurrencyIsNotQuotaExhausted(t *testing.T) {
	err := &proxy.UpstreamError{Status: http.StatusTooManyRequests, Message: "model admission concurrency limit exceeded"}
	if proxy.IsQuotaExhausted(err) {
		t.Fatal("admission concurrency should wait/retry, not rotate as quota exhaustion")
	}
}

func TestOverloaded1305IsRotatableOverload(t *testing.T) {
	err := &proxy.UpstreamError{Status: http.StatusTooManyRequests, Code: "1305", Message: "[1305]该模型当前访问量过大，请您稍后再试"}
	if !proxy.IsOverloaded(err) {
		t.Fatal("1305 should be treated as upstream overload")
	}
	if proxy.IsQuotaExhausted(err) {
		t.Fatal("1305 overload should not be treated as quota exhaustion")
	}
}

func TestUnknownTooManyRequestsIsQuotaOrRateLimited(t *testing.T) {
	err := &proxy.UpstreamError{Status: http.StatusTooManyRequests, Message: "Unknown upstream error"}
	if !proxy.IsQuotaExhausted(err) {
		t.Fatal("unknown 429 should be treated as account-rotatable quota/rate limit")
	}
}

func TestWAF405HTMLIsClassifiedAsWAFBlock(t *testing.T) {
	cfg := testConfig(t)
	cfg.CaptchaEnabled = false
	bridge := captcha.NewBridge(cfg)
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Server", "ESA")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_, _ = w.Write([]byte(`<!doctypehtml><html><body>Request has been blocked by aliyun errors.aliyun.com</body></html>`))
	}))
	defer mock.Close()

	service := proxy.New(cfg, bridge)
	_, _, err := service.Collect(context.Background(), upstream.Config{
		Endpoint:         mock.URL,
		BaseHeaders:      map[string]string{"authorization": "Bearer test"},
		HasAuthorization: true,
	}, map[string]any{"model": "GLM-5.2"})
	if err == nil {
		t.Fatal("expected WAF error")
	}
	upstreamErr, ok := err.(*proxy.UpstreamError)
	if !ok {
		t.Fatalf("expected upstream error, got %T", err)
	}
	if upstreamErr.Type != "zcode_waf_block" || upstreamErr.Code != "esa_405" || !proxy.IsWAFBlocked(upstreamErr) {
		t.Fatalf("expected WAF classification, got %+v", upstreamErr)
	}
	if proxy.IsUnknownUpstreamError(upstreamErr) {
		t.Fatalf("waf block must not degrade to unknown upstream error: %+v", upstreamErr)
	}
}

func TestProxyStreamingEvents(t *testing.T) {
	cfg := testConfig(t)
	cfg.CaptchaEnabled = false
	bridge := captcha.NewBridge(cfg)
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"stream\"}}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer mock.Close()
	service := proxy.New(cfg, bridge)
	var content string
	finishes := 0
	_, err := service.Stream(context.Background(), upstream.Config{Endpoint: mock.URL, BaseHeaders: map[string]string{"authorization": "Bearer test"}, HasAuthorization: true}, map[string]any{"model": "GLM-5.2"}, func(event proxy.StreamEvent) error {
		if value, ok := event.Delta["content"].(string); ok {
			content += value
		}
		if event.FinishReason != "" {
			finishes++
		}
		return nil
	})
	if err != nil || content != "stream" || finishes != 1 {
		t.Fatalf("unexpected raw stream events: content=%q finishes=%d err=%v", content, finishes, err)
	}
}

func TestProxyStreamsFirstUsefulEventBeforeUpstreamFinishes(t *testing.T) {
	cfg := testConfig(t)
	cfg.CaptchaEnabled = false
	bridge := captcha.NewBridge(cfg)
	firstWritten := make(chan struct{})
	releaseUpstream := make(chan struct{})
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"live\"}}\n\n"))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		close(firstWritten)
		<-releaseUpstream
		_, _ = w.Write([]byte("data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer mock.Close()
	service := proxy.New(cfg, bridge)
	received := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		_, err := service.Stream(context.Background(), upstream.Config{Endpoint: mock.URL, BaseHeaders: map[string]string{"authorization": "Bearer test"}, HasAuthorization: true}, map[string]any{"model": "GLM-5.2"}, func(event proxy.StreamEvent) error {
			if value, ok := event.Delta["content"].(string); ok {
				received <- value
			}
			return nil
		})
		done <- err
	}()
	<-firstWritten
	select {
	case value := <-received:
		if value != "live" {
			t.Fatalf("unexpected first stream value: %q", value)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("proxy buffered the first useful stream event until upstream completion")
	}
	close(releaseUpstream)
	if err := <-done; err != nil {
		t.Fatalf("stream ended with error: %v", err)
	}
}

func TestProxyStreamingReasoningContent(t *testing.T) {
	cfg := testConfig(t)
	cfg.CaptchaEnabled = false
	bridge := captcha.NewBridge(cfg)
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"thinking\",\"thinking\":\"vou pensar\"}}\n\n"))
		w.Write([]byte("data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\" mais\"}}\n\n"))
		w.Write([]byte("data: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"text_delta\",\"text\":\"resposta\"}}\n\n"))
		w.Write([]byte("data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer mock.Close()
	service := proxy.New(cfg, bridge)
	var reasoning string
	var content string
	_, err := service.Stream(context.Background(), upstream.Config{Endpoint: mock.URL, BaseHeaders: map[string]string{"authorization": "Bearer test"}, HasAuthorization: true}, map[string]any{"model": "GLM-5.2"}, func(event proxy.StreamEvent) error {
		if value, ok := event.Delta["reasoning_content"].(string); ok {
			reasoning += value
		}
		if value, ok := event.Delta["content"].(string); ok {
			content += value
		}
		return nil
	})
	if err != nil || reasoning != "vou pensar mais" || content != "resposta" {
		t.Fatalf("reasoning stream failed: reasoning=%q content=%q err=%v", reasoning, content, err)
	}
}
