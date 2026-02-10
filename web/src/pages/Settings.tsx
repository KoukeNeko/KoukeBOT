import { useEffect, useState, useRef, useCallback } from "react";
import {
  getSettings,
  updateSettings,
  getProviderStatus,
  detectGeminiCLI,
  startGeminiOAuth,
  submitOAuthCallback,
  importGeminiCLIToken,
  getOAuthStatus,
  getOAuthPool,
  removeOAuthPoolAccount,
  listModels,
} from "../api";
import Select from "../components/ui/Select";
import Checkbox from "../components/ui/Checkbox";
import type {
  SettingsData,
  ProviderStatus,
  OAuthStatus,
  ModelEntry,
  PoolEntry,
} from "../types";

import "./Settings.css"; // Import the new CSS file

type ProviderTab = "api-key" | "oauth" | "pool";

export default function Settings() {
  const [form, setForm] = useState<SettingsData>({
    gemini_api_key: "",
    gemini_model: "",
    discord_token: "",
    provider_type: "",
    brave_api_key: "",
    web_search_enabled: false,
    discord_use_reply: false,
    perplexity_api_key: "",
    search_provider: "auto",
    web_fetch_enabled: false,
    timezone: "auto",
    time_format: "auto",
    location: "",
    memory_extraction_enabled: false,
    memory_extraction_model: "",
    log_channel_id: "",
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
  const [, setOAuthStatus] = useState<OAuthStatus | null>(null);
  const [detecting, setDetecting] = useState(false);
  const [detectedClientID, setDetectedClientID] = useState("");
  const [detectError, setDetectError] = useState("");
  const [manualClientID, setManualClientID] = useState("");
  const [manualClientSecret, setManualClientSecret] = useState("");
  const [oauthStarting, setOAuthStarting] = useState(false);
  const [callbackUrl, setCallbackUrl] = useState("");
  const [submittingCallback, setSubmittingCallback] = useState(false);
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const [addAccountOpen, setAddAccountOpen] = useState(false);
  const [importingCLI, setImportingCLI] = useState(false);

  // Pool state
  const [pool, setPool] = useState<PoolEntry[]>([]);
  const [removingEmail, setRemovingEmail] = useState<string | null>(null);

  const refreshPool = useCallback(async () => {
    try {
      const entries = await getOAuthPool();
      setPool(entries);
    } catch {
      // Pool endpoint may not be available if no pool is active
    }
  }, []);

  useEffect(() => {
    Promise.all([
      getSettings(),
      getProviderStatus(),
      getOAuthStatus(),
      listModels(),
      getOAuthPool().catch(() => [] as PoolEntry[]),
    ])
      .then(([s, st, oauth, modelList, poolEntries]) => {
        setForm(s);
        setStatus(st);
        setOAuthStatus(oauth);
        setModels(modelList);
        setPool(poolEntries);
        if (s.provider_type === "oauth") {
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

  const [authUrl, setAuthUrl] = useState("");

  const handleOAuthLogin = async () => {
    setOAuthStarting(true);
    setMessage(null);
    setAuthUrl("");
    setCallbackUrl("");
    try {
      const clientId = manualClientID || undefined;
      const clientSecret = manualClientSecret || undefined;
      const result = await startGeminiOAuth(clientId, clientSecret);

      // Store the auth URL and let user open it manually.
      setAuthUrl(result.auth_url);
    } catch (e) {
      setMessage({
        type: "err",
        text: e instanceof Error ? e.message : "OAuth start failed",
      });
      setOAuthStarting(false);
    }
  };

  const handleManualCallback = async () => {
    if (!callbackUrl.trim()) return;
    setSubmittingCallback(true);
    setMessage(null);
    try {
      const result = await submitOAuthCallback(callbackUrl.trim());
      await refreshPool();
      const [oauth, st] = await Promise.all([getOAuthStatus(), getProviderStatus()]);
      setOAuthStatus(oauth);
      setStatus(st);
      setMessage({ type: "ok", text: `OAuth 認證成功！已新增 ${result.email}` });
      setOAuthStarting(false);
      setAddAccountOpen(false);
      setCallbackUrl("");
      setAuthUrl("");
    } catch (e) {
      setMessage({ type: "err", text: e instanceof Error ? e.message : "Callback 提交失敗" });
    } finally {
      setSubmittingCallback(false);
    }
  };

  const handleImportCLIToken = async () => {
    setImportingCLI(true);
    setMessage(null);
    try {
      const result = await importGeminiCLIToken();
      await refreshPool();
      const [oauth, st] = await Promise.all([getOAuthStatus(), getProviderStatus()]);
      setOAuthStatus(oauth);
      setStatus(st);
      setMessage({ type: "ok", text: `已匯入 Gemini CLI Token：${result.email}` });
      setAddAccountOpen(false);
    } catch (e) {
      setMessage({ type: "err", text: e instanceof Error ? e.message : "匯入失敗" });
    } finally {
      setImportingCLI(false);
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

              <p className={message.type === "ok" ? "success message" : "error message"}>

                {message.text}

              </p>

            )

      }

      <section className="settings-section">
        <h3 className="settings-section-title">LLM Provider</h3>

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

        <div className="provider-content">
          {providerTab === "api-key" && (
            <>
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

              <button onClick={() => handleSave("api-key")} disabled={saving} className="btn-save-bottom">
                {saving ? "Saving..." : "Save"}
              </button>
            </>
          )}

          {providerTab === "oauth" && (
            <>
              {/* Pool accounts list */}
              {pool.length > 0 && (
                <div className="pool-cards compact">
                  {pool.map((entry) => (
                    <div key={entry.email} className={`pool-card ${entry.status === "exhausted" ? "exhausted" : ""} ${entry.current ? "current" : ""}`}>
                      <div className="pool-card-header">
                        <div className="pool-card-avatar">
                          {entry.email.charAt(0).toUpperCase()}
                        </div>
                        <div className="pool-card-info">
                          <div className="pool-card-email">
                            {entry.email}
                            {entry.current && <span className="pool-current-badge">使用中</span>}
                          </div>
                          <div className={`pool-card-status ${entry.status}`}>
                            {entry.status === "available" ? (
                              <><span className="pool-dot available" /> 可用</>
                            ) : (
                              <><span className="pool-dot exhausted" /> 配額耗盡
                                {entry.exhausted_until && (
                                  <span className="pool-reset-hint">
                                    (重置於 {new Date(entry.exhausted_until).toLocaleTimeString()})
                                  </span>
                                )}
                              </>
                            )}
                          </div>
                          {entry.request_count > 0 && (
                            <div className="pool-card-quota">
                              <div className="pool-quota-row">
                                <span className="pool-quota-label">24h 使用量:</span>
                                <span className="pool-quota-value">{entry.request_count}</span>
                              </div>
                              <div className="pool-quota-row">
                                <span className="pool-quota-label">總計請求:</span>
                                <span className="pool-quota-total">{entry.total_requests.toLocaleString()}</span>
                              </div>
                              {entry.last_reset_time && (
                                <>
                                  <div className="pool-quota-row">
                                    <span className="pool-quota-label">上次重置:</span>
                                    <span className="pool-quota-time">
                                      {new Date(entry.last_reset_time).toLocaleString('zh-TW', {
                                        month: '2-digit',
                                        day: '2-digit',
                                        hour: '2-digit',
                                        minute: '2-digit'
                                      })}
                                    </span>
                                  </div>
                                  <div className="pool-quota-row">
                                    <span className="pool-quota-label">下次重置:</span>
                                    <span className="pool-quota-time">
                                      {(() => {
                                        const resetTime = new Date(entry.last_reset_time);
                                        resetTime.setHours(resetTime.getHours() + 24);
                                        const now = new Date();
                                        const diff = resetTime.getTime() - now.getTime();
                                        const hours = Math.floor(diff / (1000 * 60 * 60));
                                        const minutes = Math.floor((diff % (1000 * 60 * 60)) / (1000 * 60));
                                        return `${hours}h ${minutes}m 後`;
                                      })()}
                                    </span>
                                  </div>
                                </>
                              )}
                            </div>
                          )}
                        </div>
                      </div>
                      <button
                        className="pool-card-remove"
                        onClick={async () => {
                          if (!confirm(`確定要移除帳號 ${entry.email}？`)) return;
                          setRemovingEmail(entry.email);
                          try {
                            await removeOAuthPoolAccount(entry.email);
                            await refreshPool();
                            const [oauth, st] = await Promise.all([getOAuthStatus(), getProviderStatus()]);
                            setOAuthStatus(oauth);
                            setStatus(st);
                            setMessage({ type: "ok", text: `已移除 ${entry.email}` });
                          } catch (e) {
                            setMessage({ type: "err", text: e instanceof Error ? e.message : "移除失敗" });
                          } finally {
                            setRemovingEmail(null);
                          }
                        }}
                        disabled={removingEmail === entry.email}
                        title="移除此帳號"
                      >
                        {removingEmail === entry.email ? "..." : "×"}
                      </button>
                    </div>
                  ))}
                </div>
              )}

              {/* Add account button */}
              <button
                className="btn-secondary pool-add-account-button"
                onClick={() => setAddAccountOpen(true)}
                style={{ marginTop: pool.length > 0 ? "0.75rem" : 0 }}
              >
                <span style={{ fontSize: "1.1rem" }}>+</span> 新增帳號
              </button>

              {/* Model selector + activate */}
              {pool.length > 0 && (
                <div className="oauth-section-subtle">
                  <label className="form-field">
                    <span>Model</span>
                    <Select
                      value={form.gemini_model}
                      onChange={(val) => setForm({ ...form, gemini_model: val })}
                      options={modelOptions}
                    />
                  </label>

                  <button onClick={() => handleSave("oauth")} disabled={saving} className="btn-save-bottom">
                    {saving ? "Saving..." : "Use OAuth Provider"}
                  </button>
                </div>
              )}
            </>
          )}


        </div>
      </section>



      {/* Add Account Modal */}
      {addAccountOpen && (
        <div className="modal-overlay" onClick={() => !oauthStarting && setAddAccountOpen(false)}>
          <div className="modal-content" onClick={(e) => e.stopPropagation()}>
            <div className="modal-header">
              <h3 className="modal-title">新增 Google 帳號</h3>
              <button
                className="modal-close"
                onClick={() => !oauthStarting && setAddAccountOpen(false)}
                disabled={oauthStarting}
              >
                &times;
              </button>
            </div>

            <div className="modal-body">
              {message && (
                <p className={message.type === "ok" ? "success message" : "error message"}>
                  {message.text}
                </p>
              )}

              {/* Import stored Gemini CLI token (方案二) */}
              <div className="oauth-detect-block">
                <button
                  onClick={handleImportCLIToken}
                  disabled={importingCLI || oauthStarting}
                  className="btn-primary"
                >
                  {importingCLI ? "匯入中..." : "Import Gemini CLI Token"}
                </button>
                <p className="meta oauth-meta">
                  先在伺服器上執行 <code>gemini</code> 完成登入，再點此匯入已儲存的 Token
                </p>
              </div>

              <hr style={{ margin: "1rem 0", borderColor: "var(--color-border)" }} />

              {/* Detect Gemini CLI */}
              <div className="oauth-detect-block">
                <button
                  onClick={handleDetect}
                  disabled={detecting || oauthStarting}
                  className="btn-secondary"
                >
                  {detecting ? "Detecting..." : "Detect Gemini CLI"}
                </button>
                {detectedClientID && (
                  <p className="success oauth-meta">
                    Found and auto-filled credentials from Gemini CLI
                  </p>
                )}
                {detectError && (
                  <p className="error oauth-meta">{detectError}</p>
                )}
              </div>

              {/* OAuth Credentials */}
              <div className="oauth-credentials-block">
                <p className="meta oauth-meta">
                  {detectedClientID ? "Credentials (auto-filled):" : "OAuth Credentials:"}
                </p>
                <div className="oauth-credentials-grid">
                  <label className="form-field">
                    <span>Client ID</span>
                    <input
                      type="text"
                      value={manualClientID}
                      onChange={(e) => setManualClientID(e.target.value)}
                      placeholder="xxx.apps.googleusercontent.com"
                      disabled={oauthStarting}
                    />
                  </label>
                  <label className="form-field">
                    <span>Client Secret</span>
                    <input
                      type="password"
                      value={manualClientSecret}
                      onChange={(e) => setManualClientSecret(e.target.value)}
                      placeholder="GOCSPX-xxx"
                      disabled={oauthStarting}
                    />
                  </label>
                </div>
              </div>

              {/* Step 1: Login button */}
              {!authUrl && (
                <button
                  onClick={handleOAuthLogin}
                  disabled={oauthStarting || (!detectedClientID && !manualClientID)}
                  className="btn-primary"
                >
                  {oauthStarting ? "準備中..." : "Login with Google"}
                </button>
              )}

              {/* Step 2+3: Auth URL + paste callback */}
              {authUrl && (
                <div className="oauth-manual-callback">
                  <p className="hint" style={{ marginBottom: "0.5rem" }}>
                    <strong>步驟 1:</strong> 在新分頁中按 <strong>F12</strong> 開啟開發者工具，切換到 <strong>Network（網路）</strong>分頁
                  </p>
                  <p className="hint" style={{ marginBottom: "0.5rem" }}>
                    <strong>步驟 2:</strong> 點擊下方連結完成 Google 授權
                  </p>
                  <a
                    href={authUrl}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="btn-primary"
                    style={{ display: "inline-block", textAlign: "center", textDecoration: "none", marginBottom: "1rem" }}
                  >
                    開啟 Google 授權頁面
                  </a>

                  <p className="hint" style={{ marginBottom: "0.5rem" }}>
                    <strong>步驟 3:</strong> 授權完成後，URL 會一閃而過。在開發者工具的 Network 分頁中找到 <code>127.0.0.1</code> 的請求，右鍵 → <strong>Copy URL</strong>，貼到下方：
                  </p>
                  <input
                    type="text"
                    value={callbackUrl}
                    onChange={(e) => setCallbackUrl(e.target.value)}
                    placeholder="http://127.0.0.1:8080/api/oauth/gemini/callback?code=4/0A...&state=..."
                    style={{ width: "100%", marginBottom: "0.5rem" }}
                  />
                  <div style={{ display: "flex", gap: "0.5rem" }}>
                    <button
                      onClick={handleManualCallback}
                      disabled={submittingCallback || !callbackUrl.trim()}
                      className="btn-primary"
                    >
                      {submittingCallback ? "提交中..." : "提交完成認證"}
                    </button>
                    <button
                      onClick={() => { setAuthUrl(""); setOAuthStarting(false); setCallbackUrl(""); }}
                      className="btn-secondary"
                    >
                      取消
                    </button>
                  </div>
                </div>
              )}
            </div>
          </div>
        </div>
      )}

      <section className="settings-section">
        <h3 className="settings-section-title">Discord Bot</h3>

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

        <label className="form-field">
          <span>Log Channel ID (可選)</span>
          <input
            type="text"
            value={form.log_channel_id || ""}
            onChange={(e) =>
              setForm({ ...form, log_channel_id: e.target.value })
            }
            placeholder="例如：1234567890123456789"
          />
          <span className="hint">
            將系統日誌輸出到指定的 Discord 頻道。右鍵頻道 &gt; 複製頻道 ID 取得。留空則不輸出。
          </span>
        </label>

        <div className="form-field-checkbox-row">
          <Checkbox
            checked={form.discord_use_reply}
            onChange={(checked) => setForm({ ...form, discord_use_reply: checked })}
            label="使用回覆功能回應訊息"
          />
          <p className="hint-block">
            啟用後，BOT 會以 Discord 的「回覆」功能回應使用者訊息（顯示引用關係）
          </p>
        </div>

        <div className="discord-guide-block">
          <h4 className="discord-guide-title">Discord Bot 設定指引</h4>
          
          <div className="settings-group">
            <h5 className="discord-guide-subtitle">Gateway Intents (開發者後台設定)</h5>
            <p className="meta discord-guide-meta">
              請至 <a href="https://discord.com/developers/applications" target="_blank" rel="noopener noreferrer" className="link">Discord Developer Portal</a> &gt; Bot &gt; Privileged Gateway Intents 啟用：
            </p>
            <ul className="discord-guide-list">
              <li className="discord-guide-list-item">
                <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeLinecap="round" strokeLinejoin="round"><polyline points="20 6 9 17 4 12"/></svg>
                <span>MESSAGE CONTENT INTENT (讀取訊息內容)</span>
              </li>
              <li className="discord-guide-list-item">
                <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeLinecap="round" strokeLinejoin="round"><polyline points="20 6 9 17 4 12"/></svg>
                <span>SERVER MEMBERS INTENT (如果需要)</span>
              </li>
              <li className="discord-guide-list-item">
                <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeLinecap="round" strokeLinejoin="round"><polyline points="20 6 9 17 4 12"/></svg>
                <span>PRESENCE INTENT (如果需要)</span>
              </li>
            </ul>
          </div>

          <div className="settings-group">
            <h5 className="discord-guide-subtitle">必要權限清單 (邀請連結設定)</h5>
            <div className="discord-permissions-grid">
              <div>
                <h6 className="discord-permission-category-title">General Permissions</h6>
                <div className="discord-permission-item">
                  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeLinecap="round" strokeLinejoin="round"><polyline points="20 6 9 17 4 12"/></svg>
                  <span>View Channels — 查看頻道內容</span>
                </div>
              </div>
              <div>
                <h6 className="discord-permission-category-title">Text Permissions</h6>
                <ul className="discord-guide-list">
                  {[
                    ['Send Messages', '發送回覆訊息'],
                    ['Send Messages in Threads', '在討論串中回覆'],
                    ['Embed Links', '回覆中包含連結預覽'],
                    ['Attach Files', '發送圖片或檔案'],
                    ['Read Message History', '讀取對話歷史'],
                    ['Use Slash Commands', '支援指令'],
                    ['Add Reactions', '加表情符號回應 (可選)'],
                  ].map(([en, zh]) => (
                    <li key={en} className="discord-permission-item">
                      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeLinecap="round" strokeLinejoin="round"><polyline points="20 6 9 17 4 12"/></svg>
                      <span>{en} — {zh}</span>
                    </li>
                  ))}
                </ul>
              </div>
            </div>
          </div>

          <div className="discord-min-permissions-card">
            <h5 className="discord-min-permissions-title">
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/></svg>
              最小權限組合（推薦）
            </h5>
            <p className="meta discord-min-permissions-hint">如果只是基本對話功能，最少選這些就夠了：</p>
            <ul className="discord-min-permissions-list">
              {['View Channels', 'Send Messages', 'Read Message History', 'Use Slash Commands'].map(p => (
                <li key={p} className="discord-min-permissions-list-item">
                  <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeLinecap="round" strokeLinejoin="round"><polyline points="20 6 9 17 4 12"/></svg>
                  <span>{p}</span>
                </li>
              ))}
            </ul>
          </div>
        </div>

        <button
          onClick={() => handleSave()}
          disabled={saving}
          className="btn-save-bottom"
        >
          {saving ? "Saving..." : "Save"}
        </button>
      </section>

      <section className="settings-section">
        <h3 className="settings-section-title">
          <svg xmlns="http://www.w3.org/2000/svg" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className="lucide lucide-wrench"><path d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-2.53 2.53a1 1 0 0 0-1.4 0L6.3 14.7a1 1 0 0 0 0-1.4l.9-.9a1 1 0 0 0 0-1.4l-1.6-1.6a1 1 0 0 0-1.4 0L1.8 11.37a6 6 0 0 1 7.94-7.94l2.53-2.53a1 1 0 0 0 0-1.4Z"/><path d="m18.17 17.12 1.66 1.66"/></svg>
          Tools
        </h3>

        <div className="tool-checkbox-row">
          <Checkbox
            checked={form.web_search_enabled}
            onChange={(checked) => setForm({ ...form, web_search_enabled: checked })}
            label="Enable Web Search"
          />
        </div>

        {form.web_search_enabled && (
          <div className="tool-settings-indent">
            <label className="form-field">
              <span>Brave API Key</span>
              <input
                type="password"
                value={form.brave_api_key}
                onChange={(e) => setForm({ ...form, brave_api_key: e.target.value })}
                placeholder="Enter your Brave Search API key"
              />
              <span className="hint">
                Get your API key at{" "}
                <a
                  href="https://brave.com/search/api/"
                  target="_blank"
                  rel="noopener noreferrer"
                  className="link"
                >
                  brave.com/search/api
                </a>{" "}
                (Free tier: 2000 queries/month)
              </span>
            </label>

            <label className="form-field">
              <div className="tool-field-header">
                <span>Perplexity API Key</span>
                <span className="tool-tag">OpenClaw 相容</span>
              </div>
              <input
                type="password"
                value={form.perplexity_api_key || ''}
                onChange={(e) => setForm({ ...form, perplexity_api_key: e.target.value })}
                placeholder="Enter your Perplexity API key"
              />
              <span className="hint">
                Get your API key at{" "}
                <a
                  href="https://www.perplexity.ai/settings/api"
                  target="_blank"
                  rel="noopener noreferrer"
                  className="link"
                >
                  perplexity.ai/api
                </a>{" "}
                (Paid service, supports Sonar model)
              </span>
            </label>

            <label className="form-field">
              <div className="tool-field-header">
                <span>Search Provider</span>
                <span className="tool-tag">OpenClaw 相容</span>
              </div>
              <Select
                value={form.search_provider || 'auto'}
                onChange={(val) => setForm({ ...form, search_provider: val })}
                options={[
                  { value: 'auto', label: 'Auto (預設 Brave)' },
                  { value: 'brave', label: 'Brave Search' },
                  { value: 'perplexity', label: 'Perplexity Sonar' },
                ]}
              />
              <span className="hint">
                選擇搜尋提供商。Auto 模式會優先使用已設定的 Brave，無 Brave 時降級到 Perplexity。
              </span>
            </label>
          </div>
        )}

        <div className="tool-checkbox-row">
            <Checkbox
              checked={form.web_fetch_enabled}
              onChange={(checked) => setForm({ ...form, web_fetch_enabled: checked })}
              label="啟用 Web Fetch 工具"
            />
            <span className="tool-tag">OpenClaw 相容</span>
          </div>
          <p className="hint-block">
            網頁內容提取工具：取得並轉換網頁為 Markdown 格式。LLM 可以自動使用此工具讀取完整網頁內容。
          </p>

        <button
          onClick={() => handleSave()}
          disabled={saving}
          className="btn-save-bottom"
        >
          {saving ? "Saving..." : "Save"}
        </button>
      </section>

      <section className="settings-section">
        <h3 className="settings-section-title">
          <svg xmlns="http://www.w3.org/2000/svg" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className="lucide lucide-alarm-clock"><circle cx="12" cy="12" r="10"/><path d="M12 6v6l4 2"/><path d="M10.2 3.2c3.9 3.9 3.9 10.1 0 14l3.8 3.8c3.9-3.9 3.9-10.1 0-14Z"/><path d="M3.2 13.8c3.9-3.9 10.1-3.9 14 0l3.8-3.8c-3.9 3.9-10.1 3.9-14 0Z"/></svg>
          時間與地區設定
          <span className="tool-tag">OpenClaw 相容</span>
        </h3>

        <div style={{ marginBottom: '1.25rem' }}>
          <label className="form-field">
            <span>時區</span>
            <Select
              value={form.timezone || "auto"}
              onChange={(val) => setForm({ ...form, timezone: val })}
              options={[
                { value: "auto", label: "自動偵測 (Auto)" },
                { value: "Asia/Taipei", label: "Asia/Taipei (台北)" },
                { value: "Asia/Tokyo", label: "Asia/Tokyo (東京)" },
                { value: "Asia/Hong_Kong", label: "Asia/Hong_Kong (香港)" },
                { value: "Asia/Shanghai", label: "Asia/Shanghai (上海)" },
                { value: "Asia/Seoul", label: "Asia/Seoul (首爾)" },
                { value: "Asia/Singapore", label: "Asia/Singapore (新加坡)" },
                { value: "America/New_York", label: "America/New_York (紐約)" },
                { value: "America/Los_Angeles", label: "America/Los_Angeles (洛杉磯)" },
                { value: "America/Chicago", label: "America/Chicago (芝加哥)" },
                { value: "Europe/London", label: "Europe/London (倫敦)" },
                { value: "Europe/Paris", label: "Europe/Paris (巴黎)" },
                { value: "Australia/Sydney", label: "Australia/Sydney (雪梨)" },
              ]}
            />
            <span className="hint">
              設定時區以確保 AI 正確理解時間。時區資訊會注入 system prompt（靜態，不影響 prompt cache）。
            </span>
          </label>
        </div>

        <div style={{ marginBottom: '1.25rem' }}>
          <label className="form-field">
            <span>時間格式</span>
            <Select
              value={form.time_format || "auto"}
              onChange={(val) => setForm({ ...form, time_format: val })}
              options={[
                { value: "auto", label: "自動 (Auto)" },
                { value: "24", label: "24 小時制" },
                { value: "12", label: "12 小時制 (AM/PM)" },
              ]}
            />
            <span className="hint">
              選擇時間顯示格式。當 AI 使用 session_status 工具查詢時間時，會使用此格式。
            </span>
          </label>
        </div>

        <div style={{ marginBottom: '1.25rem' }}>
          <label className="form-field">
            <span>地區 (可選)</span>
            <input
              type="text"
              placeholder="例如：台北, 台灣"
              value={form.location || ""}
              onChange={(e) => setForm({ ...form, location: e.target.value })}
            />
            <span className="hint">
              選填：明確指定你的所在地，幫助 AI 更了解你的地理位置。時區已經隱含地理資訊（如 Asia/Taipei → 台灣）。
            </span>
          </label>
        </div>

        <button
          onClick={() => handleSave()}
          disabled={saving}
          className="btn-save-bottom"
        >
          {saving ? "Saving..." : "Save"}
        </button>
      </section>

      <section className="settings-section">
        <h3 className="settings-section-title">
          <svg xmlns="http://www.w3.org/2000/svg" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M12 2a10 10 0 1 0 10 10A10 10 0 0 0 12 2Z" opacity="0"/><path d="M21 12a9 9 0 1 1-9-9 9 9 0 0 1 9 9Z"/><path d="M12 6v6l4 2"/></svg>
          記憶系統
        </h3>

        <div className="tool-checkbox-row">
          <Checkbox
            checked={form.memory_extraction_enabled}
            onChange={(checked) => setForm({ ...form, memory_extraction_enabled: checked })}
            label="啟用自動記憶提取"
          />
        </div>
        <p className="hint-block">
          每次對話結束後，背景自動分析對話內容，更新使用者檔案 (user.md)、長期記憶 (MEMORY.md) 和每日紀錄。需要重啟生效。
        </p>

        {form.memory_extraction_enabled && (
          <div className="tool-settings-indent">
            <label className="form-field">
              <span>記憶提取模型</span>
              <Select
                value={form.memory_extraction_model || ""}
                onChange={(val) => setForm({ ...form, memory_extraction_model: val })}
                options={[
                  { value: "", label: "預設 (gemini-2.0-flash)" },
                  ...modelOptions,
                ]}
              />
              <span className="hint">
                選擇用於記憶提取的模型。建議使用便宜、快速的模型（如 gemini-2.0-flash）以節省配額。留空使用預設值。
              </span>
            </label>
          </div>
        )}

        <button
          onClick={() => handleSave()}
          disabled={saving}
          className="btn-save-bottom"
        >
          {saving ? "Saving..." : "Save"}
        </button>
      </section>
    </div>
  );
}
