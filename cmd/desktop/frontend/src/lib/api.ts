let cachedPort: number | null = null

async function getPort(): Promise<number> {
  if (cachedPort == null) {
    try {
      cachedPort = (await window.go?.main?.Desktop?.Port?.()) ?? 3005
    } catch {
      cachedPort = 3005
    }
  }
  return cachedPort
}

async function getBaseURLs(): Promise<string[]> {
  const port = await getPort()
  return [`http://127.0.0.1:${port}`, `http://localhost:${port}`]
}

function isNetworkError(err: unknown): boolean {
  return err instanceof TypeError || (err instanceof Error && /failed to fetch|network/i.test(err.message))
}

function hasNativeAPIRequest(): boolean {
  return typeof window !== 'undefined' && typeof window.go?.main?.Desktop?.APIRequest === 'function'
}

function abortError(): DOMException {
  return new DOMException('The operation was aborted.', 'AbortError')
}

function parseJSON<T>(body: string): T {
  return (body ? JSON.parse(body) : undefined) as T
}

async function nativeRequest<T>(path: string, init?: RequestInit): Promise<T> {
  if (init?.signal?.aborted) {
    throw abortError()
  }
  const method = init?.method ?? 'GET'
  const body = typeof init?.body === 'string' ? init.body : init?.body == null ? '' : String(init.body)
  const request = window.go?.main?.Desktop?.APIRequest
  if (!request) {
    throw new Error('API nativa do desktop indisponivel')
  }
  const response = await request(method, path, body)
  if (init?.signal?.aborted) {
    throw abortError()
  }
  if (response.status < 200 || response.status >= 300) {
    let message = `HTTP ${response.status}`
    if (response.body) {
      try {
        const parsed = JSON.parse(response.body)
        message = parsed?.error?.message ?? message
      } catch {
        message = response.body
      }
    }
    throw new Error(message)
  }
  return parseJSON<T>(response.body)
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  if (hasNativeAPIRequest()) {
    return nativeRequest<T>(path, init)
  }

  const bases = await getBaseURLs()
  let lastNetworkError: unknown = null
  for (const base of bases) {
    try {
      const res = await fetch(`${base}${path}`, {
        ...init,
        headers: {
          'Content-Type': 'application/json',
          ...init?.headers,
        },
      })
      if (!res.ok) {
        const body = await res.json().catch(() => ({}))
        const message = body?.error?.message ?? `HTTP ${res.status}`
        throw new Error(message)
      }
      return res.json() as Promise<T>
    } catch (err) {
      if (!isNetworkError(err) || init?.signal?.aborted) {
        throw err
      }
      lastNetworkError = err
    }
  }
  throw lastNetworkError instanceof Error ? lastNetworkError : new Error('Failed to fetch')
}

export const api = {
  get: <T>(path: string, init?: RequestInit) => request<T>(path, init),

  post: <T>(path: string, body?: unknown) =>
    request<T>(path, {
      method: 'POST',
      body: body != null ? JSON.stringify(body) : undefined,
    }),

  patch: <T>(path: string, body?: unknown) =>
    request<T>(path, {
      method: 'PATCH',
      body: body != null ? JSON.stringify(body) : undefined,
    }),

  put: <T>(path: string, body?: unknown) =>
    request<T>(path, {
      method: 'PUT',
      body: body != null ? JSON.stringify(body) : undefined,
    }),

  delete: <T>(path: string) => request<T>(path, { method: 'DELETE' }),
}
