package usbgadget

import (
	"testing"
	"time"
)

func TestEnumerationRecovery(t *testing.T) {
	start := time.Unix(1000, 0)
	type observation struct {
		second       int
		state        string
		want         bool
		disabled     bool
		absent       bool
		unknown      bool
		lastRecovery int
	}
	tests := []struct {
		name  string
		steps []observation
	}{
		{"grace period and one attempt", []observation{
			{second: 0, state: USBStateDefault},
			{second: 14, state: USBStateDefault},
			{second: 15, state: USBStateDefault, want: true},
			{second: 600, state: USBStateDefault},
		}},
		{"transient rebind states do not rearm", []observation{
			{second: 0, state: USBStateDefault},
			{second: 15, state: USBStateDefault, want: true},
			{second: 16, state: USBStateNotAttached},
			{second: 17, state: USBStateUnknown, unknown: true},
			{second: 18, state: USBStateDefault},
			{second: 60, state: USBStateDefault},
		}},
		{"successful enumeration rearms", []observation{
			{second: 0, state: USBStateDefault},
			{second: 15, state: USBStateDefault, want: true},
			{second: 16, state: USBStateConfigured},
			{second: 20, state: USBStateDefault},
			{second: 35, state: USBStateDefault, want: true},
		}},
		{"known host removal rearms", []observation{
			{second: 0, state: USBStateDefault},
			{second: 15, state: USBStateDefault, want: true},
			{second: 16, state: USBStateDefault, absent: true},
			{second: 20, state: USBStateDefault},
			{second: 35, state: USBStateDefault, want: true},
		}},
		{"master switch alone does not rearm an exhausted attempt", []observation{
			{second: 0, state: USBStateDefault},
			{second: 15, state: USBStateDefault, want: true},
			{second: 16, state: USBStateNotAttached, disabled: true},
			{second: 20, state: USBStateDefault},
			{second: 60, state: USBStateDefault},
		}},
		{"guards require a fresh grace period", []observation{
			{second: 0, state: USBStateDefault},
			{second: 15, state: USBStateDefault, disabled: true},
			{second: 30, state: USBStateDefault, absent: true},
			{second: 45, state: USBStateDefault, unknown: true},
			{second: 60, state: "suspended"},
			{second: 75, state: USBStateDefault},
			{second: 89, state: USBStateDefault},
			{second: 90, state: USBStateDefault, want: true},
		}},
		{"recent configuration or recovery delays attempt", []observation{
			{second: 0, state: USBStateDefault},
			{second: 15, state: USBStateDefault, lastRecovery: 10},
			{second: 24, state: USBStateDefault, lastRecovery: 10},
			{second: 25, state: USBStateDefault, lastRecovery: 10, want: true},
		}},
		{"normal enumeration needs no recovery", []observation{
			{second: 0, state: USBStateDefault},
			{second: 10, state: "addressed"},
			{second: 20, state: USBStateConfigured},
			{second: 40, state: "suspended"},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var recovery EnumerationRecovery
			for i, step := range tt.steps {
				var lastRecovery time.Time
				if step.lastRecovery != 0 {
					lastRecovery = start.Add(time.Duration(step.lastRecovery) * time.Second)
				}
				now := start.Add(time.Duration(step.second) * time.Second)
				got := recovery.ShouldAttempt(step.state, !step.disabled, !step.absent, !step.unknown, lastRecovery, now)
				if got != step.want {
					t.Fatalf("step %d (%s at %ds): got %v, want %v", i, step.state, step.second, got, step.want)
				}
			}
		})
	}
}
