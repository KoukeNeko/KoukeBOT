import type { Dispatch, SetStateAction } from "react";
import Select from "../../components/ui/Select";
import Checkbox from "../../components/ui/Checkbox";
import { Link } from "react-router-dom";
import type { SettingsData } from "../../types";

type Props = {
  form: SettingsData;
  setForm: Dispatch<SetStateAction<SettingsData>>;
  saving: boolean;
  onSave: () => void;
};

export default function ToolsSection({ form, setForm, saving, onSave }: Props) {
  const isClassicMode = (form.web_tool_mode || "classic") === "classic";

  return (
    <section className="settings-section">
      <h3 className="settings-section-title">
        <svg xmlns="http://www.w3.org/2000/svg" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className="lucide lucide-wrench"><path d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-2.53 2.53a1 1 0 0 0-1.4 0L6.3 14.7a1 1 0 0 0 0-1.4l.9-.9a1 1 0 0 0 0-1.4l-1.6-1.6a1 1 0 0 0-1.4 0L1.8 11.37a6 6 0 0 1 7.94-7.94l2.53-2.53a1 1 0 0 0 0-1.4Z"/><path d="m18.17 17.12 1.66 1.66"/></svg>
        Tools
      </h3>

      <label className="form-field">
        <div className="tool-field-header">
          <span>Web Tool Mode</span>
        </div>
        <Select
          value={form.web_tool_mode || "classic"}
          onChange={(val) =>
            setForm({
              ...form,
              web_tool_mode: val as SettingsData["web_tool_mode"],
            })
          }
          options={[
            { value: "classic", label: "classic (web_search/web_fetch)" },
            { value: "playwright_mcp", label: "playwright_mcp (managed MCP)" },
          ]}
        />
        <span className="hint">
          `playwright_mcp` 模式會停用 classic web tools，改用系統管理的 Playwright MCP。
        </span>
      </label>

      {isClassicMode ? (
        <>
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
                  value={form.perplexity_api_key || ""}
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
                  value={form.search_provider || "auto"}
                  onChange={(val) => setForm({ ...form, search_provider: val })}
                  options={[
                    { value: "auto", label: "Auto (預設 Brave)" },
                    { value: "brave", label: "Brave Search" },
                    { value: "perplexity", label: "Perplexity Sonar" },
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
        </>
      ) : (
        <p className="hint-block">
          目前為 `playwright_mcp` 模式。classic `web_search/web_fetch` 不會註冊，請到{" "}
          <Link to="/mcp" className="link">/mcp</Link> 管理與測試 Playwright MCP。
        </p>
      )}

      <button onClick={onSave} disabled={saving} className="btn-save-bottom">
        {saving ? "Saving..." : "Save"}
      </button>
    </section>
  );
}
