package kvm

import (
	"net/http"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/jetkvm/kvm/internal/serialnet"
	"go.bug.st/serial"
)

// Network access to the extension port UART (jetkvm/kvm#1520). The server
// lives in internal/serialnet; this file connects it to the port, the
// Serial Console settings and the web server. UART reads reach it through
// serialRx, which SerialMux's reader feeds, so the mux stays the only reader
// and writer of the port.

// normalizeNetwork fills in defaults for settings files written before
// network access existed.
func (s *SerialSettings) normalizeNetwork() {
	n := serialNetworkSettingsFrom(*s)
	n.Normalize()
	s.NetworkMode, s.Ser2NetPort, s.Ser2NetProtocol, s.NetworkMaxClients = n.Mode, n.Port, n.Protocol, n.MaxClients
}

func (s *SerialSettings) validateNetwork() error {
	return serialNetworkSettingsFrom(*s).Validate()
}

func serialNetworkSettingsFrom(s SerialSettings) serialnet.Settings {
	return serialnet.Settings{
		Mode:       s.NetworkMode,
		Port:       s.Ser2NetPort,
		Protocol:   s.Ser2NetProtocol,
		MaxClients: s.NetworkMaxClients,
	}
}

var (
	serialRx      = serialnet.NewHub()
	serialNetwork = newDeviceSerialNetServer(serialRx)
)

// newDeviceSerialNetServer wires the server to the extension port. The web
// server takes RPCs before initSerialPort runs, so this can't wait for it.
func newDeviceSerialNetServer(hub *serialnet.Hub) *serialnet.Server {
	return serialnet.NewServer(hub, serialnet.Deps{
		Write: func(p []byte, source string) {
			if m := serialMux; m != nil {
				m.Enqueue(p, source, true, TXUser)
			}
		},
		LineMode:    func() serial.Mode { return *serialPortMode },
		SetLineMode: func(m serial.Mode) error { return setSerialPortMode(&m) },
		SendBreak: func() {
			if port == nil {
				return
			}
			if err := port.Break(serialnet.BreakDuration); err != nil {
				serialLogger.Warn().Err(err).Msg("failed to send serial break")
			}
		},
		BindAddr: getBindAddress,
	})
}

func applySerialNetwork(settings SerialSettings) error {
	return serialNetwork.Apply(
		config.ActiveExtension == "serial-console",
		serialNetworkSettingsFrom(settings),
	)
}

func mountSerialConsole() error {
	// Load the stored UART settings now rather than when the UI next opens,
	// so a network client gets the configured line settings after boot.
	settings, err := getSerialSettings()
	if err != nil {
		serialLogger.Warn().Err(err).Msg("failed to load serial settings; using defaults")
	}
	if err := applySerialNetwork(settings); err != nil {
		serialLogger.Error().Err(err).Msg("failed to start serial network access")
		return err
	}
	return nil
}

func unmountSerialConsole() error {
	serialNetwork.Stop()
	return nil
}

func rpcGetSerialNetworkStatus() (serialnet.Status, error) {
	return serialNetwork.Status(), nil
}

// handleSerialWebSocket backs the /serial web console: binary frames carry
// raw UART bytes in both directions.
func handleSerialWebSocket(c *gin.Context) {
	if st := serialNetwork.Status(); st.Mode != serialnet.ModeWeb || !st.Running {
		c.JSON(http.StatusNotFound, gin.H{"error": "Serial web console is not enabled"})
		return
	}

	// Default options: the Origin must match the Host, since the auth
	// cookie would otherwise let any page open the UART.
	ws, err := websocket.Accept(c.Writer, c.Request, nil)
	if err != nil {
		serialLogger.Warn().Err(err).Msg("failed to accept serial websocket")
		return
	}
	defer func() { _ = ws.CloseNow() }()

	serialNetwork.ServeWeb(c.Request.Context(), ws, c.ClientIP())
}
