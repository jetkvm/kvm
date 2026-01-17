// VNC Server Bridge
//
// This file provides the bridge between the kvm package and the internal/vnc package.
// It implements the dependency interfaces and provides initialization functions.

package kvm

import (
	"net"
	"sync"

	"github.com/jetkvm/kvm/internal/hidrpc"
	"github.com/jetkvm/kvm/internal/vnc"
	"github.com/jetkvm/kvm/internal/vnctls"
)

var (
	vncServer     *vnc.Server
	vncServerOnce sync.Once
)

// GetVNCServer returns the global VNC server instance.
func GetVNCServer() *vnc.Server {
	vncServerOnce.Do(func() {
		deps := vnc.Dependencies{
			Config:  &vncConfigAdapter{},
			Encoder: &vncEncoderAdapter{},
			HID:     &vncHIDAdapter{},
			TLS:     &vncTLSAdapter{},
			Logger:  vncLogger, // vncLogger is already *zerolog.Logger
		}
		vncServer = vnc.NewServer(deps)
	})
	return vncServer
}

// vncConfigAdapter adapts kvm config to vnc.Config interface.
type vncConfigAdapter struct{}

func (a *vncConfigAdapter) GetTLSMode() string {
	return config.TLSMode
}

func (a *vncConfigAdapter) GetVNCQuality() int {
	return config.VNCQuality
}

func (a *vncConfigAdapter) GetVNCMaxConnections() int {
	return config.VNCMaxConnections
}

func (a *vncConfigAdapter) GetVNCPasteDelayMs() int {
	return config.VNCPasteDelayMs
}

func (a *vncConfigAdapter) GetVNCClipboardEnabled() bool {
	return config.VNCClipboardEnabled
}

func (a *vncConfigAdapter) GetLocalAuthPassword() string {
	if config.LocalAuthPassword != "" {
		return config.LocalAuthPassword
	}
	return config.VNCPassword
}

// vncEncoderAdapter adapts native encoder to vnc.NativeEncoder interface.
type vncEncoderAdapter struct{}

func (a *vncEncoderAdapter) JpegStart(quality int) error {
	return nativeInstance.JpegStart(quality)
}

func (a *vncEncoderAdapter) JpegStop() error {
	return nativeInstance.JpegStop()
}

// vncHIDAdapter adapts HID RPC calls to vnc.HIDController interface.
type vncHIDAdapter struct{}

func (a *vncHIDAdapter) KeypressReport(key uint8, down bool) error {
	return rpcKeypressReport(key, down)
}

func (a *vncHIDAdapter) AbsMouseReport(x, y int, buttons byte) error {
	return rpcAbsMouseReport(x, y, buttons)
}

func (a *vncHIDAdapter) WheelReport(wheelY, wheelX int8) error {
	return rpcWheelReport(wheelY, wheelX)
}

func (a *vncHIDAdapter) KeyboardMacro(steps []vnc.KeyboardMacroStep) error {
	// Convert vnc.KeyboardMacroStep to hidrpc.KeyboardMacroStep
	hidrpcSteps := make([]hidrpc.KeyboardMacroStep, len(steps))
	for i, step := range steps {
		hidrpcSteps[i] = hidrpc.KeyboardMacroStep{
			Modifier: step.Modifier,
			Keys:     step.Keys,
			Delay:    step.Delay,
		}
	}
	return rpcExecuteKeyboardMacro(hidrpcSteps)
}

func (a *vncHIDAdapter) IsKeyboardMacroInProgress() bool {
	return isKeyboardMacroInProgress()
}

func (a *vncHIDAdapter) CancelKeyboardMacro() {
	cancelKeyboardMacro()
}

// vncTLSAdapter adapts TLS functionality to vnc.TLSProvider interface.
type vncTLSAdapter struct{}

func (a *vncTLSAdapter) IsTLSAvailable() bool {
	switch config.TLSMode {
	case "self-signed":
		return !isTimeSyncNeeded() && timeSync != nil && timeSync.IsSyncSuccess()
	case "custom":
		return true
	default:
		return false
	}
}

func (a *vncTLSAdapter) GetCertificate() any {
	cert, err := getCertificate(nil)
	if err != nil {
		return nil
	}
	return cert
}

// initVNCServer initializes and starts the VNC server if enabled.
// Returns an error if the server fails to start.
func initVNCServer() error {
	if !config.VNCEnabled {
		vncLogger.Info().Msg("VNC server disabled in configuration")
		return nil
	}

	// Initialize OpenSSL TLS subsystem early to check hardware crypto availability
	vnctls.Init()

	// Set up the TLS and crypto hooks in the vnc package
	vnc.TLSAvailabilityChecker = func() bool {
		return (&vncTLSAdapter{}).IsTLSAvailable()
	}
	vnc.GetCertificateFunc = getCertificate
	vnc.TLSConnUpgrader = func(conn net.Conn, useX509 bool, certFile, keyFile string) (vnc.TLSConnection, error) {
		tlsConn, err := vnctls.UpgradeToTLS(conn, useX509, certFile, keyFile)
		if err != nil {
			return nil, err
		}
		return tlsConn, nil
	}
	vnc.IsHardwareCryptoEnabledFunc = vnctls.IsHardwareCryptoEnabled
	vnc.GetHardwareCryptoEngineFunc = vnctls.GetHardwareCryptoEngine

	server := GetVNCServer()
	server.SetPort(config.VNCPort)
	server.SetTLSEnabled(config.VNCUseTLS)

	vncLogger.Info().
		Int("port", config.VNCPort).
		Int("quality", config.VNCQuality).
		Bool("tls", config.VNCUseTLS).
		Bool("hwCrypto", vnctls.IsHardwareCryptoEnabled()).
		Str("hwEngine", vnctls.GetHardwareCryptoEngine()).
		Int("maxConnections", config.VNCMaxConnections).
		Msg("initializing VNC server")

	if err := server.Start(); err != nil {
		vncLogger.Error().Err(err).Msg("failed to start VNC server")
		return err
	}

	return nil
}

// BroadcastVNCJPEGFrame sends a JPEG frame to all VNC clients.
func BroadcastVNCJPEGFrame(frame []byte) {
	if vncServer != nil {
		vncServer.BroadcastJPEGFrame(frame)
	}
}

// UpdateVNCVideoState updates the VNC server with current video resolution.
func UpdateVNCVideoState(width, height uint16) {
	if vncServer != nil {
		vncServer.UpdateVideoState(width, height)
	}
}

// Compile-time interface checks
var (
	_ vnc.Config        = (*vncConfigAdapter)(nil)
	_ vnc.NativeEncoder = (*vncEncoderAdapter)(nil)
	_ vnc.HIDController = (*vncHIDAdapter)(nil)
	_ vnc.TLSProvider   = (*vncTLSAdapter)(nil)
)
