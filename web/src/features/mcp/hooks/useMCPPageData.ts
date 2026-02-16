import { useCallback, useEffect, useState } from 'react'

import { discoverMCPServer, getMCPStatus, getSettings, updateSettings } from '../../../api'
import type { MCPServerData, MCPStatusEntry, MCPToolEntry, SettingsData } from '../../../types'

type StatusMessage = { type: 'ok' | 'err'; text: string } | null

const defaultSettings: SettingsData = {
  gemini_api_key: '',
  gemini_model: '',
  discord_token: '',
  provider_type: '',
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
}

const managedPlaywrightServer: MCPServerData = {
  id: 'playwright_mcp',
  name: 'Playwright MCP (Managed)',
  enabled: true,
  transport: 'stdio',
  command: 'playwright-mcp',
  args: ['--headless', '--no-sandbox', '--isolated'],
  env: {},
  url: '',
  headers: {},
  tool_allowlist: [],
}

function ensureManagedPlaywrightServer(settings: SettingsData): SettingsData {
  if (settings.web_tool_mode !== 'playwright_mcp') {
    return settings
  }

  const managed: MCPServerData = {
    ...managedPlaywrightServer,
    args: [...managedPlaywrightServer.args],
  }
  const currentServers = settings.mcp.servers || []
  const filtered = currentServers.filter((server) => server.id !== managed.id)
  return {
    ...settings,
    mcp: {
      ...settings.mcp,
      servers: [...filtered, managed],
    },
  }
}

export function useMCPPageData() {
  const [form, setForm] = useState<SettingsData>(defaultSettings)
  const [mcpStatus, setMCPStatus] = useState<MCPStatusEntry[]>([])
  const [saving, setSaving] = useState(false)
  const [testing, setTesting] = useState(false)
  const [loading, setLoading] = useState(true)
  const [message, setMessage] = useState<StatusMessage>(null)
  const [discovered, setDiscovered] = useState<Record<string, MCPToolEntry[]>>({})

  const refreshStatus = useCallback(async () => {
    try {
      const statusEntries = await getMCPStatus()
      setMCPStatus(statusEntries)
    } catch {
      setMCPStatus([])
    }
  }, [])

  const refreshData = useCallback(async () => {
    const [settings, statusEntries] = await Promise.all([
      getSettings(),
      getMCPStatus().catch(() => [] as MCPStatusEntry[]),
    ])
    setForm(ensureManagedPlaywrightServer(settings))
    setMCPStatus(statusEntries)
  }, [])

  useEffect(() => {
    refreshData()
      .catch((error) =>
        setMessage({
          type: 'err',
          text: error instanceof Error ? error.message : 'Failed to load MCP settings',
        }),
      )
      .finally(() => setLoading(false))
  }, [refreshData])

  const saveMCP = useCallback(async () => {
    setSaving(true)
    setMessage(null)
    try {
      const nextForm = ensureManagedPlaywrightServer(form)
      const result = await updateSettings(nextForm)
      const [updated, statusEntries] = await Promise.all([
        getSettings(),
        getMCPStatus().catch(() => [] as MCPStatusEntry[]),
      ])
      setForm(ensureManagedPlaywrightServer(updated))
      setMCPStatus(statusEntries)
      if (result.warning) {
        setMessage({ type: 'ok', text: `MCP 設定已儲存。警告：${result.warning}` })
      } else {
        setMessage({ type: 'ok', text: 'MCP settings saved successfully.' })
      }
    } catch (error) {
      setMessage({ type: 'err', text: error instanceof Error ? error.message : 'MCP save failed' })
    } finally {
      setSaving(false)
    }
  }, [form])

  const discoverServer = useCallback(
    async (server: MCPServerData): Promise<MCPToolEntry[]> => {
      try {
        const result = await discoverMCPServer(server)
        setDiscovered((prev) => ({ ...prev, [server.id]: result.tools }))
        await refreshStatus()
        setMessage({
          type: 'ok',
          text: `MCP discover 成功：${server.id}（${result.tools.length} tools）`,
        })
        return result.tools
      } catch (error) {
        const text = error instanceof Error ? error.message : 'MCP discover failed'
        setMessage({ type: 'err', text })
        throw error
      }
    },
    [refreshStatus],
  )

  const testEnabledServers = useCallback(async () => {
    const enabledServers = (form.mcp.servers || []).filter((server) => server.enabled)
    if (enabledServers.length === 0) {
      setMessage({ type: 'err', text: 'No enabled MCP servers to test.' })
      return
    }

    setTesting(true)
    setMessage(null)
    let passed = 0
    const failed: string[] = []
    const nextDiscovered: Record<string, MCPToolEntry[]> = {}

    for (const server of enabledServers) {
      try {
        const result = await discoverMCPServer(server)
        nextDiscovered[server.id] = result.tools
        passed += 1
      } catch {
        failed.push(server.id)
      }
    }

    setDiscovered((prev) => ({ ...prev, ...nextDiscovered }))
    await refreshStatus()

    if (failed.length === 0) {
      setMessage({ type: 'ok', text: `MCP test 完成：${passed}/${enabledServers.length} servers passed.` })
    } else {
      setMessage({
        type: 'err',
        text: `MCP test 完成：${passed}/${enabledServers.length} passed，failed: ${failed.join(', ')}`,
      })
    }
    setTesting(false)
  }, [form.mcp.servers, refreshStatus])

  return {
    form,
    setForm,
    mcpStatus,
    discovered,
    loading,
    saving,
    testing,
    message,
    saveMCP,
    discoverServer,
    testEnabledServers,
    refreshStatus,
  }
}
