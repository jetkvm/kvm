package usbgadget

import "testing"

func TestExtconHostState(t *testing.T) {
	tests := []struct {
		name        string
		payload     string
		wantPresent bool
		wantKnown   bool
	}{
		{
			name:        "no host (all zero)",
			payload:     "USB=0\nUSB-HOST=0\nUSB_VBUS_EN=0\nSDP=0\nCDP=0\nDCP=0\nSLOW-CHARGER=0\n",
			wantPresent: false,
			wantKnown:   true,
		},
		{
			name:        "real host (SDP set)",
			payload:     "USB=1\nUSB-HOST=1\nUSB_VBUS_EN=1\nSDP=1\nCDP=0\nDCP=0\nSLOW-CHARGER=0\n",
			wantPresent: true,
			wantKnown:   true,
		},
		{
			name:        "USB-HOST set but no SDP",
			payload:     "USB=1\nUSB-HOST=1\nUSB_VBUS_EN=1\nSDP=0\nCDP=0\nDCP=0\nSLOW-CHARGER=0\n",
			wantPresent: true,
			wantKnown:   true,
		},
		{
			name:        "charger only (DCP, VBUS on) -> not a host",
			payload:     "USB=1\nUSB-HOST=0\nUSB_VBUS_EN=1\nSDP=0\nCDP=0\nDCP=1\nSLOW-CHARGER=0\n",
			wantPresent: false,
			wantKnown:   true,
		},
		{
			name:        "charger only (SLOW-CHARGER) -> not a host",
			payload:     "USB=1\nUSB-HOST=0\nUSB_VBUS_EN=1\nSDP=0\nCDP=0\nDCP=0\nSLOW-CHARGER=1\n",
			wantPresent: false,
			wantKnown:   true,
		},
		{
			name:        "VBUS only, no host-capable/charger cable -> known no host",
			payload:     "USB=1\nUSB_VBUS_EN=1\n",
			wantPresent: false,
			wantKnown:   true,
		},
		{
			name:        "empty payload -> unknown (fall back)",
			payload:     "",
			wantPresent: false,
			wantKnown:   false,
		},
		{
			name:        "host-capable lines all zero -> known no host",
			payload:     "USB=0\nUSB-HOST=0\nSDP=0\nCDP=0\n",
			wantPresent: false,
			wantKnown:   true,
		},
		{
			name:        "malformed value ignored, no known cable -> unknown",
			payload:     "USB_VBUS_EN=notanumber\n",
			wantPresent: false,
			wantKnown:   false,
		},
		{
			name:        "recognizable host cable + malformed charger -> known host",
			payload:     "SDP=1\nDCP=notanumber\n",
			wantPresent: true,
			wantKnown:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			present, known := extconHostState(tt.payload)
			if present != tt.wantPresent || known != tt.wantKnown {
				t.Fatalf(
					"extconHostState(%q) = (%v, %v), want (%v, %v)",
					tt.payload, present, known, tt.wantPresent, tt.wantKnown,
				)
			}
		})
	}
}
