package tailscale

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

const commandTimeout = 10 * time.Second

const DefaultControlURL = "https://controlplane.tailscale.com"

type Status struct {
	Installed    bool     `json:"installed"`
	Running      bool     `json:"running"`
	BackendState string   `json:"backendState,omitempty"`
	AuthURL      string   `json:"authURL,omitempty"`
	ControlURL   string   `json:"controlURL,omitempty"`
	Self         *Peer    `json:"self,omitempty"`
	Health       []string `json:"health,omitempty"`
}

type Peer struct {
	HostName     string   `json:"hostName"`
	DNSName      string   `json:"dnsName"`
	TailscaleIPs []string `json:"tailscaleIPs"`
	Online       bool     `json:"online"`
	OS           string   `json:"os"`
}

type rawStatus struct {
	BackendState string   `json:"BackendState"`
	AuthURL      string   `json:"AuthURL"`
	Self         *rawPeer `json:"Self"`
	Health       []string `json:"Health"`
}

type rawPeer struct {
	HostName     string   `json:"HostName"`
	DNSName      string   `json:"DNSName"`
	TailscaleIPs []string `json:"TailscaleIPs"`
	Online       bool     `json:"Online"`
	OS           string   `json:"OS"`
}

func isInstalled() bool {
	_, err := exec.LookPath("tailscale")
	return err == nil
}

// Package-level vars for deterministic unit tests.
var (
	CheckInstalled = isInstalled
	ExecCommand    = func(args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
		defer cancel()

		output, err := exec.CommandContext(ctx, "tailscale", args...).CombinedOutput()
		if err != nil {
			cmd := "tailscale " + strings.Join(args, " ")
			return nil, fmt.Errorf("%s: %w: %s", cmd, err, strings.TrimSpace(string(output)))
		}

		return output, nil
	}
)

func NormalizeControlURL(controlURL string) (string, error) {
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

func EffectiveControlURL(controlURL string) string {
	if controlURL == "" {
		return DefaultControlURL
	}
	return controlURL
}

func ApplyControlURL(controlURL string) error {
	effectiveURL := EffectiveControlURL(controlURL)
	loginServerFlag := "--login-server=" + effectiveURL

	if _, err := ExecCommand("set", loginServerFlag); err != nil {
		return fmt.Errorf("failed to apply login server (%s): %w", effectiveURL, err)
	}

	return nil
}

// SetControlURL validates, normalizes, and applies a control URL via the
// tailscale CLI (when installed). It returns the normalized URL; the caller
// is responsible for persisting it to config.
func SetControlURL(controlURL string) (string, error) {
	normalized, err := NormalizeControlURL(controlURL)
	if err != nil {
		return "", err
	}

	if CheckInstalled() {
		if err := ApplyControlURL(normalized); err != nil {
			return "", err
		}
	}

	return normalized, nil
}

func ParseStatus(data []byte, controlURL string) (*Status, error) {
	var raw rawStatus
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse tailscale status: %w", err)
	}

	status := &Status{
		Installed:    true,
		Running:      raw.BackendState == "Running",
		BackendState: raw.BackendState,
		AuthURL:      raw.AuthURL,
		ControlURL:   EffectiveControlURL(controlURL),
		Health:       raw.Health,
	}

	if raw.Self != nil {
		status.Self = &Peer{
			HostName:     raw.Self.HostName,
			DNSName:      raw.Self.DNSName,
			TailscaleIPs: raw.Self.TailscaleIPs,
			Online:       raw.Self.Online,
			OS:           raw.Self.OS,
		}
	}

	return status, nil
}

// GetStatus queries the Tailscale daemon for current status.
// Returns a Status with Installed=false when the binary is not found.
// The warn function is called when exec fails but tailscale is installed.
func GetStatus(controlURL string, warn func(err error)) (*Status, error) {
	if !CheckInstalled() {
		return &Status{
			Installed:  false,
			ControlURL: EffectiveControlURL(controlURL),
		}, nil
	}

	output, err := ExecCommand("status", "--json")
	if err != nil {
		if warn != nil {
			warn(err)
		}
		return &Status{
			Installed:  true,
			Running:    false,
			ControlURL: EffectiveControlURL(controlURL),
		}, nil
	}

	return ParseStatus(output, controlURL)
}
