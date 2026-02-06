import { useEffect, useState } from 'react'
import { getSettings, updateSettings, getProviderStatus } from '../api'
import type { SettingsData, ProviderStatus } from '../types'

export default function Settings() {
  const [form, setForm] = useState<SettingsData>({
    gemini_api_key: '',
    gemini_model: '',
    discord_token: '',
  })
  const [status, setStatus] = useState<ProviderStatus | null>(null)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [message, setMessage] = useState<{ type: 'ok' | 'err'; text: string } | null>(null)

  useEffect(() => {
    Promise.all([getSettings(), getProviderStatus()])
      .then(([s, st]) => {
        setForm(s)
        setStatus(st)
      })
      .catch((e) => setMessage({ type: 'err', text: e.message }))
      .finally(() => setLoading(false))
  }, [])

  const handleSave = async () => {
    setSaving(true)
    setMessage(null)
    try {
      await updateSettings(form)
      const st = await getProviderStatus()
      setStatus(st)
      const updated = await getSettings()
      setForm(updated)
      setMessage({ type: 'ok', text: 'Settings saved successfully.' })
    } catch (e) {
      setMessage({ type: 'err', text: e instanceof Error ? e.message : 'Save failed' })
    } finally {
      setSaving(false)
    }
  }

  if (loading) return <p>Loading settings...</p>

  return (
    <div>
      <h2>Settings</h2>

      {status && (
        <div className={`status-banner ${status.configured ? 'ok' : 'warn'}`}>
          {status.configured
            ? `Provider active: ${status.provider_name}`
            : 'LLM provider not configured — enter your API key below.'}
        </div>
      )}

      {message && (
        <p className={message.type === 'ok' ? 'success' : 'error'}>{message.text}</p>
      )}

      <section>
        <h3>LLM Provider</h3>

        <label className="form-field">
          <span>Gemini API Key</span>
          <input
            type="password"
            value={form.gemini_api_key}
            onChange={(e) => setForm({ ...form, gemini_api_key: e.target.value })}
            placeholder="Enter your Gemini API key"
          />
        </label>

        <label className="form-field">
          <span>Model</span>
          <input
            type="text"
            value={form.gemini_model}
            onChange={(e) => setForm({ ...form, gemini_model: e.target.value })}
            placeholder="gemini-2.5-pro-preview-06-05"
          />
        </label>
      </section>

      <section>
        <h3>Discord Bot</h3>

        <label className="form-field">
          <span>Discord Token</span>
          <input
            type="password"
            value={form.discord_token}
            onChange={(e) => setForm({ ...form, discord_token: e.target.value })}
            placeholder="Enter your Discord bot token"
          />
          <span className="hint">Discord bot requires restart to apply.</span>
        </label>
      </section>

      <button onClick={handleSave} disabled={saving} style={{ marginTop: '1rem' }}>
        {saving ? 'Saving...' : 'Save'}
      </button>
    </div>
  )
}
