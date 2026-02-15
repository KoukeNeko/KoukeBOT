import { useCallback, useEffect, useState } from "react";
import { listScheduledTasks } from "../api";
import type { ScheduledTask } from "../types";
import ScheduledTasksSection from "./settings/ScheduledTasksSection";
import "./Settings.css";

export default function Schedules() {
  const [tasks, setTasks] = useState<ScheduledTask[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    setLoading(true);
    try {
      const items = await listScheduledTasks({ includeInactive: true });
      setTasks(items);
      setError(null);
    } catch (e) {
      setTasks([]);
      setError(e instanceof Error ? e.message : "Failed to load scheduled tasks");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  return (
    <div className="settings-page">
      <h2>排程</h2>
      <ScheduledTasksSection tasks={tasks} loading={loading} error={error} onRefresh={() => void refresh()} />
    </div>
  );
}
