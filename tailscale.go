package kvm

import (
	"fmt"

	"github.com/jetkvm/kvm/internal/tailscale"
)

func rpcGetTailscaleStatus() (*tailscale.Status, error) {
	ensureConfigLoaded()
	return tailscale.GetStatus(config.TailscaleControlURL, func(err error) {
		tailscaleLogger.Warn().Err(err).Msg("failed to get tailscale status")
	})
}

func rpcGetTailscaleControlURL() (string, error) {
	ensureConfigLoaded()
	return tailscale.EffectiveControlURL(config.TailscaleControlURL), nil
}

func rpcSetTailscaleControlURL(controlURL string) error {
	ensureConfigLoaded()

	previousURL := config.TailscaleControlURL

	normalized, err := tailscale.SetControlURL(controlURL)
	if err != nil {
		return err
	}

	config.TailscaleControlURL = normalized
	if err := SaveConfig(); err != nil {
		config.TailscaleControlURL = previousURL
		return fmt.Errorf("failed to save tailscale control URL: %w", err)
	}

	return nil
}
