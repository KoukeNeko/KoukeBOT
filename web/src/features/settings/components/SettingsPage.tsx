import { useMemo, useState } from 'react'

import DiscordSection from '../../../pages/settings/DiscordSection'
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

type SettingsSectionID =
  | 'runtime_metrics'
  | 'scheduled_tasks'
  | 'provider'
  | 'discord'
  | 'tools'
  | 'tool_resilience'
  | 'prompt_guard'
  | 'memory'
  | 'time_location'

type SettingsNavGroup = {
  id: string
  label: string
  items: Array<{ id: SettingsSectionID; label: string }>
}

const settingsNav: SettingsNavGroup[] = [
  {
    id: 'overview',
    label: '總覽',
    items: [
      { id: 'runtime_metrics', label: 'Runtime Metrics' },
      { id: 'scheduled_tasks', label: '排程任務' },
    ],
  },
  {
    id: 'provider',
    label: '模型與帳號',
    items: [{ id: 'provider', label: 'LLM Provider' }],
  },
  {
    id: 'runtime',
    label: 'Bot Runtime',
    items: [
      { id: 'discord', label: 'Discord Bot' },
      { id: 'tools', label: 'Tools' },
      { id: 'tool_resilience', label: 'Tool Guard / Resilience' },
    ],
  },
  {
    id: 'behavior',
    label: '行為與記憶',
    items: [
      { id: 'prompt_guard', label: 'Prompt Guard' },
      { id: 'memory', label: 'Memory' },
    ],
  },
  {
    id: 'locale',
    label: '時間與地區',
    items: [{ id: 'time_location', label: 'Time / Location' }],
  },
]

export default function SettingsPage({ vm }: Props) {
  const [activeGroup, setActiveGroup] = useState(settingsNav[0].id)
  const [activeSection, setActiveSection] = useState<SettingsSectionID>(settingsNav[0].items[0].id)

  const activeMeta = useMemo(() => {
    for (const group of settingsNav) {
      for (const item of group.items) {
        if (item.id === activeSection) {
          return {
            groupID: group.id,
            groupLabel: group.label,
            sectionLabel: item.label,
          }
        }
      }
    }
    return {
      groupID: settingsNav[0].id,
      groupLabel: settingsNav[0].label,
      sectionLabel: settingsNav[0].items[0].label,
    }
  }, [activeSection])

  const saveSettings = () => {
    void vm.handleSave()
  }

  const renderSection = (section: SettingsSectionID) => {
    switch (section) {
      case 'runtime_metrics':
        return <RuntimeMetricsCard runtimeMetrics={vm.runtimeMetrics} />
      case 'scheduled_tasks':
        return (
          <ScheduledTasksSection
            tasks={vm.scheduledTasks}
            loading={vm.scheduledTasksLoading}
            error={vm.scheduledTasksError}
            onRefresh={() => {
              void vm.refreshScheduledTasks()
            }}
          />
        )
      case 'provider':
        return (
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
        )
      case 'discord':
        return (
          <DiscordSection
            form={vm.form}
            setForm={vm.setForm}
            personaOptions={vm.personaOptions}
            saving={vm.saving}
            onSave={saveSettings}
          />
        )
      case 'tools':
        return (
          <ToolsSection
            form={vm.form}
            setForm={vm.setForm}
            saving={vm.saving}
            onSave={saveSettings}
          />
        )
      case 'tool_resilience':
        return (
          <ToolResilienceSection
            form={vm.form}
            setForm={vm.setForm}
            saving={vm.saving}
            onSave={saveSettings}
          />
        )
      case 'prompt_guard':
        return (
          <PromptGuardSection
            form={vm.form}
            setForm={vm.setForm}
            saving={vm.saving}
            onSave={saveSettings}
          />
        )
      case 'memory':
        return (
          <MemorySection
            form={vm.form}
            setForm={vm.setForm}
            modelOptions={vm.modelOptions}
            saving={vm.saving}
            onSave={saveSettings}
          />
        )
      case 'time_location':
        return (
          <TimeLocationSection
            form={vm.form}
            setForm={vm.setForm}
            saving={vm.saving}
            onSave={saveSettings}
          />
        )
      default:
        return null
    }
  }

  const handleSelectGroup = (groupID: string) => {
    const nextGroup = settingsNav.find((group) => group.id === groupID)
    if (!nextGroup) return
    setActiveGroup(groupID)
    setActiveSection(nextGroup.items[0].id)
  }

  const handleSelectSection = (groupID: string, sectionID: SettingsSectionID) => {
    setActiveGroup(groupID)
    setActiveSection(sectionID)
  }

  if (vm.loading) return <p>Loading settings...</p>

  return (
    <div className="settings-page">
      <div className="settings-shell">
        <aside className="settings-sidebar-panel">
          <div className="settings-sidebar-heading">設定分類</div>
          {settingsNav.map((group) => (
            <div
              key={group.id}
              className={`settings-sidebar-group ${group.id === activeGroup ? 'active' : ''}`}
            >
              <button
                className="settings-sidebar-group-button"
                type="button"
                onClick={() => handleSelectGroup(group.id)}
              >
                {group.label}
              </button>
              <div className="settings-sidebar-submenu">
                {group.items.map((item) => (
                  <button
                    key={item.id}
                    className={`settings-sidebar-subitem ${item.id === activeSection ? 'active' : ''}`}
                    type="button"
                    onClick={() => handleSelectSection(group.id, item.id)}
                  >
                    {item.label}
                  </button>
                ))}
              </div>
            </div>
          ))}
        </aside>

        <div className="settings-content-panel">
          <h2>Settings</h2>

          <div className="settings-active-context">
            <span>{activeMeta.groupLabel}</span>
            <span>/</span>
            <span>{activeMeta.sectionLabel}</span>
          </div>

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

          {renderSection(activeSection)}
        </div>
      </div>
    </div>
  )
}
