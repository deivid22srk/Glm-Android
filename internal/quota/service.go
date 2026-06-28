package quota

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"glm5.2proxy/internal/config"
	"glm5.2proxy/internal/models"
	"glm5.2proxy/internal/upstream"
)

type Balance struct {
	ID           string     `json:"id"`
	Model        string     `json:"model"`
	Meter        string     `json:"meter,omitempty"`
	UnitType     string     `json:"unitType,omitempty"`
	Total        *int64     `json:"total"`
	Used         *int64     `json:"used"`
	Reserved     *int64     `json:"reserved"`
	Remaining    *int64     `json:"remaining"`
	Available    *int64     `json:"available"`
	UsagePercent *float64   `json:"usagePercent"`
	PeriodStart  *time.Time `json:"periodStart"`
	PeriodEnd    *time.Time `json:"periodEnd"`
	ExpiresAt    *time.Time `json:"expiresAt"`
}

type Entitlement struct {
	ID       string `json:"id"`
	Model    string `json:"model"`
	Meter    string `json:"meter,omitempty"`
	UnitType string `json:"unitType,omitempty"`
	Granted  *int64 `json:"granted"`
	Period   string `json:"period,omitempty"`
}

type Plan struct {
	ID           string        `json:"id"`
	Name         string        `json:"name"`
	Status       string        `json:"status"`
	StartsAt     *time.Time    `json:"startsAt"`
	EndsAt       *time.Time    `json:"endsAt"`
	Entitlements []Entitlement `json:"entitlements"`
}

type Snapshot struct {
	Object      string     `json:"object"`
	GeneratedAt time.Time  `json:"generatedAt"`
	ServerTime  *time.Time `json:"serverTime"`
	Plans       []Plan     `json:"plans"`
	Balances    []Balance  `json:"balances"`
}

type Service struct {
	cfg              config.Config
	client           *http.Client
	requestGate      chan struct{}
	lastRequestAt    time.Time
	snapshotCacheMu  sync.Mutex
	snapshotCache    map[string]snapshotCacheEntry
	snapshotInFlight map[string]chan struct{}
	balanceCache     map[string]balanceCacheEntry
	balanceInFlight  map[string]chan struct{}
}

type snapshotCacheEntry struct {
	snapshot  Snapshot
	err       error
	updatedAt time.Time
}

type balanceCacheEntry struct {
	body      map[string]any
	err       error
	updatedAt time.Time
}

func New(cfg config.Config) *Service {
	return &Service{
		cfg:              cfg,
		client:           &http.Client{Timeout: 15 * time.Second},
		requestGate:      make(chan struct{}, 1),
		snapshotCache:    map[string]snapshotCacheEntry{},
		snapshotInFlight: map[string]chan struct{}{},
		balanceCache:     map[string]balanceCacheEntry{},
		balanceInFlight:  map[string]chan struct{}{},
	}
}

func (s *Service) Snapshot(ctx context.Context, upstreamConfig upstream.Config) (Snapshot, error) {
	if usesUsageQuota(upstreamConfig) {
		return s.usageQuotaSnapshot(ctx, upstreamConfig)
	}
	current, err := s.fetch(ctx, upstreamConfig, "current")
	if err != nil {
		return Snapshot{}, err
	}
	balance, err := s.fetch(ctx, upstreamConfig, "balance")
	if err != nil {
		return Snapshot{}, err
	}
	return normalizeSnapshot(current, balance), nil
}

func (s *Service) BalanceSnapshot(ctx context.Context, upstreamConfig upstream.Config) (Snapshot, error) {
	if usesUsageQuota(upstreamConfig) {
		return s.usageQuotaSnapshot(ctx, upstreamConfig)
	}
	balance, err := s.freshBalance(ctx, upstreamConfig)
	if err != nil {
		return Snapshot{}, err
	}
	return normalizeBalanceSnapshot(balance), nil
}

func (s *Service) SnapshotCached(ctx context.Context, upstreamConfig upstream.Config, maxAge time.Duration) (Snapshot, error) {
	key := s.snapshotCacheKey(upstreamConfig)
	for {
		s.snapshotCacheMu.Lock()
		if entry, ok := s.snapshotCache[key]; ok && time.Since(entry.updatedAt) <= maxAge {
			s.snapshotCacheMu.Unlock()
			return entry.snapshot, entry.err
		}
		if inFlight, ok := s.snapshotInFlight[key]; ok {
			s.snapshotCacheMu.Unlock()
			select {
			case <-ctx.Done():
				return Snapshot{}, ctx.Err()
			case <-inFlight:
				continue
			}
		}
		inFlight := make(chan struct{})
		s.snapshotInFlight[key] = inFlight
		s.snapshotCacheMu.Unlock()

		snapshot, err := s.Snapshot(ctx, upstreamConfig)

		s.snapshotCacheMu.Lock()
		s.snapshotCache[key] = snapshotCacheEntry{snapshot: snapshot, err: err, updatedAt: time.Now()}
		close(inFlight)
		delete(s.snapshotInFlight, key)
		s.snapshotCacheMu.Unlock()
		return snapshot, err
	}
}

func (s *Service) ModelBalance(ctx context.Context, upstreamConfig upstream.Config, model models.Model) (*Balance, error) {
	if !upstreamConfig.HasAuthorization {
		return nil, nil
	}
	if usesUsageQuota(upstreamConfig) {
		snapshot, err := s.usageQuotaSnapshot(ctx, upstreamConfig)
		if err != nil {
			return nil, err
		}
		for _, balance := range snapshot.Balances {
			if strings.EqualFold(balance.Model, model.UpstreamID) {
				copy := balance
				return &copy, nil
			}
		}
		return nil, nil
	}
	body, err := s.freshBalance(ctx, upstreamConfig)
	if err != nil {
		return nil, err
	}
	return modelBalanceFromBody(body, model), nil
}

func (s *Service) ModelBalanceCached(ctx context.Context, upstreamConfig upstream.Config, model models.Model, maxAge time.Duration) (*Balance, error) {
	if !upstreamConfig.HasAuthorization {
		return nil, nil
	}
	if usesUsageQuota(upstreamConfig) {
		return s.ModelBalance(ctx, upstreamConfig, model)
	}
	if maxAge <= 0 {
		return s.ModelBalance(ctx, upstreamConfig, model)
	}
	body, err := s.balanceBodyCached(ctx, upstreamConfig, maxAge)
	if err != nil {
		return nil, err
	}
	return modelBalanceFromBody(body, model), nil
}

func modelBalanceFromBody(body map[string]any, model models.Model) *Balance {
	data := object(body["data"])
	for _, item := range array(data["balances"]) {
		value := object(item)
		if strings.EqualFold(text(value["show_name"]), model.UpstreamID) {
			balance := normalizeBalance(value)
			return &balance
		}
	}
	return nil
}

func (s *Service) balanceBodyCached(ctx context.Context, upstreamConfig upstream.Config, maxAge time.Duration) (map[string]any, error) {
	key := s.snapshotCacheKey(upstreamConfig)
	for {
		s.snapshotCacheMu.Lock()
		if entry, ok := s.balanceCache[key]; ok && time.Since(entry.updatedAt) <= maxAge {
			s.snapshotCacheMu.Unlock()
			return entry.body, entry.err
		}
		if inFlight, ok := s.balanceInFlight[key]; ok {
			s.snapshotCacheMu.Unlock()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-inFlight:
				continue
			}
		}
		inFlight := make(chan struct{})
		s.balanceInFlight[key] = inFlight
		s.snapshotCacheMu.Unlock()

		body, err := s.freshBalance(ctx, upstreamConfig)

		s.snapshotCacheMu.Lock()
		s.balanceCache[key] = balanceCacheEntry{body: body, err: err, updatedAt: time.Now()}
		close(inFlight)
		delete(s.balanceInFlight, key)
		s.snapshotCacheMu.Unlock()
		return body, err
	}
}

func (s *Service) freshBalance(ctx context.Context, upstreamConfig upstream.Config) (map[string]any, error) {
	if _, err := s.fetch(ctx, upstreamConfig, "current"); err != nil {
		return nil, err
	}
	return s.fetch(ctx, upstreamConfig, "balance")
}

func (s *Service) snapshotCacheKey(upstreamConfig upstream.Config) string {
	hash := sha256.Sum256([]byte(strings.Join([]string{
		s.cfg.BillingBaseURL,
		upstreamConfig.QuotaEndpoint,
		upstreamConfig.QuotaAuthorization,
		upstreamConfig.BaseHeaders["authorization"],
		upstreamConfig.BaseHeaders["x-zcode-app-version"],
	}, "\x00")))
	return hex.EncodeToString(hash[:])
}

func usesUsageQuota(upstreamConfig upstream.Config) bool {
	return strings.TrimSpace(upstreamConfig.QuotaEndpoint) != "" && strings.TrimSpace(upstreamConfig.QuotaAuthorization) != ""
}

func (s *Service) usageQuotaSnapshot(ctx context.Context, upstreamConfig upstream.Config) (Snapshot, error) {
	body, err := s.fetchUsageQuota(ctx, upstreamConfig)
	if err != nil {
		return Snapshot{}, err
	}
	return normalizeUsageQuotaSnapshot(body), nil
}

func (s *Service) fetchUsageQuota(ctx context.Context, upstreamConfig upstream.Config) (map[string]any, error) {
	release, err := s.beginRequest(ctx)
	if err != nil {
		return nil, err
	}
	defer release()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, upstreamConfig.QuotaEndpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", upstreamConfig.QuotaAuthorization)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", first(upstreamConfig.BaseHeaders["user-agent"], "ZCode/"+s.cfg.AppVersion))
	response, err := s.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, billingStatusError("usage-quota", response.StatusCode, raw)
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil, fmt.Errorf("billing usage-quota failed: HTTP %d empty response body", response.StatusCode)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	if !successfulCode(body["code"]) {
		return nil, fmt.Errorf("billing usage-quota failed: HTTP %d %s", response.StatusCode, first(text(body["msg"]), text(body["message"])))
	}
	if success, ok := body["success"].(bool); ok && !success {
		return nil, fmt.Errorf("billing usage-quota failed: HTTP %d %s", response.StatusCode, first(text(body["msg"]), text(body["message"])))
	}
	return body, nil
}

func (s *Service) fetch(ctx context.Context, upstreamConfig upstream.Config, kind string) (map[string]any, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		body, err := s.fetchOnce(ctx, upstreamConfig, kind)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if !retryable(err) || attempt == 2 {
			break
		}
		timer := time.NewTimer(time.Duration(attempt+1) * 250 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	return nil, lastErr
}

func (s *Service) fetchOnce(ctx context.Context, upstreamConfig upstream.Config, kind string) (map[string]any, error) {
	release, err := s.beginRequest(ctx)
	if err != nil {
		return nil, err
	}
	defer release()

	appVersion := upstreamConfig.BaseHeaders["x-zcode-app-version"]
	if appVersion == "" {
		appVersion = s.cfg.AppVersion
	}
	endpoint := strings.TrimRight(s.cfg.BillingBaseURL, "/") + "/" + kind + "?app_version=" + url.QueryEscape(appVersion)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", upstreamConfig.BaseHeaders["authorization"])
	request.Header.Set("User-Agent", first(upstreamConfig.BaseHeaders["user-agent"], "ZCode/"+appVersion))
	request.Header.Set("X-ZCode-App-Version", appVersion)
	response, err := s.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, billingStatusError(kind, response.StatusCode, raw)
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil, fmt.Errorf("billing %s failed: HTTP %d empty response body", kind, response.StatusCode)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	if integer(body["code"]) != 0 {
		return nil, fmt.Errorf("billing %s failed: HTTP %d %s", kind, response.StatusCode, first(text(body["msg"]), text(body["message"])))
	}
	return body, nil
}

func (s *Service) beginRequest(ctx context.Context) (func(), error) {
	select {
	case s.requestGate <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	released := false
	release := func() {
		if released {
			return
		}
		released = true
		<-s.requestGate
	}
	wait := s.lastRequestAt.Add(150 * time.Millisecond).Sub(time.Now())
	if wait > 0 {
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			release()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	s.lastRequestAt = time.Now()
	return release, nil
}

func billingStatusError(kind string, statusCode int, raw []byte) error {
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	message := first(text(body["msg"]), text(body["message"]), http.StatusText(statusCode))
	if strings.TrimSpace(string(raw)) == "" {
		message = strings.TrimSpace(message + " (empty response body)")
	}
	return fmt.Errorf("billing %s failed: HTTP %d %s", kind, statusCode, message)
}

func retryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	value := strings.ToLower(err.Error())
	return strings.Contains(value, "http 429") || strings.Contains(value, "connection reset") || strings.Contains(value, "server closed idle connection") || strings.Contains(value, "timeout")
}

func normalizeSnapshot(current, balances map[string]any) Snapshot {
	out := Snapshot{Object: "zcode.quota", GeneratedAt: time.Now().UTC(), Plans: []Plan{}, Balances: []Balance{}}
	currentData := object(current["data"])
	for _, item := range array(currentData["plans"]) {
		value := object(item)
		plan := Plan{ID: text(value["plan_id"]), Name: text(value["name"]), Status: text(value["status"]), StartsAt: unixTime(value["starts_at"]), EndsAt: unixTime(value["ends_at"]), Entitlements: []Entitlement{}}
		for _, raw := range array(value["entitlements"]) {
			entitlement := object(raw)
			plan.Entitlements = append(plan.Entitlements, Entitlement{ID: text(entitlement["entitlement_id"]), Model: text(entitlement["show_name"]), Meter: text(entitlement["meter"]), UnitType: text(entitlement["unit_type"]), Granted: intPointer(entitlement["grant_units"]), Period: text(entitlement["period"])})
		}
		out.Plans = append(out.Plans, plan)
	}
	balanceData := object(balances["data"])
	out.ServerTime = unixTime(balanceData["server_time"])
	for _, item := range array(balanceData["balances"]) {
		out.Balances = append(out.Balances, normalizeBalance(object(item)))
	}
	return out
}

func normalizeBalanceSnapshot(balances map[string]any) Snapshot {
	out := Snapshot{Object: "zcode.quota", GeneratedAt: time.Now().UTC(), Plans: []Plan{}, Balances: []Balance{}}
	balanceData := object(balances["data"])
	out.ServerTime = unixTime(balanceData["server_time"])
	for _, item := range array(balanceData["balances"]) {
		out.Balances = append(out.Balances, normalizeBalance(object(item)))
	}
	return out
}

func normalizeUsageQuotaSnapshot(body map[string]any) Snapshot {
	out := Snapshot{Object: "zcode.quota", GeneratedAt: time.Now().UTC(), Plans: []Plan{}, Balances: []Balance{}}
	data := object(body["data"])
	level := text(data["level"])
	plan := Plan{ID: first(level, "coding-plan"), Name: first(level, "Coding Plan"), Status: "active", Entitlements: []Entitlement{}}
	for _, item := range array(data["limits"]) {
		limit := object(item)
		balances := usageLimitBalances(limit)
		for _, balance := range balances {
			out.Balances = append(out.Balances, balance)
			plan.Entitlements = append(plan.Entitlements, Entitlement{
				ID: balance.ID, Model: balance.Model, Meter: balance.Meter, UnitType: balance.UnitType, Granted: balance.Total, Period: "current",
			})
		}
	}
	if len(plan.Entitlements) > 0 || level != "" {
		out.Plans = append(out.Plans, plan)
	}
	return out
}

func usageLimitBalances(limit map[string]any) []Balance {
	limitType := first(text(limit["type"]), "usage")
	total := intPointer(firstNumber(limit["number"], limit["unit"]))
	used := intPointer(firstNumber(limit["usage"], limit["currentValue"]))
	remaining := intPointer(limit["remaining"])
	var percent *float64
	if total != nil && remaining != nil && *total > 0 {
		value := float64(*remaining) / float64(*total)
		percent = &value
	}
	details := array(limit["usageDetails"])
	if len(details) == 0 {
		return []Balance{{
			ID: limitType, Model: limitType, Meter: limitType, UnitType: "tokens",
			Total: total, Used: used, Remaining: remaining, Available: remaining, UsagePercent: percent,
		}}
	}
	out := make([]Balance, 0, len(details))
	for _, item := range details {
		detail := object(item)
		model := first(text(detail["modelCode"]), text(detail["displayName"]), limitType)
		detailUsed := used
		if value := intPointer(detail["usage"]); value != nil {
			detailUsed = value
		}
		out = append(out, Balance{
			ID: first(model, limitType), Model: model, Meter: limitType, UnitType: "tokens",
			Total: total, Used: detailUsed, Remaining: remaining, Available: remaining, UsagePercent: percent,
		})
	}
	return out
}

func normalizeBalance(value map[string]any) Balance {
	total := intPointer(value["total_units"])
	used := intPointer(value["used_units"])
	var percent *float64
	if total != nil && used != nil && *total > 0 {
		calculated := float64(*used) * 100 / float64(*total)
		percent = &calculated
	}
	return Balance{ID: text(value["entitlement_id"]), Model: text(value["show_name"]), Meter: text(value["meter"]), UnitType: text(value["unit_type"]), Total: total, Used: used, Reserved: intPointer(value["reserved_units"]), Remaining: intPointer(value["remaining_units"]), Available: intPointer(value["available_units"]), UsagePercent: percent, PeriodStart: unixTime(value["period_start"]), PeriodEnd: unixTime(value["period_end"]), ExpiresAt: unixTime(value["expires_at"])}
}

func unixTime(value any) *time.Time {
	seconds := integer(value)
	if seconds <= 0 {
		return nil
	}
	result := time.Unix(seconds, 0).UTC()
	return &result
}

func intPointer(value any) *int64 {
	if value == nil {
		return nil
	}
	result := integer(value)
	return &result
}

func integer(value any) int64 {
	switch typed := value.(type) {
	case float64:
		return int64(typed)
	case int:
		return int64(typed)
	case int64:
		return typed
	case json.Number:
		result, _ := typed.Int64()
		return result
	case string:
		result, _ := strconv.ParseInt(typed, 10, 64)
		return result
	default:
		return 0
	}
}

func object(value any) map[string]any {
	result, _ := value.(map[string]any)
	return result
}

func array(value any) []any {
	result, _ := value.([]any)
	return result
}

func text(value any) string {
	result, _ := value.(string)
	return result
}

func firstNumber(values ...any) any {
	for _, value := range values {
		if integer(value) > 0 {
			return value
		}
	}
	return nil
}

func successfulCode(value any) bool {
	switch typed := value.(type) {
	case nil:
		return true
	case float64:
		return typed == 0 || typed == 200
	case int:
		return typed == 0 || typed == 200
	case int64:
		return typed == 0 || typed == 200
	case json.Number:
		result, _ := typed.Int64()
		return result == 0 || result == 200
	case string:
		return typed == "0" || typed == "200"
	default:
		return false
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
