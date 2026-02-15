import MCPPage from '../features/mcp/components/MCPPage'
import { useMCPPageData } from '../features/mcp/hooks/useMCPPageData'

import './Settings.css'

export default function MCP() {
  const vm = useMCPPageData()
  return <MCPPage vm={vm} />
}
