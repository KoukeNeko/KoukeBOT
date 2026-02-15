import type { RuntimeMetrics } from "../../types";

type Props = {
  runtimeMetrics: RuntimeMetrics | null;
};

export default function RuntimeMetricsCard({ runtimeMetrics }: Props) {
  if (!runtimeMetrics) return null;

  return (
    <section className="settings-section runtime-metrics-section">
      <h3 className="settings-section-title">Runtime Metrics (Guard / Exhausted)</h3>
      <p className="meta runtime-metrics-updated">
        Last update: {new Date(runtimeMetrics.timestamp).toLocaleString("zh-TW")}
      </p>

      <div className="runtime-metrics-grid">
        <div className="runtime-metric-card">
          <span className="runtime-metric-label">Guard: duplicate blocked</span>
          <span className="runtime-metric-value">{runtimeMetrics.tool_guard_duplicate_blocked.toLocaleString()}</span>
        </div>
        <div className="runtime-metric-card">
          <span className="runtime-metric-label">Guard: web_search limit blocked</span>
          <span className="runtime-metric-value">{runtimeMetrics.tool_guard_limit_blocked.toLocaleString()}</span>
        </div>
        <div className="runtime-metric-card">
          <span className="runtime-metric-label">Guard: max tool rounds</span>
          <span className="runtime-metric-value">{runtimeMetrics.tool_guard_max_rounds.toLocaleString()}</span>
        </div>
        <div className="runtime-metric-card">
          <span className="runtime-metric-label">Forced final answer (success)</span>
          <span className="runtime-metric-value">{runtimeMetrics.tool_forced_final_success.toLocaleString()}</span>
        </div>
        <div className="runtime-metric-card">
          <span className="runtime-metric-label">Forced final answer (failed)</span>
          <span className="runtime-metric-value">{runtimeMetrics.tool_forced_final_failed.toLocaleString()}</span>
        </div>
        <div className="runtime-metric-card">
          <span className="runtime-metric-label">Tool loop fallback used</span>
          <span className="runtime-metric-value">{runtimeMetrics.tool_loop_fallback.toLocaleString()}</span>
        </div>
        <div className="runtime-metric-card">
          <span className="runtime-metric-label">Pool transient wait retry</span>
          <span className="runtime-metric-value">{runtimeMetrics.pool_transient_wait_retry.toLocaleString()}</span>
        </div>
        <div className="runtime-metric-card">
          <span className="runtime-metric-label">Pool transient retry success</span>
          <span className="runtime-metric-value">{runtimeMetrics.pool_transient_wait_retry_succeeded.toLocaleString()}</span>
        </div>
        <div className="runtime-metric-card">
          <span className="runtime-metric-label">Pool transient retry failed</span>
          <span className="runtime-metric-value">{runtimeMetrics.pool_transient_wait_retry_failed.toLocaleString()}</span>
        </div>
        <div className="runtime-metric-card">
          <span className="runtime-metric-label">Pool exhausted errors (total)</span>
          <span className="runtime-metric-value">{runtimeMetrics.pool_all_accounts_exhausted.toLocaleString()}</span>
        </div>
        <div className="runtime-metric-card">
          <span className="runtime-metric-label">Pool accounts (current)</span>
          <span className="runtime-metric-value">{runtimeMetrics.pool_accounts_total.toLocaleString()}</span>
        </div>
        <div className="runtime-metric-card">
          <span className="runtime-metric-label">Pool exhausted accounts (current)</span>
          <span className="runtime-metric-value">{runtimeMetrics.pool_accounts_exhausted_current.toLocaleString()}</span>
        </div>
      </div>
    </section>
  );
}
