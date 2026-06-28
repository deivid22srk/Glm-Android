import { useEffect, useMemo, useState } from 'react'
import { Check, Copy, KeyRound, Play, Power, Server, Trash2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import type { Settings } from '@/types/api'

interface APISettingsDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  settings: Settings
  onToggleAPI: (enabled: boolean) => Promise<void>
  onUpdatePort: (port: number) => Promise<boolean>
  onCreateKey: (name: string) => Promise<string>
  onDeleteKey: (id: string) => Promise<void>
}

type CopyTarget = 'config' | 'curl' | 'secret' | null

export function APISettingsDialog({
  open,
  onOpenChange,
  settings,
  onToggleAPI,
  onUpdatePort,
  onCreateKey,
  onDeleteKey,
}: APISettingsDialogProps) {
  const [port, setPort] = useState(String(settings.port))
  const [keyName, setKeyName] = useState('')
  const [newSecret, setNewSecret] = useState<string | null>(null)
  const [exampleKey, setExampleKey] = useState('')
  const [busy, setBusy] = useState(false)
  const [copied, setCopied] = useState<CopyTarget>(null)
  const [notice, setNotice] = useState<string | null>(null)

  const baseURL = `http://127.0.0.1:${settings.port}/v1`
  const exampleModel = 'glm-5.2'
  const configuredKey = exampleKey.trim() || newSecret || (settings.apiKeys.length === 0 ? 'qualquer-valor' : '<cole-a-chave-completa-aqui>')
  const keyExplanation = newSecret
    ? 'O comando abaixo ja esta pronto com a chave criada agora.'
    : settings.apiKeys[0]
      ? 'A chave completa nao fica salva em texto puro. Crie uma nova chave para gerar um curl pronto, ou cole aqui a chave que voce salvou.'
      : 'Nenhuma API key cadastrada. Enquanto isso, qualquer valor e aceito.'

  const clientConfig = useMemo(() => [
    `Base URL: ${baseURL}`,
    `Model: ${exampleModel}`,
    `API key: ${configuredKey}`,
  ].join('\n'), [baseURL, configuredKey])

  const curlExample = useMemo(() => [
    `curl.exe http://127.0.0.1:${settings.port}/v1/chat/completions ^`,
    '  -H "Content-Type: application/json" ^',
    `  -H "Authorization: Bearer ${configuredKey}" ^`,
    `  -d "{\\"model\\":\\"${exampleModel}\\",\\"messages\\":[{\\"role\\":\\"user\\",\\"content\\":\\"responda apenas ok\\"}],\\"stream\\":false}"`,
  ].join('\n'), [configuredKey, settings.port])

  useEffect(() => setPort(String(settings.port)), [settings.port])
  useEffect(() => {
    if (newSecret) setExampleKey(newSecret)
  }, [newSecret])

  const copyText = async (target: CopyTarget, text: string) => {
    if (!target) return
    await navigator.clipboard.writeText(text)
    setCopied(target)
    setTimeout(() => setCopied(null), 1500)
  }

  const toggleAPI = async () => {
    setBusy(true)
    try {
      await onToggleAPI(!settings.apiEnabled)
    } finally {
      setBusy(false)
    }
  }

  const savePort = async () => {
    const value = Number(port)
    if (!Number.isInteger(value) || value < 1 || value > 65535) {
      setNotice('A porta deve estar entre 1 e 65535.')
      return
    }
    const restartRequired = await onUpdatePort(value)
    setNotice(restartRequired ? 'Porta salva. Reinicie o aplicativo para aplicar.' : 'Porta atualizada.')
  }

  const createKey = async () => {
    setBusy(true)
    try {
      const secret = await onCreateKey(keyName)
      setNewSecret(secret)
      setKeyName('')
      setNotice('A chave aparece completa apenas agora. Salve antes de fechar.')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[88vh] max-w-3xl overflow-y-auto">
        <DialogHeader>
          <DialogTitle>API OpenAI-compatible</DialogTitle>
          <DialogDescription>Configure a porta local, gere API keys e copie os dados para Zed, Kilo, Roo, OpenCode ou Codex.</DialogDescription>
        </DialogHeader>

        <section className="rounded-md border border-border/60 bg-background/35 p-4">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div className="flex min-w-0 items-center gap-3">
              <div className={`flex h-9 w-9 shrink-0 items-center justify-center rounded-md ${settings.apiEnabled ? 'bg-emerald-500/12 text-emerald-500' : 'bg-muted text-muted-foreground'}`}>
                <Server className="h-4 w-4" />
              </div>
              <div className="min-w-0">
                <p className="text-sm font-semibold">Servidor local</p>
                <p className="break-all text-xs text-muted-foreground">
                  {settings.apiEnabled ? `Ativo em ${baseURL}` : 'Rotas /v1 desativadas'}
                </p>
              </div>
            </div>
            <Button variant={settings.apiEnabled ? 'outline' : 'default'} onClick={toggleAPI} disabled={busy}>
              {settings.apiEnabled ? <Power className="h-4 w-4" /> : <Play className="h-4 w-4" />}
              {settings.apiEnabled ? 'Parar API' : 'Iniciar API'}
            </Button>
          </div>

          <div className="mt-4 grid gap-2 border-t border-border/50 pt-4 sm:grid-cols-[1fr_auto] sm:items-end">
            <label className="min-w-0">
              <span className="mb-1.5 block text-xs font-medium">Porta</span>
              <input
                value={port}
                onChange={(event) => setPort(event.target.value)}
                inputMode="numeric"
                className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm outline-none focus:ring-2 focus:ring-ring"
              />
            </label>
            <Button variant="outline" onClick={savePort}>Salvar porta</Button>
          </div>
        </section>

        <section className="rounded-md border border-border/60 bg-background/35 p-4">
          <div className="mb-3">
            <h3 className="text-sm font-semibold">Dados para o cliente</h3>
            <p className="mt-1 text-xs text-muted-foreground">{keyExplanation}</p>
          </div>

          <div className="grid gap-3">
            {settings.apiKeys.length > 0 && (
              <label className="block">
                <span className="mb-1.5 block text-xs font-medium">API key para montar o exemplo</span>
                <input
                  value={exampleKey}
                  onChange={(event) => setExampleKey(event.target.value)}
                  placeholder="Cole aqui a chave completa salva, ou crie uma nova chave abaixo"
                  className="h-9 w-full min-w-0 rounded-md border border-input bg-background px-3 font-mono text-xs outline-none focus:ring-2 focus:ring-ring"
                />
              </label>
            )}

            <CopyBlock
              title="Configuracao"
              description="Cole esses tres campos no cliente OpenAI-compatible."
              copied={copied === 'config'}
              onCopy={() => copyText('config', clientConfig)}
            >
              {clientConfig}
            </CopyBlock>

            <CopyBlock
              title="Teste rapido"
              description="Use no PowerShell para validar a API local."
              copied={copied === 'curl'}
              onCopy={() => copyText('curl', curlExample)}
            >
              {curlExample}
            </CopyBlock>
          </div>
        </section>

        <section className="rounded-md border border-border/60 bg-background/35 p-4">
          <div className="mb-3 flex items-center gap-2">
            <KeyRound className="h-4 w-4" />
            <h3 className="text-sm font-semibold">API keys</h3>
            <span className="text-xs text-muted-foreground">({settings.apiKeys.length})</span>
          </div>

          <div className="grid gap-2 sm:grid-cols-[1fr_auto]">
            <input
              value={keyName}
              onChange={(event) => setKeyName(event.target.value)}
              placeholder="Nome, por exemplo: Zed"
              className="h-9 min-w-0 rounded-md border border-input bg-background px-3 text-sm outline-none focus:ring-2 focus:ring-ring"
            />
            <Button onClick={createKey} disabled={busy}>Criar chave</Button>
          </div>

          {newSecret && (
            <div className="mt-3 rounded-md border border-emerald-500/30 bg-emerald-500/5 p-3">
              <div className="mb-2 flex items-center justify-between gap-2">
                <p className="text-xs font-semibold text-emerald-500">Nova chave criada</p>
                <Button variant="outline" size="sm" title="Copiar chave" onClick={() => copyText('secret', newSecret)}>
                  {copied === 'secret' ? <Check className="h-3.5 w-3.5 text-emerald-500" /> : <Copy className="h-3.5 w-3.5" />}
                  Copiar
                </Button>
              </div>
              <code className="block max-w-full select-text overflow-x-auto whitespace-nowrap rounded bg-black/20 px-2.5 py-2 font-mono text-xs">
                {newSecret}
              </code>
            </div>
          )}

          <div className="mt-3 space-y-2">
            {settings.apiKeys.length === 0 ? (
              <p className="rounded-md border border-dashed border-border/70 py-6 text-center text-xs text-muted-foreground">
                Sem chave configurada. Enquanto isso, qualquer valor de API key e aceito.
              </p>
            ) : settings.apiKeys.map((key) => (
              <div key={key.id} className="flex min-w-0 items-center justify-between gap-3 rounded-md border border-border/60 px-3 py-2.5">
                <div className="min-w-0">
                  <p className="truncate text-sm font-medium">{key.name}</p>
                  <p className="font-mono text-[11px] text-muted-foreground">{key.prefix}********</p>
                </div>
                <Button variant="ghost" size="icon" title="Excluir API key" onClick={() => onDeleteKey(key.id)}>
                  <Trash2 className="h-4 w-4 text-red-400" />
                </Button>
              </div>
            ))}
          </div>
        </section>

        {notice && <p className="rounded-md bg-muted px-3 py-2 text-xs text-muted-foreground">{notice}</p>}
      </DialogContent>
    </Dialog>
  )
}

function CopyBlock({
  title,
  description,
  copied,
  onCopy,
  children,
}: {
  title: string
  description: string
  copied: boolean
  onCopy: () => void
  children: string
}) {
  return (
    <div className="min-w-0 rounded-md border border-border/50 bg-black/20 p-3">
      <div className="mb-2 flex flex-wrap items-start justify-between gap-2">
        <div>
          <p className="text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">{title}</p>
          <p className="mt-0.5 text-[11px] text-muted-foreground">{description}</p>
        </div>
        <Button variant="ghost" size="sm" className="h-7 px-2" onClick={onCopy}>
          {copied ? <Check className="h-3.5 w-3.5 text-emerald-500" /> : <Copy className="h-3.5 w-3.5" />}
          Copiar
        </Button>
      </div>
      <pre className="max-w-full overflow-x-auto whitespace-pre rounded bg-background/80 p-3 font-mono text-xs leading-relaxed text-foreground">
        {children}
      </pre>
    </div>
  )
}
