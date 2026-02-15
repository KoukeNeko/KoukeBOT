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
