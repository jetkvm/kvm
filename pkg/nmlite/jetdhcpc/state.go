package jetdhcpc

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jetkvm/kvm/internal/network/types"
)

const (
	// DefaultStateDir is the default state directory
	DefaultStateDir = "/var/run/"
	// DHCPStateFile is the name of the DHCP state file
	DHCPStateFile = "jetkvm_dhcp_state.json"
)

// DHCPState represents the persistent state of DHCP clients
type DHCPState struct {
	InterfaceStates map[string]*InterfaceDHCPState `json:"interface_states"`
	LastUpdated     time.Time                      `json:"last_updated"`
	Version         string                         `json:"version"`
}

// InterfaceDHCPState represents the DHCP state for a specific interface
type InterfaceDHCPState struct {
	InterfaceName string               `json:"interface_name"`
	IPv4Enabled   bool                 `json:"ipv4_enabled"`
	IPv6Enabled   bool                 `json:"ipv6_enabled"`
	IPv4Lease     *Lease               `json:"ipv4_lease,omitempty"`
	IPv6Lease     *Lease               `json:"ipv6_lease,omitempty"`
	LastRenewal   time.Time            `json:"last_renewal"`
	Config        *types.NetworkConfig `json:"config,omitempty"`
}

// SaveState saves the current DHCP state to disk
func (c *Client) SaveState(state *DHCPState) error {
	if state == nil {
		return fmt.Errorf("state cannot be nil")
	}

	// Return error if state directory doesn't exist
	if _, err := os.Stat(c.stateDir); os.IsNotExist(err) {
		return fmt.Errorf("state directory does not exist: %w", err)
	}

	// Update timestamp
	state.LastUpdated = time.Now()
	state.Version = "1.0"

	// Serialize state
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	// Write to temporary file first, then rename to ensure atomic operation
	tmpFile, err := os.CreateTemp(c.stateDir, DHCPStateFile)
	if err != nil {
		return fmt.Errorf("failed to create temporary file: %w", err)
	}
	defer tmpFile.Close()

	if err := os.WriteFile(tmpFile.Name(), data, 0644); err != nil {
		return fmt.Errorf("failed to write state file: %w", err)
	}

	stateFile := filepath.Join(c.stateDir, DHCPStateFile)
	if err := os.Rename(tmpFile.Name(), stateFile); err != nil {
		os.Remove(tmpFile.Name())
		return fmt.Errorf("failed to rename state file: %w", err)
	}

	c.l.Debug().Str("file", stateFile).Msg("DHCP state saved")
	return nil
}

// LoadState loads the DHCP state from disk
func (c *Client) LoadState() (*DHCPState, error) {
	stateFile := filepath.Join(c.stateDir, DHCPStateFile)

	// Check if state file exists
	if _, err := os.Stat(stateFile); os.IsNotExist(err) {
		c.l.Debug().Msg("No existing DHCP state file found")
		return &DHCPState{
			InterfaceStates: make(map[string]*InterfaceDHCPState),
			LastUpdated:     time.Now(),
			Version:         "1.0",
		}, nil
	}

	// Read state file
	data, err := os.ReadFile(stateFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read state file: %w", err)
	}

	// Deserialize state
	var state DHCPState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to unmarshal state: %w", err)
	}

	// Initialize interface states map if nil
	if state.InterfaceStates == nil {
		state.InterfaceStates = make(map[string]*InterfaceDHCPState)
	}

	c.l.Debug().Str("file", stateFile).Msg("DHCP state loaded")
	return &state, nil
}

// UpdateInterfaceState updates the state for a specific interface
func (c *Client) UpdateInterfaceState(ifaceName string, state *InterfaceDHCPState) error {
	// Load current state
	currentState, err := c.LoadState()
	if err != nil {
		return fmt.Errorf("failed to load current state: %w", err)
	}

	// Update interface state
	currentState.InterfaceStates[ifaceName] = state

	// Save updated state
	return c.SaveState(currentState)
}

// GetInterfaceState gets the state for a specific interface
func (c *Client) GetInterfaceState(ifaceName string) (*InterfaceDHCPState, error) {
	state, err := c.LoadState()
	if err != nil {
		return nil, fmt.Errorf("failed to load state: %w", err)
	}

	return state.InterfaceStates[ifaceName], nil
}

// RemoveInterfaceState removes the state for a specific interface
func (c *Client) RemoveInterfaceState(ifaceName string) error {
	// Load current state
	currentState, err := c.LoadState()
	if err != nil {
		return fmt.Errorf("failed to load current state: %w", err)
	}

	// Remove interface state
	delete(currentState.InterfaceStates, ifaceName)

	// Save updated state
	return c.SaveState(currentState)
}

// IsLeaseValid checks if a DHCP lease is still valid
func (c *Client) IsLeaseValid(lease *Lease) bool {
	if lease == nil {
		return false
	}

	// Check if lease has expired
	if lease.LeaseExpiry == nil {
		return false
	}

	return time.Now().Before(*lease.LeaseExpiry)
}

// ShouldRenewLease checks if a lease should be renewed
func (c *Client) ShouldRenewLease(lease *Lease) bool {
	if !c.IsLeaseValid(lease) {
		return false
	}

	expiry := *lease.LeaseExpiry
	leaseTime := time.Now().Add(time.Duration(lease.LeaseTime) * time.Second)

	// Renew if lease expires within 50% of its lifetime
	leaseDuration := expiry.Sub(leaseTime)
	renewalTime := leaseTime.Add(leaseDuration / 2)

	return time.Now().After(renewalTime)
}

// CleanupExpiredStates removes expired states from the state file
func (c *Client) CleanupExpiredStates() error {
	state, err := c.LoadState()
	if err != nil {
		return fmt.Errorf("failed to load state: %w", err)
	}

	cleaned := false
	for ifaceName, ifaceState := range state.InterfaceStates {
		// Remove interface state if both leases are expired
		ipv4Valid := c.IsLeaseValid(ifaceState.IPv4Lease)
		ipv6Valid := c.IsLeaseValid(ifaceState.IPv6Lease)

		if !ipv4Valid && !ipv6Valid {
			delete(state.InterfaceStates, ifaceName)
			cleaned = true
			c.l.Debug().Str("interface", ifaceName).Msg("Removed expired DHCP state")
		}
	}

	if cleaned {
		return c.SaveState(state)
	}

	return nil
}

// GetStateSummary returns a summary of the current state
func (c *Client) GetStateSummary() (map[string]interface{}, error) {
	state, err := c.LoadState()
	if err != nil {
		return nil, fmt.Errorf("failed to load state: %w", err)
	}

	summary := map[string]interface{}{
		"last_updated":    state.LastUpdated,
		"version":         state.Version,
		"interface_count": len(state.InterfaceStates),
		"interfaces":      make(map[string]interface{}),
	}

	interfaces := summary["interfaces"].(map[string]interface{})
	for ifaceName, ifaceState := range state.InterfaceStates {
		interfaceInfo := map[string]interface{}{
			"ipv4_enabled": ifaceState.IPv4Enabled,
			"ipv6_enabled": ifaceState.IPv6Enabled,
			"last_renewal": ifaceState.LastRenewal,
			// "ipv4_lease_valid": c.IsLeaseValid(ifaceState.IPv4Lease.(*Lease)),
			// "ipv6_lease_valid": c.IsLeaseValid(ifaceState.IPv6Lease),
		}

		if ifaceState.IPv4Lease != nil {
			interfaceInfo["ipv4_lease_expiry"] = ifaceState.IPv4Lease.LeaseExpiry
		}
		if ifaceState.IPv6Lease != nil {
			interfaceInfo["ipv6_lease_expiry"] = ifaceState.IPv6Lease.LeaseExpiry
		}

		interfaces[ifaceName] = interfaceInfo
	}

	return summary, nil
}
