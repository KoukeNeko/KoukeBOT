import DiscordSection from '../../../pages/settings/DiscordSection'
import MCPSection from '../../../pages/settings/MCPSection'
import MemorySection from '../../../pages/settings/MemorySection'
import PromptGuardSection from '../../../pages/settings/PromptGuardSection'
import ProviderSection from '../../../pages/settings/ProviderSection'
import RuntimeMetricsCard from '../../../pages/settings/RuntimeMetricsCard'
import ScheduledTasksSection from '../../../pages/settings/ScheduledTasksSection'
import TimeLocationSection from '../../../pages/settings/TimeLocationSection'
import ToolResilienceSection from '../../../pages/settings/ToolResilienceSection'
import ToolsSection from '../../../pages/settings/ToolsSection'
import type { useSettingsData } from '../hooks/useSettingsData'

type ViewModel = ReturnType<typeof useSettingsData>

type Props = {
  vm: ViewModel
}

export default function SettingsPage({ vm }: Props) {
  if (vm.loading) return <p>Loading settings...</p>

  return (
    <div className="settings-page">
      <h2>Settings</h2>

      {vm.status && (
        <div className={`status-banner ${vm.status.configured ? 'ok' : 'warn'}`}>
          {vm.status.configured
            ? `Provider active: ${vm.status.provider_name}`
            : 'LLM provider not configured — set API key or use OAuth below.'}
        </div>
      )}

      {vm.message && (
        <p className={vm.message.type === 'ok' ? 'success message' : 'error message'}>
          {vm.message.text}
        </p>
      )}

      <RuntimeMetricsCard runtimeMetrics={vm.runtimeMetrics} />

      <ScheduledTasksSection
        tasks={vm.scheduledTasks}
        loading={vm.scheduledTasksLoading}
        error={vm.scheduledTasksError}
        onRefresh={() => {
          void vm.refreshScheduledTasks()
        }}
      />

      <ProviderSection
        form={vm.form}
        setForm={vm.setForm}
        modelOptions={vm.modelOptions}
        providerTab={vm.oauth.providerTab}
        setProviderTab={vm.oauth.setProviderTab}
        oauthStatus={vm.oauth.oauthStatus}
        pool={vm.oauth.pool}
        removingEmail={vm.oauth.removingEmail}
        onRemovePoolAccount={vm.oauth.handleRemovePoolAccount}
        saving={vm.saving}
        onSave={vm.handleSave}
        addAccountOpen={vm.oauth.addAccountOpen}
        onOpenAddAccount={vm.oauth.handleOpenAddAccount}
        onCloseAddAccount={vm.oauth.handleCloseAddAccount}
        oauthStarting={vm.oauth.oauthStarting}
        oauthCompleting={vm.oauth.oauthCompleting}
        oauthMode={vm.oauth.oauthMode}
        setOAuthMode={vm.oauth.setOAuthMode}
        oauthFlow={vm.oauth.oauthFlow}
        oauthCallbackURL={vm.oauth.oauthCallbackURL}
        setOAuthCallbackURL={vm.oauth.setOAuthCallbackURL}
        onStartOAuth={vm.oauth.handleStartOAuth}
        onCompleteOAuth={vm.oauth.handleCompleteOAuth}
        message={vm.message}
        pastedTokenJSON={vm.oauth.pastedTokenJSON}
        setPastedTokenJSON={vm.oauth.setPastedTokenJSON}
        importingPasted={vm.oauth.importingPasted}
        onImportPastedToken={vm.oauth.handleImportPastedToken}
        importingCLI={vm.oauth.importingCLI}
        onImportCLIToken={vm.oauth.handleImportCLIToken}
      />

      <DiscordSection
        form={vm.form}
        setForm={vm.setForm}
        personaOptions={vm.personaOptions}
        saving={vm.saving}
        onSave={() => {
          void vm.handleSave()
        }}
      />

      <PromptGuardSection
        form={vm.form}
        setForm={vm.setForm}
        saving={vm.saving}
        onSave={() => {
          void vm.handleSave()
        }}
      />

      <ToolResilienceSection
        form={vm.form}
        setForm={vm.setForm}
        saving={vm.saving}
        onSave={() => {
          void vm.handleSave()
        }}
      />

      <MCPSection
        form={vm.form}
        setForm={vm.setForm}
        statuses={vm.mcpStatus}
        saving={vm.saving}
        onSave={() => {
          void vm.handleSave()
        }}
        onDiscover={vm.handleDiscoverMCPServer}
      />

      <ToolsSection
        form={vm.form}
        setForm={vm.setForm}
        saving={vm.saving}
        onSave={() => {
          void vm.handleSave()
        }}
      />

      <TimeLocationSection
        form={vm.form}
        setForm={vm.setForm}
        saving={vm.saving}
        onSave={() => {
          void vm.handleSave()
        }}
      />

      <MemorySection
        form={vm.form}
        setForm={vm.setForm}
        modelOptions={vm.modelOptions}
        saving={vm.saving}
        onSave={() => {
          void vm.handleSave()
        }}
      />
    </div>
  )
}
