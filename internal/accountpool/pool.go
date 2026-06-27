package accountpool

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"glm5.2proxy/internal/accounts"
	"glm5.2proxy/internal/config"
	"glm5.2proxy/internal/models"
	"glm5.2proxy/internal/quota"
	"glm5.2proxy/internal/upstream"
)

type Selection struct {
	Config       upstream.Config
	Account      *accounts.Account
	Balance      *quota.Balance
	Rotated      bool
	AllExhausted bool
	Warning      string
	Reason       string
	RequestCount int64
	Available    *int64
}

type PoolItem struct {
	accounts.PublicAccount
	Balance   *quota.Balance `json:"balance"`
	Available *int64         `json:"available"`
	Exhausted bool           `json:"exhausted"`
	Error     string         `json:"error"`
}

type Pool struct {
	cfg    config.Config
	store  *accounts.Store
	loader *upstream.Loader
	quota  *quota.Service
	mu     sync.Mutex
	stats  map[string]accountStats
}

func New(cfg config.Config, store *accounts.Store, loader *upstream.Loader, quotaService *quota.Service) *Pool {
	return &Pool{cfg: cfg, store: store, loader: loader, quota: quotaService, stats: map[string]accountStats{}}
}

type accountStats struct {
	RequestCount int64
	LastSelected time.Time
}

type inspectedAccount struct {
	account   accounts.Account
	config    upstream.Config
	balance   *quota.Balance
	exhausted bool
	err       error
	stats     accountStats
}

func (p *Pool) Select(ctx context.Context, model models.Model) Selection {
	return p.SelectSkipping(ctx, model, nil)
}

func (p *Pool) SelectSkipping(ctx context.Context, model models.Model, skip map[string]bool) Selection {
	return p.selectWithRequirement(ctx, model, 0, skip)
}

func (p *Pool) SelectForRequest(ctx context.Context, model models.Model, requiredUnits int64, skip map[string]bool) Selection {
	return p.selectWithRequirement(ctx, model, requiredUnits, skip)
}

func (p *Pool) SelectBestEffort(ctx context.Context, model models.Model, skip map[string]bool) Selection {
	return p.selectWithRequirement(ctx, model, -1, skip)
}

func (p *Pool) selectWithRequirement(ctx context.Context, model models.Model, requiredUnits int64, skip map[string]bool) Selection {
	fallback := p.loader.Load(nil)
	if !p.cfg.AccountRotation || p.cfg.Authorization != "" {
		return Selection{Config: fallback}
	}
	ordered := p.ordered()
	if len(ordered) == 0 {
		return Selection{Config: fallback}
	}
	results := make([]inspectedAccount, 0, len(ordered))
	for _, account := range ordered {
		if skip[account.ID] {
			continue
		}
		upstreamConfig := p.loader.Load(&account)
		balance, err := p.quota.ModelBalanceCached(ctx, p.loader.LoadStartPlanQuota(&account), model, 15*time.Second)
		exhausted := balance != nil && balance.Available != nil && *balance.Available < p.minimumRequired(requiredUnits)
		result := inspectedAccount{account: account, config: upstreamConfig, balance: balance, exhausted: exhausted, err: err, stats: p.statsFor(account.ID)}
		results = append(results, result)
	}
	if selected, ok := p.bestEligible(results, requiredUnits); ok {
		active := p.store.Active()
		previous := ""
		if active != nil {
			previous = active.ID
		}
		rotated := previous != selected.account.ID
		if rotated {
			_, _ = p.store.Activate(selected.account.ID)
		}
		available := balanceAvailable(selected.balance)
		return Selection{
			Config: selected.config, Account: &selected.account, Balance: selected.balance, Rotated: rotated,
			Reason:       selectionReason(selected.stats, available, requiredUnits),
			RequestCount: selected.stats.RequestCount,
			Available:    availablePointer(available),
		}
	}
	if len(results) == 0 {
		return Selection{Config: fallback, AllExhausted: true}
	}
	for _, result := range results {
		if result.err != nil {
			_, _ = p.store.Activate(result.account.ID)
			return Selection{Config: result.config, Account: &result.account, Warning: fmt.Sprintf("quota unavailable for account %s: %v", result.account.ID, result.err), Reason: "cota indisponivel; usando fallback para nao bloquear o chat"}
		}
	}
	best := bestAvailable(results)
	return Selection{Config: best.config, Account: &best.account, Balance: best.balance, AllExhausted: true, Available: availablePointer(balanceAvailable(best.balance))}
}

func (p *Pool) MarkRequest(accountID string) {
	if accountID == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	current := p.stats[accountID]
	current.RequestCount++
	current.LastSelected = time.Now()
	p.stats[accountID] = current
}

func (p *Pool) statsFor(accountID string) accountStats {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.stats[accountID]
}

func (p *Pool) bestEligible(items []inspectedAccount, requiredUnits int64) (inspectedAccount, bool) {
	var best inspectedAccount
	found := false
	activeID := ""
	if active := p.store.Active(); active != nil {
		activeID = active.ID
	}
	for _, item := range items {
		if item.err != nil || item.balance == nil || item.exhausted {
			continue
		}
		if !found || p.betterCandidate(item, best, activeID, requiredUnits) {
			best = item
			found = true
		}
	}
	return best, found
}

func (p *Pool) minimumRequired(requiredUnits int64) int64 {
	if requiredUnits < 0 {
		return 0
	}
	if requiredUnits > p.cfg.AccountMinAvailable {
		return requiredUnits
	}
	return p.cfg.AccountMinAvailable
}

func (p *Pool) Snapshot(ctx context.Context, model models.Model) map[string]any {
	active := p.store.Active()
	activeID := ""
	if active != nil {
		activeID = active.ID
	}
	storedAccounts := p.store.Accounts()
	items := make([]PoolItem, 0, len(storedAccounts))
	for index, account := range storedAccounts {
		public := accounts.Sanitize(account)
		public.QueuePosition = index + 1
		public.Active = account.ID == activeID
		balance, err := p.quota.ModelBalanceCached(ctx, p.loader.LoadStartPlanQuota(&account), model, 15*time.Second)
		item := PoolItem{PublicAccount: public, Balance: balance}
		if balance != nil {
			item.Available = balance.Available
			item.Exhausted = balance.Available != nil && *balance.Available < p.cfg.AccountMinAvailable
		}
		if err != nil {
			item.Error = err.Error()
		}
		items = append(items, item)
	}
	return map[string]any{"object": "zcode.account_pool", "model": model.ID, "rotationEnabled": p.cfg.AccountRotation, "minimumAvailableUnits": p.cfg.AccountMinAvailable, "activeAccountId": activeID, "accounts": items}
}

func (p *Pool) ordered() []accounts.Account {
	storedAccounts := p.store.Accounts()
	active := p.store.Active()
	if active == nil {
		return storedAccounts
	}
	index := -1
	for current, account := range storedAccounts {
		if account.ID == active.ID {
			index = current
			break
		}
	}
	if index <= 0 {
		return storedAccounts
	}
	return append(append([]accounts.Account{}, storedAccounts[index:]...), storedAccounts[:index]...)
}

func (p *Pool) betterCandidate(left, right inspectedAccount, activeID string, requiredUnits int64) bool {
	if requiredUnits > 0 {
		leftAvailable := balanceAvailable(left.balance)
		rightAvailable := balanceAvailable(right.balance)
		if leftAvailable != rightAvailable {
			return leftAvailable > rightAvailable
		}
		if left.account.ID == activeID && right.account.ID != activeID {
			return true
		}
		if right.account.ID == activeID && left.account.ID != activeID {
			return false
		}
		return left.account.RegistrationOrder < right.account.RegistrationOrder
	}
	if left.stats.RequestCount != right.stats.RequestCount {
		return left.stats.RequestCount < right.stats.RequestCount
	}
	leftAvailable := balanceAvailable(left.balance)
	rightAvailable := balanceAvailable(right.balance)
	if leftAvailable != rightAvailable {
		return leftAvailable > rightAvailable
	}
	if left.account.ID == activeID && right.account.ID != activeID {
		return true
	}
	if right.account.ID == activeID && left.account.ID != activeID {
		return false
	}
	if !left.stats.LastSelected.Equal(right.stats.LastSelected) {
		if left.stats.LastSelected.IsZero() {
			return true
		}
		if right.stats.LastSelected.IsZero() {
			return false
		}
		return left.stats.LastSelected.Before(right.stats.LastSelected)
	}
	return left.account.RegistrationOrder < right.account.RegistrationOrder
}

func bestAvailable(items []inspectedAccount) inspectedAccount {
	var best inspectedAccount
	found := false
	for _, item := range items {
		if item.err != nil {
			continue
		}
		if !found || balanceAvailable(item.balance) > balanceAvailable(best.balance) {
			best = item
			found = true
		}
	}
	if found {
		return best
	}
	if len(items) > 0 {
		return items[0]
	}
	return inspectedAccount{}
}

func balanceAvailable(balance *quota.Balance) int64 {
	if balance == nil || balance.Available == nil {
		return 0
	}
	return *balance.Available
}

func availablePointer(value int64) *int64 {
	if value == math.MinInt64 {
		return nil
	}
	copy := value
	return &copy
}

func selectionReason(stats accountStats, available int64, requiredUnits int64) string {
	if requiredUnits > 0 {
		return fmt.Sprintf("maior cota disponivel (%d tokens) cobre a request de %d tokens", available, requiredUnits)
	}
	return fmt.Sprintf("menos requests no runtime (%d) e %d tokens disponiveis", stats.RequestCount, available)
}
