import { useEffect, useState } from 'react'
import { BrainCircuit, Gauge, Save, Settings2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Switch } from '@/components/ui/switch'
import { cn } from '@/lib/utils'
import type { Settings, ThinkingEffort, ThinkingSettings } from '@/types/api'

interface SettingsDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  settings: Settings
  onSaveGlobalThinking: (value: ThinkingSettings) => Promise<void>
}

const EFFORT_OPTIONS: Array<{ value: ThinkingEffort; label: string; hint: string }> = [
  { value: 'none', label: 'Off', hint: 'Sem pensamento' },
  { value: 'low', label: 'Leve', hint: 'Mais rapido' },
  { value: 'medium', label: 'Medio', hint: 'Equilibrado' },
  { value: 'high', label: 'Alto', hint: 'Mais cuidadoso' },
  { value: 'max', label: 'Max', hint: 'Maior budget' },
]

const BUDGET_PRESETS = [8000, 16000, 32000, 48000, 64000]

function clampBudget(value: number): number {
  if (!Number.isFinite(value)) return 32000
  return Math.max(0, Math.min(64000, Math.round(value)))
}

function formatTokens(value: number): string {
  return new Intl.NumberFormat('pt-BR', { notation: 'compact', maximumFractionDigits: 1 }).format(value)
}

function effortLabel(value: ThinkingEffort): string {
  return EFFORT_OPTIONS.find((option) => option.value === value)?.label ?? value
}

export function SettingsDialog({ open, onOpenChange, settings, onSaveGlobalThinking }: SettingsDialogProps) {
  const [draft, setDraft] = useState<ThinkingSettings>(settings.globalThinking)
  const [busy, setBusy] = useState(false)
  const [notice, setNotice] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    setDraft(settings.globalThinking)
    setNotice(null)
    setError(null)
  }, [settings.globalThinking, open])

  const dirty =
    draft.enabled !== settings.globalThinking.enabled ||
    draft.budgetTokens !== settings.globalThinking.budgetTokens ||
    draft.effort !== settings.globalThinking.effort

  const save = async () => {
    setBusy(true)
    setNotice(null)
    setError(null)
    try {
      await onSaveGlobalThinking({ ...draft, budgetTokens: clampBudget(draft.budgetTokens) })
      setNotice('Configuracao global salva.')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Nao foi possivel salvar a configuracao.')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl border-white/10 bg-card/78 backdrop-blur-2xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Settings2 className="h-5 w-5" />
            Configuracoes
          </DialogTitle>
          <DialogDescription>
            Ajuste o pensamento global usado como padrao para todas as contas que estiverem herdando essa configuracao.
          </DialogDescription>
        </DialogHeader>

        <section className="rounded-2xl border border-border/60 bg-background/35 p-4">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div className="flex min-w-0 items-center gap-3">
              <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-foreground/7 text-foreground">
                <BrainCircuit className="h-5 w-5" />
              </div>
              <div className="min-w-0">
                <p className="text-sm font-semibold">Pensamento global</p>
                <p className="truncate text-xs text-muted-foreground">
                  {draft.enabled
                    ? `${effortLabel(draft.effort)} - budget ${formatTokens(draft.budgetTokens)} tokens`
                    : 'Pensamento desativado globalmente'}
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

          <div className="mt-4 grid gap-4 md:grid-cols-[1.2fr_.8fr]">
            <div>
              <div className="mb-2 flex items-center gap-1.5 text-[11px] font-medium text-muted-foreground">
                <Gauge className="h-3.5 w-3.5" />
                Nivel
              </div>
              <div className="grid grid-cols-5 gap-1.5">
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
                        budgetTokens: option.value === 'none' ? 0 : current.budgetTokens || 32000,
                      }))
                    }
                    className={cn(
                      'rounded-lg border px-2 py-2 text-[11px] font-semibold transition-colors',
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
              <label className="mb-2 block text-[11px] font-medium text-muted-foreground">Budget tokens</label>
              <input
                type="number"
                min={0}
                max={64000}
                step={1000}
                value={draft.budgetTokens}
                onChange={(event) =>
                  setDraft((current) => ({ ...current, budgetTokens: clampBudget(Number(event.target.value)) }))
                }
                className="h-10 w-full rounded-lg border border-border/70 bg-background px-3 text-sm outline-none transition-colors focus:border-emerald-500/70"
              />
              <div className="mt-2 flex flex-wrap gap-1.5">
                {BUDGET_PRESETS.map((value) => (
                  <button
                    key={value}
                    type="button"
                    onClick={() => setDraft((current) => ({ ...current, enabled: true, budgetTokens: value }))}
                    className={cn(
                      'rounded border px-2 py-1 text-[10px] transition-colors',
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
          </div>

          <div className="mt-4 flex justify-end">
            <Button onClick={save} disabled={busy || !dirty}>
              <Save className="h-4 w-4" />
              Salvar
            </Button>
          </div>
        </section>

        {notice && <p className="rounded-md border border-emerald-500/25 bg-emerald-500/5 px-3 py-2 text-xs text-emerald-400">{notice}</p>}
        {error && <p className="rounded-md border border-red-500/25 bg-red-500/5 px-3 py-2 text-xs text-red-400">{error}</p>}
      </DialogContent>
    </Dialog>
  )
}
