package meshvpn

import (
	"context"
	"sync"
	"time"
)

// StatusProvider is the interface for getting provider status, used by StatusMonitor.
type StatusProvider interface {
	GetStatus(ctx context.Context) (*ProviderStatus, error)
}

// StatusMonitor polls for status changes and reports them via callback.
type StatusMonitor struct {
	provider            StatusProvider
	onChange            StatusChangeFunc
	interval            time.Duration
	mu                  sync.Mutex
	cancel              context.CancelFunc
	running             bool
	lastStatus          *ProviderStatus
	consecutiveFailures int
	maxFailures         int
	logger              StatusMonitorLogger
}

// StatusMonitorLogger provides logging for the status monitor.
type StatusMonitorLogger interface {
	Warn(msg string, err error, failures int)
}

// StatusMonitorConfig contains configuration for the StatusMonitor.
type StatusMonitorConfig struct {
	Provider    StatusProvider
	OnChange    StatusChangeFunc
	Interval    time.Duration
	MaxFailures int
	Logger      StatusMonitorLogger
}

// NewStatusMonitor creates a new status monitor with the given configuration.
func NewStatusMonitor(cfg StatusMonitorConfig) *StatusMonitor {
	interval := cfg.Interval
	if interval == 0 {
		interval = 5 * time.Second
	}
	maxFailures := cfg.MaxFailures
	if maxFailures == 0 {
		maxFailures = 3
	}
	return &StatusMonitor{
		provider:    cfg.Provider,
		onChange:    cfg.OnChange,
		interval:    interval,
		maxFailures: maxFailures,
		logger:      cfg.Logger,
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
		onChange := m.onChange
		m.mu.Unlock()

		if m.logger != nil {
			m.logger.Warn("failed to get status", err, failures)
		}

		if failures >= m.maxFailures && onChange != nil {
			onChange(ProviderStatus{
				State:        StateError,
				ErrorMessage: "Status monitoring failed: " + err.Error(),
			})
		}
		return
	}

	m.mu.Lock()
	m.consecutiveFailures = 0
	changed := m.hasChanged(status)
	if changed {
		m.lastStatus = status
	}
	onChange := m.onChange
	m.mu.Unlock()

	if changed && onChange != nil {
		onChange(*status)
	}
}

func (m *StatusMonitor) hasChanged(current *ProviderStatus) bool {
	if m.lastStatus == nil {
		return true
	}

	last := m.lastStatus

	return current.Provider != last.Provider ||
		current.State != last.State ||
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
