import { useEffect, useState, useRef } from "react";
import {
  getSettings,
  updateSettings,
  getProviderStatus,
  detectGeminiCLI,
  startGeminiOAuth,
  getOAuthStatus,
  listModels,
} from "../api";
import Select from "../components/ui/Select";
import type {
  SettingsData,
  ProviderStatus,
  OAuthStatus,
  ModelEntry,
} from "../types";

type ProviderTab = "api-key" | "oauth";

export default function Settings() {
  const [form, setForm] = useState<SettingsData>({
    gemini_api_key: "",
    gemini_model: "",
    discord_token: "",
    provider_type: "",
  });
  const [models, setModels] = useState<ModelEntry[]>([]);
  const [status, setStatus] = useState<ProviderStatus | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [message, setMessage] = useState<{
    type: "ok" | "err";
    text: string;
  } | null>(null);

  // OAuth state
  const [providerTab, setProviderTab] = useState<ProviderTab>("api-key");
  const [oauthStatus, setOAuthStatus] = useState<OAuthStatus | null>(null);
  const [detecting, setDetecting] = useState(false);
  const [detectedClientID, setDetectedClientID] = useState("");
  const [detectError, setDetectError] = useState("");
  const [manualClientID, setManualClientID] = useState("");
  const [manualClientSecret, setManualClientSecret] = useState("");
  const [oauthStarting, setOAuthStarting] = useState(false);
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);

  useEffect(() => {
    Promise.all([
      getSettings(),
      getProviderStatus(),
      getOAuthStatus(),
      listModels(),
    ])
      .then(([s, st, oauth, modelList]) => {
        setForm(s);
        setStatus(st);
        setOAuthStatus(oauth);
        setModels(modelList);
        if (
          s.provider_type === "oauth" ||
          (oauth.authenticated && st.provider_name === "gemini-oauth")
        ) {
          setProviderTab("oauth");
        } else if (s.provider_type === "api-key") {
          setProviderTab("api-key");
        }
      })
      .catch((e) => setMessage({ type: "err", text: e.message }))
      .finally(() => setLoading(false));
  }, []);

  // Stop polling on unmount.
  useEffect(() => {
    return () => {
      if (pollRef.current) clearInterval(pollRef.current);
    };
  }, []);

  const handleSave = async (overrideProviderType?: string) => {
    setSaving(true);
    setMessage(null);
    try {
      const payload = { ...form };
      if (overrideProviderType) {
        payload.provider_type = overrideProviderType;
      }
      await updateSettings(payload);
      const [st, updated, modelList] = await Promise.all([
        getProviderStatus(),
        getSettings(),
        listModels(),
      ]);
      setStatus(st);
      setForm(updated);
      setModels(modelList);
      setMessage({ type: "ok", text: "Settings saved successfully." });
    } catch (e) {
      setMessage({
        type: "err",
        text: e instanceof Error ? e.message : "Save failed",
      });
    } finally {
      setSaving(false);
    }
  };

  const handleDetect = async () => {
    setDetecting(true);
    setDetectError("");
    setDetectedClientID("");
    try {
      const result = await detectGeminiCLI();
      if (result.found && result.client_id) {
        setDetectedClientID(result.client_id);
        // Auto-fill both client ID and secret from Gemini CLI
        setManualClientID(result.client_id);
        if (result.client_secret) {
          setManualClientSecret(result.client_secret);
        }
      } else {
        setDetectError(result.error || "Gemini CLI not found.");
      }
    } catch (e) {
      setDetectError(e instanceof Error ? e.message : "Detection failed");
    } finally {
      setDetecting(false);
    }
  };

  const handleOAuthLogin = async () => {
    setOAuthStarting(true);
    setMessage(null);
    try {
      const clientId = manualClientID || undefined;
      const clientSecret = manualClientSecret || undefined;
      const result = await startGeminiOAuth(clientId, clientSecret);

      // Open auth URL in popup.
      window.open(result.auth_url, "gemini-oauth", "width=600,height=700");

      // Poll for completion.
      pollRef.current = setInterval(async () => {
        try {
          const [oauth, st] = await Promise.all([
            getOAuthStatus(),
            getProviderStatus(),
          ]);
          setOAuthStatus(oauth);
          setStatus(st);
          if (oauth.authenticated) {
            if (pollRef.current) clearInterval(pollRef.current);
            pollRef.current = null;
            setMessage({
              type: "ok",
              text: "OAuth authentication successful!",
            });
            setOAuthStarting(false);
          }
        } catch {
          // Ignore polling errors.
        }
      }, 2000);

      // Stop polling after 5 minutes.
      setTimeout(
        () => {
          if (pollRef.current) {
            clearInterval(pollRef.current);
            pollRef.current = null;
            setOAuthStarting(false);
          }
        },
        5 * 60 * 1000,
      );
    } catch (e) {
      setMessage({
        type: "err",
        text: e instanceof Error ? e.message : "OAuth start failed",
      });
      setOAuthStarting(false);
    }
  };

  if (loading) return <p>Loading settings...</p>;

  const modelOptions =
    models.length > 0
      ? models.map((m) => ({ value: m.id, label: m.display_name }))
      : [
          {
            value: form.gemini_model || "gemini-2.5-pro",
            label: form.gemini_model || "gemini-2.5-pro",
          },
        ];

  return (
    <div className="settings-page">
      <h2>Settings</h2>

      {status && (
        <div className={`status-banner ${status.configured ? "ok" : "warn"}`}>
          {status.configured
            ? `Provider active: ${status.provider_name}`
            : "LLM provider not configured — set API key or use OAuth below."}
        </div>
      )}

      {message && (
        <p className={message.type === "ok" ? "success" : "error"}>
          {message.text}
        </p>
      )}

      <section>
        <h3>LLM Provider</h3>

        <div className="provider-tabs">
          <button
            className={`provider-tab ${providerTab === "api-key" ? "active" : ""}`}
            onClick={() => setProviderTab("api-key")}
          >
            API Key
          </button>
          <button
            className={`provider-tab ${providerTab === "oauth" ? "active" : ""}`}
            onClick={() => setProviderTab("oauth")}
          >
            Gemini OAuth
          </button>
        </div>

        {providerTab === "api-key" && (
          <div style={{ marginTop: "1rem" }}>
            <label className="form-field">
              <span>Gemini API Key</span>
              <input
                type="password"
                value={form.gemini_api_key}
                onChange={(e) =>
                  setForm({ ...form, gemini_api_key: e.target.value })
                }
                placeholder="Enter your Gemini API key"
              />
            </label>

            <label className="form-field">
              <span>Model</span>
              <Select
                value={form.gemini_model}
                onChange={(val) => setForm({ ...form, gemini_model: val })}
                options={modelOptions}
              />
            </label>

            <button onClick={() => handleSave("api-key")} disabled={saving}>
              {saving ? "Saving..." : "Save"}
            </button>
          </div>
        )}

        {providerTab === "oauth" && (
          <div style={{ marginTop: "1rem" }}>
            {/* OAuth Status */}
            {oauthStatus?.authenticated ? (
              <div
                className="status-banner ok"
                style={{ marginBottom: "1rem" }}
              >
                Connected{oauthStatus.email ? ` as ${oauthStatus.email}` : ""}
                {oauthStatus.project_id
                  ? ` (project: ${oauthStatus.project_id})`
                  : ""}
              </div>
            ) : (
              <>
                <div
                  className="status-banner warn"
                  style={{ marginBottom: "1rem" }}
                >
                  Not connected — click below to authenticate with Google.
                </div>

                {/* Detect Gemini CLI */}
                <div style={{ marginBottom: "1rem" }}>
                  <button
                    onClick={handleDetect}
                    disabled={detecting}
                    className="btn-secondary"
                  >
                    {detecting ? "Detecting..." : "Detect Gemini CLI"}
                  </button>
                  {detectedClientID && (
                    <p
                      className="success"
                      style={{ marginTop: "0.5rem", fontSize: "0.82rem" }}
                    >
                      ✓ Found and auto-filled credentials from Gemini CLI
                    </p>
                  )}
                  {detectError && (
                    <p
                      className="error"
                      style={{ marginTop: "0.5rem", fontSize: "0.82rem" }}
                    >
                      {detectError}
                    </p>
                  )}
                </div>

                {/* OAuth Credentials */}
                <div style={{ marginBottom: "1rem" }}>
                  <p className="meta" style={{ marginBottom: "0.5rem" }}>
                    {detectedClientID
                      ? "Credentials (auto-filled):"
                      : "OAuth Credentials:"}
                  </p>
                  <div
                    style={{
                      display: "grid",
                      gridTemplateColumns: "1fr 1fr",
                      gap: "1rem",
                    }}
                  >
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
                  disabled={
                    oauthStarting || (!detectedClientID && !manualClientID)
                  }
                >
                  {oauthStarting
                    ? "Waiting for authorization..."
                    : "Login with Google"}
                </button>
                {oauthStarting && (
                  <p className="meta" style={{ marginTop: "0.5rem" }}>
                    Complete the authorization in the popup window...
                  </p>
                )}
              </>
            )}

            {/* Model selector + activate */}
            {oauthStatus?.authenticated && (
              <div
                style={{
                  marginTop: "1.5rem",
                  borderTop: "1px solid var(--border)",
                  paddingTop: "1rem",
                }}
              >
                <label className="form-field">
                  <span>Model</span>
                  <Select
                    value={form.gemini_model}
                    onChange={(val) => setForm({ ...form, gemini_model: val })}
                    options={modelOptions}
                  />
                </label>

                <button onClick={() => handleSave("oauth")} disabled={saving}>
                  {saving ? "Saving..." : "Use OAuth Provider"}
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
            onChange={(e) =>
              setForm({ ...form, discord_token: e.target.value })
            }
            placeholder="Enter your Discord bot token"
          />
          <span className="hint">Discord bot requires restart to apply.</span>
        </label>

        <div style={{ marginTop: '1.5rem', background: '#16162a', padding: '1.25rem', borderRadius: '8px', border: '1px solid rgba(255,255,255,0.06)' }}>
          <h4 style={{ margin: '0 0 1rem', fontSize: '0.95rem', color: '#e0e0e0' }}>Discord Bot 設定指引</h4>
          
          <div style={{ marginBottom: '1.5rem' }}>
            <h5 style={{ margin: '0 0 0.5rem', fontSize: '0.9rem', color: '#a0a8ff' }}>Gateway Intents (開發者後台設定)</h5>
            <p className="meta" style={{ marginBottom: '0.5rem' }}>
              請至 <a href="https://discord.com/developers/applications" target="_blank" rel="noopener noreferrer" style={{ color: '#646cff' }}>Discord Developer Portal</a> &gt; Bot &gt; Privileged Gateway Intents 啟用：
            </p>
            <ul style={{ paddingLeft: '0', margin: '0', listStyle: 'none' }}>
              <li style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', marginBottom: '0.35rem', fontSize: '0.85rem', color: '#ccc' }}>
                <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="#4caf50" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round"><polyline points="20 6 9 17 4 12"/></svg>
                <span>MESSAGE CONTENT INTENT (讀取訊息內容)</span>
              </li>
              <li style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', marginBottom: '0.35rem', fontSize: '0.85rem', color: '#ccc' }}>
                <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="#4caf50" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round"><polyline points="20 6 9 17 4 12"/></svg>
                <span>SERVER MEMBERS INTENT (如果需要)</span>
              </li>
              <li style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', fontSize: '0.85rem', color: '#ccc' }}>
                <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="#4caf50" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round"><polyline points="20 6 9 17 4 12"/></svg>
                <span>PRESENCE INTENT (如果需要)</span>
              </li>
            </ul>
          </div>

          <div style={{ marginBottom: '1.5rem' }}>
            <h5 style={{ margin: '0 0 0.5rem', fontSize: '0.9rem', color: '#a0a8ff' }}>必要權限清單 (邀請連結設定)</h5>
            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '1rem' }}>
              <div>
                <h6 style={{ margin: '0 0 0.4rem', fontSize: '0.8rem', color: '#7a7a9a', textTransform: 'uppercase' }}>General Permissions</h6>
                <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', marginBottom: '0.25rem', fontSize: '0.85rem', color: '#ccc' }}>
                  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="#4caf50" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round"><polyline points="20 6 9 17 4 12"/></svg>
                  <span>View Channels — 查看頻道內容</span>
                </div>
              </div>
              <div>
                <h6 style={{ margin: '0 0 0.4rem', fontSize: '0.8rem', color: '#7a7a9a', textTransform: 'uppercase' }}>Text Permissions</h6>
                <ul style={{ paddingLeft: '0', margin: '0', listStyle: 'none' }}>
                  {[
                    ['Send Messages', '發送回覆訊息'],
                    ['Send Messages in Threads', '在討論串中回覆'],
                    ['Embed Links', '回覆中包含連結預覽'],
                    ['Attach Files', '發送圖片或檔案'],
                    ['Read Message History', '讀取對話歷史'],
                    ['Use Slash Commands', '支援指令'],
                    ['Add Reactions', '加表情符號回應 (可選)'],
                  ].map(([en, zh]) => (
                    <li key={en} style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', marginBottom: '0.25rem', fontSize: '0.85rem', color: '#ccc' }}>
                      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="#4caf50" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round"><polyline points="20 6 9 17 4 12"/></svg>
                      <span>{en} — {zh}</span>
                    </li>
                  ))}
                </ul>
              </div>
            </div>
          </div>

          <div style={{ background: 'rgba(76, 175, 80, 0.08)', border: '1px solid rgba(76, 175, 80, 0.15)', borderRadius: '6px', padding: '0.85rem' }}>
            <h5 style={{ margin: '0 0 0.5rem', fontSize: '0.85rem', color: '#81c784', display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/></svg>
              最小權限組合（推薦）
            </h5>
            <p className="meta" style={{ marginBottom: '0.5rem', fontSize: '0.8rem' }}>如果只是基本對話功能，最少選這些就夠了：</p>
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: '0.5rem 1.5rem' }}>
              {['View Channels', 'Send Messages', 'Read Message History', 'Use Slash Commands'].map(p => (
                <div key={p} style={{ display: 'flex', alignItems: 'center', gap: '0.4rem', fontSize: '0.8rem', color: '#ccc' }}>
                  <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="#4caf50" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round"><polyline points="20 6 9 17 4 12"/></svg>
                  <span>{p}</span>
                </div>
              ))}
            </div>
          </div>
        </div>

        <button
          onClick={() => handleSave()}
          disabled={saving}
          style={{ marginTop: "1.5rem" }}
        >
          {saving ? "Saving..." : "Save"}
        </button>
      </section>
    </div>
  );
}
