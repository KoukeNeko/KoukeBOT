package scheduler

import (
	"context"
	"fmt"
	"strings"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
)

func (s *Service) CreateReminder(ctx context.Context, req CreateReminderRequest) (*domain.ScheduledTask, error) {
	if s.repo == nil {
		return nil, fmt.Errorf("scheduled task repository is not configured")
	}
	if strings.TrimSpace(req.PersonaID) == "" {
		return nil, fmt.Errorf("persona_id is required")
	}
	if strings.TrimSpace(req.ChannelID) == "" {
		return nil, fmt.Errorf("channel_id is required")
	}
	if strings.TrimSpace(req.CreatorUserID) == "" {
		return nil, fmt.Errorf("creator_user_id is required")
	}
	if strings.TrimSpace(req.TargetUserID) == "" {
		req.TargetUserID = req.CreatorUserID
	}
	if req.RunAt.IsZero() {
		return nil, fmt.Errorf("run_at is required")
	}
	now := s.now()
	runAt := req.RunAt.UTC()
	if runAt.Before(now) {
		return nil, fmt.Errorf("run_at must be in the future")
	}
	if runAt.Sub(now) > maxReminderDuration {
		return nil, fmt.Errorf("reminder duration exceeds 7 days")
	}

	task := &domain.ScheduledTask{
		ID:            domain.NewScheduledTaskID(),
		Type:          domain.ScheduledTaskTypeReminder,
		Status:        domain.ScheduledTaskStatusActive,
		PersonaID:     strings.TrimSpace(req.PersonaID),
		ChannelID:     strings.TrimSpace(req.ChannelID),
		CreatorUserID: strings.TrimSpace(req.CreatorUserID),
		TargetUserID:  strings.TrimSpace(req.TargetUserID),
		ReminderText:  strings.TrimSpace(req.ReminderText),
		StartAt:       now,
		RunAt:         &runAt,
		NextRunAt:     &runAt,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	if err := s.repo.Save(ctx, task); err != nil {
		return nil, err
	}
	return task, nil
}

func (s *Service) CreateMonitor(ctx context.Context, req CreateMonitorRequest) (*domain.ScheduledTask, error) {
	if s.repo == nil {
		return nil, fmt.Errorf("scheduled task repository is not configured")
	}
	if strings.TrimSpace(req.PersonaID) == "" {
		return nil, fmt.Errorf("persona_id is required")
	}
	if strings.TrimSpace(req.ChannelID) == "" {
		return nil, fmt.Errorf("channel_id is required")
	}
	if strings.TrimSpace(req.CreatorUserID) == "" {
		return nil, fmt.Errorf("creator_user_id is required")
	}
	if strings.TrimSpace(req.TargetUserID) == "" {
		req.TargetUserID = req.CreatorUserID
	}
	if req.Duration <= 0 {
		return nil, fmt.Errorf("duration must be greater than zero")
	}
	if req.Duration > maxMonitorDuration {
		return nil, fmt.Errorf("monitor duration exceeds 24 hours")
	}

	list, err := s.repo.List(ctx)
	if err != nil {
		return nil, err
	}
	limit := s.PolicySnapshot().MonitorMaxPerUser
	activeCount := 0
	for _, task := range list {
		if task == nil {
			continue
		}
		if task.Type != domain.ScheduledTaskTypeActivityMonitor || task.Status != domain.ScheduledTaskStatusActive {
			continue
		}
		if task.ChannelID == strings.TrimSpace(req.ChannelID) && task.CreatorUserID == strings.TrimSpace(req.CreatorUserID) {
			activeCount++
		}
	}
	if activeCount >= limit {
		return nil, fmt.Errorf("active monitor limit reached (%d per user per channel)", limit)
	}

	now := s.now()
	endAt := now.Add(req.Duration).UTC()
	nextRunAt := now.Add(s.pollInterval).UTC()
	lastCheckedAt := now
	task := &domain.ScheduledTask{
		ID:            domain.NewScheduledTaskID(),
		Type:          domain.ScheduledTaskTypeActivityMonitor,
		Status:        domain.ScheduledTaskStatusActive,
		PersonaID:     strings.TrimSpace(req.PersonaID),
		ChannelID:     strings.TrimSpace(req.ChannelID),
		CreatorUserID: strings.TrimSpace(req.CreatorUserID),
		TargetUserID:  strings.TrimSpace(req.TargetUserID),
		StartAt:       now,
		EndAt:         &endAt,
		NextRunAt:     &nextRunAt,
		LastCheckedAt: &lastCheckedAt,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := s.repo.Save(ctx, task); err != nil {
		return nil, err
	}
	return task, nil
}

func (s *Service) CancelTask(ctx context.Context, req CancelTaskRequest) (*domain.ScheduledTask, error) {
	if s.repo == nil {
		return nil, fmt.Errorf("scheduled task repository is not configured")
	}
	task, err := s.repo.GetByID(ctx, strings.TrimSpace(req.TaskID))
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.ChannelID) != "" && task.ChannelID != strings.TrimSpace(req.ChannelID) {
		return nil, fmt.Errorf("task %s does not belong to channel %s", task.ID, req.ChannelID)
	}
	if strings.TrimSpace(req.CreatorUserID) != "" && task.CreatorUserID != strings.TrimSpace(req.CreatorUserID) {
		return nil, fmt.Errorf("task %s does not belong to user %s", task.ID, req.CreatorUserID)
	}
	if task.Status != domain.ScheduledTaskStatusActive && !req.IncludeNonActive {
		return task, nil
	}

	task.Status = domain.ScheduledTaskStatusCanceled
	task.NextRunAt = nil
	task.PendingAlert = nil
	task.RetryState = domain.ScheduledTaskRetryState{}
	task.UpdatedAt = s.now()
	if err := s.repo.Save(ctx, task); err != nil {
		return nil, err
	}
	return task, nil
}

func (s *Service) CancelAll(ctx context.Context, req CancelAllRequest) (int, error) {
	if s.repo == nil {
		return 0, fmt.Errorf("scheduled task repository is not configured")
	}
	list, err := s.repo.List(ctx)
	if err != nil {
		return 0, err
	}

	updated := 0
	for _, task := range list {
		if task == nil {
			continue
		}
		if strings.TrimSpace(req.ChannelID) != "" && task.ChannelID != strings.TrimSpace(req.ChannelID) {
			continue
		}
		if strings.TrimSpace(req.CreatorUserID) != "" && task.CreatorUserID != strings.TrimSpace(req.CreatorUserID) {
			continue
		}
		if !req.IncludeInactive && task.Status != domain.ScheduledTaskStatusActive {
			continue
		}
		if task.Status == domain.ScheduledTaskStatusActive {
			task.Status = domain.ScheduledTaskStatusCanceled
			task.NextRunAt = nil
			task.PendingAlert = nil
			task.RetryState = domain.ScheduledTaskRetryState{}
			task.UpdatedAt = s.now()
			if err := s.repo.Save(ctx, task); err != nil {
				return updated, err
			}
			updated++
		}
	}
	return updated, nil
}
