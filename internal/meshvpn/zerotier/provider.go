//go:build linux

package zerotier

import (
	"context"
	"fmt"
	"os"
	"sync"
	"syscall"
	"time"

	"github.com/jetkvm/kvm/internal/meshvpn"
)

// Provider implements the meshvpn.Provider interface for ZeroTier.
type Provider struct {
	mu            sync.RWMutex
	version       string
	process       *ProcessManager
	statusMonitor *StatusMonitor
	httpClient    meshvpn.HTTPClient
	versionClient *VersionClient
}

// ProviderConfig contains configuration for creating a Provider.
type ProviderConfig struct {
	Version    string
	HTTPClient meshvpn.HTTPClient
}

// NewProvider creates a new ZeroTier provider.
func NewProvider(cfg ProviderConfig) *Provider {
	version := cfg.Version
	if version == "" {
		version = DefaultVersion
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = meshvpn.NewDefaultHTTPClient()
	}

	return &Provider{
		version:       version,
		httpClient:    httpClient,
		versionClient: NewVersionClient(httpClient),
	}
}

func (p *Provider) Name() string               { return ProviderName }
func (p *Provider) DisplayName() string        { return ProviderDisplayName }
func (p *Provider) SupportsExitNodes() bool    { return false } // ZeroTier is L2 mesh, no exit nodes
func (p *Provider) SupportsCustomServer() bool { return false } // Uses network IDs, not server URLs
func (p *Provider) SupportsAuthKey() bool      { return false } // Authorization via controller

func (p *Provider) IsInstalled() bool {
	_, err := os.Stat(ZeroTierOnePath)
	if err == nil {
		return true
	}
	if !os.IsNotExist(err) {
		logger.Warn().Err(err).Str("path", ZeroTierOnePath).Msg("unexpected error checking installation")
	}
	return false
}

func (p *Provider) Install(ctx context.Context, progress meshvpn.ProgressFunc) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.IsInstalled() {
		return meshvpn.ErrAlreadyInstalled
	}

	// Try to fetch the latest version, fall back to hardcoded default
	version := p.version
	if p.versionClient != nil {
		latestVersion, err := p.versionClient.GetLatestVersion(ctx)
		if err != nil {
			logger.Warn().Err(err).Str("fallback", version).Msg("failed to fetch latest version, using fallback")
		} else if latestVersion != "" {
			version = latestVersion
			logger.Info().Str("version", version).Msg("installing latest ZeroTier version")
		}
	}

	downloader := &Downloader{
		version:    version,
		httpClient: p.httpClient,
	}

	return downloader.Install(ctx, progress)
}

func (p *Provider) Uninstall(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.IsInstalled() {
		return meshvpn.ErrNotInstalled
	}

	// Stop the daemon before uninstalling
	if p.process != nil && p.process.IsRunning() {
		if err := p.process.Stop(); err != nil {
			return fmt.Errorf("cannot uninstall while daemon is running: %w", err)
		}
	}

	// Stop any orphaned daemon from a previous session
	if err := p.stopOrphanedDaemon(); err != nil {
		return fmt.Errorf("cannot uninstall: %w", err)
	}

	if err := os.RemoveAll(InstallBasePath); err != nil {
		return err
	}

	logger.Info().Msg("ZeroTier uninstalled")
	return nil
}

// stopOrphanedDaemon stops a daemon that was started in a previous session.
// This handles the case where the app was restarted but the daemon is still running.
func (p *Provider) stopOrphanedDaemon() error {
	pidData, err := os.ReadFile(PidFilePath)
	if err != nil {
		return nil // No PID file, nothing to stop
	}

	var pid int
	if _, parseErr := fmt.Sscanf(string(pidData), "%d", &pid); parseErr != nil || pid <= 0 {
		return nil
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return nil
	}

	// Check if process is actually running
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		return nil // Process not running
	}

	logger.Info().Int("pid", pid).Msg("stopping orphaned zerotier-one")

	// Try graceful SIGTERM first
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		if killErr := proc.Kill(); killErr != nil {
			return fmt.Errorf("failed to kill orphaned daemon (pid %d): %w", pid, killErr)
		}
	}

	// Wait for process to exit
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := proc.Signal(syscall.Signal(0)); err != nil {
			return nil // Process exited
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Force kill if still running
	if err := proc.Signal(syscall.Signal(0)); err == nil {
		if killErr := proc.Kill(); killErr != nil {
			return fmt.Errorf("failed to force kill orphaned daemon (pid %d): %w", pid, killErr)
		}
	}

	return nil
}

// Connect starts the ZeroTier daemon and joins a network.
// For ZeroTier, opts.ControlServer is repurposed as the network ID (16-digit hex).
func (p *Provider) Connect(ctx context.Context, opts meshvpn.ConnectOptions) (*meshvpn.ConnectResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.IsInstalled() {
		return nil, meshvpn.ErrNotInstalled
	}

	if p.process == nil {
		p.process = NewProcessManager()
	}

	if !p.process.IsRunning() {
		if err := p.process.Start(); err != nil {
			return nil, err
		}
	}

	// Wait for daemon to be ready
	time.Sleep(2 * time.Second)

	cli := NewCLI()

	// If a network ID is provided, join it
	networkID := opts.ControlServer
	if networkID != "" {
		if err := cli.Join(ctx, networkID); err != nil {
			return nil, fmt.Errorf("failed to join network %s: %w", networkID, err)
		}
		logger.Info().Str("networkID", networkID).Msg("joined ZeroTier network")
	}

	// Get status to check if we're waiting for authorization
	var status *StatusResponse
	var err error
	for i := 0; i < 5; i++ {
		time.Sleep(1 * time.Second)

		status, err = cli.Status(ctx)
		if err != nil {
			logger.Warn().Err(err).Int("attempt", i+1).Msg("failed to get status, retrying")
			continue
		}

		if status.Online {
			break
		}
	}

	if err != nil {
		return nil, fmt.Errorf("connected but failed to verify status: %w", err)
	}

	// Check network authorization status
	networks, netErr := cli.ListNetworks(ctx)
	if netErr == nil && len(networks) > 0 {
		for _, net := range networks {
			if net.Status == "ACCESS_DENIED" {
				logger.Info().
					Str("networkID", net.NetworkID).
					Msg("network authorization pending")
				// Return empty result - user needs to authorize on ZeroTier controller
				return &meshvpn.ConnectResult{}, nil
			}
		}
	}

	logger.Info().Msg("ZeroTier connect completed")

	return &meshvpn.ConnectResult{}, nil
}

func (p *Provider) Disconnect(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.IsInstalled() {
		return meshvpn.ErrNotInstalled
	}

	// Stop the daemon but preserve network membership (networks.d/ files remain)
	// On next Connect, daemon will auto-rejoin networks
	if p.process != nil && p.process.IsRunning() {
		if err := p.process.Stop(); err != nil {
			return err
		}
		logger.Info().Msg("ZeroTier disconnected")
		return nil
	}

	// Handle orphaned daemon from a previous session
	if err := p.stopOrphanedDaemon(); err != nil {
		return err
	}

	logger.Info().Msg("ZeroTier disconnected")
	return nil
}

func (p *Provider) Logout(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.IsInstalled() {
		return meshvpn.ErrNotInstalled
	}

	cli := NewCLI()

	// Leave all networks
	networks, err := cli.ListNetworks(ctx)
	if err != nil {
		logger.Warn().Err(err).Msg("failed to list networks for logout")
	} else {
		for _, net := range networks {
			if err := cli.Leave(ctx, net.NetworkID); err != nil {
				logger.Warn().Err(err).Str("networkID", net.NetworkID).Msg("failed to leave network")
			}
		}
	}

	// Stop the daemon (tracked process)
	if p.process != nil && p.process.IsRunning() {
		if err := p.process.Stop(); err != nil {
			return fmt.Errorf("failed to stop daemon after logout: %w", err)
		}
	}

	// Handle orphaned daemon from a previous session
	if err := p.stopOrphanedDaemon(); err != nil {
		return fmt.Errorf("failed to stop orphaned daemon after logout: %w", err)
	}

	// Remove identity files to fully logout
	var removeErrors []string
	for _, path := range []string{IdentityPublicPath, IdentitySecretPath, AuthTokenPath} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			logger.Warn().Err(err).Str("path", path).Msg("failed to remove identity file during logout")
			removeErrors = append(removeErrors, path)
		}
	}
	if len(removeErrors) > 0 {
		return fmt.Errorf("logout incomplete: failed to remove %d identity file(s)", len(removeErrors))
	}

	logger.Info().Msg("ZeroTier logged out")
	return nil
}

func (p *Provider) GetStatus(ctx context.Context) (*meshvpn.ProviderStatus, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	status := &meshvpn.ProviderStatus{
		State: meshvpn.StateNotInstalled,
	}

	if !p.IsInstalled() {
		return status, nil
	}

	status.Installed = true
	status.State = meshvpn.StateStopped
	status.Version = p.version

	processRunning := p.process != nil && p.process.IsRunning()
	if processRunning {
		status.Running = true
		status.State = meshvpn.StateConnecting
	}

	cli := NewCLI()
	cliStatus, err := cli.Status(ctx)
	if err != nil {
		logger.Info().
			Bool("processRunning", processRunning).
			Str("state", string(status.State)).
			Err(err).
			Msg("CLI status failed, returning current state")
		if processRunning {
			status.State = meshvpn.StateError
			status.ErrorMessage = err.Error()
		}
		return status, nil
	}

	// CLI worked, so daemon is definitely running (even if p.process is nil after app restart)
	status.Running = true

	if cliStatus.Version != "" {
		status.Version = cliStatus.Version
	}

	status.Hostname = cliStatus.Address

	ip, ipErr := cli.GetPrimaryIP(ctx)
	if ipErr != nil {
		logger.Debug().Err(ipErr).Msg("failed to get primary IP")
	} else if ip != "" {
		status.IP = ip
	}

	// Check network status
	networks, netErr := cli.ListNetworks(ctx)
	if netErr == nil {
		hasConnectedNetwork := false
		hasPendingNetwork := false

		for _, net := range networks {
			switch net.Status {
			case "OK":
				hasConnectedNetwork = true
			case "ACCESS_DENIED":
				hasPendingNetwork = true
			}
		}

		if hasConnectedNetwork {
			status.State = meshvpn.StateConnected
		} else if hasPendingNetwork {
			status.State = meshvpn.StateNeedsAuth
		} else if cliStatus.Online {
			// Online but no networks joined - daemon is running and connected to ZT infrastructure
			status.State = meshvpn.StateConnected
		}
	} else {
		// Failed to get networks - if CLI worked but networks failed, check online status
		if cliStatus.Online {
			status.State = meshvpn.StateConnected
		}
	}

	return status, nil
}

func (p *Provider) StartStatusMonitor(ctx context.Context, onChange meshvpn.StatusChangeFunc) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.statusMonitor != nil {
		p.statusMonitor.Stop()
	}

	p.statusMonitor = NewStatusMonitor(p, onChange)
	go p.statusMonitor.Start(ctx)
}

func (p *Provider) StopStatusMonitor() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.statusMonitor != nil {
		p.statusMonitor.Stop()
		p.statusMonitor = nil
	}
}

// ZeroTier doesn't support exit nodes - these return ErrNotSupported
func (p *Provider) ListExitNodes(_ context.Context) ([]meshvpn.ExitNode, error) {
	return nil, meshvpn.ErrNotSupported
}

func (p *Provider) SetExitNode(_ context.Context, _ string, _ bool) error {
	return meshvpn.ErrNotSupported
}

func (p *Provider) ClearExitNode(_ context.Context) error {
	return meshvpn.ErrNotSupported
}

func (p *Provider) GetInfo() meshvpn.ProviderInfo {
	return meshvpn.ProviderInfo{
		Name:                 ProviderName,
		DisplayName:          ProviderDisplayName,
		Installed:            p.IsInstalled(),
		SupportsExitNodes:    false,
		SupportsCustomServer: false,
		SupportsAuthKey:      false,
	}
}

func (p *Provider) GetVersionInfo(ctx context.Context) (*meshvpn.VersionInfo, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	result := &meshvpn.VersionInfo{}

	if p.IsInstalled() {
		cli := NewCLI()
		version, err := cli.Version(ctx)
		if err != nil {
			logger.Warn().Err(err).Msg("failed to get current ZeroTier version")
		} else if version != "" {
			result.CurrentVersion = version
		}
	}

	// Check for latest version from GitHub
	if p.versionClient != nil {
		latestVersion, err := p.versionClient.GetLatestVersion(ctx)
		if err != nil {
			logger.Warn().Err(err).Msg("failed to get latest ZeroTier version")
		} else {
			result.LatestVersion = latestVersion
			if result.CurrentVersion != "" {
				result.UpdateAvailable = IsNewerVersion(result.CurrentVersion, result.LatestVersion)
			}
		}
	}

	return result, nil
}

func (p *Provider) Update(ctx context.Context, targetVersion string, progress meshvpn.ProgressFunc) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.IsInstalled() {
		return meshvpn.ErrNotInstalled
	}

	if targetVersion == "" {
		if p.versionClient == nil {
			return fmt.Errorf("version client not initialized")
		}
		latest, err := p.versionClient.GetLatestVersion(ctx)
		if err != nil {
			return fmt.Errorf("failed to get latest version: %w", err)
		}
		targetVersion = latest
	}

	cli := NewCLI()
	currentVersion, err := cli.Version(ctx)
	if err != nil {
		logger.Warn().Err(err).Msg("cannot determine current version, proceeding with update")
	} else if currentVersion == targetVersion {
		logger.Info().Str("version", currentVersion).Msg("already on target version")
		return nil
	}

	logger.Info().
		Str("currentVersion", currentVersion).
		Str("targetVersion", targetVersion).
		Msg("updating ZeroTier")

	wasRunning := p.process != nil && p.process.IsRunning()
	if wasRunning {
		if err := p.process.Stop(); err != nil {
			return fmt.Errorf("cannot update while daemon is running: %w", err)
		}
	}

	downloader := &Downloader{
		version:    targetVersion,
		httpClient: p.httpClient,
	}

	if err := downloader.Install(ctx, progress); err != nil {
		return fmt.Errorf("failed to install update: %w", err)
	}

	p.version = targetVersion

	if wasRunning {
		if p.process == nil {
			p.process = NewProcessManager()
		}
		if err := p.process.Start(); err != nil {
			return fmt.Errorf("failed to restart zerotier-one after update: %w", err)
		}
	}

	logger.Info().Str("version", targetVersion).Msg("ZeroTier updated successfully")
	return nil
}
