package keyboard

import (
	"strings"
	"testing"

	"github.com/jetkvm/kvm/internal/hidrpc"
)

func hidStep(modifier byte, delay uint16, keys ...byte) hidrpc.KeyboardMacroStep {
	buf := make([]byte, hidrpc.HidKeyBufferSize)
	copy(buf, keys)
	return hidrpc.KeyboardMacroStep{Modifier: modifier, Keys: buf, Delay: delay}
}

func assertHIDSteps(t *testing.T, got, want []hidrpc.KeyboardMacroStep) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d steps, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Modifier != want[i].Modifier {
			t.Errorf("step %d: modifier = 0x%02x, want 0x%02x", i, got[i].Modifier, want[i].Modifier)
		}
		if got[i].Delay != want[i].Delay {
			t.Errorf("step %d: delay = %d, want %d", i, got[i].Delay, want[i].Delay)
		}
		if len(got[i].Keys) != hidrpc.HidKeyBufferSize {
			t.Fatalf("step %d: keys length = %d, want %d", i, len(got[i].Keys), hidrpc.HidKeyBufferSize)
		}
		for j := range got[i].Keys {
			if got[i].Keys[j] != want[i].Keys[j] {
				t.Errorf("step %d: keys[%d] = 0x%02x, want 0x%02x", i, j, got[i].Keys[j], want[i].Keys[j])
			}
		}
	}
}

func TestHIDSteps(t *testing.T) {
	tests := []struct {
		name  string
		steps []MacroStep
		want  []hidrpc.KeyboardMacroStep
	}{
		{
			name: "press and release per step",
			steps: []MacroStep{
				{Keys: []string{"KeyH"}, Modifiers: []string{"ShiftLeft"}, Delay: 50},
				{Keys: []string{"Enter"}, Delay: 200},
			},
			want: []hidrpc.KeyboardMacroStep{
				hidStep(0x02, pressDelayMs, 0x0b),
				hidStep(0, 50),
				hidStep(0, pressDelayMs, 0x28),
				hidStep(0, 200),
			},
		},
		{
			name: "modifiers are combined",
			steps: []MacroStep{
				{Keys: []string{"Delete"}, Modifiers: []string{"ControlLeft", "AltLeft"}, Delay: 50},
			},
			want: []hidrpc.KeyboardMacroStep{
				hidStep(0x05, pressDelayMs, 0x4c),
				hidStep(0, 50),
			},
		},
		{
			name: "zero delay falls back to the default",
			steps: []MacroStep{
				{Keys: []string{"KeyA"}},
			},
			want: []hidrpc.KeyboardMacroStep{
				hidStep(0, pressDelayMs, 0x04),
				hidStep(0, defaultDelayMs),
			},
		},
		{
			name: "oversized delay is clamped to uint16",
			steps: []MacroStep{
				{Keys: []string{"KeyA"}, Delay: 100000},
			},
			want: []hidrpc.KeyboardMacroStep{
				hidStep(0, pressDelayMs, 0x04),
				hidStep(0, 65535),
			},
		},
		{
			name: "empty steps are skipped",
			steps: []MacroStep{
				{Delay: 500},
				{Keys: []string{"KeyA"}, Delay: 50},
			},
			want: []hidrpc.KeyboardMacroStep{
				hidStep(0, pressDelayMs, 0x04),
				hidStep(0, 50),
			},
		},
		{
			name: "keys beyond the HID buffer are dropped",
			steps: []MacroStep{
				{Keys: []string{"KeyA", "KeyB", "KeyC", "KeyD", "KeyE", "KeyF", "KeyG", "KeyH"}, Delay: 50},
			},
			want: []hidrpc.KeyboardMacroStep{
				hidStep(0, pressDelayMs, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09),
				hidStep(0, 50),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := HIDSteps(tt.steps)
			if err != nil {
				t.Fatalf("HIDSteps() error = %v", err)
			}
			assertHIDSteps(t, got, tt.want)
		})
	}
}

func TestHIDStepsErrors(t *testing.T) {
	tests := []struct {
		name    string
		steps   []MacroStep
		wantErr string
	}{
		{
			name:    "unknown key",
			steps:   []MacroStep{{Keys: []string{"NotAKey"}}},
			wantErr: "unknown key",
		},
		{
			name:    "unknown modifier",
			steps:   []MacroStep{{Keys: []string{"KeyA"}, Modifiers: []string{"NotAModifier"}}},
			wantErr: "unknown modifier",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := HIDSteps(tt.steps); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("HIDSteps() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

// TestKeyMapsMatchWebUI spot-checks that the ported maps agree with
// ui/src/keyboardMappings.ts.
func TestKeyMapsMatchWebUI(t *testing.T) {
	keyChecks := map[string]byte{
		"KeyA":      0x04,
		"Digit1":    0x1e,
		"Enter":     0x28,
		"Delete":    0x4c,
		"F24":       0x73,
		"AltGr":     0xe6,
		"MetaRight": 0xe7,
	}
	for name, want := range keyChecks {
		if got := keyCodes[name]; got != want {
			t.Errorf("keyCodes[%q] = 0x%02x, want 0x%02x", name, got, want)
		}
	}

	modifierChecks := map[string]byte{
		"ControlLeft":  0x01,
		"ShiftLeft":    0x02,
		"AltRight":     0x40,
		"MetaRight":    0x80,
		"AltGr":        0x40,
		"ControlRight": 0x10,
	}
	for name, want := range modifierChecks {
		if got := modifierMasks[name]; got != want {
			t.Errorf("modifierMasks[%q] = 0x%02x, want 0x%02x", name, got, want)
		}
	}
}
