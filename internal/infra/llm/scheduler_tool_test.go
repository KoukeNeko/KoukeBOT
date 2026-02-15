package llm

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	schedulerpkg "github.com/KoukeNeko/KoukeBOT/internal/app/scheduler"
	"github.com/KoukeNeko/KoukeBOT/internal/domain"
)

type stubSchedulerService struct {
	reminders []schedulerpkg.CreateReminderRequest
	monitors  []schedulerpkg.CreateMonitorRequest
}

func (s *stubSchedulerService) CreateReminder(_ context.Context, req schedulerpkg.CreateReminderRequest) (*domain.ScheduledTask, error) {
	s.reminders = append(s.reminders, req)
	return &domain.ScheduledTask{
		ID:            "task-reminder-1",
		Type:          domain.ScheduledTaskTypeReminder,
		Status:        domain.ScheduledTaskStatusActive,
		PersonaID:     req.PersonaID,
		ChannelID:     req.ChannelID,
		CreatorUserID: req.CreatorUserID,
		TargetUserID:  req.TargetUserID,
		ReminderText:  req.ReminderText,
		StartAt:       time.Now().UTC(),
		RunAt:         &req.RunAt,
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}, nil
}

func (s *stubSchedulerService) CreateMonitor(_ context.Context, req schedulerpkg.CreateMonitorRequest) (*domain.ScheduledTask, error) {
	s.monitors = append(s.monitors, req)
	end := time.Now().UTC().Add(req.Duration)
	return &domain.ScheduledTask{
		ID:            "task-monitor-" + req.TargetUserID,
		Type:          domain.ScheduledTaskTypeActivityMonitor,
		Status:        domain.ScheduledTaskStatusActive,
		PersonaID:     req.PersonaID,
		ChannelID:     req.ChannelID,
		CreatorUserID: req.CreatorUserID,
		TargetUserID:  req.TargetUserID,
		StartAt:       time.Now().UTC(),
		EndAt:         &end,
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}, nil
}

func (s *stubSchedulerService) ListTasks(_ context.Context, _ schedulerpkg.ListTasksRequest) ([]*domain.ScheduledTask, error) {
	return []*domain.ScheduledTask{}, nil
}

func (s *stubSchedulerService) CancelTask(_ context.Context, _ schedulerpkg.CancelTaskRequest) (*domain.ScheduledTask, error) {
	return &domain.ScheduledTask{ID: "task-1"}, nil
}

func (s *stubSchedulerService) CancelAll(_ context.Context, _ schedulerpkg.CancelAllRequest) (int, error) {
	return 1, nil
}

func TestSchedulerTool_InputSchema(t *testing.T) {
	tool := NewSchedulerTool(&stubSchedulerService{})
	schema := tool.InputSchema()
	props, ok := schema["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("properties type mismatch")
	}
	if _, exists := props["action"]; !exists {
		t.Fatalf("action property is required")
	}
}

func TestSchedulerTool_RejectsNonDiscordSource(t *testing.T) {
	tool := NewSchedulerTool(&stubSchedulerService{})
	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"action": scheduleActionListTasks,
	})
	if err != nil {
		t.Fatalf("unexpected execution error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected error result for non-discord source")
	}
}

func TestSchedulerTool_CreateReminderUsesRequestContext(t *testing.T) {
	stub := &stubSchedulerService{}
	tool := NewSchedulerTool(stub)

	ctx := WithRequestContext(context.Background(), RequestContext{
		Source:    RequestSourceDiscord,
		ChannelID: "ch-1",
		UserID:    "u-1",
		PersonaID: "persona-a",
	})
	result, err := tool.Execute(ctx, map[string]interface{}{
		"action":        scheduleActionCreateReminder,
		"delay_seconds": float64(600),
		"reminder_text": "喝水",
	})
	if err != nil {
		t.Fatalf("unexpected execution error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success result, got: %s", result.Content)
	}
	if len(stub.reminders) != 1 {
		t.Fatalf("create reminder calls = %d, want 1", len(stub.reminders))
	}
	if stub.reminders[0].PersonaID != "persona-a" {
		t.Fatalf("persona_id = %q, want persona-a", stub.reminders[0].PersonaID)
	}
	if stub.reminders[0].CreatorUserID != "u-1" {
		t.Fatalf("creator_user_id = %q, want u-1", stub.reminders[0].CreatorUserID)
	}
}

func TestSchedulerTool_CreateMonitorSupportsMultiTargets(t *testing.T) {
	stub := &stubSchedulerService{}
	tool := NewSchedulerTool(stub)

	ctx := WithRequestContext(context.Background(), RequestContext{
		Source:    RequestSourceDiscord,
		ChannelID: "ch-1",
		UserID:    "u-1",
		PersonaID: "persona-a",
	})
	result, err := tool.Execute(ctx, map[string]interface{}{
		"action":           scheduleActionCreateMonitor,
		"duration_seconds": float64(3600),
		"target_user_ids":  []interface{}{"u-2", "u-3"},
	})
	if err != nil {
		t.Fatalf("unexpected execution error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success result, got: %s", result.Content)
	}
	if len(stub.monitors) != 2 {
		t.Fatalf("create monitor calls = %d, want 2", len(stub.monitors))
	}

	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(result.Content), &payload); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if payload["ok"] != true {
		t.Fatalf("expected ok=true in payload")
	}
}
