package meshvpn

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTUNModeIsValid(t *testing.T) {
	tests := []struct {
		mode  TUNMode
		valid bool
	}{
		{TUNModeUserspace, true},
		{TUNModeKernel, true},
		{TUNMode(""), false},
		{TUNMode("invalid"), false},
		{TUNMode("USERSPACE"), false}, // case sensitive
		{TUNMode("Kernel"), false},    // case sensitive
	}

	for _, tc := range tests {
		t.Run(string(tc.mode), func(t *testing.T) {
			assert.Equal(t, tc.valid, tc.mode.IsValid(),
				"TUNMode(%q).IsValid() should be %v", tc.mode, tc.valid)
		})
	}
}

func TestParseTUNMode(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    TUNMode
		wantErr bool
	}{
		{
			name:    "empty defaults to userspace",
			input:   "",
			want:    TUNModeUserspace,
			wantErr: false,
		},
		{
			name:    "explicit userspace",
			input:   "userspace",
			want:    TUNModeUserspace,
			wantErr: false,
		},
		{
			name:    "kernel mode",
			input:   "kernel",
			want:    TUNModeKernel,
			wantErr: false,
		},
		{
			name:    "invalid mode",
			input:   "invalid",
			want:    "",
			wantErr: true,
		},
		{
			name:    "wrong case",
			input:   "KERNEL",
			want:    "",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseTUNMode(tc.input)
			if tc.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "invalid TUN mode")
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tc.want, got)
			}
		})
	}
}

func TestConfigIsProviderEnabled(t *testing.T) {
	tests := []struct {
		name     string
		config   *Config
		provider string
		want     bool
	}{
		{
			name:     "nil config",
			config:   nil,
			provider: "tailscale",
			want:     false,
		},
		{
			name:     "empty config",
			config:   &Config{},
			provider: "tailscale",
			want:     false,
		},
		{
			name: "tailscale disabled",
			config: &Config{
				Tailscale: &TailscaleConfig{Enabled: false},
			},
			provider: "tailscale",
			want:     false,
		},
		{
			name: "tailscale enabled",
			config: &Config{
				Tailscale: &TailscaleConfig{Enabled: true},
			},
			provider: "tailscale",
			want:     true,
		},
		{
			name: "unknown provider",
			config: &Config{
				Tailscale: &TailscaleConfig{Enabled: true},
			},
			provider: "unknown",
			want:     false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.config.IsProviderEnabled(tc.provider)
			assert.Equal(t, tc.want, got)
		})
	}
}
