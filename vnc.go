package kvm

import (
	"errors"
	"fmt"

	"github.com/jetkvm/kvm/internal/sync"
)

// vncServer is the global VNC server instance, or nil when the server
// is disabled. Accessed from native.go's frame callback.
var (
	vncServer   *VNCServer
	vncServerMu sync.Mutex
)

// VNCConfig is the JSON-RPC view of the VNC configuration.
type VNCConfig struct {
	Enabled      bool   `json:"enabled"`
	Port         int    `json:"port"`
	Password     string `json:"password"`
	AllowOverWAN bool   `json:"allow_over_wan"`
}

// rpcGetVNCConfig returns the current VNC server configuration. The
// password field is returned masked (empty if a password is set, or
// empty if no password is set) so it never leaks back to the
// frontend after configuration.
func rpcGetVNCConfig() VNCConfig {
	masked := ""
	if config.VncPassword != "" {
		masked = "********"
	}
	return VNCConfig{
		Enabled:      config.VncEnabled,
		Port:         config.VncPort,
		Password:     masked,
		AllowOverWAN: config.VncAllowOverWAN,
	}
}

// rpcSetVNCConfig persists the supplied VNC configuration and
// restarts the listener if needed. A password value of "********"
// (the same masked sentinel returned by rpcGetVNCConfig) is treated
// as "leave unchanged" so the frontend can submit the form without
// re-typing the password every time.
func rpcSetVNCConfig(cfg VNCConfig) error {
	if cfg.Port <= 0 || cfg.Port > 65535 {
		return errors.New("vnc port must be in 1..65535")
	}

	prev := struct {
		enabled      bool
		port         int
		password     string
		allowOverWAN bool
	}{
		enabled: config.VncEnabled, port: config.VncPort,
		password: config.VncPassword, allowOverWAN: config.VncAllowOverWAN,
	}

	config.VncEnabled = cfg.Enabled
	config.VncPort = cfg.Port
	if cfg.Password != "********" {
		config.VncPassword = cfg.Password
	}
	config.VncAllowOverWAN = cfg.AllowOverWAN

	if err := SaveConfig(); err != nil {
		// Roll back in-memory.
		config.VncEnabled, config.VncPort = prev.enabled, prev.port
		config.VncPassword, config.VncAllowOverWAN = prev.password, prev.allowOverWAN
		return fmt.Errorf("failed to save vnc config: %w", err)
	}

	// Reconcile the running server. Only restart when something
	// runtime-relevant changed: enable flag, port, or WAN policy.
	needRestart := prev.enabled != config.VncEnabled ||
		prev.port != config.VncPort ||
		prev.allowOverWAN != config.VncAllowOverWAN
	if needRestart {
		StopVNCServer()
		if config.VncEnabled {
			if err := StartVNCServer(); err != nil {
				vncLogger.Error().Err(err).Msg("failed to start VNC server after config change")
				return err
			}
		}
	}
	return nil
}

// StartVNCServer starts the VNC TCP listener if VNC is enabled in
// the configuration. Subsequent commits flesh out the actual server.
//
// Safe to call repeatedly — calls are idempotent: a second call while
// the server is already running is a no-op and returns nil.
func StartVNCServer() error {
	vncServerMu.Lock()
	defer vncServerMu.Unlock()

	if vncServer != nil {
		return nil
	}
	if !config.VncEnabled {
		vncLogger.Debug().Msg("VNC server disabled in config")
		return nil
	}

	vncLogger.Info().Int("port", config.VncPort).Msg("VNC server scaffold (no listener yet)")
	// TODO(commit 6): create listener, accept loop, frame fan-out.
	return nil
}

// StopVNCServer shuts down the VNC server if it is running. Safe to
// call when no server is active.
func StopVNCServer() {
	vncServerMu.Lock()
	defer vncServerMu.Unlock()

	if vncServer == nil {
		return
	}
	vncLogger.Info().Msg("stopping VNC server")
	// TODO(commit 6): close listener, cancel per-conn goroutines.
	vncServer = nil
}

// VNCServer holds runtime state for the listener. Filled in by
// commit 6.
type VNCServer struct {
	// fields filled in by commit 6
}
