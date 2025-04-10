package kvm

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/jetkvm/kvm/internal/usbgadget"
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
	CloudURL             string             `json:"cloud_url"`
	CloudAppURL          string             `json:"cloud_app_url"`
	CloudToken           string             `json:"cloud_token"`
	GoogleIdentity       string             `json:"google_identity"`
	JigglerEnabled       bool               `json:"jiggler_enabled"`
	AutoUpdateEnabled    bool               `json:"auto_update_enabled"`
	IncludePreRelease    bool               `json:"include_pre_release"`
	HashedPassword       string             `json:"hashed_password"`
	LocalAuthToken       string             `json:"local_auth_token"`
	LocalAuthMode        string             `json:"localAuthMode"` //TODO: fix it with migration
	WakeOnLanDevices     []WakeOnLanDevice  `json:"wake_on_lan_devices"`
	KeyboardMacros       []KeyboardMacro    `json:"keyboard_macros"`
	EdidString           string             `json:"hdmi_edid_string"`
	ActiveExtension      string             `json:"active_extension"`
	DisplayMaxBrightness int                `json:"display_max_brightness"`
	DisplayDimAfterSec   int                `json:"display_dim_after_sec"`
	DisplayOffAfterSec   int                `json:"display_off_after_sec"`
	TLSMode              string             `json:"tls_mode"`
	UsbConfig            *usbgadget.Config  `json:"usb_config"`
	UsbDevices           *usbgadget.Devices `json:"usb_devices"`
}

const configPath = "/userdata/kvm_config.json"

var defaultConfig = &Config{
	CloudURL:             "https://api.jetkvm.com",
	CloudAppURL:          "https://app.jetkvm.com",
	AutoUpdateEnabled:    true, // Set a default value
	ActiveExtension:      "",
	KeyboardMacros:       []KeyboardMacro{},
	DisplayMaxBrightness: 64,
	DisplayDimAfterSec:   120,  // 2 minutes
	DisplayOffAfterSec:   1800, // 30 minutes
	TLSMode:              "",
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
}

var (
	config     *Config
	configLock = &sync.Mutex{}
)

func LoadConfig() {
	configLock.Lock()
	defer configLock.Unlock()

	if config != nil {
		logger.Info().Msg("config already loaded, skipping")
		return
	}

	// load the default config
	config = defaultConfig

	file, err := os.Open(configPath)
	if err != nil {
		logger.Debug().Msg("default config file doesn't exist, using default")
		return
	}
	defer file.Close()

	// load and merge the default config with the user config
	loadedConfig := *defaultConfig
	if err := json.NewDecoder(file).Decode(&loadedConfig); err != nil {
		logger.Warn().Err(err).Msg("config file JSON parsing failed")
		return
	}

	// merge the user config with the default config
	if loadedConfig.UsbConfig == nil {
		loadedConfig.UsbConfig = defaultConfig.UsbConfig
	}

	if loadedConfig.UsbDevices == nil {
		loadedConfig.UsbDevices = defaultConfig.UsbDevices
	}

	config = &loadedConfig
}

func SaveConfig() error {
	configLock.Lock()
	defer configLock.Unlock()

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

	return nil
}

func ensureConfigLoaded() {
	if config == nil {
		LoadConfig()
	}
}
