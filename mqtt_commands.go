package kvm

import (
	"strings"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// --- Command Subscriptions ---

func (m *MQTTManager) subscribeCommands() {
	commands := map[string]mqtt.MessageHandler{
		m.topic("dc_power", "set"):        m.handleDCPowerCommand,
		m.topic("dc_restore", "set"):      m.handleDCRestoreCommand,
		m.topic("atx_power_short", "set"): m.handleATXPowerShortCommand,
		m.topic("atx_power_long", "set"):  m.handleATXPowerLongCommand,
		m.topic("atx_reset", "set"):       m.handleATXResetCommand,
		m.topic("jiggler", "set"):         m.handleJigglerCommand,
		m.topic("reboot", "set"):          m.handleRebootCommand,
		m.topic("update", "install"):      m.handleUpdateInstallCommand,
		m.topic("virtual_media", "set"):   m.handleVirtualMediaCommand,
	}

	for topic, handler := range commands {
		if token := m.client.Subscribe(topic, 1, handler); token.Wait() && token.Error() != nil {
			mqttLogger.Error().Err(token.Error()).Str("topic", topic).Msg("failed to subscribe")
		}
	}

	mqttLogger.Info().Msg("subscribed to command topics")
}

func (m *MQTTManager) handleDCPowerCommand(client mqtt.Client, msg mqtt.Message) {
	if !m.actionsAllowed() {
		mqttLogger.Warn().Msg("DC power command rejected: actions are disabled")
		return
	}
	payload := strings.TrimSpace(string(msg.Payload()))
	mqttLogger.Info().Str("payload", payload).Msg("received DC power command")

	switch strings.ToUpper(payload) {
	case "ON":
		if err := setDCPowerState(true); err != nil {
			mqttLogger.Error().Err(err).Msg("failed to set DC power on")
		}
	case "OFF":
		if err := setDCPowerState(false); err != nil {
			mqttLogger.Error().Err(err).Msg("failed to set DC power off")
		}
	default:
		mqttLogger.Warn().Str("payload", payload).Msg("unknown DC power command")
	}
}

func (m *MQTTManager) handleDCRestoreCommand(client mqtt.Client, msg mqtt.Message) {
	if !m.actionsAllowed() {
		mqttLogger.Warn().Msg("DC restore command rejected: actions are disabled")
		return
	}
	payload := strings.TrimSpace(string(msg.Payload()))
	mqttLogger.Info().Str("payload", payload).Msg("received DC restore command")

	var state int
	switch strings.ToLower(payload) {
	case "off":
		state = 0
	case "on":
		state = 1
	case "last_state":
		state = 2
	default:
		mqttLogger.Warn().Str("payload", payload).Msg("unknown DC restore command")
		return
	}

	if err := setDCRestoreState(state); err != nil {
		mqttLogger.Error().Err(err).Msg("failed to set DC restore state")
	}
}

func (m *MQTTManager) handleATXPowerShortCommand(client mqtt.Client, msg mqtt.Message) {
	if !m.actionsAllowed() {
		mqttLogger.Warn().Msg("ATX power short command rejected: actions are disabled")
		return
	}
	mqttLogger.Info().Msg("received ATX power short press command")
	if err := pressATXPowerButton(500 * time.Millisecond); err != nil {
		mqttLogger.Error().Err(err).Msg("failed to press ATX power button (short)")
	}
}

func (m *MQTTManager) handleATXPowerLongCommand(client mqtt.Client, msg mqtt.Message) {
	if !m.actionsAllowed() {
		mqttLogger.Warn().Msg("ATX power long command rejected: actions are disabled")
		return
	}
	mqttLogger.Info().Msg("received ATX power long press command")
	if err := pressATXPowerButton(5 * time.Second); err != nil {
		mqttLogger.Error().Err(err).Msg("failed to press ATX power button (long)")
	}
}

func (m *MQTTManager) handleATXResetCommand(client mqtt.Client, msg mqtt.Message) {
	if !m.actionsAllowed() {
		mqttLogger.Warn().Msg("ATX reset command rejected: actions are disabled")
		return
	}
	mqttLogger.Info().Msg("received ATX reset command")
	if err := pressATXResetButton(500 * time.Millisecond); err != nil {
		mqttLogger.Error().Err(err).Msg("failed to press ATX reset button")
	}
}

func (m *MQTTManager) handleJigglerCommand(client mqtt.Client, msg mqtt.Message) {
	if !m.actionsAllowed() {
		mqttLogger.Warn().Msg("jiggler command rejected: actions are disabled")
		return
	}
	payload := strings.TrimSpace(string(msg.Payload()))
	mqttLogger.Info().Str("payload", payload).Msg("received jiggler command")

	switch strings.ToUpper(payload) {
	case "ON":
		if err := rpcSetJigglerState(true); err != nil {
			mqttLogger.Error().Err(err).Msg("failed to enable jiggler")
		}
	case "OFF":
		if err := rpcSetJigglerState(false); err != nil {
			mqttLogger.Error().Err(err).Msg("failed to disable jiggler")
		}
	default:
		mqttLogger.Warn().Str("payload", payload).Msg("unknown jiggler command")
	}

	// Publish updated state immediately
	m.publishJigglerState()
}

func (m *MQTTManager) handleRebootCommand(client mqtt.Client, msg mqtt.Message) {
	if !m.actionsAllowed() {
		mqttLogger.Warn().Msg("reboot command rejected: actions are disabled")
		return
	}
	mqttLogger.Info().Msg("received reboot command via MQTT")
	if err := rpcReboot(false); err != nil {
		mqttLogger.Error().Err(err).Msg("failed to reboot")
	}
}

func (m *MQTTManager) handleUpdateInstallCommand(client mqtt.Client, msg mqtt.Message) {
	if !m.actionsAllowed() {
		mqttLogger.Warn().Msg("update install command rejected: actions are disabled")
		return
	}
	mqttLogger.Info().Msg("received update install command via MQTT")

	// Set flag to keep in_progress state until OTA state confirms updating
	m.updateRequested.Store(true)

	// Determine latest version for the in_progress message
	latestVer := lastKnownLatestVersion
	if latestVer == "" {
		latestVer = getInstalledVersion()
	}

	// Immediately publish in_progress state so HA shows the update dialog
	var zero float32
	m.publish(m.topic("update", "state"), mqttUpdateState{
		InstalledVersion: getInstalledVersion(),
		LatestVersion:    latestVer,
		InProgress:       true,
		UpdatePercentage: &zero,
	}, true)

	if err := rpcTryUpdate(); err != nil {
		mqttLogger.Error().Err(err).Msg("failed to start update")
		m.updateRequested.Store(false)
		// Reset in_progress on failure
		m.publishUpdateState()
	}
}

func (m *MQTTManager) handleVirtualMediaCommand(client mqtt.Client, msg mqtt.Message) {
	if !m.actionsAllowed() {
		mqttLogger.Warn().Msg("virtual media command rejected: actions are disabled")
		return
	}
	payload := strings.TrimSpace(string(msg.Payload()))
	mqttLogger.Info().Str("payload", payload).Msg("received virtual media command")

	if payload == "-- no media --" {
		// Unmount current image
		if err := rpcUnmountImage(); err != nil {
			mqttLogger.Error().Err(err).Msg("failed to unmount image")
		}
	} else {
		// Defense-in-depth: reject obviously malicious filenames before
		// passing them to rpcMountWithStorage (which also validates via sanitizeFilename).
		if strings.Contains(payload, "..") || strings.ContainsRune(payload, '/') || strings.ContainsRune(payload, '\\') {
			mqttLogger.Warn().Str("payload", payload).Msg("rejected invalid filename")
			return
		}

		// Check if something is already mounted, unmount first
		virtualMediaStateMutex.RLock()
		mounted := currentVirtualMediaState != nil
		virtualMediaStateMutex.RUnlock()
		if mounted {
			if err := rpcUnmountImage(); err != nil {
				mqttLogger.Error().Err(err).Msg("failed to unmount current image before mounting new one")
				return
			}
		}

		// Mount the image as Disk mode (default).
		// rpcMountWithStorage validates the filename and checks file existence.
		if err := rpcMountWithStorage(payload, Disk); err != nil {
			mqttLogger.Error().Err(err).Msg("failed to mount image")
		}
	}

	// Publish updated state immediately
	m.publishVirtualMediaState()
}
