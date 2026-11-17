package mdns

import "testing"

func TestSameServiceExceptTXT(t *testing.T) {
	notSetUp := deviceService(DeviceState{Hostname: "jetkvm-a", DeviceID: "a"})
	setUp := deviceService(DeviceState{Hostname: "jetkvm-a", DeviceID: "a", SetupDone: true})
	tls := deviceService(DeviceState{Hostname: "jetkvm-a", DeviceID: "a", SetupDone: true, TLSEnabled: true})
	renamed := deviceService(DeviceState{Hostname: "jetkvm-b", DeviceID: "a", SetupDone: true})
	loopback := deviceService(DeviceState{Hostname: "jetkvm-a", LoopbackOnly: true})

	tests := []struct {
		name string
		a, b *MDNSService
		want bool
	}{
		{"setup flips", notSetUp, setUp, true},
		{"port changes", setUp, tls, false},
		{"instance changes", setUp, renamed, false},
		{"service removed", setUp, loopback, false},
		{"service added", loopback, setUp, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sameServiceExceptTXT(tt.a, tt.b); got != tt.want {
				t.Errorf("sameServiceExceptTXT = %v, want %v", got, tt.want)
			}
		})
	}
}
