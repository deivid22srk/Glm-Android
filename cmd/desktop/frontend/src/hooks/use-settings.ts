import { useEffect, useState } from 'react'
import { api } from '@/lib/api'
import type { Settings, ThinkingSettings } from '@/types/api'

interface UpdateResult {
  settings: Settings
  restartRequired: boolean
}

export function useSettings() {
  const [settings, setSettings] = useState<Settings | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const refresh = async () => {
    try {
      const res = await api.get<Settings>('/api/admin/settings')
      setSettings(res)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load settings')
    } finally {
      setLoading(false)
    }
  }

  const updatePort = async (port: number): Promise<UpdateResult> => {
    const res = await api.patch<UpdateResult>('/api/admin/settings', { port })
    setSettings(res.settings)
    return res
  }

  const setAPIEnabled = async (apiEnabled: boolean): Promise<UpdateResult> => {
    const res = await api.patch<UpdateResult>('/api/admin/settings', { apiEnabled })
    setSettings(res.settings)
    return res
  }

  const createAPIKey = async (name: string) => {
    const res = await api.post<{ apiKey: { id: string; name: string }; secret: string }>(
      '/api/admin/api-keys',
      { name }
    )
    await refresh()
    return res
  }

  const deleteAPIKey = async (id: string) => {
    await api.delete(`/api/admin/api-keys/${id}`)
    await refresh()
  }

  const setGlobalThinking = async (value: ThinkingSettings) => {
    const res = await api.put<ThinkingSettings>('/api/admin/thinking', value)
    await refresh()
    return res
  }

  const setAccountThinking = async (accountId: string, value: ThinkingSettings) => {
    const res = await api.put<{
      accountId: string
      override: ThinkingSettings
      effective: ThinkingSettings
      inherited: boolean
    }>(`/api/admin/accounts/${accountId}/thinking`, value)
    await refresh()
    return res
  }

  const resetAccountThinking = async (accountId: string) => {
    const res = await api.delete<{
      accountId: string
      override: null
      effective: ThinkingSettings
      inherited: boolean
    }>(`/api/admin/accounts/${accountId}/thinking`)
    await refresh()
    return res
  }

  useEffect(() => {
    refresh()
  }, [])

  return {
    settings,
    loading,
    error,
    refresh,
    updatePort,
    setAPIEnabled,
    createAPIKey,
    deleteAPIKey,
    setGlobalThinking,
    setAccountThinking,
    resetAccountThinking,
  }
}
