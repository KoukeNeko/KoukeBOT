import { useState, type Dispatch, type SetStateAction } from "react";
import Select from "../../components/ui/Select";
import Checkbox from "../../components/ui/Checkbox";
import type { SettingsData, OAuthStatus, PoolEntry, OAuthStartResult, AIStudioSessionStatus } from "../../types";
import type { AIStudioImportResult } from "../../features/settings/utils/aistudioImport";

export type ProviderTab = "api-key" | "oauth" | "pool";

type SelectOption = {
  value: string;
  label: string;
};

type StatusMessage = {
  type: "ok" | "err";
  text: string;
} | null;

type AIStudioStatusMessage = {
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
  aistudioStatus: AIStudioSessionStatus | null;
  aistudioFileName: string;
  aistudioPastedInput: string;
  aistudioImport: AIStudioImportResult | null;
  aistudioMessage: AIStudioStatusMessage;
  aistudioUploading: boolean;
  aistudioVerifyingCurrent: boolean;
  aistudioDeleting: boolean;
  setAIStudioPastedInput: (value: string) => void;
  onAIStudioFileSelected: (file: File | null) => Promise<void>;
  onAIStudioParsePastedInput: () => boolean;
  onAIStudioUploadVerifyApply: () => Promise<boolean>;
  onAIStudioVerifyCurrent: () => Promise<boolean>;
  onAIStudioDelete: () => Promise<boolean>;
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
  aistudioStatus,
  aistudioFileName,
  aistudioPastedInput,
  aistudioImport,
  aistudioMessage,
  aistudioUploading,
  aistudioVerifyingCurrent,
  aistudioDeleting,
  setAIStudioPastedInput,
  onAIStudioFileSelected,
  onAIStudioParsePastedInput,
  onAIStudioUploadVerifyApply,
  onAIStudioVerifyCurrent,
  onAIStudioDelete,
}: Props) {
  const isModalBusy = oauthStarting || oauthCompleting;
  const [errorDetailEmail, setErrorDetailEmail] = useState<string | null>(null);
  const selectedOAuthPoolProviderID =
    form.provider_id === "google-gemini-cli" ? "google-gemini-cli" : "google-antigravity";
  const selectedProviderID =
    form.provider_id === "google-antigravity" ||
    form.provider_id === "google-gemini-cli" ||
    form.provider_id === "aistudio-rpc"
      ? form.provider_id
      : "google-antigravity";
  const defaultModelForProvider = (providerID: string) => {
    switch (providerID) {
      case "google-antigravity":
        return "google-antigravity/claude-opus-4-6-thinking";
      case "aistudio-rpc":
        return "aistudio-rpc/gemini-3-pro-preview";
      default:
        return "google-gemini-cli/gemini-3-pro-preview";
    }
  };
  const currentModelValue = form.model_id || form.gemini_model;
  const toLegacyModel = (modelID: string) =>
    modelID.includes("/") ? modelID.split("/").slice(1).join("/") : modelID;
  const summarizeSecret = (value?: string) => {
    const text = (value ?? "").trim();
    if (!text) {
      return "(empty)";
    }
    return `set(len=${text.length})`;
  };

  const fallbackValueFromSettings = form.fallback_model_id || form.fallback_model || "";
  const resolvedFallbackValue =
    modelOptions.find((option) => option.value === fallbackValueFromSettings)?.value ||
    modelOptions.find((option) => option.value.endsWith(`/${fallbackValueFromSettings}`))?.value ||
    fallbackValueFromSettings;
  const fallbackOptions: SelectOption[] = [
    { value: "", label: "不使用備用模型" },
    ...modelOptions,
  ];
  if (
    resolvedFallbackValue &&
    !fallbackOptions.some((option) => option.value === resolvedFallbackValue)
  ) {
    fallbackOptions.splice(1, 0, {
      value: resolvedFallbackValue,
      label: resolvedFallbackValue,
    });
  }

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

              <div className="oauth-section-subtle">
                <label className="form-field">
                  <span>Provider</span>
                  <Select
                    value={selectedProviderID}
                    onChange={(val) => {
                      const providerID =
                        val === "google-antigravity" || val === "google-gemini-cli" || val === "aistudio-rpc"
                          ? val
                          : "google-antigravity";
                      const modelID = defaultModelForProvider(providerID);
                      setForm({
                        ...form,
                        provider_id: providerID,
                        model_id: modelID,
                        gemini_model: toLegacyModel(modelID),
                      });
                    }}
                    options={[
                      { value: "google-antigravity", label: "Google Antigravity (OAuth Pool / Recommended)" },
                      { value: "google-gemini-cli", label: "Google Gemini CLI (Local CLI Runtime)" },
                      { value: "aistudio-rpc", label: "AI Studio RPC (Direct / Test)" },
                    ]}
                  />
                  <span className="hint">
                    Antigravity 走 OAuth 帳號池；Gemini CLI 走本機 `gemini`；AI Studio RPC 可強制直連（測試用）。
                  </span>
                  {selectedProviderID === "aistudio-rpc" && !aistudioStatus?.configured && (
                    <span className="hint" style={{ color: "#fca5a5" }}>
                      尚未設定 AI Studio session，套用後請先到下方 AI Studio Fallback 區塊上傳並驗證。
                    </span>
                  )}
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
                  {saving ? "Saving..." : "Use Selected Provider"}
                </button>
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
                    onClick={() => { void onVerifyPool(selectedOAuthPoolProviderID); }}
                    disabled={verifyingPool}
                  >
                    {verifyingPool ? "驗證中..." : "驗證全部帳號"}
                  </button>
                )}
              </div>
            </>
          )}
        </div>
      </section>

      <section className="settings-section">
        <h3 className="settings-section-title">AI Studio Fallback</h3>

        <div className={`oauth-status-banner ${aistudioStatus?.configured ? "" : "aistudio-status-warn"}`}>
          {aistudioStatus?.configured ? (
            <p className="meta">
              已設定 AI Studio session（OAuth exhausted 時可自動切換）。
            </p>
          ) : (
            <p className="meta">尚未設定 AI Studio session。</p>
          )}
        </div>

        <div className="aistudio-ops-guide">
          <p className="meta aistudio-ops-title">操作說明（快速）</p>
          <ol className="aistudio-ops-list">
            <li>先上傳或貼上 HAR / session JSON / cURL，並點 `Parse Pasted Text`。</li>
            <li>確認摘要包含有效 `cookie` 或 `authorization` 後，點 `Upload & Verify & Apply`。</li>
            <li>若只做備援：維持 OAuth provider；若要強制測試：上方 Provider 選 `AI Studio RPC (Direct / Test)` 並儲存。</li>
            <li>憑證過期時重新擷取最新 HAR/cURL 再套用。</li>
          </ol>
          <p className="hint">詳細文件：<code>docs/runbook/aistudio-rpc.md</code></p>
        </div>

        <label className="form-field">
          <span>Upload HAR / Session JSON / cURL</span>
          <input
            type="file"
            accept=".har,.json,.txt,.sh,application/json,text/plain"
            onChange={(e) => {
              const file = e.target.files?.[0] ?? null;
              void onAIStudioFileSelected(file);
            }}
          />
          <span className="hint">
            可上傳 AI Studio HAR、`aistudio_session.json` 或含 cURL 指令的文字檔。流程會先 verify，成功才套用。
          </span>
          {aistudioFileName && (
            <span className="hint">已選擇：<code>{aistudioFileName}</code></span>
          )}
        </label>

        <label className="form-field">
          <span>Paste HAR / Session JSON / cURL</span>
          <textarea
            value={aistudioPastedInput}
            onChange={(e) => setAIStudioPastedInput(e.target.value)}
            placeholder="貼上完整 cURL、HAR JSON 或 aistudio_session.json 內容"
            rows={7}
            style={{ width: "100%", fontFamily: "monospace", fontSize: "0.85rem" }}
            disabled={aistudioUploading}
          />
          <span className="hint">
            貼上後先點 Parse，確認摘要與 warning，再執行 verify/apply。
          </span>
        </label>

        {aistudioImport && (
          <div className="aistudio-import-summary">
            <div className="meta">來源：<code>{aistudioImport.sourceType}</code></div>
            <div className="aistudio-import-grid">
              <div className="aistudio-import-row">
                <span>model</span>
                <code>{aistudioImport.model || "(auto)"}</code>
              </div>
              <div className="aistudio-import-row">
                <span>endpoint</span>
                <code>{aistudioImport.session.endpoint || "(empty)"}</code>
              </div>
              <div className="aistudio-import-row">
                <span>api_key</span>
                <code>{summarizeSecret(aistudioImport.session.api_key)}</code>
              </div>
              <div className="aistudio-import-row">
                <span>x_browser_validation</span>
                <code>{summarizeSecret(aistudioImport.session.x_browser_validation)}</code>
              </div>
              <div className="aistudio-import-row">
                <span>x_client_data</span>
                <code>{summarizeSecret(aistudioImport.session.x_client_data)}</code>
              </div>
              <div className="aistudio-import-row">
                <span>x_goog_authuser</span>
                <code>{summarizeSecret(aistudioImport.session.x_goog_authuser)}</code>
              </div>
              <div className="aistudio-import-row">
                <span>x_goog_ext_519733851_bin</span>
                <code>{summarizeSecret(aistudioImport.session.x_goog_ext_519733851_bin)}</code>
              </div>
              <div className="aistudio-import-row">
                <span>cookie</span>
                <code>{summarizeSecret(aistudioImport.session.cookie)}</code>
              </div>
              <div className="aistudio-import-row">
                <span>authorization</span>
                <code>{summarizeSecret(aistudioImport.session.authorization)}</code>
              </div>
            </div>
            {aistudioImport.warnings.length > 0 && (
              <ul className="aistudio-warning-list">
                {aistudioImport.warnings.map((warning, idx) => (
                  <li key={`${warning}-${idx}`}>{warning}</li>
                ))}
              </ul>
            )}
          </div>
        )}

        {aistudioMessage && (
          <p className={aistudioMessage.type === "ok" ? "success message" : "error message"}>
            {aistudioMessage.text}
          </p>
        )}

        <div className="aistudio-actions-row">
          <button
            type="button"
            className="btn-secondary aistudio-btn"
            onClick={() => { onAIStudioParsePastedInput(); }}
            disabled={aistudioUploading || !aistudioPastedInput.trim()}
          >
            Parse Pasted Text
          </button>
          <button
            type="button"
            className="btn-primary aistudio-btn"
            onClick={() => { void onAIStudioUploadVerifyApply(); }}
            disabled={aistudioUploading || !aistudioImport}
          >
            {aistudioUploading ? "Verifying..." : "Upload & Verify & Apply"}
          </button>
          <button
            type="button"
            className="btn-secondary aistudio-btn"
            onClick={() => { void onAIStudioVerifyCurrent(); }}
            disabled={aistudioVerifyingCurrent || !aistudioStatus?.configured}
          >
            {aistudioVerifyingCurrent ? "Verifying..." : "Verify Current Session"}
          </button>
          <button
            type="button"
            className="btn-secondary aistudio-btn"
            onClick={() => {
              if (!aistudioStatus?.configured) return;
              if (!confirm("確定要刪除 AI Studio session？")) return;
              void onAIStudioDelete();
            }}
            disabled={aistudioDeleting || !aistudioStatus?.configured}
          >
            {aistudioDeleting ? "Deleting..." : "Delete Session"}
          </button>
        </div>
      </section>

      <section className="settings-section">
        <h3 className="settings-section-title">備用模型（Fallback）</h3>

        <div className="form-field-checkbox-row">
          <Checkbox
            checked={form.fallback_model_enabled}
            onChange={(checked) =>
              setForm({ ...form, fallback_model_enabled: checked })
            }
            label="啟用模型降級（主模型無容量時自動切換備用模型）"
          />
        </div>

        <label className="form-field">
          <span>備用模型</span>
          <Select
            value={resolvedFallbackValue}
            onChange={(val) =>
              setForm({
                ...form,
                fallback_model_id: val,
                fallback_model: toLegacyModel(val),
              })
            }
            options={fallbackOptions}
            disabled={!form.fallback_model_enabled}
          />
          <span className="hint">
            主模型因容量不足（MODEL_CAPACITY）失敗時，會嘗試此備用模型。預設關閉。
          </span>
        </label>

        <button onClick={() => onSave()} disabled={saving} className="btn-save-bottom">
          {saving ? "Saving..." : "Save"}
        </button>
      </section>

      <section className="settings-section">
        <h3 className="settings-section-title">請求佇列（Request Queue）</h3>

        <div className="form-field-checkbox-row">
          <Checkbox
            checked={form.request_queue_enabled}
            onChange={(checked) =>
              setForm({ ...form, request_queue_enabled: checked })
            }
            label="啟用請求佇列（所有帳號冷卻時自動排隊等待）"
          />
        </div>

        <label className="form-field">
          <span>佇列容量上限</span>
          <input
            type="number"
            min={1}
            max={50}
            value={form.request_queue_capacity}
            onChange={(e) =>
              setForm({
                ...form,
                request_queue_capacity: Number.parseInt(e.target.value || "0", 10),
              })
            }
            disabled={!form.request_queue_enabled}
          />
          <span className="hint">
            同時排隊等待的請求數量上限（1-50）。超過上限的請求會直接回傳錯誤。
          </span>
        </label>

        <label className="form-field">
          <span>等待逾時（秒）</span>
          <input
            type="number"
            min={10}
            max={300}
            value={form.request_queue_timeout_seconds}
            onChange={(e) =>
              setForm({
                ...form,
                request_queue_timeout_seconds: Number.parseInt(e.target.value || "0", 10),
              })
            }
            disabled={!form.request_queue_enabled}
          />
          <span className="hint">
            請求在佇列中最長等待時間（10-300 秒）。逾時後回傳錯誤。
          </span>
        </label>

        <button onClick={() => onSave()} disabled={saving} className="btn-save-bottom">
          {saving ? "Saving..." : "Save"}
        </button>
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
                  onClick={() => onStartOAuth(selectedOAuthPoolProviderID)}
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
                  onClick={() => onImportPastedToken(selectedOAuthPoolProviderID)}
                  disabled={importingPasted || !pastedTokenJSON.trim()}
                  className="btn-primary"
                >
                  {importingPasted ? "匯入中..." : "Import Token"}
                </button>
              </div>

              <hr style={{ margin: "1rem 0", borderColor: "var(--color-border)" }} />

              <div className="oauth-detect-block">
                <button
                  onClick={() => onImportCLIToken(selectedOAuthPoolProviderID)}
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
