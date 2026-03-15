package kvm

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const tailscaleCommandTimeout = 10 * time.Second

// TailscaleStatus represents the current state of Tailscale on the device.
type TailscaleStatus struct {
	Installed    bool           `json:"installed"`
	Running      bool           `json:"running"`
	BackendState string         `json:"backendState,omitempty"`
	AuthURL      string         `json:"authURL,omitempty"`
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

// execTailscaleStatus runs `tailscale status --json` and returns the raw output.
// This is a package-level var to allow test substitution.
var execTailscaleStatus = func() ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), tailscaleCommandTimeout)
	defer cancel()

	output, err := exec.CommandContext(ctx, "tailscale", "status", "--json").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("tailscale status: %w: %s", err, strings.TrimSpace(string(output)))
	}

	return output, nil
}

// getTailscaleStatus queries the Tailscale daemon for current status.
// Returns a TailscaleStatus with Installed=false when the binary is not found.
func getTailscaleStatus() (*TailscaleStatus, error) {
	if !isTailscaleInstalled() {
		return &TailscaleStatus{Installed: false}, nil
	}

	output, err := execTailscaleStatus()
	if err != nil {
		tailscaleLogger.Warn().Err(err).Msg("failed to get tailscale status")
		return &TailscaleStatus{
			Installed: true,
			Running:   false,
		}, nil
	}

	return parseTailscaleStatus(output)
}

// parseTailscaleStatus parses the JSON output from `tailscale status --json`.
func parseTailscaleStatus(data []byte) (*TailscaleStatus, error) {
	var raw tailscaleRawStatus
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse tailscale status: %w", err)
	}

	status := &TailscaleStatus{
		Installed:    true,
		Running:      raw.BackendState == "Running",
		BackendState: raw.BackendState,
		AuthURL:      raw.AuthURL,
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
