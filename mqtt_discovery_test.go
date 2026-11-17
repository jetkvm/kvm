package kvm

import (
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

func TestMacroObjectID(t *testing.T) {
	cases := map[string]string{
		"macro-1761590000000000000": "macro_macro-1761590000000000000",
		"login_root":                "macro_login_root",
		"with space/slash":          "macro_with_space_slash",
	}
	for id, want := range cases {
		if got := macroObjectID(id); got != want {
			t.Errorf("macroObjectID(%q) = %q, want %q", id, got, want)
		}
	}
}

type doneToken struct{}

func (doneToken) Wait() bool                     { return true }
func (doneToken) WaitTimeout(time.Duration) bool { return true }
func (doneToken) Error() error                   { return nil }
func (doneToken) Done() <-chan struct{} {
	c := make(chan struct{})
	close(c)
	return c
}

// retainingClient keeps the last retained payload per topic, like a broker.
type retainingClient struct {
	mqtt.Client
	connected bool
	retained  map[string]string
}

func (c *retainingClient) IsConnected() bool { return c.connected }
func (c *retainingClient) Publish(topic string, _ byte, _ bool, payload interface{}) mqtt.Token {
	var s string
	switch p := payload.(type) {
	case string:
		s = p
	case []byte:
		s = string(p)
	}
	if s == "" {
		delete(c.retained, topic)
	} else {
		c.retained[topic] = s
	}
	return doneToken{}
}

func TestMacroDeletedWhileDisconnected(t *testing.T) {
	client := &retainingClient{connected: true, retained: map[string]string{}}
	m := &MQTTManager{client: client, deviceID: "dev", baseTopic: "jetkvm/dev"}
	m.connected.Store(true)
	useConfig(t, &Config{
		MqttConfig:     &MQTTConfig{EnableKeyboardMacros: true},
		KeyboardMacros: []KeyboardMacro{{ID: "a", Name: "A"}, {ID: "b", Name: "B"}},
	})
	button := func(id string) string { return "homeassistant/button/jetkvm_dev/macro_" + id + "/config" }

	m.publishMacroDiscovery(&haDevice{}, "jetkvm/dev/status", "")
	if _, ok := client.retained[button("a")]; !ok {
		t.Fatal("button for macro a was not published")
	}

	client.connected = false
	config.KeyboardMacros = config.KeyboardMacros[1:]
	m.removeMacroDiscovery([]string{"a"})

	client.connected = true
	m.publishMacroDiscovery(&haDevice{}, "jetkvm/dev/status", "")
	if _, ok := client.retained[button("a")]; ok {
		t.Fatal("button of the macro deleted while disconnected survived the reconnect")
	}
	if _, ok := client.retained[button("b")]; !ok {
		t.Fatal("button of the surviving macro is missing")
	}
}

func TestMacroButtonsFollowKeyboardMacroSwitch(t *testing.T) {
	client := &retainingClient{connected: true, retained: map[string]string{}}
	m := &MQTTManager{client: client, deviceID: "dev", baseTopic: "jetkvm/dev"}
	m.connected.Store(true)
	useConfig(t, &Config{
		MqttConfig:     &MQTTConfig{EnableActions: true},
		KeyboardMacros: []KeyboardMacro{{ID: "a", Name: "A"}},
	})
	button := "homeassistant/button/jetkvm_dev/macro_a/config"

	m.publishMacroDiscovery(&haDevice{}, "jetkvm/dev/status", "")
	if _, ok := client.retained[button]; ok {
		t.Fatal("macro button published with the keyboard macro switch off")
	}

	config.MqttConfig.EnableKeyboardMacros = true
	m.publishMacroDiscovery(&haDevice{}, "jetkvm/dev/status", "")
	if _, ok := client.retained[button]; !ok {
		t.Fatal("macro button missing with the keyboard macro switch on")
	}

	config.MqttConfig.EnableKeyboardMacros = false
	m.publishMacroDiscovery(&haDevice{}, "jetkvm/dev/status", "")
	if _, ok := client.retained[button]; ok {
		t.Fatal("macro button survived turning the keyboard macro switch off")
	}
}
