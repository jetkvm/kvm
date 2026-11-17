package kvm

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

type macroMessage struct {
	payload  string
	retained bool
}

func (m macroMessage) Duplicate() bool   { return false }
func (m macroMessage) Qos() byte         { return 0 }
func (m macroMessage) Retained() bool    { return m.retained }
func (m macroMessage) Topic() string     { return "jetkvm/dev/keyboard_macro/set" }
func (m macroMessage) MessageID() uint16 { return 0 }
func (m macroMessage) Payload() []byte   { return []byte(m.payload) }
func (m macroMessage) Ack()              {}

var slowStoredMacro = KeyboardMacro{
	ID:    "macro-1",
	Name:  "Slow",
	Steps: []KeyboardMacroStep{{Keys: []string{"KeyA"}, Delay: 2000}},
}

func useConfig(t *testing.T, c *Config) {
	t.Helper()
	saved := config
	config = c
	t.Cleanup(func() {
		stopRegisteredMacro()
		config = saved
	})
}

func stopRegisteredMacro() {
	cancelKeyboardMacro()
	for !keyboardMacroExec.TryLock() {
		time.Sleep(time.Millisecond)
	}
	keyboardMacroExec.Unlock()
}

func macroConfig(enabled bool) *Config {
	return &Config{
		MqttConfig:     &MQTTConfig{Enabled: true, EnableActions: true, EnableKeyboardMacros: enabled},
		KeyboardMacros: []KeyboardMacro{slowStoredMacro},
	}
}

func TestMQTTMacroIgnoredAfterLoadingOldConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kvm_config.json")
	old := `{"mqtt_config":{"enabled":true,"broker":"b","port":1883,"base_topic":"jetkvm","enable_ha_discovery":true,"enable_actions":true},` +
		`"keyboard_macros":[{"id":"macro-1","name":"Slow","steps":[{"keys":["KeyA"],"modifiers":[],"delay":2000}]}]}`
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	savedPath := configPath
	configPath = path
	defer func() { configPath = savedPath }()
	useConfig(t, nil)
	LoadConfig()

	m := &MQTTManager{}
	m.handleKeyboardMacroCommand(nil, macroMessage{payload: "macro-1"})
	if registeredKeyboardMacro() != nil {
		t.Fatal("a config saved before the keyboard macro switch let MQTT start a macro")
	}

	config.MqttConfig.EnableKeyboardMacros = true
	m.handleKeyboardMacroCommand(nil, macroMessage{payload: "macro-1"})
	if registeredKeyboardMacro() == nil {
		t.Fatal("turning the switch on did not let MQTT start the stored macro")
	}
}

func TestMQTTMacroIgnoredWhenSwitchOffDespiteActions(t *testing.T) {
	useConfig(t, macroConfig(false))
	(&MQTTManager{}).handleKeyboardMacroCommand(nil, macroMessage{payload: "Slow"})
	if registeredKeyboardMacro() != nil {
		t.Fatal("MQTT started a macro with the keyboard macro switch off")
	}
}

func TestMQTTMacroIgnoresRetainedMessage(t *testing.T) {
	useConfig(t, macroConfig(true))
	(&MQTTManager{}).handleKeyboardMacroCommand(nil, macroMessage{payload: "macro-1", retained: true})
	if registeredKeyboardMacro() != nil {
		t.Fatal("a retained message started a macro")
	}
}

func TestMQTTMacroRunsOneAtATime(t *testing.T) {
	useConfig(t, macroConfig(true))
	m := &MQTTManager{}
	m.handleKeyboardMacroCommand(nil, macroMessage{payload: "macro-1"})
	first := registeredKeyboardMacro()
	if first == nil {
		t.Fatal("the first message did not start the macro")
	}
	for range 50 {
		m.handleKeyboardMacroCommand(nil, macroMessage{payload: "macro-1"})
	}
	if registeredKeyboardMacro() != first || first.ctx.Err() != nil {
		t.Fatal("a second message replaced or stopped the running MQTT macro")
	}
}

func TestMQTTMacroDoesNotInterruptBrowserMacro(t *testing.T) {
	useConfig(t, macroConfig(true))
	browser := registerKeyboardMacro()
	(&MQTTManager{}).handleKeyboardMacroCommand(nil, macroMessage{payload: "macro-1"})
	if registeredKeyboardMacro() != browser || browser.ctx.Err() != nil {
		t.Fatal("an MQTT message interrupted a macro from the browser")
	}
}
