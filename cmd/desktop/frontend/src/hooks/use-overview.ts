import { useCallback, useEffect, useState } from 'react'
import { api } from '@/lib/api'
import type { AdminOverview } from '@/types/api'

export function useOverview(pollInterval = 5000) {
  const [overview, setOverview] = useState<AdminOverview | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const refresh = useCallback(async () => {
    try {
      const res = await api.get<AdminOverview>('/api/admin/overview')
      setOverview(res)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Falha ao consultar o status geral')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void refresh()
    const timer = window.setInterval(() => {
      void refresh()
    }, pollInterval)
    return () => window.clearInterval(timer)
  }, [pollInterval, refresh])

  return {
    overview,
    loading,
    error,
    refresh,
  }
}
