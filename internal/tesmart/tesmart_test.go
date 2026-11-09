package tesmart

import (
	"testing"
)

func TestBuildFrame(t *testing.T) {
	tests := []struct {
		name    string
		cmd     byte
		payload byte
		want    []byte
	}{
		{"read active input", CmdReadActive, 0x00, []byte{0xAA, 0xBB, 0x03, 0x10, 0x00, 0xEE}},
		{"switch to PC1", CmdSwitch, 0x01, []byte{0xAA, 0xBB, 0x03, 0x01, 0x01, 0xEE}},
		{"switch to PC8", CmdSwitch, 0x08, []byte{0xAA, 0xBB, 0x03, 0x01, 0x08, 0xEE}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildFrame(tt.cmd, tt.payload)
			if len(got) != len(tt.want) {
				t.Fatalf("frame length = %d, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("frame[%d] = 0x%02x, want 0x%02x (full frame: % x)", i, got[i], tt.want[i], got)
				}
			}
		})
	}
}

func TestParseActiveInput(t *testing.T) {
	tests := []struct {
		name    string
		resp    []byte
		want    int
		wantErr bool
	}{
		{
			name: "valid response with 0xEE trailer",
			resp: []byte{0xAA, 0xBB, 0x03, 0x11, 0x00, 0xEE},
			want: 0,
		},
		{
			name: "valid response with non-0xEE trailer (observed on real hardware)",
			resp: []byte{0xAA, 0xBB, 0x03, 0x11, 0x04, 0x1A},
			want: 4,
		},
		{
			name: "last input of a 16-port switch",
			resp: []byte{0xAA, 0xBB, 0x03, 0x11, 0x0F, 0xEE},
			want: 15,
		},
		{
			name:    "wrong length",
			resp:    []byte{0xAA, 0xBB, 0x03, 0x11, 0x00},
			wantErr: true,
		},
		{
			name:    "wrong header",
			resp:    []byte{0xAA, 0xBC, 0x03, 0x11, 0x00, 0xEE},
			wantErr: true,
		},
		{
			name:    "wrong response command",
			resp:    []byte{0xAA, 0xBB, 0x03, 0x10, 0x00, 0xEE},
			wantErr: true,
		},
		{
			name:    "active input out of range",
			resp:    []byte{0xAA, 0xBB, 0x03, 0x11, 0x10, 0xEE},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseActiveInput(tt.resp)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseActiveInput() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && got != tt.want {
				t.Fatalf("ParseActiveInput() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestNormalizeConfig(t *testing.T) {
	tests := []struct {
		name           string
		in             Config
		wantPort       int
		wantPortCount  int
		wantLabelCount int
		wantFirstLabel string
		wantLastLabel  string
	}{
		{
			name:           "empty config gets defaults",
			in:             Config{},
			wantPort:       5000,
			wantPortCount:  8,
			wantLabelCount: 8,
			wantFirstLabel: "PC1",
			wantLastLabel:  "PC8",
		},
		{
			name: "labels truncated to port count",
			in: Config{
				PortCount: 4,
				Labels:    []string{"a", "b", "c", "d", "e", "f"},
			},
			wantPort:       5000,
			wantPortCount:  4,
			wantLabelCount: 4,
			wantFirstLabel: "a",
			wantLastLabel:  "d",
		},
		{
			name: "labels padded to port count",
			in: Config{
				PortCount: 6,
				Labels:    []string{"a", "", "c"},
			},
			wantPort:       5000,
			wantPortCount:  6,
			wantLabelCount: 6,
			wantFirstLabel: "a",
			wantLastLabel:  "PC6",
		},
		{
			name: "port count clamped to protocol maximum",
			in: Config{
				PortCount: 32,
			},
			wantPort:       5000,
			wantPortCount:  16,
			wantLabelCount: 16,
			wantFirstLabel: "PC1",
			wantLastLabel:  "PC16",
		},
		{
			name: "custom tcp port preserved",
			in: Config{
				Host: "192.168.1.10",
				Port: 1234,
			},
			wantPort:       1234,
			wantPortCount:  8,
			wantLabelCount: 8,
			wantFirstLabel: "PC1",
			wantLastLabel:  "PC8",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tt.in
			NormalizeConfig(&cfg)
			if cfg.Port != tt.wantPort {
				t.Errorf("Port = %d, want %d", cfg.Port, tt.wantPort)
			}
			if cfg.PortCount != tt.wantPortCount {
				t.Errorf("PortCount = %d, want %d", cfg.PortCount, tt.wantPortCount)
			}
			if len(cfg.Labels) != tt.wantLabelCount {
				t.Fatalf("len(Labels) = %d, want %d", len(cfg.Labels), tt.wantLabelCount)
			}
			if cfg.Labels[0] != tt.wantFirstLabel {
				t.Errorf("Labels[0] = %q, want %q", cfg.Labels[0], tt.wantFirstLabel)
			}
			if got := cfg.Labels[len(cfg.Labels)-1]; got != tt.wantLastLabel {
				t.Errorf("Labels[last] = %q, want %q", got, tt.wantLastLabel)
			}
		})
	}
}
