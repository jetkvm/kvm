package failsafe

import (
	"testing"

	"github.com/jetkvm/kvm/internal/supervisor"
)

func TestClassifyCrashLog(t *testing.T) {
	tests := []struct {
		name     string
		crashLog string
		reason   string
		active   bool
	}{
		{
			name:     "native restart exhaustion marker activates native failsafe",
			crashLog: "max restart attempts reached, exiting: " + supervisor.FailsafeReasonNativeMaxRestartAttemptsReached,
			reason:   ReasonNative,
			active:   true,
		},
		{
			name:     "empty crash log is diagnostic only",
			crashLog: "",
			reason:   ReasonUnknown,
			active:   false,
		},
		{
			name: "native looking crash without marker is diagnostic only",
			crashLog: `goroutine 1 [syscall]:
runtime.cgocall()
github.com/jetkvm/kvm/internal/native.NewNative(...)`,
			reason: ReasonUnknown,
			active: false,
		},
		{
			name: "network monitor crash is diagnostic only",
			crashLog: `SIGILL: illegal instruction
github.com/vishvananda/netlink.(*Handle).LinkByName
github.com/jetkvm/kvm/pkg/nmlite.(*InterfaceManager).monitorInterfaceState`,
			reason: ReasonUnknown,
			active: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reason, active := ClassifyCrashLog(tt.crashLog)
			if reason != tt.reason || active != tt.active {
				t.Fatalf("ClassifyCrashLog() = (%q, %v), want (%q, %v)", reason, active, tt.reason, tt.active)
			}
		})
	}
}
