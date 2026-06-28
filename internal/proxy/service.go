package proxy

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"glm5.2proxy/internal/captcha"
	"glm5.2proxy/internal/config"
	"glm5.2proxy/internal/upstream"
)

type ToolCall struct {
	ID       string `json:"id,omitempty"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type Completion struct {
	Text         string
	FinishReason string
	ToolCalls    []ToolCall
	Usage        map[string]any
}

type UpstreamError struct {
	Message   string `json:"message"`
	Type      string `json:"type"`
	Code      any    `json:"code,omitempty"`
	RequestID string `json:"request_id,omitempty"`
	Status    int    `json:"status,omitempty"`
	Attempts  int    `json:"attempts,omitempty"`
}

func (e *UpstreamError) Error() string {
	if e.Status >= 400 {
		return fmt.Sprintf("%s (HTTP %d)", e.Message, e.Status)
	}
	return e.Message
}

type Service struct {
	cfg     config.Config
	captcha *captcha.Bridge
	runtime *runtimeHeaderManager
	client  *http.Client
}

type StreamEvent struct {
	Delta        map[string]any
	FinishReason string
}

func New(cfg config.Config, bridge *captcha.Bridge) *Service {
	return &Service{cfg: cfg, captcha: bridge, runtime: newRuntimeHeaderManager(cfg, bridge), client: newHTTPClient(cfg)}
}

func (s *Service) Request(ctx context.Context, upstreamConfig upstream.Config, body map[string]any) (*http.Response, int, error) {
	upstreamConfig = withStableLogicalRequestIDs(upstreamConfig)
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, 0, err
	}
	captchaRequired := false
	for attempt := 1; attempt <= s.cfg.RetryMaxAttempts; attempt++ {
		prepared, prepareErr := s.prepareForAttempt(ctx, upstreamConfig, captchaRequired)
		if prepareErr != nil {
			return nil, attempt, prepareErr
		}
		response, err := s.requestOnce(ctx, prepared, raw)
		if err != nil {
			if attempt < s.cfg.RetryMaxAttempts {
				s.wait(ctx, attempt, "fetch error")
				continue
			}
			return nil, attempt, err
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			upstreamError := decodeHTTPError(response)
			if IsCaptchaChallenge(upstreamError) && attempt < s.cfg.RetryMaxAttempts {
				captchaRequired = true
				continue
			}
			if retryable(upstreamError) && attempt < s.cfg.RetryMaxAttempts {
				s.wait(ctx, attempt, "retryable HTTP error")
				continue
			}
			upstreamError.Attempts = attempt
			return nil, attempt, upstreamError
		}
		return response, attempt, nil
	}
	return nil, s.cfg.RetryMaxAttempts, &UpstreamError{Message: "ZCode upstream overloaded after retries", Type: "overloaded_error", Code: "1305"}
}

func (s *Service) Collect(ctx context.Context, upstreamConfig upstream.Config, body map[string]any) (Completion, int, error) {
	return s.CollectWithAttemptLimit(ctx, upstreamConfig, body, s.cfg.RetryMaxAttempts)
}

func (s *Service) CollectWithAttemptLimit(ctx context.Context, upstreamConfig upstream.Config, body map[string]any, maxAttempts int) (Completion, int, error) {
	maxAttempts = s.normalizedAttemptLimit(maxAttempts)
	upstreamConfig = withStableLogicalRequestIDs(upstreamConfig)
	captchaRequired := false
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		prepared, prepareErr := s.prepareForAttempt(ctx, upstreamConfig, captchaRequired)
		if prepareErr != nil {
			return Completion{}, attempt, prepareErr
		}
		raw, marshalErr := json.Marshal(body)
		if marshalErr != nil {
			return Completion{}, attempt, marshalErr
		}
		response, err := s.requestOnce(ctx, prepared, raw)
		if err != nil {
			if retryable(err) && attempt < maxAttempts {
				s.waitWithLimit(ctx, attempt, maxAttempts, "retryable upstream error")
				continue
			}
			return Completion{}, attempt, err
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			upstreamErr := decodeHTTPError(response)
			if IsCaptchaChallenge(upstreamErr) && attempt < maxAttempts {
				captchaRequired = true
				continue
			}
			if retryable(upstreamErr) && attempt < maxAttempts {
				s.waitWithLimit(ctx, attempt, maxAttempts, "retryable upstream error")
				continue
			}
			return Completion{}, attempt, upstreamErr
		}
		completion, parseErr := collectSSE(response.Body)
		response.Body.Close()
		if parseErr != nil {
			if IsCaptchaChallenge(parseErr) && attempt < maxAttempts {
				captchaRequired = true
				continue
			}
			if retryable(parseErr) && attempt < maxAttempts && completion.Text == "" && len(completion.ToolCalls) == 0 {
				s.waitWithLimit(ctx, attempt, maxAttempts, "retryable SSE error")
				continue
			}
			return completion, attempt, parseErr
		}
		return completion, attempt, nil
	}
	return Completion{}, maxAttempts, &UpstreamError{Message: "ZCode upstream overloaded after retries", Type: "overloaded_error", Code: "1305"}
}

func (s *Service) Stream(ctx context.Context, upstreamConfig upstream.Config, body map[string]any, emit func(StreamEvent) error) (int, error) {
	return s.StreamWithAttemptLimit(ctx, upstreamConfig, body, s.cfg.RetryMaxAttempts, emit)
}

func (s *Service) StreamWithAttemptLimit(ctx context.Context, upstreamConfig upstream.Config, body map[string]any, maxAttempts int, emit func(StreamEvent) error) (int, error) {
	maxAttempts = s.normalizedAttemptLimit(maxAttempts)
	upstreamConfig = withStableLogicalRequestIDs(upstreamConfig)
	captchaRequired := false
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		prepared, prepareErr := s.prepareForAttempt(ctx, upstreamConfig, captchaRequired)
		if prepareErr != nil {
			return attempt, prepareErr
		}
		raw, err := json.Marshal(body)
		if err != nil {
			return attempt, err
		}
		response, err := s.requestOnce(ctx, prepared, raw)
		if err != nil {
			if retryable(err) && attempt < maxAttempts {
				s.waitWithLimit(ctx, attempt, maxAttempts, "retryable upstream error")
				continue
			}
			return attempt, err
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			upstreamErr := decodeHTTPError(response)
			if IsCaptchaChallenge(upstreamErr) && attempt < maxAttempts {
				captchaRequired = true
				continue
			}
			if retryable(upstreamErr) && attempt < maxAttempts {
				s.waitWithLimit(ctx, attempt, maxAttempts, "retryable upstream error")
				continue
			}
			return attempt, upstreamErr
		}
		emitted := false
		finalEmitted := false
		pendingFinish := ""
		emitAttempt := func(event StreamEvent) error {
			return emit(event)
		}
		streamErr := readSSE(response.Body, func(event string, data []byte) error {
			if string(data) == "[DONE]" || len(data) == 0 {
				return nil
			}
			var message map[string]any
			if json.Unmarshal(data, &message) != nil {
				return nil
			}
			messageType := text(message["type"])
			if messageType == "error" || event == "error" {
				return normalizeError(message, 502)
			}
			if messageType == "content_block_start" {
				block := object(message["content_block"])
				switch text(block["type"]) {
				case "tool_use":
					emitted = true
					return emitAttempt(StreamEvent{Delta: map[string]any{"tool_calls": []any{map[string]any{"index": integer(message["index"]), "id": text(block["id"]), "type": "function", "function": map[string]any{"name": text(block["name"]), "arguments": ""}}}}})
				case "thinking", "reasoning":
					if value := first(text(block["thinking"]), text(block["text"]), text(block["content"])); value != "" {
						emitted = true
						return emitAttempt(StreamEvent{Delta: map[string]any{"reasoning_content": value}})
					}
				}
			}
			if messageType == "content_block_delta" {
				delta := object(message["delta"])
				switch text(delta["type"]) {
				case "text_delta":
					if value := text(delta["text"]); value != "" {
						emitted = true
						return emitAttempt(StreamEvent{Delta: map[string]any{"content": value}})
					}
				case "input_json_delta":
					if value := text(delta["partial_json"]); value != "" {
						emitted = true
						return emitAttempt(StreamEvent{Delta: map[string]any{"tool_calls": []any{map[string]any{"index": integer(message["index"]), "function": map[string]any{"arguments": value}}}}})
					}
				case "thinking_delta", "reasoning_delta":
					if value := first(text(delta["thinking"]), text(delta["text"]), text(delta["content"])); value != "" {
						emitted = true
						return emitAttempt(StreamEvent{Delta: map[string]any{"reasoning_content": value}})
					}
				}
			}
			if messageType == "thinking_delta" || messageType == "reasoning_delta" {
				if value := first(text(message["thinking"]), text(message["text"]), text(message["content"])); value != "" {
					emitted = true
					return emitAttempt(StreamEvent{Delta: map[string]any{"reasoning_content": value}})
				}
			}
			if messageType == "message_delta" {
				if reason := text(object(message["delta"])["stop_reason"]); reason != "" {
					pendingFinish = mapFinishReason(reason)
					if emitted {
						finalEmitted = true
						return emitAttempt(StreamEvent{Delta: map[string]any{}, FinishReason: pendingFinish})
					}
				}
			}
			if messageType == "message_stop" {
				if pendingFinish == "" {
					pendingFinish = "stop"
				}
				if emitted && !finalEmitted {
					finalEmitted = true
					return emitAttempt(StreamEvent{Delta: map[string]any{}, FinishReason: pendingFinish})
				}
			}
			return nil
		})
		response.Body.Close()
		if streamErr == nil && !emitted {
			streamErr = staleConnectionError()
		}
		if streamErr == nil && emitted && !finalEmitted {
			if pendingFinish != "" {
				finalEmitted = true
				streamErr = emitAttempt(StreamEvent{Delta: map[string]any{}, FinishReason: pendingFinish})
			} else {
				streamErr = staleConnectionError()
			}
		}
		if streamErr != nil && !emitted && IsCaptchaChallenge(streamErr) && attempt < maxAttempts {
			captchaRequired = true
			continue
		}
		if streamErr != nil && !emitted && retryable(streamErr) && attempt < maxAttempts {
			s.waitWithLimit(ctx, attempt, maxAttempts, "retryable SSE error")
			continue
		}
		return attempt, streamErr
	}
	return maxAttempts, &UpstreamError{Message: "ZCode upstream overloaded after retries", Type: "overloaded_error", Code: "1305"}
}

func (s *Service) prepareForAttempt(ctx context.Context, upstreamConfig upstream.Config, captchaRequired bool) (upstream.Config, error) {
	if captchaRequired {
		return s.runtime.PrepareWithCaptcha(ctx, upstreamConfig)
	}
	return s.runtime.Prepare(ctx, upstreamConfig)
}

func (s *Service) normalizedAttemptLimit(maxAttempts int) int {
	if maxAttempts <= 0 || maxAttempts > s.cfg.RetryMaxAttempts {
		return s.cfg.RetryMaxAttempts
	}
	return maxAttempts
}

func (s *Service) RequestSingleAttempt(ctx context.Context, upstreamConfig upstream.Config, body map[string]any) (*http.Response, int, error) {
	upstreamConfig = withStableLogicalRequestIDs(upstreamConfig)
	var err error
	upstreamConfig, err = s.runtime.Prepare(ctx, upstreamConfig)
	if err != nil {
		return nil, 0, err
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, 1, err
	}
	response, err := s.requestOnce(ctx, upstreamConfig, raw)
	if err != nil {
		return nil, 1, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, 1, decodeHTTPError(response)
	}
	return response, 1, nil
}

func (s *Service) requestOnce(parent context.Context, upstreamConfig upstream.Config, raw []byte) (*http.Response, error) {
	ctx, cancel := context.WithTimeout(parent, s.cfg.UpstreamTimeout)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, upstreamConfig.Endpoint, bytes.NewReader(raw))
	if err != nil {
		cancel()
		return nil, err
	}
	setHeadersInOrder(request, upstreamConfig.BaseHeaders)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "text/event-stream")
	response, err := s.client.Do(request)
	if err != nil {
		cancel()
		return nil, err
	}
	response.Body = &cancelReadCloser{ReadCloser: response.Body, cancel: cancel}
	return response, nil
}

type cancelReadCloser struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c *cancelReadCloser) Close() error {
	err := c.ReadCloser.Close()
	c.cancel()
	return err
}

func collectSSE(reader io.Reader) (Completion, error) {
	result := Completion{FinishReason: "stop", ToolCalls: []ToolCall{}}
	toolCalls := map[int]*ToolCall{}
	err := readSSE(reader, func(event string, data []byte) error {
		if string(data) == "[DONE]" || len(data) == 0 {
			return nil
		}
		var message map[string]any
		if json.Unmarshal(data, &message) != nil {
			return nil
		}
		messageType := text(message["type"])
		if messageType == "error" || event == "error" {
			return normalizeError(message, 502)
		}
		if messageType == "message_start" {
			result.Usage = mergeUsage(result.Usage, object(object(message["message"])["usage"]))
		}
		if messageType == "content_block_start" {
			block := object(message["content_block"])
			if text(block["type"]) == "tool_use" {
				index := integer(message["index"])
				call := &ToolCall{ID: text(block["id"]), Type: "function"}
				call.Function.Name = text(block["name"])
				toolCalls[index] = call
			}
		}
		if messageType == "content_block_delta" {
			delta := object(message["delta"])
			switch text(delta["type"]) {
			case "text_delta":
				result.Text += text(delta["text"])
			case "input_json_delta":
				if call := toolCalls[integer(message["index"])]; call != nil {
					call.Function.Arguments += text(delta["partial_json"])
				}
			}
		}
		if messageType == "message_delta" {
			result.FinishReason = mapFinishReason(text(object(message["delta"])["stop_reason"]))
			result.Usage = mergeUsage(result.Usage, object(message["usage"]))
		}
		return nil
	})
	for index := 0; index < len(toolCalls); index++ {
		if call := toolCalls[index]; call != nil {
			result.ToolCalls = append(result.ToolCalls, *call)
		}
	}
	if err == nil && result.Text == "" && len(result.ToolCalls) == 0 {
		err = staleConnectionError()
	}
	return result, err
}

func readSSE(reader io.Reader, callback func(string, []byte) error) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	event := "message"
	var data strings.Builder
	flush := func() error {
		if data.Len() == 0 {
			event = "message"
			return nil
		}
		value := strings.TrimSuffix(data.String(), "\n")
		err := callback(event, []byte(value))
		event = "message"
		data.Reset()
		return err
	}
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if line == "" {
			if err := flush(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, "event:") {
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "data:") {
			data.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			data.WriteByte('\n')
		}
	}
	if err := flush(); err != nil {
		return err
	}
	return scanner.Err()
}

func decodeHTTPError(response *http.Response) *UpstreamError {
	defer response.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if isZCodeWAFBlock(response, raw) {
		return &UpstreamError{
			Message: "ZCode WAF/ESA blocked this network before the request reached the model",
			Type:    "zcode_waf_block",
			Code:    "esa_405",
			Status:  response.StatusCode,
		}
	}
	var payload map[string]any
	_ = json.Unmarshal(raw, &payload)
	return normalizeError(payload, response.StatusCode)
}

func isZCodeWAFBlock(response *http.Response, raw []byte) bool {
	if response == nil || response.StatusCode != http.StatusMethodNotAllowed {
		return false
	}
	server := strings.ToLower(response.Header.Get("Server"))
	contentType := strings.ToLower(response.Header.Get("Content-Type"))
	body := bytes.ToLower(bytes.TrimSpace(raw))
	if strings.Contains(server, "esa") ||
		bytes.Contains(body, []byte("errors.aliyun.com")) ||
		bytes.Contains(body, []byte("the requested url may cause potential security risks")) ||
		bytes.Contains(body, []byte("request has been blocked")) ||
		bytes.Contains(body, []byte("<!doctype")) ||
		bytes.Contains(body, []byte("<html")) {
		return true
	}
	if len(body) == 0 {
		return true
	}
	if !strings.Contains(contentType, "application/json") {
		return true
	}
	var payload map[string]any
	if json.Unmarshal(raw, &payload) != nil {
		return false
	}
	value := payload
	if nested := object(payload["error"]); nested != nil {
		value = nested
	}
	message := strings.ToLower(first(text(value["message"]), text(payload["msg"]), text(payload["message"])))
	code := strings.TrimSpace(fmt.Sprint(firstAny(value["code"], payload["code"])))
	kind := strings.ToLower(first(text(value["type"]), text(payload["type"])))
	if code == "" && (message == "" || strings.Contains(message, "unknown upstream error") || strings.Contains(message, "method not allowed")) {
		return true
	}
	return kind == "" && code == "" && message == ""
}

func normalizeError(payload map[string]any, status int) *UpstreamError {
	value := payload
	if nested := object(payload["error"]); nested != nil {
		value = nested
	}
	message := first(text(value["message"]), text(payload["msg"]), text(payload["message"]), "Unknown upstream error")
	return &UpstreamError{Message: message, Type: first(text(value["type"]), "zcode_upstream_error"), Code: firstAny(value["code"], payload["code"]), RequestID: first(text(payload["request_id"]), text(value["request_id"])), Status: status}
}

func IsOverloaded(err error) bool {
	if IsAdmissionConcurrency(err) {
		return true
	}
	var upstreamError *UpstreamError
	if !errors.As(err, &upstreamError) {
		return false
	}
	return fmt.Sprint(upstreamError.Code) == "1305" || upstreamError.Type == "overloaded_error" || strings.Contains(upstreamError.Message, "[1305]")
}

func overloaded(err error) bool {
	return IsOverloaded(err)
}

func retryable(err error) bool {
	if err == nil || IsQuotaExhausted(err) {
		return false
	}
	if overloaded(err) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var upstreamError *UpstreamError
	if errors.As(err, &upstreamError) {
		if upstreamError.Status == http.StatusRequestTimeout || upstreamError.Status == http.StatusTooEarly || upstreamError.Status >= 500 {
			return true
		}
	}
	value := strings.ToLower(err.Error())
	for _, marker := range []string{"stale connection", "stream idle", "timeout", "timed out", "connection reset", "unexpected eof", "temporarily unavailable"} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}

func IsAuthFailed(err error) bool {
	if err == nil || IsQuotaExhausted(err) || IsAdmissionConcurrency(err) {
		return false
	}
	var upstreamError *UpstreamError
	if !errors.As(err, &upstreamError) {
		return false
	}
	if upstreamError.Status != http.StatusUnauthorized && upstreamError.Status != http.StatusForbidden {
		return false
	}
	value := strings.ToLower(strings.Join([]string{
		upstreamError.Message,
		upstreamError.Type,
		fmt.Sprint(upstreamError.Code),
	}, " "))
	if strings.Contains(value, "captcha") {
		return false
	}
	for _, marker := range []string{"auth", "token", "jwt", "login", "session", "unauthorized", "invalid api key", "invalid key", "expired"} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return upstreamError.Status == http.StatusUnauthorized
}

func IsCaptchaChallenge(err error) bool {
	if err == nil {
		return false
	}
	var upstreamError *UpstreamError
	if !errors.As(err, &upstreamError) {
		return false
	}
	value := strings.ToLower(strings.Join([]string{
		upstreamError.Message,
		upstreamError.Type,
		fmt.Sprint(upstreamError.Code),
	}, " "))
	for _, marker := range []string{"captcha", "aliyun", "verify", "verification", "challenge", "risk control", "security check", "human"} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}

func IsWAFBlocked(err error) bool {
	if err == nil {
		return false
	}
	var upstreamError *UpstreamError
	if !errors.As(err, &upstreamError) {
		return false
	}
	if upstreamError.Type == "zcode_waf_block" || fmt.Sprint(upstreamError.Code) == "esa_405" {
		return true
	}
	text := strings.ToLower(strings.Join([]string{
		upstreamError.Message,
		upstreamError.Type,
		fmt.Sprint(upstreamError.Code),
	}, " "))
	return upstreamError.Status == http.StatusMethodNotAllowed &&
		(strings.Contains(text, "waf") || strings.Contains(text, "esa") || strings.Contains(text, "blocked"))
}

func staleConnectionError() error {
	return &UpstreamError{Message: "upstream stream ended before producing text or tool calls", Type: "stale_connection", Status: http.StatusBadGateway}
}

func IsAdmissionConcurrency(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	var upstreamError *UpstreamError
	if errors.As(err, &upstreamError) {
		text += " " + strings.ToLower(strings.Join([]string{
			upstreamError.Message,
			upstreamError.Type,
			fmt.Sprint(upstreamError.Code),
		}, " "))
	}
	return strings.Contains(text, "admission concurrency") ||
		strings.Contains(text, "model admission concurrency") ||
		strings.Contains(text, "concurrency limit exceeded") ||
		strings.Contains(text, "model concurrency limit exceeded")
}

func IsParameterError(err error) bool {
	if err == nil {
		return false
	}
	var upstreamError *UpstreamError
	if !errors.As(err, &upstreamError) {
		return false
	}
	text := strings.ToLower(strings.Join([]string{
		upstreamError.Message,
		upstreamError.Type,
		fmt.Sprint(upstreamError.Code),
	}, " "))
	return upstreamError.Status == http.StatusBadRequest &&
		(strings.Contains(text, "parameter error") || strings.Contains(text, "3001"))
}

func IsUnknownUpstreamError(err error) bool {
	if err == nil || IsQuotaExhausted(err) || IsAdmissionConcurrency(err) || IsCaptchaChallenge(err) || IsParameterError(err) || IsOverloaded(err) || IsWAFBlocked(err) {
		return false
	}
	var upstreamError *UpstreamError
	if !errors.As(err, &upstreamError) {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(strings.Join([]string{
		upstreamError.Message,
		upstreamError.Type,
		fmt.Sprint(upstreamError.Code),
	}, " ")))
	return strings.Contains(text, "unknown upstream error")
}

func IsQuotaExhausted(err error) bool {
	var upstreamError *UpstreamError
	if !errors.As(err, &upstreamError) {
		return false
	}
	text := strings.ToLower(strings.Join([]string{
		upstreamError.Message,
		upstreamError.Type,
		fmt.Sprint(upstreamError.Code),
	}, " "))
	if upstreamError.Status != http.StatusTooManyRequests && upstreamError.Status != http.StatusForbidden && upstreamError.Status != http.StatusPaymentRequired {
		return false
	}
	if IsAdmissionConcurrency(err) {
		return false
	}
	if upstreamError.Status == http.StatusTooManyRequests && strings.Contains(text, "unknown upstream error") {
		return true
	}
	for _, marker := range []string{"quota", "exhaust", "insufficient", "balance", "credit", "available", "usage", "tokens"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func IsStaleConnection(err error) bool {
	var upstreamError *UpstreamError
	if !errors.As(err, &upstreamError) {
		return false
	}
	return upstreamError.Type == "stale_connection"
}

func (s *Service) wait(ctx context.Context, attempt int, reason string) {
	s.waitWithLimit(ctx, attempt, s.cfg.RetryMaxAttempts, reason)
}

func (s *Service) waitWithLimit(ctx context.Context, attempt int, maxAttempts int, reason string) {
	delay := retryDelay(s.cfg.RetryBaseDelay, s.cfg.RetryMaxDelay, attempt)
	log.Printf("upstream retry %d/%d after %s; waiting %s", attempt+1, maxAttempts, reason, delay)
	timer := time.NewTimer(delay)
	select {
	case <-timer.C:
	case <-ctx.Done():
		timer.Stop()
	}
}

func retryDelay(base, maximum time.Duration, attempt int) time.Duration {
	delay := base
	for step := 0; step < attempt; step++ {
		if delay >= maximum/2 {
			delay = maximum
			break
		}
		delay *= 2
	}
	if delay > maximum {
		delay = maximum
	}
	random := make([]byte, 2)
	if _, err := rand.Read(random); err != nil {
		return delay
	}
	factor := 0.5 + float64(int(random[0])<<8|int(random[1]))/131070
	return time.Duration(float64(delay) * factor)
}

func randomID() string {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	raw := hex.EncodeToString(value)
	return raw[0:8] + "-" + raw[8:12] + "-" + raw[12:16] + "-" + raw[16:20] + "-" + raw[20:32]
}

func withStableLogicalRequestIDs(value upstream.Config) upstream.Config {
	headers := make(map[string]string, len(value.BaseHeaders)+3)
	for key, headerValue := range value.BaseHeaders {
		headers[key] = headerValue
	}
	for _, name := range []string{"x-request-id", "x-zcode-trace-id", "x-query-id"} {
		if strings.TrimSpace(headers[name]) == "" {
			headers[name] = randomID()
		}
	}
	value.BaseHeaders = headers
	return value
}

func mapFinishReason(reason string) string {
	switch reason {
	case "", "end_turn", "stop_sequence", "stop":
		return "stop"
	case "max_tokens":
		return "length"
	case "tool_use", "tool_calls":
		return "tool_calls"
	default:
		return reason
	}
}

func object(value any) map[string]any {
	result, _ := value.(map[string]any)
	return result
}

func text(value any) string {
	result, _ := value.(string)
	return result
}

func integer(value any) int {
	switch number := value.(type) {
	case int:
		return number
	case int32:
		return int(number)
	case int64:
		return int(number)
	case float32:
		return int(number)
	case float64:
		return int(number)
	case json.Number:
		parsed, _ := number.Int64()
		return int(parsed)
	default:
		return 0
	}
}

func first(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func firstAny(values ...any) any {
	for _, value := range values {
		if value != nil && fmt.Sprint(value) != "" {
			return value
		}
	}
	return nil
}

func mergeUsage(current map[string]any, value map[string]any) map[string]any {
	if current == nil {
		current = map[string]any{"prompt_tokens": 0, "completion_tokens": 0, "total_tokens": 0}
	}
	prompt := integer(current["prompt_tokens"])
	if input := integer(value["input_tokens"]); input > 0 {
		prompt = input + integer(value["cache_read_input_tokens"]) + integer(value["cache_creation_input_tokens"])
		current["prompt_tokens"] = prompt
		cached := integer(value["cache_read_input_tokens"])
		if cached > 0 {
			current["prompt_tokens_details"] = map[string]any{"cached_tokens": cached}
		}
	}
	completion := integer(current["completion_tokens"])
	if output := integer(value["output_tokens"]); output > 0 {
		completion = output
		current["completion_tokens"] = completion
	}
	current["total_tokens"] = prompt + completion
	return current
}
