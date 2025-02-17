package kvm

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

type WakeOnLanDevice struct {
	Name       string `json:"name"`
	MacAddress string `json:"macAddress"`
}

type Config struct {
	CloudURL             string            `json:"cloud_url"`
	CloudToken           string            `json:"cloud_token"`
	GoogleIdentity       string            `json:"google_identity"`
	JigglerEnabled       bool              `json:"jiggler_enabled"`
	AutoUpdateEnabled    bool              `json:"auto_update_enabled"`
	IncludePreRelease    bool              `json:"include_pre_release"`
	HashedPassword       string            `json:"hashed_password"`
	LocalAuthToken       string            `json:"local_auth_token"`
	LocalAuthMode        string            `json:"localAuthMode"` //TODO: fix it with migration
	WakeOnLanDevices     []WakeOnLanDevice `json:"wake_on_lan_devices"`
	EdidString           string            `json:"hdmi_edid_string"`
	ActiveExtension      string            `json:"active_extension"`
	DisplayMaxBrightness int               `json:"display_max_brightness"`
	DisplayDimAfterSec   int               `json:"display_dim_after_sec"`
	DisplayOffAfterSec   int               `json:"display_off_after_sec"`
}

const configPath = "/userdata/kvm_config.json"

var defaultConfig = &Config{
	CloudURL:             "https://api.jetkvm.com",
	AutoUpdateEnabled:    true, // Set a default value
	ActiveExtension:      "",
	DisplayMaxBrightness: 64,
	DisplayDimAfterSec:   120,  // 2 minutes
	DisplayOffAfterSec:   1800, // 30 minutes
}

var (
	config     *Config
	configLock = &sync.Mutex{}
)

func LoadConfig() {
	if config != nil {
		logger.Info("config already loaded, skipping")
		return
	}

	file, err := os.Open(configPath)
	if err != nil {
		logger.Debug("default config file doesn't exist, using default")
		return
	}
	defer file.Close()

	// load and merge the default config with the user config
	loadedConfig := *defaultConfig
	if err := json.NewDecoder(file).Decode(&loadedConfig); err != nil {
		logger.Errorf("config file JSON parsing failed, %v", err)
		return
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
