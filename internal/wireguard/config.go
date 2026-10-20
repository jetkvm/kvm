// Package wireguard runs a kernel WireGuard interface for reaching the device
// over a VPN. It only answers connections that come in through the tunnel: the
// peers' allowed IPs are routed in a separate table that is used for traffic
// sent from the tunnel address, so the main routing table, and with it the
// device's normal networking, is never changed.
package wireguard

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

// SecretMask stands in for a stored private or preshared key in settings sent
// to the UI. Saving it back keeps the stored key.
const SecretMask = "********"

const (
	maxPeers        = 16
	maxAllowedIPs   = 32
	maxAddresses    = 4
	maxPeerNameLen  = 64
	maxKeepaliveSec = 65535
)

// Config is the WireGuard section of the device config.
type Config struct {
	Enabled    bool   `json:"enabled"`
	PrivateKey string `json:"private_key,omitempty"`
	// Addresses are the device's addresses in the VPN, such as 10.8.0.5/24.
	// Only the address itself is used; the prefix length is kept as entered.
	Addresses  []string `json:"addresses"`
	ListenPort int      `json:"listen_port,omitempty"`
	Peers      []Peer   `json:"peers"`
}

// Peer is a WireGuard peer, usually the VPN server.
type Peer struct {
	Name         string `json:"name,omitempty"`
	PublicKey    string `json:"public_key"`
	PresharedKey string `json:"preshared_key,omitempty"`
	// Endpoint is host:port. A host name is resolved again while the peer
	// has no recent handshake, so a changing address is followed.
	Endpoint   string   `json:"endpoint,omitempty"`
	AllowedIPs []string `json:"allowed_ips"`
	// PersistentKeepalive is in seconds; 0 turns it off.
	PersistentKeepalive int `json:"persistent_keepalive"`
}

// Validate checks the format of every value that is set, and when the config
// is enabled, that everything needed to bring the tunnel up is there. It
// normalizes keys, addresses and allowed IPs in place.
func (c *Config) Validate() error {
	if c.PrivateKey != "" {
		key, err := wgtypes.ParseKey(c.PrivateKey)
		if err != nil {
			return errors.New("private key is not a valid WireGuard key")
		}
		c.PrivateKey = key.String()
	}

	if len(c.Addresses) > maxAddresses {
		return fmt.Errorf("at most %d addresses are supported", maxAddresses)
	}
	for i, a := range c.Addresses {
		p, err := parseAddress(a)
		if err != nil {
			return err
		}
		c.Addresses[i] = p.String()
	}

	if c.ListenPort < 0 || c.ListenPort > 65535 {
		return errors.New("listen port must be between 1 and 65535, or empty")
	}

	if len(c.Peers) > maxPeers {
		return fmt.Errorf("at most %d peers are supported", maxPeers)
	}
	seen := make(map[string]bool, len(c.Peers))
	for i := range c.Peers {
		p := &c.Peers[i]
		if err := p.validate(c.Enabled); err != nil {
			return fmt.Errorf("peer %d: %w", i+1, err)
		}
		if seen[p.PublicKey] {
			return fmt.Errorf("peer %d: another peer has the same public key", i+1)
		}
		seen[p.PublicKey] = true
	}

	if !c.Enabled {
		return nil
	}
	if c.PrivateKey == "" {
		return errors.New("a private key is required; generate one")
	}
	if len(c.Addresses) == 0 {
		return errors.New("an address in the VPN is required")
	}
	if len(c.Peers) == 0 {
		return errors.New("at least one peer is required")
	}
	if own, err := PublicKey(c.PrivateKey); err == nil && seen[own] {
		return errors.New("a peer has this device's own public key")
	}
	return nil
}

func (p *Peer) validate(enabled bool) error {
	p.Name = strings.TrimSpace(p.Name)
	if len(p.Name) > maxPeerNameLen {
		return fmt.Errorf("name is longer than %d characters", maxPeerNameLen)
	}

	key, err := wgtypes.ParseKey(strings.TrimSpace(p.PublicKey))
	if err != nil {
		return errors.New("public key is not a valid WireGuard key")
	}
	p.PublicKey = key.String()

	if p.PresharedKey != "" {
		psk, err := wgtypes.ParseKey(strings.TrimSpace(p.PresharedKey))
		if err != nil {
			return errors.New("preshared key is not a valid WireGuard key")
		}
		p.PresharedKey = psk.String()
	}

	p.Endpoint = strings.TrimSpace(p.Endpoint)
	if p.Endpoint != "" {
		if _, _, err := splitEndpoint(p.Endpoint); err != nil {
			return err
		}
	}

	if len(p.AllowedIPs) > maxAllowedIPs {
		return fmt.Errorf("at most %d allowed IPs are supported", maxAllowedIPs)
	}
	for i, a := range p.AllowedIPs {
		prefix, err := parseAllowedIP(a)
		if err != nil {
			return err
		}
		p.AllowedIPs[i] = prefix.String()
	}
	if enabled && len(p.AllowedIPs) == 0 {
		return errors.New("at least one allowed IP is required")
	}

	if p.PersistentKeepalive < 0 || p.PersistentKeepalive > maxKeepaliveSec {
		return fmt.Errorf("keepalive must be between 0 and %d seconds", maxKeepaliveSec)
	}
	return nil
}

// parseAddress parses an interface address such as 10.8.0.5/24 or a bare
// 10.8.0.5, keeping the host bits.
func parseAddress(s string) (netip.Prefix, error) {
	s = strings.TrimSpace(s)
	if !strings.Contains(s, "/") {
		addr, err := netip.ParseAddr(s)
		if err != nil {
			return netip.Prefix{}, fmt.Errorf("address %q is not a valid IP address", s)
		}
		return netip.PrefixFrom(addr, addr.BitLen()), nil
	}
	p, err := netip.ParsePrefix(s)
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("address %q is not a valid IP address with prefix length", s)
	}
	if p.Addr().Zone() != "" {
		return netip.Prefix{}, fmt.Errorf("address %q must not have a zone", s)
	}
	return p, nil
}

// ErrDefaultRoute is returned for an allowed IP of 0.0.0.0/0 or ::/0. Sending
// all of the device's traffic through the VPN is not supported.
var ErrDefaultRoute = errors.New("sending all traffic through the VPN (0.0.0.0/0 or ::/0) is not supported; enter the VPN's own address range")

// parseAllowedIP parses an allowed IP such as 10.8.0.0/24, or a bare address
// meaning that address alone, and returns it with the host bits cleared.
func parseAllowedIP(s string) (netip.Prefix, error) {
	p, err := parseAddress(s)
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("allowed IP %q is not a valid IP range", strings.TrimSpace(s))
	}
	if p.Bits() == 0 {
		return netip.Prefix{}, ErrDefaultRoute
	}
	return p.Masked(), nil
}

// splitEndpoint splits host:port, accepting [v6]:port.
func splitEndpoint(s string) (string, int, error) {
	host, portStr, err := net.SplitHostPort(s)
	if err != nil || host == "" {
		return "", 0, fmt.Errorf("endpoint %q must be host:port, such as vpn.example.com:51820", s)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return "", 0, fmt.Errorf("endpoint %q has an invalid port", s)
	}
	return host, port, nil
}

// GeneratePrivateKey returns a new private key.
func GeneratePrivateKey() (string, error) {
	key, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		return "", err
	}
	return key.String(), nil
}

// PublicKey returns the public key for a private key.
func PublicKey(privateKey string) (string, error) {
	key, err := wgtypes.ParseKey(privateKey)
	if err != nil {
		return "", err
	}
	return key.PublicKey().String(), nil
}

// Masked returns a copy of c with the private and preshared keys replaced by
// SecretMask, for sending to the UI.
func (c Config) Masked() Config {
	if c.PrivateKey != "" {
		c.PrivateKey = SecretMask
	}
	c.Addresses = append([]string{}, c.Addresses...)
	peers := make([]Peer, len(c.Peers))
	for i, p := range c.Peers {
		if p.PresharedKey != "" {
			p.PresharedKey = SecretMask
		}
		p.AllowedIPs = append([]string{}, p.AllowedIPs...)
		peers[i] = p
	}
	c.Peers = peers
	return c
}

// Unmask replaces SecretMask values in c with the keys stored in old. A peer's
// preshared key is matched by the peer's public key.
func (c *Config) Unmask(old Config) {
	if c.PrivateKey == SecretMask {
		c.PrivateKey = old.PrivateKey
	}
	oldPSK := make(map[string]string, len(old.Peers))
	for _, p := range old.Peers {
		oldPSK[strings.TrimSpace(p.PublicKey)] = p.PresharedKey
	}
	for i := range c.Peers {
		if c.Peers[i].PresharedKey == SecretMask {
			c.Peers[i].PresharedKey = oldPSK[strings.TrimSpace(c.Peers[i].PublicKey)]
		}
	}
}
