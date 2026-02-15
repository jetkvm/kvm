//go:build linux

package zerotier

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// ErrInvalidNetworkID is returned when a network ID is not a valid 16-character hex string.
var ErrInvalidNetworkID = errors.New("invalid network ID: must be 16 hexadecimal characters")

// CLI wraps the zerotier-one command-line interface.
// ZeroTier uses a single binary (zerotier-one) with -q flag for CLI mode.
type CLI struct {
	binaryPath string
}

func NewCLI() *CLI {
	return &CLI{binaryPath: ZeroTierOnePath}
}

// NodeInfo represents the local node information from zerotier-cli info.
type NodeInfo struct {
	Address string `json:"address"` // 10-digit node ID
	Version string `json:"version"`
	Online  bool   `json:"online"`
}

// NetworkInfo represents a joined network from zerotier-cli listnetworks.
type NetworkInfo struct {
	NetworkID        string   `json:"nwid"`
	Name             string   `json:"name"`
	MAC              string   `json:"mac"`
	Status           string   `json:"status"` // OK, ACCESS_DENIED, NOT_FOUND, PORT_ERROR, etc.
	Type             string   `json:"type"`   // PUBLIC or PRIVATE
	Device           string   `json:"dev"`
	AssignedAddrs    []string `json:"assignedAddresses"`
	AllowManaged     bool     `json:"allowManaged"`
	AllowGlobal      bool     `json:"allowGlobal"`
	AllowDefault     bool     `json:"allowDefault"`
	AllowDNS         bool     `json:"allowDNS"`
	BroadcastEnabled bool     `json:"broadcastEnabled"`
	Bridge           bool     `json:"bridge"`
}

// PeerInfo represents a peer from zerotier-cli listpeers.
type PeerInfo struct {
	Address string     `json:"address"` // 10-digit node ID
	Version string     `json:"version"`
	Role    string     `json:"role"` // LEAF or PLANET
	Latency int        `json:"latency"`
	Paths   []PeerPath `json:"paths"`
}

// PeerPath represents a network path to a peer.
type PeerPath struct {
	Address   string `json:"address"`
	LastSend  int64  `json:"lastSend"`
	LastRecv  int64  `json:"lastReceive"`
	Active    bool   `json:"active"`
	Preferred bool   `json:"preferred"`
}

// StatusResponse represents the output of zerotier-cli status.
type StatusResponse struct {
	Address string `json:"address"` // 10-digit node ID
	Version string `json:"version"`
	Online  bool   `json:"online"`
}

func (c *CLI) run(ctx context.Context, args ...string) ([]byte, error) {
	// zerotier-one uses -q flag for CLI mode and -D to specify working directory
	fullArgs := append([]string{"-q", "-D" + WorkingDirectory}, args...)
	cmd := exec.CommandContext(ctx, c.binaryPath, fullArgs...)
	cmd.Dir = WorkingDirectory

	output, err := cmd.CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("zerotier-one %s: %w (output: %s)", strings.Join(args, " "), err, string(output))
	}

	return output, nil
}

// IsValidNetworkID validates that a network ID is exactly 16 hexadecimal characters.
func IsValidNetworkID(id string) bool {
	if len(id) != 16 {
		return false
	}
	for _, c := range id {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// Join joins a ZeroTier network by its 16-character hex network ID.
func (c *CLI) Join(ctx context.Context, networkID string) error {
	if !IsValidNetworkID(networkID) {
		return ErrInvalidNetworkID
	}
	_, err := c.run(ctx, "join", networkID)
	return err
}

// Leave leaves a ZeroTier network.
func (c *CLI) Leave(ctx context.Context, networkID string) error {
	_, err := c.run(ctx, "leave", networkID)
	return err
}

// Info returns information about the local node.
func (c *CLI) Info(ctx context.Context) (*NodeInfo, error) {
	output, err := c.run(ctx, "-j", "info")
	if err != nil {
		return nil, err
	}

	var info NodeInfo
	if err := json.Unmarshal(output, &info); err != nil {
		return nil, fmt.Errorf("failed to parse info: %w", err)
	}

	return &info, nil
}

// Status returns the daemon status.
func (c *CLI) Status(ctx context.Context) (*StatusResponse, error) {
	output, err := c.run(ctx, "-j", "status")
	if err != nil {
		return nil, err
	}

	var status StatusResponse
	if err := json.Unmarshal(output, &status); err != nil {
		return nil, fmt.Errorf("failed to parse status: %w", err)
	}

	return &status, nil
}

// ListNetworks returns all joined networks.
func (c *CLI) ListNetworks(ctx context.Context) ([]NetworkInfo, error) {
	output, err := c.run(ctx, "-j", "listnetworks")
	if err != nil {
		return nil, err
	}

	var networks []NetworkInfo
	if err := json.Unmarshal(output, &networks); err != nil {
		return nil, fmt.Errorf("failed to parse networks: %w", err)
	}

	return networks, nil
}

// ListPeers returns all connected peers.
func (c *CLI) ListPeers(ctx context.Context) ([]PeerInfo, error) {
	output, err := c.run(ctx, "-j", "listpeers")
	if err != nil {
		return nil, err
	}

	var peers []PeerInfo
	if err := json.Unmarshal(output, &peers); err != nil {
		return nil, fmt.Errorf("failed to parse peers: %w", err)
	}

	return peers, nil
}

// Version returns the ZeroTier version.
func (c *CLI) Version(ctx context.Context) (string, error) {
	info, err := c.Info(ctx)
	if err != nil {
		return "", err
	}
	return info.Version, nil
}

// GetNodeID returns the local node's 10-digit ZeroTier address.
func (c *CLI) GetNodeID(ctx context.Context) (string, error) {
	// Try reading from identity.public first (works even if daemon not running)
	if data, err := os.ReadFile(IdentityPublicPath); err == nil {
		parts := strings.Split(strings.TrimSpace(string(data)), ":")
		if len(parts) > 0 && len(parts[0]) == 10 {
			return parts[0], nil
		}
	}

	// Fall back to CLI
	info, err := c.Info(ctx)
	if err != nil {
		return "", err
	}
	return info.Address, nil
}

// GetAssignedIPs returns the IPs assigned to this node across all joined networks.
func (c *CLI) GetAssignedIPs(ctx context.Context) ([]string, error) {
	networks, err := c.ListNetworks(ctx)
	if err != nil {
		return nil, err
	}

	var ips []string
	for _, net := range networks {
		ips = append(ips, net.AssignedAddrs...)
	}
	return ips, nil
}

// GetPrimaryIP returns the first assigned IP, or empty string if none.
func (c *CLI) GetPrimaryIP(ctx context.Context) (string, error) {
	ips, err := c.GetAssignedIPs(ctx)
	if err != nil {
		return "", err
	}
	if len(ips) > 0 {
		// Strip CIDR notation if present
		ip := ips[0]
		if idx := strings.Index(ip, "/"); idx != -1 {
			ip = ip[:idx]
		}
		return ip, nil
	}
	return "", nil
}
