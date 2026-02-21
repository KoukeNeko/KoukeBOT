package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/app/scheduler"
	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
)

const (
	scheduleActionCreateReminder = "create_reminder"
	scheduleActionCreateMonitor  = "create_monitor"
	scheduleActionListTasks      = "list_tasks"
	scheduleActionCancelTask     = "cancel_task"
	scheduleActionCancelAll      = "cancel_all"
)

// SchedulerService is the minimal scheduler service contract used by schedule_manager.
type SchedulerService interface {
	CreateReminder(ctx context.Context, req scheduler.CreateReminderRequest) (*domain.ScheduledTask, error)
	CreateMonitor(ctx context.Context, req scheduler.CreateMonitorRequest) (*domain.ScheduledTask, error)
	ListTasks(ctx context.Context, req scheduler.ListTasksRequest) ([]*domain.ScheduledTask, error)
	CancelTask(ctx context.Context, req scheduler.CancelTaskRequest) (*domain.ScheduledTask, error)
	CancelAll(ctx context.Context, req scheduler.CancelAllRequest) (int, error)
}

type SchedulerTool struct {
	service SchedulerService
}

func NewSchedulerTool(service SchedulerService) *SchedulerTool {
	return &SchedulerTool{service: service}
}

func (t *SchedulerTool) Name() string {
	return "schedule_manager"
}

func (t *SchedulerTool) Description() string {
	return "Manage Discord-only reminders and activity-monitor tasks. Actions: create_reminder, create_monitor, list_tasks, cancel_task, cancel_all."
}

func (t *SchedulerTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type": "string",
				"enum": []string{
					scheduleActionCreateReminder,
					scheduleActionCreateMonitor,
					scheduleActionListTasks,
					scheduleActionCancelTask,
					scheduleActionCancelAll,
				},
			},
			"delay_seconds": map[string]interface{}{
				"type":        "integer",
				"description": "Reminder delay in seconds (alternative to run_at).",
			},
			"run_at": map[string]interface{}{
				"type":        "string",
				"description": "Reminder trigger time in RFC3339 (alternative to delay_seconds).",
			},
			"reminder_text": map[string]interface{}{
				"type":        "string",
				"description": "Reminder content text.",
			},
			"duration_seconds": map[string]interface{}{
				"type":        "integer",
				"description": "Monitor duration in seconds.",
			},
			"target_user_ids": map[string]interface{}{
				"type":        "array",
				"description": "Target Discord user IDs to monitor/remind.",
				"items": map[string]interface{}{
					"type": "string",
				},
			},
			"task_id": map[string]interface{}{
				"type":        "string",
				"description": "Task ID used by cancel_task.",
			},
			"include_inactive": map[string]interface{}{
				"type":        "boolean",
				"description": "When true, list/cancel includes inactive tasks.",
			},
			"persona_id": map[string]interface{}{
				"type":        "string",
				"description": "Optional persona override. Defaults to request context persona.",
			},
		},
		"required": []string{"action"},
	}
}

func (t *SchedulerTool) Execute(ctx context.Context, input map[string]interface{}) (domain.ToolResult, error) {
	if t.service == nil {
		return scheduleToolError("scheduler service is not configured"), nil
	}

	reqCtx, ok := llm.GetRequestContext(ctx)
	if !ok || reqCtx.Source != llm.RequestSourceDiscord {
		return scheduleToolError("schedule_manager is only available for Discord requests"), nil
	}
	if strings.TrimSpace(reqCtx.ChannelID) == "" || strings.TrimSpace(reqCtx.UserID) == "" {
		return scheduleToolError("missing Discord request context (channel_id/user_id)"), nil
	}

	action := strings.TrimSpace(readString(input, "action"))
	if action == "" {
		return scheduleToolError("missing required field: action"), nil
	}

	personaID := strings.TrimSpace(readString(input, "persona_id"))
	if personaID == "" {
		personaID = strings.TrimSpace(reqCtx.PersonaID)
	}
	if personaID == "" {
		return scheduleToolError("missing persona context"), nil
	}

	now := time.Now().UTC()
	switch action {
	case scheduleActionCreateReminder:
		return t.handleCreateReminder(ctx, input, reqCtx, personaID, now), nil
	case scheduleActionCreateMonitor:
		return t.handleCreateMonitor(ctx, input, reqCtx, personaID), nil
	case scheduleActionListTasks:
		return t.handleListTasks(ctx, input, reqCtx), nil
	case scheduleActionCancelTask:
		return t.handleCancelTask(ctx, input, reqCtx), nil
	case scheduleActionCancelAll:
		return t.handleCancelAll(ctx, input, reqCtx), nil
	default:
		return scheduleToolError("unsupported action"), nil
	}
}

func (t *SchedulerTool) handleCreateReminder(ctx context.Context, input map[string]interface{}, reqCtx llm.RequestContext, personaID string, now time.Time) domain.ToolResult {
	targets := parseStringSlice(input["target_user_ids"])
	if len(targets) == 0 {
		targets = []string{reqCtx.UserID}
	}
	target := targets[0]

	runAt, err := parseReminderRunAt(input, now)
	if err != nil {
		return scheduleToolError(err.Error())
	}

	reminderText := strings.TrimSpace(readString(input, "reminder_text"))
	if reminderText == "" {
		reminderText = "提醒時間到了。"
	}

	task, err := t.service.CreateReminder(ctx, scheduler.CreateReminderRequest{
		PersonaID:     personaID,
		ChannelID:     reqCtx.ChannelID,
		CreatorUserID: reqCtx.UserID,
		TargetUserID:  target,
		ReminderText:  reminderText,
		RunAt:         runAt,
	})
	if err != nil {
		return scheduleToolError(err.Error())
	}

	payload := map[string]interface{}{
		"action": actionName(scheduleActionCreateReminder),
		"task":   summarizeScheduledTask(task),
	}
	return scheduleToolOK(payload)
}

func (t *SchedulerTool) handleCreateMonitor(ctx context.Context, input map[string]interface{}, reqCtx llm.RequestContext, personaID string) domain.ToolResult {
	durationSeconds, err := readInt64(input, "duration_seconds")
	if err != nil {
		return scheduleToolError("duration_seconds is required for create_monitor")
	}
	if durationSeconds <= 0 {
		return scheduleToolError("duration_seconds must be greater than zero")
	}
	duration := time.Duration(durationSeconds) * time.Second

	targets := parseStringSlice(input["target_user_ids"])
	if len(targets) == 0 {
		targets = []string{reqCtx.UserID}
	}
	targets = uniqueNonEmptyStrings(targets)
	if len(targets) == 0 {
		targets = []string{reqCtx.UserID}
	}

	created := make([]map[string]interface{}, 0, len(targets))
	errorsList := make([]string, 0)
	for _, target := range targets {
		task, createErr := t.service.CreateMonitor(ctx, scheduler.CreateMonitorRequest{
			PersonaID:     personaID,
			ChannelID:     reqCtx.ChannelID,
			CreatorUserID: reqCtx.UserID,
			TargetUserID:  target,
			Duration:      duration,
		})
		if createErr != nil {
			errorsList = append(errorsList, fmt.Sprintf("target=%s: %s", target, createErr.Error()))
			continue
		}
		created = append(created, summarizeScheduledTask(task))
	}

	if len(created) == 0 {
		return scheduleToolError(strings.Join(errorsList, "; "))
	}

	payload := map[string]interface{}{
		"action":         actionName(scheduleActionCreateMonitor),
		"created_count":  len(created),
		"tasks":          created,
		"partial_errors": errorsList,
	}
	return scheduleToolOK(payload)
}

func (t *SchedulerTool) handleListTasks(ctx context.Context, input map[string]interface{}, reqCtx llm.RequestContext) domain.ToolResult {
	includeInactive := readBool(input, "include_inactive")
	tasks, err := t.service.ListTasks(ctx, scheduler.ListTasksRequest{
		ChannelID:       reqCtx.ChannelID,
		CreatorUserID:   reqCtx.UserID,
		IncludeInactive: includeInactive,
	})
	if err != nil {
		return scheduleToolError(err.Error())
	}

	items := make([]map[string]interface{}, 0, len(tasks))
	for _, task := range tasks {
		items = append(items, summarizeScheduledTask(task))
	}
	sort.Slice(items, func(i, j int) bool {
		return readMapString(items[i], "id") < readMapString(items[j], "id")
	})

	payload := map[string]interface{}{
		"action": actionName(scheduleActionListTasks),
		"count":  len(items),
		"tasks":  items,
	}
	return scheduleToolOK(payload)
}

func (t *SchedulerTool) handleCancelTask(ctx context.Context, input map[string]interface{}, reqCtx llm.RequestContext) domain.ToolResult {
	taskID := strings.TrimSpace(readString(input, "task_id"))
	if taskID == "" {
		return scheduleToolError("task_id is required for cancel_task")
	}

	task, err := t.service.CancelTask(ctx, scheduler.CancelTaskRequest{
		TaskID:           taskID,
		ChannelID:        reqCtx.ChannelID,
		CreatorUserID:    reqCtx.UserID,
		IncludeNonActive: true,
	})
	if err != nil {
		return scheduleToolError(err.Error())
	}

	payload := map[string]interface{}{
		"action": actionName(scheduleActionCancelTask),
		"task":   summarizeScheduledTask(task),
	}
	return scheduleToolOK(payload)
}

func (t *SchedulerTool) handleCancelAll(ctx context.Context, input map[string]interface{}, reqCtx llm.RequestContext) domain.ToolResult {
	includeInactive := readBool(input, "include_inactive")
	count, err := t.service.CancelAll(ctx, scheduler.CancelAllRequest{
		ChannelID:       reqCtx.ChannelID,
		CreatorUserID:   reqCtx.UserID,
		IncludeInactive: includeInactive,
	})
	if err != nil {
		return scheduleToolError(err.Error())
	}

	payload := map[string]interface{}{
		"action":         actionName(scheduleActionCancelAll),
		"canceled_count": count,
	}
	return scheduleToolOK(payload)
}

func parseReminderRunAt(input map[string]interface{}, now time.Time) (time.Time, error) {
	if delay, err := readInt64(input, "delay_seconds"); err == nil && delay > 0 {
		return now.Add(time.Duration(delay) * time.Second).UTC(), nil
	}

	runAtRaw := strings.TrimSpace(readString(input, "run_at"))
	if runAtRaw == "" {
		return time.Time{}, fmt.Errorf("either delay_seconds or run_at is required")
	}
	runAt, err := time.Parse(time.RFC3339, runAtRaw)
	if err != nil {
		return time.Time{}, fmt.Errorf("run_at must be RFC3339")
	}
	return runAt.UTC(), nil
}

func summarizeScheduledTask(task *domain.ScheduledTask) map[string]interface{} {
	if task == nil {
		return map[string]interface{}{}
	}

	out := map[string]interface{}{
		"id":              task.ID,
		"type":            string(task.Type),
		"status":          string(task.Status),
		"persona_id":      task.PersonaID,
		"channel_id":      task.ChannelID,
		"creator_user_id": task.CreatorUserID,
		"target_user_id":  task.TargetUserID,
		"start_at":        task.StartAt.UTC().Format(time.RFC3339),
		"created_at":      task.CreatedAt.UTC().Format(time.RFC3339),
		"updated_at":      task.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if task.EndAt != nil {
		out["end_at"] = task.EndAt.UTC().Format(time.RFC3339)
	}
	if task.RunAt != nil {
		out["run_at"] = task.RunAt.UTC().Format(time.RFC3339)
	}
	if task.NextRunAt != nil {
		out["next_run_at"] = task.NextRunAt.UTC().Format(time.RFC3339)
	}
	if task.LastCheckedAt != nil {
		out["last_checked_at"] = task.LastCheckedAt.UTC().Format(time.RFC3339)
	}
	if strings.TrimSpace(task.ReminderText) != "" {
		out["reminder_text"] = task.ReminderText
	}
	if task.PendingAlert != nil {
		out["pending_alert"] = map[string]interface{}{
			"triggered_at":    task.PendingAlert.TriggeredAt.UTC().Format(time.RFC3339),
			"violation_count": task.PendingAlert.ViolationCount,
		}
	}
	if task.RetryState.NextRetryAt != nil || task.RetryState.Attempts > 0 || task.RetryState.LastError != "" {
		retry := map[string]interface{}{
			"attempts": task.RetryState.Attempts,
		}
		if task.RetryState.WindowStartedAt != nil {
			retry["window_started_at"] = task.RetryState.WindowStartedAt.UTC().Format(time.RFC3339)
		}
		if task.RetryState.NextRetryAt != nil {
			retry["next_retry_at"] = task.RetryState.NextRetryAt.UTC().Format(time.RFC3339)
		}
		if strings.TrimSpace(task.RetryState.LastError) != "" {
			retry["last_error"] = task.RetryState.LastError
		}
		out["retry_state"] = retry
	}
	return out
}

func scheduleToolOK(payload map[string]interface{}) domain.ToolResult {
	if payload == nil {
		payload = map[string]interface{}{}
	}
	payload["ok"] = true
	return scheduleToolResult(payload, false)
}

func scheduleToolError(message string) domain.ToolResult {
	payload := map[string]interface{}{
		"ok":    false,
		"error": strings.TrimSpace(message),
	}
	return scheduleToolResult(payload, true)
}

func scheduleToolResult(payload map[string]interface{}, isError bool) domain.ToolResult {
	b, err := json.Marshal(payload)
	if err != nil {
		return domain.ToolResult{
			Content: `{"ok":false,"error":"encode schedule_manager result failed"}`,
			IsError: true,
		}
	}
	return domain.ToolResult{
		Content: string(b),
		IsError: isError,
	}
}

func actionName(name string) string {
	return strings.TrimSpace(name)
}
