package domain

import (
	"fmt"
	"time"
)

type ScheduledTaskType string

const (
	ScheduledTaskTypeReminder        ScheduledTaskType = "reminder"
	ScheduledTaskTypeActivityMonitor ScheduledTaskType = "activity_monitor"
)

type ScheduledTaskStatus string

const (
	ScheduledTaskStatusActive    ScheduledTaskStatus = "active"
	ScheduledTaskStatusCompleted ScheduledTaskStatus = "completed"
	ScheduledTaskStatusCanceled  ScheduledTaskStatus = "canceled"
	ScheduledTaskStatusFailed    ScheduledTaskStatus = "failed"
)

type ScheduledTaskRetryState struct {
	Attempts        int        `json:"attempts"`
	WindowStartedAt *time.Time `json:"window_started_at,omitempty"`
	NextRetryAt     *time.Time `json:"next_retry_at,omitempty"`
	LastError       string     `json:"last_error,omitempty"`
}

type ScheduledTaskAlert struct {
	TriggeredAt    time.Time `json:"triggered_at"`
	ViolationCount int       `json:"violation_count"`
}

// ScheduledTask stores one reminder or background monitoring job.
type ScheduledTask struct {
	ID            string              `json:"id"`
	Type          ScheduledTaskType   `json:"type"`
	Status        ScheduledTaskStatus `json:"status"`
	PersonaID     string              `json:"persona_id"`
	ChannelID     string              `json:"channel_id"`
	CreatorUserID string              `json:"creator_user_id"`
	TargetUserID  string              `json:"target_user_id"`
	ReminderText  string              `json:"reminder_text,omitempty"`

	StartAt time.Time  `json:"start_at"`
	EndAt   *time.Time `json:"end_at,omitempty"`
	RunAt   *time.Time `json:"run_at,omitempty"`

	NextRunAt     *time.Time              `json:"next_run_at,omitempty"`
	LastCheckedAt *time.Time              `json:"last_checked_at,omitempty"`
	PendingAlert  *ScheduledTaskAlert     `json:"pending_alert,omitempty"`
	RetryState    ScheduledTaskRetryState `json:"retry_state,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type ScheduledActivityMessage struct {
	ID        string    `json:"id"`
	AuthorID  string    `json:"author_id"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

func NewScheduledTaskID() string {
	return fmt.Sprintf("task_%d", time.Now().UnixNano())
}
