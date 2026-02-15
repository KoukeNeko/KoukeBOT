import { useMemo, useState, type Dispatch, type SetStateAction } from 'react'
import Select from '../../../components/ui/Select'
import Checkbox from '../../../components/ui/Checkbox'
import type { SettingsData, MCPServerData, MCPToolEntry, MCPStatusEntry } from '../../../types'

type Props = {
  form: SettingsData
  setForm: Dispatch<SetStateAction<SettingsData>>
  statuses: MCPStatusEntry[]
  saving: boolean
  onSave: () => void
  onDiscover: (server: MCPServerData) => Promise<MCPToolEntry[]>
}

function mapToLines(values: Record<string, string>): string {
  return Object.entries(values)
    .map(([key, value]) => `${key}=${value}`)
    .join('\n')
}

function linesToMap(raw: string): Record<string, string> {
  const result: Record<string, string> = {}
  raw
    .split(/\r?\n/g)
    .map((line) => line.trim())
    .filter(Boolean)
    .forEach((line) => {
      const index = line.indexOf('=')
      if (index <= 0) return;
      const key = line.slice(0, index).trim();
      const value = line.slice(index + 1).trim();
      if (!key) return;
      result[key] = value;
    });
  return result
}

function argsToText(args: string[]): string {
  return args.join('\n')
}

function textToArgs(raw: string): string[] {
  return raw
    .split(/\r?\n/g)
    .map((line) => line.trim())
    .filter(Boolean);
}

function nextServerID(existing: MCPServerData[]): string {
  const base = 'server'
  let n = existing.length + 1
  while (existing.some((s) => s.id === `${base}_${n}`)) {
    n += 1;
  }
  return `${base}_${n}`
}

export default function MCPSection({ form, setForm, statuses, saving, onSave, onDiscover }: Props) {
  const [discoveringID, setDiscoveringID] = useState<string | null>(null)
  const [discovered, setDiscovered] = useState<Record<string, MCPToolEntry[]>>({})

  const statusMap = useMemo(() => {
    const map = new Map<string, MCPStatusEntry>()
    statuses.forEach((item) => map.set(item.server_id, item))
    return map
  }, [statuses])

  const servers = form.mcp.servers || []

  const updateMCP = (next: SettingsData["mcp"]) => {
    setForm({ ...form, mcp: next })
  }

  const updateServer = (index: number, nextServer: MCPServerData) => {
    const nextServers = [...servers];
    nextServers[index] = nextServer;
    updateMCP({ ...form.mcp, servers: nextServers });
  };

  const handleAddServer = () => {
    const serverID = nextServerID(servers);
    updateMCP({
      ...form.mcp,
      servers: [
        ...servers,
        {
          id: serverID,
          name: serverID,
          enabled: true,
          transport: 'stdio',
          command: '',
          args: [],
          env: {},
          url: '',
          headers: {},
          tool_allowlist: [],
        },
      ],
    })
  }

  const handleRemoveServer = (index: number) => {
    const nextServers = servers.filter((_, i) => i !== index);
    updateMCP({ ...form.mcp, servers: nextServers });
  };

  const handleDiscover = async (server: MCPServerData) => {
    setDiscoveringID(server.id)
    try {
      const tools = await onDiscover(server)
      setDiscovered((prev) => ({ ...prev, [server.id]: tools }))
    } finally {
      setDiscoveringID(null)
    }
  }

  const toggleAllowlist = (serverIndex: number, toolName: string, checked: boolean) => {
    const server = servers[serverIndex]
    const allowlist = new Set(server.tool_allowlist || [])
    if (checked) {
      allowlist.add(toolName);
    } else {
      allowlist.delete(toolName);
    }
    updateServer(serverIndex, {
      ...server,
      tool_allowlist: Array.from(allowlist),
    })
  }

  return (
    <section className="settings-section">
      <h3 className="settings-section-title">MCP</h3>

      <div className="form-field-checkbox-row">
        <Checkbox
          checked={form.mcp.enabled}
          onChange={(checked) => updateMCP({ ...form.mcp, enabled: checked })}
          label="啟用 MCP（Model Context Protocol）"
        />
      </div>
      <div className="form-field-checkbox-row">
        <Checkbox
          checked={form.mcp.verbose_logging}
          onChange={(checked) => updateMCP({ ...form.mcp, verbose_logging: checked })}
          label="啟用 MCP 詳細 Log（MCP prefix）"
        />
      </div>

      <label className="form-field">
        <span>Discovery Timeout (秒)</span>
        <input
          type="number"
          min={1}
          max={30}
          value={form.mcp.discovery_timeout_seconds}
          onChange={(e) =>
            updateMCP({
              ...form.mcp,
              discovery_timeout_seconds: Number.parseInt(e.target.value || "0", 10),
            })
          }
        />
      </label>

      <label className="form-field">
        <span>Tool Call Timeout (秒)</span>
        <input
          type="number"
          min={1}
          max={120}
          value={form.mcp.tool_call_timeout_seconds}
          onChange={(e) =>
            updateMCP({
              ...form.mcp,
              tool_call_timeout_seconds: Number.parseInt(e.target.value || "0", 10),
            })
          }
        />
      </label>

      <div className="scheduled-header-row">
        <h4 className="settings-subsection-title">Servers</h4>
        <button type="button" className="btn-outline" onClick={handleAddServer}>
          新增 Server
        </button>
      </div>

      {servers.length === 0 && <p className="hint">尚未設定 MCP server。</p>}

      <div className="mcp-server-list">
        {servers.map((server, idx) => {
          const status = statusMap.get(server.id);
          const discoveredTools = discovered[server.id] || [];
          return (
            <article className="mcp-server-card" key={server.id || idx}>
              <div className="mcp-server-card-header">
                <strong>{server.name || server.id || `server-${idx + 1}`}</strong>
                <div className="mcp-server-actions">
                  {status && (
                    <span className={`mcp-status-chip ${status.reachable ? "ok" : "err"}`}>
                      {status.reachable ? "reachable" : "offline"}
                    </span>
                  )}
                  <button type="button" className="btn-outline" onClick={() => handleRemoveServer(idx)}>
                    刪除
                  </button>
                </div>
              </div>

              <label className="form-field">
                <span>ID</span>
                <input
                  value={server.id}
                  onChange={(e) => updateServer(idx, { ...server, id: e.target.value.trim() })}
                  placeholder="server id (a-z0-9_-)"
                />
              </label>

              <label className="form-field">
                <span>Name</span>
                <input
                  value={server.name}
                  onChange={(e) => updateServer(idx, { ...server, name: e.target.value })}
                  placeholder="Human readable name"
                />
              </label>

              <div className="form-field-checkbox-row">
                <Checkbox
                  checked={server.enabled}
                  onChange={(checked) => updateServer(idx, { ...server, enabled: checked })}
                  label="啟用此 server"
                />
              </div>

              <label className="form-field">
                <span>Transport</span>
                <Select
                  value={server.transport}
                  onChange={(value) => updateServer(idx, { ...server, transport: value as 'stdio' | 'http' })}
                  options={[
                    { value: 'stdio', label: 'stdio' },
                    { value: 'http', label: 'http (streamable)' },
                  ]}
                />
              </label>

              {server.transport === 'stdio' ? (
                <>
                  <label className="form-field">
                    <span>Command</span>
                    <input
                      value={server.command}
                      onChange={(e) => updateServer(idx, { ...server, command: e.target.value })}
                      placeholder="npx"
                    />
                  </label>

                  <label className="form-field">
                    <span>Args（每行一個）</span>
                    <textarea
                      rows={3}
                      value={argsToText(server.args || [])}
                      onChange={(e) => updateServer(idx, { ...server, args: textToArgs(e.target.value) })}
                      placeholder="-y\n@modelcontextprotocol/server-filesystem"
                    />
                  </label>

                  <label className="form-field">
                    <span>Env（KEY=VALUE 每行一個）</span>
                    <textarea
                      rows={4}
                      value={mapToLines(server.env || {})}
                      onChange={(e) => updateServer(idx, { ...server, env: linesToMap(e.target.value) })}
                    />
                  </label>
                </>
              ) : (
                <>
                  <label className="form-field">
                    <span>URL</span>
                    <input
                      value={server.url}
                      onChange={(e) => updateServer(idx, { ...server, url: e.target.value })}
                      placeholder="https://example.com/mcp"
                    />
                  </label>

                  <label className="form-field">
                    <span>Headers（KEY=VALUE 每行一個）</span>
                    <textarea
                      rows={4}
                      value={mapToLines(server.headers || {})}
                      onChange={(e) => updateServer(idx, { ...server, headers: linesToMap(e.target.value) })}
                    />
                  </label>
                </>
              )}

              <div className="mcp-discover-row">
                <button
                  type="button"
                  className="btn-outline"
                  disabled={discoveringID === server.id}
                  onClick={() => {
                    void handleDiscover(server);
                  }}
                >
                  {discoveringID === server.id ? 'Discovering...' : 'Discover tools'}
                </button>
                {status?.last_error && <span className="hint">{status.last_error}</span>}
              </div>

              {discoveredTools.length > 0 && (
                <div className="mcp-tools-grid">
                  {discoveredTools.map((tool) => {
                    const checked = (server.tool_allowlist || []).includes(tool.name);
                    return (
                      <div key={tool.name} className="mcp-tool-item">
                        <Checkbox
                          checked={checked}
                          onChange={(next) => toggleAllowlist(idx, tool.name, next)}
                          label={tool.name}
                        />
                        {tool.description && <span className="hint">{tool.description}</span>}
                      </div>
                    )
                  })}
                </div>
              )}
            </article>
          )
        })}
      </div>

      <button onClick={onSave} disabled={saving} className="btn-save-bottom">
        {saving ? 'Saving...' : 'Save'}
      </button>
    </section>
  )
}
