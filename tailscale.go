package kvm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"strings"
	"time"
)

const tailscaleCommandTimeout = 10 * time.Second

const tailscaleDefaultControlURL = "https://controlplane.tailscale.com"

// TailscaleStatus represents the current state of Tailscale on the device.
type TailscaleStatus struct {
	Installed    bool           `json:"installed"`
	Running      bool           `json:"running"`
	BackendState string         `json:"backendState,omitempty"`
	AuthURL      string         `json:"authURL,omitempty"`
	ControlURL   string         `json:"controlURL,omitempty"`
	Self         *TailscalePeer `json:"self,omitempty"`
	Health       []string       `json:"health,omitempty"`
}

// TailscalePeer represents a Tailscale peer (including self).
type TailscalePeer struct {
	HostName     string   `json:"hostName"`
	DNSName      string   `json:"dnsName"`
	TailscaleIPs []string `json:"tailscaleIPs"`
	Online       bool     `json:"online"`
	OS           string   `json:"os"`
}

// tailscaleRawStatus represents the subset of fields we parse from `tailscale status --json`.
type tailscaleRawStatus struct {
	BackendState string            `json:"BackendState"`
	AuthURL      string            `json:"AuthURL"`
	Self         *tailscaleRawPeer `json:"Self"`
	Health       []string          `json:"Health"`
}

type tailscaleRawPeer struct {
	HostName     string   `json:"HostName"`
	DNSName      string   `json:"DNSName"`
	TailscaleIPs []string `json:"TailscaleIPs"`
	Online       bool     `json:"Online"`
	OS           string   `json:"OS"`
}

// isTailscaleInstalled checks if the tailscale binary is available on the system.
func isTailscaleInstalled() bool {
	_, err := exec.LookPath("tailscale")
	return err == nil
}

// These package-level vars allow deterministic unit tests.
var (
	checkTailscaleInstalled = isTailscaleInstalled
	saveTailscaleConfig     = SaveConfig
	execTailscaleCommand    = func(args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), tailscaleCommandTimeout)
		defer cancel()

		output, err := exec.CommandContext(ctx, "tailscale", args...).CombinedOutput()
		if err != nil {
			cmd := "tailscale " + strings.Join(args, " ")
			return nil, fmt.Errorf("%s: %w: %s", cmd, err, strings.TrimSpace(string(output)))
		}

		return output, nil
	}
)

// execTailscaleStatus runs `tailscale status --json` and returns the raw output.
func execTailscaleStatus() ([]byte, error) {
	return execTailscaleCommand("status", "--json")
}

func normalizeTailscaleControlURL(controlURL string) (string, error) {
	trimmed := strings.TrimSpace(controlURL)
	if trimmed == "" {
		return "", nil
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("invalid control URL: %w", err)
	}

	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return "", errors.New("control URL must start with http:// or https://")
	}
	if parsed.Host == "" {
		return "", errors.New("control URL must include a host")
	}
	if parsed.User != nil {
		return "", errors.New("control URL must not include user info")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("control URL must not include query or fragment")
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return "", errors.New("control URL path is not supported")
	}

	parsed.Path = ""
	parsed.RawPath = ""

	return strings.TrimSuffix(parsed.String(), "/"), nil
}

func effectiveTailscaleControlURL(controlURL string) string {
	if controlURL == "" {
		return tailscaleDefaultControlURL
	}
	return controlURL
}

func applyTailscaleControlURL(controlURL string) error {
	effectiveURL := effectiveTailscaleControlURL(controlURL)
	loginServerFlag := "--login-server=" + effectiveURL

	if _, err := execTailscaleCommand("set", loginServerFlag); err != nil {
		return fmt.Errorf("failed to apply login server (%s): %w", effectiveURL, err)
	}

	return nil
}

// getTailscaleStatus queries the Tailscale daemon for current status.
// Returns a TailscaleStatus with Installed=false when the binary is not found.
func getTailscaleStatus() (*TailscaleStatus, error) {
	ensureConfigLoaded()

	controlURL := config.TailscaleControlURL
	if !checkTailscaleInstalled() {
		return &TailscaleStatus{
			Installed:  false,
			ControlURL: effectiveTailscaleControlURL(controlURL),
		}, nil
	}

	output, err := execTailscaleStatus()
	if err != nil {
		tailscaleLogger.Warn().Err(err).Msg("failed to get tailscale status")
		return &TailscaleStatus{
			Installed:  true,
			Running:    false,
			ControlURL: effectiveTailscaleControlURL(controlURL),
		}, nil
	}

	return parseTailscaleStatus(output, controlURL)
}

// parseTailscaleStatus parses the JSON output from `tailscale status --json`.
func parseTailscaleStatus(data []byte, controlURL string) (*TailscaleStatus, error) {
	var raw tailscaleRawStatus
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse tailscale status: %w", err)
	}

	status := &TailscaleStatus{
		Installed:    true,
		Running:      raw.BackendState == "Running",
		BackendState: raw.BackendState,
		AuthURL:      raw.AuthURL,
		ControlURL:   effectiveTailscaleControlURL(controlURL),
		Health:       raw.Health,
	}

	if raw.Self != nil {
		status.Self = &TailscalePeer{
			HostName:     raw.Self.HostName,
			DNSName:      raw.Self.DNSName,
			TailscaleIPs: raw.Self.TailscaleIPs,
			Online:       raw.Self.Online,
			OS:           raw.Self.OS,
		}
	}

	return status, nil
}

func rpcGetTailscaleStatus() (*TailscaleStatus, error) {
	return getTailscaleStatus()
}

func rpcGetTailscaleControlURL() (string, error) {
	ensureConfigLoaded()
	return effectiveTailscaleControlURL(config.TailscaleControlURL), nil
}

func rpcSetTailscaleControlURL(controlURL string) error {
	ensureConfigLoaded()

	normalizedURL, err := normalizeTailscaleControlURL(controlURL)
	if err != nil {
		return err
	}

	previousURL := config.TailscaleControlURL
	config.TailscaleControlURL = normalizedURL

	if !checkTailscaleInstalled() {
		if err := saveTailscaleConfig(); err != nil {
			config.TailscaleControlURL = previousURL
			return fmt.Errorf("failed to save tailscale control URL: %w", err)
		}
		return nil
	}

	if err := applyTailscaleControlURL(normalizedURL); err != nil {
		config.TailscaleControlURL = previousURL
		return err
	}

	if err := saveTailscaleConfig(); err != nil {
		config.TailscaleControlURL = previousURL
		return fmt.Errorf("failed to save tailscale control URL: %w", err)
	}

	return nil
}
