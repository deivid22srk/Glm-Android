package api

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

type zcodeBridge struct {
	mu      sync.Mutex
	nextID  int64
	pending *zcodeBridgeCommand
	last    zcodeBridgeStatus
}

type zcodeBridgeCommand struct {
	CommandID      string   `json:"commandId"`
	Action         string   `json:"action"`
	ProviderIDs    []string `json:"providerIds"`
	AccountID      string   `json:"accountId,omitempty"`
	Account        string   `json:"account,omitempty"`
	ReloadRenderer bool     `json:"reloadRenderer"`
	CreatedAt      string   `json:"createdAt"`
}

type zcodeBridgeStatus struct {
	State       string `json:"state"`
	CommandID   string `json:"commandId,omitempty"`
	AccountID   string `json:"accountId,omitempty"`
	Account     string `json:"account,omitempty"`
	Message     string `json:"message,omitempty"`
	UpdatedAt   string `json:"updatedAt,omitempty"`
	BridgePatch string `json:"bridgePatch"`
}

func newZCodeBridge() *zcodeBridge {
	return &zcodeBridge{last: zcodeBridgeStatus{
		State:       "idle",
		Message:     "Nenhum refresh live solicitado ainda.",
		UpdatedAt:   time.Now().UTC().Format(time.RFC3339Nano),
		BridgePatch: "requires-zcode-renderer-patch",
	}}
}

func (b *zcodeBridge) QueueRefresh(accountID, accountLabel string) zcodeBridgeCommand {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nextID++
	command := zcodeBridgeCommand{
		CommandID:      "zcode-refresh-" + time.Now().UTC().Format("20060102T150405.000000000Z") + "-" + itoa64(b.nextID),
		Action:         "refreshCodingPlanApiKey",
		ProviderIDs:    []string{"builtin:zai-start-plan", "builtin:zai-coding-plan"},
		AccountID:      accountID,
		Account:        accountLabel,
		ReloadRenderer: true,
		CreatedAt:      time.Now().UTC().Format(time.RFC3339Nano),
	}
	b.pending = &command
	b.last = zcodeBridgeStatus{
		State:       "queued",
		CommandID:   command.CommandID,
		AccountID:   accountID,
		Account:     accountLabel,
		Message:     "Refresh live enfileirado para o renderer patchado do ZCode; a janela sera recarregada para atualizar o perfil visual.",
		UpdatedAt:   command.CreatedAt,
		BridgePatch: "requires-zcode-renderer-patch",
	}
	return command
}

func (b *zcodeBridge) Next() *zcodeBridgeCommand {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.pending == nil {
		return nil
	}
	command := *b.pending
	return &command
}

func (b *zcodeBridge) Ack(commandID string, ok bool, message string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.pending == nil || b.pending.CommandID != commandID {
		return false
	}
	state := "failed"
	if ok {
		state = "applied"
	}
	b.last = zcodeBridgeStatus{
		State:       state,
		CommandID:   b.pending.CommandID,
		AccountID:   b.pending.AccountID,
		Account:     b.pending.Account,
		Message:     message,
		UpdatedAt:   time.Now().UTC().Format(time.RFC3339Nano),
		BridgePatch: "requires-zcode-renderer-patch",
	}
	b.pending = nil
	return true
}

func (b *zcodeBridge) FallbackRestarted(commandID, message string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.pending == nil || b.pending.CommandID != commandID {
		return false
	}
	b.last = zcodeBridgeStatus{
		State:       "restarted",
		CommandID:   b.pending.CommandID,
		AccountID:   b.pending.AccountID,
		Account:     b.pending.Account,
		Message:     message,
		UpdatedAt:   time.Now().UTC().Format(time.RFC3339Nano),
		BridgePatch: "requires-zcode-renderer-patch",
	}
	b.pending = nil
	return true
}

func (b *zcodeBridge) Pending(commandID string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.pending != nil && b.pending.CommandID == commandID
}

func (b *zcodeBridge) HasPending() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.pending != nil
}

func (b *zcodeBridge) Status() zcodeBridgeStatus {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.last
}

func (s *Server) zcodeBridgeStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"object": "zcode.bridge_status", "data": s.zcode.Status()})
}

func (s *Server) zcodeBridgeNext(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"object": "zcode.bridge_next", "data": map[string]any{"command": s.zcode.Next()}})
}

func (s *Server) zcodeBridgeAckQuery(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	commandID := query.Get("commandId")
	ok := query.Get("ok") == "1" || query.Get("ok") == "true"
	s.handleZCodeBridgeAck(w, commandID, ok, query.Get("message"))
}

func (s *Server) zcodeBridgeAck(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CommandID string `json:"commandId"`
		OK        bool   `json:"ok"`
		Message   string `json:"message"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "invalid_request_error")
		return
	}
	s.handleZCodeBridgeAck(w, body.CommandID, body.OK, body.Message)
}

func (s *Server) handleZCodeBridgeAck(w http.ResponseWriter, commandID string, ok bool, message string) {
	if !s.zcode.Ack(commandID, ok, message) {
		writeError(w, http.StatusNotFound, "bridge command not found", "not_found")
		return
	}
	event := "zcode.bridge_refresh_failed"
	level := "warn"
	if ok {
		event = "zcode.bridge_refresh_applied"
		level = "info"
	}
	s.logs.add(level, event, message)
	writeJSON(w, http.StatusOK, map[string]any{"object": "zcode.bridge_ack", "ok": true})
}

func itoa64(value int64) string {
	if value == 0 {
		return "0"
	}
	const digits = "0123456789"
	var buf [20]byte
	i := len(buf)
	for value > 0 {
		i--
		buf[i] = digits[value%10]
		value /= 10
	}
	return string(buf[i:])
}
