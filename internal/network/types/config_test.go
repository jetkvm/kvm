package types

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/guregu/null/v6"
	"github.com/jetkvm/kvm/internal/confparser"
	"github.com/stretchr/testify/require"
)

func loadStoredNetworkConfig(t *testing.T, path string) *NetworkConfig {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err)

	var stored struct {
		NetworkConfig *NetworkConfig `json:"network_config"`
	}
	require.NoError(t, json.Unmarshal(raw, &stored))
	require.NotNil(t, stored.NetworkConfig)
	return stored.NetworkConfig
}

func TestStoredJetdhcpcConfigKeepsNetworkSettings(t *testing.T) {
	c := loadStoredNetworkConfig(t, "testdata/network_config_jetdhcpc.json")
	c.Migrate()

	require.NoError(t, confparser.SetDefaultsAndValidate(c))

	require.Equal(t, &NetworkConfig{
		Hostname:  null.StringFrom("rack-kvm-07"),
		HTTPProxy: null.StringFrom("http://proxy.example.com:3128"),
		Domain:    null.StringFrom("lab.example.com"),
		IPv4Mode:  null.StringFrom("static"),
		IPv4Static: &IPv4StaticConfig{
			Address: null.StringFrom("10.20.30.40"),
			Netmask: null.StringFrom("255.255.254.0"),
			Gateway: null.StringFrom("10.20.30.1"),
			DNS:     []string{"10.20.30.2", "10.20.30.3"},
		},
		IPv6Mode:                null.StringFrom("slaac"),
		LLDPMode:                null.StringFrom("all"),
		LLDPTxTLVs:              []string{"chassis", "port"},
		MDNSMode:                null.StringFrom("ipv4_only"),
		TimeSyncMode:            null.StringFrom("custom"),
		TimeSyncOrdering:        []string{"ntp_user_provided", "http"},
		TimeSyncDisableFallback: null.BoolFrom(true),
		TimeSyncParallel:        null.IntFrom(2),
		TimeSyncNTPServers:      []string{"ntp.example.com"},
		TimeSyncHTTPUrls:        []string{"https://time.example.com"},
	}, c)
}

func TestMigrateReplacesRemovedIPv6Modes(t *testing.T) {
	for mode, want := range map[string]string{
		"dhcpv6":           "slaac",
		"slaac_and_dhcpv6": "slaac",
		"slaac":            "slaac",
		"link_local":       "link_local",
		"disabled":         "disabled",
	} {
		c := &NetworkConfig{IPv6Mode: null.StringFrom(mode)}
		c.Migrate()
		require.Equal(t, want, c.IPv6Mode.String, mode)
		require.NoError(t, confparser.SetDefaultsAndValidate(c), mode)
	}
}

func TestStoredRemovedIPv6ModeFailsValidationWithoutMigrate(t *testing.T) {
	c := loadStoredNetworkConfig(t, "testdata/network_config_jetdhcpc.json")
	require.Error(t, confparser.SetDefaultsAndValidate(c))
}
