import { useState } from 'react'
import { ThemeProvider } from '@/components/theme-provider'
import { Onboarding } from '@/components/onboarding'
import { Home } from '@/components/home'

const ONBOARDING_KEY = 'hasSeenOnboarding'

function hasSeenOnboarding(): boolean {
  try {
    return localStorage.getItem(ONBOARDING_KEY) === 'true'
  } catch {
    return false
  }
}

export default function App() {
  const [showOnboarding, setShowOnboarding] = useState(!hasSeenOnboarding())

  return (
    <ThemeProvider>
      {showOnboarding ? (
        <Onboarding onComplete={() => setShowOnboarding(false)} />
      ) : (
        <Home />
      )}
    </ThemeProvider>
  )
}
