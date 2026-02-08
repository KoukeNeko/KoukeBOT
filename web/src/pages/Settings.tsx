import { useEffect, useState, useRef } from 'react'
import { getSettings, updateSettings, getProviderStatus, detectGeminiCLI, startGeminiOAuth, getOAuthStatus, listModels } from '../api'
import type { SettingsData, ProviderStatus, OAuthStatus, ModelEntry } from '../types'

type ProviderTab = 'api-key' | 'oauth'

export default function Settings() {
  const [form, setForm] = useState<SettingsData>({
    gemini_api_key: '',
    gemini_model: '',
    discord_token: '',
    provider_type: '',
  })
  const [models, setModels] = useState<ModelEntry[]>([])
  const [status, setStatus] = useState<ProviderStatus | null>(null)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [message, setMessage] = useState<{ type: 'ok' | 'err'; text: string } | null>(null)

  // OAuth state
  const [providerTab, setProviderTab] = useState<ProviderTab>('api-key')
  const [oauthStatus, setOAuthStatus] = useState<OAuthStatus | null>(null)
  const [detecting, setDetecting] = useState(false)
  const [detectedClientID, setDetectedClientID] = useState('')
  const [detectError, setDetectError] = useState('')
  const [manualClientID, setManualClientID] = useState('')
  const [manualClientSecret, setManualClientSecret] = useState('')
  const [oauthStarting, setOAuthStarting] = useState(false)
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null)

  useEffect(() => {
    Promise.all([getSettings(), getProviderStatus(), getOAuthStatus(), listModels()])
      .then(([s, st, oauth, modelList]) => {
        setForm(s)
        setStatus(st)
        setOAuthStatus(oauth)
        setModels(modelList)
        if (s.provider_type === 'oauth' || (oauth.authenticated && st.provider_name === 'gemini-oauth')) {
          setProviderTab('oauth')
        } else if (s.provider_type === 'api-key') {
          setProviderTab('api-key')
        }
      })
      .catch((e) => setMessage({ type: 'err', text: e.message }))
      .finally(() => setLoading(false))
  }, [])

  // Stop polling on unmount.
  useEffect(() => {
    return () => {
      if (pollRef.current) clearInterval(pollRef.current)
    }
  }, [])

  const handleSave = async (overrideProviderType?: string) => {
    setSaving(true)
    setMessage(null)
    try {
      const payload = { ...form }
      if (overrideProviderType) {
        payload.provider_type = overrideProviderType
      }
      await updateSettings(payload)
      const [st, updated, modelList] = await Promise.all([getProviderStatus(), getSettings(), listModels()])
      setStatus(st)
      setForm(updated)
      setModels(modelList)
      setMessage({ type: 'ok', text: 'Settings saved successfully.' })
    } catch (e) {
      setMessage({ type: 'err', text: e instanceof Error ? e.message : 'Save failed' })
    } finally {
      setSaving(false)
    }
  }

  const handleDetect = async () => {
    setDetecting(true)
    setDetectError('')
    setDetectedClientID('')
    try {
      const result = await detectGeminiCLI()
      if (result.found && result.client_id) {
        setDetectedClientID(result.client_id)
        // Auto-fill the client ID so it persists and is used for OAuth
        // Client secret is not stored in Gemini CLI config
        setManualClientID(result.client_id)
      } else {
        setDetectError(result.error || 'Gemini CLI not found.')
      }
    } catch (e) {
      setDetectError(e instanceof Error ? e.message : 'Detection failed')
    } finally {
      setDetecting(false)
    }
  }

  const handleOAuthLogin = async () => {
    setOAuthStarting(true)
    setMessage(null)
    try {
      const clientId = manualClientID || undefined
      const clientSecret = manualClientSecret || undefined
      const result = await startGeminiOAuth(clientId, clientSecret)

      // Open auth URL in popup.
      window.open(result.auth_url, 'gemini-oauth', 'width=600,height=700')

      // Poll for completion.
      pollRef.current = setInterval(async () => {
        try {
          const [oauth, st] = await Promise.all([getOAuthStatus(), getProviderStatus()])
          setOAuthStatus(oauth)
          setStatus(st)
          if (oauth.authenticated) {
            if (pollRef.current) clearInterval(pollRef.current)
            pollRef.current = null
            setMessage({ type: 'ok', text: 'OAuth authentication successful!' })
            setOAuthStarting(false)
          }
        } catch {
          // Ignore polling errors.
        }
      }, 2000)

      // Stop polling after 5 minutes.
      setTimeout(() => {
        if (pollRef.current) {
          clearInterval(pollRef.current)
          pollRef.current = null
          setOAuthStarting(false)
        }
      }, 5 * 60 * 1000)
    } catch (e) {
      setMessage({ type: 'err', text: e instanceof Error ? e.message : 'OAuth start failed' })
      setOAuthStarting(false)
    }
  }

  if (loading) return <p>Loading settings...</p>

  return (
    <div className="settings-page">
      <h2>Settings</h2>

      {status && (
        <div className={`status-banner ${status.configured ? 'ok' : 'warn'}`}>
          {status.configured
            ? `Provider active: ${status.provider_name}`
            : 'LLM provider not configured — set API key or use OAuth below.'}
        </div>
      )}

      {message && (
        <p className={message.type === 'ok' ? 'success' : 'error'}>{message.text}</p>
      )}

      <section>
        <h3>LLM Provider</h3>

        <div className="provider-tabs">
          <button
            className={`provider-tab ${providerTab === 'api-key' ? 'active' : ''}`}
            onClick={() => setProviderTab('api-key')}
          >
            API Key
          </button>
          <button
            className={`provider-tab ${providerTab === 'oauth' ? 'active' : ''}`}
            onClick={() => setProviderTab('oauth')}
          >
            Gemini OAuth
          </button>
        </div>

        {providerTab === 'api-key' && (
          <div style={{ marginTop: '1rem' }}>
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
              <select
                value={form.gemini_model}
                onChange={(e) => setForm({ ...form, gemini_model: e.target.value })}
              >
                {models.length > 0 ? (
                  models.map((m) => (
                    <option key={m.id} value={m.id}>
                      {m.display_name}
                    </option>
                  ))
                ) : (
                  <option value={form.gemini_model || 'gemini-2.5-pro'}>
                    {form.gemini_model || 'gemini-2.5-pro'}
                  </option>
                )}
              </select>
            </label>

            <button onClick={() => handleSave('api-key')} disabled={saving}>
              {saving ? 'Saving...' : 'Save'}
            </button>
          </div>
        )}

        {providerTab === 'oauth' && (
          <div style={{ marginTop: '1rem' }}>
            {/* OAuth Status */}
            {oauthStatus?.authenticated ? (
              <div className="status-banner ok" style={{ marginBottom: '1rem' }}>
                Connected{oauthStatus.email ? ` as ${oauthStatus.email}` : ''}
                {oauthStatus.project_id ? ` (project: ${oauthStatus.project_id})` : ''}
              </div>
            ) : (
              <>
                <div className="status-banner warn" style={{ marginBottom: '1rem' }}>
                  Not connected — click below to authenticate with Google.
                </div>

                {/* Detect Gemini CLI */}
                <div style={{ marginBottom: '1rem' }}>
                  <button onClick={handleDetect} disabled={detecting} className="btn-secondary">
                    {detecting ? 'Detecting...' : 'Detect Gemini CLI'}
                  </button>
                  {detectedClientID && (
                    <p className="success" style={{ marginTop: '0.5rem', fontSize: '0.82rem' }}>
                      ✓ Found and auto-filled credentials from Gemini CLI
                    </p>
                  )}
                  {detectError && (
                    <p className="error" style={{ marginTop: '0.5rem', fontSize: '0.82rem' }}>
                      {detectError}
                    </p>
                  )}
                </div>

                {/* OAuth Credentials */}
                <div style={{ marginBottom: '1rem' }}>
                  <p className="meta" style={{ marginBottom: '0.5rem' }}>
                    {detectedClientID ? 'Credentials (auto-filled):' : 'OAuth Credentials:'}
                  </p>
                  <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '1rem' }}>
                    <label className="form-field">
                      <span>Client ID</span>
                      <input
                        type="text"
                        value={manualClientID}
                        onChange={(e) => setManualClientID(e.target.value)}
                        placeholder="xxx.apps.googleusercontent.com"
                      />
                    </label>
                    <label className="form-field">
                      <span>Client Secret</span>
                      <input
                        type="password"
                        value={manualClientSecret}
                        onChange={(e) => setManualClientSecret(e.target.value)}
                        placeholder="GOCSPX-xxx"
                      />
                    </label>
                  </div>
                </div>

                {/* Login button */}
                <button
                  onClick={handleOAuthLogin}
                  disabled={oauthStarting || (!detectedClientID && !manualClientID)}
                >
                  {oauthStarting ? 'Waiting for authorization...' : 'Login with Google'}
                </button>
                {oauthStarting && (
                  <p className="meta" style={{ marginTop: '0.5rem' }}>
                    Complete the authorization in the popup window...
                  </p>
                )}
              </>
            )}

            {/* Model selector + activate */}
            {oauthStatus?.authenticated && (
              <div style={{ marginTop: '1.5rem', borderTop: '1px solid var(--border)', paddingTop: '1rem' }}>
                <label className="form-field">
                  <span>Model</span>
                  <select
                    value={form.gemini_model}
                    onChange={(e) => setForm({ ...form, gemini_model: e.target.value })}
                  >
                    {models.length > 0 ? (
                      models.map((m) => (
                        <option key={m.id} value={m.id}>
                          {m.display_name}
                        </option>
                      ))
                    ) : (
                      <option value={form.gemini_model || 'gemini-2.5-pro'}>
                        {form.gemini_model || 'gemini-2.5-pro'}
                      </option>
                    )}
                  </select>
                </label>

                <button onClick={() => handleSave('oauth')} disabled={saving}>
                  {saving ? 'Saving...' : 'Use OAuth Provider'}
                </button>
              </div>
            )}
          </div>
        )}
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

        <button onClick={() => handleSave()} disabled={saving}>
          {saving ? 'Saving...' : 'Save'}
        </button>
      </section>
    </div>
  )
}
