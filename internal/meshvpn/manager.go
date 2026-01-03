package meshvpn

import (
	"context"
	"sync"
)

type OnConfigChangeFunc func(config *Config)

type ManagerConfig struct {
	Config         *Config
	Registry       *Registry
	OnStatusChange StatusChangeFunc
	OnConfigChange OnConfigChangeFunc
	SaveConfig     func(*Config) error
}

type Manager struct {
	mu             sync.RWMutex
	config         *Config
	registry       *Registry
	activeProvider Provider
	onStatusChange StatusChangeFunc
	onConfigChange OnConfigChangeFunc
	saveConfig     func(*Config) error
}

func NewManager(cfg ManagerConfig) *Manager {
	m := &Manager{
		config:         cfg.Config,
		registry:       cfg.Registry,
		onStatusChange: cfg.OnStatusChange,
		onConfigChange: cfg.OnConfigChange,
		saveConfig:     cfg.SaveConfig,
	}

	if cfg.Config != nil && cfg.Config.ActiveProvider != "" {
		if provider, ok := cfg.Registry.Get(cfg.Config.ActiveProvider); ok {
			m.activeProvider = provider
		}
	}

	return m
}

func (m *Manager) GetConfig() *Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config
}

func (m *Manager) SetConfig(config *Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.config = config

	if config != nil && config.ActiveProvider != "" {
		if provider, ok := m.registry.Get(config.ActiveProvider); ok {
			m.activeProvider = provider
		}
	} else {
		m.activeProvider = nil
	}

	if m.saveConfig != nil {
		if err := m.saveConfig(config); err != nil {
			return err
		}
	}

	if m.onConfigChange != nil {
		m.onConfigChange(config)
	}

	return nil
}

func (m *Manager) GetActiveProvider() Provider {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.activeProvider
}

func (m *Manager) SetActiveProvider(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if name == "" {
		m.activeProvider = nil
		if m.config != nil {
			m.config.ActiveProvider = ""
		}
		return nil
	}

	provider, ok := m.registry.Get(name)
	if !ok {
		return ErrProviderNotFound
	}

	m.activeProvider = provider
	if m.config == nil {
		m.config = &Config{}
	}
	m.config.ActiveProvider = name

	return nil
}

func (m *Manager) GetStatus(ctx context.Context) (*ProviderStatus, error) {
	m.mu.RLock()
	provider := m.activeProvider
	m.mu.RUnlock()

	if provider == nil {
		return nil, ErrNoActiveProvider
	}

	return provider.GetStatus(ctx)
}

func (m *Manager) GetProviderStatus(ctx context.Context, name string) (*ProviderStatus, error) {
	provider, ok := m.registry.Get(name)
	if !ok {
		return nil, ErrProviderNotFound
	}

	return provider.GetStatus(ctx)
}

func (m *Manager) Install(ctx context.Context, name string, progress ProgressFunc) error {
	provider, ok := m.registry.Get(name)
	if !ok {
		return ErrProviderNotFound
	}

	return provider.Install(ctx, progress)
}

func (m *Manager) Uninstall(ctx context.Context, name string) error {
	provider, ok := m.registry.Get(name)
	if !ok {
		return ErrProviderNotFound
	}

	return provider.Uninstall(ctx)
}

func (m *Manager) Connect(ctx context.Context, opts ConnectOptions) (*ConnectResult, error) {
	m.mu.RLock()
	provider := m.activeProvider
	m.mu.RUnlock()

	if provider == nil {
		return nil, ErrNoActiveProvider
	}

	return provider.Connect(ctx, opts)
}

func (m *Manager) Disconnect(ctx context.Context) error {
	m.mu.RLock()
	provider := m.activeProvider
	m.mu.RUnlock()

	if provider == nil {
		return ErrNoActiveProvider
	}

	return provider.Disconnect(ctx)
}

func (m *Manager) Logout(ctx context.Context) error {
	m.mu.RLock()
	provider := m.activeProvider
	m.mu.RUnlock()

	if provider == nil {
		return ErrNoActiveProvider
	}

	return provider.Logout(ctx)
}

func (m *Manager) ListExitNodes(ctx context.Context) ([]ExitNode, error) {
	m.mu.RLock()
	provider := m.activeProvider
	m.mu.RUnlock()

	if provider == nil {
		return nil, ErrNoActiveProvider
	}

	return provider.ListExitNodes(ctx)
}

func (m *Manager) SetExitNode(ctx context.Context, hostname string, allowLAN bool) error {
	m.mu.RLock()
	provider := m.activeProvider
	m.mu.RUnlock()

	if provider == nil {
		return ErrNoActiveProvider
	}

	return provider.SetExitNode(ctx, hostname, allowLAN)
}

func (m *Manager) ClearExitNode(ctx context.Context) error {
	m.mu.RLock()
	provider := m.activeProvider
	m.mu.RUnlock()

	if provider == nil {
		return ErrNoActiveProvider
	}

	return provider.ClearExitNode(ctx)
}

func (m *Manager) StartStatusMonitor(ctx context.Context) {
	m.mu.RLock()
	provider := m.activeProvider
	onChange := m.onStatusChange
	m.mu.RUnlock()

	if provider == nil || onChange == nil {
		return
	}

	provider.StartStatusMonitor(ctx, onChange)
}

func (m *Manager) StopStatusMonitor() {
	m.mu.RLock()
	provider := m.activeProvider
	m.mu.RUnlock()

	if provider != nil {
		provider.StopStatusMonitor()
	}
}

func (m *Manager) ListProviders() []ProviderInfo {
	return m.registry.ListProviderInfo()
}

// EnableProvider marks a provider as enabled for auto-start on boot.
func (m *Manager) EnableProvider(name string, controlServer string, authKey string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.config == nil {
		m.config = &Config{}
	}

	m.config.ActiveProvider = name

	switch name {
	case "tailscale":
		if m.config.Tailscale == nil {
			m.config.Tailscale = &TailscaleConfig{}
		}
		m.config.Tailscale.Enabled = true
		if controlServer != "" {
			m.config.Tailscale.ControlServer = controlServer
		}
		if authKey != "" {
			m.config.Tailscale.AuthKey = authKey
		}
	}

	if m.saveConfig != nil {
		return m.saveConfig(m.config)
	}

	return nil
}

func (m *Manager) DisableProvider(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.config == nil {
		return nil
	}

	switch name {
	case "tailscale":
		if m.config.Tailscale != nil {
			m.config.Tailscale.Enabled = false
		}
	}

	if m.saveConfig != nil {
		return m.saveConfig(m.config)
	}

	return nil
}

// AutoStart restores VPN state after reboot by starting enabled providers.
// Connect errors are logged but not returned so status monitoring can still start.
func (m *Manager) AutoStart(ctx context.Context) error {
	m.mu.RLock()
	config := m.config
	provider := m.activeProvider
	m.mu.RUnlock()

	if provider == nil || config == nil {
		return nil
	}

	if !provider.IsInstalled() {
		return nil
	}

	if !config.IsProviderEnabled(provider.Name()) {
		return nil
	}

	logger.Info().
		Str("provider", provider.Name()).
		Msg("auto-starting mesh VPN provider")

	var opts ConnectOptions
	if config.Tailscale != nil && provider.Name() == "tailscale" {
		opts.ControlServer = config.Tailscale.ControlServer
		opts.AuthKey = config.Tailscale.AuthKey
	}

	_, err := provider.Connect(ctx, opts)
	if err != nil {
		logger.Error().
			Err(err).
			Str("provider", provider.Name()).
			Msg("failed to auto-start provider")
	}

	m.StartStatusMonitor(ctx)

	return nil
}

type VersionInfoProvider interface {
	GetVersionInfo(ctx context.Context) (*VersionInfo, error)
}

type UpdateProvider interface {
	Update(ctx context.Context, targetVersion string, progress ProgressFunc) error
}

func (m *Manager) getProvider(name string) (Provider, error) {
	if name != "" {
		provider, ok := m.registry.Get(name)
		if !ok {
			return nil, ErrProviderNotFound
		}
		return provider, nil
	}

	m.mu.RLock()
	provider := m.activeProvider
	m.mu.RUnlock()

	if provider == nil {
		return nil, ErrNoActiveProvider
	}
	return provider, nil
}

func (m *Manager) GetVersionInfo(ctx context.Context, providerName string) (*VersionInfo, error) {
	provider, err := m.getProvider(providerName)
	if err != nil {
		return nil, err
	}

	vip, ok := provider.(VersionInfoProvider)
	if !ok {
		return &VersionInfo{}, nil
	}

	return vip.GetVersionInfo(ctx)
}

func (m *Manager) Update(ctx context.Context, providerName string, targetVersion string, progress ProgressFunc) error {
	provider, err := m.getProvider(providerName)
	if err != nil {
		return err
	}

	up, ok := provider.(UpdateProvider)
	if !ok {
		return ErrNotSupported
	}

	return up.Update(ctx, targetVersion, progress)
}

func (m *Manager) GetTUNMode(providerName string) (TUNMode, error) {
	provider, err := m.getProvider(providerName)
	if err != nil {
		return "", err
	}

	tmp, ok := provider.(TUNModeProvider)
	if !ok {
		return TUNModeUserspace, nil
	}

	return tmp.GetTUNMode(), nil
}

func (m *Manager) SetTUNMode(ctx context.Context, providerName string, mode TUNMode) error {
	provider, err := m.getProvider(providerName)
	if err != nil {
		return err
	}

	tmp, ok := provider.(TUNModeProvider)
	if !ok {
		return ErrNotSupported
	}

	return tmp.SetTUNMode(ctx, mode)
}

func (m *Manager) SetAdvertiseExitNode(ctx context.Context, providerName string, advertise bool) error {
	provider, err := m.getProvider(providerName)
	if err != nil {
		return err
	}

	ena, ok := provider.(ExitNodeAdvertiser)
	if !ok {
		return ErrNotSupported
	}

	return ena.SetAdvertiseExitNode(ctx, advertise)
}
