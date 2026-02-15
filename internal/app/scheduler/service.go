package scheduler

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/logger"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/store"
)

var logScheduler = logger.New("scheduler", logger.Cyan)

const (
	defaultPollInterval        = 60 * time.Second
	defaultNotifyTimeout       = 20 * time.Second
	defaultMonitorMaxPerUser   = 3
	maxMonitorMaxPerUser       = 20
	maxReminderDuration        = 7 * 24 * time.Hour
	maxMonitorDuration         = 24 * time.Hour
	retryWindow                = 10 * time.Minute
	messagePaginationPageLimit = 5
	messagePaginationPageSize  = 100
)

var retryDelays = []time.Duration{
	1 * time.Minute,
	3 * time.Minute,
	6 * time.Minute,
}

type Policy struct {
	MonitorMaxPerUser int
}

type ServiceOptions struct {
	PollInterval      time.Duration
	NotifyTimeout     time.Duration
	MonitorMaxPerUser int
	NowFunc           func() time.Time
}

type Notifier interface {
	SendMessage(channelID, content string) error
}

type ActivitySource interface {
	ListChannelMessages(ctx context.Context, channelID, before string, limit int) ([]domain.ScheduledActivityMessage, error)
}

type MessageGenerator interface {
	GeneratePersonaNotification(ctx context.Context, personaID, channelID, prompt string) (string, error)
}

type CreateReminderRequest struct {
	PersonaID     string
	ChannelID     string
	CreatorUserID string
	TargetUserID  string
	ReminderText  string
	RunAt         time.Time
}

type CreateMonitorRequest struct {
	PersonaID     string
	ChannelID     string
	CreatorUserID string
	TargetUserID  string
	Duration      time.Duration
}

type ListTasksRequest struct {
	ChannelID       string
	CreatorUserID   string
	IncludeInactive bool
}

type CancelTaskRequest struct {
	TaskID           string
	ChannelID        string
	CreatorUserID    string
	IncludeNonActive bool
}

type CancelAllRequest struct {
	ChannelID       string
	CreatorUserID   string
	IncludeInactive bool
}

type Service struct {
	repo store.ScheduledTaskRepository

	mu            sync.RWMutex
	notifier      Notifier
	activity      ActivitySource
	generator     MessageGenerator
	policy        Policy
	pollInterval  time.Duration
	notifyTimeout time.Duration
	nowFunc       func() time.Time
	startOnce     sync.Once
}

func NewService(repo store.ScheduledTaskRepository, opts ServiceOptions) *Service {
	if opts.PollInterval <= 0 {
		opts.PollInterval = defaultPollInterval
	}
	if opts.NotifyTimeout <= 0 {
		opts.NotifyTimeout = defaultNotifyTimeout
	}
	if opts.NowFunc == nil {
		opts.NowFunc = func() time.Time { return time.Now().UTC() }
	}
	return &Service{
		repo:          repo,
		policy:        normalizePolicy(Policy{MonitorMaxPerUser: opts.MonitorMaxPerUser}),
		pollInterval:  opts.PollInterval,
		notifyTimeout: opts.NotifyTimeout,
		nowFunc:       opts.NowFunc,
	}
}

func (s *Service) SetNotifier(notifier Notifier) {
	s.mu.Lock()
	s.notifier = notifier
	s.mu.Unlock()
}

func (s *Service) SetActivitySource(activity ActivitySource) {
	s.mu.Lock()
	s.activity = activity
	s.mu.Unlock()
}

func (s *Service) SetMessageGenerator(generator MessageGenerator) {
	s.mu.Lock()
	s.generator = generator
	s.mu.Unlock()
}

func (s *Service) UpdatePolicy(policy Policy) {
	s.mu.Lock()
	s.policy = normalizePolicy(policy)
	s.mu.Unlock()
}

func (s *Service) PolicySnapshot() Policy {
	s.mu.RLock()
	policy := s.policy
	s.mu.RUnlock()
	return normalizePolicy(policy)
}

func (s *Service) Start(ctx context.Context) {
	s.startOnce.Do(func() {
		go s.loop(ctx)
	})
}

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

func (s *Service) handleReminderTask(ctx context.Context, task *domain.ScheduledTask, now time.Time) {
	if task.RunAt == nil {
		task.Status = domain.ScheduledTaskStatusFailed
		task.RetryState.LastError = "run_at is missing"
		task.NextRunAt = nil
		_ = s.repo.Save(ctx, task)
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
		_ = s.repo.Save(ctx, task)
		return
	}

	if s.scheduleRetry(task, now, notifyErr) {
		task.UpdatedAt = now
		_ = s.repo.Save(ctx, task)
		return
	}

	fallback := fmt.Sprintf("提醒時間到了：%s", safeReminderText(task.ReminderText))
	fallbackErr := s.sendFallbackNotification(ctx, task, fallback)
	if fallbackErr != nil {
		task.Status = domain.ScheduledTaskStatusFailed
		task.RetryState.LastError = truncateSchedulerError(fallbackErr.Error(), 220)
		task.NextRunAt = nil
		task.UpdatedAt = now
		_ = s.repo.Save(ctx, task)
		return
	}
	task.Status = domain.ScheduledTaskStatusCompleted
	task.RetryState = domain.ScheduledTaskRetryState{}
	task.NextRunAt = nil
	task.UpdatedAt = now
	_ = s.repo.Save(ctx, task)
}

func (s *Service) handleMonitorTask(ctx context.Context, task *domain.ScheduledTask, now time.Time) {
	if task.EndAt == nil {
		task.Status = domain.ScheduledTaskStatusFailed
		task.RetryState.LastError = "end_at is missing"
		task.NextRunAt = nil
		_ = s.repo.Save(ctx, task)
		return
	}
	if !now.Before(task.EndAt.UTC()) {
		task.Status = domain.ScheduledTaskStatusCompleted
		task.NextRunAt = nil
		task.PendingAlert = nil
		task.RetryState = domain.ScheduledTaskRetryState{}
		task.UpdatedAt = now
		_ = s.repo.Save(ctx, task)
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
		task.RetryState.LastError = truncateSchedulerError(err.Error(), 220)
		task.UpdatedAt = now
		_ = s.repo.Save(ctx, task)
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
					task.RetryState = domain.ScheduledTaskRetryState{
						LastError: truncateSchedulerError(fallbackErr.Error(), 220),
					}
				}
			}
		}
	}

	task.UpdatedAt = now
	_ = s.repo.Save(ctx, task)
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
		_ = s.repo.Save(ctx, task)
		return
	}
	if s.scheduleRetry(task, now, notifyErr) {
		task.UpdatedAt = now
		_ = s.repo.Save(ctx, task)
		return
	}
	fallback := fmt.Sprintf("偵測到監控期間有 %d 則活動訊息。", task.PendingAlert.ViolationCount)
	if fallbackErr := s.sendFallbackNotification(ctx, task, fallback); fallbackErr == nil {
		task.PendingAlert = nil
		task.RetryState = domain.ScheduledTaskRetryState{}
	} else {
		task.PendingAlert = nil
		task.RetryState = domain.ScheduledTaskRetryState{
			LastError: truncateSchedulerError(fallbackErr.Error(), 220),
		}
	}
	task.UpdatedAt = now
	_ = s.repo.Save(ctx, task)
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

func (s *Service) sendPersonaNotification(ctx context.Context, task *domain.ScheduledTask, prompt string) error {
	notifier, generator, timeout := s.deliverySnapshot()
	if notifier == nil {
		return fmt.Errorf("notifier is not configured")
	}
	if generator == nil {
		return fmt.Errorf("message generator is not configured")
	}

	notifyCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	content, err := generator.GeneratePersonaNotification(notifyCtx, task.PersonaID, task.ChannelID, prompt)
	if err != nil {
		return err
	}
	text := strings.TrimSpace(content)
	if text == "" {
		return fmt.Errorf("generated notification is empty")
	}
	message := fmt.Sprintf("<@%s> %s", task.TargetUserID, text)
	return notifier.SendMessage(task.ChannelID, message)
}

func (s *Service) sendFallbackNotification(_ context.Context, task *domain.ScheduledTask, fallback string) error {
	notifier := s.notifierSnapshot()
	if notifier == nil {
		return fmt.Errorf("notifier is not configured")
	}
	text := strings.TrimSpace(fallback)
	if text == "" {
		text = "任務已觸發。"
	}
	return notifier.SendMessage(task.ChannelID, fmt.Sprintf("<@%s> %s", task.TargetUserID, text))
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
		task.RetryState.LastError = truncateSchedulerError(err.Error(), 220)
		return false
	}

	delay := retryDelays[task.RetryState.Attempts]
	task.RetryState.Attempts++
	task.RetryState.LastError = truncateSchedulerError(err.Error(), 220)
	next := normalizedNow.Add(delay).UTC()
	task.RetryState.NextRetryAt = &next
	if task.Type == domain.ScheduledTaskTypeReminder {
		task.NextRunAt = &next
	}
	return true
}

func (s *Service) now() time.Time {
	if s.nowFunc == nil {
		return time.Now().UTC()
	}
	return s.nowFunc().UTC()
}

func (s *Service) activitySnapshot() ActivitySource {
	s.mu.RLock()
	activity := s.activity
	s.mu.RUnlock()
	return activity
}

func (s *Service) notifierSnapshot() Notifier {
	s.mu.RLock()
	notifier := s.notifier
	s.mu.RUnlock()
	return notifier
}

func (s *Service) deliverySnapshot() (Notifier, MessageGenerator, time.Duration) {
	s.mu.RLock()
	notifier := s.notifier
	generator := s.generator
	timeout := s.notifyTimeout
	s.mu.RUnlock()
	return notifier, generator, timeout
}

func normalizePolicy(policy Policy) Policy {
	if policy.MonitorMaxPerUser < 1 {
		policy.MonitorMaxPerUser = defaultMonitorMaxPerUser
	}
	if policy.MonitorMaxPerUser > maxMonitorMaxPerUser {
		policy.MonitorMaxPerUser = maxMonitorMaxPerUser
	}
	return policy
}

func buildReminderPrompt(task *domain.ScheduledTask, now time.Time) string {
	reminder := safeReminderText(task.ReminderText)
	return fmt.Sprintf(
		"你要在 Discord 對使用者送出提醒。現在時間：%s。提醒內容：%s。請用角色語氣寫一則簡短提醒訊息。",
		now.Format(time.RFC3339),
		reminder,
	)
}

func buildMonitorViolationPrompt(task *domain.ScheduledTask, now time.Time) string {
	count := 1
	if task.PendingAlert != nil && task.PendingAlert.ViolationCount > 0 {
		count = task.PendingAlert.ViolationCount
	}
	return fmt.Sprintf(
		"你在 Discord 背景監控中偵測到目標使用者在限制期間有活動。現在時間：%s。偵測到 %d 則新活動。請用角色語氣發出簡短警告。",
		now.Format(time.RFC3339),
		count,
	)
}

func safeReminderText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return "提醒時間到了。"
	}
	return text
}

func truncateSchedulerError(text string, maxLen int) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if maxLen <= 0 || len(text) <= maxLen {
		return text
	}
	if maxLen <= 3 {
		return text[:maxLen]
	}
	return text[:maxLen-3] + "..."
}
