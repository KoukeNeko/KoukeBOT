import { useCallback, useState } from 'react'

import {
  deleteAIStudioSession,
  getAIStudioSessionStatus,
  putAIStudioSession,
  verifyAIStudioSession,
} from '../../../api'
import type { AIStudioSessionStatus } from '../../../types'
import { parseAIStudioImport, type AIStudioImportResult } from '../utils/aistudioImport'

export type AIStudioStatusMessage = {
  type: 'ok' | 'err'
  text: string
} | null

export function useAIStudioActions() {
  const [sessionStatus, setSessionStatus] = useState<AIStudioSessionStatus | null>(null)
  const [selectedFileName, setSelectedFileName] = useState('')
  const [parsedImport, setParsedImport] = useState<AIStudioImportResult | null>(null)
  const [statusMessage, setStatusMessage] = useState<AIStudioStatusMessage>(null)
  const [uploading, setUploading] = useState(false)
  const [verifyingCurrent, setVerifyingCurrent] = useState(false)
  const [deleting, setDeleting] = useState(false)

  const refreshSessionStatus = useCallback(async () => {
    const status = await getAIStudioSessionStatus()
    setSessionStatus(status)
    return status
  }, [])

  const parseUploadFile = useCallback(async (file: File | null) => {
    if (!file) {
      setSelectedFileName('')
      setParsedImport(null)
      setStatusMessage(null)
      return
    }

    const text = await file.text()
    const parsed = parseAIStudioImport(text)
    setSelectedFileName(file.name)
    setParsedImport(parsed)
    if (parsed.warnings.length > 0) {
      setStatusMessage({
        type: 'err',
        text: `解析完成，但有風險：${parsed.warnings[0]}`,
      })
      return
    }
    setStatusMessage({
      type: 'ok',
      text: '檔案解析成功，可直接上傳驗證。',
    })
  }, [])

  const uploadVerifyApply = useCallback(async () => {
    if (!parsedImport) {
      setStatusMessage({ type: 'err', text: '請先上傳 HAR、session JSON 或 cURL。' })
      return false
    }
    setUploading(true)
    try {
      const verify = await verifyAIStudioSession({ session: parsedImport.session })
      if (!verify.verified) {
        setStatusMessage({
          type: 'err',
          text: `驗證失敗：${verify.error ?? 'unknown error'}`,
        })
        return false
      }

      await putAIStudioSession(parsedImport.session)
      await refreshSessionStatus()
      setStatusMessage({ type: 'ok', text: 'AI Studio session 已驗證並套用。' })
      return true
    } catch (error) {
      setStatusMessage({
        type: 'err',
        text: error instanceof Error ? error.message : '上傳套用失敗',
      })
      return false
    } finally {
      setUploading(false)
    }
  }, [parsedImport, refreshSessionStatus])

  const verifyCurrentSession = useCallback(async () => {
    setVerifyingCurrent(true)
    try {
      const verify = await verifyAIStudioSession()
      if (!verify.verified) {
        setStatusMessage({
          type: 'err',
          text: `目前 session 驗證失敗：${verify.error ?? 'unknown error'}`,
        })
        return false
      }
      setStatusMessage({ type: 'ok', text: '目前 session 驗證成功。' })
      return true
    } catch (error) {
      setStatusMessage({
        type: 'err',
        text: error instanceof Error ? error.message : '驗證失敗',
      })
      return false
    } finally {
      setVerifyingCurrent(false)
    }
  }, [])

  const deleteSession = useCallback(async () => {
    setDeleting(true)
    try {
      await deleteAIStudioSession()
      await refreshSessionStatus()
      setSelectedFileName('')
      setParsedImport(null)
      setStatusMessage({ type: 'ok', text: 'AI Studio session 已刪除。' })
      return true
    } catch (error) {
      setStatusMessage({
        type: 'err',
        text: error instanceof Error ? error.message : '刪除失敗',
      })
      return false
    } finally {
      setDeleting(false)
    }
  }, [refreshSessionStatus])

  return {
    sessionStatus,
    selectedFileName,
    parsedImport,
    statusMessage,
    uploading,
    verifyingCurrent,
    deleting,
    setStatusMessage,
    refreshSessionStatus,
    parseUploadFile,
    uploadVerifyApply,
    verifyCurrentSession,
    deleteSession,
  }
}
