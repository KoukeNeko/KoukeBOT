package store

import (
	"context"
	"errors"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
)

var ErrScheduledTaskNotFound = errors.New("scheduled task not found")

type ScheduledTaskRepository interface {
	GetByID(ctx context.Context, taskID string) (*domain.ScheduledTask, error)
	List(ctx context.Context) ([]*domain.ScheduledTask, error)
	Save(ctx context.Context, task *domain.ScheduledTask) error
	Delete(ctx context.Context, taskID string) error
}
