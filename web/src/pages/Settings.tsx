import { useEffect, useState, useCallback } from "react";
import {
  startGeminiOAuth,
  completeGeminiOAuth,
  getSettings,
  updateSettings,
  getProviderStatus,
  getRuntimeMetrics,
  importGeminiCLIToken,
  importPastedToken,
  getOAuthStatus,
  getOAuthPool,
  removeOAuthPoolAccount,
  listModels,
  listPersonas,
} from "../api";
import ProviderSection, { type ProviderTab } from "./settings/ProviderSection";
import DiscordSection from "./settings/DiscordSection";
import PromptGuardSection from "./settings/PromptGuardSection";
import ToolResilienceSection from "./settings/ToolResilienceSection";
import RuntimeMetricsCard from "./settings/RuntimeMetricsCard";
import ToolsSection from "./settings/ToolsSection";
import TimeLocationSection from "./settings/TimeLocationSection";
import MemorySection from "./settings/MemorySection";
import type {
  SettingsData,
  ProviderStatus,
  RuntimeMetrics,
  OAuthStatus,
  ModelEntry,
  PoolEntry,
  PersonaSummary,
} from "../types";

import "./Settings.css"; // Import the new CSS file

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
    discord_persona_id: "",
    discord_observe_enabled: false,
    discord_member_intent_enabled: false,
    discord_presence_intent_enabled: false,
    discord_observe_max_results: 25,
    discord_observe_message_scope: "current_guild",
    max_tool_rounds: 3,
    max_web_search_calls_per_turn: 2,
    force_final_answer_after_tool_limit: true,
    transient_recovery_wait_enabled: true,
    transient_recovery_wait_max_seconds: 5,
    discord_show_internal_tool_errors: true,
    persona_lock_every_turns: 4,
    triplet_output_enabled: true,
    analysis_visibility: "visible",
    negative_constraint_enabled: true,
    negative_constraint_default:
      "You are FORBIDDEN from switching into a generic assistant tone. Keep persona-specific stance, diction, and attitude.",
  });
  const [models, setModels] = useState<ModelEntry[]>([]);
  const [personas, setPersonas] = useState<PersonaSummary[]>([]);
  const [status, setStatus] = useState<ProviderStatus | null>(null);
  const [runtimeMetrics, setRuntimeMetrics] = useState<RuntimeMetrics | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [message, setMessage] = useState<{
    type: "ok" | "err";
    text: string;
  } | null>(null);

  // OAuth state
  const [providerTab, setProviderTab] = useState<ProviderTab>("api-key");
  const [oauthStatus, setOAuthStatus] = useState<OAuthStatus | null>(null);
  const [oauthStarting, setOAuthStarting] = useState(false);
  const [oauthCompleting, setOAuthCompleting] = useState(false);
  const [oauthMode, setOAuthMode] = useState<"local" | "remote">(() => {
    const host = window.location.hostname.toLowerCase();
    return host === "localhost" || host === "127.0.0.1" || host === "::1"
      ? "local"
      : "remote";
  });
  const [oauthFlow, setOAuthFlow] = useState<{
    flow_id: string;
    auth_url: string;
    expires_at: string;
    mode: "local" | "remote";
  } | null>(null);
  const [oauthCallbackURL, setOAuthCallbackURL] = useState("");
  const [addAccountOpen, setAddAccountOpen] = useState(false);
  const [importingCLI, setImportingCLI] = useState(false);
  const [pastedTokenJSON, setPastedTokenJSON] = useState("");
  const [importingPasted, setImportingPasted] = useState(false);

  // Pool state
  const [pool, setPool] = useState<PoolEntry[]>([]);
  const [removingEmail, setRemovingEmail] = useState<string | null>(null);

  const refreshPool = useCallback(async (): Promise<PoolEntry[]> => {
    try {
      const entries = await getOAuthPool();
      setPool(entries);
      return entries;
    } catch {
      // Pool endpoint may not be available if no pool is active
      setPool([]);
      return [];
    }
  }, []);

  const refreshOAuthSnapshot = useCallback(async () => {
    const [oauth, st, poolEntries] = await Promise.all([
      getOAuthStatus(),
      getProviderStatus(),
      refreshPool(),
    ]);
    setOAuthStatus(oauth);
    setStatus(st);
    setPool(poolEntries);
    return { oauth, poolEntries };
  }, [refreshPool]);

  const refreshRuntimeMetrics = useCallback(async () => {
    try {
      const metrics = await getRuntimeMetrics();
      setRuntimeMetrics(metrics);
    } catch {
      // Ignore metrics errors for settings UX.
    }
  }, []);

  useEffect(() => {
    Promise.all([
      getSettings(),
      getProviderStatus(),
      getRuntimeMetrics().catch(() => null as RuntimeMetrics | null),
      getOAuthStatus(),
      listModels(),
      listPersonas(),
      getOAuthPool().catch(() => [] as PoolEntry[]),
    ])
      .then(([s, st, metrics, oauth, modelList, personaList, poolEntries]) => {
        setForm(s);
        setStatus(st);
        setRuntimeMetrics(metrics);
        setOAuthStatus(oauth);
        setModels(modelList);
        setPersonas(personaList);
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

  useEffect(() => {
    const id = window.setInterval(() => {
      void refreshRuntimeMetrics();
    }, 10_000);
    return () => window.clearInterval(id);
  }, [refreshRuntimeMetrics]);

  const handleSave = async (overrideProviderType?: string) => {
    setSaving(true);
    setMessage(null);
    try {
      const payload = { ...form };
      if (overrideProviderType) {
        payload.provider_type = overrideProviderType;
      }
      const result = await updateSettings(payload);
      const [st, updated, modelList] = await Promise.all([
        getProviderStatus(),
        getSettings(),
        listModels(),
      ]);
      setStatus(st);
      setForm(updated);
      setModels(modelList);
      if (result.warning) {
        setMessage({ type: "ok", text: `設定已儲存。警告：${result.warning}` });
      } else {
        setMessage({ type: "ok", text: "Settings saved successfully." });
      }
    } catch (e) {
      setMessage({
        type: "err",
        text: e instanceof Error ? e.message : "Save failed",
      });
    } finally {
      setSaving(false);
    }
  };

  const pollOAuthCompletion = useCallback(
    async (previousPoolSize: number, timeoutMs: number): Promise<boolean> => {
      const deadline = Date.now() + timeoutMs;
      while (Date.now() < deadline) {
        await new Promise((resolve) => setTimeout(resolve, 1500));
        try {
          const { oauth, poolEntries } = await refreshOAuthSnapshot();
          const poolSize = poolEntries.length;
          if (poolSize > previousPoolSize || (oauth.pool_size ?? 0) > previousPoolSize) {
            return true;
          }
        } catch {
          // Ignore polling error and continue until timeout.
        }
      }
      return false;
    },
    [refreshOAuthSnapshot],
  );

  const handleStartOAuth = async () => {
    setOAuthStarting(true);
    setMessage(null);
    const previousPoolSize = pool.length;
    try {
      const start = await startGeminiOAuth({ mode: oauthMode });
      setOAuthFlow(start);
      setOAuthCallbackURL("");

      const popup = window.open(
        start.auth_url,
        "koukebot-oauth",
        "popup=yes,width=560,height=760",
      );
      if (!popup) {
        throw new Error("瀏覽器封鎖了授權視窗，請允許 popup 後重試");
      }
      popup.focus();

      const completed = await pollOAuthCompletion(
        previousPoolSize,
        start.mode === "local" ? 120_000 : 45_000,
      );
      if (completed) {
        setOAuthFlow(null);
        setAddAccountOpen(false);
        setMessage({ type: "ok", text: "Google 帳號已連線並加入帳號池。" });
        return;
      }

      if (start.mode === "remote") {
        setMessage({
          type: "ok",
          text: "已開啟 OAuth 授權頁。若無法自動完成，請將 callback URL 貼到下方完成綁定。",
        });
      } else {
        setMessage({
          type: "ok",
          text: "授權視窗已完成，但尚未偵測到回寫。可將 callback URL 貼到下方手動完成。",
        });
      }
    } catch (e) {
      setMessage({
        type: "err",
        text: e instanceof Error ? e.message : "OAuth 啟動失敗",
      });
    } finally {
      setOAuthStarting(false);
    }
  };

  const handleCompleteOAuth = async () => {
    if (!oauthFlow) {
      setMessage({ type: "err", text: "找不到 OAuth flow，請重新開始" });
      return;
    }
    if (!oauthCallbackURL.trim()) {
      setMessage({ type: "err", text: "請貼上 callback URL" });
      return;
    }

    setOAuthCompleting(true);
    setMessage(null);
    try {
      const result = await completeGeminiOAuth(oauthFlow.flow_id, oauthCallbackURL.trim());
      await refreshOAuthSnapshot();
      setOAuthFlow(null);
      setOAuthCallbackURL("");
      setAddAccountOpen(false);
      setMessage({ type: "ok", text: `OAuth 完成：${result.email}` });
    } catch (e) {
      setMessage({
        type: "err",
        text: e instanceof Error ? e.message : "OAuth 完成失敗",
      });
    } finally {
      setOAuthCompleting(false);
    }
  };

  const handleImportCLIToken = async () => {
    setImportingCLI(true);
    setMessage(null);
    try {
      const result = await importGeminiCLIToken();
      await refreshOAuthSnapshot();
      setMessage({ type: "ok", text: `已匯入 Gemini CLI Token：${result.email}` });
      setAddAccountOpen(false);
    } catch (e) {
      setMessage({ type: "err", text: e instanceof Error ? e.message : "匯入失敗" });
    } finally {
      setImportingCLI(false);
    }
  };

  const handleImportPastedToken = async () => {
    if (!pastedTokenJSON.trim()) return;
    setImportingPasted(true);
    setMessage(null);
    try {
      const result = await importPastedToken(pastedTokenJSON.trim());
      await refreshOAuthSnapshot();
      setMessage({ type: "ok", text: `已匯入 Token：${result.email}` });
      setAddAccountOpen(false);
      setPastedTokenJSON("");
    } catch (e) {
      setMessage({ type: "err", text: e instanceof Error ? e.message : "匯入失敗" });
    } finally {
      setImportingPasted(false);
    }
  };

  const handleRemovePoolAccount = async (email: string) => {
    setRemovingEmail(email);
    try {
      await removeOAuthPoolAccount(email);
      await refreshOAuthSnapshot();
      setMessage({ type: "ok", text: `已移除 ${email}` });
    } catch (e) {
      setMessage({ type: "err", text: e instanceof Error ? e.message : "移除失敗" });
    } finally {
      setRemovingEmail(null);
    }
  };

  const handleOpenAddAccount = () => {
    setOAuthFlow(null);
    setOAuthCallbackURL("");
    setAddAccountOpen(true);
  };

  const handleCloseAddAccount = () => {
    if (oauthStarting || oauthCompleting) return;
    setAddAccountOpen(false);
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
  const personaOptions = personas.map((p) => ({ value: p.id, label: `${p.name} (${p.id})` }));

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
      )}

      <RuntimeMetricsCard runtimeMetrics={runtimeMetrics} />

      <ProviderSection
        form={form}
        setForm={setForm}
        modelOptions={modelOptions}
        providerTab={providerTab}
        setProviderTab={setProviderTab}
        oauthStatus={oauthStatus}
        pool={pool}
        removingEmail={removingEmail}
        onRemovePoolAccount={handleRemovePoolAccount}
        saving={saving}
        onSave={handleSave}
        addAccountOpen={addAccountOpen}
        onOpenAddAccount={handleOpenAddAccount}
        onCloseAddAccount={handleCloseAddAccount}
        oauthStarting={oauthStarting}
        oauthCompleting={oauthCompleting}
        oauthMode={oauthMode}
        setOAuthMode={setOAuthMode}
        oauthFlow={oauthFlow}
        oauthCallbackURL={oauthCallbackURL}
        setOAuthCallbackURL={setOAuthCallbackURL}
        onStartOAuth={handleStartOAuth}
        onCompleteOAuth={handleCompleteOAuth}
        message={message}
        pastedTokenJSON={pastedTokenJSON}
        setPastedTokenJSON={setPastedTokenJSON}
        importingPasted={importingPasted}
        onImportPastedToken={handleImportPastedToken}
        importingCLI={importingCLI}
        onImportCLIToken={handleImportCLIToken}
      />

      <DiscordSection
        form={form}
        setForm={setForm}
        personaOptions={personaOptions}
        saving={saving}
        onSave={() => handleSave()}
      />

      <PromptGuardSection
        form={form}
        setForm={setForm}
        saving={saving}
        onSave={() => handleSave()}
      />

      <ToolResilienceSection
        form={form}
        setForm={setForm}
        saving={saving}
        onSave={() => handleSave()}
      />

      <ToolsSection form={form} setForm={setForm} saving={saving} onSave={() => handleSave()} />

      <TimeLocationSection form={form} setForm={setForm} saving={saving} onSave={() => handleSave()} />

      <MemorySection
        form={form}
        setForm={setForm}
        modelOptions={modelOptions}
        saving={saving}
        onSave={() => handleSave()}
      />
    </div>
  );
}
