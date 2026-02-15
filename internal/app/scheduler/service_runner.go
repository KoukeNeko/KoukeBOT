package scheduler

import (
	"context"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
)

func (s *Service) loop(ctx context.Context) {
	logScheduler.Logf("worker started (poll=%s)", s.pollInterval)
	s.runCycle(ctx)

	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logScheduler.Log("worker stopped")
			return
		case <-ticker.C:
			s.runCycle(ctx)
		}
	}
}

func (s *Service) runCycle(ctx context.Context) {
	if s.repo == nil {
		return
	}
	now := s.now()

	tasks, err := s.repo.List(ctx)
	if err != nil {
		logScheduler.Warnf("list tasks failed: %v", err)
		return
	}
	for _, task := range tasks {
		if task == nil || task.Status != domain.ScheduledTaskStatusActive {
			continue
		}
		switch task.Type {
		case domain.ScheduledTaskTypeReminder:
			s.handleReminderTask(ctx, task, now)
		case domain.ScheduledTaskTypeActivityMonitor:
			s.handleMonitorTask(ctx, task, now)
		}
	}
}
