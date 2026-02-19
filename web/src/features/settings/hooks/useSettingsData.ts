import { useCallback, useEffect, useMemo, useState } from 'react'

import {
  getProviderStatus,
  getRuntimeMetrics,
  getSettings,
  listModels,
  listPersonas,
  updateSettings,
} from '../../../api'
import type {
  ModelEntry,
  PersonaSummary,
  ProviderStatus,
  RuntimeMetrics,
  SettingsData,
} from '../../../types'
import { useAIStudioActions } from './useAIStudioActions'
import { useOAuthActions, type StatusMessage } from './useOAuthActions'

const defaultProviderModels: Record<string, string> = {
  'gemini-api-key': 'gemini-api-key/gemini-2.5-pro',
  'google-antigravity': 'google-antigravity/claude-opus-4-6-thinking',
  'google-gemini-cli': 'google-gemini-cli/gemini-3-pro-preview',
  'aistudio-rpc': 'aistudio-rpc/gemini-3-pro-preview',
}

function extractProviderFromModelID(modelID: string): string {
  const idx = modelID.indexOf('/')
  if (idx <= 0) return ''
  return modelID.slice(0, idx).trim()
}

function toLegacyModel(modelID: string): string {
  return modelID.includes('/') ? modelID.split('/').slice(1).join('/') : modelID
}

function defaultModelForProvider(providerID: string): string {
  return defaultProviderModels[providerID] ?? defaultProviderModels['google-antigravity']
}

const defaultSettings: SettingsData = {
  gemini_api_key: '',
  gemini_model: '',
  discord_token: '',
  provider_type: '',
  provider_id: 'google-antigravity',
  model_id: 'google-antigravity/claude-opus-4-6-thinking',
  fallback_enabled: false,
  fallback_model_id: '',
  endpoint_policy: 'primary_then_sandbox',
  brave_api_key: '',
  web_search_enabled: false,
  discord_use_reply: false,
  perplexity_api_key: '',
  search_provider: 'auto',
  web_fetch_enabled: false,
  web_tool_mode: 'classic',
  timezone: 'auto',
  time_format: 'auto',
  location: '',
  memory_extraction_enabled: false,
  memory_extraction_model: '',
  memory_recent_rounds: 50,
  log_channel_id: '',
  discord_persona_id: '',
  discord_observe_enabled: false,
  discord_member_intent_enabled: false,
  discord_presence_intent_enabled: false,
  discord_observe_max_results: 25,
  discord_observe_message_scope: 'current_guild',
  tool_guard_enabled: false,
  max_tool_rounds: 5,
  max_web_search_calls_per_turn: 2,
  force_final_answer_after_tool_limit: true,
  transient_recovery_wait_enabled: true,
  transient_recovery_wait_max_seconds: 5,
  discord_show_internal_tool_errors: true,
  scheduled_monitor_max_per_user: 3,
  mcp: {
    enabled: false,
    verbose_logging: false,
    discovery_timeout_seconds: 10,
    tool_call_timeout_seconds: 20,
    servers: [],
  },
  persona_lock_every_turns: 4,
  triplet_output_enabled: true,
  analysis_visibility: 'visible',
  negative_constraint_enabled: true,
  negative_constraint_default:
    'You are FORBIDDEN from switching into a generic assistant tone. Keep persona-specific stance, diction, and attitude.',
  fallback_model_enabled: false,
  fallback_model: '',
  request_queue_enabled: true,
  request_queue_capacity: 10,
  request_queue_timeout_seconds: 120,
}

export function useSettingsData() {
  const [form, setForm] = useState<SettingsData>(defaultSettings)
  const [models, setModels] = useState<ModelEntry[]>([])
  const [personas, setPersonas] = useState<PersonaSummary[]>([])
  const [status, setStatus] = useState<ProviderStatus | null>(null)
  const [runtimeMetrics, setRuntimeMetrics] = useState<RuntimeMetrics | null>(null)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [message, setMessage] = useState<StatusMessage>(null)

  const oauth = useOAuthActions({
    setMessage,
    setStatus: (next) => setStatus(next),
  })
  const aistudio = useAIStudioActions()

  const refreshRuntimeMetrics = useCallback(async () => {
    try {
      const metrics = await getRuntimeMetrics()
      setRuntimeMetrics(metrics)
    } catch {
      // Ignore metrics errors for settings UX.
    }
  }, [])

  const selectedProviderID = useMemo(() => {
    if (form.provider_id) return form.provider_id
    if (form.provider_type === 'api-key') return 'gemini-api-key'
    return 'google-antigravity'
  }, [form.provider_id, form.provider_type])

  useEffect(() => {
    Promise.all([
      getSettings(),
      getProviderStatus(),
      getRuntimeMetrics().catch(() => null as RuntimeMetrics | null),
      oauth.refreshOAuthSnapshot().catch(() => ({ oauth: null, poolEntries: [] })),
      aistudio.refreshSessionStatus().catch(() => null),
      listPersonas(),
    ])
      .then(async ([settings, currentStatus, metrics, oauthSnapshot, aiStudioStatus, personaList]) => {
        setForm(settings)
        setStatus(currentStatus)
        setRuntimeMetrics(metrics)
        if (aiStudioStatus) {
          // Session status is already pushed into hook state by refreshSessionStatus.
        }
        if (oauthSnapshot.oauth) {
          oauth.setOAuthStatus(oauthSnapshot.oauth)
        }
        oauth.setPool(oauthSnapshot.poolEntries)
        setPersonas(personaList)

        if (settings.provider_id === 'gemini-api-key' || settings.provider_type === 'api-key') {
          oauth.setProviderTab('api-key')
        } else {
          oauth.setProviderTab('oauth')
        }

        const initialProviderID = settings.provider_id || settings.provider_type || 'google-antigravity'
        return listModels(initialProviderID)
      })
      .then((modelList) => {
        setModels(modelList)
      })
      .catch((error) => setMessage({ type: 'err', text: error instanceof Error ? error.message : 'Failed to load settings' }))
      .finally(() => setLoading(false))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    if (loading) return
    let cancelled = false
    listModels(selectedProviderID)
      .then((modelList) => {
        if (!cancelled) setModels(modelList)
      })
      .catch(() => {
        // Ignore model refresh failures while editing provider selection.
      })
    return () => {
      cancelled = true
    }
  }, [loading, selectedProviderID])

  useEffect(() => {
    const id = window.setInterval(() => {
      void refreshRuntimeMetrics()
    }, 10_000)
    return () => window.clearInterval(id)
  }, [refreshRuntimeMetrics])

  const handleSave = useCallback(async (overrideProviderType?: string) => {
    setSaving(true)
    setMessage(null)
    try {
      const payload = { ...form }
      if (overrideProviderType) {
        payload.provider_type = overrideProviderType
        if (overrideProviderType === 'api-key') {
          payload.provider_id = 'gemini-api-key'
          if (extractProviderFromModelID(payload.model_id) !== 'gemini-api-key') {
            payload.model_id = defaultModelForProvider('gemini-api-key')
          }
          payload.gemini_model = toLegacyModel(payload.model_id || payload.gemini_model)
        } else {
          const oauthProviderID =
            payload.provider_id === 'google-gemini-cli' ||
            payload.provider_id === 'google-antigravity' ||
            payload.provider_id === 'aistudio-rpc'
              ? payload.provider_id
              : 'google-antigravity'
          payload.provider_id = oauthProviderID
          if (extractProviderFromModelID(payload.model_id) !== oauthProviderID) {
            payload.model_id = defaultModelForProvider(oauthProviderID)
          }
          payload.gemini_model = toLegacyModel(payload.model_id || payload.gemini_model)
        }
      }

      const result = await updateSettings(payload)
      const [nextStatus, updated] = await Promise.all([
        getProviderStatus(),
        getSettings(),
      ])
      const modelList = await listModels(updated.provider_id || updated.provider_type)
      setStatus(nextStatus)
      setForm(updated)
      setModels(modelList)

      if (result.warning) {
        setMessage({ type: 'ok', text: `設定已儲存。警告：${result.warning}` })
      } else {
        setMessage({ type: 'ok', text: 'Settings saved successfully.' })
      }
    } catch (error) {
      setMessage({ type: 'err', text: error instanceof Error ? error.message : 'Save failed' })
    } finally {
      setSaving(false)
    }
  }, [form])

  const modelOptions = useMemo(() => {
    const scoped = models.filter((model) => {
      const modelProviderID = (model.provider_id || '').trim()
      if (!modelProviderID) return true
      return modelProviderID === selectedProviderID
    })
    if (scoped.length > 0) {
      return scoped.map((model) => ({
        value: model.id,
        label: `${model.display_name}${model.reasoning ? ' · Reasoning' : ''}${model.supports_multimodal ? ' · MM' : ''}${model.supports_tools ? ' · Tools' : ''}`,
      }))
    }
    const fallback = form.model_id || form.gemini_model || defaultModelForProvider(selectedProviderID)
    return [{ value: fallback, label: fallback }]
  }, [form.gemini_model, form.model_id, models, selectedProviderID])

  const personaOptions = useMemo(
    () => personas.map((persona) => ({ value: persona.id, label: `${persona.name} (${persona.id})` })),
    [personas],
  )

  return {
    form,
    setForm,
    models,
    personas,
    status,
    runtimeMetrics,
    loading,
    saving,
    message,
    modelOptions,
    personaOptions,
    handleSave,
    oauth,
    aistudio,
  }
}
