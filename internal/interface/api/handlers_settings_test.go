package api

import (
	"testing"

	"github.com/KoukeNeko/KoukeBOT/internal/infra/config"
)

func TestValidatePromptGuardSettings(t *testing.T) {
	tests := []struct {
		name    string
		lock    int
		vis     string
		enabled bool
		text    string
		wantErr bool
	}{
		{
			name:    "valid defaults",
			lock:    4,
			vis:     "visible",
			enabled: true,
			text:    "forbidden generic tone",
			wantErr: false,
		},
		{
			name:    "invalid lock",
			lock:    0,
			vis:     "visible",
			enabled: true,
			text:    "x",
			wantErr: true,
		},
		{
			name:    "invalid visibility",
			lock:    4,
			vis:     "all",
			enabled: true,
			text:    "x",
			wantErr: true,
		},
		{
			name:    "missing constraint when enabled",
			lock:    4,
			vis:     "debug",
			enabled: true,
			text:    "   ",
			wantErr: true,
		},
		{
			name:    "empty constraint allowed when disabled",
			lock:    4,
			vis:     "hidden",
			enabled: false,
			text:    "",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		err := validatePromptGuardSettings(tt.lock, tt.vis, tt.enabled, tt.text)
		if tt.wantErr && err == nil {
			t.Fatalf("%s: expected error", tt.name)
		}
		if !tt.wantErr && err != nil {
			t.Fatalf("%s: unexpected error: %v", tt.name, err)
		}
	}
}

func TestValidateDiscordObserveSettings(t *testing.T) {
	tests := []struct {
		name      string
		maxResult int
		scope     string
		wantErr   bool
	}{
		{
			name:      "valid current guild",
			maxResult: 25,
			scope:     config.DiscordObserveScopeCurrentGuild,
			wantErr:   false,
		},
		{
			name:      "valid current channel",
			maxResult: 100,
			scope:     config.DiscordObserveScopeCurrentChannel,
			wantErr:   false,
		},
		{
			name:      "invalid max",
			maxResult: 0,
			scope:     config.DiscordObserveScopeCurrentGuild,
			wantErr:   true,
		},
		{
			name:      "invalid scope",
			maxResult: 25,
			scope:     "all",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		err := validateDiscordObserveSettings(tt.maxResult, tt.scope)
		if tt.wantErr && err == nil {
			t.Fatalf("%s: expected error", tt.name)
		}
		if !tt.wantErr && err != nil {
			t.Fatalf("%s: unexpected error: %v", tt.name, err)
		}
	}
}

func TestValidateToolLoopSettings(t *testing.T) {
	tests := []struct {
		name           string
		maxRounds      int
		maxWebSearch   int
		maxWaitSeconds int
		wantErr        bool
	}{
		{
			name:           "valid defaults",
			maxRounds:      3,
			maxWebSearch:   2,
			maxWaitSeconds: 5,
			wantErr:        false,
		},
		{
			name:           "invalid max rounds low",
			maxRounds:      0,
			maxWebSearch:   2,
			maxWaitSeconds: 5,
			wantErr:        true,
		},
		{
			name:           "invalid max rounds high",
			maxRounds:      7,
			maxWebSearch:   2,
			maxWaitSeconds: 5,
			wantErr:        true,
		},
		{
			name:           "invalid web search low",
			maxRounds:      3,
			maxWebSearch:   0,
			maxWaitSeconds: 5,
			wantErr:        true,
		},
		{
			name:           "invalid web search high",
			maxRounds:      3,
			maxWebSearch:   5,
			maxWaitSeconds: 5,
			wantErr:        true,
		},
		{
			name:           "invalid wait seconds low",
			maxRounds:      3,
			maxWebSearch:   2,
			maxWaitSeconds: 0,
			wantErr:        true,
		},
		{
			name:           "invalid wait seconds high",
			maxRounds:      3,
			maxWebSearch:   2,
			maxWaitSeconds: 16,
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		err := validateToolLoopSettings(tt.maxRounds, tt.maxWebSearch, tt.maxWaitSeconds)
		if tt.wantErr && err == nil {
			t.Fatalf("%s: expected error", tt.name)
		}
		if !tt.wantErr && err != nil {
			t.Fatalf("%s: unexpected error: %v", tt.name, err)
		}
	}
}

func TestValidateMemoryRetentionSettings(t *testing.T) {
	tests := []struct {
		name    string
		rounds  int
		wantErr bool
	}{
		{name: "valid minimum", rounds: config.MinMemoryRecentRounds, wantErr: false},
		{name: "valid default", rounds: config.DefaultMemoryRecentRounds, wantErr: false},
		{name: "valid maximum", rounds: config.MaxMemoryRecentRounds, wantErr: false},
		{name: "too small", rounds: config.MinMemoryRecentRounds - 1, wantErr: true},
		{name: "too large", rounds: config.MaxMemoryRecentRounds + 1, wantErr: true},
	}

	for _, tt := range tests {
		err := validateMemoryRetentionSettings(tt.rounds)
		if tt.wantErr && err == nil {
			t.Fatalf("%s: expected error", tt.name)
		}
		if !tt.wantErr && err != nil {
			t.Fatalf("%s: unexpected error: %v", tt.name, err)
		}
	}
}
