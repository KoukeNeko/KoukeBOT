package api

import "testing"

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
