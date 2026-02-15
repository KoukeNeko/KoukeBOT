import type { useMCPPageData } from '../hooks/useMCPPageData'
import MCPSection from './MCPSection'

type ViewModel = ReturnType<typeof useMCPPageData>

type Props = {
  vm: ViewModel
}

export default function MCPPage({ vm }: Props) {
  if (vm.loading) return <p>Loading MCP settings...</p>

  return (
    <div className="settings-page">
      <h2>MCP</h2>

      {vm.message && (
        <p className={vm.message.type === 'ok' ? 'success message' : 'error message'}>
          {vm.message.text}
        </p>
      )}

      <div className="scheduled-header-row">
        <h4 className="settings-subsection-title">MCP Test</h4>
        <div style={{ display: 'flex', gap: '0.5rem' }}>
          <button
            type="button"
            className="btn-outline"
            disabled={vm.testing}
            onClick={() => {
              void vm.testEnabledServers()
            }}
          >
            {vm.testing ? 'Testing...' : 'Test Enabled Servers'}
          </button>
          <button
            type="button"
            className="btn-outline"
            onClick={() => {
              void vm.refreshStatus()
            }}
          >
            Refresh Status
          </button>
        </div>
      </div>

      <MCPSection
        form={vm.form}
        setForm={vm.setForm}
        statuses={vm.mcpStatus}
        saving={vm.saving}
        onSave={() => {
          void vm.saveMCP()
        }}
        onDiscover={vm.discoverServer}
      />
    </div>
  )
}
