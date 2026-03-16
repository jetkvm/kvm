package usbgadget

import (
	"testing"
	"time"
)

func TestShouldAttemptUSBRecovery(t *testing.T) {
	now := time.Unix(100, 0)

	tests := []struct {
		name        string
		state       string
		desired     bool
		lastAttempt time.Time
		want        bool
	}{
		{
			name:    "recover when detached and unbound",
			state:   USBStateNotAttached,
			desired: true,
			want:    true,
		},
		{
			name:    "skip when emulation intentionally disabled",
			state:   USBStateNotAttached,
			desired: false,
			want:    false,
		},
		{
			name:    "skip when USB is configured",
			state:   "configured",
			desired: true,
			want:    false,
		},
		{
			name:        "rate limit repeated recovery attempts",
			state:       USBStateNotAttached,
			desired:     true,
			lastAttempt: now.Add(-USBRecoveryRetryInterval + time.Second),
			want:        false,
		},
		{
			name:        "allow retry after interval passes",
			state:       USBStateNotAttached,
			desired:     true,
			lastAttempt: now.Add(-USBRecoveryRetryInterval),
			want:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShouldAttemptUSBRecovery(tt.state, tt.desired, tt.lastAttempt, now)
			if got != tt.want {
				t.Fatalf("ShouldAttemptUSBRecovery() = %v, want %v", got, tt.want)
			}
		})
	}
}
