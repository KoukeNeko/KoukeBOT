package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
)

// ScheduledTaskFileStore persists scheduled tasks to a JSON file.
type ScheduledTaskFileStore struct {
	mu    sync.RWMutex
	path  string
	tasks map[string]*domain.ScheduledTask
}

func NewScheduledTaskFileStore(path string) (*ScheduledTaskFileStore, error) {
	store := &ScheduledTaskFileStore{
		path:  path,
		tasks: make(map[string]*domain.ScheduledTask),
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create scheduled task directory: %w", err)
	}
	if err := store.loadFromDisk(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *ScheduledTaskFileStore) loadFromDisk() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read scheduled task file: %w", err)
	}
	if strings.TrimSpace(string(data)) == "" {
		return nil
	}

	var list []*domain.ScheduledTask
	if err := json.Unmarshal(data, &list); err != nil {
		return fmt.Errorf("parse scheduled task file: %w", err)
	}
	for _, task := range list {
		if task == nil || strings.TrimSpace(task.ID) == "" {
			continue
		}
		normalized := normalizeScheduledTask(task)
		s.tasks[normalized.ID] = normalized
	}
	return nil
}

func (s *ScheduledTaskFileStore) GetByID(_ context.Context, taskID string) (*domain.ScheduledTask, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	task, ok := s.tasks[taskID]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrScheduledTaskNotFound, taskID)
	}
	return cloneScheduledTask(task), nil
}

func (s *ScheduledTaskFileStore) List(_ context.Context) ([]*domain.ScheduledTask, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	list := make([]*domain.ScheduledTask, 0, len(s.tasks))
	for _, task := range s.tasks {
		list = append(list, cloneScheduledTask(task))
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].CreatedAt.Equal(list[j].CreatedAt) {
			return list[i].ID < list[j].ID
		}
		return list[i].CreatedAt.Before(list[j].CreatedAt)
	})
	return list, nil
}

func (s *ScheduledTaskFileStore) Save(_ context.Context, task *domain.ScheduledTask) error {
	if task == nil {
		return fmt.Errorf("scheduled task is nil")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	copyTask := normalizeScheduledTask(task)
	now := time.Now().UTC()
	if strings.TrimSpace(copyTask.ID) == "" {
		copyTask.ID = domain.NewScheduledTaskID()
	}
	if copyTask.CreatedAt.IsZero() {
		copyTask.CreatedAt = now
	}
	copyTask.UpdatedAt = now
	if copyTask.Status == "" {
		copyTask.Status = domain.ScheduledTaskStatusActive
	}

	s.tasks[copyTask.ID] = copyTask
	return s.saveToDiskLocked()
}

func (s *ScheduledTaskFileStore) Delete(_ context.Context, taskID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.tasks[taskID]; !ok {
		return fmt.Errorf("%w: %s", ErrScheduledTaskNotFound, taskID)
	}
	delete(s.tasks, taskID)
	return s.saveToDiskLocked()
}

func (s *ScheduledTaskFileStore) saveToDiskLocked() error {
	list := make([]*domain.ScheduledTask, 0, len(s.tasks))
	for _, task := range s.tasks {
		list = append(list, cloneScheduledTask(task))
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].CreatedAt.Equal(list[j].CreatedAt) {
			return list[i].ID < list[j].ID
		}
		return list[i].CreatedAt.Before(list[j].CreatedAt)
	})

	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal scheduled tasks: %w", err)
	}

	tmpPath := s.path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
		return fmt.Errorf("write scheduled task temp file: %w", err)
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		return fmt.Errorf("replace scheduled task file: %w", err)
	}
	return nil
}

func normalizeScheduledTask(task *domain.ScheduledTask) *domain.ScheduledTask {
	if task == nil {
		return nil
	}
	clone := cloneScheduledTask(task)
	clone.CreatedAt = clone.CreatedAt.UTC()
	clone.UpdatedAt = clone.UpdatedAt.UTC()
	clone.StartAt = clone.StartAt.UTC()
	clone.EndAt = cloneTimePtrUTC(clone.EndAt)
	clone.RunAt = cloneTimePtrUTC(clone.RunAt)
	clone.NextRunAt = cloneTimePtrUTC(clone.NextRunAt)
	clone.LastCheckedAt = cloneTimePtrUTC(clone.LastCheckedAt)
	if clone.PendingAlert != nil {
		clone.PendingAlert.TriggeredAt = clone.PendingAlert.TriggeredAt.UTC()
	}
	clone.RetryState.WindowStartedAt = cloneTimePtrUTC(clone.RetryState.WindowStartedAt)
	clone.RetryState.NextRetryAt = cloneTimePtrUTC(clone.RetryState.NextRetryAt)
	return clone
}

func cloneScheduledTask(task *domain.ScheduledTask) *domain.ScheduledTask {
	if task == nil {
		return nil
	}
	copyTask := *task
	copyTask.EndAt = cloneTimePtr(task.EndAt)
	copyTask.RunAt = cloneTimePtr(task.RunAt)
	copyTask.NextRunAt = cloneTimePtr(task.NextRunAt)
	copyTask.LastCheckedAt = cloneTimePtr(task.LastCheckedAt)
	if task.PendingAlert != nil {
		alert := *task.PendingAlert
		copyTask.PendingAlert = &alert
	}
	copyTask.RetryState = task.RetryState
	copyTask.RetryState.WindowStartedAt = cloneTimePtr(task.RetryState.WindowStartedAt)
	copyTask.RetryState.NextRetryAt = cloneTimePtr(task.RetryState.NextRetryAt)
	return &copyTask
}

func cloneTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	t := *value
	return &t
}

func cloneTimePtrUTC(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	t := value.UTC()
	return &t
}
