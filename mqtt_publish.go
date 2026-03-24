package kvm

import (
	"fmt"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jetkvm/kvm/internal/ota"
)

// --- State Payload Types ---

type mqttVideoState struct {
	Ready  bool    `json:"ready"`
	Error  string  `json:"error,omitempty"`
	Width  int     `json:"width"`
	Height int     `json:"height"`
	FPS    float64 `json:"fps"`
}

type mqttUSBState struct {
	State string `json:"state"`
}

type mqttCloudState struct {
	Connected bool `json:"connected"`
}

type mqttSessionsState struct {
	ActiveSessions int `json:"active_sessions"`
}

type mqttJigglerState struct {
	Enabled bool `json:"enabled"`
}

type mqttNetworkState struct {
	IPAddress string `json:"ip_address"`
	Hostname  string `json:"hostname"`
}

type mqttUpdateState struct {
	InstalledVersion string   `json:"installed_version"`
	LatestVersion    string   `json:"latest_version"`
	InProgress       bool     `json:"in_progress"`
	UpdatePercentage *float32 `json:"update_percentage"`
}

type mqttSystemState struct {
	CPULoad     float64 `json:"cpu_load"`
	Temperature float64 `json:"temperature"`
	MemoryUsed  uint64  `json:"memory_used"`
	MemoryTotal uint64  `json:"memory_total"`
	StorageUsed int64   `json:"storage_used"`
	StorageFree int64   `json:"storage_free"`
}

type mqttVirtualMediaState struct {
	MountedImage string `json:"mounted_image"`
	Source       string `json:"source"`
}

// --- ATX State Publishing with Debounce ---

// publishATXState publishes the current ATX state to MQTT with debounce logic.
// When debouncing is enabled (debounceMs > 0), OFF transitions for the HDD LED
// are delayed: if the LED turns back ON within the debounce window, the OFF is
// suppressed so that rapid flickering (e.g. during heavy disk I/O) keeps the
// published state as ON. ON transitions and Power LED changes are published
// immediately (but only once per transition).
func (m *MQTTManager) publishATXState(state ATXState) {
	if !m.IsConnected() {
		return
	}

	// No debounce configured: publish immediately.
	if m.debounceMs <= 0 {
		m.publish(m.topic("atx", "state"), state, true)
		return
	}

	m.atxDebounceMu.Lock()
	defer m.atxDebounceMu.Unlock()

	lastState := m.atxLastPublished

	// First frame ever: publish immediately.
	if lastState == nil {
		m.publishATXStateLocked(state)
		return
	}

	// Power LED changed: always publish immediately and reset debounce.
	if state.Power != lastState.Power {
		m.cancelATXDebounceTimerLocked()
		m.publishATXStateLocked(state)
		return
	}

	// HDD LED turned ON (or stayed ON):
	if state.HDD {
		// Cancel any pending OFF timer – the LED is active.
		if m.atxDebounceTimer != nil {
			m.cancelATXDebounceTimerLocked()
		}
		// Only publish if last published state was OFF (i.e. the ON transition).
		if !lastState.HDD {
			m.publishATXStateLocked(state)
		}
		return
	}

	// HDD LED is OFF:
	// If already published as OFF, nothing to do.
	if !lastState.HDD {
		return
	}

	// HDD LED just turned OFF: delay publishing.
	// If a timer is already running, let it continue.
	if m.atxDebounceTimer != nil {
		return
	}

	debounceState := state // capture for closure
	m.atxDebounceTimer = time.AfterFunc(time.Duration(m.debounceMs)*time.Millisecond, func() {
		m.atxDebounceMu.Lock()
		defer m.atxDebounceMu.Unlock()
		m.atxDebounceTimer = nil
		m.publishATXStateLocked(debounceState)
	})
}

// publishATXStateLocked publishes the ATX state and records it. Must be called with atxDebounceMu held.
func (m *MQTTManager) publishATXStateLocked(state ATXState) {
	m.atxLastPublished = &state
	m.publish(m.topic("atx", "state"), state, true)
}

// cancelATXDebounceTimerLocked stops a pending debounce timer. Must be called with atxDebounceMu held.
func (m *MQTTManager) cancelATXDebounceTimerLocked() {
	if m.atxDebounceTimer != nil {
		m.atxDebounceTimer.Stop()
		m.atxDebounceTimer = nil
	}
}

// --- Simple State Publishers ---

// publishDCState publishes the current DC power state to MQTT.
func (m *MQTTManager) publishDCState(state DCPowerState) {
	if !m.IsConnected() {
		return
	}
	m.publish(m.topic("dc", "state"), state, true)
}

// publishVideoState publishes the current video state to MQTT.
func (m *MQTTManager) publishVideoState() {
	if !m.IsConnected() {
		return
	}
	state := mqttVideoState{
		Ready:  lastVideoState.Ready,
		Error:  lastVideoState.Error,
		Width:  lastVideoState.Width,
		Height: lastVideoState.Height,
		FPS:    lastVideoState.FramePerSecond,
	}
	m.publish(m.topic("video", "state"), state, true)
}

// publishJigglerState publishes the current jiggler state.
func (m *MQTTManager) publishJigglerState() {
	if !m.IsConnected() {
		return
	}
	m.publish(m.topic("jiggler", "state"), mqttJigglerState{
		Enabled: config.JigglerEnabled,
	}, true)
}

// publishSessionsState publishes the current active sessions count.
func (m *MQTTManager) publishSessionsState() {
	if !m.IsConnected() {
		return
	}
	m.publish(m.topic("sessions", "state"), mqttSessionsState{
		ActiveSessions: getActiveSessions(),
	}, true)
}

// publishNetworkState publishes the current network state.
func (m *MQTTManager) publishNetworkState() {
	if !m.IsConnected() || networkManager == nil {
		return
	}

	netState := mqttNetworkState{}
	state, err := networkManager.GetInterfaceState(NetIfName)
	if err == nil {
		rpcState := state.ToRpcInterfaceState()
		if rpcState != nil {
			netState.IPAddress = rpcState.IPv4Address
			netState.Hostname = rpcState.Hostname
		}
	}
	m.publish(m.topic("network", "state"), netState, true)
}

// publishSystemState publishes CPU load, temperature, memory and storage metrics.
func (m *MQTTManager) publishSystemState() {
	if !m.IsConnected() {
		return
	}

	state := mqttSystemState{}

	// CPU load average (1 min) from /proc/loadavg
	if data, err := os.ReadFile("/proc/loadavg"); err == nil {
		fields := strings.Fields(string(data))
		if len(fields) > 0 {
			if load, err := strconv.ParseFloat(fields[0], 64); err == nil {
				state.CPULoad = load
			}
		}
	}

	// SoC temperature from thermal zone
	if data, err := os.ReadFile("/sys/class/thermal/thermal_zone0/temp"); err == nil {
		if temp, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64); err == nil {
			state.Temperature = temp / 1000.0 // millidegrees to degrees
		}
	}

	// Memory from /proc/meminfo
	if data, err := os.ReadFile("/proc/meminfo"); err == nil {
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			val, err := strconv.ParseUint(fields[1], 10, 64)
			if err != nil {
				continue
			}
			switch fields[0] {
			case "MemTotal:":
				state.MemoryTotal = val * 1024 // kB to bytes
			case "MemAvailable:":
				state.MemoryUsed = state.MemoryTotal - (val * 1024)
			}
		}
	}

	// Storage space
	var stat syscall.Statfs_t
	if err := syscall.Statfs(imagesFolder, &stat); err == nil {
		totalSpace := stat.Blocks * uint64(stat.Bsize)
		freeSpace := stat.Bfree * uint64(stat.Bsize)
		state.StorageUsed = int64(totalSpace - freeSpace)
		state.StorageFree = int64(freeSpace)
	}

	m.publish(m.topic("system", "state"), state, true)
}

// publishVirtualMediaState publishes the currently mounted disk image.
func (m *MQTTManager) publishVirtualMediaState() {
	if !m.IsConnected() {
		return
	}

	state := mqttVirtualMediaState{
		MountedImage: "-- no media --",
		Source:       "none",
	}

	virtualMediaStateMutex.RLock()
	if currentVirtualMediaState != nil {
		switch currentVirtualMediaState.Source {
		case Storage:
			if currentVirtualMediaState.Filename != "" {
				state.MountedImage = currentVirtualMediaState.Filename
				state.Source = "storage"
			}
		case HTTP:
			state.Source = "url"
			// Extract just the filename from the URL path
			imageName := currentVirtualMediaState.URL
			if parsed, err := url.Parse(currentVirtualMediaState.URL); err == nil {
				base := path.Base(parsed.Path)
				if base != "" && base != "." && base != "/" {
					imageName = base
				}
			}
			state.MountedImage = imageName
		}
	}
	virtualMediaStateMutex.RUnlock()

	m.publish(m.topic("virtual_media", "state"), state, true)

	// Re-publish discovery only when select options actually changed
	// (e.g. when a URL image is mounted/unmounted or files are added/removed).
	if config.MqttConfig != nil && config.MqttConfig.EnableHADiscovery && config.MqttConfig.EnableActions {
		vmOptions := getAvailableImages()
		if state.Source == "url" {
			vmOptions = append(vmOptions, state.MountedImage)
		}
		if !slicesEqual(vmOptions, m.lastVMOptions) {
			m.lastVMOptions = vmOptions
			m.publishDiscovery("select", "virtual_media", haDiscoveryPayload{
				Name:                   "Virtual Media",
				UniqueID:               fmt.Sprintf("jetkvm_%s_virtual_media", m.deviceID),
				StateTopic:             m.topic("virtual_media", "state"),
				CommandTopic:           m.topic("virtual_media", "set"),
				ValueTemplate:          "{{ value_json.mounted_image }}",
				Options:                vmOptions,
				Icon:                   "mdi:disc",
				JsonAttributesTopic:    m.topic("virtual_media", "state"),
				JsonAttributesTemplate: "{{ {'source': value_json.source} | tojson }}",
				AvailabilityTopic:      m.topic("status"),
				AvailTemplate:          "{{ 'online' if value_json.online else 'offline' }}",
				Device:                 m.haDeviceInfo(),
			})
		}
	}
}

// slicesEqual reports whether two string slices have the same elements.
func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// getAvailableImages returns a list of filenames available for mounting.
func getAvailableImages() []string {
	options := []string{"-- no media --"}
	files, err := os.ReadDir(imagesFolder)
	if err != nil {
		return options
	}
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		options = append(options, file.Name())
	}
	return options
}

// lastKnownLatestVersion stores the latest version to avoid losing it during OTA.
var lastKnownLatestVersion string

// updateCheckInterval controls how often the expensive getUpdateStatus API call
// is made. Between checks, the last known result is re-published.
const updateCheckInterval = 10 * time.Minute

// publishUpdateState publishes the current update state.
// When not updating, it caches getUpdateStatus results and only re-queries
// the update API every updateCheckInterval to avoid excessive HTTP calls.
func (m *MQTTManager) publishUpdateState() {
	if !m.IsConnected() {
		return
	}

	// Check if an update is currently in progress via OTA state
	otaUpdating := false
	var otaRPCState *ota.RPCState
	if otaState != nil {
		otaRPCState = otaState.ToRPCState()
		if otaRPCState != nil && otaRPCState.Updating {
			otaUpdating = true
		}
	}

	// Determine effective updating state:
	// - OTA says updating → definitely updating
	// - We requested update via MQTT but OTA hasn't started yet → still updating (bridge the gap)
	// - OTA finished (was requested, OTA no longer updating) → clear the flag
	if m.updateRequested.Load() && !otaUpdating {
		// Check if OTA has actually run and finished (error field set or metadata fetched after request)
		if otaRPCState != nil && otaRPCState.Error != "" {
			// OTA encountered an error, clear the flag
			m.updateRequested.Store(false)
		}
		// Otherwise keep updateRequested=true to bridge the gap
	}
	updating := otaUpdating || m.updateRequested.Load()

	updatePayload := mqttUpdateState{}

	// Get installed version
	_, appVer, err := GetLocalVersion()
	if err == nil && appVer != nil {
		updatePayload.InstalledVersion = appVer.String()
		updatePayload.LatestVersion = appVer.String() // Default: no update available
	}

	if updating {
		// During an active update, do NOT call getUpdateStatus (it may reset versions).
		// Use the last known latest version and only update progress.
		if lastKnownLatestVersion != "" {
			updatePayload.LatestVersion = lastKnownLatestVersion
		}
		updatePayload.InProgress = true
		if otaState != nil {
			rpcState := otaState.ToRPCState()
			if rpcState != nil {
				progress := calculateOTAProgress(rpcState)
				updatePayload.UpdatePercentage = &progress
			}
		}
		// Invalidate cache so we re-check after update completes
		m.lastUpdateCheck = time.Time{}
	} else {
		// Not updating: query the update API, but only every updateCheckInterval.
		if m.lastUpdatePayload != nil && time.Since(m.lastUpdateCheck) < updateCheckInterval {
			// Use cached result
			updatePayload = *m.lastUpdatePayload
			// Refresh installed version in case it changed after an update
			if appVer != nil {
				updatePayload.InstalledVersion = appVer.String()
			}
		} else {
			updateStatus, statusErr := getUpdateStatus(config.IncludePreRelease)
			if statusErr == nil && updateStatus != nil {
				if updateStatus.Local != nil {
					updatePayload.InstalledVersion = updateStatus.Local.AppVersion
				}
				if updateStatus.Remote != nil && updateStatus.AppUpdateAvailable {
					updatePayload.LatestVersion = updateStatus.Remote.AppVersion
					// Remember the latest version for when an update starts
					lastKnownLatestVersion = updateStatus.Remote.AppVersion
				}
			}
			// Reset progress fields when not updating
			updatePayload.InProgress = false
			updatePayload.UpdatePercentage = nil
			// Cache the result
			cached := updatePayload
			m.lastUpdatePayload = &cached
			m.lastUpdateCheck = time.Now()
		}
	}

	m.publish(m.topic("update", "state"), updatePayload, true)
}

// getInstalledVersion returns the current installed app version as string.
func getInstalledVersion() string {
	_, appVer, err := GetLocalVersion()
	if err == nil && appVer != nil {
		return appVer.String()
	}
	return "unknown"
}

// calculateOTAProgress computes an overall update percentage (0-100) from the OTA state.
func calculateOTAProgress(state *ota.RPCState) float32 {
	// Weight: download 40%, verification 20%, install 40%
	var total float32
	var components float32

	for _, prefix := range []struct {
		download     *float32
		verification *float32
		update       *float32
	}{
		{state.AppDownloadProgress, state.AppVerificationProgress, state.AppUpdateProgress},
		{state.SystemDownloadProgress, state.SystemVerificationProgress, state.SystemUpdateProgress},
	} {
		hasAny := prefix.download != nil || prefix.verification != nil || prefix.update != nil
		if !hasAny {
			continue
		}
		components++
		var dl, ver, upd float32
		if prefix.download != nil {
			dl = *prefix.download
		}
		if prefix.verification != nil {
			ver = *prefix.verification
		}
		if prefix.update != nil {
			upd = *prefix.update
		}
		total += dl*40 + ver*20 + upd*40
	}

	if components == 0 {
		return 0
	}
	return total / components
}

// publishExtendedStates publishes all extended metric states.
func (m *MQTTManager) publishExtendedStates() {
	// Video state
	m.publishVideoState()

	// USB state
	usbPayload := mqttUSBState{
		State: gadget.GetUsbState(),
	}
	m.publish(m.topic("usb", "state"), usbPayload, true)

	// Cloud state
	cloudPayload := mqttCloudState{
		Connected: getCloudConnectionState() == CloudConnectionStateConnected,
	}
	m.publish(m.topic("cloud", "state"), cloudPayload, true)

	// Active sessions
	m.publishSessionsState()

	// Jiggler state
	m.publishJigglerState()

	// Network state
	m.publishNetworkState()

	// System state (CPU, temp, memory, storage)
	m.publishSystemState()

	// Virtual media state
	m.publishVirtualMediaState()

	// Update state
	m.publishUpdateState()
}

// startPeriodicStatusUpdates starts a goroutine that periodically publishes the device status.
// The goroutine stops when the MQTTManager's done channel is closed.
func (m *MQTTManager) startPeriodicStatusUpdates(interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-m.done:
				return
			case <-ticker.C:
				if !m.IsConnected() {
					continue
				}
				m.publish(m.topic("status"), mqttStatusPayload{Online: true}, true)

				// Publish current ATX state only if ATX extension is active
				if config.ActiveExtension == "atx-power" {
					m.publishATXState(ATXState{
						Power: ledPWRState.Load(),
						HDD:   ledHDDState.Load(),
					})
				}

				// Publish current DC state only if DC extension is active
				if config.ActiveExtension == "dc-power" {
					m.publishDCState(getDCState())
				}

				// Publish extended metric states
				m.publishExtendedStates()
			}
		}
	}()
}
