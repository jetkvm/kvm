//go:build linux

package tailscale

import (
	"github.com/jetkvm/kvm/internal/meshvpn"
)

// statusMonitorLogger adapts the package logger to the StatusMonitorLogger interface.
type statusMonitorLogger struct{}

func (l *statusMonitorLogger) Warn(msg string, err error, failures int) {
	logger.Warn().Err(err).Int("consecutiveFailures", failures).Msg(msg)
}

// StatusMonitor wraps the shared meshvpn.StatusMonitor for backward compatibility.
type StatusMonitor = meshvpn.StatusMonitor

// NewStatusMonitor creates a new status monitor that polls every 5 seconds.
func NewStatusMonitor(provider *Provider, onChange meshvpn.StatusChangeFunc) *StatusMonitor {
	return meshvpn.NewStatusMonitor(meshvpn.StatusMonitorConfig{
		Provider: provider,
		OnChange: onChange,
		Logger:   &statusMonitorLogger{},
	})
}
