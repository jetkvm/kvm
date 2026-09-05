package kvm

import "testing"

func TestAbsMouseMappingValidate(t *testing.T) {
	tests := []struct {
		name    string
		mapping AbsMouseMappingConfig
		wantErr bool
	}{
		{"disabled is always valid", AbsMouseMappingConfig{Enabled: false}, false},
		{"valid interior screen", AbsMouseMappingConfig{Enabled: true, TotalWidth: 5263, TotalHeight: 1080, ScreenX: 3343, ScreenY: 0, ScreenWidth: 1920, ScreenHeight: 1080}, false},
		{"screen equals desktop", AbsMouseMappingConfig{Enabled: true, TotalWidth: 1920, TotalHeight: 1080, ScreenX: 0, ScreenY: 0, ScreenWidth: 1920, ScreenHeight: 1080}, false},
		{"zero total", AbsMouseMappingConfig{Enabled: true, TotalWidth: 0, TotalHeight: 1080, ScreenWidth: 1920, ScreenHeight: 1080}, true},
		{"zero screen", AbsMouseMappingConfig{Enabled: true, TotalWidth: 1920, TotalHeight: 1080, ScreenWidth: 0, ScreenHeight: 1080}, true},
		{"negative position", AbsMouseMappingConfig{Enabled: true, TotalWidth: 1920, TotalHeight: 1080, ScreenX: -1, ScreenWidth: 1920, ScreenHeight: 1080}, true},
		{"screen exceeds desktop", AbsMouseMappingConfig{Enabled: true, TotalWidth: 1920, TotalHeight: 1080, ScreenX: 1, ScreenY: 0, ScreenWidth: 1920, ScreenHeight: 1080}, true},
		// Rejected by the sanity bound (which also renders the int64 fit
		// check unreachable for wrapping-scale values — defense in depth).
		{"absurd int32-scale values rejected", AbsMouseMappingConfig{Enabled: true, TotalWidth: 2000000000, TotalHeight: 1080, ScreenX: 2000000000, ScreenY: 0, ScreenWidth: 2000000000, ScreenHeight: 1080}, true},
		{"dimension above sanity bound rejected", AbsMouseMappingConfig{Enabled: true, TotalWidth: maxMappingDimension + 1, TotalHeight: 1080, ScreenX: 0, ScreenY: 0, ScreenWidth: 1920, ScreenHeight: 1080}, true},
		{"dimension at sanity bound accepted", AbsMouseMappingConfig{Enabled: true, TotalWidth: maxMappingDimension, TotalHeight: maxMappingDimension, ScreenX: 0, ScreenY: 0, ScreenWidth: maxMappingDimension, ScreenHeight: maxMappingDimension}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.mapping.validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestApplyAbsMouseMapping(t *testing.T) {
	// The user-facing contract: video position fraction f maps to desktop
	// position screenX + f*screenW, expressed as a fraction of the desktop.
	withMapping := func(m *AbsMouseMappingConfig, fn func()) {
		absMouseMappingLock.Lock()
		if config == nil {
			config = &Config{}
		}
		previous := config.AbsMouseMapping
		config.AbsMouseMapping = m
		absMouseMappingLock.Unlock()
		defer func() {
			absMouseMappingLock.Lock()
			config.AbsMouseMapping = previous
			absMouseMappingLock.Unlock()
		}()
		fn()
	}

	t.Run("nil mapping is identity", func(t *testing.T) {
		withMapping(nil, func() {
			for _, v := range []int{0, 1, 16384, 32766, 32767} {
				if x, y := applyAbsMouseMapping(v, v); x != v || y != v {
					t.Fatalf("identity violated: %d -> (%d,%d)", v, x, y)
				}
			}
		})
	})

	t.Run("disabled mapping is identity", func(t *testing.T) {
		withMapping(&AbsMouseMappingConfig{Enabled: false, TotalWidth: 5263, TotalHeight: 1080, ScreenX: 3343, ScreenWidth: 1920, ScreenHeight: 1080}, func() {
			if x, y := applyAbsMouseMapping(100, 200); x != 100 || y != 200 {
				t.Fatalf("disabled mapping altered coords: (%d,%d)", x, y)
			}
		})
	})

	t.Run("invalid enabled mapping falls back to identity", func(t *testing.T) {
		withMapping(&AbsMouseMappingConfig{Enabled: true, TotalWidth: 0, TotalHeight: 0, ScreenWidth: 0, ScreenHeight: 0}, func() {
			if x, y := applyAbsMouseMapping(100, 200); x != 100 || y != 200 {
				t.Fatalf("invalid mapping altered coords: (%d,%d)", x, y)
			}
		})
	})

	t.Run("three-screen layout maps into captured screen", func(t *testing.T) {
		m := &AbsMouseMappingConfig{Enabled: true, TotalWidth: 5263, TotalHeight: 1080, ScreenX: 3343, ScreenY: 0, ScreenWidth: 1920, ScreenHeight: 1080}
		withMapping(m, func() {
			cases := []struct{ in, wantX, wantY int }{
				// left edge of video -> left edge of captured screen
				{0, 20813, 0}, // round(3343/5263*32767)
				// center -> screen center within desktop
				{16384, 26790, 16384}, // round((3343+960.03)/5263*32767)
				// right edge -> right edge of desktop (screen is rightmost)
				{32767, 32767, 32767},
			}
			for _, c := range cases {
				x, y := applyAbsMouseMapping(c.in, c.in)
				if x != c.wantX {
					t.Errorf("x: %d -> %d, want %d", c.in, x, c.wantX)
				}
				if y != c.wantY {
					t.Errorf("y: %d -> %d, want %d", c.in, y, c.wantY)
				}
			}
		})
	})

	t.Run("screen equals desktop is near-identity", func(t *testing.T) {
		m := &AbsMouseMappingConfig{Enabled: true, TotalWidth: 1920, TotalHeight: 1080, ScreenX: 0, ScreenY: 0, ScreenWidth: 1920, ScreenHeight: 1080}
		withMapping(m, func() {
			for _, v := range []int{0, 1, 12345, 32766, 32767} {
				if x, _ := applyAbsMouseMapping(v, v); x != v {
					t.Fatalf("identity-shaped mapping moved %d to %d", v, x)
				}
			}
		})
	})

	t.Run("output clamped to valid range", func(t *testing.T) {
		m := &AbsMouseMappingConfig{Enabled: true, TotalWidth: 100, TotalHeight: 100, ScreenX: 0, ScreenY: 0, ScreenWidth: 100, ScreenHeight: 100}
		withMapping(m, func() {
			// Inputs outside 0..32767 (defensive: hidrpc decodes uint16)
			if x, _ := applyAbsMouseMapping(40000, 0); x != 32767 {
				t.Fatalf("clamp high failed: %d", x)
			}
			if x, _ := applyAbsMouseMapping(-5, 0); x != 0 {
				t.Fatalf("clamp low failed: %d", x)
			}
		})
	})
}

func TestClampAbsCoord(t *testing.T) {
	cases := []struct {
		in   float64
		want int
	}{
		{-1, 0}, {0, 0}, {0.4, 0}, {0.5, 1}, {16383.6, 16384},
		{32766.5, 32767}, {32767, 32767}, {40000, 32767},
	}
	for _, c := range cases {
		if got := clampAbsCoord(c.in); got != c.want {
			t.Errorf("clampAbsCoord(%v) = %d, want %d", c.in, got, c.want)
		}
	}
}
