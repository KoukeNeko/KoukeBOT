package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
)

func TestScheduledTaskFileStore_SaveListReload(t *testing.T) {
	taskPath := filepath.Join(t.TempDir(), "schedules", "tasks.json")
	store, err := NewScheduledTaskFileStore(taskPath)
	if err != nil {
		t.Fatalf("NewScheduledTaskFileStore() error: %v", err)
	}

	runAt := time.Now().UTC().Add(10 * time.Minute)
	task := &domain.ScheduledTask{
		ID:            "task-1",
		Type:          domain.ScheduledTaskTypeReminder,
		Status:        domain.ScheduledTaskStatusActive,
		PersonaID:     "persona-a",
		ChannelID:     "ch-1",
		CreatorUserID: "u-1",
		TargetUserID:  "u-1",
		ReminderText:  "喝水",
		StartAt:       time.Now().UTC(),
		RunAt:         &runAt,
		NextRunAt:     &runAt,
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}
	if err := store.Save(context.Background(), task); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	list, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("List() len=%d, want 1", len(list))
	}
	if list[0].ID != "task-1" {
		t.Fatalf("task ID = %q, want task-1", list[0].ID)
	}

	reloaded, err := NewScheduledTaskFileStore(taskPath)
	if err != nil {
		t.Fatalf("reload store error: %v", err)
	}
	loadedTask, err := reloaded.GetByID(context.Background(), "task-1")
	if err != nil {
		t.Fatalf("GetByID() error: %v", err)
	}
	if loadedTask.ReminderText != "喝水" {
		t.Fatalf("ReminderText = %q, want 喝水", loadedTask.ReminderText)
	}
}

func TestScheduledTaskFileStore_Delete(t *testing.T) {
	taskPath := filepath.Join(t.TempDir(), "schedules", "tasks.json")
	store, err := NewScheduledTaskFileStore(taskPath)
	if err != nil {
		t.Fatalf("NewScheduledTaskFileStore() error: %v", err)
	}

	task := &domain.ScheduledTask{
		ID:            "task-del",
		Type:          domain.ScheduledTaskTypeActivityMonitor,
		Status:        domain.ScheduledTaskStatusActive,
		PersonaID:     "persona-a",
		ChannelID:     "ch-1",
		CreatorUserID: "u-1",
		TargetUserID:  "u-2",
		StartAt:       time.Now().UTC(),
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}
	if err := store.Save(context.Background(), task); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	if err := store.Delete(context.Background(), "task-del"); err != nil {
		t.Fatalf("Delete() error: %v", err)
	}
	if _, err := store.GetByID(context.Background(), "task-del"); err == nil {
		t.Fatalf("expected not found after delete")
	}
}
