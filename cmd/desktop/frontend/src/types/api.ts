export interface QuotaBalance {
  id: string
  model: string
  total: number | null
  used: number | null
  remaining: number | null
  available: number | null
  usagePercent: number | null
  periodEnd: string | null
}

export interface QuotaSnapshot {
  generatedAt: string
  balances: QuotaBalance[]
}

export interface Account {
  id: string
  label: string
  active: boolean
  queuePosition: number
  registrationOrder: number
  user: {
    id?: string
    email?: string
    name?: string
    avatar?: string
  }
  quota: QuotaSnapshot | null
  quotaError: { message: string; type: string } | null
  quotaSkipped?: boolean
  quotaLoading?: boolean
  hasZcodeJwtToken: boolean
  hasZaiAccessToken: boolean
  tokenExpiresAt: string | null
  tokenExpired: boolean | null
}

export interface AccountsResponse {
  object: string
  activeAccountId: string | null
  data: Account[]
}

export interface ZCodeEnvironment {
  homeDir: string
  dataDir: string
  credentialsPath: string
  configPath: string
  settingPath: string
  codingPlanPath: string
  installPath?: string
  appServerScript?: string
  runningProcesses: Array<{
    pid: number
    executable?: string
    commandLine?: string
    role: string
  }>
  currentUser?: {
    id?: string
    email?: string
    name?: string
  }
  credentialsPresent: boolean
  configPresent: boolean
  detectedAt: string
  restartRecommended: boolean
  liveRefreshPossible: boolean
  liveRefreshReason?: string
  bridgeInstalled: boolean
  bridgeVersion?: string
  bridgeScriptPath?: string
  warnings?: string[]
}

export interface ZCodeApplyResult {
  environment: ZCodeEnvironment
  account: Account
  backupPath?: string
  configUpdated: boolean
  credentialsUpdated: boolean
  restartRecommended: boolean
  liveRefreshPossible: boolean
  liveRefreshReason?: string
  liveRefreshQueued: boolean
  bridgePatched: boolean
  bridgePatchMessage?: string
  bridgeRestartedApp: boolean
}

export interface AccountActivateResponse {
  activeAccount: Account
}

export interface AccountDeleteResponse {
  removed: boolean
  accountId: string
  activeAccount: Account | null
}

export interface APIKey {
  id: string
  name: string
  prefix: string
  createdAt: string
}

export type ThinkingEffort = 'none' | 'low' | 'medium' | 'high' | 'max'

export interface ThinkingSettings {
  enabled: boolean
  budgetTokens: number
  effort: ThinkingEffort
}

export interface Settings {
  version: number
  port: number
  apiEnabled: boolean
  apiKeyRequired: boolean
  globalThinking: ThinkingSettings
  accountThinking: Record<string, ThinkingSettings>
  apiKeys: APIKey[]
}

export interface AccountCreatorResult {
  enabled: boolean
  started: boolean
  command?: string
  workDir?: string
  output?: string
  duration?: string
  username?: string
  email?: string
  accountId?: string
  label?: string
}

export interface AccountCreatorProgress {
  stage: string
  message: string
  percent: number
  detail?: string
  startedAt?: string
  updatedAt?: string
  lastLogLine?: string
}

export interface AccountCreatorStatus {
  enabled: boolean
  busy: boolean
  mode: string
  workDir: string
  embeddedRootDir: string
  creatorDataDir: string
  creatorLogDir: string
  creatorLogFile: string
  creatorEmailFile: string
  solverDir: string
  solverApiBase: string
  lastRunAt?: string
  cooldownRemainingMs: number
  lastError?: string
  lastResult?: AccountCreatorResult
  progress: AccountCreatorProgress
}

export interface AccountRepairProgress {
  object: string
  active: boolean
  trigger?: string
  startedAt?: string
  completedAt?: string
  total: number
  processed: number
  healthy: number
  repaired: number
  removed: number
  skipped: number
  failed: number
  currentId?: string
  current?: string
  currentEmail?: string
  message?: string
}

export interface RecentQuotaUpdate {
  accountId: string
  updatedAt: string
}

export interface AdminOverview {
  runtime: string
  port: number
  activeAccountId: string | null
  accountCount: number
  settings: Settings
  accountCreator?: AccountCreatorStatus | null
  accountRepair?: AccountRepairProgress | null
  recentQuotaUpdate?: RecentQuotaUpdate | null
}
