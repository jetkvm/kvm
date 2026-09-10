// Package netbird mirrors the shape of JetKVM's own internal/tailscale
// package: detect whether the netbird CLI is installed, shell out to it
// for status and control, and expose a small typed Status for the UI.
// No jetkvm-specific imports - buildable and testable standalone, meant
// to be copied into jetkvm/kvm's internal/netbird.
package netbird

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

const DefaultManagementURL = "https://api.netbird.io:443"

type Status struct {
	Installed       bool   `json:"installed"`
	Running         bool   `json:"running"`
	DaemonStatus    string `json:"daemonStatus,omitempty"`
	ManagementURL   string `json:"managementURL,omitempty"`
	ManagementError string `json:"managementError,omitempty"`
	FQDN            string `json:"fqdn,omitempty"`
	IP              string `json:"netbirdIp,omitempty"`
	IPv6            string `json:"netbirdIpv6,omitempty"`
	PeersTotal      int    `json:"peersTotal,omitempty"`
	PeersConnected  int    `json:"peersConnected,omitempty"`
}

// rawOverview is the subset of `netbird status --json`'s OutputOverview
// (client/status/status.go in netbirdio/netbird) this package reads.
type rawOverview struct {
	DaemonStatus    string `json:"daemonStatus"`
	ManagementState struct {
		URL       string `json:"url"`
		Connected bool   `json:"connected"`
		Error     string `json:"error"`
	} `json:"management"`
	IP    string `json:"netbirdIp"`
	IPv6  string `json:"netbirdIpv6"`
	FQDN  string `json:"fqdn"`
	Peers struct {
		Total     int `json:"total"`
		Connected int `json:"connected"`
	} `json:"peers"`
}

func isInstalled() bool {
	_, err := exec.LookPath("netbird")
	return err == nil
}

// Package-level vars for deterministic unit tests.
var (
	CheckInstalled = isInstalled
	ExecCommand    = func(args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
		defer cancel()

		output, err := exec.CommandContext(ctx, "netbird", args...).CombinedOutput()
		if err != nil {
			cmd := "netbird " + strings.Join(args, " ")
			return nil, fmt.Errorf("%s: %w: %s", cmd, err, strings.TrimSpace(string(output)))
		}

		return output, nil
	}
)

func NormalizeManagementURL(managementURL string) (string, error) {
	trimmed := strings.TrimSpace(managementURL)
	if trimmed == "" {
		return "", nil
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("invalid management URL: %w", err)
	}

	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return "", errors.New("management URL must start with http:// or https://")
	}
	if parsed.Host == "" {
		return "", errors.New("management URL must include a host")
	}
	if parsed.User != nil {
		return "", errors.New("management URL must not include user info")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("management URL must not include query or fragment")
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return "", errors.New("management URL path is not supported")
	}

	parsed.Path = ""
	parsed.RawPath = ""

	return strings.TrimSuffix(parsed.String(), "/"), nil
}

func EffectiveManagementURL(managementURL string) string {
	if managementURL == "" {
		return DefaultManagementURL
	}
	return managementURL
}

// ApplyManagementURL reconnects with the given management URL. Unlike
// Tailscale's `tailscale set --login-server=`, NetBird has no lightweight
// config-only command - changing the management URL means a full `up`.
func ApplyManagementURL(managementURL string) error {
	effectiveURL := EffectiveManagementURL(managementURL)

	if _, err := ExecCommand("up", "--management-url", effectiveURL); err != nil {
		return fmt.Errorf("failed to apply management URL (%s): %w", effectiveURL, err)
	}

	return nil
}

// SetManagementURL validates, normalizes, and applies a management URL
// via the netbird CLI (when installed). It returns the normalized URL;
// the caller is responsible for persisting it if needed.
func SetManagementURL(managementURL string) (string, error) {
	normalized, err := NormalizeManagementURL(managementURL)
	if err != nil {
		return "", err
	}

	if CheckInstalled() {
		if err := ApplyManagementURL(normalized); err != nil {
			return "", err
		}
	}

	return normalized, nil
}

func ParseStatus(data []byte) (*Status, error) {
	var raw rawOverview
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse netbird status: %w", err)
	}

	status := &Status{
		Installed:      true,
		Running:        raw.DaemonStatus == "Connected",
		DaemonStatus:   raw.DaemonStatus,
		ManagementURL:  EffectiveManagementURL(raw.ManagementState.URL),
		FQDN:           raw.FQDN,
		IP:             raw.IP,
		IPv6:           raw.IPv6,
		PeersTotal:     raw.Peers.Total,
		PeersConnected: raw.Peers.Connected,
	}

	if raw.ManagementState.Error != "" {
		status.ManagementError = raw.ManagementState.Error
	}

	return status, nil
}

// GetStatus queries the NetBird daemon for current status. Returns a
// Status with Installed=false when the binary is not found.
func GetStatus(warn func(err error)) (*Status, error) {
	if !CheckInstalled() {
		return &Status{
			Installed:     false,
			ManagementURL: DefaultManagementURL,
		}, nil
	}

	output, err := ExecCommand("status", "--json")
	if err != nil {
		if warn != nil {
			warn(err)
		}
		return &Status{
			Installed:     true,
			Running:       false,
			ManagementURL: DefaultManagementURL,
		}, nil
	}

	return ParseStatus(output)
}
