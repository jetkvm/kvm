package hidrpc

import "testing"

func TestWheelReport(t *testing.T) {
	tests := []struct {
		name       string
		message    Message
		wantDeltaY int8
		wantDeltaX int8
		wantErr    bool
	}{
		{
			name:       "decode positive Y, zero X",
			message:    Message{t: TypeWheelReport, d: []byte{0x01, 0x00}},
			wantDeltaY: 1,
			wantDeltaX: 0,
		},
		{
			name:       "decode negative Y as two's complement",
			message:    Message{t: TypeWheelReport, d: []byte{0xFF, 0x00}},
			wantDeltaY: -1,
			wantDeltaX: 0,
		},
		{
			name:       "decode zero",
			message:    Message{t: TypeWheelReport, d: []byte{0x00, 0x00}},
			wantDeltaY: 0,
			wantDeltaX: 0,
		},
		{
			name:       "decode positive X, zero Y",
			message:    Message{t: TypeWheelReport, d: []byte{0x00, 0x01}},
			wantDeltaY: 0,
			wantDeltaX: 1,
		},
		{
			name:       "decode negative X as two's complement",
			message:    Message{t: TypeWheelReport, d: []byte{0x00, 0xFF}},
			wantDeltaY: 0,
			wantDeltaX: -1,
		},
		{
			name:       "decode both axes",
			message:    Message{t: TypeWheelReport, d: []byte{0x02, 0xFE}},
			wantDeltaY: 2,
			wantDeltaX: -2,
		},
		{
			name:    "wrong message type",
			message: Message{t: TypeMouseReport, d: []byte{0x01, 0x02}},
			wantErr: true,
		},
		{
			name:    "empty payload",
			message: Message{t: TypeWheelReport, d: []byte{}},
			wantErr: true,
		},
		{
			name:    "payload too short",
			message: Message{t: TypeWheelReport, d: []byte{0x01}},
			wantErr: true,
		},
		{
			name:    "payload too long",
			message: Message{t: TypeWheelReport, d: []byte{0x01, 0x02, 0x03}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.message.WheelReport()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("WheelReport() expected error, got nil (deltaY=%d, deltaX=%d)", got.DeltaY, got.DeltaX)
				}
				return
			}
			if err != nil {
				t.Fatalf("WheelReport() unexpected error: %v", err)
			}
			if got.DeltaY != tt.wantDeltaY {
				t.Fatalf("WheelReport() DeltaY = %d, want %d", got.DeltaY, tt.wantDeltaY)
			}
			if got.DeltaX != tt.wantDeltaX {
				t.Fatalf("WheelReport() DeltaX = %d, want %d", got.DeltaX, tt.wantDeltaX)
			}
		})
	}
}
