import { useState, type Dispatch, type SetStateAction } from "react";
import Select from "../../components/ui/Select";
import type { SettingsData, OAuthStatus, PoolEntry, OAuthStartResult } from "../../types";

export type ProviderTab = "api-key" | "oauth" | "pool";

type SelectOption = {
  value: string;
  label: string;
};

type StatusMessage = {
  type: "ok" | "err";
  text: string;
} | null;

type Props = {
  form: SettingsData;
  setForm: Dispatch<SetStateAction<SettingsData>>;
  modelOptions: SelectOption[];
  providerTab: ProviderTab;
  setProviderTab: (tab: ProviderTab) => void;
  oauthStatus: OAuthStatus | null;
  pool: PoolEntry[];
  removingEmail: string | null;
  onRemovePoolAccount: (email: string) => Promise<void>;
  verifyingPool: boolean;
  onVerifyPool: (providerID: string) => Promise<void>;
  saving: boolean;
  onSave: (overrideProviderType?: string) => void;
  addAccountOpen: boolean;
  onOpenAddAccount: () => void;
  onCloseAddAccount: () => void;
  oauthStarting: boolean;
  oauthCompleting: boolean;
  oauthMode: "local" | "remote";
  setOAuthMode: (mode: "local" | "remote") => void;
  oauthFlow: OAuthStartResult | null;
  oauthCallbackURL: string;
  setOAuthCallbackURL: (value: string) => void;
  onStartOAuth: (providerID: string) => void;
  onCompleteOAuth: () => void;
  message: StatusMessage;
  pastedTokenJSON: string;
  setPastedTokenJSON: (value: string) => void;
  importingPasted: boolean;
  onImportPastedToken: (providerID: string) => void;
  importingCLI: boolean;
  onImportCLIToken: (providerID: string) => void;
};

export default function ProviderSection({
  form,
  setForm,
  modelOptions,
  providerTab,
  setProviderTab,
  oauthStatus,
  pool,
  removingEmail,
  onRemovePoolAccount,
  verifyingPool,
  onVerifyPool,
  saving,
  onSave,
  addAccountOpen,
  onOpenAddAccount,
  onCloseAddAccount,
  oauthStarting,
  oauthCompleting,
  oauthMode,
  setOAuthMode,
  oauthFlow,
  oauthCallbackURL,
  setOAuthCallbackURL,
  onStartOAuth,
  onCompleteOAuth,
  message,
  pastedTokenJSON,
  setPastedTokenJSON,
  importingPasted,
  onImportPastedToken,
  importingCLI,
  onImportCLIToken,
}: Props) {
  const isModalBusy = oauthStarting || oauthCompleting;
  const [errorDetailEmail, setErrorDetailEmail] = useState<string | null>(null);
  const selectedOAuthProviderID =
    form.provider_id === "google-gemini-cli" ? "google-gemini-cli" : "google-antigravity";
  const currentModelValue = form.model_id || form.gemini_model;
  const toLegacyModel = (modelID: string) =>
    modelID.includes("/") ? modelID.split("/").slice(1).join("/") : modelID;

  return (
    <>
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
                  value={currentModelValue}
                  onChange={(val) =>
                    setForm({
                      ...form,
                      provider_id: "gemini-api-key",
                      model_id: val,
                      gemini_model: toLegacyModel(val),
                    })
                  }
                  options={modelOptions}
                />
              </label>

              <button onClick={() => onSave("api-key")} disabled={saving} className="btn-save-bottom">
                {saving ? "Saving..." : "Save"}
              </button>
            </>
          )}

          {providerTab === "oauth" && (
            <>
              <div className="oauth-status-banner">
                {oauthStatus?.authenticated ? (
                  <p className="meta">
                    已驗證帳號：<code>{oauthStatus.email}</code>
                    {oauthStatus.project_id && (
                      <>
                        {" "}| Project: <code>{oauthStatus.project_id}</code>
                      </>
                    )}
                  </p>
                ) : (
                  <p className="meta">尚未完成 OAuth 綁定，請先新增帳號。</p>
                )}
              </div>

              {pool.length > 0 && (
                <div className="pool-cards compact">
                  {pool.map((entry) => (
                    <div key={entry.email} className={`pool-card ${entry.status !== "available" ? "exhausted" : ""} ${entry.current ? "current" : ""}`}>
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
                            ) : entry.status === "disabled" ? (
                              <><span className="pool-dot disabled" /> 已停用
                                {entry.disabled_reason && (
                                  <span className="pool-reset-hint">({entry.disabled_reason})</span>
                                )}
                                {entry.disabled_until && (
                                  <span className="pool-reset-hint">
                                    — 恢復於 {new Date(entry.disabled_until).toLocaleTimeString()}
                                  </span>
                                )}
                              </>
                            ) : (
                              <><span className="pool-dot cooldown" /> 冷卻中
                                {entry.cooldown_until && (
                                  <span className="pool-reset-hint">
                                    (重置於 {new Date(entry.cooldown_until).toLocaleTimeString()})
                                  </span>
                                )}
                              </>
                            )}
                          </div>
                          {(() => {
                            const attempts24h = entry.attempts_24h ?? entry.request_count ?? 0;
                            const attemptsTotal = entry.attempts_total ?? entry.total_requests ?? 0;
                            const successes24h = entry.successes_24h ?? 0;
                            const failures24h = entry.failures_24h ?? 0;
                            const successesTotal = entry.successes_total ?? 0;
                            const failuresTotal = entry.failures_total ?? 0;
                            const hasUsage =
                              attempts24h > 0 ||
                              attemptsTotal > 0 ||
                              successes24h > 0 ||
                              failures24h > 0 ||
                              successesTotal > 0 ||
                              failuresTotal > 0;
                            if (!hasUsage) return null;
                            return (
                              <div className="pool-card-quota">
                                <div className="pool-quota-row">
                                  <span className="pool-quota-label">24h 嘗試:</span>
                                  <span className="pool-quota-value">{attempts24h}</span>
                                </div>
                                <div className="pool-quota-row">
                                  <span className="pool-quota-label">24h 成功:</span>
                                  <span className="pool-quota-total">{successes24h}</span>
                                </div>
                                <div className="pool-quota-row">
                                  <span className="pool-quota-label">24h 失敗:</span>
                                  <span className="pool-quota-total">{failures24h}</span>
                                </div>
                                <div className="pool-quota-row">
                                  <span className="pool-quota-label">總嘗試:</span>
                                  <span className="pool-quota-total">{attemptsTotal.toLocaleString()}</span>
                                </div>
                                <div className="pool-quota-row">
                                  <span className="pool-quota-label">總成功:</span>
                                  <span className="pool-quota-total">{successesTotal.toLocaleString()}</span>
                                </div>
                                <div className="pool-quota-row">
                                  <span className="pool-quota-label">總失敗:</span>
                                  <span className="pool-quota-total">{failuresTotal.toLocaleString()}</span>
                                </div>
                                {entry.last_attempt_at && (
                                  <div className="pool-quota-row">
                                    <span className="pool-quota-label">最近嘗試:</span>
                                    <span className="pool-quota-time">
                                      {new Date(entry.last_attempt_at).toLocaleString("zh-TW")}
                                    </span>
                                  </div>
                                )}
                                {entry.last_reset_time && (
                                  <div className="pool-quota-row">
                                    <span className="pool-quota-label">窗口起點:</span>
                                    <span className="pool-quota-time">
                                      {new Date(entry.last_reset_time).toLocaleString("zh-TW", {
                                        month: "2-digit",
                                        day: "2-digit",
                                        hour: "2-digit",
                                        minute: "2-digit"
                                      })}
                                    </span>
                                  </div>
                                )}
                              {(entry.consecutive_failures ?? 0) > 0 && (
                                <>
                                  <div className="pool-quota-row">
                                    <span className="pool-quota-label">連續失敗:</span>
                                    <span className="pool-quota-total">{entry.consecutive_failures}</span>
                                  </div>
                                  {entry.last_error && (
                                    <div className="pool-quota-row">
                                      <span className="pool-quota-label">最後錯誤:</span>
                                      <span className="pool-quota-time">
                                        {entry.last_error.slice(0, 64)}
                                        {entry.last_error.length > 64 && "…"}
                                      </span>
                                      <button
                                        type="button"
                                        className="btn-error-detail"
                                        onClick={() => setErrorDetailEmail(entry.email)}
                                        title="檢視完整錯誤"
                                      >
                                        詳細
                                      </button>
                                    </div>
                                  )}
                                </>
                              )}
                              {entry.last_success_at && (
                                <div className="pool-quota-row">
                                  <span className="pool-quota-label">最後成功:</span>
                                  <span className="pool-quota-time">
                                    {new Date(entry.last_success_at).toLocaleString("zh-TW")}
                                  </span>
                                </div>
                              )}
                              </div>
                            );
                          })()}
                        </div>
                      </div>
                      <button
                        className="pool-card-remove"
                        onClick={async () => {
                          if (!confirm(`確定要移除帳號 ${entry.email}？`)) return;
                          await onRemovePoolAccount(entry.email);
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

              <div style={{ display: "flex", gap: "0.5rem", marginTop: pool.length > 0 ? "0.75rem" : 0 }}>
                <button
                  className="btn-secondary pool-add-account-button"
                  onClick={onOpenAddAccount}
                >
                  <span style={{ fontSize: "1.1rem" }}>+</span> 新增帳號
                </button>
                {pool.length > 0 && (
                  <button
                    type="button"
                    className="btn-secondary"
                    onClick={() => { void onVerifyPool(selectedOAuthProviderID); }}
                    disabled={verifyingPool}
                  >
                    {verifyingPool ? "驗證中..." : "驗證全部帳號"}
                  </button>
                )}
              </div>

              {pool.length > 0 && (
                <div className="oauth-section-subtle">
                  <label className="form-field">
                    <span>OAuth Provider</span>
                    <Select
                      value={selectedOAuthProviderID}
                      onChange={(val) => {
                        const providerID = val === "google-antigravity" ? "google-antigravity" : "google-gemini-cli";
                        setForm({
                          ...form,
                          provider_id: providerID,
                          model_id: providerID === "google-antigravity"
                            ? "google-antigravity/claude-opus-4-6-thinking"
                            : "google-gemini-cli/gemini-3-pro-preview",
                          gemini_model: providerID === "google-antigravity" ? "claude-opus-4-6-thinking" : "gemini-3-pro-preview",
                        });
                      }}
                      options={[
                        { value: "google-antigravity", label: "Google Antigravity (OAuth Pool / Recommended)" },
                        { value: "google-gemini-cli", label: "Google Gemini CLI (Local CLI Runtime)" },
                      ]}
                    />
                    <span className="hint">
                      Google Antigravity 走 OAuth 帳號池（Cloud Code Assist）；Google Gemini CLI 走伺服器本機 `gemini` CLI。
                    </span>
                  </label>

                  <label className="form-field">
                    <span>Model</span>
                    <Select
                      value={currentModelValue}
                      onChange={(val) =>
                        setForm({
                          ...form,
                          model_id: val,
                          gemini_model: toLegacyModel(val),
                        })
                      }
                      options={modelOptions}
                    />
                  </label>

                  <button onClick={() => onSave("oauth")} disabled={saving} className="btn-save-bottom">
                    {saving ? "Saving..." : "Use OAuth Provider"}
                  </button>
                </div>
              )}
            </>
          )}
        </div>
      </section>

      {addAccountOpen && (
        <div className="modal-overlay" onClick={() => !isModalBusy && onCloseAddAccount()}>
          <div className="modal-content" onClick={(e) => e.stopPropagation()}>
            <div className="modal-header">
              <h3 className="modal-title">新增 Google 帳號</h3>
              <button
                className="modal-close"
                onClick={() => !isModalBusy && onCloseAddAccount()}
                disabled={isModalBusy}
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

              <div className="oauth-detect-block">
                <p className="meta oauth-meta" style={{ marginBottom: "0.5rem" }}>
                  推薦：直接使用 Google OAuth 授權，會自動加入帳號池。
                </p>
                <div style={{ display: "flex", gap: "0.5rem", marginBottom: "0.75rem" }}>
                  <button
                    className={oauthMode === "local" ? "btn-primary" : "btn-secondary"}
                    type="button"
                    onClick={() => setOAuthMode("local")}
                    disabled={isModalBusy}
                  >
                    Local 模式
                  </button>
                  <button
                    className={oauthMode === "remote" ? "btn-primary" : "btn-secondary"}
                    type="button"
                    onClick={() => setOAuthMode("remote")}
                    disabled={isModalBusy}
                  >
                    Remote 模式
                  </button>
                </div>
                <button
                  onClick={() => onStartOAuth(selectedOAuthProviderID)}
                  disabled={isModalBusy}
                  className="btn-primary"
                >
                  {oauthStarting ? "連線中..." : "Connect Google Account"}
                </button>
                {oauthFlow && (
                  <div className="meta oauth-meta" style={{ marginTop: "0.6rem" }}>
                    <div>
                      flow_id: <code>{oauthFlow.flow_id}</code>
                    </div>
                    <div>
                      expires: <code>{new Date(oauthFlow.expires_at).toLocaleString("zh-TW")}</code>
                    </div>
                  </div>
                )}
              </div>

              {(oauthFlow || oauthMode === "remote") && (
                <div className="oauth-detect-block">
                  <p className="meta oauth-meta" style={{ marginBottom: "0.5rem" }}>
                    若無法自動 callback，請把瀏覽器最終網址（含 <code>code</code> 與 <code>state</code>）貼到這裡：
                  </p>
                  <textarea
                    value={oauthCallbackURL}
                    onChange={(e) => setOAuthCallbackURL(e.target.value)}
                    placeholder="http://localhost:8080/api/oauth/gemini/callback?code=...&state=..."
                    rows={3}
                    style={{ width: "100%", fontFamily: "monospace", fontSize: "0.85rem", marginBottom: "0.5rem" }}
                    disabled={oauthCompleting}
                  />
                  <button
                    onClick={onCompleteOAuth}
                    disabled={oauthCompleting || !oauthCallbackURL.trim() || !oauthFlow}
                    className="btn-secondary"
                  >
                    {oauthCompleting ? "完成中..." : "Complete OAuth"}
                  </button>
                </div>
              )}

              <hr style={{ margin: "1rem 0", borderColor: "var(--color-border)" }} />

              <div className="oauth-detect-block">
                <p className="meta oauth-meta" style={{ marginBottom: "0.5rem" }}>
                  Fallback：在本地電腦執行 Gemini CLI 登入後，貼上 Token 檔案內容。
                </p>
                <ol className="meta oauth-meta" style={{ margin: "0 0 0.75rem 1.2rem", padding: 0, lineHeight: 1.8 }}>
                  <li>安裝並登入：<code>npx @google/gemini-cli</code></li>
                  <li>複製 Token：<code>cat ~/.gemini/oauth_creds.json</code></li>
                  <li>貼到下方，點擊匯入</li>
                </ol>
                <textarea
                  value={pastedTokenJSON}
                  onChange={(e) => setPastedTokenJSON(e.target.value)}
                  placeholder='{"access_token":"ya29...","refresh_token":"1//...","token_type":"Bearer","expiry_date":1234567890000}'
                  rows={3}
                  style={{ width: "100%", fontFamily: "monospace", fontSize: "0.85rem", marginBottom: "0.5rem" }}
                  disabled={importingPasted}
                />
                <button
                  onClick={() => onImportPastedToken(selectedOAuthProviderID)}
                  disabled={importingPasted || !pastedTokenJSON.trim()}
                  className="btn-primary"
                >
                  {importingPasted ? "匯入中..." : "Import Token"}
                </button>
              </div>

              <hr style={{ margin: "1rem 0", borderColor: "var(--color-border)" }} />

              <div className="oauth-detect-block">
                <button
                  onClick={() => onImportCLIToken(selectedOAuthProviderID)}
                  disabled={importingCLI}
                  className="btn-secondary"
                >
                  {importingCLI ? "匯入中..." : "Import from Server"}
                </button>
                <p className="meta oauth-meta">
                  可 SSH 的環境：在伺服器上執行 <code>gemini</code> 登入後，直接讀取 Token
                </p>
              </div>
            </div>
          </div>
        </div>
      )}

      {errorDetailEmail && (() => {
        const entry = pool.find((e) => e.email === errorDetailEmail);
        if (!entry?.last_error) return null;
        return (
          <div className="modal-overlay" onClick={() => setErrorDetailEmail(null)}>
            <div className="modal-content" onClick={(e) => e.stopPropagation()}>
              <div className="modal-header">
                <h3 className="modal-title">錯誤詳情 — {entry.email}</h3>
                <button className="modal-close" onClick={() => setErrorDetailEmail(null)}>
                  &times;
                </button>
              </div>
              <div className="modal-body">
                <pre style={{
                  whiteSpace: "pre-wrap",
                  wordBreak: "break-all",
                  fontSize: "0.85rem",
                  fontFamily: "monospace",
                  background: "rgba(0,0,0,0.3)",
                  padding: "1rem",
                  borderRadius: "6px",
                  maxHeight: "60vh",
                  overflow: "auto",
                }}>
                  {entry.last_error}
                </pre>
              </div>
            </div>
          </div>
        );
      })()}
    </>
  );
}
