package kvm

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseTailscaleStatus(t *testing.T) {
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
			wantControl: tailscaleDefaultControlURL,
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
			wantControl: tailscaleDefaultControlURL,
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
			wantControl:  tailscaleDefaultControlURL,
			wantHostName: "test-node",
			wantIPs:      []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, err := parseTailscaleStatus([]byte(tt.input), tt.controlURL)
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

func TestNormalizeTailscaleControlURL(t *testing.T) {
	t.Run("empty means default", func(t *testing.T) {
		got, err := normalizeTailscaleControlURL("")
		require.NoError(t, err)
		assert.Equal(t, "", got)
	})

	t.Run("valid URL trimmed", func(t *testing.T) {
		got, err := normalizeTailscaleControlURL(" https://headscale.example.com/ ")
		require.NoError(t, err)
		assert.Equal(t, "https://headscale.example.com", got)
	})

	t.Run("rejects path", func(t *testing.T) {
		_, err := normalizeTailscaleControlURL("https://headscale.example.com/api")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "path")
	})

	t.Run("rejects invalid scheme", func(t *testing.T) {
		_, err := normalizeTailscaleControlURL("ftp://headscale.example.com")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "http:// or https://")
	})
}

func TestGetTailscaleStatus_NotInstalled(t *testing.T) {
	origCheck := checkTailscaleInstalled
	origExec := execTailscaleCommand
	origConfig := config
	defer func() {
		checkTailscaleInstalled = origCheck
		execTailscaleCommand = origExec
		config = origConfig
	}()

	config = &Config{TailscaleControlURL: "https://headscale.example.com"}
	checkTailscaleInstalled = func() bool { return false }
	execTailscaleCommand = func(_ ...string) ([]byte, error) {
		return nil, fmt.Errorf("should not be called")
	}

	status, err := getTailscaleStatus()
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.False(t, status.Installed)
	assert.False(t, status.Running)
	assert.Equal(t, "https://headscale.example.com", status.ControlURL)
}

func TestGetTailscaleStatus_ExecFailure(t *testing.T) {
	origCheck := checkTailscaleInstalled
	origExec := execTailscaleCommand
	origConfig := config
	defer func() {
		checkTailscaleInstalled = origCheck
		execTailscaleCommand = origExec
		config = origConfig
	}()

	config = &Config{}
	checkTailscaleInstalled = func() bool { return true }
	execTailscaleCommand = func(args ...string) ([]byte, error) {
		require.Equal(t, []string{"status", "--json"}, args)
		return nil, fmt.Errorf("connection refused")
	}

	status, err := getTailscaleStatus()
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.True(t, status.Installed)
	assert.False(t, status.Running)
	assert.Equal(t, tailscaleDefaultControlURL, status.ControlURL)
}

func TestGetTailscaleStatus_ValidJSON(t *testing.T) {
	origCheck := checkTailscaleInstalled
	origExec := execTailscaleCommand
	origConfig := config
	defer func() {
		checkTailscaleInstalled = origCheck
		execTailscaleCommand = origExec
		config = origConfig
	}()

	config = &Config{TailscaleControlURL: "https://headscale.example.com"}
	checkTailscaleInstalled = func() bool { return true }
	execTailscaleCommand = func(args ...string) ([]byte, error) {
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

	status, err := getTailscaleStatus()
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.True(t, status.Installed)
	assert.True(t, status.Running)
	assert.Equal(t, "https://headscale.example.com", status.ControlURL)
	require.NotNil(t, status.Self)
	assert.Equal(t, "test-kvm", status.Self.HostName)
	assert.Equal(t, []string{"100.64.0.1"}, status.Self.TailscaleIPs)
}

func TestApplyTailscaleControlURL_SetOnly(t *testing.T) {
	origExec := execTailscaleCommand
	defer func() { execTailscaleCommand = origExec }()

	var commands [][]string
	execTailscaleCommand = func(args ...string) ([]byte, error) {
		commands = append(commands, append([]string{}, args...))
		return []byte("ok"), nil
	}

	err := applyTailscaleControlURL("https://headscale.example.com")
	require.NoError(t, err)
	require.Len(t, commands, 1)
	assert.Equal(t, []string{"set", "--login-server=https://headscale.example.com"}, commands[0])
}

func TestApplyTailscaleControlURL_SetFailureReturnedWithoutFallback(t *testing.T) {
	origExec := execTailscaleCommand
	defer func() { execTailscaleCommand = origExec }()

	var commands [][]string
	execTailscaleCommand = func(args ...string) ([]byte, error) {
		commands = append(commands, append([]string{}, args...))
		return nil, fmt.Errorf("unknown command")
	}

	err := applyTailscaleControlURL("https://headscale.example.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to apply login server")
	require.Len(t, commands, 1)
	assert.Equal(t, []string{"set", "--login-server=https://headscale.example.com"}, commands[0])
}

func TestRPCSetTailscaleControlURL_SaveAndApply(t *testing.T) {
	origCheck := checkTailscaleInstalled
	origExec := execTailscaleCommand
	origSave := saveTailscaleConfig
	origConfig := config
	defer func() {
		checkTailscaleInstalled = origCheck
		execTailscaleCommand = origExec
		saveTailscaleConfig = origSave
		config = origConfig
	}()

	config = &Config{}
	checkTailscaleInstalled = func() bool { return true }
	var callOrder []string
	saveTailscaleConfig = func() error {
		callOrder = append(callOrder, "save")
		return nil
	}

	var commands [][]string
	execTailscaleCommand = func(args ...string) ([]byte, error) {
		callOrder = append(callOrder, "apply")
		commands = append(commands, append([]string{}, args...))
		return []byte("ok"), nil
	}

	err := rpcSetTailscaleControlURL("https://headscale.example.com/")
	require.NoError(t, err)
	assert.Equal(t, []string{"apply", "save"}, callOrder)
	assert.Equal(t, "https://headscale.example.com", config.TailscaleControlURL)
	require.Len(t, commands, 1)
	assert.Equal(t, []string{"set", "--login-server=https://headscale.example.com"}, commands[0])
}

func TestRPCSetTailscaleControlURL_ApplyFailureDoesNotSaveOrPersistConfig(t *testing.T) {
	origCheck := checkTailscaleInstalled
	origExec := execTailscaleCommand
	origSave := saveTailscaleConfig
	origConfig := config
	defer func() {
		checkTailscaleInstalled = origCheck
		execTailscaleCommand = origExec
		saveTailscaleConfig = origSave
		config = origConfig
	}()

	config = &Config{TailscaleControlURL: "https://previous.example.com"}
	checkTailscaleInstalled = func() bool { return true }
	saveTailscaleConfig = func() error {
		t.Fatal("save should not be called when apply fails")
		return nil
	}
	execTailscaleCommand = func(args ...string) ([]byte, error) {
		require.Equal(t, []string{"set", "--login-server=https://headscale.example.com"}, args)
		return nil, fmt.Errorf("apply failed")
	}

	err := rpcSetTailscaleControlURL("https://headscale.example.com")
	require.Error(t, err)
	assert.Equal(t, "https://previous.example.com", config.TailscaleControlURL)
}

func TestRPCSetTailscaleControlURL_NotInstalledSkipsApply(t *testing.T) {
	origCheck := checkTailscaleInstalled
	origExec := execTailscaleCommand
	origSave := saveTailscaleConfig
	origConfig := config
	defer func() {
		checkTailscaleInstalled = origCheck
		execTailscaleCommand = origExec
		saveTailscaleConfig = origSave
		config = origConfig
	}()

	config = &Config{}
	checkTailscaleInstalled = func() bool { return false }
	saveTailscaleConfig = func() error { return nil }
	execTailscaleCommand = func(_ ...string) ([]byte, error) {
		return nil, fmt.Errorf("should not be called")
	}

	err := rpcSetTailscaleControlURL("")
	require.NoError(t, err)
	assert.Equal(t, "", config.TailscaleControlURL)
}
