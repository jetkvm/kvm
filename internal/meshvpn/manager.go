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
	mu               sync.RWMutex
	config           *Config
	registry         *Registry
	runningProviders map[string]Provider // Multiple providers can run simultaneously
	onStatusChange   StatusChangeFunc
	onConfigChange   OnConfigChangeFunc
	saveConfig       func(*Config) error
}

func NewManager(cfg ManagerConfig) *Manager {
	return &Manager{
		config:           cfg.Config,
		registry:         cfg.Registry,
		runningProviders: make(map[string]Provider),
		onStatusChange:   cfg.OnStatusChange,
		onConfigChange:   cfg.OnConfigChange,
		saveConfig:       cfg.SaveConfig,
	}
}

func (m *Manager) GetConfig() *Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config
}

func (m *Manager) SetConfig(config *Config) error {
	m.mu.Lock()
	m.config = config

	if m.saveConfig != nil {
		if err := m.saveConfig(config); err != nil {
			m.mu.Unlock()
			return err
		}
	}

	onChange := m.onConfigChange
	m.mu.Unlock()

	if onChange != nil {
		onChange(config)
	}

	return nil
}

// GetActiveProvider returns a running provider for backward compatibility.
// If multiple providers are running, returns the first one found.
// Prefer using GetRunningProvider(name) for explicit provider access.
func (m *Manager) GetActiveProvider() Provider {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, p := range m.runningProviders {
		return p
	}
	return nil
}

// GetRunningProvider returns a specific running provider by name.
func (m *Manager) GetRunningProvider(name string) Provider {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.runningProviders[name]
}

// GetRunningProviders returns all currently running providers.
func (m *Manager) GetRunningProviders() []Provider {
	m.mu.RLock()
	defer m.mu.RUnlock()
	providers := make([]Provider, 0, len(m.runningProviders))
	for _, p := range m.runningProviders {
		providers = append(providers, p)
	}
	return providers
}

// trackRunningProvider adds a provider to the running set.
func (m *Manager) trackRunningProvider(p Provider) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runningProviders[p.Name()] = p
}

// untrackRunningProvider removes a provider from the running set.
func (m *Manager) untrackRunningProvider(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.runningProviders, name)
}

func (m *Manager) GetStatus(ctx context.Context) (*ProviderStatus, error) {
	provider := m.GetActiveProvider()
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

// GetProvider returns a provider by name. Returns (nil, false) if not found.
func (m *Manager) GetProvider(name string) (Provider, bool) {
	return m.registry.Get(name)
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

// ConnectProvider connects a specific provider by name.
func (m *Manager) ConnectProvider(ctx context.Context, name string, opts ConnectOptions) (*ConnectResult, error) {
	provider, ok := m.registry.Get(name)
	if !ok {
		return nil, ErrProviderNotFound
	}

	result, err := provider.Connect(ctx, opts)
	if err != nil {
		return nil, err
	}

	m.trackRunningProvider(provider)
	return result, nil
}

// Connect connects the first available provider (backward compatibility).
func (m *Manager) Connect(ctx context.Context, opts ConnectOptions) (*ConnectResult, error) {
	provider := m.GetActiveProvider()
	if provider == nil {
		return nil, ErrNoActiveProvider
	}

	result, err := provider.Connect(ctx, opts)
	if err != nil {
		return nil, err
	}

	m.trackRunningProvider(provider)
	return result, nil
}

// DisconnectProvider disconnects a specific provider by name.
func (m *Manager) DisconnectProvider(ctx context.Context, name string) error {
	provider, ok := m.registry.Get(name)
	if !ok {
		return ErrProviderNotFound
	}

	err := provider.Disconnect(ctx)
	m.untrackRunningProvider(name)
	return err
}

// Disconnect disconnects the first running provider (backward compatibility).
func (m *Manager) Disconnect(ctx context.Context) error {
	provider := m.GetActiveProvider()
	if provider == nil {
		return ErrNoActiveProvider
	}

	err := provider.Disconnect(ctx)
	m.untrackRunningProvider(provider.Name())
	return err
}

// LogoutProvider logs out a specific provider by name.
func (m *Manager) LogoutProvider(ctx context.Context, name string) error {
	provider, ok := m.registry.Get(name)
	if !ok {
		return ErrProviderNotFound
	}

	err := provider.Logout(ctx)
	m.untrackRunningProvider(name)
	return err
}

// Logout logs out the first running provider (backward compatibility).
func (m *Manager) Logout(ctx context.Context) error {
	provider := m.GetActiveProvider()
	if provider == nil {
		return ErrNoActiveProvider
	}

	err := provider.Logout(ctx)
	m.untrackRunningProvider(provider.Name())
	return err
}

func (m *Manager) ListExitNodes(ctx context.Context) ([]ExitNode, error) {
	provider := m.GetActiveProvider()
	if provider == nil {
		return nil, ErrNoActiveProvider
	}
	return provider.ListExitNodes(ctx)
}

// ListExitNodesForProvider lists exit nodes for a specific provider by name.
func (m *Manager) ListExitNodesForProvider(ctx context.Context, providerName string) ([]ExitNode, error) {
	provider, ok := m.registry.Get(providerName)
	if !ok {
		return nil, ErrProviderNotFound
	}
	return provider.ListExitNodes(ctx)
}

func (m *Manager) SetExitNode(ctx context.Context, hostname string, allowLAN bool) error {
	provider := m.GetActiveProvider()
	if provider == nil {
		return ErrNoActiveProvider
	}
	return provider.SetExitNode(ctx, hostname, allowLAN)
}

func (m *Manager) ClearExitNode(ctx context.Context) error {
	provider := m.GetActiveProvider()
	if provider == nil {
		return ErrNoActiveProvider
	}
	return provider.ClearExitNode(ctx)
}

// StartStatusMonitor starts monitoring all running providers.
func (m *Manager) StartStatusMonitor(ctx context.Context) {
	m.mu.RLock()
	onChange := m.onStatusChange
	m.mu.RUnlock()

	if onChange == nil {
		return
	}

	for _, p := range m.GetRunningProviders() {
		providerName := p.Name()
		// Wrap the callback to include the provider name in status updates
		wrappedOnChange := func(status ProviderStatus) {
			status.Provider = providerName
			onChange(status)
		}
		p.StartStatusMonitor(ctx, wrappedOnChange)
	}
}

// StartProviderStatusMonitor starts monitoring a specific provider.
func (m *Manager) StartProviderStatusMonitor(ctx context.Context, name string) {
	m.mu.RLock()
	onChange := m.onStatusChange
	m.mu.RUnlock()

	if onChange == nil {
		return
	}

	provider, ok := m.registry.Get(name)
	if ok {
		// Wrap the callback to include the provider name in status updates
		wrappedOnChange := func(status ProviderStatus) {
			status.Provider = name
			onChange(status)
		}
		provider.StartStatusMonitor(ctx, wrappedOnChange)
	}
}

// StopStatusMonitor stops monitoring all running providers.
func (m *Manager) StopStatusMonitor() {
	for _, p := range m.GetRunningProviders() {
		p.StopStatusMonitor()
	}
}

// StopProviderStatusMonitor stops monitoring a specific provider.
func (m *Manager) StopProviderStatusMonitor(name string) {
	provider, ok := m.registry.Get(name)
	if ok {
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
	case "zerotier":
		if m.config.ZeroTier == nil {
			m.config.ZeroTier = &ZeroTierConfig{}
		}
		m.config.ZeroTier.Enabled = true
		if controlServer != "" {
			m.config.ZeroTier.NetworkID = controlServer
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
	case "zerotier":
		if m.config.ZeroTier != nil {
			m.config.ZeroTier.Enabled = false
		}
	}

	if m.saveConfig != nil {
		return m.saveConfig(m.config)
	}

	return nil
}

// AutoStart restores VPN state after reboot by starting all enabled providers.
// Connect errors are logged but not returned so status monitoring can still start.
func (m *Manager) AutoStart(ctx context.Context) error {
	m.mu.RLock()
	config := m.config
	m.mu.RUnlock()

	if config == nil {
		return nil
	}

	providers := m.registry.List()
	for _, name := range providers {
		if !config.IsProviderEnabled(name) {
			continue
		}

		provider, ok := m.registry.Get(name)
		if !ok || !provider.IsInstalled() {
			continue
		}

		logger.Info().
			Str("provider", name).
			Msg("auto-starting mesh VPN provider")

		var opts ConnectOptions
		switch name {
		case "tailscale":
			if config.Tailscale != nil {
				opts.ControlServer = config.Tailscale.ControlServer
				opts.AuthKey = config.Tailscale.AuthKey
			}
		case "zerotier":
			if config.ZeroTier != nil {
				opts.ControlServer = config.ZeroTier.NetworkID
			}
		}

		_, err := provider.Connect(ctx, opts)
		if err != nil {
			logger.Error().
				Err(err).
				Str("provider", name).
				Msg("failed to auto-start provider")
			continue
		}

		m.trackRunningProvider(provider)
		m.StartProviderStatusMonitor(ctx, name)
	}

	return nil
}

func (m *Manager) getProvider(name string) (Provider, error) {
	if name != "" {
		provider, ok := m.registry.Get(name)
		if !ok {
			return nil, ErrProviderNotFound
		}
		return provider, nil
	}

	provider := m.GetActiveProvider()
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
