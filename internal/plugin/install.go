package plugin

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path"
	"syscall"
)

type PluginInstall struct {
	Enabled bool `json:"enabled"`

	// Current active version of the plugin
	Version string `json:"version"`

	// Map of a plugin version to the extracted directory
	ExtractedVersions map[string]string `json:"extracted_versions"`

	manifest       *PluginManifest
	runningVersion *string
	processManager *ProcessManager
	rpcServer      *PluginRpcServer
}

func (p *PluginInstall) GetManifest() (*PluginManifest, error) {
	if p.manifest != nil {
		return p.manifest, nil
	}

	manifest, err := readManifest(p.GetExtractedFolder())
	if err != nil {
		return nil, err
	}

	p.manifest = manifest
	return manifest, nil
}

func (p *PluginInstall) GetExtractedFolder() string {
	return p.ExtractedVersions[p.Version]
}

func (p *PluginInstall) GetStatus() (*PluginStatus, error) {
	manifest, err := p.GetManifest()
	if err != nil {
		return nil, fmt.Errorf("failed to get plugin manifest: %v", err)
	}

	status := PluginStatus{
		PluginManifest: *manifest,
		Enabled:        p.Enabled,
	}

	// If the rpc server is connected and the plugin is reporting status, use that
	if p.rpcServer != nil &&
		p.rpcServer.status.Status != "disconnected" &&
		p.rpcServer.status.Status != "unknown" {
		status.Status = p.rpcServer.status.Status
		status.Message = p.rpcServer.status.Message

		if status.Status == "error" {
			status.Message = p.rpcServer.status.Message
		}
	} else {
		status.Status = "stopped"
		if p.processManager != nil {
			status.Status = "running"
			if p.processManager.LastError != nil {
				status.Status = "error"
				status.Message = p.processManager.LastError.Error()
			}
		}
		log.Printf("Status from process manager: %v", status.Status)
	}

	return &status, nil
}

func (p *PluginInstall) ReconcileSubprocess() error {
	manifest, err := p.GetManifest()
	if err != nil {
		return fmt.Errorf("failed to get plugin manifest: %v", err)
	}

	versionRunning := ""
	if p.runningVersion != nil {
		versionRunning = *p.runningVersion
	}

	versionShouldBeRunning := p.Version
	if !p.Enabled {
		versionShouldBeRunning = ""
	}

	log.Printf("Reconciling plugin %s running %v, should be running %v", manifest.Name, versionRunning, versionShouldBeRunning)

	if versionRunning == versionShouldBeRunning {
		log.Printf("Plugin %s is already running version %s", manifest.Name, versionRunning)
		return nil
	}

	if p.processManager != nil {
		log.Printf("Stopping plugin %s running version %s", manifest.Name, versionRunning)
		p.processManager.Disable()
		p.processManager = nil
		p.runningVersion = nil
		err = p.rpcServer.Stop()
		if err != nil {
			return fmt.Errorf("failed to stop rpc server: %v", err)
		}
	}

	if versionShouldBeRunning == "" {
		return nil
	}

	workingDir := path.Join(pluginsFolder, "working_dirs", p.manifest.Name)
	err = os.MkdirAll(workingDir, 0755)
	if err != nil {
		return fmt.Errorf("failed to create working directory: %v", err)
	}

	p.rpcServer = NewPluginRpcServer(p, workingDir)
	err = p.rpcServer.Start()
	if err != nil {
		return fmt.Errorf("failed to start rpc server: %v", err)
	}

	p.processManager = NewProcessManager(func() *exec.Cmd {
		cmd := exec.Command(manifest.BinaryPath)
		cmd.Dir = p.GetExtractedFolder()
		cmd.Env = append(cmd.Env,
			"JETKVM_PLUGIN_SOCK="+p.rpcServer.SocketPath(),
			"JETKVM_PLUGIN_WORKING_DIR="+workingDir,
		)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		// Ensure that the process is killed when the parent dies
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Setpgid:   true,
			Pdeathsig: syscall.SIGKILL,
		}
		return cmd
	})
	p.processManager.StartMonitor()
	p.processManager.Enable()
	p.runningVersion = &p.Version
	log.Printf("Started plugin %s version %s", manifest.Name, p.Version)
	return nil
}

func (p *PluginInstall) Shutdown() {
	if p.processManager != nil {
		p.processManager.Disable()
		p.processManager = nil
		p.runningVersion = nil
	}

	if p.rpcServer != nil {
		p.rpcServer.Stop()
	}
}
