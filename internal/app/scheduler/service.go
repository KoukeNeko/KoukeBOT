package scheduler

import (
	"context"
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
