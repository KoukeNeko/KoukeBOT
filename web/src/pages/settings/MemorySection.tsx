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
  modelOptions: SelectOption[];
  saving: boolean;
  onSave: () => void;
};

export default function MemorySection({ form, setForm, modelOptions, saving, onSave }: Props) {
  return (
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

      <button onClick={onSave} disabled={saving} className="btn-save-bottom">
        {saving ? "Saving..." : "Save"}
      </button>
    </section>
  );
}
