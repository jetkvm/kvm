package tailscale

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseStatus(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		controlURL   string
		wantErr      bool
		wantRunning  bool
		wantState    string
		wantAuthURL  string
		wantControl  string
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
			controlURL:   "https://headscale.example.com",
			wantRunning:  true,
			wantState:    "Running",
			wantControl:  "https://headscale.example.com",
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
			controlURL:  "",
			wantRunning: false,
			wantState:   "NeedsLogin",
			wantAuthURL: "https://login.tailscale.com/a/abc123",
			wantControl: DefaultControlURL,
		},
		{
			name: "stopped",
			input: `{
				"BackendState": "Stopped",
				"Self": null,
				"Health": []
			}`,
			controlURL:  "https://headscale.example.com/",
			wantRunning: false,
			wantState:   "Stopped",
			wantControl: "https://headscale.example.com/",
		},
		{
			name: "starting",
			input: `{
				"BackendState": "Starting",
				"Self": null,
				"Health": ["not yet connected"]
			}`,
			controlURL:  "",
			wantRunning: false,
			wantState:   "Starting",
			wantControl: DefaultControlURL,
		},
		{
			name:       "invalid json",
			input:      `{invalid`,
			controlURL: "",
			wantErr:    true,
		},
		{
			name:        "empty json",
			input:       `{}`,
			controlURL:  "https://headscale.example.com",
			wantRunning: false,
			wantState:   "",
			wantControl: "https://headscale.example.com",
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
			controlURL:   "",
			wantRunning:  true,
			wantState:    "Running",
			wantControl:  DefaultControlURL,
			wantHostName: "test-node",
			wantIPs:      []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, err := ParseStatus([]byte(tt.input), tt.controlURL)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.True(t, status.Installed)
			assert.Equal(t, tt.wantRunning, status.Running)
			assert.Equal(t, tt.wantState, status.BackendState)
			assert.Equal(t, tt.wantAuthURL, status.AuthURL)
			assert.Equal(t, tt.wantControl, status.ControlURL)

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

func TestNormalizeControlURL(t *testing.T) {
	t.Run("empty means default", func(t *testing.T) {
		got, err := NormalizeControlURL("")
		require.NoError(t, err)
		assert.Equal(t, "", got)
	})

	t.Run("valid URL trimmed", func(t *testing.T) {
		got, err := NormalizeControlURL(" https://headscale.example.com/ ")
		require.NoError(t, err)
		assert.Equal(t, "https://headscale.example.com", got)
	})

	t.Run("rejects path", func(t *testing.T) {
		_, err := NormalizeControlURL("https://headscale.example.com/api")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "path")
	})

	t.Run("rejects invalid scheme", func(t *testing.T) {
		_, err := NormalizeControlURL("ftp://headscale.example.com")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "http:// or https://")
	})
}

func TestGetStatus_NotInstalled(t *testing.T) {
	origCheck := CheckInstalled
	origExec := ExecCommand
	defer func() {
		CheckInstalled = origCheck
		ExecCommand = origExec
	}()

	CheckInstalled = func() bool { return false }
	ExecCommand = func(_ ...string) ([]byte, error) {
		return nil, fmt.Errorf("should not be called")
	}

	status, err := GetStatus("https://headscale.example.com", nil)
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.False(t, status.Installed)
	assert.False(t, status.Running)
	assert.Equal(t, "https://headscale.example.com", status.ControlURL)
}

func TestGetStatus_ExecFailure(t *testing.T) {
	origCheck := CheckInstalled
	origExec := ExecCommand
	defer func() {
		CheckInstalled = origCheck
		ExecCommand = origExec
	}()

	CheckInstalled = func() bool { return true }
	ExecCommand = func(args ...string) ([]byte, error) {
		require.Equal(t, []string{"status", "--json"}, args)
		return nil, fmt.Errorf("connection refused")
	}

	status, err := GetStatus("", nil)
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.True(t, status.Installed)
	assert.False(t, status.Running)
	assert.Equal(t, DefaultControlURL, status.ControlURL)
}

func TestGetStatus_ValidJSON(t *testing.T) {
	origCheck := CheckInstalled
	origExec := ExecCommand
	defer func() {
		CheckInstalled = origCheck
		ExecCommand = origExec
	}()

	CheckInstalled = func() bool { return true }
	ExecCommand = func(args ...string) ([]byte, error) {
		require.Equal(t, []string{"status", "--json"}, args)
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

	status, err := GetStatus("https://headscale.example.com", nil)
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.True(t, status.Installed)
	assert.True(t, status.Running)
	assert.Equal(t, "https://headscale.example.com", status.ControlURL)
	require.NotNil(t, status.Self)
	assert.Equal(t, "test-kvm", status.Self.HostName)
	assert.Equal(t, []string{"100.64.0.1"}, status.Self.TailscaleIPs)
}

func TestApplyControlURL_SetOnly(t *testing.T) {
	origExec := ExecCommand
	defer func() { ExecCommand = origExec }()

	var commands [][]string
	ExecCommand = func(args ...string) ([]byte, error) {
		commands = append(commands, append([]string{}, args...))
		return []byte("ok"), nil
	}

	err := ApplyControlURL("https://headscale.example.com")
	require.NoError(t, err)
	require.Len(t, commands, 1)
	assert.Equal(t, []string{"set", "--login-server=https://headscale.example.com"}, commands[0])
}

func TestApplyControlURL_SetFailure(t *testing.T) {
	origExec := ExecCommand
	defer func() { ExecCommand = origExec }()

	var commands [][]string
	ExecCommand = func(args ...string) ([]byte, error) {
		commands = append(commands, append([]string{}, args...))
		return nil, fmt.Errorf("unknown command")
	}

	err := ApplyControlURL("https://headscale.example.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to apply login server")
	require.Len(t, commands, 1)
	assert.Equal(t, []string{"set", "--login-server=https://headscale.example.com"}, commands[0])
}

func TestSetControlURL_InstalledAppliesAndReturnsNormalized(t *testing.T) {
	origCheck := CheckInstalled
	origExec := ExecCommand
	defer func() {
		CheckInstalled = origCheck
		ExecCommand = origExec
	}()

	CheckInstalled = func() bool { return true }
	var commands [][]string
	ExecCommand = func(args ...string) ([]byte, error) {
		commands = append(commands, append([]string{}, args...))
		return []byte("ok"), nil
	}

	normalized, err := SetControlURL("https://headscale.example.com/")
	require.NoError(t, err)
	assert.Equal(t, "https://headscale.example.com", normalized)
	require.Len(t, commands, 1)
	assert.Equal(t, []string{"set", "--login-server=https://headscale.example.com"}, commands[0])
}

func TestSetControlURL_ApplyFailureReturnsError(t *testing.T) {
	origCheck := CheckInstalled
	origExec := ExecCommand
	defer func() {
		CheckInstalled = origCheck
		ExecCommand = origExec
	}()

	CheckInstalled = func() bool { return true }
	ExecCommand = func(args ...string) ([]byte, error) {
		return nil, fmt.Errorf("apply failed")
	}

	_, err := SetControlURL("https://headscale.example.com")
	require.Error(t, err)
}

func TestSetControlURL_NotInstalledSkipsApply(t *testing.T) {
	origCheck := CheckInstalled
	origExec := ExecCommand
	defer func() {
		CheckInstalled = origCheck
		ExecCommand = origExec
	}()

	CheckInstalled = func() bool { return false }
	ExecCommand = func(_ ...string) ([]byte, error) {
		t.Fatal("exec should not be called when not installed")
		return nil, nil
	}

	normalized, err := SetControlURL("")
	require.NoError(t, err)
	assert.Equal(t, "", normalized)
}
