package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"glm5.2proxy/internal/accounts"
	"glm5.2proxy/internal/zcodeenv"
)

func TestQueueZCodeRefreshSkipsUnavailableLiveRefresh(t *testing.T) {
	server := &Server{zcode: newZCodeBridge()}
	result := &zcodeenv.ApplyResult{
		Account:             accounts.PublicAccount{ID: "one", Label: "Conta 1"},
		LiveRefreshPossible: false,
		LiveRefreshQueued:   true,
		LiveRefreshReason:   "sem bridge",
	}

	commandID := server.queueZCodeRefresh(result)

	if commandID != "" {
		t.Fatalf("unexpected bridge command: %s", commandID)
	}
	if result.LiveRefreshQueued {
		t.Fatal("live refresh should not be queued when bridge refresh is unavailable")
	}
	if command := server.zcode.Next(); command != nil {
		t.Fatalf("unexpected pending bridge command: %+v", command)
	}
}

func TestAccountListIncludesQuotaFlag(t *testing.T) {
	if accountListIncludesQuota(httptest.NewRequest("GET", "/api/admin/accounts", nil)) != true {
		t.Fatal("default account list should include quota for API compatibility")
	}
	for _, target := range []string{
		"/api/admin/accounts?quota=0",
		"/api/admin/accounts?quota=false",
		"/api/admin/accounts?include_quota=skip",
	} {
		if accountListIncludesQuota(httptest.NewRequest("GET", target, nil)) {
			t.Fatalf("%s should skip quota", target)
		}
	}
}

func TestQueueZCodeRefreshQueuesAvailableLiveRefresh(t *testing.T) {
	server := &Server{zcode: newZCodeBridge()}
	result := &zcodeenv.ApplyResult{
		Account:             accounts.PublicAccount{ID: "one", Label: "Conta 1"},
		LiveRefreshPossible: true,
	}

	commandID := server.queueZCodeRefresh(result)

	if commandID == "" {
		t.Fatal("expected bridge command")
	}
	if !result.LiveRefreshQueued {
		t.Fatal("expected live refresh to be marked as queued")
	}
	if command := server.zcode.Next(); command == nil || command.CommandID != commandID || command.AccountID != "one" {
		t.Fatalf("unexpected pending bridge command: %+v", command)
	}
	if !server.zcode.Ack(commandID, true, "ok") {
		t.Fatal("expected queued bridge command to ack")
	}
}

func TestZCodeBridgeAckQuery(t *testing.T) {
	server := &Server{zcode: newZCodeBridge(), logs: newLogBuffer(10)}
	command := server.zcode.QueueRefresh("one", "Conta 1")
	request := httptest.NewRequest(http.MethodGet, "/api/admin/zcode/bridge/ack?commandId="+command.CommandID+"&ok=1&message=ok", nil)
	recorder := httptest.NewRecorder()

	server.zcodeBridgeAckQuery(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", recorder.Code, recorder.Body.String())
	}
	if pending := server.zcode.Next(); pending != nil {
		t.Fatalf("expected command to be consumed, got %+v", pending)
	}
	if status := server.zcode.Status(); status.State != "applied" {
		t.Fatalf("unexpected bridge status: %+v", status)
	}
}
