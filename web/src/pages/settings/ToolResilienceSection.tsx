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

export default function ToolResilienceSection({ form, setForm, modelOptions, saving, onSave }: Props) {
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

  const toLegacyModel = (modelID: string) =>
    modelID.includes("/") ? modelID.split("/").slice(1).join("/") : modelID;

  return (
    <section className="settings-section">
      <h3 className="settings-section-title">Tool Loop Guard / Resilience</h3>

      <div className="form-field-checkbox-row">
        <Checkbox
          checked={form.tool_guard_enabled}
          onChange={(checked) =>
            setForm({ ...form, tool_guard_enabled: checked })
          }
          label="啟用工具迴圈保護（重複呼叫 / 上限攔截）"
        />
      </div>

      <label className="form-field">
        <span>最大工具回合數（每次訊息）</span>
        <input
          type="number"
          min={1}
          max={6}
          value={form.max_tool_rounds}
          onChange={(e) =>
            setForm({
              ...form,
              max_tool_rounds: Number.parseInt(e.target.value || "0", 10),
            })
          }
        />
        <span className="hint">建議 5。超過後會觸發強制最終回答（若已啟用）。</span>
      </label>

      <label className="form-field">
        <span>Web Search 每回合上限</span>
        <input
          type="number"
          min={1}
          max={4}
          value={form.max_web_search_calls_per_turn}
          onChange={(e) =>
            setForm({
              ...form,
              max_web_search_calls_per_turn: Number.parseInt(e.target.value || "0", 10),
            })
          }
        />
        <span className="hint">建議 2，可避免重複搜尋導致 token 暴增。</span>
      </label>

      <div className="form-field-checkbox-row">
        <Checkbox
          checked={form.force_final_answer_after_tool_limit}
          onChange={(checked) =>
            setForm({ ...form, force_final_answer_after_tool_limit: checked })
          }
          label="工具受限後強制最終回答（禁用工具再呼叫一次）"
        />
      </div>

      <div className="form-field-checkbox-row">
        <Checkbox
          checked={form.transient_recovery_wait_enabled}
          onChange={(checked) =>
            setForm({ ...form, transient_recovery_wait_enabled: checked })
          }
          label="全池短暫冷卻時自動等待後重試一次"
        />
      </div>

      <label className="form-field">
        <span>短暫冷卻最大等待秒數</span>
        <input
          type="number"
          min={1}
          max={15}
          value={form.transient_recovery_wait_max_seconds}
          onChange={(e) =>
            setForm({
              ...form,
              transient_recovery_wait_max_seconds: Number.parseInt(e.target.value || "0", 10),
            })
          }
          disabled={!form.transient_recovery_wait_enabled}
        />
        <span className="hint">建議 5 秒內。只會等待一次，不會無限重試。</span>
      </label>

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

      <label className="form-field">
        <span>每位使用者背景監控上限（同一頻道）</span>
        <input
          type="number"
          min={1}
          max={20}
          value={form.scheduled_monitor_max_per_user}
          onChange={(e) =>
            setForm({
              ...form,
              scheduled_monitor_max_per_user: Number.parseInt(e.target.value || "0", 10),
            })
          }
        />
        <span className="hint">預設 3。可避免同頻道排程任務過多。</span>
      </label>

      <div className="form-field-checkbox-row">
        <Checkbox
          checked={form.discord_show_internal_tool_errors}
          onChange={(checked) =>
            setForm({ ...form, discord_show_internal_tool_errors: checked })
          }
          label="Discord 顯示工具內部錯誤/guard 訊息"
        />
      </div>

      <button onClick={onSave} disabled={saving} className="btn-save-bottom">
        {saving ? "Saving..." : "Save"}
      </button>
    </section>
  );
}
