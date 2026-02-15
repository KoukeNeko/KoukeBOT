import type { ScheduledTask } from "../../types";

type Props = {
  tasks: ScheduledTask[];
  loading: boolean;
  error: string | null;
  onRefresh: () => void;
};

function formatTime(value?: string) {
  if (!value) return "-";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
}

function typeLabel(value: ScheduledTask["type"]) {
  switch (value) {
    case "reminder":
      return "提醒";
    case "activity_monitor":
      return "活動監控";
    default:
      return value;
  }
}

function statusLabel(value: ScheduledTask["status"]) {
  switch (value) {
    case "active":
      return "執行中";
    case "completed":
      return "已完成";
    case "canceled":
      return "已取消";
    case "failed":
      return "失敗";
    default:
      return value;
  }
}

export default function ScheduledTasksSection({ tasks, loading, error, onRefresh }: Props) {
  return (
    <section className="settings-section">
      <div className="scheduled-header-row">
        <h3 className="settings-section-title">排程任務</h3>
        <button className="btn-outline" type="button" onClick={onRefresh} disabled={loading}>
          {loading ? "載入中..." : "重新整理"}
        </button>
      </div>

      {error && <p className="error message">{error}</p>}

      {!loading && tasks.length === 0 && (
        <p className="hint">目前沒有排程任務。</p>
      )}

      {tasks.length > 0 && (
        <div className="scheduled-list">
          {tasks.map((task) => (
            <article key={task.id} className="scheduled-card">
              <div className="scheduled-card-row">
                <code>{task.id}</code>
                <span className={`scheduled-status scheduled-status-${task.status}`}>
                  {statusLabel(task.status)}
                </span>
              </div>
              <div className="scheduled-card-grid">
                <div><strong>類型</strong>：{typeLabel(task.type)}</div>
                <div><strong>頻道</strong>：{task.channel_id}</div>
                <div><strong>建立者</strong>：{task.creator_user_id}</div>
                <div><strong>目標</strong>：{task.target_user_id}</div>
                <div><strong>Persona</strong>：{task.persona_id}</div>
                <div><strong>建立時間</strong>：{formatTime(task.created_at)}</div>
                <div><strong>開始時間</strong>：{formatTime(task.start_at)}</div>
                <div><strong>執行時間</strong>：{formatTime(task.run_at || task.next_run_at)}</div>
                <div><strong>結束時間</strong>：{formatTime(task.end_at)}</div>
                <div><strong>最後檢查</strong>：{formatTime(task.last_checked_at)}</div>
              </div>
              {task.reminder_text && (
                <p className="scheduled-reminder"><strong>提醒內容</strong>：{task.reminder_text}</p>
              )}
            </article>
          ))}
        </div>
      )}
    </section>
  );
}
