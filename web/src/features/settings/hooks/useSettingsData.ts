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
import { useOAuthActions, type StatusMessage } from './useOAuthActions'

const defaultSettings: SettingsData = {
  gemini_api_key: '',
  gemini_model: '',
  discord_token: '',
  provider_type: '',
  provider_id: 'google-gemini-cli',
  model_id: 'google-gemini-cli/gemini-3-pro-preview',
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

  const refreshRuntimeMetrics = useCallback(async () => {
    try {
      const metrics = await getRuntimeMetrics()
      setRuntimeMetrics(metrics)
    } catch {
      // Ignore metrics errors for settings UX.
    }
  }, [])

  useEffect(() => {
    Promise.all([
      getSettings(),
      getProviderStatus(),
      getRuntimeMetrics().catch(() => null as RuntimeMetrics | null),
      oauth.refreshOAuthSnapshot().catch(() => ({ oauth: null, poolEntries: [] })),
      listModels(),
      listPersonas(),
    ])
      .then(([settings, currentStatus, metrics, oauthSnapshot, modelList, personaList]) => {
        setForm(settings)
        setStatus(currentStatus)
        setRuntimeMetrics(metrics)
        if (oauthSnapshot.oauth) {
          oauth.setOAuthStatus(oauthSnapshot.oauth)
        }
        oauth.setPool(oauthSnapshot.poolEntries)
        setModels(modelList)
        setPersonas(personaList)

        if (settings.provider_id === 'gemini-api-key' || settings.provider_type === 'api-key') {
          oauth.setProviderTab('api-key')
        } else {
          oauth.setProviderTab('oauth')
        }
      })
      .catch((error) => setMessage({ type: 'err', text: error instanceof Error ? error.message : 'Failed to load settings' }))
      .finally(() => setLoading(false))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

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
        } else if (payload.provider_id === 'gemini-api-key' || !payload.provider_id) {
          payload.provider_id = 'google-gemini-cli'
        }
      }

      const result = await updateSettings(payload)
      const [nextStatus, updated, modelList] = await Promise.all([
        getProviderStatus(),
        getSettings(),
        listModels(),
      ])
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

  const modelOptions = useMemo(
    () =>
      models.length > 0
        ? models.map((model) => ({
            value: model.id,
            label: `${model.display_name}${model.reasoning ? ' · Reasoning' : ''}${model.supports_multimodal ? ' · MM' : ''}${model.supports_tools ? ' · Tools' : ''}`,
          }))
        : [{ value: form.model_id || form.gemini_model || 'google-gemini-cli/gemini-3-pro-preview', label: form.model_id || form.gemini_model || 'google-gemini-cli/gemini-3-pro-preview' }],
    [form.gemini_model, form.model_id, models],
  )

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
  }
}
