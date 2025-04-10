package kvm

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
)

// SwitchChannelCommandProtocol is the protocol used to connect to the remote KVM switch
type SwitchChannelCommandProtocol string

// SwitchChannelCommandFormat is the format of the commands (hex, base64, ascii)
type SwitchChannelCommandFormat string

const (
	SwitchChannelCommandProtocolTCP   SwitchChannelCommandProtocol = "tcp"
	SwitchChannelCommandProtocolUDP   SwitchChannelCommandProtocol = "udp"
	SwitchChannelCommandProtocolHTTP  SwitchChannelCommandProtocol = "http"
	SwitchChannelCommandProtocolHTTPs SwitchChannelCommandProtocol = "https"
)

const (
	SwitchChannelCommandFormatHEX    SwitchChannelCommandFormat = "hex"
	SwitchChannelCommandFormatBase64 SwitchChannelCommandFormat = "base64"
	SwitchChannelCommandFormatASCII  SwitchChannelCommandFormat = "ascii"
	SwitchChannelCommandFormatHTTP   SwitchChannelCommandFormat = "http-raw"
)

// SwitchChannelCommand represents a command to be sent to a remote KVM switch
type SwitchChannelCommand struct {
	Address  string                       `json:"address"`
	Protocol SwitchChannelCommandProtocol `json:"protocol"`
	Format   SwitchChannelCommandFormat   `json:"format"`
	Commands string                       `json:"commands"`
}

// SwitchChannel represents a remote KVM switch channel
type SwitchChannel struct {
	Commands []SwitchChannelCommand `json:"commands"`
	Name     string                 `json:"name"`
	Id       string                 `json:"id"`
}

// SwitchChannelConfig represents the remote KVM switch configuration
type SwitchChannelConfig struct {
	Channels []SwitchChannel `json:"channels"`
}

func remoteKvmSwitchChannelRawIP(channel *SwitchChannel, idx int, command *SwitchChannelCommand) error {
	var err error
	var payloadBytes = make([][]byte, 0)

	// Parse commands
	switch command.Format {
	case SwitchChannelCommandFormatHEX:
		// Split by comma and parse as HEX
		for _, cmd := range strings.Split(command.Commands, ",") {
			// Trim spaces, remove 0x prefix and parse as HEX
			commandText := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(cmd), "0x"))
			b, err := hex.DecodeString(commandText)
			if err != nil {
				return fmt.Errorf("invalid command provided for command #%d: %w", idx, err)
			}
			payloadBytes = append(payloadBytes, b)
		}
		break
	case SwitchChannelCommandFormatBase64:
		// Split by comma and parse as Base64
		for _, cmd := range strings.Split(command.Commands, ",") {
			// Parse Base64
			b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(cmd))
			if err != nil {
				return fmt.Errorf("invalid command provided for command #%d: %w", idx, err)
			}
			payloadBytes = append(payloadBytes, b)
		}
		break
	case SwitchChannelCommandFormatASCII:
		// Split by newline and parse as ASCII
		for _, cmd := range strings.Split(command.Commands, "\n") {
			// Parse ASCII
			b := []byte(strings.TrimSpace(cmd))
			payloadBytes = append(payloadBytes, b)
		}
		break
	default:
		return fmt.Errorf("invalid format provided for %s command #%d: %s", command.Protocol, idx, command.Format)
	}

	// Connect to the address
	var conn net.Conn
	switch command.Protocol {
	case SwitchChannelCommandProtocolTCP:
		conn, err = net.Dial("tcp", command.Address)
		break
	case SwitchChannelCommandProtocolUDP:
		conn, err = net.Dial("udp", command.Address)
		break
	default:
		return fmt.Errorf("invalid protocol provided for command #%d: %s", idx, command.Protocol)
	}

	if err != nil {
		return fmt.Errorf("failed to connect to address for command #%d: %w", idx, err)
	}
	if conn == nil {
		return fmt.Errorf("failed to connect to address for command #%d: connection is nil", idx)
	}

	defer func() {
		if conn != nil {
			_ = conn.Close()
		}
	}()

	// Send commands
	for _, b := range payloadBytes {
		_, err := conn.Write(b)
		if err != nil {
			return fmt.Errorf("failed to send command for command #%d: %w", idx, err)
		}
	}

	// Close the connection
	err = conn.Close()
	if err != nil {
		return fmt.Errorf("failed to close connection for command #%d: %w", idx, err)
	}

	return nil
}

func remoteKvmSwitchChannelHttps(channel *SwitchChannel, idx int, command *SwitchChannelCommand) error {
	var err error

	// Validation
	scheme := string(command.Protocol)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("invalid protocol provided for command #%d: %s", idx, command.Protocol)
	}

	if command.Format != SwitchChannelCommandFormatHTTP {
		return fmt.Errorf("invalid format provided for %s command #%d: %s", command.Protocol, idx, command.Format)
	}

	httpPayload := command.Commands
	// If there is no \r\n at then end - add
	if !strings.HasSuffix(httpPayload, "\r\n\r\n") {
		if strings.HasSuffix(httpPayload, "\r\n") {
			httpPayload += "\r\n"
		} else {
			httpPayload += "\r\n\r\n"
		}
	}

	// Parse request
	requestReader := bufio.NewReader(strings.NewReader(httpPayload))
	r, err := http.ReadRequest(requestReader)
	if err != nil {
		return fmt.Errorf("failed to read request for command #%d: %w", idx, err)
	}
	r.RequestURI, r.URL.Scheme, r.URL.Host = "", scheme, r.Host

	// Execute request
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		return fmt.Errorf("failed to send request for command #%d: %w", idx, err)
	}

	// Read data to buffer
	var buf bytes.Buffer
	_, err = io.Copy(&buf, resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response for command #%d: %w", idx, err)
	}

	// Close the response
	defer func() {
		if resp != nil {
			_ = resp.Body.Close()
		}
	}()

	if resp.StatusCode >= 400 || resp.StatusCode < 200 {
		if buf.Len() > 0 {
			return fmt.Errorf("failed to send request for command #%d: %s: %s", idx, resp.Status, buf.String())
		} else {
			return fmt.Errorf("failed to send request for command #%d: %s", idx, resp.Status)
		}
	}

	return nil
}

// RemoteKvmSwitchChannel sends commands to a remote KVM switch
func RemoteKvmSwitchChannel(id string) error {
	if !config.RemoteKvmEnabled {
		return fmt.Errorf("remote KVM is not enabled")
	}
	if len(config.RemoteKvmChannels) == 0 {
		return fmt.Errorf("no remote KVM channels configured")
	}
	if len(id) == 0 {
		return fmt.Errorf("no channel id provided")
	}

	var channel *SwitchChannel

	for _, c := range config.RemoteKvmChannels {
		if c.Id == id {
			channel = &c
			break
		}
	}
	if channel == nil {
		return fmt.Errorf("channel not found")
	}

	// Try to run commands
	if len(channel.Commands) == 0 {
		return fmt.Errorf("no commands found for channel %s", id)
	}

	for idx, c := range channel.Commands {
		// Initial validation
		if c.Protocol == SwitchChannelCommandProtocolTCP || c.Protocol == SwitchChannelCommandProtocolUDP {
			if c.Address == "" {
				return fmt.Errorf("no address provided for command #%d", idx)
			}

			_, _, err := net.SplitHostPort(c.Address)
			if err != nil {
				return fmt.Errorf("invalid address provided for command #%d: %w", idx, err)
			}
		}

		if c.Protocol == "" {
			return fmt.Errorf("no protocol provided for command #%d", idx)
		}
		if c.Format == "" {
			return fmt.Errorf("no format provided for command #%d", idx)
		}
		if c.Commands == "" {
			return fmt.Errorf("no commands provided for command #%d", idx)
		}

		switch {
		case c.Protocol == SwitchChannelCommandProtocolTCP || c.Protocol == SwitchChannelCommandProtocolUDP:
			return remoteKvmSwitchChannelRawIP(channel, idx, &c)
		case c.Protocol == SwitchChannelCommandProtocolHTTPs || c.Protocol == SwitchChannelCommandProtocolHTTP:
			return remoteKvmSwitchChannelHttps(channel, idx, &c)
		default:
			return fmt.Errorf("invalid protocol provided for command #%d: %s", idx, c.Protocol)
		}
	}

	return nil
}
