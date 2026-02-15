//go:build linux

package tailscale

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jetkvm/kvm/internal/meshvpn"
)

// statusMonitorLogger adapts the package logger to the StatusMonitorLogger interface.
type statusMonitorLogger struct{}

func (l *statusMonitorLogger) Warn(msg string, err error, failures int) {
	logger.Warn().Err(err).Int("consecutiveFailures", failures).Msg(msg)
}

type Provider struct {
	mu            sync.RWMutex
	version       string
	tunMode       meshvpn.TUNMode
	process       *ProcessManager
	statusMonitor *meshvpn.StatusMonitor
	httpClient    meshvpn.HTTPClient
	versionClient *VersionClient
	connectCancel context.CancelFunc
	lastUpError   error // Error from most recent tailscale up command
}

type ProviderConfig struct {
	Version    string
	TUNMode    meshvpn.TUNMode
	HTTPClient meshvpn.HTTPClient
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

// isDaemonRunning checks if tailscaled is running by checking if its socket exists.
// This detects both our tracked process and orphaned daemons from previous sessions.
func (p *Provider) isDaemonRunning() bool {
	// Check if the socket exists - tailscaled creates this when running
	info, err := os.Stat(SocketPath)
	if err != nil {
		return false
	}
	// Verify it's a socket
	return info.Mode()&os.ModeSocket != 0
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
		latestInfo, err := p.versionClient.GetLatestVersion(ctx, TrackStable)
		if err != nil {
			logger.Warn().Err(err).Str("fallback", version).Msg("failed to fetch latest version, using fallback")
		} else if latestInfo.Version != "" {
			version = latestInfo.Version
			logger.Info().Str("version", version).Msg("installing latest Tailscale version")
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

	// Stop the daemon before uninstalling - fail if we can't stop it
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

	logger.Info().Msg("Tailscale uninstalled")
	return nil
}

// forceKillDaemon forcefully kills any running tailscaled process without relying on CLI communication.
// This is used when the daemon is hung and not responding to commands.
func (p *Provider) forceKillDaemon() error {
	logger.Info().Msg("forceKillDaemon: starting")

	// Find the tailscaled process by name
	pgrepCmd := exec.Command("pgrep", "-x", "tailscaled")
	output, err := pgrepCmd.Output()
	if err != nil {
		logger.Info().Err(err).Str("output", string(output)).Msg("forceKillDaemon: pgrep found no process")
		// No process found - check if socket exists and clean it up
		if p.isDaemonRunning() {
			logger.Info().Str("socketPath", SocketPath).Msg("forceKillDaemon: removing stale socket")
			os.Remove(SocketPath)
		}
		return nil
	}
	logger.Info().Str("pgrepOutput", string(output)).Msg("forceKillDaemon: found process")

	var pid int
	if _, parseErr := fmt.Sscanf(string(output), "%d", &pid); parseErr != nil || pid <= 0 {
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

	logger.Info().Int("pid", pid).Msg("force killing tailscaled")

	// Skip SIGTERM since daemon is unresponsive - go straight to SIGKILL
	if err := proc.Kill(); err != nil {
		return fmt.Errorf("failed to kill tailscaled (pid %d): %w", pid, err)
	}

	// Wait for process to exit
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if err := proc.Signal(syscall.Signal(0)); err != nil {
			logger.Info().Int("pid", pid).Msg("tailscaled killed successfully")
			// Clean up the socket
			os.Remove(SocketPath)
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	return fmt.Errorf("tailscaled (pid %d) did not exit after SIGKILL", pid)
}

// stopOrphanedDaemon stops a tailscaled daemon that was started in a previous session.
// This handles the case where the app was restarted but the daemon is still running.
func (p *Provider) stopOrphanedDaemon() error {
	// Check if daemon is running by trying to communicate with it
	// Use version check which is faster than status
	cli := NewCLI()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	_, err := cli.Version(ctx)
	cancel()

	if err != nil {
		// Daemon is not responding - force kill it if socket exists
		if p.isDaemonRunning() {
			logger.Debug().Msg("daemon socket exists but not responding, force killing")
			return p.forceKillDaemon()
		}
		return nil
	}

	// Daemon is responding - try graceful shutdown first
	pgrepCmd := exec.Command("pgrep", "-x", "tailscaled")
	output, err := pgrepCmd.Output()
	if err != nil {
		// No process found
		return nil
	}

	var pid int
	if _, parseErr := fmt.Sscanf(string(output), "%d", &pid); parseErr != nil || pid <= 0 {
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

	logger.Info().Int("pid", pid).Msg("stopping orphaned tailscaled")

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

func (p *Provider) Connect(ctx context.Context, opts meshvpn.ConnectOptions) (*meshvpn.ConnectResult, error) {
	logger.Info().
		Str("controlServer", opts.ControlServer).
		Bool("hasAuthKey", opts.AuthKey != "").
		Msg("Connect: starting")

	// Use a short lock just to set up the daemon and cancel context
	// Release the lock before polling so GetStatus can run concurrently
	cli := NewCLI()

	var startErr error
	func() {
		p.mu.Lock()
		defer p.mu.Unlock()

		if !p.IsInstalled() {
			logger.Error().Msg("Connect: Tailscale not installed")
			return
		}

		// Kill any hung daemon before starting fresh
		// This handles the case where daemon exists but doesn't respond
		processNil := p.process == nil
		processRunning := false
		if p.process != nil {
			processRunning = p.process.IsRunning()
		}
		daemonSocketExists := p.isDaemonRunning()
		logger.Info().
			Bool("processNil", processNil).
			Bool("processRunning", processRunning).
			Bool("daemonSocketExists", daemonSocketExists).
			Msg("Connect: checking for hung daemon")

		if processNil || !processRunning {
			if daemonSocketExists {
				logger.Info().Msg("Connect: found existing daemon, force killing before fresh start")
				if err := p.forceKillDaemon(); err != nil {
					logger.Warn().Err(err).Msg("Connect: failed to force kill existing daemon")
					// Continue anyway - Start() might still work
				}
				// Give the system a moment to clean up
				time.Sleep(500 * time.Millisecond)
			}
		}

		if p.process == nil {
			logger.Info().Str("tunMode", string(p.tunMode)).Msg("Connect: creating new ProcessManager")
			p.process = NewProcessManager(p.tunMode)
		}

		if !p.process.IsRunning() {
			logger.Info().Msg("Connect: starting tailscaled process")
			if err := p.process.Start(); err != nil {
				logger.Error().Err(err).Msg("Connect: failed to start tailscaled")
				startErr = err
				return
			}
			logger.Info().Msg("Connect: tailscaled process started")
		} else {
			logger.Info().Msg("Connect: tailscaled already running")
		}

		// Cancel any previous connect operation
		if p.connectCancel != nil {
			p.connectCancel()
		}

		// Create a cancellable context for the background Up operation
		upCtx, upCancel := context.WithCancel(context.Background())
		p.connectCancel = upCancel

		// Clear any previous error before starting new connection
		p.lastUpError = nil

		go func() {
			_, err := cli.Up(upCtx, UpOptions{
				ControlServer: opts.ControlServer,
				AuthKey:       opts.AuthKey,
			})
			if err != nil {
				if upCtx.Err() == nil {
					// Not cancelled - real error
					logger.Warn().Err(err).Msg("tailscale up completed with error")
					p.mu.Lock()
					p.lastUpError = err
					p.mu.Unlock()
				}
			} else {
				logger.Info().Msg("tailscale up completed successfully")
				p.mu.Lock()
				p.lastUpError = nil
				p.mu.Unlock()
			}
		}()
	}()

	// Check if installed (without lock since IsInstalled uses atomic)
	if !p.IsInstalled() {
		return nil, meshvpn.ErrNotInstalled
	}

	// Check if daemon failed to start
	if startErr != nil {
		return nil, fmt.Errorf("failed to start tailscaled: %w", startErr)
	}

	// Poll for status WITHOUT holding the lock so GetStatus can run
	var status *StatusResponse
	var err error
	pollStart := time.Now()
	for i := 0; i < 10; i++ {
		time.Sleep(1 * time.Second)

		// Check if daemon socket exists before calling CLI to avoid blocking
		socketExists := p.isDaemonRunning()
		logger.Debug().
			Int("attempt", i+1).
			Bool("socketExists", socketExists).
			Str("socketPath", SocketPath).
			Msg("Connect: polling for status")

		if !socketExists {
			continue
		}

		// First try a quick version check to verify CLI communication works
		// This is much faster than status and helps detect daemon responsiveness
		versionStart := time.Now()
		versionCtx, versionCancel := context.WithTimeout(ctx, 5*time.Second)
		version, verr := cli.Version(versionCtx)
		versionCancel()
		versionDuration := time.Since(versionStart)
		if verr != nil {
			logger.Info().Err(verr).Int("attempt", i+1).Dur("duration", versionDuration).Msg("version check failed, daemon not ready")
			continue
		}
		logger.Info().Str("version", version).Dur("duration", versionDuration).Int("attempt", i+1).Msg("version check passed")

		// Use a longer timeout for status check - ARM devices need more time
		statusStart := time.Now()
		statusCtx, statusCancel := context.WithTimeout(ctx, 15*time.Second)
		status, err = cli.Status(statusCtx)
		statusCancel()
		statusDuration := time.Since(statusStart)

		if err != nil {
			logger.Warn().Err(err).Int("attempt", i+1).Dur("statusDuration", statusDuration).Dur("totalPollTime", time.Since(pollStart)).Msg("failed to get status, retrying")
			continue
		}
		logger.Info().Dur("statusDuration", statusDuration).Dur("totalPollTime", time.Since(pollStart)).Msg("status check completed")

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

	// Stop the daemon but preserve state files (for easy reconnect later)
	// This is different from Logout which clears auth state
	if p.process != nil && p.process.IsRunning() {
		if err := p.process.Stop(); err != nil {
			return err
		}
		logger.Info().Msg("Tailscale disconnected")
		return nil
	}

	// Handle orphaned daemon from a previous session
	if err := p.stopOrphanedDaemon(); err != nil {
		return err
	}

	logger.Info().Msg("Tailscale disconnected")
	return nil
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

	// Logout clears the auth state
	cli := NewCLI()
	if err := cli.Logout(ctx); err != nil {
		logger.Warn().Err(err).Msg("tailscale logout command failed")
	}

	// Stop the daemon after logout
	if p.process != nil && p.process.IsRunning() {
		if err := p.process.Stop(); err != nil {
			return fmt.Errorf("failed to stop daemon after logout: %w", err)
		}
	}

	// Handle orphaned daemon from a previous session
	if err := p.stopOrphanedDaemon(); err != nil {
		return fmt.Errorf("failed to stop orphaned daemon after logout: %w", err)
	}

	logger.Info().Msg("Tailscale logged out")
	return nil
}

func (p *Provider) GetStatus(ctx context.Context) (*meshvpn.ProviderStatus, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	status := &meshvpn.ProviderStatus{
		State: meshvpn.StateNotInstalled,
	}

	if !p.IsInstalled() {
		logger.Debug().Msg("Tailscale not installed")
		return status, nil
	}

	status.Installed = true
	status.State = meshvpn.StateStopped
	status.Version = p.version

	// Check if daemon is running (either our tracked process or an orphaned one)
	daemonRunning := false
	if p.process != nil && p.process.IsRunning() {
		daemonRunning = true
		status.Running = true
		status.State = meshvpn.StateConnecting
	} else if p.isDaemonRunning() {
		// Check for orphaned daemon from previous session
		daemonRunning = true
		status.Running = true
		status.State = meshvpn.StateConnecting
		logger.Debug().Msg("found orphaned tailscaled process")
	}

	// Only call CLI if daemon is running - tailscale CLI hangs if daemon isn't running
	if !daemonRunning {
		logger.Debug().Msg("tailscaled not running, skipping CLI status check")
		return status, nil
	}

	cli := NewCLI()
	cliStatus, err := cli.Status(ctx)
	if err != nil {
		logger.Warn().Err(err).Msg("CLI status failed")

		// If daemon just stopped, the socket might still exist briefly
		// Treat "not running" errors as stopped, not error
		errStr := err.Error()
		if strings.Contains(errStr, "doesn't appear to be running") ||
			strings.Contains(errStr, "not running") ||
			strings.Contains(errStr, "connection refused") {
			logger.Debug().Msg("daemon appears to have stopped, returning stopped state")
			status.State = meshvpn.StateStopped
			status.Running = false
			return status, nil
		}

		if version, verr := cli.Version(ctx); verr == nil && version != "" {
			status.Version = version
		}

		status.State = meshvpn.StateError
		status.ErrorMessage = err.Error()
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

	// If we have an error from the background Up command, include it
	if p.lastUpError != nil && status.State == meshvpn.StateConnecting {
		status.ErrorMessage = p.lastUpError.Error()
	}

	return status, nil
}

func (p *Provider) StartStatusMonitor(ctx context.Context, onChange meshvpn.StatusChangeFunc) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.statusMonitor != nil {
		p.statusMonitor.Stop()
	}

	p.statusMonitor = meshvpn.NewStatusMonitor(meshvpn.StatusMonitorConfig{
		Provider: p,
		OnChange: onChange,
		Logger:   &statusMonitorLogger{},
	})
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
