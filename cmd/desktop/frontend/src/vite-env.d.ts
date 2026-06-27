/// <reference types="vite/client" />

interface Window {
  runtime?: {
    BrowserOpenURL?: (url: string) => void
  }
  go?: {
    main?: {
      Desktop?: {
        APIRequest?: (method: string, path: string, body: string) => Promise<{ status: number; body: string }>
        OpenExternalURL?: (url: string) => Promise<void>
        Port?: () => Promise<number>
      }
    }
  }
}
