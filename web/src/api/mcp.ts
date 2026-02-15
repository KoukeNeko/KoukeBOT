import type { MCPDiscoverResponse, MCPServerData, MCPStatusEntry } from '../types'
import { BASE, fetchJSON } from './client'

export function discoverMCPServer(server: MCPServerData): Promise<MCPDiscoverResponse> {
  return fetchJSON(`${BASE}/mcp/discover`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ server }),
  })
}

export function getMCPStatus(): Promise<MCPStatusEntry[]> {
  return fetchJSON(`${BASE}/mcp/status`)
}
