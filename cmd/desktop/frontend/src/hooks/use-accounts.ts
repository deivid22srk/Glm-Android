import { useCallback, useEffect, useRef, useState } from 'react'
import { api } from '@/lib/api'
import type { Account, AccountsResponse } from '@/types/api'

export interface RefreshAccountsOptions {
  cancelInFlight?: boolean
  includeQuota?: boolean
}

export interface RefreshAccountQuotaOptions {
  clearError?: boolean
}

const QUOTA_CONCURRENCY = 2

type AccountDetailResponse = Account & { object?: string; credentialSource?: string }

export function useAccounts(pollInterval = 5000) {
  const [data, setData] = useState<AccountsResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [quotaRefreshing, setQuotaRefreshing] = useState(false)
  const dataRef = useRef<AccountsResponse | null>(null)
  const mountedRef = useRef(false)
  const inFlightRef = useRef<Promise<void> | null>(null)
  const controllerRef = useRef<AbortController | null>(null)
  const requestIdRef = useRef(0)
  const quotaRunIdRef = useRef(0)
  const quotaControllersRef = useRef<Map<string, AbortController>>(new Map())

  const stopQuotaRefresh = useCallback(() => {
    quotaRunIdRef.current += 1
    for (const controller of quotaControllersRef.current.values()) {
      controller.abort()
    }
    quotaControllersRef.current.clear()
    if (mountedRef.current) {
      setQuotaRefreshing(false)
    }
  }, [])

  useEffect(() => {
    dataRef.current = data
  }, [data])

  const mergeLightRefresh = (current: AccountsResponse | null, next: AccountsResponse): AccountsResponse => {
    const previousById = new Map((current?.data ?? []).map((account) => [account.id, account]))
    return {
      ...next,
      data: next.data.map((account) => {
        const previous = previousById.get(account.id)
        if (!previous) {
          return account
        }
        return {
          ...account,
          quota: previous.quota,
          quotaError: previous.quotaError,
          quotaSkipped: previous.quotaSkipped ?? account.quotaSkipped,
          quotaLoading: previous.quotaLoading ?? false,
        }
      }),
    }
  }

  const mergeQuotaDetail = (current: AccountsResponse | null, detail: AccountDetailResponse): AccountsResponse | null => {
    if (!current) return current
    return {
      ...current,
      data: current.data.map((account) =>
        account.id === detail.id
          ? {
              ...account,
              quota: detail.quota,
              quotaError: detail.quotaError,
              quotaSkipped: false,
              quotaLoading: false,
            }
          : account
      ),
    }
  }

  const markQuotaTargets = (current: AccountsResponse | null, ids: Set<string>, clearError: boolean): AccountsResponse | null => {
    if (!current) return current
    return {
      ...current,
      data: current.data.map((account) =>
        ids.has(account.id)
          ? {
              ...account,
              quotaLoading: true,
              quotaSkipped: false,
              quotaError: clearError ? null : account.quotaError,
            }
          : account
      ),
    }
  }

  const markQuotaFailure = (current: AccountsResponse | null, id: string, message: string): AccountsResponse | null => {
    if (!current) return current
    return {
      ...current,
      data: current.data.map((account) =>
        account.id === id
          ? {
              ...account,
              quotaLoading: false,
              quotaSkipped: false,
              quotaError: { message, type: 'zcode_quota_fetch_failed' },
            }
          : account
      ),
    }
  }

  const refreshAccountQuota = useCallback(async (id: string, options: RefreshAccountQuotaOptions = {}) => {
    const controller = new AbortController()
    quotaControllersRef.current.get(id)?.abort()
    quotaControllersRef.current.set(id, controller)
    if (mountedRef.current) {
      setData((current) => markQuotaTargets(current, new Set([id]), options.clearError !== false))
      setQuotaRefreshing(true)
    }
    try {
      const detail = await api.get<AccountDetailResponse>(`/api/admin/accounts/${id}`, { signal: controller.signal })
      if (!mountedRef.current || controller.signal.aborted) {
        return
      }
      setData((current) => mergeQuotaDetail(current, detail))
    } catch (err) {
      if (controller.signal.aborted || (err instanceof DOMException && err.name === 'AbortError')) {
        return
      }
      if (mountedRef.current) {
        const message = err instanceof Error ? err.message : 'Nao foi possivel consultar a cota'
        setData((current) => markQuotaFailure(current, id, message))
      }
    } finally {
      quotaControllersRef.current.delete(id)
      if (mountedRef.current && quotaControllersRef.current.size === 0) {
        setQuotaRefreshing(false)
      }
    }
  }, [])

  const pickQuotaTargets = (snapshot: AccountsResponse, force: boolean): string[] => {
    if (force) {
      return snapshot.data.map((account) => account.id)
    }
    return snapshot.data
      .filter((account) => account.quotaLoading !== true && account.quota == null && account.quotaError == null)
      .map((account) => account.id)
  }

  const refreshQuotas = useCallback(async (snapshot: AccountsResponse, force: boolean) => {
    const targets = pickQuotaTargets(snapshot, force)
    if (targets.length === 0) {
      return
    }

    stopQuotaRefresh()
    const runId = quotaRunIdRef.current
    const targetSet = new Set(targets)
    setData((current) => markQuotaTargets(current, targetSet, force))
    setQuotaRefreshing(true)

    let cursor = 0
    const worker = async () => {
      while (true) {
        if (quotaRunIdRef.current !== runId) {
          return
        }
        const id = targets[cursor]
        cursor += 1
        if (!id) {
          return
        }
        const controller = new AbortController()
        quotaControllersRef.current.set(id, controller)
        try {
          const detail = await api.get<AccountDetailResponse>(`/api/admin/accounts/${id}`, { signal: controller.signal })
          if (!mountedRef.current || controller.signal.aborted || quotaRunIdRef.current !== runId) {
            continue
          }
          setData((current) => mergeQuotaDetail(current, detail))
        } catch (err) {
          if (controller.signal.aborted || (err instanceof DOMException && err.name === 'AbortError')) {
            continue
          }
          if (mountedRef.current && quotaRunIdRef.current === runId) {
            const message = err instanceof Error ? err.message : 'Nao foi possivel consultar a cota'
            setData((current) => markQuotaFailure(current, id, message))
          }
        } finally {
          quotaControllersRef.current.delete(id)
        }
      }
    }

    try {
      await Promise.all(Array.from({ length: Math.min(QUOTA_CONCURRENCY, targets.length) }, () => worker()))
    } finally {
      if (mountedRef.current && quotaRunIdRef.current === runId) {
        setQuotaRefreshing(false)
      }
    }
  }, [stopQuotaRefresh])

  const refresh = useCallback(async (options: RefreshAccountsOptions = {}) => {
    if (inFlightRef.current && !options.cancelInFlight) {
      return inFlightRef.current
    }

    if (options.cancelInFlight) {
      controllerRef.current?.abort()
    }

    const requestId = requestIdRef.current + 1
    requestIdRef.current = requestId
    const controller = new AbortController()
    controllerRef.current = controller

    const promise = (async () => {
      try {
        const res = await api.get<AccountsResponse>('/api/admin/accounts?quota=0', { signal: controller.signal })
        if (!mountedRef.current || controller.signal.aborted || requestId !== requestIdRef.current) return
        const merged = mergeLightRefresh(dataRef.current, res)
        setData(merged)
        setError(null)
        void refreshQuotas(merged, options.includeQuota === true)
      } catch (err) {
        if (controller.signal.aborted || (err instanceof DOMException && err.name === 'AbortError')) return
        if (mountedRef.current) setError(err instanceof Error ? err.message : 'Unknown error')
      } finally {
        if (controllerRef.current === controller) {
          controllerRef.current = null
          inFlightRef.current = null
          if (mountedRef.current) setLoading(false)
        }
      }
    })()

    inFlightRef.current = promise
    return promise
  }, [refreshQuotas])

  const reorder = async (accounts: Account[]) => {
    const previous = dataRef.current
    const ordered = accounts.map((account, index) => ({
      ...account,
      queuePosition: index + 1,
      registrationOrder: index + 1,
      label: `Conta ${index + 1}`,
    }))
    setData((current) => current ? { ...current, data: ordered } : current)
    try {
      await api.put('/api/admin/accounts/order', {
        accountIds: ordered.map((account) => account.id),
      })
      setError(null)
    } catch (err) {
      setData(previous)
      setError(err instanceof Error ? err.message : 'Falha ao salvar a ordem')
      throw err
    }
  }

  useEffect(() => {
    mountedRef.current = true
    void refresh()
    const id = setInterval(() => {
      if (!inFlightRef.current) void refresh()
    }, pollInterval)
    return () => {
      mountedRef.current = false
      controllerRef.current?.abort()
      stopQuotaRefresh()
      clearInterval(id)
    }
  }, [pollInterval, refresh, stopQuotaRefresh])

  return { data, loading, error, refresh, reorder, quotaRefreshing, refreshAccountQuota }
}
