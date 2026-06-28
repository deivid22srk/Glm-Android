package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"glm5.2proxy/internal/accounts"
	"glm5.2proxy/internal/codingplan"
	"glm5.2proxy/internal/quota"
	"glm5.2proxy/internal/upstream"
)

const legacyAuthMigrationKey = "zcode-auth-business-token-3.1.8"

type codingPlanRefreshOutcome struct {
	Account           accounts.PublicAccount `json:"account"`
	Result            codingplan.Result      `json:"data"`
	CredentialStored  bool                   `json:"credentialStored"`
	StartPlanSnapshot quota.Snapshot         `json:"startPlanSnapshot,omitempty"`
}

type accountRepairReport struct {
	Object   string              `json:"object"`
	Trigger  string              `json:"trigger"`
	Started  time.Time           `json:"startedAt"`
	Duration string              `json:"duration"`
	Total    int                 `json:"total"`
	Healthy  int                 `json:"healthy"`
	Repaired int                 `json:"repaired"`
	Removed  int                 `json:"removed"`
	Skipped  int                 `json:"skipped"`
	Failed   int                 `json:"failed"`
	Items    []accountRepairItem `json:"items"`
}

type accountRepairProgress struct {
	Object      string     `json:"object"`
	Active      bool       `json:"active"`
	Trigger     string     `json:"trigger,omitempty"`
	StartedAt   *time.Time `json:"startedAt,omitempty"`
	CompletedAt *time.Time `json:"completedAt,omitempty"`
	Total       int        `json:"total"`
	Processed   int        `json:"processed"`
	Healthy     int        `json:"healthy"`
	Repaired    int        `json:"repaired"`
	Removed     int        `json:"removed"`
	Skipped     int        `json:"skipped"`
	Failed      int        `json:"failed"`
	CurrentID   string     `json:"currentId,omitempty"`
	Current     string     `json:"current,omitempty"`
	CurrentMail string     `json:"currentEmail,omitempty"`
	Message     string     `json:"message,omitempty"`
}

type accountRepairItem struct {
	Account         accounts.PublicAccount `json:"account"`
	Action          string                 `json:"action"`
	Reason          string                 `json:"reason,omitempty"`
	Error           string                 `json:"error,omitempty"`
	BalanceCount    int                    `json:"balanceCount"`
	QuotaVerified   bool                   `json:"quotaVerified"`
	StartPlanOK     bool                   `json:"startPlanVerified"`
	CredentialSaved bool                   `json:"credentialSaved"`
}

func (s *Server) repairAccounts(w http.ResponseWriter, r *http.Request) {
	report := s.RepairBrokenAccounts(r.Context(), "manual")
	writeJSON(w, http.StatusOK, report)
}

func (s *Server) accountRepairStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.accountRepairProgressSnapshot())
}

func (s *Server) RemoveLegacyAuthAccountsOnce(ctx context.Context) accountRepairReport {
	started := time.Now()
	report := accountRepairReport{Object: "zcode.account_repair", Trigger: "startup_auth_migration", Started: started, Items: []accountRepairItem{}}
	if s.admin.MigrationCompleted(legacyAuthMigrationKey) {
		report.Duration = time.Since(started).Round(time.Millisecond).String()
		report.Skipped = 1
		return report
	}
	if !s.repairMu.TryLock() {
		s.logs.add("warn", "account_repair.skipped_already_running", "Migracao de contas antigas ignorada porque outra varredura ja esta em andamento")
		report.Skipped = 1
		report.Duration = time.Since(started).Round(time.Millisecond).String()
		return report
	}
	defer s.repairMu.Unlock()

	legacyAccounts := make([]accounts.Account, 0)
	for _, account := range s.accounts.Accounts() {
		if legacyAuthAccount(account) {
			legacyAccounts = append(legacyAccounts, account)
		}
	}
	report.Total = len(legacyAccounts)
	if report.Total == 0 {
		_ = s.admin.MarkMigrationCompleted(legacyAuthMigrationKey)
		report.Duration = time.Since(started).Round(time.Millisecond).String()
		return report
	}

	s.beginAccountRepairProgress(report.Trigger, report.Total)
	s.logs.add("warn", "account_repair.legacy_auth_migration_started", fmt.Sprintf("Migracao de auth do ZCode %s iniciada; removendo %d conta(s) antigas sem codingPlanApiKey", s.cfg.AppVersion, report.Total))
	for _, account := range legacyAccounts {
		select {
		case <-ctx.Done():
			cancelled := accountRepairItem{Account: accounts.Sanitize(account), Action: "cancelled", Error: ctx.Err().Error()}
			report.Items = append(report.Items, cancelled)
			report.Failed++
			s.updateAccountRepairProgress(cancelled)
			s.finishAccountRepairProgress("Migracao de contas antigas cancelada: " + ctx.Err().Error())
			report.Duration = time.Since(started).Round(time.Millisecond).String()
			return report
		default:
		}
		public := accounts.Sanitize(account)
		s.setAccountRepairCurrent(public)
		item := accountRepairItem{Account: public, Action: "removed", Reason: "conta criada antes do fluxo novo do ZCode; ausente codingPlanApiKey"}
		removed, err := s.accounts.Remove(account.ID)
		if err != nil {
			item.Action = "failed"
			item.Error = err.Error()
			report.Failed++
			s.logs.add("error", "account_repair.legacy_auth_remove_failed", fmt.Sprintf("%s nao pode ser removida na migracao de auth: %s", public.Label, err))
		} else if !removed {
			item.Action = "failed"
			item.Error = "conta nao encontrada para remocao"
			report.Failed++
			s.logs.add("warn", "account_repair.legacy_auth_remove_missing", fmt.Sprintf("%s nao foi encontrada para remocao na migracao de auth", public.Label))
		} else {
			report.Removed++
			_ = s.admin.SetAccountThinking(account.ID, nil)
			s.logs.add("warn", "account_repair.legacy_auth_account_removed", fmt.Sprintf("%s removida sem consultar cota; conta antiga nao tinha codingPlanApiKey do fluxo novo do ZCode", public.Label))
		}
		report.Items = append(report.Items, item)
		s.updateAccountRepairProgress(item)
	}
	report.Duration = time.Since(started).Round(time.Millisecond).String()
	message := fmt.Sprintf("Migracao de contas antigas concluida em %s: removidas=%d falhas=%d", report.Duration, report.Removed, report.Failed)
	if report.Failed == 0 {
		if err := s.admin.MarkMigrationCompleted(legacyAuthMigrationKey); err != nil {
			report.Failed++
			message += "; falha ao gravar marcador: " + err.Error()
			s.logs.add("error", "account_repair.legacy_auth_marker_failed", "Falha ao gravar marcador da migracao de auth: "+err.Error())
		}
	}
	s.logs.add("warn", "account_repair.legacy_auth_migration_completed", message)
	s.finishAccountRepairProgress(message)
	return report
}

func (s *Server) RepairBrokenAccounts(ctx context.Context, trigger string) accountRepairReport {
	started := time.Now()
	report := accountRepairReport{Object: "zcode.account_repair", Trigger: trigger, Started: started, Items: []accountRepairItem{}}
	if !s.repairMu.TryLock() {
		s.logs.add("warn", "account_repair.skipped_already_running", "Manutencao de contas ignorada porque outra varredura ja esta em andamento")
		report.Skipped = 1
		report.Duration = time.Since(started).Round(time.Millisecond).String()
		return report
	}
	defer s.repairMu.Unlock()

	savedAccounts := s.accounts.Accounts()
	report.Total = len(savedAccounts)
	s.beginAccountRepairProgress(trigger, report.Total)
	s.logs.add("info", "account_repair.started", fmt.Sprintf("Manutencao de contas iniciada por %s; avaliando %d conta(s)", trigger, len(savedAccounts)))
	for _, account := range savedAccounts {
		select {
		case <-ctx.Done():
			report.Failed++
			cancelled := accountRepairItem{Account: accounts.Sanitize(account), Action: "cancelled", Error: ctx.Err().Error()}
			report.Items = append(report.Items, cancelled)
			s.updateAccountRepairProgress(cancelled)
			s.logs.add("warn", "account_repair.cancelled", "Manutencao de contas cancelada: "+ctx.Err().Error())
			report.Duration = time.Since(started).Round(time.Millisecond).String()
			s.finishAccountRepairProgress("Manutencao cancelada: " + ctx.Err().Error())
			return report
		default:
		}
		s.setAccountRepairCurrent(accounts.Sanitize(account))
		item := s.repairOneAccount(ctx, account)
		report.Items = append(report.Items, item)
		s.updateAccountRepairProgress(item)
		switch item.Action {
		case "healthy":
			report.Healthy++
		case "repaired":
			report.Repaired++
		case "removed":
			report.Removed++
		case "skipped":
			report.Skipped++
		default:
			report.Failed++
		}
	}
	report.Duration = time.Since(started).Round(time.Millisecond).String()
	message := fmt.Sprintf("Manutencao de contas concluida em %s: saudaveis=%d reparadas=%d removidas=%d ignoradas=%d falhas=%d", report.Duration, report.Healthy, report.Repaired, report.Removed, report.Skipped, report.Failed)
	s.logs.add("info", "account_repair.completed", message)
	s.finishAccountRepairProgress(message)
	return report
}

func legacyAuthAccount(account accounts.Account) bool {
	return strings.TrimSpace(account.CodingPlanAPIKey) == ""
}

func (s *Server) repairOneAccount(ctx context.Context, account accounts.Account) accountRepairItem {
	public := accounts.Sanitize(account)
	item := accountRepairItem{Account: public}
	s.logs.add("info", "account_repair.account_started", fmt.Sprintf("Verificando %s (%s)", public.Label, public.User.Email))

	before, beforeErr := s.startPlanSnapshot(ctx, account)
	if beforeErr == nil && hasQuotaBalances(before) {
		if strings.TrimSpace(account.CodingPlanAPIKey) != "" {
			item.Action = "healthy"
			item.Reason = "Start Plan ja possui balances e Coding Plan API key esta salva"
			item.BalanceCount = len(before.Balances)
			item.StartPlanOK = true
			item.CredentialSaved = true
			s.logs.add("info", "account_repair.account_healthy", fmt.Sprintf("%s mantida: Start Plan retornou %d balance(s) e Coding Plan API key esta salva", public.Label, len(before.Balances)))
			return item
		}
		item.BalanceCount = len(before.Balances)
		item.StartPlanOK = true
		s.logs.add("warn", "account_repair.missing_coding_plan_key", fmt.Sprintf("%s tem Start Plan com %d balance(s), mas nao tem Coding Plan API key; tentando reparar credencial de chat", public.Label, len(before.Balances)))
	}
	if beforeErr != nil {
		item.Error = beforeErr.Error()
		s.logs.add("warn", "account_repair.quota_check_failed", fmt.Sprintf("%s falhou ao consultar Start Plan antes do reparo: %s", public.Label, beforeErr))
		if transientAccountRepairError(beforeErr) {
			item.Action = "skipped"
			item.Reason = "erro temporario ao consultar cota; nao removi a conta"
			s.logs.add("warn", "account_repair.account_skipped_transient", fmt.Sprintf("%s nao foi removida porque a falha parece temporaria: %s", public.Label, beforeErr))
			return item
		}
	} else if !hasQuotaBalances(before) {
		s.logs.add("warn", "account_repair.empty_quota", fmt.Sprintf("%s esta sem balances de Start Plan; tentando reparar direto pelo proxy", public.Label))
	}

	if strings.TrimSpace(account.ZAIAcccessToken) == "" {
		return s.removeBrokenAccount(item, "conta sem balances de Start Plan e sem ZAI access token salvo para migrar")
	}
	outcome, err := s.refreshCodingPlanForAccount(ctx, account)
	if err != nil {
		item.Error = err.Error()
		s.logs.add("warn", "account_repair.repair_failed", fmt.Sprintf("%s falhou no reparo direto: %s", public.Label, err))
		if transientAccountRepairError(err) {
			item.Action = "skipped"
			item.Reason = "erro temporario no reparo direto; nao removi a conta"
			return item
		}
		return s.removeBrokenAccount(item, "reparo direto falhou e a conta continuava sem cota: "+err.Error())
	}
	item.QuotaVerified = outcome.Result.QuotaVerified
	item.StartPlanOK = outcome.Result.StartPlanVerified
	item.CredentialSaved = outcome.CredentialStored
	item.BalanceCount = len(outcome.StartPlanSnapshot.Balances)
	if outcome.Result.StartPlanVerified && hasQuotaBalances(outcome.StartPlanSnapshot) && outcome.CredentialStored {
		item.Action = "repaired"
		item.Reason = "Start Plan voltou a retornar balances depois do reparo"
		s.logs.add("info", "account_repair.account_repaired", fmt.Sprintf("%s reparada: Start Plan retornou %d balance(s); quota_verified=%t credential_saved=%t", public.Label, len(outcome.StartPlanSnapshot.Balances), outcome.Result.QuotaVerified, outcome.CredentialStored))
		return item
	}
	reason := "reparo direto terminou, mas Start Plan continuou sem balances"
	if outcome.Result.StartPlanVerified && hasQuotaBalances(outcome.StartPlanSnapshot) && !outcome.CredentialStored {
		reason = "reparo direto confirmou Start Plan, mas nao conseguiu salvar a credencial de chat"
	} else if outcome.Result.StartPlanError != "" {
		reason += ": " + outcome.Result.StartPlanError
	}
	return s.removeBrokenAccount(item, reason)
}

func (s *Server) refreshCodingPlanForAccount(ctx context.Context, account accounts.Account) (codingPlanRefreshOutcome, error) {
	outcome := codingPlanRefreshOutcome{Account: accounts.Sanitize(account)}
	result, err := s.codingPlan.Refresh(ctx, account)
	if err != nil {
		return outcome, err
	}
	if result.Credential != "" {
		quotaSnapshot, err := s.quota.BalanceSnapshot(ctx, upstream.Config{
			QuotaEndpoint:      s.cfg.ZAIUsageQuotaURL,
			QuotaAuthorization: result.Credential,
			BaseHeaders:        map[string]string{"user-agent": "ZCode/" + s.cfg.AppVersion},
			HasAuthorization:   true,
		})
		if err != nil || len(quotaSnapshot.Balances) == 0 {
			if err != nil {
				result.QuotaError = err.Error()
			} else {
				result.QuotaError = "coding plan quota returned no balances"
			}
		} else {
			result.QuotaVerified = true
		}
		if _, err := s.accounts.UpdateCodingPlanAPIKey(account.ID, result.Credential); err != nil {
			return outcome, err
		}
		outcome.CredentialStored = true
	}
	startPlanSnapshot, err := s.startPlanSnapshot(ctx, account)
	if err != nil {
		result.StartPlanError = err.Error()
	} else {
		outcome.StartPlanSnapshot = startPlanSnapshot
		result.StartPlanVerified = hasQuotaBalances(startPlanSnapshot)
		if !result.StartPlanVerified {
			result.StartPlanError = "start plan returned no balances"
		}
	}
	outcome.Result = result
	return outcome, nil
}

func (s *Server) startPlanSnapshot(ctx context.Context, account accounts.Account) (quota.Snapshot, error) {
	return s.quota.Snapshot(ctx, s.loader.LoadStartPlanQuota(&account))
}

func hasQuotaBalances(snapshot quota.Snapshot) bool {
	return len(snapshot.Balances) > 0
}

func (s *Server) removeBrokenAccount(item accountRepairItem, reason string) accountRepairItem {
	item.Action = "removed"
	item.Reason = reason
	removed, err := s.accounts.Remove(item.Account.ID)
	if err != nil {
		item.Action = "failed"
		item.Error = err.Error()
		s.logs.add("error", "account_repair.remove_failed", fmt.Sprintf("%s nao pode ser removida: %s. Motivo original: %s", item.Account.Label, err, reason))
		return item
	}
	if !removed {
		item.Action = "failed"
		item.Error = "conta nao encontrada para remocao"
		s.logs.add("warn", "account_repair.remove_missing", fmt.Sprintf("%s nao foi encontrada para remocao. Motivo original: %s", item.Account.Label, reason))
		return item
	}
	_ = s.admin.SetAccountThinking(item.Account.ID, nil)
	s.logs.add("warn", "account_repair.account_removed", fmt.Sprintf("%s removida do pool; fluxo antigo do ZCode nao conseguiu ser migrado. Motivo: %s", item.Account.Label, reason))
	return item
}

func (s *Server) beginAccountRepairProgress(trigger string, total int) {
	now := time.Now()
	s.repairStateMu.Lock()
	defer s.repairStateMu.Unlock()
	s.repairState = accountRepairProgress{
		Object:    "zcode.account_repair_progress",
		Active:    true,
		Trigger:   trigger,
		StartedAt: &now,
		Total:     total,
		Message:   "Verificando contas salvas do fluxo antigo do ZCode",
	}
}

func (s *Server) setAccountRepairCurrent(account accounts.PublicAccount) {
	s.repairStateMu.Lock()
	defer s.repairStateMu.Unlock()
	if s.repairState.Object == "" {
		s.repairState.Object = "zcode.account_repair_progress"
	}
	s.repairState.CurrentID = account.ID
	s.repairState.Current = account.Label
	s.repairState.CurrentMail = account.User.Email
	s.repairState.Message = fmt.Sprintf("Verificando %s", account.Label)
}

func (s *Server) updateAccountRepairProgress(item accountRepairItem) {
	s.repairStateMu.Lock()
	defer s.repairStateMu.Unlock()
	if s.repairState.Object == "" {
		s.repairState.Object = "zcode.account_repair_progress"
	}
	s.repairState.Processed++
	switch item.Action {
	case "healthy":
		s.repairState.Healthy++
	case "repaired":
		s.repairState.Repaired++
	case "removed":
		s.repairState.Removed++
	case "skipped":
		s.repairState.Skipped++
	default:
		s.repairState.Failed++
	}
	if item.Action == "removed" {
		s.repairState.Message = fmt.Sprintf("%s removida: %s", item.Account.Label, item.Reason)
		return
	}
	if item.Action == "repaired" {
		s.repairState.Message = fmt.Sprintf("%s migrada e mantida", item.Account.Label)
		return
	}
	s.repairState.Message = fmt.Sprintf("%s verificada", item.Account.Label)
}

func (s *Server) finishAccountRepairProgress(message string) {
	now := time.Now()
	s.repairStateMu.Lock()
	defer s.repairStateMu.Unlock()
	if s.repairState.Object == "" {
		s.repairState.Object = "zcode.account_repair_progress"
	}
	s.repairState.Active = false
	s.repairState.CompletedAt = &now
	s.repairState.CurrentID = ""
	s.repairState.Current = ""
	s.repairState.CurrentMail = ""
	s.repairState.Message = message
}

func (s *Server) accountRepairProgressSnapshot() accountRepairProgress {
	s.repairStateMu.RLock()
	defer s.repairStateMu.RUnlock()
	if s.repairState.Object == "" {
		return accountRepairProgress{Object: "zcode.account_repair_progress", Message: "Aguardando manutencao automatica de contas"}
	}
	return s.repairState
}

func transientAccountRepairError(err error) bool {
	if err == nil {
		return false
	}
	value := strings.ToLower(err.Error())
	for _, marker := range []string{"context deadline", "context canceled", "timeout", "temporarily", "connection reset", "server closed idle connection", "http 429", "too many requests", "http 500", "http 502", "http 503", "http 504"} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}
