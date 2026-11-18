package kvm

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jetkvm/kvm/internal/tailscale"
)

func withScriptInstalledTailscale(t *testing.T, installed bool, statusJSON string) {
	t.Helper()
	previousConfig, previousPath := config, configPath
	previousCheck, previousExec := tailscale.CheckInstalled, tailscale.ExecCommand
	t.Cleanup(func() {
		config, configPath = previousConfig, previousPath
		tailscale.CheckInstalled, tailscale.ExecCommand = previousCheck, previousExec
	})

	config = &Config{}
	configPath = filepath.Join(t.TempDir(), "kvm_config.json")
	tailscale.CheckInstalled = func() bool { return installed }
	tailscale.ExecCommand = func(args ...string) ([]byte, error) {
		if !installed {
			return nil, errors.New("tailscale: not found")
		}
		return []byte(statusJSON), nil
	}
}

func TestTailscaleStatusReportsScriptInstallWhileBuiltinIsOff(t *testing.T) {
	withScriptInstalledTailscale(t, true, `{"BackendState":"Running","Self":{"HostName":"kvm","TailscaleIPs":["100.80.1.2"]}}`)

	status, err := rpcGetTailscaleStatus()
	require.NoError(t, err)

	assert.False(t, status.Builtin)
	assert.True(t, status.Running)
	assert.Equal(t, []string{"100.80.1.2"}, status.Self.TailscaleIPs)
}

func TestTailscaleStatusOffersBuiltinClientWithoutScriptInstall(t *testing.T) {
	withScriptInstalledTailscale(t, false, "")

	status, err := rpcGetTailscaleStatus()
	require.NoError(t, err)

	assert.True(t, status.Installed)
	assert.True(t, status.Builtin)
	assert.False(t, status.Enabled)
	assert.False(t, status.Running)
}

func TestEnableTailscaleIsRefusedWhileScriptInstallExists(t *testing.T) {
	withScriptInstalledTailscale(t, true, `{"BackendState":"Running"}`)

	require.Error(t, rpcSetTailscaleEnabled(true))

	assert.False(t, config.TailscaleEnabled)
	assert.Nil(t, tailscaleNode)
	assert.NoFileExists(t, configPath)
	status, err := rpcGetTailscaleStatus()
	require.NoError(t, err)
	assert.False(t, status.Builtin)
}
