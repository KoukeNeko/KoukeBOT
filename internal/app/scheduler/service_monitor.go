package scheduler

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
)

// saveTaskOrWarn persists task state, logging a warning on failure instead of
// silently discarding the error. Scheduler tick must not abort on save failure.
func (s *Service) saveTaskOrWarn(ctx context.Context, task *domain.ScheduledTask) {
	if err := s.repo.Save(ctx, task); err != nil {
		logScheduler.Warnf("save task %s failed: %v", task.ID, err)
	}
}

func (s *Service) handleReminderTask(ctx context.Context, task *domain.ScheduledTask, now time.Time) {
	if task.RunAt == nil {
		task.Status = domain.ScheduledTaskStatusFailed
		task.RetryState.LastError = "run_at is missing"
		task.NextRunAt = nil
		s.saveTaskOrWarn(ctx, task)
		return
	}
	if task.NextRunAt == nil {
		runAt := task.RunAt.UTC()
		task.NextRunAt = &runAt
	}
	if now.Before(task.NextRunAt.UTC()) {
		return
	}

	prompt := buildReminderPrompt(task, now)
	notifyErr := s.sendPersonaNotification(ctx, task, prompt)
	if notifyErr == nil {
		task.Status = domain.ScheduledTaskStatusCompleted
		task.NextRunAt = nil
		task.RetryState = domain.ScheduledTaskRetryState{}
		task.UpdatedAt = now
		s.saveTaskOrWarn(ctx, task)
		return
	}

	if s.scheduleRetry(task, now, notifyErr) {
		task.UpdatedAt = now
		s.saveTaskOrWarn(ctx, task)
		return
	}

	fallback := fmt.Sprintf("提醒時間到了：%s", safeReminderText(task.ReminderText))
	fallbackErr := s.sendFallbackNotification(ctx, task, fallback)
	if fallbackErr != nil {
		task.Status = domain.ScheduledTaskStatusFailed
		task.RetryState.LastError = truncateSchedulerError(fallbackErr.Error(), schedulerErrorMaxLen)
		task.NextRunAt = nil
		task.UpdatedAt = now
		s.saveTaskOrWarn(ctx, task)
		return
	}
	task.Status = domain.ScheduledTaskStatusCompleted
	task.RetryState = domain.ScheduledTaskRetryState{}
	task.NextRunAt = nil
	task.UpdatedAt = now
	s.saveTaskOrWarn(ctx, task)
}

func (s *Service) handleMonitorTask(ctx context.Context, task *domain.ScheduledTask, now time.Time) {
	if task.EndAt == nil {
		task.Status = domain.ScheduledTaskStatusFailed
		task.RetryState.LastError = "end_at is missing"
		task.NextRunAt = nil
		s.saveTaskOrWarn(ctx, task)
		return
	}
	if !now.Before(task.EndAt.UTC()) {
		task.Status = domain.ScheduledTaskStatusCompleted
		task.NextRunAt = nil
		task.PendingAlert = nil
		task.RetryState = domain.ScheduledTaskRetryState{}
		task.UpdatedAt = now
		s.saveTaskOrWarn(ctx, task)
		return
	}

	if task.PendingAlert != nil && task.RetryState.NextRetryAt != nil && !now.Before(task.RetryState.NextRetryAt.UTC()) {
		s.retryMonitorAlert(ctx, task, now)
	}

	if task.NextRunAt == nil {
		next := now.Add(s.pollInterval).UTC()
		task.NextRunAt = &next
	}
	if now.Before(task.NextRunAt.UTC()) {
		return
	}

	violations, err := s.countViolations(ctx, task, now)
	next := now.Add(s.pollInterval).UTC()
	task.NextRunAt = &next
	lastChecked := now.UTC()
	task.LastCheckedAt = &lastChecked
	if err != nil {
		task.RetryState.LastError = truncateSchedulerError(err.Error(), schedulerErrorMaxLen)
		task.UpdatedAt = now
		s.saveTaskOrWarn(ctx, task)
		return
	}

	if violations > 0 {
		if task.PendingAlert == nil {
			task.PendingAlert = &domain.ScheduledTaskAlert{
				TriggeredAt:    now.UTC(),
				ViolationCount: violations,
			}
		} else {
			task.PendingAlert.ViolationCount += violations
		}

		if task.RetryState.NextRetryAt == nil {
			prompt := buildMonitorViolationPrompt(task, now)
			notifyErr := s.sendPersonaNotification(ctx, task, prompt)
			if notifyErr == nil {
				task.PendingAlert = nil
				task.RetryState = domain.ScheduledTaskRetryState{}
			} else if !s.scheduleRetry(task, now, notifyErr) {
				fallback := fmt.Sprintf("偵測到監控期間有 %d 則活動訊息。", task.PendingAlert.ViolationCount)
				if fallbackErr := s.sendFallbackNotification(ctx, task, fallback); fallbackErr == nil {
					task.PendingAlert = nil
					task.RetryState = domain.ScheduledTaskRetryState{}
				} else {
					task.PendingAlert = nil
					task.RetryState = domain.ScheduledTaskRetryState{LastError: truncateSchedulerError(fallbackErr.Error(), schedulerErrorMaxLen)}
				}
			}
		}
	}

	task.UpdatedAt = now
	s.saveTaskOrWarn(ctx, task)
}

func (s *Service) retryMonitorAlert(ctx context.Context, task *domain.ScheduledTask, now time.Time) {
	if task.PendingAlert == nil {
		task.RetryState = domain.ScheduledTaskRetryState{}
		return
	}
	prompt := buildMonitorViolationPrompt(task, now)
	notifyErr := s.sendPersonaNotification(ctx, task, prompt)
	if notifyErr == nil {
		task.PendingAlert = nil
		task.RetryState = domain.ScheduledTaskRetryState{}
		task.UpdatedAt = now
		s.saveTaskOrWarn(ctx, task)
		return
	}
	if s.scheduleRetry(task, now, notifyErr) {
		task.UpdatedAt = now
		s.saveTaskOrWarn(ctx, task)
		return
	}
	fallback := fmt.Sprintf("偵測到監控期間有 %d 則活動訊息。", task.PendingAlert.ViolationCount)
	if fallbackErr := s.sendFallbackNotification(ctx, task, fallback); fallbackErr == nil {
		task.PendingAlert = nil
		task.RetryState = domain.ScheduledTaskRetryState{}
	} else {
		task.PendingAlert = nil
		task.RetryState = domain.ScheduledTaskRetryState{LastError: truncateSchedulerError(fallbackErr.Error(), schedulerErrorMaxLen)}
	}
	task.UpdatedAt = now
	s.saveTaskOrWarn(ctx, task)
}

func (s *Service) countViolations(ctx context.Context, task *domain.ScheduledTask, now time.Time) (int, error) {
	activity := s.activitySnapshot()
	if activity == nil {
		return 0, fmt.Errorf("activity source is not configured")
	}
	cutoff := task.StartAt.UTC()
	if task.LastCheckedAt != nil && task.LastCheckedAt.After(cutoff) {
		cutoff = task.LastCheckedAt.UTC()
	}

	before := ""
	total := 0
	for page := 0; page < messagePaginationPageLimit; page++ {
		msgs, err := activity.ListChannelMessages(ctx, task.ChannelID, before, messagePaginationPageSize)
		if err != nil {
			return total, err
		}
		if len(msgs) == 0 {
			break
		}

		stop := false
		for _, msg := range msgs {
			ts := msg.Timestamp.UTC()
			if !ts.After(cutoff) {
				stop = true
				continue
			}
			if ts.After(now) {
				continue
			}
			if msg.AuthorID == task.TargetUserID {
				total++
			}
		}
		if stop || len(msgs) < messagePaginationPageSize {
			break
		}
		before = strings.TrimSpace(msgs[len(msgs)-1].ID)
		if before == "" {
			break
		}
	}
	return total, nil
}

func (s *Service) scheduleRetry(task *domain.ScheduledTask, now time.Time, err error) bool {
	normalizedNow := now.UTC()
	if task.RetryState.WindowStartedAt == nil || normalizedNow.Sub(task.RetryState.WindowStartedAt.UTC()) > retryWindow {
		window := normalizedNow
		task.RetryState.WindowStartedAt = &window
		task.RetryState.Attempts = 0
	}
	if task.RetryState.Attempts >= len(retryDelays) {
		task.RetryState.NextRetryAt = nil
		task.RetryState.LastError = truncateSchedulerError(err.Error(), schedulerErrorMaxLen)
		return false
	}

	delay := retryDelays[task.RetryState.Attempts]
	task.RetryState.Attempts++
	task.RetryState.LastError = truncateSchedulerError(err.Error(), schedulerErrorMaxLen)
	next := normalizedNow.Add(delay).UTC()
	task.RetryState.NextRetryAt = &next
	if task.Type == domain.ScheduledTaskTypeReminder {
		task.NextRunAt = &next
	}
	return true
}
