import type { Dispatch, SetStateAction } from "react";
import Select from "../../components/ui/Select";
import Checkbox from "../../components/ui/Checkbox";
import type { SettingsData } from "../../types";

type SelectOption = {
  value: string;
  label: string;
};

type Props = {
  form: SettingsData;
  setForm: Dispatch<SetStateAction<SettingsData>>;
  personaOptions: SelectOption[];
  saving: boolean;
  onSave: () => void;
};

export default function DiscordSection({ form, setForm, personaOptions, saving, onSave }: Props) {
  return (
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

      <label className="form-field">
        <span>Discord Bot 角色</span>
        <Select
          value={form.discord_persona_id || ""}
          onChange={(val) => setForm({ ...form, discord_persona_id: val })}
          options={personaOptions}
          placeholder="選擇 Discord bot 預設角色"
          disabled={personaOptions.length === 0}
        />
        <span className="hint">
          儲存後會立即切換 Discord Bot 使用的角色，可在 /persona 指令選單尚未更新時先使用。
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

      <div className="form-field-checkbox-row">
        <Checkbox
          checked={form.discord_observe_enabled}
          onChange={(checked) => setForm({ ...form, discord_observe_enabled: checked })}
          label="啟用 Discord Observe 工具（唯讀）"
        />
        <p className="hint-block">
          讓 LLM 可讀取目前 guild 內的狀態/頻道/訊息（不含任何寫入操作）。預設關閉。
          變更工具開關後需重啟 bot 套用。
        </p>
      </div>

      {form.discord_observe_enabled && (
        <>
          <label className="form-field">
            <span>訊息讀取範圍</span>
            <Select
              value={form.discord_observe_message_scope}
              onChange={(val) =>
                setForm({
                  ...form,
                  discord_observe_message_scope: val as SettingsData["discord_observe_message_scope"],
                })
              }
              options={[
                { value: "current_guild", label: "current_guild（可讀同 guild 任一頻道）" },
                { value: "current_channel", label: "current_channel（僅當前頻道）" },
              ]}
            />
          </label>

          <label className="form-field">
            <span>單次最大結果數</span>
            <input
              type="number"
              min={1}
              max={100}
              value={form.discord_observe_max_results}
              onChange={(e) =>
                setForm({
                  ...form,
                  discord_observe_max_results: Number.parseInt(e.target.value || "25", 10),
                })
              }
            />
          </label>

          <div className="form-field-checkbox-row">
            <Checkbox
              checked={form.discord_member_intent_enabled}
              onChange={(checked) => setForm({ ...form, discord_member_intent_enabled: checked })}
              label="啟用 Member Intent（list_members）"
            />
          </div>

          <div className="form-field-checkbox-row">
            <Checkbox
              checked={form.discord_presence_intent_enabled}
              onChange={(checked) => setForm({ ...form, discord_presence_intent_enabled: checked })}
              label="啟用 Presence Intent（list_presences）"
            />
          </div>

          <p className="hint-block">
            Intents 變更需要同時在 Discord Developer Portal 開啟對應 Privileged Gateway Intents，且重啟 bot 才會生效。
          </p>
        </>
      )}

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
                  ["Send Messages", "發送回覆訊息"],
                  ["Send Messages in Threads", "在討論串中回覆"],
                  ["Embed Links", "回覆中包含連結預覽"],
                  ["Attach Files", "發送圖片或檔案"],
                  ["Read Message History", "讀取對話歷史"],
                  ["Use Slash Commands", "支援指令"],
                  ["Add Reactions", "加表情符號回應 (可選)"],
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
            {["View Channels", "Send Messages", "Read Message History", "Use Slash Commands"].map(p => (
              <li key={p} className="discord-min-permissions-list-item">
                <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeLinecap="round" strokeLinejoin="round"><polyline points="20 6 9 17 4 12"/></svg>
                <span>{p}</span>
              </li>
            ))}
          </ul>
        </div>
      </div>

      <button onClick={onSave} disabled={saving} className="btn-save-bottom">
        {saving ? "Saving..." : "Save"}
      </button>
    </section>
  );
}
