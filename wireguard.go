package kvm

import (
	"fmt"

	"github.com/jetkvm/kvm/internal/wireguard"
)

var wireguardManager = wireguard.NewManager(wireguardLogger)

func defaultWireguardConfig() *wireguard.Config {
	return &wireguard.Config{Addresses: []string{}, Peers: []wireguard.Peer{}}
}

// initWireguard brings the tunnel up at boot when it is enabled. A disabled
// config leaves the interfaces alone, so a tunnel set up by hand over SSH
// keeps working until WireGuard is turned off in the UI.
func initWireguard() {
	cfg := *config.WireguardConfig
	if !cfg.Enabled {
		wireguardLogger.Info().Msg("WireGuard is disabled")
		return
	}
	if err := cfg.Validate(); err != nil {
		wireguardLogger.Error().Err(err).Msg("invalid WireGuard config, not starting the tunnel")
		return
	}
	// resolving the endpoints can wait for DNS, so do not hold up the boot
	go func() {
		_ = wireguardManager.Apply(cfg)
	}()
}

func rpcGetWireguardSettings() (wireguard.Config, error) {
	return config.WireguardConfig.Masked(), nil
}

func rpcSetWireguardSettings(settings wireguard.Config) error {
	settings.Unmask(*config.WireguardConfig)
	if err := settings.Validate(); err != nil {
		return err
	}

	previous := config.WireguardConfig
	config.WireguardConfig = &settings
	if err := SaveConfig(); err != nil {
		config.WireguardConfig = previous
		return fmt.Errorf("failed to save config: %w", err)
	}

	if err := wireguardManager.Apply(settings); err != nil {
		return fmt.Errorf("settings saved, but the tunnel could not be updated: %w", err)
	}
	return nil
}

// rpcGenerateWireguardKey replaces the device's private key and returns the
// new public key. The private key never leaves the device.
func rpcGenerateWireguardKey() (string, error) {
	privateKey, err := wireguard.GeneratePrivateKey()
	if err != nil {
		return "", fmt.Errorf("failed to generate key: %w", err)
	}
	publicKey, err := wireguard.PublicKey(privateKey)
	if err != nil {
		return "", err
	}

	previous := config.WireguardConfig
	updated := *previous
	updated.PrivateKey = privateKey
	config.WireguardConfig = &updated
	if err := SaveConfig(); err != nil {
		config.WireguardConfig = previous
		return "", fmt.Errorf("failed to save config: %w", err)
	}

	if updated.Enabled {
		if err := wireguardManager.Apply(updated); err != nil {
			return "", fmt.Errorf("key saved, but the tunnel could not be updated: %w", err)
		}
	}
	return publicKey, nil
}

func rpcGetWireguardStatus() (wireguard.Status, error) {
	status := wireguardManager.Status()
	// show the stored key's public key also while the tunnel is off
	status.PublicKey = ""
	if privateKey := config.WireguardConfig.PrivateKey; privateKey != "" {
		status.PublicKey, _ = wireguard.PublicKey(privateKey)
	}
	return status, nil
}

// rpcParseWireguardConfig parses a pasted .conf for the UI to fill in its
// form; nothing is saved.
func rpcParseWireguardConfig(text string) (*wireguard.ImportResult, error) {
	return wireguard.ParseConf(text)
}
