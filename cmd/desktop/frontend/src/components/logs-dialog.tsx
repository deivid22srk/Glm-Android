import { useCallback, useEffect, useState } from 'react'
import { AlertTriangle, Check, Clipboard, Copy, Info, RefreshCw, XCircle } from 'lucide-react'
import { api } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { ScrollArea } from '@/components/ui/scroll-area'

interface LogEntry {
  id: number
  timestamp: string
  level: 'info' | 'warn' | 'error'
  event: string
  message: string
}

interface LogsDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

const levelStyle = {
  info: { icon: Info, className: 'text-sky-400 bg-sky-500/10' },
  warn: { icon: AlertTriangle, className: 'text-amber-400 bg-amber-500/10' },
  error: { icon: XCircle, className: 'text-red-400 bg-red-500/10' },
}

function isAccountSwitchEvent(event: string) {
  return event === 'account.rotated' || event === 'account.activated'
}

export function LogsDialog({ open, onOpenChange }: LogsDialogProps) {
  const [logs, setLogs] = useState<LogEntry[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [copied, setCopied] = useState<number | 'all' | null>(null)

  const refresh = useCallback(async () => {
    setLoading(true)
    try {
      const response = await api.get<{ data: LogEntry[] }>('/api/admin/logs?limit=250')
      setLogs([...response.data].reverse())
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Falha ao carregar logs')
    } finally {
      setLoading(false)
    }
  }, [])

  const copyText = useCallback(async (target: number | 'all', text: string) => {
    await navigator.clipboard.writeText(text)
    setCopied(target)
    window.setTimeout(() => setCopied(null), 1400)
  }, [])

  const copyAll = useCallback(async () => {
    await copyText('all', logs.map(formatLogEntry).join('\n\n'))
  }, [copyText, logs])

  useEffect(() => {
    if (!open) return
    void refresh()
    const timer = setInterval(refresh, 3000)
    return () => clearInterval(timer)
  }, [open, refresh])

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[86vh] max-w-5xl">
        <DialogHeader className="flex-row items-center justify-between pr-8">
          <div>
            <DialogTitle>Logs do sistema</DialogTitle>
            <p className="mt-1 text-xs text-muted-foreground">Eventos completos mantidos pelo processo Go</p>
          </div>
          <div className="flex gap-2">
            <Button variant="outline" size="sm" onClick={copyAll} disabled={logs.length === 0}>
              {copied === 'all' ? <Check className="h-3.5 w-3.5" /> : <Clipboard className="h-3.5 w-3.5" />}
              Copiar tudo
            </Button>
            <Button variant="outline" size="sm" onClick={refresh} disabled={loading}>
              <RefreshCw className={loading ? 'h-3.5 w-3.5 animate-spin' : 'h-3.5 w-3.5'} />
              Atualizar
            </Button>
          </div>
        </DialogHeader>

        <ScrollArea className="h-[64vh] pr-4">
          {error ? (
            <p className="rounded-md border border-red-500/25 bg-red-500/5 p-3 text-sm text-red-400">{error}</p>
          ) : logs.length === 0 ? (
            <p className="py-16 text-center text-sm text-muted-foreground">Nenhum evento registrado nesta execucao.</p>
          ) : (
            <div className="space-y-3">
              {logs.map((entry) => {
                const style = levelStyle[entry.level] ?? levelStyle.info
                const Icon = style.icon
                const diagnostic = parseDiagnostic(entry.message)
                const entryText = formatLogEntry(entry)
                const switchEvent = isAccountSwitchEvent(entry.event)
                return (
                  <div
                    key={entry.id}
                    className={`flex gap-3 rounded-md border p-3 ${
                      switchEvent
                        ? 'border-sky-400/35 bg-sky-500/[0.06] shadow-[0_10px_30px_rgba(14,165,233,0.08)]'
                        : 'border-border/50 bg-background/35'
                    }`}
                  >
                    <div className={`mt-0.5 flex h-7 w-7 shrink-0 items-center justify-center rounded ${style.className}`}>
                      <Icon className="h-3.5 w-3.5" />
                    </div>
                    <div className="min-w-0 flex-1">
                      <div className="flex flex-wrap items-center justify-between gap-2">
                        <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
                          <span className="font-mono text-[10px] text-muted-foreground">
                            {new Date(entry.timestamp).toLocaleString('pt-BR')}
                          </span>
                          <span className="font-mono text-[10px] uppercase text-muted-foreground">{entry.event}</span>
                          {switchEvent && (
                            <span className="rounded-full border border-sky-400/25 bg-sky-500/10 px-2 py-0.5 text-[10px] font-semibold text-sky-300">
                              Troca de conta
                            </span>
                          )}
                        </div>
                        <Button
                          variant="ghost"
                          size="sm"
                          className="h-7 px-2"
                          title="Copiar log completo"
                          onClick={() => copyText(entry.id, entryText)}
                        >
                          {copied === entry.id ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
                          Copiar
                        </Button>
                      </div>
                      {diagnostic ? (
                        <div className="mt-2 space-y-2">
                          <p className="break-words text-xs leading-relaxed">{diagnostic.prefix}</p>
                          <pre className="max-h-[420px] overflow-auto rounded-md border border-border/60 bg-black/30 p-3 font-mono text-[11px] leading-relaxed text-foreground">
                            {diagnostic.pretty}
                          </pre>
                        </div>
                      ) : (
                        <p className="mt-1 whitespace-pre-wrap break-words text-xs leading-relaxed">{entry.message}</p>
                      )}
                    </div>
                  </div>
                )
              })}
            </div>
          )}
        </ScrollArea>
      </DialogContent>
    </Dialog>
  )
}

function formatLogEntry(entry: LogEntry) {
  const diagnostic = parseDiagnostic(entry.message)
  const message = diagnostic ? `${diagnostic.prefix}\n${diagnostic.pretty}` : entry.message
  return [
    `[${new Date(entry.timestamp).toLocaleString('pt-BR')}] ${entry.level.toUpperCase()} ${entry.event}`,
    message,
  ].join('\n')
}

function parseDiagnostic(message: string): { prefix: string; pretty: string } | null {
  const marker = 'Diagnostico sanitizado: '
  const index = message.indexOf(marker)
  if (index < 0) return null
  const prefix = message.slice(0, index + marker.length).trim()
  const raw = message.slice(index + marker.length).trim()
  try {
    return { prefix, pretty: JSON.stringify(JSON.parse(raw), null, 2) }
  } catch {
    return { prefix, pretty: raw }
  }
}
