import type { Dispatch, SetStateAction } from "react";
import type { SettingsData } from "../../types";

type Props = {
  form: SettingsData;
  setForm: Dispatch<SetStateAction<SettingsData>>;
  saving: boolean;
  onSave: () => void;
};

export default function MemorySection({ form, setForm, saving, onSave }: Props) {
  return (
    <section className="settings-section">
      <h3 className="settings-section-title">
        <svg xmlns="http://www.w3.org/2000/svg" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M12 2a10 10 0 1 0 10 10A10 10 0 0 0 12 2Z" opacity="0"/><path d="M21 12a9 9 0 1 1-9-9 9 9 0 0 1 9 9Z"/><path d="M12 6v6l4 2"/></svg>
        記憶系統
      </h3>

      <label className="form-field">
        <span>近期原始對話輪數</span>
        <input
          type="number"
          min={20}
          max={100}
          value={form.memory_recent_rounds}
          onChange={(e) =>
            setForm({
              ...form,
              memory_recent_rounds: Number.parseInt(e.target.value || "0", 10),
            })
          }
        />
        <span className="hint">
          控制保留多少近期原始對話（1 輪 = 使用者 + 助手）。預設 50 輪約等於 100 則訊息。
        </span>
      </label>

      <button onClick={onSave} disabled={saving} className="btn-save-bottom">
        {saving ? "Saving..." : "Save"}
      </button>
    </section>
  );
}
