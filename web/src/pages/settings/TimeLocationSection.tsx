import type { Dispatch, SetStateAction } from "react";
import Select from "../../components/ui/Select";
import type { SettingsData } from "../../types";

type Props = {
  form: SettingsData;
  setForm: Dispatch<SetStateAction<SettingsData>>;
  saving: boolean;
  onSave: () => void;
};

export default function TimeLocationSection({ form, setForm, saving, onSave }: Props) {
  return (
    <section className="settings-section">
      <h3 className="settings-section-title">
        <svg xmlns="http://www.w3.org/2000/svg" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className="lucide lucide-alarm-clock"><circle cx="12" cy="12" r="10"/><path d="M12 6v6l4 2"/><path d="M10.2 3.2c3.9 3.9 3.9 10.1 0 14l3.8 3.8c3.9-3.9 3.9-10.1 0-14Z"/><path d="M3.2 13.8c3.9-3.9 10.1-3.9 14 0l3.8-3.8c-3.9 3.9-10.1 3.9-14 0Z"/></svg>
        時間與地區設定
        <span className="tool-tag">OpenClaw 相容</span>
      </h3>

      <div style={{ marginBottom: "1.25rem" }}>
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

      <div style={{ marginBottom: "1.25rem" }}>
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

      <div style={{ marginBottom: "1.25rem" }}>
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

      <button onClick={onSave} disabled={saving} className="btn-save-bottom">
        {saving ? "Saving..." : "Save"}
      </button>
    </section>
  );
}
