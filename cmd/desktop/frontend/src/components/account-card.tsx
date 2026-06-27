import type { ReactNode } from 'react'
import { useEffect, useMemo, useState } from 'react'
import { motion, Reorder, useDragControls } from 'framer-motion'
import {
  ArrowDown,
  ArrowUp,
  BrainCircuit,
  Check,
  CircleAlert,
  CircleCheck,
  Loader2,
  Gauge,
  GripVertical,
  RefreshCw,
  RotateCcw,
  Save,
  SquareCode,
  Trash2,
  UserRound,
  X,
} from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Progress } from '@/components/ui/progress'
import { Switch } from '@/components/ui/switch'
import { cn } from '@/lib/utils'
import type { Account, QuotaBalance, ThinkingEffort, ThinkingSettings } from '@/types/api'

interface AccountCardProps {
  account: Account
  isActive: boolean
  isSwitching: boolean
  isFirst: boolean
  isLast: boolean
  refreshing: boolean
  activatePending: boolean
  zcodePending: boolean
  deletePending: boolean
  actionsDisabled: boolean
  globalThinking: ThinkingSettings | null
  accountThinking: ThinkingSettings | null
  onActivate: () => Promise<void>
  onApplyZCode: () => Promise<void>
  onDelete: () => Promise<void>
  onMoveUp: () => void
  onMoveDown: () => void
  onRefresh: () => void
  onDragEnd: () => void
  onSaveThinking: (value: ThinkingSettings) => Promise<void>
  onResetThinking: () => Promise<void>
  zcodeSyncStatus?: 'idle' | 'syncing' | 'synced' | 'skipped' | 'error'
  zcodeSyncMessage?: string | null
}

interface HoverHintProps {
  content: string
  children: ReactNode
}

const DEFAULT_THINKING: ThinkingSettings = {
  enabled: true,
  budgetTokens: 32000,
  effort: 'max',
}

const EFFORT_OPTIONS: Array<{ value: ThinkingEffort; label: string; hint: string }> = [
  { value: 'none', label: 'Off', hint: 'Sem pensamento' },
  { value: 'low', label: 'Leve', hint: 'Mais rapido' },
  { value: 'medium', label: 'Medio', hint: 'Equilibrado' },
  { value: 'high', label: 'Alto', hint: 'Mais cuidadoso' },
  { value: 'max', label: 'Max', hint: 'Maior budget' },
]

const BUDGET_PRESETS = [8000, 16000, 32000, 48000, 64000]

function formatTokens(value: number | null): string {
  if (value == null) return '--'
  return new Intl.NumberFormat('pt-BR', { notation: 'compact', maximumFractionDigits: 1 }).format(value)
}

function quotaColor(percent: number): string {
  if (percent >= 90) return 'bg-red-500'
  if (percent >= 70) return 'bg-amber-400'
  return 'bg-emerald-500'
}

function clampBudget(value: number): number {
  if (!Number.isFinite(value)) return DEFAULT_THINKING.budgetTokens
  return Math.max(0, Math.min(64000, Math.round(value)))
}

function effortLabel(value: ThinkingEffort): string {
  return EFFORT_OPTIONS.find((option) => option.value === value)?.label ?? value
}

function HoverHint({ content, children }: HoverHintProps) {
  return (
    <div className="group/tooltip relative">
      {children}
      <div
        className={cn(
          'pointer-events-none absolute bottom-full right-0 z-30 mb-2 w-72 rounded-md border border-border/70 bg-popover px-3 py-2 text-[11px] leading-relaxed text-popover-foreground shadow-xl',
          'opacity-0 translate-y-1 transition-all duration-150',
          'group-hover/tooltip:translate-y-0 group-hover/tooltip:opacity-100',
          'group-focus-within/tooltip:translate-y-0 group-focus-within/tooltip:opacity-100'
        )}
      >
        {content}
      </div>
    </div>
  )
}

function ModelQuota({ balance }: { balance: QuotaBalance }) {
  const percent = Math.max(0, Math.min(100, balance.usagePercent ?? 0))
  return (
    <div className="rounded-md border border-border/50 bg-background/35 px-3 py-2.5">
      <div className="mb-2 flex items-center justify-between gap-3">
        <div>
          <p className="text-xs font-semibold">{balance.model}</p>
          <p className="text-[11px] text-muted-foreground">
            {formatTokens(balance.used)} usados de {formatTokens(balance.total)}
          </p>
        </div>
        <span className="text-xs font-semibold tabular-nums">{Math.round(percent)}%</span>
      </div>
      <Progress value={percent} indicatorClassName={quotaColor(percent)} className="h-1.5" />
      <div className="mt-1.5 flex justify-between text-[10px] text-muted-foreground">
        <span>{formatTokens(balance.available)} disponiveis</span>
        <span>{formatTokens(balance.remaining)} restantes</span>
      </div>
    </div>
  )
}

interface ThinkingControlProps {
  globalThinking: ThinkingSettings | null
  accountThinking: ThinkingSettings | null
  onSave: (value: ThinkingSettings) => Promise<void>
  onReset: () => Promise<void>
}

function ThinkingControl({ globalThinking, accountThinking, onSave, onReset }: ThinkingControlProps) {
  const inherited = !accountThinking
  const effective = useMemo(
    () => accountThinking ?? globalThinking ?? DEFAULT_THINKING,
    [accountThinking, globalThinking]
  )
  const [draft, setDraft] = useState<ThinkingSettings>(effective)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    setDraft(effective)
    setError(null)
  }, [effective.enabled, effective.budgetTokens, effective.effort])

  const dirty =
    draft.enabled !== effective.enabled ||
    draft.budgetTokens !== effective.budgetTokens ||
    draft.effort !== effective.effort

  const save = async () => {
    setSaving(true)
    setError(null)
    try {
      await onSave({ ...draft, budgetTokens: clampBudget(draft.budgetTokens) })
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Nao foi possivel salvar')
    } finally {
      setSaving(false)
    }
  }

  const reset = async () => {
    setSaving(true)
    setError(null)
    try {
      await onReset()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Nao foi possivel resetar')
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="mt-4 rounded-md border border-border/60 bg-background/35 p-3">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex min-w-0 items-center gap-2">
          <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-foreground/7 text-foreground">
            <BrainCircuit className="h-4 w-4" />
          </span>
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2">
              <p className="text-xs font-semibold">Esforco de raciocinio</p>
              <span
                className={cn(
                  'rounded px-1.5 py-0.5 text-[10px] font-semibold',
                  inherited
                    ? 'bg-muted text-muted-foreground'
                    : 'bg-emerald-500/12 text-emerald-500'
                )}
              >
                {inherited ? 'Herdando global' : 'Personalizado'}
              </span>
            </div>
            <p className="truncate text-[11px] text-muted-foreground">
              {draft.enabled
                ? `${effortLabel(draft.effort)} - budget ${formatTokens(draft.budgetTokens)} tokens`
                : 'Pensamento desativado para esta conta'}
            </p>
          </div>
        </div>

        <div className="flex items-center gap-2">
          <span className="text-[11px] font-medium text-muted-foreground">Ativo</span>
          <Switch
            checked={draft.enabled}
            onCheckedChange={(enabled) =>
              setDraft((current) => ({
                ...current,
                enabled,
                effort: enabled && current.effort === 'none' ? 'medium' : current.effort,
              }))
            }
          />
        </div>
      </div>

      <div className="mt-3 grid gap-3 lg:grid-cols-[1.2fr_.8fr_auto] lg:items-end">
        <div>
          <div className="mb-1.5 flex items-center gap-1.5 text-[11px] font-medium text-muted-foreground">
            <Gauge className="h-3.5 w-3.5" />
            Nivel
          </div>
          <div className="grid grid-cols-5 gap-1">
            {EFFORT_OPTIONS.map((option) => (
              <button
                key={option.value}
                type="button"
                title={option.hint}
                onClick={() =>
                  setDraft((current) => ({
                    ...current,
                    effort: option.value,
                    enabled: option.value === 'none' ? false : true,
                    budgetTokens: option.value === 'none' ? 0 : current.budgetTokens || DEFAULT_THINKING.budgetTokens,
                  }))
                }
                className={cn(
                  'rounded-md border px-2 py-1.5 text-[11px] font-semibold transition-colors',
                  draft.effort === option.value
                    ? 'border-emerald-500/60 bg-emerald-500/12 text-emerald-500'
                    : 'border-border/60 bg-card/40 text-muted-foreground hover:bg-accent hover:text-foreground'
                )}
              >
                {option.label}
              </button>
            ))}
          </div>
        </div>

        <div>
          <label className="mb-1.5 block text-[11px] font-medium text-muted-foreground">
            Budget tokens
          </label>
          <input
            type="number"
            min={0}
            max={64000}
            step={1000}
            value={draft.budgetTokens}
            onChange={(event) =>
              setDraft((current) => ({ ...current, budgetTokens: clampBudget(Number(event.target.value)) }))
            }
            className="h-8 w-full rounded-md border border-border/70 bg-background px-2 text-xs outline-none transition-colors focus:border-emerald-500/70"
          />
          <div className="mt-1.5 flex flex-wrap gap-1">
            {BUDGET_PRESETS.map((value) => (
              <button
                key={value}
                type="button"
                onClick={() => setDraft((current) => ({ ...current, enabled: true, budgetTokens: value }))}
                className={cn(
                  'rounded border px-1.5 py-0.5 text-[10px] transition-colors',
                  draft.budgetTokens === value
                    ? 'border-emerald-500/60 bg-emerald-500/12 text-emerald-500'
                    : 'border-border/50 text-muted-foreground hover:text-foreground'
                )}
              >
                {formatTokens(value)}
              </button>
            ))}
          </div>
        </div>

        <div className="flex gap-2 lg:justify-end">
          <Button variant="outline" size="sm" onClick={reset} disabled={saving || inherited}>
            <RotateCcw className="h-3.5 w-3.5" />
            Herdar
          </Button>
          <Button size="sm" onClick={save} disabled={saving || !dirty}>
            <Save className="h-3.5 w-3.5" />
            Salvar
          </Button>
        </div>
      </div>

      {error && (
        <p className="mt-2 rounded border border-red-500/25 bg-red-500/5 px-2 py-1.5 text-[11px] text-red-400">
          {error}
        </p>
      )}
    </div>
  )
}

export function AccountCard({
  account,
  isActive,
  isSwitching,
  isFirst,
  isLast,
  refreshing,
  activatePending,
  zcodePending,
  deletePending,
  actionsDisabled,
  globalThinking,
  accountThinking,
  onActivate,
  onApplyZCode,
  onDelete,
  onMoveUp,
  onMoveDown,
  onRefresh,
  onDragEnd,
  onSaveThinking,
  onResetThinking,
  zcodeSyncStatus = 'idle',
  zcodeSyncMessage = null,
}: AccountCardProps) {
  const dragControls = useDragControls()
  const displayName = account.user.name || account.label
  const email = account.user.email || account.user.id || account.id
  const [zcodeMessage, setZCodeMessage] = useState<string | null>(null)
  const [deleteConfirming, setDeleteConfirming] = useState(false)
  const [deleteError, setDeleteError] = useState<string | null>(null)
  const effectiveZCodeStatus = zcodePending ? 'syncing' : zcodeSyncStatus
  const effectiveZCodeMessage = zcodeMessage ?? zcodeSyncMessage
  const activateTooltip = isActive
    ? 'Esta ja e a conta ativa do proxy.'
    : 'Troca somente a conta ativa do proxy e da API OpenAI-compatible. Nao mexe no ZCode.'
  const applyZCodeTooltip = account.hasZcodeJwtToken
    ? 'So grava esta conta dentro do app ZCode detectado. Nao muda a conta ativa do proxy.'
    : 'Essa conta nao tem JWT salvo. Faca login novamente nela para conseguir aplicar no ZCode.'
  const deleteTooltip = isActive
    ? 'Remove esta conta do proxy. Se ela estiver ativa, o proxy escolhe a proxima conta salva sem mexer no ZCode.'
    : 'Remove esta conta salva do proxy. Isso nao apaga a conta na Z.ai.'

  useEffect(() => {
    setDeleteConfirming(false)
    setDeleteError(null)
  }, [account.id])

  const activateAccount = async () => {
    if (isActive || activatePending || actionsDisabled) return
    setZCodeMessage(null)
    try {
      await onActivate()
    } catch (err) {
      setZCodeMessage(err instanceof Error ? err.message : 'Nao foi possivel ativar conta')
    }
  }

  const applyZCode = async () => {
    if (!account.hasZcodeJwtToken) {
      setZCodeMessage('Essa conta antiga nao tem JWT salvo; faca login novamente nela para conseguir migrar para o ZCode.')
      return
    }
    setZCodeMessage(null)
    try {
      await onApplyZCode()
      setZCodeMessage(null)
    } catch (err) {
      setZCodeMessage(err instanceof Error ? err.message : 'Nao foi possivel aplicar no ZCode')
    }
  }

  const deleteAccount = async () => {
    if (deletePending) return
    setDeleteError(null)
    try {
      await onDelete()
    } catch (err) {
      setDeleteError(err instanceof Error ? err.message : 'Nao foi possivel apagar a conta')
    }
  }

  return (
    <Reorder.Item
      value={account}
      dragListener={false}
      dragControls={dragControls}
      onDragEnd={onDragEnd}
      layout
      whileDrag={{
        scale: 1.02,
        zIndex: 30,
        boxShadow: '0 22px 60px rgba(0,0,0,.32)',
      }}
      transition={{ type: 'spring', stiffness: 360, damping: 32 }}
      className="list-none"
    >
      <motion.article
        layout
        animate={
          isSwitching
            ? {
                scale: [1, 1.018, 1],
                boxShadow: [
                  '0 8px 28px rgba(16,185,129,.08)',
                  '0 0 0 1px rgba(16,185,129,.45), 0 24px 64px rgba(16,185,129,.22)',
                  '0 8px 28px rgba(16,185,129,.08)',
                ],
              }
            : undefined
        }
        transition={{ duration: 1.2, ease: 'easeOut' }}
        className={cn(
          'group relative overflow-hidden rounded-lg border bg-card/75',
          'transition-colors duration-200',
          isActive ? 'border-emerald-500/60 shadow-[0_8px_28px_rgba(16,185,129,.08)]' : 'border-border/70',
        )}
      >
        {isSwitching && (
          <motion.div
            initial={{ opacity: 0, x: '-100%' }}
            animate={{ opacity: [0, 0.8, 0], x: ['-100%', '15%', '100%'] }}
            transition={{ duration: 1.4, ease: 'easeInOut' }}
            className="pointer-events-none absolute inset-y-0 left-0 w-40 bg-gradient-to-r from-transparent via-emerald-400/18 to-transparent"
          />
        )}
        <div className="flex items-stretch">
          <button
            type="button"
            aria-label={`Arrastar ${account.label}`}
            title="Segure e arraste para reorganizar"
            onPointerDown={(event) => dragControls.start(event)}
            className="flex w-10 shrink-0 cursor-grab touch-none items-center justify-center border-r border-border/50 text-muted-foreground transition-colors hover:bg-accent hover:text-foreground active:cursor-grabbing"
          >
            <GripVertical className="h-4 w-4" />
          </button>

          <div className="min-w-0 flex-1 p-4">
            <div className="flex items-start justify-between gap-4">
              <div className="flex min-w-0 items-center gap-3">
                <div className="relative h-10 w-10 shrink-0 overflow-hidden rounded-full border border-border/70 bg-muted">
                  {account.user.avatar ? (
                    <img src={account.user.avatar} alt="" className="h-full w-full object-cover" />
                  ) : (
                    <UserRound className="m-2.5 h-5 w-5 text-muted-foreground" />
                  )}
                </div>
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    <h3 className="truncate text-sm font-semibold">{displayName}</h3>
                    <span className="rounded bg-muted px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground">
                      {account.label}
                    </span>
                    {isActive && (
                      <span className="inline-flex items-center gap-1 rounded bg-emerald-500/12 px-1.5 py-0.5 text-[10px] font-semibold text-emerald-500">
                        <Check className="h-3 w-3" /> Em uso
                      </span>
                    )}
                    {isSwitching && (
                      <span className="inline-flex items-center gap-1 rounded bg-sky-500/12 px-1.5 py-0.5 text-[10px] font-semibold text-sky-400">
                        Conta trocada agora
                      </span>
                    )}
                  </div>
                  <p className="truncate text-xs text-muted-foreground">{email}</p>
                </div>
              </div>

              <div className="flex shrink-0 items-center gap-1">
                <Button variant="ghost" size="icon" title="Atualizar cota" onClick={onRefresh}>
                  <RefreshCw className={cn('h-4 w-4', refreshing && 'animate-spin')} />
                </Button>
                <Button variant="ghost" size="icon" title="Mover para cima" disabled={isFirst} onClick={onMoveUp}>
                  <ArrowUp className="h-4 w-4" />
                </Button>
                <Button variant="ghost" size="icon" title="Mover para baixo" disabled={isLast} onClick={onMoveDown}>
                  <ArrowDown className="h-4 w-4" />
                </Button>
                {deleteConfirming ? (
                  <div className="inline-flex items-center gap-1 rounded-md border border-red-500/30 bg-red-500/7 p-1">
                    <Button
                      variant="destructive"
                      size="sm"
                      disabled={deletePending}
                      onClick={() => { void deleteAccount() }}
                    >
                      {deletePending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Trash2 className="h-3.5 w-3.5" />}
                      Confirmar
                    </Button>
                    <Button
                      variant="ghost"
                      size="icon"
                      className="h-8 w-8 text-muted-foreground hover:text-foreground"
                      disabled={deletePending}
                      onClick={() => {
                        setDeleteConfirming(false)
                        setDeleteError(null)
                      }}
                    >
                      <X className="h-4 w-4" />
                    </Button>
                  </div>
                ) : (
                  <HoverHint content={deleteTooltip}>
                    <span className="inline-flex">
                      <Button
                        variant="ghost"
                        size="icon"
                        className="text-muted-foreground hover:bg-red-500/10 hover:text-red-400"
                        title="Apagar conta"
                        disabled={actionsDisabled}
                        onClick={() => setDeleteConfirming(true)}
                      >
                        <Trash2 className="h-4 w-4" />
                      </Button>
                    </span>
                  </HoverHint>
                )}
                <HoverHint content={activateTooltip}>
                  <span className="inline-flex">
                    <Button
                      variant={isActive ? 'secondary' : 'outline'}
                      size="sm"
                      disabled={isActive || activatePending || actionsDisabled}
                      onClick={() => { void activateAccount() }}
                    >
                      {activatePending ? 'Ativando...' : isActive ? 'Conta ativa' : 'Usar agora'}
                    </Button>
                  </span>
                </HoverHint>
                <HoverHint content={applyZCodeTooltip}>
                  <span className="inline-flex">
                    <Button
                      variant="outline"
                      size="sm"
                      disabled={zcodePending || actionsDisabled || !account.hasZcodeJwtToken}
                      onClick={() => { void applyZCode() }}
                    >
                      <SquareCode className="h-3.5 w-3.5" />
                      {zcodePending ? 'Aplicando...' : 'Aplicar no ZCode'}
                    </Button>
                  </span>
                </HoverHint>
              </div>
            </div>

            {zcodeMessage && (
              <p className="mt-3 rounded-md border border-sky-500/25 bg-sky-500/5 px-3 py-2 text-[11px] text-sky-300">
                {zcodeMessage}
              </p>
            )}
            {deleteError && (
              <p className="mt-3 rounded-md border border-red-500/25 bg-red-500/5 px-3 py-2 text-[11px] text-red-400">
                {deleteError}
              </p>
            )}
            <div
              className={cn(
                'mt-3 flex flex-wrap items-center gap-2 rounded-md border px-3 py-2 text-[11px]',
                effectiveZCodeStatus === 'synced' && 'border-emerald-500/30 bg-emerald-500/7 text-emerald-300',
                effectiveZCodeStatus === 'syncing' && 'border-sky-500/30 bg-sky-500/7 text-sky-300',
                effectiveZCodeStatus === 'error' && 'border-red-500/30 bg-red-500/7 text-red-300',
                effectiveZCodeStatus === 'skipped' && 'border-amber-500/30 bg-amber-500/7 text-amber-300',
                effectiveZCodeStatus === 'idle' && 'border-border/60 bg-background/35 text-muted-foreground'
              )}
            >
              {effectiveZCodeStatus === 'syncing' ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
              ) : effectiveZCodeStatus === 'synced' ? (
                <CircleCheck className="h-3.5 w-3.5" />
              ) : effectiveZCodeStatus === 'error' || effectiveZCodeStatus === 'skipped' ? (
                <CircleAlert className="h-3.5 w-3.5" />
              ) : (
                <SquareCode className="h-3.5 w-3.5" />
              )}
              <span className="font-semibold">ZCode</span>
              <span>
                {effectiveZCodeMessage ??
                  (account.hasZcodeJwtToken
                    ? 'Use Aplicar no ZCode para gravar esta conta no app ZCode detectado.'
                    : 'Conta sem JWT salvo; nao da para aplicar no ZCode sem fazer login novamente.')}
              </span>
            </div>

            <ThinkingControl
              globalThinking={globalThinking}
              accountThinking={accountThinking}
              onSave={onSaveThinking}
              onReset={onResetThinking}
            />

            {account.quota ? (
              <div className="mt-4">
                <div className="grid gap-2 sm:grid-cols-2">
                  {account.quota.balances.map((balance) => (
                    <ModelQuota key={balance.id || balance.model} balance={balance} />
                  ))}
                </div>
                {account.quotaLoading && (
                  <p className="mt-2 text-[11px] text-muted-foreground">Atualizando cota...</p>
                )}
              </div>
            ) : account.quotaLoading ? (
              <div className="mt-4 h-[74px] animate-pulse rounded-md bg-muted/60" />
            ) : account.quotaError ? (
              <div className="mt-4 rounded-md border border-red-500/25 bg-red-500/5 px-3 py-2 text-xs text-red-400">
                Nao foi possivel consultar a cota: {account.quotaError.message}
              </div>
            ) : account.quotaSkipped ? (
              <div className="mt-4 rounded-md border border-border/60 bg-background/35 px-3 py-2 text-xs text-muted-foreground">
                Cota nao carregada nesta atualizacao.
              </div>
            ) : (
              <div className="mt-4 h-[74px] animate-pulse rounded-md bg-muted/60" />
            )}
          </div>
        </div>
      </motion.article>
    </Reorder.Item>
  )
}
