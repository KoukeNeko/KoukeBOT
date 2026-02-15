package api

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	schedulerpkg "github.com/KoukeNeko/KoukeBOT/internal/app/scheduler"
	"github.com/KoukeNeko/KoukeBOT/internal/domain"
)

type ScheduledTaskResponse struct {
	ID            string  `json:"id"`
	Type          string  `json:"type"`
	Status        string  `json:"status"`
	PersonaID     string  `json:"persona_id"`
	ChannelID     string  `json:"channel_id"`
	CreatorUserID string  `json:"creator_user_id"`
	TargetUserID  string  `json:"target_user_id"`
	ReminderText  string  `json:"reminder_text,omitempty"`
	StartAt       string  `json:"start_at"`
	EndAt         *string `json:"end_at,omitempty"`
	RunAt         *string `json:"run_at,omitempty"`
	NextRunAt     *string `json:"next_run_at,omitempty"`
	LastCheckedAt *string `json:"last_checked_at,omitempty"`
	CreatedAt     string  `json:"created_at"`
	UpdatedAt     string  `json:"updated_at"`
}

func (h *Handlers) ListScheduledTasks(w http.ResponseWriter, r *http.Request) {
	if h.schedulerService == nil {
		writeError(w, http.StatusServiceUnavailable, "scheduler service is not configured")
		return
	}

	query := r.URL.Query()
	channelID := strings.TrimSpace(query.Get("channel_id"))
	creatorUserID := strings.TrimSpace(query.Get("creator_user_id"))
	includeInactive := true
	if raw := strings.TrimSpace(query.Get("include_inactive")); raw != "" {
		value, err := strconv.ParseBool(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "include_inactive must be true or false")
			return
		}
		includeInactive = value
	}

	tasks, err := h.schedulerService.ListTasks(r.Context(), schedulerpkg.ListTasksRequest{
		ChannelID:       channelID,
		CreatorUserID:   creatorUserID,
		IncludeInactive: includeInactive,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list scheduled tasks: "+err.Error())
		return
	}

	sort.Slice(tasks, func(i, j int) bool {
		if tasks[i].CreatedAt.Equal(tasks[j].CreatedAt) {
			return tasks[i].ID > tasks[j].ID
		}
		return tasks[i].CreatedAt.After(tasks[j].CreatedAt)
	})

	resp := make([]ScheduledTaskResponse, 0, len(tasks))
	for _, task := range tasks {
		resp = append(resp, toScheduledTaskResponse(task))
	}
	writeJSON(w, http.StatusOK, resp)
}

func toScheduledTaskResponse(task *domain.ScheduledTask) ScheduledTaskResponse {
	if task == nil {
		return ScheduledTaskResponse{}
	}
	return ScheduledTaskResponse{
		ID:            task.ID,
		Type:          string(task.Type),
		Status:        string(task.Status),
		PersonaID:     task.PersonaID,
		ChannelID:     task.ChannelID,
		CreatorUserID: task.CreatorUserID,
		TargetUserID:  task.TargetUserID,
		ReminderText:  task.ReminderText,
		StartAt:       task.StartAt.UTC().Format(time.RFC3339),
		EndAt:         formatRFC3339Ptr(task.EndAt),
		RunAt:         formatRFC3339Ptr(task.RunAt),
		NextRunAt:     formatRFC3339Ptr(task.NextRunAt),
		LastCheckedAt: formatRFC3339Ptr(task.LastCheckedAt),
		CreatedAt:     task.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:     task.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func formatRFC3339Ptr(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := value.UTC().Format(time.RFC3339)
	return &formatted
}
