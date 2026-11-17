package keyboard

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
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

var uiMapEntry = regexp.MustCompile(`^\s*"?([A-Za-z0-9_]+)"?:\s*0x([0-9a-fA-F]{1,2}),\s*(//.*)?$`)

func parseUIMap(t *testing.T, source, name string) map[string]byte {
	t.Helper()
	header := "export const " + name + " = {"
	start := strings.Index(source, header)
	if start < 0 {
		t.Fatalf("%s not found in keyboardMappings.ts", header)
	}
	body := source[start+len(header):]
	end := strings.Index(body, "\n}")
	if end < 0 {
		t.Fatalf("end of %s not found in keyboardMappings.ts", name)
	}
	entries := map[string]byte{}
	for _, line := range strings.Split(body[:end], "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue
		}
		m := uiMapEntry.FindStringSubmatch(line)
		if m == nil {
			t.Fatalf("cannot parse %s entry %q in keyboardMappings.ts", name, trimmed)
		}
		code, err := strconv.ParseUint(m[2], 16, 8)
		if err != nil {
			t.Fatal(err)
		}
		entries[m[1]] = byte(code)
	}
	return entries
}

func assertSameMap(t *testing.T, label string, got, want map[string]byte) {
	t.Helper()
	for name, code := range want {
		if goCode, ok := got[name]; !ok {
			t.Errorf("%s: %q is in the UI map but not in the Go map", label, name)
		} else if goCode != code {
			t.Errorf("%s: %q = 0x%02x in Go, 0x%02x in the UI", label, name, goCode, code)
		}
	}
	for name := range got {
		if _, ok := want[name]; !ok {
			t.Errorf("%s: %q is in the Go map but not in the UI map", label, name)
		}
	}
}

func TestKeyMapsMatchWebUI(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "ui", "src", "keyboardMappings.ts"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	assertSameMap(t, "keys", keyCodes, parseUIMap(t, source, "keys"))
	assertSameMap(t, "modifiers", modifierMasks, parseUIMap(t, source, "modifiers"))
}
