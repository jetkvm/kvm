package kvm

import (
	"fmt"
	"net/url"
	"path"
)

// --- Home Assistant MQTT Discovery ---

type haDevice struct {
	Identifiers  []string `json:"identifiers"`
	Name         string   `json:"name"`
	Manufacturer string   `json:"manufacturer"`
	Model        string   `json:"model"`
	SwVersion    string   `json:"sw_version,omitempty"`
	SerialNumber string   `json:"serial_number,omitempty"`
	ConfigURL    string   `json:"configuration_url,omitempty"`
}

type haDiscoveryPayload struct {
	// Common
	Name              string    `json:"name"`
	UniqueID          string    `json:"unique_id"`
	StateTopic        string    `json:"state_topic,omitempty"`
	CommandTopic      string    `json:"command_topic,omitempty"`
	ValueTemplate     string    `json:"value_template,omitempty"`
	AvailabilityTopic string    `json:"availability_topic"`
	AvailTemplate     string    `json:"availability_template"`
	Device            *haDevice `json:"device"`

	// Sensor-specific
	DeviceClass       string `json:"device_class,omitempty"`
	UnitOfMeasurement string `json:"unit_of_measurement,omitempty"`
	StateClass        string `json:"state_class,omitempty"`
	PayloadOn         string `json:"payload_on,omitempty"`
	PayloadOff        string `json:"payload_off,omitempty"`
	PayloadPress      string `json:"payload_press,omitempty"`
	Icon              string `json:"icon,omitempty"`
	EntityCategory    string `json:"entity_category,omitempty"`
	DefaultEntityID   string `json:"default_entity_id,omitempty"`
	EnabledByDefault  *bool  `json:"enabled_by_default,omitempty"`

	// Attributes
	JsonAttributesTopic    string `json:"json_attributes_topic,omitempty"`
	JsonAttributesTemplate string `json:"json_attributes_template,omitempty"`

	// Select-specific
	Options []string `json:"options,omitempty"`

	// Update-specific
	LatestVersionTopic    string `json:"latest_version_topic,omitempty"`
	LatestVersionTemplate string `json:"latest_version_template,omitempty"`
	PayloadInstall        string `json:"payload_install,omitempty"`
	ReleaseURL            string `json:"release_url,omitempty"`
}

func (m *MQTTManager) haDeviceInfo() *haDevice {
	deviceID := m.deviceID

	// Build configuration URL from network state
	configURL := ""
	if networkManager != nil {
		state, err := networkManager.GetInterfaceState(NetIfName)
		if err == nil {
			rpcState := state.ToRpcInterfaceState()
			if rpcState != nil && rpcState.IPv4Address != "" {
				scheme := "http"
				if config.TLSMode != "" {
					scheme = "https"
				}
				configURL = fmt.Sprintf("%s://%s", scheme, rpcState.IPv4Address)
			}
		}
	}

	// Build software version from app + system versions
	swVersion := ""
	sysVer, appVer, err := GetLocalVersion()
	if err == nil {
		if appVer != nil && sysVer != nil {
			swVersion = fmt.Sprintf("App: %s | Sys: %s", appVer.String(), sysVer.String())
		} else if appVer != nil {
			swVersion = fmt.Sprintf("App %s", appVer.String())
		}
	}

	return &haDevice{
		Identifiers:  []string{deviceID},
		Name:         fmt.Sprintf("JetKVM %s", deviceID),
		Manufacturer: "JetKVM",
		Model:        "JetKVM",
		SwVersion:    swVersion,
		SerialNumber: deviceID,
		ConfigURL:    configURL,
	}
}

func boolPtr(b bool) *bool {
	return &b
}

func (m *MQTTManager) publishHADiscovery() {
	device := m.haDeviceInfo()
	availTopic := m.topic("status")
	availTemplate := "{{ 'online' if value_json.online else 'offline' }}"

	// --- General entities (always published) ---

	// Binary sensor: Online
	m.publishDiscovery("binary_sensor", "online", haDiscoveryPayload{
		Name:              "Online",
		UniqueID:          fmt.Sprintf("jetkvm_%s_online", m.deviceID),
		StateTopic:        m.topic("status"),
		ValueTemplate:     "{{ 'ON' if value_json.online else 'OFF' }}",
		DeviceClass:       "connectivity",
		AvailabilityTopic: availTopic,
		AvailTemplate:     availTemplate,
		Device:            device,
	})

	// Binary sensor: Video Signal
	m.publishDiscovery("binary_sensor", "video_signal", haDiscoveryPayload{
		Name:              "Video Signal",
		UniqueID:          fmt.Sprintf("jetkvm_%s_video_signal", m.deviceID),
		StateTopic:        m.topic("video", "state"),
		ValueTemplate:     "{{ 'ON' if value_json.ready else 'OFF' }}",
		DeviceClass:       "connectivity",
		Icon:              "mdi:video-input-hdmi",
		AvailabilityTopic: availTopic,
		AvailTemplate:     availTemplate,
		Device:            device,
	})

	// Sensor: Video Resolution (disabled by default)
	enabledByDefault := boolPtr(false)
	m.publishDiscovery("sensor", "video_resolution", haDiscoveryPayload{
		Name:              "Video Resolution",
		UniqueID:          fmt.Sprintf("jetkvm_%s_video_resolution", m.deviceID),
		StateTopic:        m.topic("video", "state"),
		ValueTemplate:     "{{ value_json.width }}x{{ value_json.height }}",
		Icon:              "mdi:monitor",
		EntityCategory:    "diagnostic",
		EnabledByDefault:  enabledByDefault,
		AvailabilityTopic: availTopic,
		AvailTemplate:     availTemplate,
		Device:            device,
	})

	// Sensor: Video FPS (disabled by default)
	m.publishDiscovery("sensor", "video_fps", haDiscoveryPayload{
		Name:              "Video FPS",
		UniqueID:          fmt.Sprintf("jetkvm_%s_video_fps", m.deviceID),
		StateTopic:        m.topic("video", "state"),
		ValueTemplate:     "{{ value_json.fps | round(1) }}",
		UnitOfMeasurement: "fps",
		StateClass:        "measurement",
		Icon:              "mdi:speedometer",
		EntityCategory:    "diagnostic",
		EnabledByDefault:  enabledByDefault,
		AvailabilityTopic: availTopic,
		AvailTemplate:     availTemplate,
		Device:            device,
	})

	// Binary sensor: Cloud Connected
	m.publishDiscovery("binary_sensor", "cloud_connected", haDiscoveryPayload{
		Name:              "Cloud Connected",
		UniqueID:          fmt.Sprintf("jetkvm_%s_cloud_connected", m.deviceID),
		StateTopic:        m.topic("cloud", "state"),
		ValueTemplate:     "{{ 'ON' if value_json.connected else 'OFF' }}",
		DeviceClass:       "connectivity",
		Icon:              "mdi:cloud",
		EntityCategory:    "diagnostic",
		AvailabilityTopic: availTopic,
		AvailTemplate:     availTemplate,
		Device:            device,
	})

	// Sensor: Active Sessions
	m.publishDiscovery("sensor", "active_sessions", haDiscoveryPayload{
		Name:              "Active Sessions",
		UniqueID:          fmt.Sprintf("jetkvm_%s_active_sessions", m.deviceID),
		StateTopic:        m.topic("sessions", "state"),
		ValueTemplate:     "{{ value_json.active_sessions }}",
		Icon:              "mdi:account-multiple",
		StateClass:        "measurement",
		AvailabilityTopic: availTopic,
		AvailTemplate:     availTemplate,
		Device:            device,
	})

	// Binary sensor: USB State
	m.publishDiscovery("binary_sensor", "usb_state", haDiscoveryPayload{
		Name:              "USB Connected",
		UniqueID:          fmt.Sprintf("jetkvm_%s_usb_state", m.deviceID),
		StateTopic:        m.topic("usb", "state"),
		ValueTemplate:     "{{ 'ON' if value_json.state == 'configured' else 'OFF' }}",
		DeviceClass:       "connectivity",
		Icon:              "mdi:usb",
		AvailabilityTopic: availTopic,
		AvailTemplate:     availTemplate,
		Device:            device,
	})

	// Sensor: IP Address (disabled by default)
	m.publishDiscovery("sensor", "ip_address", haDiscoveryPayload{
		Name:              "IP Address",
		UniqueID:          fmt.Sprintf("jetkvm_%s_ip_address", m.deviceID),
		StateTopic:        m.topic("network", "state"),
		ValueTemplate:     "{{ value_json.ip_address }}",
		Icon:              "mdi:ip-network",
		EntityCategory:    "diagnostic",
		EnabledByDefault:  enabledByDefault,
		AvailabilityTopic: availTopic,
		AvailTemplate:     availTemplate,
		Device:            device,
	})

	// Sensor: Hostname (disabled by default)
	m.publishDiscovery("sensor", "hostname", haDiscoveryPayload{
		Name:              "Hostname",
		UniqueID:          fmt.Sprintf("jetkvm_%s_hostname", m.deviceID),
		StateTopic:        m.topic("network", "state"),
		ValueTemplate:     "{{ value_json.hostname }}",
		Icon:              "mdi:dns",
		EntityCategory:    "diagnostic",
		EnabledByDefault:  enabledByDefault,
		AvailabilityTopic: availTopic,
		AvailTemplate:     availTemplate,
		Device:            device,
	})

	// Sensor: CPU Load (diagnostic, disabled by default)
	m.publishDiscovery("sensor", "cpu_load", haDiscoveryPayload{
		Name:              "CPU Load",
		UniqueID:          fmt.Sprintf("jetkvm_%s_cpu_load", m.deviceID),
		StateTopic:        m.topic("system", "state"),
		ValueTemplate:     "{{ value_json.cpu_load | round(2) }}",
		StateClass:        "measurement",
		Icon:              "mdi:chip",
		EntityCategory:    "diagnostic",
		EnabledByDefault:  enabledByDefault,
		AvailabilityTopic: availTopic,
		AvailTemplate:     availTemplate,
		Device:            device,
	})

	// Sensor: Temperature
	m.publishDiscovery("sensor", "temperature", haDiscoveryPayload{
		Name:              "Temperature",
		UniqueID:          fmt.Sprintf("jetkvm_%s_temperature", m.deviceID),
		StateTopic:        m.topic("system", "state"),
		ValueTemplate:     "{{ value_json.temperature | round(1) }}",
		DeviceClass:       "temperature",
		UnitOfMeasurement: "°C",
		StateClass:        "measurement",
		EntityCategory:    "diagnostic",
		AvailabilityTopic: availTopic,
		AvailTemplate:     availTemplate,
		Device:            device,
	})

	// Sensor: Memory Usage (diagnostic, disabled by default)
	m.publishDiscovery("sensor", "memory_used", haDiscoveryPayload{
		Name:              "Memory Used",
		UniqueID:          fmt.Sprintf("jetkvm_%s_memory_used", m.deviceID),
		StateTopic:        m.topic("system", "state"),
		ValueTemplate:     "{{ (value_json.memory_used / 1048576) | round(1) }}",
		UnitOfMeasurement: "MB",
		Icon:              "mdi:memory",
		StateClass:        "measurement",
		EntityCategory:    "diagnostic",
		EnabledByDefault:  enabledByDefault,
		AvailabilityTopic: availTopic,
		AvailTemplate:     availTemplate,
		Device:            device,
	})

	// Sensor: Storage Used (diagnostic, disabled by default)
	m.publishDiscovery("sensor", "storage_used", haDiscoveryPayload{
		Name:              "Storage Used",
		UniqueID:          fmt.Sprintf("jetkvm_%s_storage_used", m.deviceID),
		StateTopic:        m.topic("system", "state"),
		ValueTemplate:     "{{ (value_json.storage_used / 1048576) | round(1) }}",
		DeviceClass:       "data_size",
		UnitOfMeasurement: "MB",
		StateClass:        "measurement",
		EntityCategory:    "diagnostic",
		EnabledByDefault:  enabledByDefault,
		AvailabilityTopic: availTopic,
		AvailTemplate:     availTemplate,
		Device:            device,
	})

	// Sensor: Storage Free (diagnostic, disabled by default)
	m.publishDiscovery("sensor", "storage_free", haDiscoveryPayload{
		Name:              "Storage Free",
		UniqueID:          fmt.Sprintf("jetkvm_%s_storage_free", m.deviceID),
		StateTopic:        m.topic("system", "state"),
		ValueTemplate:     "{{ (value_json.storage_free / 1048576) | round(1) }}",
		DeviceClass:       "data_size",
		UnitOfMeasurement: "MB",
		StateClass:        "measurement",
		EntityCategory:    "diagnostic",
		EnabledByDefault:  enabledByDefault,
		AvailabilityTopic: availTopic,
		AvailTemplate:     availTemplate,
		Device:            device,
	})

	// Virtual Media: Select (actions enabled) or Sensor (actions disabled)
	actionsEnabled := config.MqttConfig != nil && config.MqttConfig.EnableActions
	vmAttrsTopic := m.topic("virtual_media", "state")
	vmAttrsTemplate := "{{ {'source': value_json.source} | tojson }}"
	if actionsEnabled {
		// Build options list, including currently mounted URL image if applicable
		vmOptions := getAvailableImages()
		virtualMediaStateMutex.RLock()
		if currentVirtualMediaState != nil && currentVirtualMediaState.Source == HTTP {
			imageName := currentVirtualMediaState.URL
			if parsed, err := url.Parse(currentVirtualMediaState.URL); err == nil {
				base := path.Base(parsed.Path)
				if base != "" && base != "." && base != "/" {
					imageName = base
				}
			}
			vmOptions = append(vmOptions, imageName)
		}
		virtualMediaStateMutex.RUnlock()

		// Remove read-only sensor variant if it exists, then publish select
		m.removeDiscovery("sensor", "virtual_media")
		m.publishDiscovery("select", "virtual_media", haDiscoveryPayload{
			Name:                   "Virtual Media",
			UniqueID:               fmt.Sprintf("jetkvm_%s_virtual_media", m.deviceID),
			StateTopic:             m.topic("virtual_media", "state"),
			CommandTopic:           m.topic("virtual_media", "set"),
			ValueTemplate:          "{{ value_json.mounted_image }}",
			Options:                vmOptions,
			Icon:                   "mdi:disc",
			JsonAttributesTopic:    vmAttrsTopic,
			JsonAttributesTemplate: vmAttrsTemplate,
			AvailabilityTopic:      availTopic,
			AvailTemplate:          availTemplate,
			Device:                 device,
		})
	} else {
		// Remove select variant, then publish read-only sensor
		m.removeDiscovery("select", "virtual_media")
		m.publishDiscovery("sensor", "virtual_media", haDiscoveryPayload{
			Name:                   "Virtual Media",
			UniqueID:               fmt.Sprintf("jetkvm_%s_virtual_media", m.deviceID),
			StateTopic:             m.topic("virtual_media", "state"),
			ValueTemplate:          "{{ value_json.mounted_image }}",
			Icon:                   "mdi:disc",
			JsonAttributesTopic:    vmAttrsTopic,
			JsonAttributesTemplate: vmAttrsTemplate,
			AvailabilityTopic:      availTopic,
			AvailTemplate:          availTemplate,
			Device:                 device,
		})
	}

	// Mouse Jiggler: Switch (actions enabled) or Binary Sensor (actions disabled)
	if actionsEnabled {
		// Remove read-only variant if it exists, then publish switch
		m.removeDiscovery("binary_sensor", "jiggler")
		m.publishDiscovery("switch", "jiggler", haDiscoveryPayload{
			Name:              "Mouse Jiggler",
			UniqueID:          fmt.Sprintf("jetkvm_%s_jiggler", m.deviceID),
			StateTopic:        m.topic("jiggler", "state"),
			CommandTopic:      m.topic("jiggler", "set"),
			ValueTemplate:     "{{ 'ON' if value_json.enabled else 'OFF' }}",
			PayloadOn:         "ON",
			PayloadOff:        "OFF",
			Icon:              "mdi:mouse",
			AvailabilityTopic: availTopic,
			AvailTemplate:     availTemplate,
			Device:            device,
		})
	} else {
		// Remove switch variant, then publish read-only binary sensor
		m.removeDiscovery("switch", "jiggler")
		m.publishDiscovery("binary_sensor", "jiggler", haDiscoveryPayload{
			Name:              "Mouse Jiggler",
			UniqueID:          fmt.Sprintf("jetkvm_%s_jiggler", m.deviceID),
			StateTopic:        m.topic("jiggler", "state"),
			ValueTemplate:     "{{ 'ON' if value_json.enabled else 'OFF' }}",
			Icon:              "mdi:mouse",
			AvailabilityTopic: availTopic,
			AvailTemplate:     availTemplate,
			Device:            device,
		})
	}

	// Reboot Button: only when actions enabled
	if actionsEnabled {
		m.publishDiscovery("button", "reboot", haDiscoveryPayload{
			Name:              "Reboot",
			UniqueID:          fmt.Sprintf("jetkvm_%s_reboot", m.deviceID),
			CommandTopic:      m.topic("reboot", "set"),
			PayloadPress:      "PRESS",
			DeviceClass:       "restart",
			Icon:              "mdi:restart",
			EntityCategory:    "config",
			AvailabilityTopic: availTopic,
			AvailTemplate:     availTemplate,
			Device:            device,
		})
	} else {
		m.removeDiscovery("button", "reboot")
	}

	// Firmware Update: always published, but command_topic only when actions enabled.
	// NOTE: Do NOT use value_template/latest_version_template here — HA needs to parse
	// the full JSON directly to recognize in_progress and update_percentage fields.
	firmwarePayload := haDiscoveryPayload{
		Name:              "Firmware",
		UniqueID:          fmt.Sprintf("jetkvm_%s_firmware", m.deviceID),
		StateTopic:        m.topic("update", "state"),
		DeviceClass:       "firmware",
		EntityCategory:    "config",
		ReleaseURL:        "https://github.com/jetkvm/kvm/releases",
		AvailabilityTopic: availTopic,
		AvailTemplate:     availTemplate,
		Device:            device,
	}
	if actionsEnabled {
		firmwarePayload.CommandTopic = m.topic("update", "install")
		firmwarePayload.PayloadInstall = "INSTALL"
	}
	m.publishDiscovery("update", "firmware", firmwarePayload)

	// --- Extension-dependent entities ---

	activeExtension := config.ActiveExtension

	switch activeExtension {
	case "atx-power":
		m.publishATXDiscovery(device, availTopic, availTemplate, actionsEnabled)
		m.removeDCDiscovery()
	case "dc-power":
		m.publishDCDiscovery(device, availTopic, availTemplate, actionsEnabled)
		m.removeATXDiscovery()
	default:
		m.removeATXDiscovery()
		m.removeDCDiscovery()
	}

	mqttLogger.Info().Str("extension", activeExtension).Msg("published Home Assistant discovery configs")
}

func (m *MQTTManager) publishATXDiscovery(device *haDevice, availTopic, availTemplate string, actionsEnabled bool) {
	// Binary sensor: ATX Power LED (always published as read-only)
	m.publishDiscovery("binary_sensor", "power_led", haDiscoveryPayload{
		Name:              "ATX Power LED",
		UniqueID:          fmt.Sprintf("jetkvm_%s_power_led", m.deviceID),
		StateTopic:        m.topic("atx", "state"),
		ValueTemplate:     "{{ 'ON' if value_json.power else 'OFF' }}",
		Icon:              "mdi:led-on",
		AvailabilityTopic: availTopic,
		AvailTemplate:     availTemplate,
		Device:            device,
	})

	// Binary sensor: ATX HDD LED (always published as read-only)
	m.publishDiscovery("binary_sensor", "hdd_led", haDiscoveryPayload{
		Name:              "ATX HDD LED",
		UniqueID:          fmt.Sprintf("jetkvm_%s_hdd_led", m.deviceID),
		StateTopic:        m.topic("atx", "state"),
		ValueTemplate:     "{{ 'ON' if value_json.hdd else 'OFF' }}",
		Icon:              "mdi:harddisk",
		AvailabilityTopic: availTopic,
		AvailTemplate:     availTemplate,
		Device:            device,
	})

	// ATX Buttons: only when actions enabled
	if actionsEnabled {
		m.publishDiscovery("button", "atx_power_short", haDiscoveryPayload{
			Name:              "ATX Power (Short Press)",
			UniqueID:          fmt.Sprintf("jetkvm_%s_atx_power_short", m.deviceID),
			CommandTopic:      m.topic("atx_power_short", "set"),
			PayloadPress:      "PRESS",
			Icon:              "mdi:power",
			AvailabilityTopic: availTopic,
			AvailTemplate:     availTemplate,
			Device:            device,
		})

		m.publishDiscovery("button", "atx_power_long", haDiscoveryPayload{
			Name:              "ATX Power (Long Press)",
			UniqueID:          fmt.Sprintf("jetkvm_%s_atx_power_long", m.deviceID),
			CommandTopic:      m.topic("atx_power_long", "set"),
			PayloadPress:      "PRESS",
			Icon:              "mdi:power",
			AvailabilityTopic: availTopic,
			AvailTemplate:     availTemplate,
			Device:            device,
		})

		m.publishDiscovery("button", "atx_reset", haDiscoveryPayload{
			Name:              "ATX Reset",
			UniqueID:          fmt.Sprintf("jetkvm_%s_atx_reset", m.deviceID),
			CommandTopic:      m.topic("atx_reset", "set"),
			PayloadPress:      "PRESS",
			Icon:              "mdi:restart",
			AvailabilityTopic: availTopic,
			AvailTemplate:     availTemplate,
			Device:            device,
		})
	} else {
		m.removeDiscovery("button", "atx_power_short")
		m.removeDiscovery("button", "atx_power_long")
		m.removeDiscovery("button", "atx_reset")
	}
}

func (m *MQTTManager) publishDCDiscovery(device *haDevice, availTopic, availTemplate string, actionsEnabled bool) {
	// Sensor: DC Voltage (always read-only)
	m.publishDiscovery("sensor", "voltage", haDiscoveryPayload{
		Name:              "DC Voltage",
		UniqueID:          fmt.Sprintf("jetkvm_%s_voltage", m.deviceID),
		StateTopic:        m.topic("dc", "state"),
		ValueTemplate:     "{{ value_json.voltage | round(2) }}",
		DeviceClass:       "voltage",
		UnitOfMeasurement: "V",
		StateClass:        "measurement",
		AvailabilityTopic: availTopic,
		AvailTemplate:     availTemplate,
		Device:            device,
	})

	// Sensor: DC Current (always read-only)
	m.publishDiscovery("sensor", "current", haDiscoveryPayload{
		Name:              "DC Current",
		UniqueID:          fmt.Sprintf("jetkvm_%s_current", m.deviceID),
		StateTopic:        m.topic("dc", "state"),
		ValueTemplate:     "{{ value_json.current | round(3) }}",
		DeviceClass:       "current",
		UnitOfMeasurement: "A",
		StateClass:        "measurement",
		AvailabilityTopic: availTopic,
		AvailTemplate:     availTemplate,
		Device:            device,
	})

	// Sensor: DC Power (always read-only)
	m.publishDiscovery("sensor", "power", haDiscoveryPayload{
		Name:              "DC Power",
		UniqueID:          fmt.Sprintf("jetkvm_%s_power", m.deviceID),
		StateTopic:        m.topic("dc", "state"),
		ValueTemplate:     "{{ value_json.power | round(2) }}",
		DeviceClass:       "power",
		UnitOfMeasurement: "W",
		StateClass:        "measurement",
		AvailabilityTopic: availTopic,
		AvailTemplate:     availTemplate,
		Device:            device,
	})

	// DC Power: Switch (actions enabled) or Binary Sensor (actions disabled)
	if actionsEnabled {
		// Remove read-only variant if it exists, then publish switch
		m.removeDiscovery("binary_sensor", "dc_power")
		m.publishDiscovery("switch", "dc_power", haDiscoveryPayload{
			Name:              "DC Power",
			UniqueID:          fmt.Sprintf("jetkvm_%s_dc_power", m.deviceID),
			StateTopic:        m.topic("dc", "state"),
			CommandTopic:      m.topic("dc_power", "set"),
			ValueTemplate:     "{{ 'ON' if value_json.isOn else 'OFF' }}",
			PayloadOn:         "ON",
			PayloadOff:        "OFF",
			DeviceClass:       "switch",
			Icon:              "mdi:power",
			AvailabilityTopic: availTopic,
			AvailTemplate:     availTemplate,
			Device:            device,
		})
	} else {
		// Remove switch variant, then publish read-only binary sensor
		m.removeDiscovery("switch", "dc_power")
		m.publishDiscovery("binary_sensor", "dc_power", haDiscoveryPayload{
			Name:              "DC Power",
			UniqueID:          fmt.Sprintf("jetkvm_%s_dc_power", m.deviceID),
			StateTopic:        m.topic("dc", "state"),
			ValueTemplate:     "{{ 'ON' if value_json.isOn else 'OFF' }}",
			DeviceClass:       "power",
			Icon:              "mdi:power",
			AvailabilityTopic: availTopic,
			AvailTemplate:     availTemplate,
			Device:            device,
		})
	}

	// DC Restore on Power Loss: Select (actions enabled) or Sensor (actions disabled)
	// Only published when the DC extension firmware supports the restore feature (restoreState != -1)
	dcRestoreValueTemplate := "{% set rs = value_json.restoreState | int %}{% if rs == 0 %}off{% elif rs == 1 %}on{% elif rs == 2 %}last_state{% else %}unknown{% endif %}"
	if getDCState().RestoreState >= 0 {
		if actionsEnabled {
			m.removeDiscovery("sensor", "dc_restore")
			m.publishDiscovery("select", "dc_restore", haDiscoveryPayload{
				Name:              "Restore on Power Loss",
				UniqueID:          fmt.Sprintf("jetkvm_%s_dc_restore", m.deviceID),
				StateTopic:        m.topic("dc", "state"),
				CommandTopic:      m.topic("dc_restore", "set"),
				ValueTemplate:     dcRestoreValueTemplate,
				Options:           []string{"off", "on", "last_state"},
				Icon:              "mdi:power-settings",
				EntityCategory:    "config",
				AvailabilityTopic: availTopic,
				AvailTemplate:     availTemplate,
				Device:            device,
			})
		} else {
			m.removeDiscovery("select", "dc_restore")
			m.publishDiscovery("sensor", "dc_restore", haDiscoveryPayload{
				Name:              "Restore on Power Loss",
				UniqueID:          fmt.Sprintf("jetkvm_%s_dc_restore_ro", m.deviceID),
				StateTopic:        m.topic("dc", "state"),
				ValueTemplate:     dcRestoreValueTemplate,
				Icon:              "mdi:power-settings",
				AvailabilityTopic: availTopic,
				AvailTemplate:     availTemplate,
				Device:            device,
			})
		}
	} else {
		// Feature not supported: remove both variants
		m.removeDiscovery("select", "dc_restore")
		m.removeDiscovery("sensor", "dc_restore")
	}
}

func (m *MQTTManager) publishDiscovery(component, objectID string, payload haDiscoveryPayload) {
	payload.DefaultEntityID = fmt.Sprintf("%s.jetkvm_%s_%s", component, m.deviceID, objectID)
	discoveryTopic := fmt.Sprintf("homeassistant/%s/jetkvm_%s/%s/config", component, m.deviceID, objectID)
	m.publish(discoveryTopic, payload, true)
}

// removeDiscovery removes a single HA discovery entity by publishing an empty retained payload.
func (m *MQTTManager) removeDiscovery(component, objectID string) {
	discoveryTopic := fmt.Sprintf("homeassistant/%s/jetkvm_%s/%s/config", component, m.deviceID, objectID)
	m.publishString(discoveryTopic, "", true)
}

// removeATXDiscovery removes all ATX-related HA discovery entities.
func (m *MQTTManager) removeATXDiscovery() {
	m.removeDiscovery("binary_sensor", "power_led")
	m.removeDiscovery("binary_sensor", "hdd_led")
	m.removeDiscovery("button", "atx_power_short")
	m.removeDiscovery("button", "atx_power_long")
	m.removeDiscovery("button", "atx_reset")
}

// removeDCDiscovery removes all DC-related HA discovery entities.
func (m *MQTTManager) removeDCDiscovery() {
	m.removeDiscovery("sensor", "voltage")
	m.removeDiscovery("sensor", "current")
	m.removeDiscovery("sensor", "power")
	m.removeDiscovery("switch", "dc_power")
	m.removeDiscovery("binary_sensor", "dc_power")
	m.removeDiscovery("select", "dc_restore")
	m.removeDiscovery("sensor", "dc_restore")
}

// removeAllDiscovery removes all HA discovery entries (general + extension-specific).
func (m *MQTTManager) removeAllDiscovery() {
	// General entities
	m.removeDiscovery("binary_sensor", "online")
	m.removeDiscovery("binary_sensor", "video_signal")
	m.removeDiscovery("sensor", "video_resolution")
	m.removeDiscovery("sensor", "video_fps")
	m.removeDiscovery("binary_sensor", "cloud_connected")
	m.removeDiscovery("sensor", "active_sessions")
	m.removeDiscovery("binary_sensor", "usb_state")
	m.removeDiscovery("sensor", "ip_address")
	m.removeDiscovery("sensor", "hostname")
	// System metrics
	m.removeDiscovery("sensor", "cpu_load")
	m.removeDiscovery("sensor", "temperature")
	m.removeDiscovery("sensor", "memory_used")
	m.removeDiscovery("sensor", "storage_used")
	m.removeDiscovery("sensor", "storage_free")
	// Virtual media can be select or sensor depending on actions setting
	m.removeDiscovery("select", "virtual_media")
	m.removeDiscovery("sensor", "virtual_media")
	// Jiggler can be switch or binary_sensor depending on actions setting
	m.removeDiscovery("switch", "jiggler")
	m.removeDiscovery("binary_sensor", "jiggler")
	m.removeDiscovery("button", "reboot")
	m.removeDiscovery("update", "firmware")

	// Extension-specific entities (both switch and binary_sensor variants)
	m.removeATXDiscovery()
	m.removeDCDiscovery()
	// Also remove read-only DC power variant
	m.removeDiscovery("binary_sensor", "dc_power")

	mqttLogger.Info().Msg("removed all HA discovery entries")
}

// cleanupAllTopics removes all discovery entries and clears all state topics.
func (m *MQTTManager) cleanupAllTopics() {
	// Remove all HA discovery entries
	m.removeAllDiscovery()

	// Clear all state topics by publishing empty retained messages
	stateTopics := []string{
		m.topic("status"),
		m.topic("video", "state"),
		m.topic("cloud", "state"),
		m.topic("sessions", "state"),
		m.topic("usb", "state"),
		m.topic("jiggler", "state"),
		m.topic("network", "state"),
		m.topic("system", "state"),
		m.topic("virtual_media", "state"),
		m.topic("update", "state"),
		m.topic("atx", "state"),
		m.topic("dc", "state"),
	}
	for _, t := range stateTopics {
		m.publishString(t, "", true)
	}

	mqttLogger.Info().Msg("cleaned up all MQTT topics and discovery entries")
}

// republishHADiscovery removes old extension entities and re-publishes all discovery configs.
// Call this when the active extension changes.
func (m *MQTTManager) republishHADiscovery() {
	if !m.IsConnected() {
		return
	}
	if config.MqttConfig == nil || !config.MqttConfig.EnableHADiscovery {
		return
	}

	// Re-publish all discovery configs (publishHADiscovery handles removal of inactive extension entities)
	m.publishHADiscovery()

	mqttLogger.Info().Str("extension", config.ActiveExtension).Msg("republished HA discovery after extension change")
}
