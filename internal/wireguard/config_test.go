package wireguard

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testPrivateKey = "yAnz5TF+lXXJte14tji3zlMNq+hd2rYUIgJBgB3fBmk="
	testPeerKey    = "xTIBA5rboUvnH4htodjb6e697QjLERt1NAB4mZqp8Dg="
	testPeerKey2   = "TrMvSoP4jYQlY6RIzBgbssQqY3vxI2Pi+y71lOWWXX0="
)

func validConfig() Config {
	return Config{
		Enabled:    true,
		PrivateKey: testPrivateKey,
		Addresses:  []string{"10.8.0.5/24"},
		Peers: []Peer{{
			PublicKey:           testPeerKey,
			Endpoint:            "vpn.example.com:51820",
			AllowedIPs:          []string{"10.8.0.0/24"},
			PersistentKeepalive: 25,
		}},
	}
}

func TestValidateAcceptsValidConfig(t *testing.T) {
	cfg := validConfig()
	require.NoError(t, cfg.Validate())
}

func TestValidateNormalizes(t *testing.T) {
	cfg := validConfig()
	cfg.Addresses = []string{" 10.8.0.5 ", "fd00::5/64"}
	cfg.Peers[0].PublicKey = " " + testPeerKey + " "
	cfg.Peers[0].Endpoint = " [2001:db8::1]:51820 "
	cfg.Peers[0].AllowedIPs = []string{"10.8.0.7/24", "192.168.1.10", "fd00::/64"}

	require.NoError(t, cfg.Validate())
	assert.Equal(t, []string{"10.8.0.5/32", "fd00::5/64"}, cfg.Addresses)
	assert.Equal(t, testPeerKey, cfg.Peers[0].PublicKey)
	assert.Equal(t, "[2001:db8::1]:51820", cfg.Peers[0].Endpoint)
	assert.Equal(t, []string{"10.8.0.0/24", "192.168.1.10/32", "fd00::/64"}, cfg.Peers[0].AllowedIPs)
}

func TestValidateRejects(t *testing.T) {
	tests := []struct {
		name   string
		modify func(*Config)
		errMsg string
	}{
		{"default route v4", func(c *Config) { c.Peers[0].AllowedIPs = []string{"10.8.0.0/24", "0.0.0.0/0"} }, "not supported"},
		{"default route v6", func(c *Config) { c.Peers[0].AllowedIPs = []string{"::/0"} }, "not supported"},
		{"bad private key", func(c *Config) { c.PrivateKey = "nope" }, "private key"},
		{"missing private key", func(c *Config) { c.PrivateKey = "" }, "private key is required"},
		{"missing address", func(c *Config) { c.Addresses = nil }, "address"},
		{"bad address", func(c *Config) { c.Addresses = []string{"10.8.0.300/24"} }, "not a valid"},
		{"no peers", func(c *Config) { c.Peers = nil }, "at least one peer"},
		{"bad peer key", func(c *Config) { c.Peers[0].PublicKey = "abc" }, "public key"},
		{"bad preshared key", func(c *Config) { c.Peers[0].PresharedKey = "abc" }, "preshared key"},
		{"endpoint with path", func(c *Config) { c.Peers[0].Endpoint = "vpn.example.com/x" }, "host name or address"},
		{"endpoint empty host", func(c *Config) { c.Peers[0].Endpoint = ":51820" }, "host name or address"},
		{"duplicate address", func(c *Config) { c.Addresses = []string{"10.8.0.5", "10.8.0.5/24"} }, "listed twice"},
		{"endpoint bad port", func(c *Config) { c.Peers[0].Endpoint = "vpn.example.com:99999" }, "invalid port"},
		{"no allowed IPs", func(c *Config) { c.Peers[0].AllowedIPs = nil }, "allowed IP"},
		{"bad allowed IP", func(c *Config) { c.Peers[0].AllowedIPs = []string{"10.8.0.0/33"} }, "not a valid IP range"},
		{"keepalive range", func(c *Config) { c.Peers[0].PersistentKeepalive = -1 }, "keepalive"},
		{"listen port range", func(c *Config) { c.ListenPort = 70000 }, "listen port"},
		{"duplicate peers", func(c *Config) { c.Peers = append(c.Peers, c.Peers[0]) }, "same public key"},
		{"own key as peer", func(c *Config) {
			own, _ := PublicKey(testPrivateKey)
			c.Peers[0].PublicKey = own
		}, "own public key"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.modify(&cfg)
			err := cfg.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errMsg)
		})
	}
}

func TestValidateEndpointDefaultPort(t *testing.T) {
	tests := map[string]string{
		"vpn.example.com":       "vpn.example.com:51820",
		"vpn.example.com:443":   "vpn.example.com:443",
		"192.0.2.1":             "192.0.2.1:51820",
		"2001:db8::1":           "[2001:db8::1]:51820",
		"[2001:db8::1]":         "[2001:db8::1]:51820",
		"[2001:db8::1]:51821":   "[2001:db8::1]:51821",
		" vpn.example.com:443 ": "vpn.example.com:443",
	}
	for in, want := range tests {
		cfg := validConfig()
		cfg.Peers[0].Endpoint = in
		require.NoError(t, cfg.Validate(), in)
		assert.Equal(t, want, cfg.Peers[0].Endpoint, in)
	}
}

func TestValidateDisabledAllowsIncompleteConfig(t *testing.T) {
	cfg := Config{Enabled: false, Peers: []Peer{{PublicKey: testPeerKey}}}
	require.NoError(t, cfg.Validate())

	// but values that are set must still be well-formed
	cfg.Peers[0].AllowedIPs = []string{"0.0.0.0/0"}
	require.ErrorIs(t, cfg.Validate(), ErrDefaultRoute)
}

func TestMaskAndUnmask(t *testing.T) {
	stored := validConfig()
	stored.Peers[0].PresharedKey = testPeerKey2

	masked := stored.Masked()
	assert.Equal(t, SecretMask, masked.PrivateKey)
	assert.Equal(t, SecretMask, masked.Peers[0].PresharedKey)
	// the stored config is untouched
	assert.Equal(t, testPrivateKey, stored.PrivateKey)
	assert.Equal(t, testPeerKey2, stored.Peers[0].PresharedKey)

	masked.Peers[0].AllowedIPs[0] = "10.9.0.0/24"
	assert.Equal(t, "10.8.0.0/24", stored.Peers[0].AllowedIPs[0])

	require.NoError(t, masked.Unmask(stored))
	assert.Equal(t, testPrivateKey, masked.PrivateKey)
	assert.Equal(t, testPeerKey2, masked.Peers[0].PresharedKey)
}

func TestUnmaskChangedPublicKeyNeedsPresharedKeyAgain(t *testing.T) {
	stored := validConfig()
	stored.Peers[0].PresharedKey = testPeerKey2
	cfg := validConfig()
	cfg.Peers[0].PublicKey = testPeerKey2
	cfg.Peers[0].PresharedKey = SecretMask

	err := cfg.Unmask(stored)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "preshared key again")
}

func TestMaskedEmptyKeysStayEmpty(t *testing.T) {
	masked := Config{}.Masked()
	assert.Empty(t, masked.PrivateKey)
	assert.NotNil(t, masked.Addresses)
	assert.NotNil(t, masked.Peers)
}

func TestGenerateAndPublicKey(t *testing.T) {
	priv, err := GeneratePrivateKey()
	require.NoError(t, err)
	pub, err := PublicKey(priv)
	require.NoError(t, err)
	assert.NotEqual(t, priv, pub)
	assert.Len(t, pub, 44)
}
