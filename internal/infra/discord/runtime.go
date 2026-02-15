package discord

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/bwmarrin/discordgo"
)

const (
	ObserveScopeCurrentChannel = "current_channel"
	ObserveScopeCurrentGuild   = "current_guild"
)

var (
	ErrRuntimeUnavailable    = errors.New("discord runtime unavailable")
	ErrMissingGuildID        = errors.New("guild context is required")
	ErrMissingChannelID      = errors.New("channel context is required")
	ErrMissingUserID         = errors.New("user id is required")
	ErrMissingMemberIntent   = errors.New("discord member intent is disabled")
	ErrMissingPresenceIntent = errors.New("discord presence intent is disabled")
)

type RuntimeOptions struct {
	MemberIntentEnabled   bool
	PresenceIntentEnabled bool
	MessageScope          string
	MaxResults            int
}

type RuntimeCapabilities struct {
	Ready                 bool   `json:"ready"`
	MemberIntentEnabled   bool   `json:"member_intent_enabled"`
	PresenceIntentEnabled bool   `json:"presence_intent_enabled"`
	MessageScope          string `json:"message_scope"`
	MaxResults            int    `json:"max_results"`
}

// Runtime provides read-only access to Discord session state and API.
type Runtime struct {
	mu      sync.RWMutex
	session *discordgo.Session
	options RuntimeOptions
}

func NewRuntime(options RuntimeOptions) *Runtime {
	options = normalizeRuntimeOptions(options)
	return &Runtime{options: options}
}

func normalizeRuntimeOptions(options RuntimeOptions) RuntimeOptions {
	if options.MaxResults <= 0 {
		options.MaxResults = 25
	}
	if options.MaxResults > 100 {
		options.MaxResults = 100
	}
	switch options.MessageScope {
	case ObserveScopeCurrentChannel, ObserveScopeCurrentGuild:
		// keep
	default:
		options.MessageScope = ObserveScopeCurrentGuild
	}
	return options
}

func (r *Runtime) SetOptions(options RuntimeOptions) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.options = normalizeRuntimeOptions(options)
}

func (r *Runtime) Options() RuntimeOptions {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.options
}

func (r *Runtime) Capabilities() RuntimeCapabilities {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return RuntimeCapabilities{
		Ready:                 r.session != nil,
		MemberIntentEnabled:   r.options.MemberIntentEnabled,
		PresenceIntentEnabled: r.options.PresenceIntentEnabled,
		MessageScope:          r.options.MessageScope,
		MaxResults:            r.options.MaxResults,
	}
}

func (r *Runtime) BindSession(session *discordgo.Session) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.session = session
}

func (r *Runtime) withSession(fn func(s *discordgo.Session) error) error {
	r.mu.RLock()
	session := r.session
	r.mu.RUnlock()
	if session == nil {
		return ErrRuntimeUnavailable
	}
	return fn(session)
}

func (r *Runtime) Guild(ctx context.Context, guildID string) (*discordgo.Guild, error) {
	_ = ctx
	if guildID == "" {
		return nil, ErrMissingGuildID
	}
	var out *discordgo.Guild
	err := r.withSession(func(s *discordgo.Session) error {
		g, err := s.Guild(guildID)
		if err != nil {
			return fmt.Errorf("fetch guild: %w", err)
		}
		out = g
		return nil
	})
	return out, err
}

func (r *Runtime) GuildChannels(ctx context.Context, guildID string) ([]*discordgo.Channel, error) {
	_ = ctx
	if guildID == "" {
		return nil, ErrMissingGuildID
	}
	var out []*discordgo.Channel
	err := r.withSession(func(s *discordgo.Session) error {
		channels, err := s.GuildChannels(guildID)
		if err != nil {
			return fmt.Errorf("list guild channels: %w", err)
		}
		out = channels
		return nil
	})
	return out, err
}

func (r *Runtime) GuildMembers(ctx context.Context, guildID, after string, limit int) ([]*discordgo.Member, error) {
	_ = ctx
	if guildID == "" {
		return nil, ErrMissingGuildID
	}
	if !r.Options().MemberIntentEnabled {
		return nil, ErrMissingMemberIntent
	}
	var out []*discordgo.Member
	err := r.withSession(func(s *discordgo.Session) error {
		members, err := s.GuildMembers(guildID, after, limit)
		if err != nil {
			return fmt.Errorf("list guild members: %w", err)
		}
		out = members
		return nil
	})
	return out, err
}

func (r *Runtime) GuildMember(ctx context.Context, guildID, userID string) (*discordgo.Member, error) {
	_ = ctx
	if guildID == "" {
		return nil, ErrMissingGuildID
	}
	if userID == "" {
		return nil, ErrMissingUserID
	}
	var out *discordgo.Member
	err := r.withSession(func(s *discordgo.Session) error {
		member, err := s.GuildMember(guildID, userID)
		if err != nil {
			return fmt.Errorf("get guild member: %w", err)
		}
		out = member
		return nil
	})
	return out, err
}

func (r *Runtime) GuildPresences(guildID string) ([]*discordgo.Presence, error) {
	if guildID == "" {
		return nil, ErrMissingGuildID
	}
	if !r.Options().PresenceIntentEnabled {
		return nil, ErrMissingPresenceIntent
	}

	r.mu.RLock()
	session := r.session
	r.mu.RUnlock()
	if session == nil {
		return nil, ErrRuntimeUnavailable
	}

	guild, err := session.State.Guild(guildID)
	if err != nil {
		return nil, fmt.Errorf("get guild from state: %w", err)
	}
	return guild.Presences, nil
}

func (r *Runtime) Channel(ctx context.Context, channelID string) (*discordgo.Channel, error) {
	_ = ctx
	if channelID == "" {
		return nil, ErrMissingChannelID
	}
	var out *discordgo.Channel
	err := r.withSession(func(s *discordgo.Session) error {
		channel, err := s.Channel(channelID)
		if err != nil {
			return fmt.Errorf("get channel: %w", err)
		}
		out = channel
		return nil
	})
	return out, err
}

func (r *Runtime) ChannelMessages(ctx context.Context, channelID string, limit int, before string) ([]*discordgo.Message, error) {
	_ = ctx
	if channelID == "" {
		return nil, ErrMissingChannelID
	}
	if limit <= 0 {
		limit = r.Options().MaxResults
	}
	if limit > 100 {
		limit = 100
	}
	var out []*discordgo.Message
	err := r.withSession(func(s *discordgo.Session) error {
		msgs, err := s.ChannelMessages(channelID, limit, before, "", "")
		if err != nil {
			return fmt.Errorf("list channel messages: %w", err)
		}
		out = msgs
		return nil
	})
	return out, err
}
