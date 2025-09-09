package kvm

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/jetkvm/kvm/internal/logging"
	"github.com/jetkvm/kvm/internal/network"
	"github.com/jetkvm/kvm/internal/usbgadget"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type WakeOnLanDevice struct {
	Name       string `json:"name"`
	MacAddress string `json:"macAddress"`
}

// Constants for keyboard macro limits
const (
	MaxMacrosPerDevice = 25
	MaxStepsPerMacro   = 10
	MaxKeysPerStep     = 10
	MinStepDelay       = 50
	MaxStepDelay       = 2000
)

type KeyboardMacroStep struct {
	Keys      []string `json:"keys"`
	Modifiers []string `json:"modifiers"`
	Delay     int      `json:"delay"`
}

func (s *KeyboardMacroStep) Validate() error {
	if len(s.Keys) > MaxKeysPerStep {
		return fmt.Errorf("too many keys in step (max %d)", MaxKeysPerStep)
	}

	if s.Delay < MinStepDelay {
		s.Delay = MinStepDelay
	} else if s.Delay > MaxStepDelay {
		s.Delay = MaxStepDelay
	}

	return nil
}

type KeyboardMacro struct {
	ID        string              `json:"id"`
	Name      string              `json:"name"`
	Steps     []KeyboardMacroStep `json:"steps"`
	SortOrder int                 `json:"sortOrder,omitempty"`
}

func (m *KeyboardMacro) Validate() error {
	if m.Name == "" {
		return fmt.Errorf("macro name cannot be empty")
	}

	if len(m.Steps) == 0 {
		return fmt.Errorf("macro must have at least one step")
	}

	if len(m.Steps) > MaxStepsPerMacro {
		return fmt.Errorf("too many steps in macro (max %d)", MaxStepsPerMacro)
	}

	for i := range m.Steps {
		if err := m.Steps[i].Validate(); err != nil {
			return fmt.Errorf("invalid step %d: %w", i+1, err)
		}
	}

	return nil
}

type Config struct {
	CloudURL             string                 `json:"cloud_url"`
	CloudAppURL          string                 `json:"cloud_app_url"`
	CloudToken           string                 `json:"cloud_token"`
	GoogleIdentity       string                 `json:"google_identity"`
	JigglerEnabled       bool                   `json:"jiggler_enabled"`
	JigglerConfig        *JigglerConfig         `json:"jiggler_config"`
	AutoUpdateEnabled    bool                   `json:"auto_update_enabled"`
	IncludePreRelease    bool                   `json:"include_pre_release"`
	HashedPassword       string                 `json:"hashed_password"`
	LocalAuthToken       string                 `json:"local_auth_token"`
	LocalAuthMode        string                 `json:"localAuthMode"` //TODO: fix it with migration
	LocalLoopbackOnly    bool                   `json:"local_loopback_only"`
	WakeOnLanDevices     []WakeOnLanDevice      `json:"wake_on_lan_devices"`
	KeyboardMacros       []KeyboardMacro        `json:"keyboard_macros"`
	KeyboardLayout       string                 `json:"keyboard_layout"`
	EdidString           string                 `json:"hdmi_edid_string"`
	ActiveExtension      string                 `json:"active_extension"`
	DisplayRotation      string                 `json:"display_rotation"`
	DisplayMaxBrightness int                    `json:"display_max_brightness"`
	DisplayDimAfterSec   int                    `json:"display_dim_after_sec"`
	DisplayOffAfterSec   int                    `json:"display_off_after_sec"`
	TLSMode              string                 `json:"tls_mode"` // options: "self-signed", "user-defined", ""
	UsbConfig            *usbgadget.Config      `json:"usb_config"`
	UsbDevices           *usbgadget.Devices     `json:"usb_devices"`
	NetworkConfig        *network.NetworkConfig `json:"network_config"`
	DefaultLogLevel      string                 `json:"default_log_level"`
}

const configPath = "/userdata/kvm_config.json"

var defaultConfig = &Config{
	CloudURL:             "https://api.jetkvm.com",
	CloudAppURL:          "https://app.jetkvm.com",
	AutoUpdateEnabled:    true, // Set a default value
	ActiveExtension:      "",
	KeyboardMacros:       []KeyboardMacro{},
	DisplayRotation:      "270",
	KeyboardLayout:       "en-US",
	DisplayMaxBrightness: 64,
	DisplayDimAfterSec:   120,  // 2 minutes
	DisplayOffAfterSec:   1800, // 30 minutes
	JigglerEnabled:       false,
	// This is the "Standard" jiggler option in the UI
	JigglerConfig: &JigglerConfig{
		InactivityLimitSeconds: 60,
		JitterPercentage:       25,
		ScheduleCronTab:        "0 * * * * *",
		Timezone:               "UTC",
	},
	TLSMode: "",
	UsbConfig: &usbgadget.Config{
		VendorId:     "0x1d6b", //The Linux Foundation
		ProductId:    "0x0104", //Multifunction Composite Gadget
		SerialNumber: "",
		Manufacturer: "JetKVM",
		Product:      "USB Emulation Device",
	},
	UsbDevices: &usbgadget.Devices{
		AbsoluteMouse: true,
		RelativeMouse: true,
		Keyboard:      true,
		MassStorage:   true,
	},
	NetworkConfig:   &network.NetworkConfig{},
	DefaultLogLevel: "INFO",
}

var (
	config     *Config
	configLock = &sync.Mutex{}
)

var (
	configSuccess = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "jetkvm_config_last_reload_successful",
			Help: "The last configuration load succeeded",
		},
	)
	configSuccessTime = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "jetkvm_config_last_reload_success_timestamp_seconds",
			Help: "Timestamp of last successful config load",
		},
	)
)

func LoadConfig() {
	configLock.Lock()
	defer configLock.Unlock()

	if config != nil {
		logger.Debug().Msg("config already loaded, skipping")
		return
	}

	// load the default config
	config = defaultConfig

	file, err := os.Open(configPath)
	if err != nil {
		logger.Debug().Msg("default config file doesn't exist, using default")
		configSuccess.Set(1.0)
		configSuccessTime.SetToCurrentTime()
		return
	}
	defer file.Close()

	// load and merge the default config with the user config
	loadedConfig := *defaultConfig
	if err := json.NewDecoder(file).Decode(&loadedConfig); err != nil {
		logger.Warn().Err(err).Msg("config file JSON parsing failed")
		configSuccess.Set(0.0)
		return
	}

	// merge the user config with the default config
	if loadedConfig.UsbConfig == nil {
		loadedConfig.UsbConfig = defaultConfig.UsbConfig
	}

	if loadedConfig.UsbDevices == nil {
		loadedConfig.UsbDevices = defaultConfig.UsbDevices
	}

	if loadedConfig.NetworkConfig == nil {
		loadedConfig.NetworkConfig = defaultConfig.NetworkConfig
	}

	if loadedConfig.JigglerConfig == nil {
		loadedConfig.JigglerConfig = defaultConfig.JigglerConfig
	}

	// fixup old keyboard layout value
	if loadedConfig.KeyboardLayout == "en_US" {
		loadedConfig.KeyboardLayout = "en-US"
	}

	config = &loadedConfig

	logging.GetRootLogger().UpdateLogLevel(config.DefaultLogLevel)

	configSuccess.Set(1.0)
	configSuccessTime.SetToCurrentTime()

	logger.Info().Str("path", configPath).Msg("config loaded")
}

func SaveConfig() error {
	configLock.Lock()
	defer configLock.Unlock()

	logger.Trace().Str("path", configPath).Msg("Saving config")

	// fixup old keyboard layout value
	if config.KeyboardLayout == "en_US" {
		config.KeyboardLayout = "en-US"
	}

	file, err := os.Create(configPath)
	if err != nil {
		return fmt.Errorf("failed to create config file: %w", err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(config); err != nil {
		return fmt.Errorf("failed to encode config: %w", err)
	}

	if err := file.Sync(); err != nil {
		return fmt.Errorf("failed to wite config: %w", err)
	}

	logger.Info().Str("path", configPath).Msg("config saved")
	return nil
}

func ensureConfigLoaded() {
	if config == nil {
		LoadConfig()
	}
}
