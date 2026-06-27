package api

import (
	"fmt"
	"net/http"
	"time"

	"glm5.2proxy/internal/accounts"
	"glm5.2proxy/internal/zcodeenv"
)

func (s *Server) zcodeEnvironment(w http.ResponseWriter, _ *http.Request) {
	env := zcodeenv.Detect()
	writeJSON(w, http.StatusOK, map[string]any{"object": "zcode.environment", "data": env})
}

func (s *Server) activateAccountInZCode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	activated, err := s.accounts.Activate(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), "storage_error")
		return
	}
	if activated == nil {
		writeError(w, http.StatusNotFound, "account not found", "not_found")
		return
	}
	account := s.accounts.Get(id)
	if account == nil {
		writeError(w, http.StatusNotFound, "account not found", "not_found")
		return
	}
	result, commandID, err := s.applyAccountToZCode(*account, false)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "zcode_environment_apply_failed")
		return
	}
	if result.BridgePatched {
		s.logs.add("info", "zcode.bridge_patched", result.BridgePatchMessage)
	}
	s.logs.add("info", "zcode.account_applied", zcodeApplyLogMessage(result, commandID, "gravada no ambiente interno do ZCode"))
	writeJSON(w, http.StatusOK, map[string]any{"object": "zcode.account_applied", "data": result})
}

func (s *Server) applyAccountInZCode(accountID string) (*zcodeenv.ApplyResult, error) {
	account := s.accounts.Get(accountID)
	if account == nil {
		return nil, nil
	}
	result, commandID, err := s.applyAccountToZCode(*account, true)
	if err != nil {
		s.logs.add("warn", "zcode.account_apply_failed", "Conta "+account.ID+" ativada no proxy, mas nao foi aplicada no ZCode: "+err.Error())
		return nil, err
	}
	if result == nil {
		return nil, nil
	}
	if result.BridgePatched {
		s.logs.add("info", "zcode.bridge_patched", result.BridgePatchMessage)
	}
	s.logs.add("info", "zcode.account_applied", zcodeApplyLogMessage(result, commandID, "sincronizada no disco do ZCode"))
	return result, nil
}

func (s *Server) applyAccountToZCode(account accounts.Account, requireEnvironment bool) (*zcodeenv.ApplyResult, string, error) {
	s.zcodeApplyMu.Lock()
	defer s.zcodeApplyMu.Unlock()
	s.zcodeApplySeq++
	applySeq := s.zcodeApplySeq

	env := zcodeenv.Detect()
	if requireEnvironment && !zcodeenv.Available(env) {
		s.logs.add("info", "zcode.environment_missing", "Conta ativada no proxy; ambiente interno do ZCode nao foi detectado, entao a sincronizacao foi ignorada")
		return nil, "", nil
	}
	result, err := zcodeenv.ApplyAccountWithBridgeInEnvironment(env, account, fmt.Sprintf("http://127.0.0.1:%d", s.port))
	if err != nil {
		return nil, "", err
	}
	commandID := s.queueZCodeRefresh(&result)
	s.scheduleZCodeStateReconcile(account.ID, applySeq)
	return &result, commandID, nil
}

func (s *Server) queueZCodeRefresh(result *zcodeenv.ApplyResult) string {
	if result == nil || !result.LiveRefreshPossible {
		if result != nil {
			result.LiveRefreshQueued = false
		}
		return ""
	}
	commandID := s.queueZCodeRefreshForAccount(result.Account.ID, result.Account.Label, result.LiveRefreshPossible)
	result.LiveRefreshQueued = commandID != ""
	return commandID
}

func (s *Server) scheduleZCodeRefreshFallback(commandID string) {
	go s.finishPendingZCodeRefresh(commandID)
}

func (s *Server) queueZCodeRefreshForAccount(accountID, accountLabel string, liveRefreshPossible bool) string {
	if !liveRefreshPossible {
		return ""
	}
	command := s.zcode.QueueRefresh(accountID, accountLabel)
	s.scheduleZCodeRefreshFallback(command.CommandID)
	return command.CommandID
}

func (s *Server) scheduleZCodeStateReconcile(accountID string, applySeq int64) {
	go s.reconcileZCodeState(accountID, applySeq)
}

func (s *Server) reconcileZCodeState(accountID string, applySeq int64) {
	needsLiveRefresh := false
	for _, delay := range []time.Duration{
		2 * time.Second,
		4 * time.Second,
		4 * time.Second,
		4 * time.Second,
		4 * time.Second,
		4 * time.Second,
		4 * time.Second,
		4 * time.Second,
	} {
		time.Sleep(delay)
		s.zcodeApplyMu.Lock()
		if s.zcodeApplySeq != applySeq {
			s.zcodeApplyMu.Unlock()
			return
		}
		account := s.accounts.Get(accountID)
		if account == nil {
			s.zcodeApplyMu.Unlock()
			return
		}
		env := zcodeenv.Detect()
		changed, err := zcodeenv.EnforceCodingPlanStateInEnvironment(env, *account)
		if changed {
			needsLiveRefresh = true
		}
		queuedRefresh := false
		if needsLiveRefresh && env.LiveRefreshPossible && !s.zcode.HasPending() {
			queuedRefresh = s.queueZCodeRefreshForAccount(account.ID, accounts.Sanitize(*account).Label, true) != ""
			if queuedRefresh {
				needsLiveRefresh = false
			}
		}
		s.zcodeApplyMu.Unlock()
		if err != nil {
			s.logs.add("warn", "zcode.coding_plan_reconcile_failed", "Falha ao reconciliar estado do Coding Plan no ZCode: "+err.Error())
			continue
		}
		if changed {
			s.logs.add("info", "zcode.coding_plan_reconciled", "Estado local do Coding Plan no ZCode foi reaplicado apos a inicializacao para impedir retorno de not_entitled.")
		}
		if queuedRefresh {
			s.logs.add("info", "zcode.coding_plan_reconcile_refresh_queued", "Refresh live reenfileirado apos o reparo local do Coding Plan para atualizar o estado em memoria do ZCode.")
		}
	}
}

func (s *Server) finishPendingZCodeRefresh(commandID string) {
	for range 10 {
		time.Sleep(500 * time.Millisecond)
		if !s.zcode.Pending(commandID) {
			return
		}
	}
	env := zcodeenv.Detect()
	if err := zcodeenv.Restart(env); err != nil {
		s.logs.add("warn", "zcode.bridge_refresh_fallback_failed", "Bridge do ZCode nao confirmou o refresh live e o reinicio automatico falhou: "+err.Error())
		return
	}
	message := "Bridge do ZCode nao confirmou o refresh live; credenciais foram gravadas e o ZCode foi reiniciado para carregar a conta."
	if s.zcode.FallbackRestarted(commandID, message) {
		s.logs.add("info", "zcode.bridge_refresh_fallback_restarted", message)
	}
}

func zcodeApplyLogMessage(result *zcodeenv.ApplyResult, commandID, action string) string {
	if commandID != "" {
		return "Conta " + result.Account.Label + " " + action + "; refresh live enfileirado em " + commandID
	}
	reason := result.LiveRefreshReason
	if reason == "" {
		reason = "bridge live nao disponivel"
	}
	return "Conta " + result.Account.Label + " " + action + "; refresh live nao enfileirado: " + reason
}
