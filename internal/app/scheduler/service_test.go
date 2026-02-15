package scheduler

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/store"
)

type testTaskRepo struct {
	mu    sync.RWMutex
	tasks map[string]*domain.ScheduledTask
}

func newTestTaskRepo() *testTaskRepo {
	return &testTaskRepo{tasks: make(map[string]*domain.ScheduledTask)}
}

func (r *testTaskRepo) GetByID(_ context.Context, taskID string) (*domain.ScheduledTask, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	task, ok := r.tasks[taskID]
	if !ok {
		return nil, fmt.Errorf("%w: %s", store.ErrScheduledTaskNotFound, taskID)
	}
	return cloneTask(task), nil
}

func (r *testTaskRepo) List(_ context.Context) ([]*domain.ScheduledTask, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*domain.ScheduledTask, 0, len(r.tasks))
	for _, task := range r.tasks {
		out = append(out, cloneTask(task))
	}
	return out, nil
}

func (r *testTaskRepo) Save(_ context.Context, task *domain.ScheduledTask) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tasks[task.ID] = cloneTask(task)
	return nil
}

func (r *testTaskRepo) Delete(_ context.Context, taskID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.tasks, taskID)
	return nil
}

func cloneTask(task *domain.ScheduledTask) *domain.ScheduledTask {
	if task == nil {
		return nil
	}
	copyTask := *task
	if task.RunAt != nil {
		runAt := *task.RunAt
		copyTask.RunAt = &runAt
	}
	if task.EndAt != nil {
		endAt := *task.EndAt
		copyTask.EndAt = &endAt
	}
	if task.NextRunAt != nil {
		next := *task.NextRunAt
		copyTask.NextRunAt = &next
	}
	if task.LastCheckedAt != nil {
		last := *task.LastCheckedAt
		copyTask.LastCheckedAt = &last
	}
	if task.PendingAlert != nil {
		alert := *task.PendingAlert
		copyTask.PendingAlert = &alert
	}
	copyTask.RetryState = task.RetryState
	if task.RetryState.NextRetryAt != nil {
		nextRetry := *task.RetryState.NextRetryAt
		copyTask.RetryState.NextRetryAt = &nextRetry
	}
	if task.RetryState.WindowStartedAt != nil {
		window := *task.RetryState.WindowStartedAt
		copyTask.RetryState.WindowStartedAt = &window
	}
	return &copyTask
}

type testNotifier struct {
	mu       sync.Mutex
	messages []string
}

func (n *testNotifier) SendMessage(_ string, content string) error {
	n.mu.Lock()
	n.messages = append(n.messages, content)
	n.mu.Unlock()
	return nil
}

func (n *testNotifier) Count() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.messages)
}

type testGenerator struct {
	mu      sync.Mutex
	err     error
	content string
}

func (g *testGenerator) GeneratePersonaNotification(_ context.Context, _, _ string, _ string) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.err != nil {
		return "", g.err
	}
	return g.content, nil
}

type testActivitySource struct {
	mu        sync.Mutex
	pages     [][]domain.ScheduledActivityMessage
	callCount int
}

func (s *testActivitySource) ListChannelMessages(_ context.Context, _ string, _ string, _ int) ([]domain.ScheduledActivityMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.callCount >= len(s.pages) {
		s.callCount++
		return []domain.ScheduledActivityMessage{}, nil
	}
	out := s.pages[s.callCount]
	s.callCount++
	return out, nil
}

func TestServiceReminderTriggersAndCompletes(t *testing.T) {
	repo := newTestTaskRepo()
	now := time.Date(2026, time.February, 15, 10, 0, 0, 0, time.UTC)
	service := NewService(repo, ServiceOptions{
		NowFunc: func() time.Time { return now },
	})

	notifier := &testNotifier{}
	generator := &testGenerator{content: "提醒到了"}
	service.SetNotifier(notifier)
	service.SetMessageGenerator(generator)

	task, err := service.CreateReminder(context.Background(), CreateReminderRequest{
		PersonaID:     "persona-a",
		ChannelID:     "ch-1",
		CreatorUserID: "u-1",
		TargetUserID:  "u-1",
		ReminderText:  "喝水",
		RunAt:         now.Add(2 * time.Minute),
	})
	if err != nil {
		t.Fatalf("CreateReminder error: %v", err)
	}

	now = now.Add(2 * time.Minute)
	service.runCycle(context.Background())

	stored, err := repo.GetByID(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("GetByID error: %v", err)
	}
	if stored.Status != domain.ScheduledTaskStatusCompleted {
		t.Fatalf("status=%s, want completed", stored.Status)
	}
	if notifier.Count() != 1 {
		t.Fatalf("notifier count=%d, want 1", notifier.Count())
	}
}

func TestServiceReminderRetryThenFallback(t *testing.T) {
	repo := newTestTaskRepo()
	now := time.Date(2026, time.February, 15, 10, 0, 0, 0, time.UTC)
	service := NewService(repo, ServiceOptions{
		NowFunc: func() time.Time { return now },
	})

	notifier := &testNotifier{}
	generator := &testGenerator{err: fmt.Errorf("llm unavailable")}
	service.SetNotifier(notifier)
	service.SetMessageGenerator(generator)

	task, err := service.CreateReminder(context.Background(), CreateReminderRequest{
		PersonaID:     "persona-a",
		ChannelID:     "ch-1",
		CreatorUserID: "u-1",
		TargetUserID:  "u-1",
		ReminderText:  "喝水",
		RunAt:         now,
	})
	if err != nil {
		t.Fatalf("CreateReminder error: %v", err)
	}

	service.runCycle(context.Background()) // retry #1 scheduled (+1m)
	now = now.Add(1 * time.Minute)
	service.runCycle(context.Background()) // retry #2 scheduled (+3m)
	now = now.Add(3 * time.Minute)
	service.runCycle(context.Background()) // retry #3 scheduled (+6m)
	now = now.Add(6 * time.Minute)
	service.runCycle(context.Background()) // fallback send

	stored, err := repo.GetByID(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("GetByID error: %v", err)
	}
	if stored.Status != domain.ScheduledTaskStatusCompleted {
		t.Fatalf("status=%s, want completed", stored.Status)
	}
	if notifier.Count() != 1 {
		t.Fatalf("fallback message count=%d, want 1", notifier.Count())
	}
}

func TestServiceMonitorViolationSendsSingleWindowAlert(t *testing.T) {
	repo := newTestTaskRepo()
	now := time.Date(2026, time.February, 15, 10, 0, 0, 0, time.UTC)
	service := NewService(repo, ServiceOptions{
		NowFunc: func() time.Time { return now },
	})

	notifier := &testNotifier{}
	generator := &testGenerator{content: "抓到你了"}
	source := &testActivitySource{
		pages: [][]domain.ScheduledActivityMessage{
			{
				{ID: "m1", AuthorID: "u-2", Content: "hi", Timestamp: now.Add(30 * time.Second)},
			},
			{},
		},
	}
	service.SetNotifier(notifier)
	service.SetMessageGenerator(generator)
	service.SetActivitySource(source)

	task, err := service.CreateMonitor(context.Background(), CreateMonitorRequest{
		PersonaID:     "persona-a",
		ChannelID:     "ch-1",
		CreatorUserID: "u-1",
		TargetUserID:  "u-2",
		Duration:      2 * time.Hour,
	})
	if err != nil {
		t.Fatalf("CreateMonitor error: %v", err)
	}

	now = now.Add(service.pollInterval)
	service.runCycle(context.Background())
	now = now.Add(service.pollInterval)
	service.runCycle(context.Background())

	stored, err := repo.GetByID(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("GetByID error: %v", err)
	}
	if stored.Status != domain.ScheduledTaskStatusActive {
		t.Fatalf("status=%s, want active", stored.Status)
	}
	if notifier.Count() != 1 {
		t.Fatalf("alert count=%d, want 1", notifier.Count())
	}
}

func TestServiceMonitorMaxPerUserLimit(t *testing.T) {
	repo := newTestTaskRepo()
	now := time.Date(2026, time.February, 15, 10, 0, 0, 0, time.UTC)
	service := NewService(repo, ServiceOptions{
		NowFunc:           func() time.Time { return now },
		MonitorMaxPerUser: 3,
	})

	for i := 0; i < 3; i++ {
		_, err := service.CreateMonitor(context.Background(), CreateMonitorRequest{
			PersonaID:     "persona-a",
			ChannelID:     "ch-1",
			CreatorUserID: "u-1",
			TargetUserID:  fmt.Sprintf("u-%d", i),
			Duration:      time.Hour,
		})
		if err != nil {
			t.Fatalf("CreateMonitor #%d error: %v", i+1, err)
		}
	}

	if _, err := service.CreateMonitor(context.Background(), CreateMonitorRequest{
		PersonaID:     "persona-a",
		ChannelID:     "ch-1",
		CreatorUserID: "u-1",
		TargetUserID:  "u-over",
		Duration:      time.Hour,
	}); err == nil {
		t.Fatalf("expected monitor limit error")
	}
}
