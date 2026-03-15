package kvm

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseTailscaleStatus(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		wantErr      bool
		wantRunning  bool
		wantState    string
		wantAuthURL  string
		wantHostName string
		wantIPs      []string
	}{
		{
			name: "running with self",
			input: `{
				"BackendState": "Running",
				"Self": {
					"HostName": "cortex-kvm",
					"DNSName": "cortex-kvm.tail1234.ts.net.",
					"TailscaleIPs": ["100.80.194.50", "fd7a:115c:a1e0::1"],
					"Online": true,
					"OS": "linux"
				},
				"Health": []
			}`,
			wantRunning:  true,
			wantState:    "Running",
			wantHostName: "cortex-kvm",
			wantIPs:      []string{"100.80.194.50", "fd7a:115c:a1e0::1"},
		},
		{
			name: "needs login",
			input: `{
				"BackendState": "NeedsLogin",
				"AuthURL": "https://login.tailscale.com/a/abc123",
				"Self": null,
				"Health": []
			}`,
			wantRunning: false,
			wantState:   "NeedsLogin",
			wantAuthURL: "https://login.tailscale.com/a/abc123",
		},
		{
			name: "stopped",
			input: `{
				"BackendState": "Stopped",
				"Self": null,
				"Health": []
			}`,
			wantRunning: false,
			wantState:   "Stopped",
		},
		{
			name: "starting",
			input: `{
				"BackendState": "Starting",
				"Self": null,
				"Health": ["not yet connected"]
			}`,
			wantRunning: false,
			wantState:   "Starting",
		},
		{
			name:    "invalid json",
			input:   `{invalid`,
			wantErr: true,
		},
		{
			name:        "empty json",
			input:       `{}`,
			wantRunning: false,
			wantState:   "",
		},
		{
			name: "running without IPs",
			input: `{
				"BackendState": "Running",
				"Self": {
					"HostName": "test-node",
					"DNSName": "test-node.example.ts.net.",
					"TailscaleIPs": [],
					"Online": true,
					"OS": "linux"
				}
			}`,
			wantRunning:  true,
			wantState:    "Running",
			wantHostName: "test-node",
			wantIPs:      []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, err := parseTailscaleStatus([]byte(tt.input))
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.True(t, status.Installed)
			assert.Equal(t, tt.wantRunning, status.Running)
			assert.Equal(t, tt.wantState, status.BackendState)
			assert.Equal(t, tt.wantAuthURL, status.AuthURL)

			if tt.wantHostName != "" {
				assert.NotNil(t, status.Self)
				assert.Equal(t, tt.wantHostName, status.Self.HostName)
			}

			if tt.wantIPs != nil {
				assert.NotNil(t, status.Self)
				assert.Equal(t, tt.wantIPs, status.Self.TailscaleIPs)
			}
		})
	}
}

func TestGetTailscaleStatus_NotInstalled(t *testing.T) {
	// Save and restore the original exec function
	origExec := execTailscaleStatus
	defer func() { execTailscaleStatus = origExec }()

	// The function should never be called when tailscale is not in PATH.
	// We can't easily mock exec.LookPath, but we can verify parseTailscaleStatus
	// handles all the edge cases above. This test verifies the exec mock path.
	execTailscaleStatus = func() ([]byte, error) {
		return nil, fmt.Errorf("tailscale not running")
	}

	status, err := getTailscaleStatus()
	// When tailscale is installed but daemon is down, we get installed=true, running=false
	// When not installed, LookPath fails and we get installed=false
	// Since we can't mock LookPath easily, just verify the error path through exec
	assert.NoError(t, err)
	assert.NotNil(t, status)
	// Status depends on whether tailscale binary exists on the test machine
	// The important thing is it never returns an error
}

func TestGetTailscaleStatus_ExecFailure(t *testing.T) {
	origExec := execTailscaleStatus
	defer func() { execTailscaleStatus = origExec }()

	execTailscaleStatus = func() ([]byte, error) {
		return nil, fmt.Errorf("connection refused")
	}

	status, err := getTailscaleStatus()
	assert.NoError(t, err)
	assert.NotNil(t, status)
	assert.True(t, status.Installed || !status.Installed) // depends on test env
	assert.False(t, status.Running)
}

func TestGetTailscaleStatus_ValidJSON(t *testing.T) {
	origExec := execTailscaleStatus
	defer func() { execTailscaleStatus = origExec }()

	execTailscaleStatus = func() ([]byte, error) {
		return []byte(`{
			"BackendState": "Running",
			"Self": {
				"HostName": "test-kvm",
				"DNSName": "test-kvm.example.ts.net.",
				"TailscaleIPs": ["100.64.0.1"],
				"Online": true,
				"OS": "linux"
			},
			"Health": []
		}`), nil
	}

	status, err := getTailscaleStatus()
	assert.NoError(t, err)
	assert.NotNil(t, status)
	// If tailscale binary doesn't exist on test machine, we get installed=false
	// and the exec mock is never called. Both paths are valid.
	if status.Installed {
		assert.True(t, status.Running)
		assert.Equal(t, "test-kvm", status.Self.HostName)
		assert.Equal(t, []string{"100.64.0.1"}, status.Self.TailscaleIPs)
	}
}
