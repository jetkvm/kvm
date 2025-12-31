package camera

import (
	"testing"
)

func TestVideoCodec_IsValid(t *testing.T) {
	tests := []struct {
		name  string
		codec VideoCodec
		want  bool
	}{
		{"H264 is valid", CodecH264, true},
		{"MJPEG is valid", CodecMJPEG, true},
		{"Stop is valid", CodecStop, true},
		{"empty string is invalid", VideoCodec(""), false},
		{"arbitrary string is invalid", VideoCodec("invalid"), false},
		{"uppercase H264 is invalid", VideoCodec("H264"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.codec.IsValid(); got != tt.want {
				t.Errorf("VideoCodec(%q).IsValid() = %v, want %v", tt.codec, got, tt.want)
			}
		})
	}
}

func TestVideoCodec_String(t *testing.T) {
	tests := []struct {
		codec VideoCodec
		want  string
	}{
		{CodecH264, "h264"},
		{CodecMJPEG, "mjpeg"},
		{CodecStop, "stop"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.codec.String(); got != tt.want {
				t.Errorf("VideoCodec.String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestVideoCodec_ToByte(t *testing.T) {
	tests := []struct {
		name  string
		codec VideoCodec
		want  byte
	}{
		{"H264 maps to 0x01", CodecH264, CodecByteH264},
		{"MJPEG maps to 0x02", CodecMJPEG, CodecByteMJPEG},
		{"Stop maps to 0x00", CodecStop, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.codec.ToByte(); got != tt.want {
				t.Errorf("VideoCodec(%q).ToByte() = 0x%02x, want 0x%02x", tt.codec, got, tt.want)
			}
		})
	}

	// Invalid codec should panic
	t.Run("Invalid codec panics", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("VideoCodec(\"invalid\").ToByte() should panic")
			}
		}()
		VideoCodec("invalid").ToByte()
	})
}

func TestCodecByteConstants(t *testing.T) {
	// Verify wire protocol constants match documented values
	if CodecByteH264 != 0x01 {
		t.Errorf("CodecByteH264 = 0x%02x, want 0x01", CodecByteH264)
	}
	if CodecByteMJPEG != 0x02 {
		t.Errorf("CodecByteMJPEG = 0x%02x, want 0x02", CodecByteMJPEG)
	}
}

func TestStopFormat(t *testing.T) {
	sf := StopFormat()
	if sf.Codec != CodecStop {
		t.Errorf("StopFormat().Codec = %v, want %v", sf.Codec, CodecStop)
	}
	if sf.Width != 0 || sf.Height != 0 || sf.FrameRate != 0 {
		t.Errorf("StopFormat() should have zero dimensions, got %dx%d@%dfps",
			sf.Width, sf.Height, sf.FrameRate)
	}
}

func TestFormatInfo_Validate(t *testing.T) {
	tests := []struct {
		name    string
		info    FormatInfo
		wantErr bool
	}{
		{
			name:    "valid H264 1080p30",
			info:    FormatInfo{Codec: CodecH264, Width: 1920, Height: 1080, FrameRate: 30},
			wantErr: false,
		},
		{
			name:    "valid MJPEG 720p60",
			info:    FormatInfo{Codec: CodecMJPEG, Width: 1280, Height: 720, FrameRate: 60},
			wantErr: false,
		},
		{
			name:    "stop format is valid",
			info:    StopFormat(),
			wantErr: false,
		},
		{
			name:    "invalid codec",
			info:    FormatInfo{Codec: VideoCodec("invalid"), Width: 1920, Height: 1080, FrameRate: 30},
			wantErr: true,
		},
		{
			name:    "zero width",
			info:    FormatInfo{Codec: CodecH264, Width: 0, Height: 1080, FrameRate: 30},
			wantErr: true,
		},
		{
			name:    "zero height",
			info:    FormatInfo{Codec: CodecH264, Width: 1920, Height: 0, FrameRate: 30},
			wantErr: true,
		},
		{
			name:    "negative width",
			info:    FormatInfo{Codec: CodecH264, Width: -1, Height: 1080, FrameRate: 30},
			wantErr: true,
		},
		{
			name:    "zero frame rate (non-stop)",
			info:    FormatInfo{Codec: CodecH264, Width: 1920, Height: 1080, FrameRate: 0},
			wantErr: true,
		},
		{
			name:    "frame rate too high",
			info:    FormatInfo{Codec: CodecH264, Width: 1920, Height: 1080, FrameRate: 241},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.info.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("FormatInfo.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNewFormatInfo(t *testing.T) {
	tests := []struct {
		name      string
		codec     VideoCodec
		width     int
		height    int
		frameRate int
		wantErr   bool
	}{
		{"valid 1080p30 H264", CodecH264, 1920, 1080, 30, false},
		{"valid 720p60 MJPEG", CodecMJPEG, 1280, 720, 60, false},
		{"invalid codec", VideoCodec("bad"), 1920, 1080, 30, true},
		{"stop codec not allowed", CodecStop, 0, 0, 0, true},
		{"zero width", CodecH264, 0, 1080, 30, true},
		{"zero height", CodecH264, 1920, 0, 30, true},
		{"zero frame rate", CodecH264, 1920, 1080, 0, true},
		{"frame rate too high", CodecH264, 1920, 1080, 300, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := NewFormatInfo(tt.codec, tt.width, tt.height, tt.frameRate)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewFormatInfo() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err == nil {
				if info.Codec != tt.codec || info.Width != tt.width ||
					info.Height != tt.height || info.FrameRate != tt.frameRate {
					t.Errorf("NewFormatInfo() = %+v, want codec=%v w=%d h=%d fps=%d",
						info, tt.codec, tt.width, tt.height, tt.frameRate)
				}
			}
		})
	}
}

func TestFrameStats_Zero(t *testing.T) {
	stats := FrameStats{}
	if stats.DroppedStateFrames != 0 || stats.DroppedWriteFrames != 0 || stats.WriteErrors != 0 {
		t.Errorf("zero FrameStats should have all zero values: %+v", stats)
	}
}

func TestManager_FrameStats(t *testing.T) {
	// Create a minimal manager for testing stats
	m := &Manager{}

	// Initially stats should be zero
	stats := m.GetFrameStats()
	if stats.DroppedStateFrames != 0 || stats.DroppedWriteFrames != 0 || stats.WriteErrors != 0 {
		t.Errorf("initial stats should be zero: %+v", stats)
	}

	// Simulate frame drops
	m.droppedStateFrames.Add(5)
	m.droppedWriteFrames.Add(3)
	m.uvcFrameErrors.Add(1)

	stats = m.GetFrameStats()
	if stats.DroppedStateFrames != 5 {
		t.Errorf("DroppedStateFrames = %d, want 5", stats.DroppedStateFrames)
	}
	if stats.DroppedWriteFrames != 3 {
		t.Errorf("DroppedWriteFrames = %d, want 3", stats.DroppedWriteFrames)
	}
	if stats.WriteErrors != 1 {
		t.Errorf("WriteErrors = %d, want 1", stats.WriteErrors)
	}

	// Reset stats
	m.ResetFrameStats()
	stats = m.GetFrameStats()
	if stats.DroppedStateFrames != 0 || stats.DroppedWriteFrames != 0 || stats.WriteErrors != 0 {
		t.Errorf("stats after reset should be zero: %+v", stats)
	}
}

func TestNewManager(t *testing.T) {
	t.Run("nil gadget returns error", func(t *testing.T) {
		_, err := NewManager(Config{})
		if err != ErrNilGadget {
			t.Errorf("NewManager(nil gadget) error = %v, want %v", err, ErrNilGadget)
		}
	})

	t.Run("valid config creates manager", func(t *testing.T) {
		m, err := NewManager(Config{Gadget: &mockGadget{}})
		if err != nil {
			t.Errorf("NewManager() unexpected error: %v", err)
		}
		if m == nil {
			t.Error("NewManager() returned nil manager")
		}
	})
}

func TestManager_SetEnabled(t *testing.T) {
	m := &Manager{}

	if m.IsEnabled() {
		t.Error("Manager should be disabled by default")
	}

	m.SetEnabled(true)
	if !m.IsEnabled() {
		t.Error("Manager should be enabled after SetEnabled(true)")
	}

	m.SetEnabled(false)
	if m.IsEnabled() {
		t.Error("Manager should be disabled after SetEnabled(false)")
	}
}

// mockGadget implements GadgetController for testing
type mockGadget struct{}

func (m *mockGadget) GetUVCVideoDevice() (string, error) {
	return "/dev/video0", nil
}
