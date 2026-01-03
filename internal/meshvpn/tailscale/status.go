//go:build linux

package tailscale

import (
	"context"
	"sync"
	"time"

	"github.com/jetkvm/kvm/internal/meshvpn"
)

// StatusMonitor polls for status changes and reports them via callback.
type StatusMonitor struct {
	provider            *Provider
	onChange            meshvpn.StatusChangeFunc
	interval            time.Duration
	mu                  sync.Mutex
	cancel              context.CancelFunc
	running             bool
	lastStatus          *meshvpn.ProviderStatus
	consecutiveFailures int
}

const maxConsecutiveFailures = 3

// NewStatusMonitor creates a new status monitor that polls every 5 seconds.
// Balances responsiveness with resource usage.
func NewStatusMonitor(provider *Provider, onChange meshvpn.StatusChangeFunc) *StatusMonitor {
	return &StatusMonitor{
		provider: provider,
		onChange: onChange,
		interval: 5 * time.Second,
	}
}

func (m *StatusMonitor) Start(ctx context.Context) {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return
	}
	m.running = true

	monitorCtx, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	m.mu.Unlock()

	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	m.checkStatus(monitorCtx)

	for {
		select {
		case <-monitorCtx.Done():
			m.mu.Lock()
			m.running = false
			m.mu.Unlock()
			return
		case <-ticker.C:
			m.checkStatus(monitorCtx)
		}
	}
}

func (m *StatusMonitor) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.running = false
}

func (m *StatusMonitor) checkStatus(ctx context.Context) {
	status, err := m.provider.GetStatus(ctx)
	if err != nil {
		m.mu.Lock()
		m.consecutiveFailures++
		failures := m.consecutiveFailures
		m.mu.Unlock()

		logger.Warn().Err(err).Int("consecutiveFailures", failures).Msg("failed to get status")

		// After repeated failures, report error state to UI so user knows monitoring is broken
		if failures >= maxConsecutiveFailures && m.onChange != nil {
			m.onChange(meshvpn.ProviderStatus{
				State:        meshvpn.StateError,
				ErrorMessage: "Status monitoring failed: " + err.Error(),
			})
		}
		return
	}

	m.mu.Lock()
	m.consecutiveFailures = 0 // Reset on success
	changed := m.hasChanged(status)
	if changed {
		m.lastStatus = status
	}
	m.mu.Unlock()

	if changed && m.onChange != nil {
		m.onChange(*status)
	}
}

func (m *StatusMonitor) hasChanged(current *meshvpn.ProviderStatus) bool {
	if m.lastStatus == nil {
		return true
	}

	last := m.lastStatus

	return current.State != last.State ||
		current.Installed != last.Installed ||
		current.Running != last.Running ||
		current.IP != last.IP ||
		current.Hostname != last.Hostname ||
		current.AuthURL != last.AuthURL ||
		current.ExitNode != last.ExitNode ||
		current.ControlServer != last.ControlServer ||
		current.BackendState != last.BackendState ||
		current.ErrorMessage != last.ErrorMessage ||
		current.Version != last.Version
}
