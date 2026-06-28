import { useEffect, useRef, useState } from 'react'
import { AnimatePresence, motion, Reorder } from 'framer-motion'
import { Activity, AlertTriangle, ArrowRight, Bot, CheckCircle2, Clock3, KeyRound, ListRestart, Loader2, MailCheck, PlugZap, Plus, RefreshCw, Server, Settings2, ShieldAlert, Sparkles, Trash2, UserPlus } from 'lucide-react'
import { AccountCard } from '@/components/account-card'
import { AccountCreatorCard } from '@/components/account-creator-card'
import { APISettingsDialog } from '@/components/api-settings-dialog'
import { LoginDialog } from '@/components/login-dialog'
import { LogsDialog } from '@/components/logs-dialog'
import { SettingsDialog } from '@/components/settings-dialog'
import { ThemeToggle } from '@/components/theme-toggle'
import { Button } from '@/components/ui/button'
import { useAccounts } from '@/hooks/use-accounts'
import { useOverview } from '@/hooks/use-overview'
import { useSettings } from '@/hooks/use-settings'
import { api } from '@/lib/api'
import type { Account, AccountActivateResponse, AccountCreatorStatus, AccountDeleteResponse, AccountRepairProgress, ThinkingSettings, ZCodeApplyResult } from '@/types/api'

function move(items: Account[], from: number, to: number): Account[] {
  const next = [...items]
  const [item] = next.splice(from, 1)
  if (item) next.splice(to, 0, item)
  return next
}

function formatElapsed(from: number, now: number): string {
  const diffSeconds = Math.max(0, Math.floor((now - from) / 1000))
  if (diffSeconds < 60) return `${diffSeconds}s`
  const minutes = Math.floor(diffSeconds / 60)
  const seconds = diffSeconds % 60
  return `${minutes}m ${seconds}s`
}

function zcodeApplyMessage(result: ZCodeApplyResult): string {
  if (result.bridgePatched) {
    const restartText = result.bridgeRestartedApp
      ? ' O ZCode foi reiniciado uma vez para carregar o bridge.'
      : ''
    return `${result.bridgePatchMessage ?? 'Bridge do ZCode instalado automaticamente.'}${restartText} A conta foi aplicada e o refresh live ficou pronto.`
  }
  if (result.bridgeRestartedApp) {
    return 'Conta gravada no ZCode. O bridge nao confirmou o refresh live, entao o ZCode foi reiniciado para carregar a conta.'
  }
  if (result.liveRefreshQueued) {
    return 'Conta gravada no ZCode e refresh live enfileirado. Com o bridge instalado, a janela do ZCode recarrega sozinha para mostrar o perfil certo.'
  }
  if (result.liveRefreshPossible) return 'Conta aplicada no ZCode e refresh live disponivel.'
  const suffix = result.liveRefreshReason ? ` Motivo: ${result.liveRefreshReason}` : ''
  return `Conta gravada no ZCode. A janela aberta pode continuar usando a credencial antiga ate o ZCode recarregar o runtime.${suffix}`
}

function shouldShowRepairNotice(progress: AccountRepairProgress | null | undefined): progress is AccountRepairProgress {
  if (!progress) return false
  if (progress.active) return true
  return progress.total > 0 && (progress.removed > 0 || progress.repaired > 0 || progress.skipped > 0 || progress.failed > 0)
}

function repairNoticeKey(progress: AccountRepairProgress | null | undefined): string | null {
  if (!progress) return null
  return `${progress.trigger ?? 'repair'}:${progress.startedAt ?? ''}:${progress.completedAt ?? ''}:${progress.total}:${progress.processed}:${progress.removed}:${progress.failed}`
}

function AuthMigrationModal({ progress, onClose }: { progress: AccountRepairProgress; onClose: () => void }) {
  const total = Math.max(progress.total, 0)
  const processed = Math.min(Math.max(progress.processed, 0), total || progress.processed)
  const percent = total > 0 ? Math.min(100, Math.round((processed / total) * 100)) : 0
  const isLegacyMigration = progress.trigger === 'startup_auth_migration'
  const status = progress.active
    ? progress.current
      ? `${isLegacyMigration ? 'Removendo' : 'Analisando'} ${progress.current}${progress.currentEmail ? ` - ${progress.currentEmail}` : ''}`
      : isLegacyMigration ? 'Removendo contas antigas' : 'Analisando contas salvas'
    : progress.message || 'Manutencao concluida'

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/68 p-6 backdrop-blur-md">
      <section className="relative max-h-[92vh] w-full max-w-3xl overflow-hidden rounded-2xl border border-red-400/24 bg-[linear-gradient(135deg,rgba(76,29,29,0.28),rgba(18,18,20,0.97)_48%,rgba(10,15,23,0.96))] p-5 shadow-[0_28px_90px_rgba(0,0,0,0.58)]">
        <div className="pointer-events-none absolute inset-0 bg-[radial-gradient(circle_at_top_right,rgba(239,68,68,0.15),transparent_34%),radial-gradient(circle_at_bottom_left,rgba(148,163,184,0.10),transparent_35%)]" />
        <div className="relative">
          <div className="flex flex-col gap-4 sm:flex-row sm:items-start">
            <div className="flex h-14 w-14 shrink-0 items-center justify-center rounded-xl border border-red-300/24 bg-red-400/10 text-red-100">
              <ShieldAlert className="h-7 w-7" />
            </div>
            <div className="min-w-0 flex-1">
              <div className="flex flex-wrap items-center gap-2">
                <p className="text-lg font-semibold text-red-50">Atualizacao urgente de autenticacao do ZCode</p>
                <span className="inline-flex items-center gap-1 rounded-full border border-red-300/24 bg-red-300/10 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-[0.14em] text-red-100">
                  <AlertTriangle className="h-3 w-3" />
                  {isLegacyMigration ? 'Limpeza unica' : 'Manutencao'}
                </span>
              </div>
              <p className="mt-2 text-sm leading-6 text-slate-300">
                {isLegacyMigration
                  ? 'Esta versao detectou contas criadas antes do fluxo novo de auth do ZCode. Elas nao possuem codingPlanApiKey e quebram no endpoint atual, entao o app esta removendo essas contas antigas direto, sem perder tempo consultando cota.'
                  : 'O app esta revisando contas salvas. Quando uma conta nao consegue ser migrada para o fluxo atual do ZCode, ela sai do pool para nao continuar derrubando requests.'}
              </p>
            </div>
          </div>

          <div className="mt-5 grid grid-cols-3 gap-2 text-center">
            <div className="rounded-lg border border-white/10 bg-black/22 px-3 py-3">
              <p className="text-[11px] uppercase tracking-[0.12em] text-slate-400">Removidas</p>
              <p className="mt-1 text-2xl font-semibold text-red-50">{progress.removed}</p>
            </div>
            <div className="rounded-lg border border-white/10 bg-black/22 px-3 py-3">
              <p className="text-[11px] uppercase tracking-[0.12em] text-slate-400">Migradas</p>
              <p className="mt-1 text-2xl font-semibold text-emerald-100">{progress.repaired}</p>
            </div>
            <div className="rounded-lg border border-white/10 bg-black/22 px-3 py-3">
              <p className="text-[11px] uppercase tracking-[0.12em] text-slate-400">Falhas</p>
              <p className="mt-1 text-2xl font-semibold text-red-100">{progress.failed}</p>
            </div>
          </div>

          <div className="mt-5 rounded-xl border border-white/10 bg-black/25 p-4">
            <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
              <p className="flex min-w-0 items-center gap-2 truncate text-sm font-medium text-slate-100">
                <Trash2 className="h-4 w-4 shrink-0 text-red-200" />
                <span className="truncate">{status}</span>
              </p>
              <p className="text-xs font-medium text-slate-400">
                {processed}/{total} contas processadas
              </p>
            </div>
            <div className="h-2.5 overflow-hidden rounded-full bg-zinc-700">
              <div
                className="h-full rounded-full bg-[linear-gradient(90deg,#a1a1aa,#ef4444)] transition-[width] duration-500"
                style={{ width: `${percent}%` }}
              />
            </div>
            <p className="mt-3 text-xs leading-5 text-slate-400">
              Antes de remover, o app cria backup local do arquivo de credenciais. Depois dessa limpeza unica, a inicializacao
              volta a usar a manutencao normal de cota e reparo.
            </p>
          </div>

          {!progress.active && (
            <div className="mt-5 flex justify-end">
              <Button onClick={onClose}>
                Entendi
              </Button>
            </div>
          )}
        </div>
      </section>
    </div>
  )
}

const creationSteps = [
  { id: 'email', label: 'Email', icon: MailCheck },
  { id: 'register', label: 'Cadastro', icon: UserPlus },
  { id: 'captcha', label: 'Captcha', icon: ShieldAlert },
  { id: 'link_proxy', label: 'Vinculo', icon: PlugZap },
  { id: 'coding_plan', label: 'Cota', icon: CheckCircle2 },
]

function AccountCreationModal({ status }: { status: AccountCreatorStatus }) {
  const progress = status.progress ?? { stage: 'prepare', message: 'Preparando automacao', percent: 8 }
  const percent = Math.max(0, Math.min(100, progress.percent || 0))
  const activeIndex = Math.max(0, creationSteps.findIndex((step) => step.id === progress.stage))

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/68 p-6 backdrop-blur-md">
      <section className="relative w-full max-w-3xl overflow-hidden rounded-2xl border border-emerald-400/24 bg-[linear-gradient(135deg,rgba(5,46,22,0.24),rgba(12,18,20,0.98)_45%,rgba(8,13,22,0.96))] p-5 shadow-[0_28px_90px_rgba(0,0,0,0.58)]">
        <div className="pointer-events-none absolute inset-0 bg-[radial-gradient(circle_at_top_left,rgba(16,185,129,0.18),transparent_34%),radial-gradient(circle_at_bottom_right,rgba(59,130,246,0.13),transparent_35%)]" />
        <div className="relative">
          <div className="flex flex-col gap-4 sm:flex-row sm:items-start">
            <div className="flex h-14 w-14 shrink-0 items-center justify-center rounded-xl border border-emerald-300/22 bg-emerald-400/10 text-emerald-100">
              <Bot className="h-7 w-7" />
            </div>
            <div className="min-w-0 flex-1">
              <div className="flex flex-wrap items-center gap-2">
                <h2 className="text-lg font-semibold text-emerald-50">Criando nova conta automaticamente</h2>
                <span className="inline-flex items-center gap-1 rounded-full border border-emerald-300/20 bg-emerald-300/10 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-[0.14em] text-emerald-100">
                  <Loader2 className="h-3 w-3 animate-spin" />
                  Em andamento
                </span>
              </div>
              <p className="mt-2 text-sm leading-6 text-slate-300">
                O proxy nao encontrou conta capaz de atender a request. A automacao esta criando uma conta, resolvendo o captcha,
                vinculando ao proxy e validando a cota antes de liberar a fila.
              </p>
            </div>
          </div>

          <div className="mt-6 rounded-xl border border-white/10 bg-black/24 p-4">
            <div className="mb-3 flex items-center justify-between gap-3">
              <div className="min-w-0">
                <p className="truncate text-base font-semibold text-slate-50">{progress.message || 'Executando automacao'}</p>
                <p className="mt-1 truncate text-xs text-slate-400" title={progress.detail || progress.lastLogLine}>
                  {progress.detail || progress.lastLogLine || status.workDir}
                </p>
              </div>
              <p className="shrink-0 text-2xl font-semibold tabular-nums text-emerald-100">{percent}%</p>
            </div>
            <div className="h-2.5 overflow-hidden rounded-full bg-zinc-700">
              <div
                className="h-full rounded-full bg-[linear-gradient(90deg,#38bdf8,#10b981)] transition-[width] duration-500"
                style={{ width: `${percent}%` }}
              />
            </div>
          </div>

          <div className="mt-5 grid gap-2 sm:grid-cols-5">
            {creationSteps.map((step, index) => {
              const Icon = step.icon
              const done = index < activeIndex || percent >= 100
              const active = index === activeIndex && percent < 100
              return (
                <div
                  key={step.id}
                  className={`rounded-xl border px-3 py-3 text-center transition-colors ${
                    done
                      ? 'border-emerald-400/24 bg-emerald-400/10 text-emerald-100'
                      : active
                        ? 'border-sky-400/28 bg-sky-400/10 text-sky-100'
                        : 'border-white/10 bg-black/18 text-slate-500'
                  }`}
                >
                  <Icon className={`mx-auto h-4 w-4 ${active ? 'animate-pulse' : ''}`} />
                  <p className="mt-2 text-[11px] font-semibold uppercase tracking-[0.12em]">{step.label}</p>
                </div>
              )
            })}
          </div>

          <div className="mt-5 rounded-xl border border-white/10 bg-black/20 px-4 py-3">
            <p className="text-xs uppercase tracking-[0.14em] text-slate-500">Runtime</p>
            <div className="mt-2 grid gap-2 text-sm text-slate-300 sm:grid-cols-2">
              <p className="truncate"><span className="text-slate-500">Modo:</span> {status.mode === 'external' ? 'Pasta externa' : 'Embutido no app'}</p>
              <p className="truncate"><span className="text-slate-500">Solver:</span> {status.solverApiBase}</p>
            </div>
          </div>
        </div>
      </section>
    </div>
  )
}

type AccountAction = { id: string; type: 'activate' | 'applyZCode' | 'delete' }

export function Home() {
  const { data: accountsData, loading, error: accountsError, refresh, reorder, quotaRefreshing, refreshAccountQuota } = useAccounts()
  const settingsState = useSettings()
  const overviewState = useOverview(1000)
  const [accounts, setAccounts] = useState<Account[]>([])
  const [optimisticActiveAccountId, setOptimisticActiveAccountId] = useState<string | null>(null)
  const [loginOpen, setLoginOpen] = useState(false)
  const [logsOpen, setLogsOpen] = useState(false)
  const [apiSettingsOpen, setAPISettingsOpen] = useState(false)
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [refreshing, setRefreshing] = useState(false)
  const [dismissedRepairNotice, setDismissedRepairNotice] = useState<string | null>(null)
  const [accountAction, setAccountActionState] = useState<AccountAction | null>(null)
  const [switchEvent, setSwitchEvent] = useState<{ fromId: string | null; toId: string; timestamp: number } | null>(null)
  const [zcodeSync, setZCodeSync] = useState<Record<string, { status: 'idle' | 'syncing' | 'synced' | 'skipped' | 'error'; message: string | null }>>({})
  const [now, setNow] = useState(() => Date.now())
  const dragOrderRef = useRef<Account[]>([])
  const previousActiveIdRef = useRef<string | null>(null)
  const accountActionRef = useRef<AccountAction | null>(null)
  const lastQuotaRefreshSignalRef = useRef<string | null>(null)

  const setAccountAction = (action: AccountAction | null) => {
    accountActionRef.current = action
    setAccountActionState(action)
  }

  const clearAccountAction = (id: string, type: AccountAction['type']) => {
    const current: AccountAction | null = accountActionRef.current
    if (current?.id === id && current.type === type) {
      setAccountAction(null)
    }
  }

  useEffect(() => {
    if (accountsData) {
      setAccounts(accountsData.data)
      dragOrderRef.current = accountsData.data
      if (optimisticActiveAccountId && accountsData.activeAccountId === optimisticActiveAccountId) {
        setOptimisticActiveAccountId(null)
      }
    }
  }, [accountsData, optimisticActiveAccountId])

  const activeAccountId = optimisticActiveAccountId ?? accountsData?.activeAccountId ?? null
  const activeAccount = accounts.find((account) => account.id === activeAccountId) ?? null
  const accountRepair = overviewState.overview?.accountRepair
  const accountCreator = overviewState.overview?.accountCreator
  const recentQuotaUpdate = overviewState.overview?.recentQuotaUpdate
  const accountRepairNoticeKey = repairNoticeKey(accountRepair)
  const showRepairModal = shouldShowRepairNotice(accountRepair) && (accountRepair.active || accountRepairNoticeKey !== dismissedRepairNotice)
  const settings = settingsState.settings
  const apiStatusText = settings?.apiEnabled
    ? settings.apiKeys.length > 0
      ? `${settings.apiKeys.length} API key${settings.apiKeys.length === 1 ? '' : 's'} local${settings.apiKeys.length === 1 ? '' : 's'} - /v1/chat/completions disponivel`
      : 'Sem API key local - localhost liberado - /v1/chat/completions disponivel'
    : 'O painel continua funcionando; clientes /v1 recebem API indisponivel'

  useEffect(() => {
    if (!activeAccountId) {
      previousActiveIdRef.current = activeAccountId
      return
    }
    const previousActiveId = previousActiveIdRef.current
    if (previousActiveId && previousActiveId !== activeAccountId) {
      setSwitchEvent({ fromId: previousActiveId, toId: activeAccountId, timestamp: Date.now() })
    }
    previousActiveIdRef.current = activeAccountId
  }, [activeAccountId])

  useEffect(() => {
    if (!recentQuotaUpdate?.accountId || !recentQuotaUpdate.updatedAt) {
      return
    }
    const signal = `${recentQuotaUpdate.accountId}:${recentQuotaUpdate.updatedAt}`
    if (lastQuotaRefreshSignalRef.current === signal) {
      return
    }
    lastQuotaRefreshSignalRef.current = signal
    void refreshAccountQuota(recentQuotaUpdate.accountId, { clearError: false })
  }, [recentQuotaUpdate, refreshAccountQuota])

  useEffect(() => {
    if (!switchEvent) return
    const timer = window.setInterval(() => setNow(Date.now()), 1000)
    const cleanup = window.setTimeout(() => setSwitchEvent((current) => {
      if (!current) return current
      return Date.now() - current.timestamp >= 45000 ? null : current
    }), 45000)
    return () => {
      window.clearInterval(timer)
      window.clearTimeout(cleanup)
    }
  }, [switchEvent])

  const findAccount = (id: string | null) => accounts.find((account) => account.id === id) ?? null
  const previousAccount = switchEvent ? findAccount(switchEvent.fromId) : null
  const switchedAccount = switchEvent ? findAccount(switchEvent.toId) : null

  const persistOrder = async (ordered: Account[]) => {
    setAccounts(ordered)
    dragOrderRef.current = ordered
    try {
      await reorder(ordered)
      await refresh({ cancelInFlight: true })
    } catch {
      await refresh({ cancelInFlight: true })
    }
  }

  const activate = async (id: string) => {
    if (accountActionRef.current) return
    setAccountAction({ id, type: 'activate' })
    setZCodeSync((current) => ({ ...current, [id]: { status: 'skipped', message: 'Conta ativa do proxy alterada. O ZCode nao foi modificado.' } }))
    try {
      const result = await api.post<AccountActivateResponse>(`/api/admin/accounts/${id}/activate`)
      setOptimisticActiveAccountId(result.activeAccount.id || id)
      setZCodeSync((current) => ({
        ...current,
        [id]: { status: 'skipped', message: 'Conta ativa do proxy alterada. Use Aplicar no ZCode para mudar o app ZCode.' },
      }))
      await refresh({ cancelInFlight: true })
    } catch (err) {
      setZCodeSync((current) => ({ ...current, [id]: { status: 'error', message: err instanceof Error ? err.message : 'Falha ao ativar conta' } }))
    } finally {
      clearAccountAction(id, 'activate')
    }
  }

  const applyAccountInZCode = async (id: string) => {
    if (accountActionRef.current) return
    setAccountAction({ id, type: 'applyZCode' })
    setZCodeSync((current) => ({ ...current, [id]: { status: 'syncing', message: 'Aplicando manualmente no ZCode...' } }))
    try {
      const response = await api.post<{ data: ZCodeApplyResult }>(`/api/admin/zcode/accounts/${id}/activate`)
      setOptimisticActiveAccountId(response.data.account.id || id)
      setZCodeSync((current) => ({ ...current, [id]: { status: 'synced', message: zcodeApplyMessage(response.data) } }))
      await refresh({ cancelInFlight: true })
    } catch (err) {
      setZCodeSync((current) => ({ ...current, [id]: { status: 'error', message: err instanceof Error ? err.message : 'Falha ao aplicar no ZCode' } }))
      throw err
    } finally {
      clearAccountAction(id, 'applyZCode')
    }
  }

  const deleteAccount = async (id: string) => {
    if (accountActionRef.current) return
    setAccountAction({ id, type: 'delete' })
    try {
      const response = await api.delete<AccountDeleteResponse>(`/api/admin/accounts/${id}`)
      setAccounts((current) => current.filter((account) => account.id !== id))
      if (activeAccountId === id) {
        setOptimisticActiveAccountId(response.activeAccount?.id ?? null)
      }
      await refresh({ cancelInFlight: true, includeQuota: true })
    } finally {
      clearAccountAction(id, 'delete')
    }
  }

  const refreshAccounts = async () => {
    setRefreshing(true)
    try {
      await refresh({ cancelInFlight: true, includeQuota: true })
    } finally {
      setRefreshing(false)
    }
  }

  const moveAccount = async (index: number, direction: -1 | 1) => {
    const target = index + direction
    if (target < 0 || target >= accounts.length) return
    await persistOrder(move(accounts, index, target))
  }

  const saveAccountThinking = async (accountId: string, value: ThinkingSettings) => {
    await settingsState.setAccountThinking(accountId, value)
  }

  const resetAccountThinking = async (accountId: string) => {
    await settingsState.resetAccountThinking(accountId)
  }

  return (
    <div className="flex h-screen w-full flex-col overflow-hidden bg-background text-foreground">
      <header className="flex h-14 shrink-0 items-center justify-between border-b border-border/50 px-5">
        <div className="flex items-center gap-3">
          <span className={`h-2 w-2 rounded-full ${settings?.apiEnabled ? 'bg-emerald-500 shadow-[0_0_9px_rgba(16,185,129,.8)]' : 'bg-muted-foreground/50'}`} />
          <span className="text-sm font-semibold">glm5.2proxy</span>
          {settings && <span className="text-xs text-muted-foreground">127.0.0.1:{settings.port}</span>}
        </div>
        <div className="flex items-center gap-1.5">
          <Button variant="ghost" size="sm" onClick={() => setSettingsOpen(true)}>
            <Settings2 className="h-4 w-4" /> Config
          </Button>
          <Button variant="ghost" size="sm" onClick={() => setLogsOpen(true)}>
            <Activity className="h-4 w-4" /> Logs
          </Button>
          <Button variant="ghost" size="sm" onClick={() => setAPISettingsOpen(true)}>
            <KeyRound className="h-4 w-4" /> API
          </Button>
          <ThemeToggle />
        </div>
      </header>

      <main className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto max-w-5xl space-y-6 px-6 py-6">
          {settings && (
          <section className={`flex items-center justify-between rounded-lg border px-4 py-3 ${settings.apiEnabled ? 'border-emerald-500/30 bg-emerald-500/[.045]' : 'border-border/70 bg-card/50'}`}>
              <div className="flex items-center gap-3">
                <div className={`flex h-9 w-9 items-center justify-center rounded-md ${settings.apiEnabled ? 'bg-emerald-500/12 text-emerald-500' : 'bg-muted text-muted-foreground'}`}>
                  <Server className="h-4 w-4" />
                </div>
                <div>
                  <p className="text-sm font-semibold">{settings.apiEnabled ? 'API OpenAI ativa' : 'API OpenAI parada'}</p>
                  <p className="text-xs text-muted-foreground">{apiStatusText}</p>
                </div>
              </div>
              <Button
                variant={settings.apiEnabled ? 'outline' : 'default'}
                onClick={() => settingsState.setAPIEnabled(!settings.apiEnabled)}
              >
                {settings.apiEnabled ? 'Parar API' : 'Iniciar API'}
              </Button>
          </section>
          )}

          <AccountCreatorCard
            status={accountCreator}
          />

          {overviewState.error && (
            <p className="rounded-md border border-red-500/25 bg-red-500/5 px-3 py-2 text-xs text-red-400">
              {overviewState.error}
            </p>
          )}

          <AnimatePresence initial={false}>
            {switchEvent && switchedAccount && (
              <motion.section
                key={switchEvent.timestamp}
                initial={{ opacity: 0, y: 18, scale: 0.985 }}
                animate={{ opacity: 1, y: 0, scale: 1 }}
                exit={{ opacity: 0, y: -12 }}
                transition={{ duration: 0.35, ease: 'easeOut' }}
                className="relative overflow-hidden rounded-2xl border border-sky-400/30 bg-[radial-gradient(circle_at_top_left,_rgba(56,189,248,0.18),_transparent_40%),linear-gradient(135deg,rgba(15,23,42,0.94),rgba(9,14,24,0.98))] p-4 text-slate-50 shadow-[0_18px_55px_rgba(14,165,233,0.16)]"
              >
                <motion.div
                  initial={{ opacity: 0.25, x: '-30%' }}
                  animate={{ opacity: [0.2, 0.5, 0.2], x: ['-30%', '15%', '85%'] }}
                  transition={{ duration: 2.4, repeat: Infinity, ease: 'linear' }}
                  className="pointer-events-none absolute inset-y-0 left-0 w-40 bg-gradient-to-r from-transparent via-sky-300/18 to-transparent"
                />
                <div className="relative flex flex-col gap-4 md:flex-row md:items-center md:justify-between">
                  <div className="min-w-0">
                    <div className="mb-2 inline-flex items-center gap-2 rounded-full border border-sky-300/20 bg-sky-400/10 px-3 py-1 text-[11px] font-semibold uppercase tracking-[0.18em] text-sky-200">
                      <Sparkles className="h-3.5 w-3.5" />
                      Conta trocada
                    </div>
                    <div className="flex flex-wrap items-center gap-2 text-base font-semibold md:text-lg">
                      <span className="truncate">{previousAccount?.label ?? 'Conta anterior'}</span>
                      <ArrowRight className="h-4 w-4 text-sky-300" />
                      <span className="truncate text-sky-200">{switchedAccount.label}</span>
                    </div>
                    <p className="mt-1 text-sm text-slate-300">
                      {switchedAccount.user.email || switchedAccount.user.name || switchedAccount.id}
                    </p>
                  </div>

                  <div className="grid gap-2 sm:grid-cols-2">
                    <div className="rounded-xl border border-white/10 bg-white/5 px-3 py-2">
                      <p className="text-[11px] uppercase tracking-[0.16em] text-slate-400">Momento</p>
                      <p className="mt-1 text-sm font-medium text-slate-100">
                        {new Date(switchEvent.timestamp).toLocaleTimeString('pt-BR')}
                      </p>
                    </div>
                    <div className="rounded-xl border border-white/10 bg-white/5 px-3 py-2">
                      <p className="flex items-center gap-1 text-[11px] uppercase tracking-[0.16em] text-slate-400">
                        <Clock3 className="h-3.5 w-3.5" />
                        Tempo desde a troca
                      </p>
                      <p className="mt-1 text-sm font-medium text-sky-200">
                        {formatElapsed(switchEvent.timestamp, now)}
                      </p>
                    </div>
                  </div>
                </div>
              </motion.section>
            )}
          </AnimatePresence>

          <section>
            <div className="mb-4 flex items-end justify-between gap-4">
              <div>
                <h1 className="text-2xl font-semibold">Fila de contas</h1>
                <p className="mt-1 text-sm text-muted-foreground">
                  {loading
                    ? 'Consultando contas e cotas...'
                    : activeAccount
                      ? `${accounts.length} conta${accounts.length === 1 ? '' : 's'} - ativa agora: ${activeAccount.label}`
                      : `${accounts.length} conta${accounts.length === 1 ? '' : 's'} - arraste pelo puxador para reordenar`}
                </p>
              </div>
              <div className="flex gap-2">
                <Button variant="outline" size="sm" onClick={refreshAccounts} disabled={refreshing || quotaRefreshing}>
                  <RefreshCw className={refreshing || quotaRefreshing ? 'h-4 w-4 animate-spin' : 'h-4 w-4'} />
                  Atualizar cotas
                </Button>
                <Button size="sm" onClick={() => setLoginOpen(true)}>
                  <Plus className="h-4 w-4" /> Adicionar conta
                </Button>
              </div>
            </div>

            {accountsError && (
              <p className="mb-3 rounded-md border border-red-500/25 bg-red-500/5 px-3 py-2 text-xs text-red-400">{accountsError}</p>
            )}

            {accounts.length === 0 && !loading ? (
              <div className="rounded-lg border border-dashed border-border/70 bg-card/30 py-16 text-center">
                <ListRestart className="mx-auto h-9 w-9 text-muted-foreground/50" />
                <p className="mt-3 text-sm font-medium">Nenhuma conta salva</p>
                <p className="mt-1 text-xs text-muted-foreground">Adicione uma conta ZCode para iniciar o pool.</p>
              </div>
            ) : (
              <Reorder.Group
                axis="y"
                values={accounts}
                onReorder={(ordered) => {
                  setAccounts(ordered)
                  dragOrderRef.current = ordered
                }}
                className="space-y-3"
              >
                {accounts.map((account, index) => (
                  <AccountCard
                    key={account.id}
                    account={account}
                    isActive={account.id === activeAccountId}
                    isSwitching={account.id === switchEvent?.toId}
                    isFirst={index === 0}
                    isLast={index === accounts.length - 1}
                    refreshing={refreshing || quotaRefreshing}
                    activatePending={accountAction?.id === account.id && accountAction.type === 'activate'}
                    zcodePending={accountAction?.id === account.id && accountAction.type === 'applyZCode'}
                    deletePending={accountAction?.id === account.id && accountAction.type === 'delete'}
                    actionsDisabled={accountAction !== null}
                    globalThinking={settings?.globalThinking ?? null}
                    accountThinking={settings?.accountThinking?.[account.id] ?? null}
                    onActivate={() => activate(account.id)}
                    onApplyZCode={() => applyAccountInZCode(account.id)}
                    onDelete={() => deleteAccount(account.id)}
                    onMoveUp={() => moveAccount(index, -1)}
                    onMoveDown={() => moveAccount(index, 1)}
                    onRefresh={refreshAccounts}
                    onDragEnd={() => persistOrder(dragOrderRef.current)}
                    onSaveThinking={(value) => saveAccountThinking(account.id, value)}
                    onResetThinking={() => resetAccountThinking(account.id)}
                    zcodeSyncStatus={zcodeSync[account.id]?.status}
                    zcodeSyncMessage={zcodeSync[account.id]?.message}
                  />
                ))}
              </Reorder.Group>
            )}
          </section>
        </div>
      </main>

      <LoginDialog open={loginOpen} onOpenChange={setLoginOpen} onSuccess={() => { void refresh({ cancelInFlight: true }) }} />
      <LogsDialog open={logsOpen} onOpenChange={setLogsOpen} />
      {settings && (
        <SettingsDialog
          open={settingsOpen}
          onOpenChange={setSettingsOpen}
          settings={settings}
          onSaveGlobalThinking={async (value) => {
            await settingsState.setGlobalThinking(value)
          }}
        />
      )}
      {settings && (
        <APISettingsDialog
          open={apiSettingsOpen}
          onOpenChange={setAPISettingsOpen}
          settings={settings}
          onToggleAPI={async (enabled) => { await settingsState.setAPIEnabled(enabled) }}
          onUpdatePort={async (port) => (await settingsState.updatePort(port)).restartRequired}
          onCreateKey={async (name) => (await settingsState.createAPIKey(name)).secret}
          onDeleteKey={settingsState.deleteAPIKey}
        />
      )}
      {showRepairModal && accountRepair && accountRepairNoticeKey && (
        <AuthMigrationModal progress={accountRepair} onClose={() => setDismissedRepairNotice(accountRepairNoticeKey)} />
      )}
      {accountCreator?.busy && (
        <AccountCreationModal status={accountCreator} />
      )}
    </div>
  )
}
