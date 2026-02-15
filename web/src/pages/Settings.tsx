import SettingsPage from '../features/settings/components/SettingsPage'
import { useSettingsData } from '../features/settings/hooks/useSettingsData'

import './Settings.css'

export default function Settings() {
  const vm = useSettingsData()
  return <SettingsPage vm={vm} />
}
