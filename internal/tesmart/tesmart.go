// Package tesmart implements the TESmart HDMI switch LAN control protocol.
//
// The framing is identical over RS-232 and TCP:
//
//	request frame:  AA BB 03 <cmd> <payload> EE
//	response frame: AA BB 03 11 <active-input> <varies>   (always 6 bytes)
//
// Behavior verified against a real 8x1 switch and consistent with PiKVM's
// kvmd tesmart driver:
//   - every command, including a switch, is answered with a 6-byte frame
//   - the trailing byte is NOT reliably 0xEE (0x1a observed), so it is ignored
//   - the active input in responses is 0-based (0x00 = PC1)
//   - the input in a switch command is 1-based (0x01 = PC1)
package tesmart

import (
	"fmt"
	"io"
	"net"
	"strconv"
	"time"
)

const (
	CmdSwitch     byte = 0x01
	CmdReadActive byte = 0x10
	RespActive    byte = 0x11

	FrameSize        = 6
	MaxPorts         = 16
	DefaultPortCount = 8
	DefaultTCPPort   = 5000
	// SwitchSettleDelay is how long to wait after a switch command before
	// reading back the active input; the switch applies the change
	// asynchronously (kvmd waits the same 1s).
	SwitchSettleDelay = 1 * time.Second
	// IOTimeout bounds the TCP dial and one command/response exchange.
	IOTimeout = 5 * time.Second
)

// Config holds the LAN connection details for a TESmart HDMI switch.
// An empty Host means the switch is not configured.
type Config struct {
	Host      string   `json:"host"`
	Port      int      `json:"port"`
	PortCount int      `json:"port_count"`
	Labels    []string `json:"labels"`
}

// State is the observed state of the switch, pushed to the UI via the
// "hdmiSwitchState" JSON-RPC event.
type State struct {
	ActiveInput int  `json:"activeInput"` // 0-based index into Labels, -1 = unknown
	Online      bool `json:"online"`
	PortCount   int  `json:"portCount"`
}

// BuildFrame builds a 6-byte TESmart command frame.
func BuildFrame(cmd byte, payload byte) []byte {
	return []byte{0xAA, 0xBB, 0x03, cmd, payload, 0xEE}
}

// ParseActiveInput extracts the 0-based active input from a response frame.
// Only the header and command bytes are validated; the trailing byte is
// ignored because it is not stable across switch models/firmware.
func ParseActiveInput(resp []byte) (int, error) {
	if len(resp) != FrameSize {
		return 0, fmt.Errorf("unexpected response length: %d", len(resp))
	}
	if resp[0] != 0xAA || resp[1] != 0xBB || resp[2] != 0x03 {
		return 0, fmt.Errorf("unexpected response header: % x", resp[:3])
	}
	if resp[3] != RespActive {
		return 0, fmt.Errorf("unexpected response command: 0x%02x", resp[3])
	}
	if resp[4] > 0x0F {
		return 0, fmt.Errorf("active input out of range: 0x%02x", resp[4])
	}
	return int(resp[4]), nil
}

// SendCommand sends one command to the switch and returns the 6-byte
// response. A fresh TCP connection is used per command, matching how the
// switch is driven by its other integrations (e.g. kvmd).
func SendCommand(host string, tcpPort int, cmd byte, payload byte) ([]byte, error) {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(tcpPort)), IOTimeout)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to HDMI switch: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(IOTimeout))

	if _, err := conn.Write(BuildFrame(cmd, payload)); err != nil {
		return nil, fmt.Errorf("failed to send command to HDMI switch: %w", err)
	}

	resp := make([]byte, FrameSize)
	if _, err := io.ReadFull(conn, resp); err != nil {
		return nil, fmt.Errorf("failed to read response from HDMI switch: %w", err)
	}
	return resp, nil
}

// DefaultLabels returns "PC1".."PCn" labels for n ports.
func DefaultLabels(portCount int) []string {
	labels := make([]string, portCount)
	for i := range labels {
		labels[i] = fmt.Sprintf("PC%d", i+1)
	}
	return labels
}

// DefaultConfig returns the default switch configuration.
func DefaultConfig() *Config {
	cfg := &Config{
		Port:      DefaultTCPPort,
		PortCount: DefaultPortCount,
	}
	cfg.Labels = DefaultLabels(cfg.PortCount)
	return cfg
}

// NormalizeConfig fills in defaults and keeps the label list in sync with the
// port count. Empty labels fall back to "PC<n>".
func NormalizeConfig(cfg *Config) {
	if cfg.Port <= 0 {
		cfg.Port = DefaultTCPPort
	}
	if cfg.PortCount < 1 {
		cfg.PortCount = DefaultPortCount
	}
	if cfg.PortCount > MaxPorts {
		cfg.PortCount = MaxPorts
	}
	if len(cfg.Labels) != cfg.PortCount {
		labels := make([]string, cfg.PortCount)
		copy(labels, cfg.Labels)
		cfg.Labels = labels
	}
	for i, label := range cfg.Labels {
		if label == "" {
			cfg.Labels[i] = fmt.Sprintf("PC%d", i+1)
		}
	}
}
