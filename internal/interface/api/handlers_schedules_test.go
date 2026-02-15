package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	schedulerpkg "github.com/KoukeNeko/KoukeBOT/internal/app/scheduler"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/store"
)

func TestListScheduledTasksReturnsItems(t *testing.T) {
	taskStore, err := store.NewScheduledTaskFileStore(filepath.Join(t.TempDir(), "tasks.json"))
	if err != nil {
		t.Fatalf("NewScheduledTaskFileStore() error: %v", err)
	}
	service := schedulerpkg.NewService(taskStore, schedulerpkg.ServiceOptions{})

	runAt := time.Now().UTC().Add(5 * time.Minute)
	if _, err := service.CreateReminder(t.Context(), schedulerpkg.CreateReminderRequest{
		PersonaID:     "persona-a",
		ChannelID:     "ch-1",
		CreatorUserID: "u-1",
		TargetUserID:  "u-1",
		ReminderText:  "喝水",
		RunAt:         runAt,
	}); err != nil {
		t.Fatalf("CreateReminder() error: %v", err)
	}

	h := &Handlers{schedulerService: service}
	req := httptest.NewRequest(http.MethodGet, "/api/schedules?channel_id=ch-1&creator_user_id=u-1", nil)
	rec := httptest.NewRecorder()

	h.ListScheduledTasks(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", rec.Code, http.StatusOK)
	}

	var items []ScheduledTaskResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items len = %d, want 1", len(items))
	}
	if items[0].Type != "reminder" {
		t.Fatalf("task type = %q, want reminder", items[0].Type)
	}
}
