export async function openExternalURL(url: string): Promise<void> {
  if (!/^https?:\/\//i.test(url)) {
    throw new Error('A URL de autenticacao retornada e invalida')
  }

  try {
    const browserOpenURL = window.runtime?.BrowserOpenURL
    if (browserOpenURL) {
      browserOpenURL(url)
      return
    }
  } catch {
    // Wails runtime unavailable.
  }

  try {
    const openExternal = window.go?.main?.Desktop?.OpenExternalURL
    if (openExternal) {
      await openExternal(url)
      return
    }
  } catch {
    // Browser preview does not expose Go bindings.
  }

  const opened = window.open(url, '_blank', 'noopener,noreferrer')
  if (!opened) {
    throw new Error('Nao foi possivel abrir o navegador para autenticacao')
  }
}
