package kvm

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
			state:   "not attached",
			desired: true,
			want:    true,
		},
		{
			name:    "skip when emulation intentionally disabled",
			state:   "not attached",
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
			state:       "not attached",
			desired:     true,
			lastAttempt: now.Add(-usbRecoveryRetryInterval + time.Second),
			want:        false,
		},
		{
			name:        "allow retry after interval passes",
			state:       "not attached",
			desired:     true,
			lastAttempt: now.Add(-usbRecoveryRetryInterval),
			want:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldAttemptUSBRecovery(tt.state, tt.desired, tt.lastAttempt, now)
			if got != tt.want {
				t.Fatalf("shouldAttemptUSBRecovery() = %v, want %v", got, tt.want)
			}
		})
	}
}
