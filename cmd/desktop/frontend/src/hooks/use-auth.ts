import { useState, useCallback } from 'react'
import { api } from '@/lib/api'
import { openExternalURL } from '@/lib/wails'

interface LoginFlow {
  flowId: string
  authorizeUrl: string
  pollIntervalSec: number
  status: string
}

interface PollResult {
  status: string
  account?: {
    id: string
    email: string
  }
  error?: string
}

export function useAuth() {
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const startLogin = useCallback(async () => {
    setPending(true)
    setError(null)
    try {
      const t0 = performance.now()
      const flow = await api.post<LoginFlow>('/api/admin/auth/login/start')
      const t1 = performance.now()
      console.info(`[auth] login/start: ${(t1 - t0).toFixed(0)}ms (flow ${flow.flowId})`)
      const openT0 = performance.now()
      void openExternalURL(flow.authorizeUrl).then(() => {
        console.info(`[auth] openExternalURL: ${(performance.now() - openT0).toFixed(0)}ms`)
      }).catch((err) => {
        console.warn(`[auth] openExternalURL falhou apos ${(performance.now() - openT0).toFixed(0)}ms`, err)
        const msg = err instanceof Error ? err.message : 'Nao foi possivel abrir o navegador'
        setError(msg)
      })
      return flow
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'Login failed'
      setError(msg)
      setPending(false)
      throw err
    }
  }, [])

  const pollLogin = useCallback(async (flowId: string): Promise<PollResult> => {
    try {
      const result = await api.get<PollResult>(`/api/admin/auth/login/poll?flow_id=${flowId}`)
      if (result.status === 'ready' || result.status === 'failed') {
        setPending(false)
      }
      if (result.status === 'failed') {
        setError(result.error || 'Login nao concluido')
      }
      return result
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'Poll failed'
      setError(msg)
      setPending(false)
      throw err
    }
  }, [])

  const resetLogin = useCallback(() => {
    setPending(false)
    setError(null)
  }, [])

  return { pending, error, startLogin, pollLogin, resetLogin }
}
