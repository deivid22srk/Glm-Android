import { useMemo, useState } from 'react'
import { Bot, ChevronRight, Clock3, FolderCog, Info, RefreshCw, ShieldCheck, Sparkles } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import type { AccountCreatorStatus } from '@/types/api'

interface AccountCreatorCardProps {
  status: AccountCreatorStatus | null | undefined
}

function formatDateTime(value?: string): string | null {
  if (!value) return null
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return null
  return date.toLocaleString('pt-BR')
}

function formatCooldown(ms: number): string | null {
  if (ms <= 0) return null
  const seconds = Math.ceil(ms / 1000)
  if (seconds < 60) return `${seconds}s`
  const minutes = Math.floor(seconds / 60)
  const remainder = seconds % 60
  return remainder > 0 ? `${minutes}m ${remainder}s` : `${minutes}m`
}

function CreatorFlowArt() {
  return (
    <svg viewBox="0 0 760 220" className="h-full w-full" role="img" aria-label="Fluxo da automacao de contas">
      <defs>
        <linearGradient id="creator-card-line" x1="0%" y1="0%" x2="100%" y2="0%">
          <stop offset="0%" stopColor="rgba(16,185,129,0.28)" />
          <stop offset="50%" stopColor="rgba(59,130,246,0.38)" />
          <stop offset="100%" stopColor="rgba(16,185,129,0.28)" />
        </linearGradient>
        <linearGradient id="creator-card-box" x1="0%" y1="0%" x2="100%" y2="100%">
          <stop offset="0%" stopColor="rgba(15,23,42,0.96)" />
          <stop offset="100%" stopColor="rgba(17,24,39,0.82)" />
        </linearGradient>
      </defs>

      <rect x="28" y="56" width="180" height="104" rx="18" fill="url(#creator-card-box)" stroke="rgba(148,163,184,0.22)" />
      <rect x="290" y="40" width="180" height="136" rx="18" fill="url(#creator-card-box)" stroke="rgba(59,130,246,0.28)" />
      <rect x="552" y="56" width="180" height="104" rx="18" fill="url(#creator-card-box)" stroke="rgba(16,185,129,0.24)" />

      <line x1="208" y1="108" x2="290" y2="108" stroke="url(#creator-card-line)" strokeWidth="4" strokeLinecap="round" />
      <line x1="470" y1="108" x2="552" y2="108" stroke="url(#creator-card-line)" strokeWidth="4" strokeLinecap="round" />

      <circle cx="250" cy="108" r="7" fill="rgba(59,130,246,0.9)" />
      <circle cx="512" cy="108" r="7" fill="rgba(16,185,129,0.9)" />

      <text x="52" y="90" fill="rgba(226,232,240,0.95)" fontSize="18" fontWeight="700">Request chega</text>
      <text x="52" y="118" fill="rgba(148,163,184,0.92)" fontSize="14">Proxy mede tokens reais</text>
      <text x="52" y="140" fill="rgba(148,163,184,0.92)" fontSize="14">e tenta a melhor conta livre</text>

      <text x="314" y="78" fill="rgba(191,219,254,0.98)" fontSize="18" fontWeight="700">Sem cota suficiente</text>
      <text x="314" y="106" fill="rgba(148,163,184,0.92)" fontSize="14">Cria conta, resolve captcha,</text>
      <text x="314" y="128" fill="rgba(148,163,184,0.92)" fontSize="14">confirma email, vincula ao pool</text>
      <text x="314" y="150" fill="rgba(148,163,184,0.92)" fontSize="14">e atualiza Coding Plan / billing</text>

      <text x="576" y="90" fill="rgba(220,252,231,0.98)" fontSize="18" fontWeight="700">Pool volta pronto</text>
      <text x="576" y="118" fill="rgba(148,163,184,0.92)" fontSize="14">Conta nova entra na fila</text>
      <text x="576" y="140" fill="rgba(148,163,184,0.92)" fontSize="14">e a request tenta de novo</text>
    </svg>
  )
}

export function AccountCreatorCard({ status }: AccountCreatorCardProps) {
  const [open, setOpen] = useState(false)

  const modeLabel = useMemo(() => {
    if (!status) return 'Indisponivel'
    return status.mode === 'external' ? 'Pasta externa' : 'Embutido no app'
  }, [status])

  const lastRunText = formatDateTime(status?.lastRunAt)
  const cooldownText = formatCooldown(status?.cooldownRemainingMs ?? 0)
  const lastResultSummary = useMemo(() => {
    const result = status?.lastResult
    if (!result) return null
    const parts = [result.label, result.email, result.username].filter(Boolean)
    if (parts.length === 0) return result.duration ? `Ultima execucao em ${result.duration}` : 'Ultima execucao concluida'
    return parts.join(' · ')
  }, [status?.lastResult])

  const stateLabel = !status?.enabled
    ? 'Desativado'
    : status.busy
      ? 'Rodando agora'
      : 'Automatico'

  const stateTone = !status?.enabled
    ? 'border-white/10 bg-white/5 text-muted-foreground'
    : status.busy
      ? 'border-amber-500/30 bg-amber-500/10 text-amber-200'
      : 'border-emerald-500/30 bg-emerald-500/10 text-emerald-200'

  return (
    <>
      <section className="rounded-lg border border-border/70 bg-card/40 p-4">
        <div className="flex flex-col gap-4 md:flex-row md:items-start md:justify-between">
          <div className="min-w-0">
            <div className="flex items-center gap-3">
              <div className="flex h-10 w-10 items-center justify-center rounded-md bg-primary/10 text-primary">
                <Bot className="h-4 w-4" />
              </div>
              <div>
                <div className="flex flex-wrap items-center gap-2">
                  <p className="text-sm font-semibold">Criacao automatizada de contas</p>
                  <span className={`inline-flex items-center rounded-full border px-2 py-0.5 text-[10px] font-semibold uppercase tracking-[0.14em] ${stateTone}`}>
                    {stateLabel}
                  </span>
                </div>
                <p className="text-xs text-muted-foreground">
                  O gatilho e automatico. O painel agora so monitora e explica o fluxo.
                </p>
              </div>
            </div>

            <div className="mt-4 grid gap-2 md:grid-cols-2">
              <div className="rounded-md border border-border/70 bg-background/40 px-3 py-2">
                <p className="text-[11px] uppercase tracking-[0.12em] text-muted-foreground">Modo</p>
                <p className="mt-1 text-sm font-medium">{modeLabel}</p>
              </div>
              <div className="rounded-md border border-border/70 bg-background/40 px-3 py-2">
                <p className="text-[11px] uppercase tracking-[0.12em] text-muted-foreground">Cooldown</p>
                <p className="mt-1 text-sm font-medium">{cooldownText ?? 'Livre para disparo automatico'}</p>
              </div>
            </div>
          </div>

          <div className="flex shrink-0 flex-col items-stretch gap-2 md:w-60">
            <Button variant="outline" onClick={() => setOpen(true)}>
              <Info className="h-4 w-4" />
              Entender a automacao
            </Button>
            <div className="rounded-md border border-border/70 bg-background/40 px-3 py-2">
              <p className="flex items-center gap-1 text-[11px] uppercase tracking-[0.12em] text-muted-foreground">
                <FolderCog className="h-3.5 w-3.5" />
                Diretorio ativo
              </p>
              <p className="mt-1 truncate font-mono text-[11px] text-muted-foreground" title={status?.workDir}>
                {status?.workDir ?? 'Nao definido'}
              </p>
            </div>
          </div>
        </div>

        <div className="mt-4 grid gap-2 lg:grid-cols-3">
          <div className="rounded-md border border-border/70 bg-background/40 px-3 py-2">
            <p className="flex items-center gap-1 text-[11px] uppercase tracking-[0.12em] text-muted-foreground">
              <Clock3 className="h-3.5 w-3.5" />
              Ultima execucao
            </p>
            <p className="mt-1 text-sm font-medium">{lastRunText ?? 'Ainda nao executou neste runtime'}</p>
          </div>
          <div className="rounded-md border border-border/70 bg-background/40 px-3 py-2">
            <p className="flex items-center gap-1 text-[11px] uppercase tracking-[0.12em] text-muted-foreground">
              <RefreshCw className="h-3.5 w-3.5" />
              Ultimo resultado
            </p>
            <p className="mt-1 truncate text-sm font-medium" title={lastResultSummary ?? status?.lastError}>
              {lastResultSummary ?? status?.lastError ?? 'Sem historico ainda'}
            </p>
          </div>
          <div className="rounded-md border border-border/70 bg-background/40 px-3 py-2">
            <p className="flex items-center gap-1 text-[11px] uppercase tracking-[0.12em] text-muted-foreground">
              <ShieldCheck className="h-3.5 w-3.5" />
              Regra de disparo
            </p>
            <p className="mt-1 text-sm font-medium">
              Quando nenhuma conta consegue atender a cota pedida.
            </p>
          </div>
        </div>

        <div className="mt-4 rounded-lg border border-sky-400/20 bg-sky-500/[0.045] p-3">
          <p className="text-sm font-medium text-sky-100">Sem botao de criacao manual</p>
          <p className="mt-1 text-xs leading-5 text-sky-200/80">
            Se todas as contas falharem, ou se a request pedir mais tokens do que qualquer conta disponivel tem no momento,
            o proxy cria uma conta nova, passa pelo captcha, vincula ao pool e reprocessa a selecao automaticamente.
          </p>
        </div>
      </section>

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="max-h-[90vh] max-w-4xl overflow-y-auto border-white/10 bg-card/88 p-0 backdrop-blur-2xl">
          <div className="border-b border-white/10 bg-[radial-gradient(circle_at_top_left,_rgba(16,185,129,0.16),_transparent_36%),radial-gradient(circle_at_top_right,_rgba(59,130,246,0.14),_transparent_34%),linear-gradient(180deg,rgba(15,23,42,0.98),rgba(8,12,20,0.92))] px-6 pb-5 pt-6">
            <DialogHeader className="space-y-3 text-left">
              <div className="inline-flex w-fit items-center gap-2 rounded-full border border-emerald-400/20 bg-emerald-500/10 px-3 py-1 text-[11px] font-semibold uppercase tracking-[0.18em] text-emerald-200">
                <Sparkles className="h-3.5 w-3.5" />
                Automacao embutida no app
              </div>
              <DialogTitle className="text-2xl text-white">
                Como a criacao automatizada de contas entra em acao
              </DialogTitle>
              <DialogDescription className="max-w-2xl text-sm leading-6 text-slate-300">
                O fluxo nao fica esperando clique no painel. Ele so entra quando a fila real de contas nao consegue
                sustentar a request que chegou para a API.
              </DialogDescription>
            </DialogHeader>

            <div className="mt-5 h-[220px] rounded-2xl border border-white/10 bg-black/20 p-4">
              <CreatorFlowArt />
            </div>
          </div>

          <div className="space-y-6 px-6 py-6">
            <div className="grid gap-3 lg:grid-cols-3">
              <div className="rounded-xl border border-border/70 bg-background/40 p-4">
                <p className="text-sm font-semibold text-foreground">1. Leitura real de cota</p>
                <p className="mt-2 text-sm leading-6 text-muted-foreground">
                  O proxy mede quantos tokens a request precisa e compara isso com a cota disponivel das contas que estao
                  no pool naquele instante.
                </p>
              </div>
              <div className="rounded-xl border border-border/70 bg-background/40 p-4">
                <p className="text-sm font-semibold text-foreground">2. Recuperacao automatica</p>
                <p className="mt-2 text-sm leading-6 text-muted-foreground">
                  Se nenhuma conta suportar a carga, o app cria outra conta, resolve captcha, confirma email, faz login
                  OAuth e injeta essa conta direto no proxy.
                </p>
              </div>
              <div className="rounded-xl border border-border/70 bg-background/40 p-4">
                <p className="text-sm font-semibold text-foreground">3. Retorno ao pool</p>
                <p className="mt-2 text-sm leading-6 text-muted-foreground">
                  Depois do vinculo, o app atualiza Coding Plan e billing da conta nova. A selecao da request roda de
                  novo com essa conta ja disponivel.
                </p>
              </div>
            </div>

            <div className="grid gap-4 lg:grid-cols-[1.3fr_1fr]">
              <div className="rounded-xl border border-border/70 bg-background/40 p-4">
                <p className="text-sm font-semibold text-foreground">Quando exatamente ele dispara</p>
                <div className="mt-3 space-y-3">
                  {[
                    'A request pede mais tokens do que qualquer conta disponivel consegue oferecer agora.',
                    'As contas elegiveis daquele modelo acabam a cota ou falham antes de atender a necessidade.',
                    'O proxy precisa repor o pool para continuar a fila sem descartar a request cedo demais.',
                  ].map((item) => (
                    <div key={item} className="flex items-start gap-3 rounded-lg border border-white/8 bg-white/[0.03] px-3 py-2">
                      <div className="mt-0.5 flex h-5 w-5 items-center justify-center rounded-full bg-emerald-500/10 text-emerald-300">
                        <ChevronRight className="h-3.5 w-3.5" />
                      </div>
                      <p className="text-sm leading-6 text-muted-foreground">{item}</p>
                    </div>
                  ))}
                </div>
              </div>

              <div className="rounded-xl border border-sky-400/20 bg-sky-500/[0.045] p-4">
                <p className="text-sm font-semibold text-sky-100">Leitura do runtime atual</p>
                <div className="mt-3 space-y-3 text-sm">
                  <div>
                    <p className="text-[11px] uppercase tracking-[0.14em] text-sky-200/70">Modo</p>
                    <p className="mt-1 font-medium text-sky-50">{modeLabel}</p>
                  </div>
                  <div>
                    <p className="text-[11px] uppercase tracking-[0.14em] text-sky-200/70">Estado</p>
                    <p className="mt-1 font-medium text-sky-50">{stateLabel}</p>
                  </div>
                  <div>
                    <p className="text-[11px] uppercase tracking-[0.14em] text-sky-200/70">Ultimo resultado</p>
                    <p className="mt-1 break-words font-medium text-sky-50">
                      {lastResultSummary ?? status?.lastError ?? 'Sem historico ainda'}
                    </p>
                  </div>
                </div>
              </div>
            </div>
          </div>
        </DialogContent>
      </Dialog>
    </>
  )
}
