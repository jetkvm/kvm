package kvm

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/jetkvm/kvm/internal/tailscale"
)

const tailscaleStatePath = "/userdata/jetkvm/tailscale"

var (
	tailscaleMu       sync.Mutex
	tailscaleNode     *tailscale.Node
	tailscaleStartErr error
)

func initTailscale() {
	if !config.TailscaleEnabled {
		return
	}
	go func() {
		tailscaleMu.Lock()
		defer tailscaleMu.Unlock()
		if !config.TailscaleEnabled || tailscaleNode != nil {
			return
		}
		_ = startTailscaleLocked("")
		broadcastTailscaleStatus(tailscaleStatusLocked())
	}()
}

func startTailscaleLocked(authKey string) error {
	hostname := config.NetworkConfig.Hostname.String
	if hostname == "" {
		hostname = GetDefaultHostname()
	}

	tailscaleNode, tailscaleStartErr = tailscale.StartNode(tailscale.NodeOptions{
		StateDir:   tailscaleStatePath,
		Hostname:   hostname,
		ControlURL: config.TailscaleControlURL,
		AuthKey:    authKey,
		OpenLink: func() (*tailscale.Link, error) {
			return tailscale.OpenTUN(tailscaleDebugf)
		},
		Logf:     tailscaleLogf,
		Debugf:   tailscaleDebugf,
		OnChange: broadcastTailscaleStatus,
	})
	if tailscaleStartErr != nil {
		tailscaleNode = nil
		tailscaleLogger.Error().Err(tailscaleStartErr).Msg("failed to start tailscale")
	}
	return tailscaleStartErr
}

func stopTailscaleLocked() {
	if tailscaleNode == nil {
		return
	}
	if err := tailscaleNode.Close(); err != nil {
		tailscaleLogger.Warn().Err(err).Msg("failed to stop tailscale")
	}
	tailscaleNode = nil
}

func logoutTailscaleLocked() {
	if tailscaleNode == nil {
		return
	}
	if err := tailscaleNode.Logout(); err != nil {
		tailscaleLogger.Warn().Err(err).Msg("failed to log out of tailscale")
	}
	tailscaleNode = nil
}

func tailscaleLogf(format string, args ...any) {
	tailscaleLogger.Info().Msg(strings.TrimSpace(fmt.Sprintf(format, args...)))
}

func tailscaleDebugf(format string, args ...any) {
	if e := tailscaleLogger.Debug(); e.Enabled() {
		e.Msg(strings.TrimSpace(fmt.Sprintf(format, args...)))
	}
}

func broadcastTailscaleStatus(status *tailscale.Status) {
	if session := currentSession; session != nil {
		writeJSONRPCEvent("tailscaleStatus", status, session)
	}
}

func tailscaleStatusLocked() *tailscale.Status {
	if tailscaleNode != nil {
		return tailscaleNode.Status()
	}

	status := &tailscale.Status{
		Installed:  true,
		Builtin:    true,
		Enabled:    config.TailscaleEnabled,
		ControlURL: tailscale.EffectiveControlURL(config.TailscaleControlURL),
	}
	if config.TailscaleEnabled {
		if tailscaleStartErr != nil {
			status.Error = tailscaleStartErr.Error()
		}
		return status
	}

	external, _ := tailscale.GetStatus(config.TailscaleControlURL, func(err error) {
		tailscaleLogger.Warn().Err(err).Msg("failed to get tailscale status")
	})
	if external != nil && external.Installed {
		return external
	}
	return status
}

func rpcGetTailscaleStatus() (*tailscale.Status, error) {
	ensureConfigLoaded()
	tailscaleMu.Lock()
	defer tailscaleMu.Unlock()
	return tailscaleStatusLocked(), nil
}

func rpcSetTailscaleEnabled(enabled bool) error {
	ensureConfigLoaded()
	tailscaleMu.Lock()
	defer tailscaleMu.Unlock()

	if enabled == config.TailscaleEnabled {
		return nil
	}
	if enabled && tailscale.CheckInstalled() {
		return errors.New("remove the Tailscale installation in /userdata/tailscale before you enable the built-in client")
	}

	config.TailscaleEnabled = enabled
	if err := SaveConfig(); err != nil {
		config.TailscaleEnabled = !enabled
		return fmt.Errorf("failed to save tailscale state: %w", err)
	}

	var err error
	if enabled {
		if err = startTailscaleLocked(""); err != nil {
			config.TailscaleEnabled = false
			tailscaleStartErr = nil
			if saveErr := SaveConfig(); saveErr != nil {
				tailscaleLogger.Warn().Err(saveErr).Msg("failed to save tailscale state")
			}
		}
	} else {
		stopTailscaleLocked()
	}

	broadcastTailscaleStatus(tailscaleStatusLocked())
	return err
}

func rpcLoginTailscale(authKey string) error {
	ensureConfigLoaded()
	tailscaleMu.Lock()
	defer tailscaleMu.Unlock()

	if !config.TailscaleEnabled {
		return errors.New("tailscale is not enabled")
	}

	stopTailscaleLocked()
	err := startTailscaleLocked(strings.TrimSpace(authKey))
	broadcastTailscaleStatus(tailscaleStatusLocked())
	return err
}

func rpcLogoutTailscale() error {
	ensureConfigLoaded()
	tailscaleMu.Lock()
	defer tailscaleMu.Unlock()

	if tailscaleNode == nil {
		return errors.New("tailscale is not running")
	}

	logoutTailscaleLocked()
	err := startTailscaleLocked("")
	broadcastTailscaleStatus(tailscaleStatusLocked())
	return err
}

func rpcGetTailscaleControlURL() (string, error) {
	ensureConfigLoaded()
	return tailscale.EffectiveControlURL(config.TailscaleControlURL), nil
}

func rpcSetTailscaleControlURL(controlURL string) error {
	ensureConfigLoaded()
	tailscaleMu.Lock()
	defer tailscaleMu.Unlock()

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

	if tailscaleNode == nil || normalized == previousURL {
		return nil
	}

	logoutTailscaleLocked()
	err = startTailscaleLocked("")
	broadcastTailscaleStatus(tailscaleStatusLocked())
	return err
}
