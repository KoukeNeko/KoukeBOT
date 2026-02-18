import type { Dispatch, SetStateAction } from "react";
import Select from "../../components/ui/Select";
import Checkbox from "../../components/ui/Checkbox";
import type { SettingsData } from "../../types";

type Props = {
  form: SettingsData;
  setForm: Dispatch<SetStateAction<SettingsData>>;
  saving: boolean;
  onSave: () => void;
};

export default function PromptGuardSection({ form, setForm, saving, onSave }: Props) {
  return (
    <section className="settings-section">
      <h3 className="settings-section-title">Persona Stability / Prompt Guard</h3>

      <label className="form-field">
        <span>Persona Lock 週期（每 N 個 user turn）</span>
        <input
          type="number"
          min={1}
          max={16}
          value={form.persona_lock_every_turns}
          onChange={(e) =>
            setForm({
              ...form,
              persona_lock_every_turns: Number.parseInt(e.target.value || "0", 10),
            })
          }
        />
        <span className="hint">建議 4-5。數值越小越穩定，但 token 成本會增加。</span>
      </label>

      <div className="form-field-checkbox-row">
        <Checkbox
          checked={form.triplet_output_enabled}
          onChange={(checked) => setForm({ ...form, triplet_output_enabled: checked })}
          label="強制三段輸出 [Status]/[Analysis]/[Response]"
        />
      </div>

      <label className="form-field">
        <span>Analysis 顯示模式</span>
        <Select
          value={form.analysis_visibility}
          onChange={(val) =>
            setForm({
              ...form,
              analysis_visibility: val as SettingsData["analysis_visibility"],
            })
          }
          options={[
            { value: "visible", label: "visible（完整顯示）" },
            { value: "hidden", label: "hidden（只顯示 response）" },
            { value: "debug", label: "debug（除錯加強）" },
          ]}
        />
      </label>

      <button onClick={onSave} disabled={saving} className="btn-save-bottom">
        {saving ? "Saving..." : "Save"}
      </button>
    </section>
  );
}
