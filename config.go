package kvm

import (
	"encoding/json"
	"fmt"
	"os"
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
	DisplayMaxBrightness int               `json:"display_max_brightness"`
	DisplayDimAfterMs    int64             `json:"display_dim_after_ms"`
	DisplayOffAfterMs    int64             `json:"display_off_after_ms"`
}

const configPath = "/userdata/kvm_config.json"

var defaultConfig = &Config{
	CloudURL:             "https://api.jetkvm.com",
	AutoUpdateEnabled:    true, // Set a default value
	DisplayMaxBrightness: 100,
	DisplayDimAfterMs:    0,
	DisplayOffAfterMs:    0,
}

var config *Config

func LoadConfig() {
	if config != nil {
		return
	}

	file, err := os.Open(configPath)
	if err != nil {
		logger.Debug("default config file doesn't exist, using default")
		config = defaultConfig
		return
	}
	defer file.Close()

	var loadedConfig Config
	if err := json.NewDecoder(file).Decode(&loadedConfig); err != nil {
		logger.Errorf("config file JSON parsing failed, %v", err)
		config = defaultConfig
		return
	}

	config = &loadedConfig
}

func SaveConfig() error {
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
