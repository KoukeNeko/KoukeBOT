import { useCallback, useState } from 'react'

import {
  completeGeminiOAuth,
  getOAuthPool,
  getOAuthStatus,
  getProviderStatus,
  importGeminiCLIToken,
  importPastedToken,
  removeOAuthPoolAccount,
  startGeminiOAuth,
} from '../../../api'
import type { OAuthStartResult, OAuthStatus, PoolEntry, ProviderStatus } from '../../../types'

export type ProviderTab = 'api-key' | 'oauth' | 'pool'
export type StatusMessage = { type: 'ok' | 'err'; text: string } | null

type Params = {
  setMessage: (message: StatusMessage) => void
  setStatus: (status: ProviderStatus) => void
}

export function useOAuthActions({ setMessage, setStatus }: Params) {
  const [providerTab, setProviderTab] = useState<ProviderTab>('api-key')
  const [oauthStatus, setOAuthStatus] = useState<OAuthStatus | null>(null)
  const [oauthStarting, setOAuthStarting] = useState(false)
  const [oauthCompleting, setOAuthCompleting] = useState(false)
  const [oauthMode, setOAuthMode] = useState<'local' | 'remote'>(() => {
    const host = window.location.hostname.toLowerCase()
    return host === 'localhost' || host === '127.0.0.1' || host === '::1' ? 'local' : 'remote'
  })
  const [oauthFlow, setOAuthFlow] = useState<OAuthStartResult | null>(null)
  const [oauthCallbackURL, setOAuthCallbackURL] = useState('')
  const [addAccountOpen, setAddAccountOpen] = useState(false)
  const [importingCLI, setImportingCLI] = useState(false)
  const [pastedTokenJSON, setPastedTokenJSON] = useState('')
  const [importingPasted, setImportingPasted] = useState(false)
  const [pool, setPool] = useState<PoolEntry[]>([])
  const [removingEmail, setRemovingEmail] = useState<string | null>(null)

  const refreshPool = useCallback(async (): Promise<PoolEntry[]> => {
    try {
      const entries = await getOAuthPool()
      setPool(entries)
      return entries
    } catch {
      setPool([])
      return []
    }
  }, [])

  const refreshOAuthSnapshot = useCallback(async () => {
    const [oauth, status, poolEntries] = await Promise.all([
      getOAuthStatus(),
      getProviderStatus(),
      refreshPool(),
    ])
    setOAuthStatus(oauth)
    setStatus(status)
    setPool(poolEntries)
    return { oauth, poolEntries }
  }, [refreshPool, setStatus])

  const pollOAuthCompletion = useCallback(
    async (previousPoolSize: number, timeoutMs: number): Promise<boolean> => {
      const deadline = Date.now() + timeoutMs
      while (Date.now() < deadline) {
        await new Promise((resolve) => setTimeout(resolve, 1500))
        try {
          const { oauth, poolEntries } = await refreshOAuthSnapshot()
          const poolSize = poolEntries.length
          if (poolSize > previousPoolSize || (oauth.pool_size ?? 0) > previousPoolSize) {
            return true
          }
        } catch {
          // Ignore polling errors until timeout.
        }
      }
      return false
    },
    [refreshOAuthSnapshot],
  )

  const handleStartOAuth = useCallback(async (providerID: string) => {
    setOAuthStarting(true)
    setMessage(null)
    const previousPoolSize = pool.length

    try {
      const start = await startGeminiOAuth({ mode: oauthMode, providerId: providerID })
      setOAuthFlow(start)
      setOAuthCallbackURL('')

      const popup = window.open(start.auth_url, 'koukebot-oauth', 'popup=yes,width=560,height=760')
      if (!popup) {
        throw new Error('瀏覽器封鎖了授權視窗，請允許 popup 後重試')
      }
      popup.focus()

      const completed = await pollOAuthCompletion(previousPoolSize, start.mode === 'local' ? 120_000 : 45_000)
      if (completed) {
        setOAuthFlow(null)
        setAddAccountOpen(false)
        setMessage({ type: 'ok', text: 'Google 帳號已連線並加入帳號池。' })
        return
      }

      if (start.mode === 'remote') {
        setMessage({
          type: 'ok',
          text: '已開啟 OAuth 授權頁。若無法自動完成，請將 callback URL 貼到下方完成綁定。',
        })
      } else {
        setMessage({
          type: 'ok',
          text: '授權視窗已完成，但尚未偵測到回寫。可將 callback URL 貼到下方手動完成。',
        })
      }
    } catch (error) {
      setMessage({
        type: 'err',
        text: error instanceof Error ? error.message : 'OAuth 啟動失敗',
      })
    } finally {
      setOAuthStarting(false)
    }
  }, [oauthMode, pollOAuthCompletion, pool.length, setMessage])

  const handleCompleteOAuth = useCallback(async () => {
    if (!oauthFlow) {
      setMessage({ type: 'err', text: '找不到 OAuth flow，請重新開始' })
      return
    }
    if (!oauthCallbackURL.trim()) {
      setMessage({ type: 'err', text: '請貼上 callback URL' })
      return
    }

    setOAuthCompleting(true)
    setMessage(null)
    try {
      const result = await completeGeminiOAuth(oauthFlow.flow_id, oauthCallbackURL.trim(), oauthFlow.provider_id)
      await refreshOAuthSnapshot()
      setOAuthFlow(null)
      setOAuthCallbackURL('')
      setAddAccountOpen(false)
      setMessage({ type: 'ok', text: `OAuth 完成：${result.email}` })
    } catch (error) {
      setMessage({
        type: 'err',
        text: error instanceof Error ? error.message : 'OAuth 完成失敗',
      })
    } finally {
      setOAuthCompleting(false)
    }
  }, [oauthCallbackURL, oauthFlow, refreshOAuthSnapshot, setMessage])

  const handleImportCLIToken = useCallback(async () => {
    setImportingCLI(true)
    setMessage(null)
    try {
      const result = await importGeminiCLIToken()
      await refreshOAuthSnapshot()
      setMessage({ type: 'ok', text: `已匯入 Gemini CLI Token：${result.email}` })
      setAddAccountOpen(false)
    } catch (error) {
      setMessage({ type: 'err', text: error instanceof Error ? error.message : '匯入失敗' })
    } finally {
      setImportingCLI(false)
    }
  }, [refreshOAuthSnapshot, setMessage])

  const handleImportPastedToken = useCallback(async () => {
    if (!pastedTokenJSON.trim()) return

    setImportingPasted(true)
    setMessage(null)
    try {
      const result = await importPastedToken(pastedTokenJSON.trim())
      await refreshOAuthSnapshot()
      setMessage({ type: 'ok', text: `已匯入 Token：${result.email}` })
      setAddAccountOpen(false)
      setPastedTokenJSON('')
    } catch (error) {
      setMessage({ type: 'err', text: error instanceof Error ? error.message : '匯入失敗' })
    } finally {
      setImportingPasted(false)
    }
  }, [pastedTokenJSON, refreshOAuthSnapshot, setMessage])

  const handleRemovePoolAccount = useCallback(async (email: string) => {
    setRemovingEmail(email)
    try {
      await removeOAuthPoolAccount(email)
      await refreshOAuthSnapshot()
      setMessage({ type: 'ok', text: `已移除 ${email}` })
    } catch (error) {
      setMessage({ type: 'err', text: error instanceof Error ? error.message : '移除失敗' })
    } finally {
      setRemovingEmail(null)
    }
  }, [refreshOAuthSnapshot, setMessage])

  const handleOpenAddAccount = useCallback(() => {
    setOAuthFlow(null)
    setOAuthCallbackURL('')
    setAddAccountOpen(true)
  }, [])

  const handleCloseAddAccount = useCallback(() => {
    if (oauthStarting || oauthCompleting) return
    setAddAccountOpen(false)
  }, [oauthCompleting, oauthStarting])

  return {
    providerTab,
    setProviderTab,
    oauthStatus,
    setOAuthStatus,
    oauthStarting,
    oauthCompleting,
    oauthMode,
    setOAuthMode,
    oauthFlow,
    setOAuthFlow,
    oauthCallbackURL,
    setOAuthCallbackURL,
    addAccountOpen,
    importingCLI,
    pastedTokenJSON,
    setPastedTokenJSON,
    importingPasted,
    pool,
    setPool,
    removingEmail,
    refreshPool,
    refreshOAuthSnapshot,
    handleStartOAuth,
    handleCompleteOAuth,
    handleImportCLIToken,
    handleImportPastedToken,
    handleRemovePoolAccount,
    handleOpenAddAccount,
    handleCloseAddAccount,
  }
}
