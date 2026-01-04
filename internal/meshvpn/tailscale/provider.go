//go:build linux

package tailscale

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/jetkvm/kvm/internal/meshvpn"
)

type Provider struct {
	mu            sync.RWMutex
	version       string
	tunMode       meshvpn.TUNMode
	process       *ProcessManager
	statusMonitor *StatusMonitor
	httpClient    HTTPClient
	versionClient *VersionClient
	connectCancel context.CancelFunc
}

type HTTPClient interface {
	Get(url string) ([]byte, error)
	Download(url string, dest string, progress meshvpn.ProgressFunc) error
}

type ProviderConfig struct {
	Version    string
	TUNMode    meshvpn.TUNMode
	HTTPClient HTTPClient
}

func NewProvider(cfg ProviderConfig) *Provider {
	version := cfg.Version
	if version == "" {
		version = DefaultVersion
	}

	tunMode := cfg.TUNMode
	if tunMode == "" {
		tunMode = meshvpn.TUNModeUserspace
	}

	var versionClient *VersionClient
	if cfg.HTTPClient != nil {
		versionClient = NewVersionClient(cfg.HTTPClient)
	}

	return &Provider{
		version:       version,
		tunMode:       tunMode,
		httpClient:    cfg.HTTPClient,
		versionClient: versionClient,
	}
}

func (p *Provider) Name() string               { return ProviderName }
func (p *Provider) DisplayName() string        { return ProviderDisplayName }
func (p *Provider) SupportsExitNodes() bool    { return true }
func (p *Provider) SupportsCustomServer() bool { return true }
func (p *Provider) SupportsAuthKey() bool      { return true }

func (p *Provider) IsInstalled() bool {
	_, err := os.Stat(TailscalePath)
	if err == nil {
		return true
	}
	if !os.IsNotExist(err) {
		logger.Warn().Err(err).Str("path", TailscalePath).Msg("unexpected error checking installation")
	}
	return false
}

func (p *Provider) Install(ctx context.Context, progress meshvpn.ProgressFunc) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.IsInstalled() {
		return meshvpn.ErrAlreadyInstalled
	}

	downloader := &Downloader{
		version:    p.version,
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

	// Stop the daemon before uninstalling - fail if we can't stop it
	if p.process != nil && p.process.IsRunning() {
		if err := p.process.Stop(); err != nil {
			return fmt.Errorf("cannot uninstall while daemon is running: %w", err)
		}
	}

	if err := os.RemoveAll(InstallBasePath); err != nil {
		return err
	}

	return nil
}

func (p *Provider) Connect(ctx context.Context, opts meshvpn.ConnectOptions) (*meshvpn.ConnectResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.IsInstalled() {
		return nil, meshvpn.ErrNotInstalled
	}

	if p.process == nil {
		p.process = NewProcessManager(p.tunMode)
	}

	if !p.process.IsRunning() {
		if err := p.process.Start(); err != nil {
			return nil, err
		}
	}

	cli := NewCLI()

	// Cancel any previous connect operation
	if p.connectCancel != nil {
		p.connectCancel()
	}

	// Create a cancellable context for the background Up operation
	upCtx, upCancel := context.WithCancel(context.Background())
	p.connectCancel = upCancel

	go func() {
		_, err := cli.Up(upCtx, UpOptions{
			ControlServer: opts.ControlServer,
			AuthKey:       opts.AuthKey,
		})
		if err != nil {
			if upCtx.Err() == nil {
				logger.Warn().Err(err).Msg("tailscale up completed with error")
			}
		} else {
			logger.Info().Msg("tailscale up completed successfully")
		}
	}()

	var status *StatusResponse
	var err error
	for i := 0; i < 10; i++ {
		time.Sleep(1 * time.Second)

		status, err = cli.Status(ctx)
		if err != nil {
			logger.Warn().Err(err).Int("attempt", i+1).Msg("failed to get status, retrying")
			continue
		}

		if status.AuthURL != "" || status.BackendState == "Running" {
			break
		}

		logger.Debug().
			Str("backendState", status.BackendState).
			Int("attempt", i+1).
			Msg("waiting for auth URL")
	}

	if err != nil {
		logger.Error().Err(err).Msg("failed to get status after connect")
		return nil, fmt.Errorf("failed to get status after connect: %w", err)
	}

	logger.Info().
		Str("authUrl", status.AuthURL).
		Str("backendState", status.BackendState).
		Msg("connect returning status")

	return &meshvpn.ConnectResult{
		AuthURL: status.AuthURL,
	}, nil
}

func (p *Provider) Disconnect(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.IsInstalled() {
		return meshvpn.ErrNotInstalled
	}

	// Cancel any pending connect operation
	if p.connectCancel != nil {
		p.connectCancel()
		p.connectCancel = nil
	}

	cli := NewCLI()
	return cli.Down(ctx)
}

func (p *Provider) Logout(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.IsInstalled() {
		return meshvpn.ErrNotInstalled
	}

	// Cancel any pending connect operation
	if p.connectCancel != nil {
		p.connectCancel()
		p.connectCancel = nil
	}

	cli := NewCLI()
	return cli.Logout(ctx)
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

	if p.process != nil && p.process.IsRunning() {
		status.Running = true
		status.State = meshvpn.StateConnecting
	}

	cli := NewCLI()
	cliStatus, err := cli.Status(ctx)
	if err != nil {
		if version, verr := cli.Version(ctx); verr == nil && version != "" {
			status.Version = version
		}

		if p.process != nil && p.process.IsRunning() {
			status.State = meshvpn.StateError
			status.ErrorMessage = err.Error()
		}
		return status, nil
	}

	if cliStatus.Version != "" {
		status.Version = cliStatus.Version
	}
	status.BackendState = cliStatus.BackendState
	if len(cliStatus.TailscaleIPs) > 0 {
		status.IP = cliStatus.TailscaleIPs[0]
	}
	status.Hostname = cliStatus.Self.HostName

	switch cliStatus.BackendState {
	case "Running":
		status.State = meshvpn.StateConnected
	case "NeedsLogin":
		status.State = meshvpn.StateNeedsAuth
		status.AuthURL = cliStatus.AuthURL
	case "Starting":
		status.State = meshvpn.StateConnecting
	case "Stopped":
		status.State = meshvpn.StateStopped
	default:
		status.State = meshvpn.StateConnecting
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

func (p *Provider) ListExitNodes(ctx context.Context) ([]meshvpn.ExitNode, error) {
	if !p.IsInstalled() {
		return nil, meshvpn.ErrNotInstalled
	}

	cli := NewCLI()
	return cli.ListExitNodes(ctx)
}

func (p *Provider) SetExitNode(ctx context.Context, hostname string, allowLAN bool) error {
	if !p.IsInstalled() {
		return meshvpn.ErrNotInstalled
	}

	cli := NewCLI()
	return cli.SetExitNode(ctx, hostname, allowLAN)
}

func (p *Provider) ClearExitNode(ctx context.Context) error {
	if !p.IsInstalled() {
		return meshvpn.ErrNotInstalled
	}

	cli := NewCLI()
	return cli.ClearExitNode(ctx)
}

func (p *Provider) SetAdvertiseExitNode(ctx context.Context, advertise bool) error {
	if !p.IsInstalled() {
		return meshvpn.ErrNotInstalled
	}

	cli := NewCLI()
	return cli.SetAdvertiseExitNode(ctx, advertise)
}

func (p *Provider) GetInfo() meshvpn.ProviderInfo {
	return meshvpn.ProviderInfo{
		Name:                 ProviderName,
		DisplayName:          ProviderDisplayName,
		Installed:            p.IsInstalled(),
		SupportsExitNodes:    true,
		SupportsCustomServer: true,
		SupportsAuthKey:      true,
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
			logger.Warn().Err(err).Msg("failed to get current Tailscale version")
		} else if version != "" {
			result.CurrentVersion = version
		}
	}

	if p.versionClient != nil {
		info, err := p.versionClient.GetLatestVersion(ctx, TrackStable)
		if err != nil {
			logger.Warn().Err(err).Msg("failed to get latest Tailscale version")
		} else {
			result.LatestVersion = info.TarballsVersion
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
		info, err := p.versionClient.GetLatestVersion(ctx, TrackStable)
		if err != nil {
			return fmt.Errorf("failed to get latest version: %w", err)
		}
		targetVersion = info.TarballsVersion
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
		Msg("updating Tailscale")

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
			p.process = NewProcessManager(p.tunMode)
		}
		if err := p.process.Start(); err != nil {
			return fmt.Errorf("failed to restart tailscaled after update: %w", err)
		}
	}

	logger.Info().Str("version", targetVersion).Msg("Tailscale updated successfully")
	return nil
}

func (p *Provider) GetTUNMode() meshvpn.TUNMode {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.tunMode
}

func (p *Provider) SetTUNMode(ctx context.Context, mode meshvpn.TUNMode) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if mode == p.tunMode {
		return nil
	}

	wasRunning := p.process != nil && p.process.IsRunning()
	if wasRunning {
		if err := p.process.Stop(); err != nil {
			return fmt.Errorf("cannot change TUN mode while daemon is running: %w", err)
		}
	}

	p.tunMode = mode
	p.process = NewProcessManager(mode)

	if wasRunning {
		if err := p.process.Start(); err != nil {
			return fmt.Errorf("failed to restart tailscaled with new TUN mode: %w", err)
		}
	}

	logger.Info().Str("tunMode", string(mode)).Msg("TUN mode changed")
	return nil
}
