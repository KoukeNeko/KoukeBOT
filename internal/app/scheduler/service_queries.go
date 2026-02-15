package scheduler

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
)

func (s *Service) ListTasks(ctx context.Context, req ListTasksRequest) ([]*domain.ScheduledTask, error) {
	if s.repo == nil {
		return nil, fmt.Errorf("scheduled task repository is not configured")
	}
	list, err := s.repo.List(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]*domain.ScheduledTask, 0, len(list))
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
		out = append(out, task)
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}
