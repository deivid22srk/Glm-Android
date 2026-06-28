import { createContext, useContext } from 'react'

type Theme = 'dark' | 'light' | 'system'

interface ThemeProviderState {
  theme: Theme
  setTheme: (theme: Theme) => void
  resolvedTheme: 'dark' | 'light'
}

const initialState: ThemeProviderState = {
  theme: 'system',
  setTheme: () => null,
  resolvedTheme: 'dark',
}

const ThemeProviderContext = createContext<ThemeProviderState>(initialState)

const STORAGE_KEY = 'kimi-ui-theme'

export function useThemeProvider() {
  const context = useContext(ThemeProviderContext)
  if (context === undefined) {
    throw new Error('useThemeProvider must be used within a ThemeProvider')
  }
  return context
}

export function resolveTheme(theme: Theme): 'dark' | 'light' {
  if (theme === 'system') {
    return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
  }
  return theme
}

export function getStoredTheme(): Theme {
  try {
    const stored = localStorage.getItem(STORAGE_KEY)
    if (stored === 'dark' || stored === 'light' || stored === 'system') {
      return stored
    }
  } catch {}
  return 'system'
}

export function applyTheme(theme: Theme) {
  const resolved = resolveTheme(theme)
  const root = document.documentElement
  if (resolved === 'dark') {
    root.classList.add('dark')
  } else {
    root.classList.remove('dark')
  }
}

export { ThemeProviderContext, STORAGE_KEY }
export type { Theme, ThemeProviderState }
