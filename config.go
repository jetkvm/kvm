package kvm

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/jetkvm/kvm/internal/confparser"
	"github.com/jetkvm/kvm/internal/logging"
	"github.com/jetkvm/kvm/internal/native"
	"github.com/jetkvm/kvm/internal/network/types"
	"github.com/jetkvm/kvm/internal/usbgadget"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	DefaultAPIURL = "https://api.jetkvm.com"
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
	CloudURL             string               `json:"cloud_url"`
	UpdateAPIURL         string               `json:"update_api_url"`
	CloudAppURL          string               `json:"cloud_app_url"`
	CloudToken           string               `json:"cloud_token"`
	GoogleIdentity       string               `json:"google_identity"`
	JigglerEnabled       bool                 `json:"jiggler_enabled"`
	JigglerConfig        *JigglerConfig       `json:"jiggler_config"`
	AutoUpdateEnabled    bool                 `json:"auto_update_enabled"`
	IncludePreRelease    bool                 `json:"include_pre_release"`
	HashedPassword       string               `json:"hashed_password"`
	LocalAuthToken       string               `json:"local_auth_token"`
	LocalAuthMode        string               `json:"localAuthMode"` // Uses camelCase for backwards compatibility with existing configs
	LocalLoopbackOnly    bool                 `json:"local_loopback_only"`
	WakeOnLanDevices     []WakeOnLanDevice    `json:"wake_on_lan_devices"`
	KeyboardMacros       []KeyboardMacro      `json:"keyboard_macros"`
	KeyboardLayout       string               `json:"keyboard_layout"`
	EdidString           string               `json:"hdmi_edid_string"`
	ActiveExtension      string               `json:"active_extension"`
	DisplayRotation      string               `json:"display_rotation"`
	DisplayMaxBrightness int                  `json:"display_max_brightness"`
	DisplayDimAfterSec   int                  `json:"display_dim_after_sec"`
	DisplayOffAfterSec   int                  `json:"display_off_after_sec"`
	TLSMode              string               `json:"tls_mode"` // options: "self-signed", "user-defined", ""
	UsbConfig            *usbgadget.Config    `json:"usb_config"`
	UsbDevices           *usbgadget.Devices   `json:"usb_devices"`
	NetworkConfig        *types.NetworkConfig `json:"network_config"`
	DefaultLogLevel      string               `json:"default_log_level"`
	VideoSleepAfterSec   int                  `json:"video_sleep_after_sec"`
	VideoQualityFactor   float64              `json:"video_quality_factor"`
	AudioInputAutoEnable bool                 `json:"audio_input_auto_enable"`
	AudioOutputEnabled   bool                 `json:"audio_output_enabled"`
	AudioBitrate         int                  `json:"audio_bitrate"`    // kbps (64-256)
	AudioComplexity      int                  `json:"audio_complexity"` // 0-10
	AudioDTXEnabled      bool                 `json:"audio_dtx_enabled"`
	AudioFECEnabled      bool                 `json:"audio_fec_enabled"`
	AudioBufferPeriods   int                  `json:"audio_buffer_periods"`   // 2-24
	AudioPacketLossPerc  int                  `json:"audio_packet_loss_perc"` // 0-100
	NativeMaxRestart     uint                 `json:"native_max_restart_attempts"`

	// Camera/UVC settings
	CameraResolution   string `json:"camera_resolution"`    // "1080p", "720p", "480p"
	CameraFrameRate    int    `json:"camera_frame_rate"`    // 15, 24, 30
	CameraH264Bitrate  int    `json:"camera_h264_bitrate"`  // 1-10 Mbps
	CameraMjpegQuality int    `json:"camera_mjpeg_quality"` // 0-100%
}

// GetUpdateAPIURL returns the update API URL
func (c *Config) GetUpdateAPIURL() string {
	if c.UpdateAPIURL == "" {
		return DefaultAPIURL
	}
	return strings.TrimSuffix(c.UpdateAPIURL, "/") + "/releases"
}

// GetDisplayRotation returns the display rotation
func (c *Config) GetDisplayRotation() uint16 {
	rotationInt, err := strconv.ParseUint(c.DisplayRotation, 10, 16)
	if err != nil {
		logger.Warn().Err(err).Msg("invalid display rotation, using default")
		return 270
	}
	return uint16(rotationInt)
}

// SetDisplayRotation sets the display rotation
func (c *Config) SetDisplayRotation(rotation string) error {
	_, err := strconv.ParseUint(rotation, 10, 16)
	if err != nil {
		logger.Warn().Err(err).Msg("invalid display rotation, using default")
		return err
	}
	c.DisplayRotation = rotation
	return nil
}

const configPath = "/userdata/kvm_config.json"

// Default configuration structs used to create independent copies in getDefaultConfig().
// These are package-level variables to avoid repeated allocations.
var (
	defaultJigglerConfig = JigglerConfig{
		InactivityLimitSeconds: 60,
		JitterPercentage:       25,
		ScheduleCronTab:        "0 * * * * *",
		Timezone:               "UTC",
	}
	defaultUsbConfig = usbgadget.Config{
		VendorId:     "0x1d6b", //The Linux Foundation
		ProductId:    "0x0104", //Multifunction Composite Gadget
		SerialNumber: "",
		Manufacturer: "JetKVM",
		Product:      "USB Emulation Device",
	}
	defaultUsbDevices = usbgadget.Devices{
		AbsoluteMouse: true,
		RelativeMouse: true,
		Keyboard:      true,
		MassStorage:   true,
		Audio:         true,
	}
)

func getDefaultConfig() Config {
	return Config{
		CloudURL:             DefaultAPIURL,
		UpdateAPIURL:         DefaultAPIURL,
		CloudAppURL:          "https://app.jetkvm.com",
		AutoUpdateEnabled:    true,
		ActiveExtension:      "",
		KeyboardMacros:       []KeyboardMacro{},
		DisplayRotation:      "270",
		KeyboardLayout:       "en-US",
		EdidString:           native.DefaultEDID,
		DisplayMaxBrightness: 64,
		DisplayDimAfterSec:   120,  // 2 minutes
		DisplayOffAfterSec:   1800, // 30 minutes
		JigglerEnabled:       false,
		// This is the "Standard" jiggler option in the UI
		JigglerConfig: func() *JigglerConfig { c := defaultJigglerConfig; return &c }(),
		TLSMode:       "",
		UsbConfig:     func() *usbgadget.Config { c := defaultUsbConfig; return &c }(),
		UsbDevices:    func() *usbgadget.Devices { c := defaultUsbDevices; return &c }(),
		NetworkConfig: func() *types.NetworkConfig {
			c := &types.NetworkConfig{}
			_ = confparser.SetDefaultsAndValidate(c)
			return c
		}(),
		DefaultLogLevel:      "INFO",
		VideoQualityFactor:   1.0,
		AudioInputAutoEnable: false,
		AudioOutputEnabled:   true,
		AudioBitrate:         192,
		AudioComplexity:      8,
		AudioDTXEnabled:      true,
		AudioFECEnabled:      true,
		AudioBufferPeriods:   12,
		AudioPacketLossPerc:  20,

		// Camera/UVC defaults
		CameraResolution:   "1080p",
		CameraFrameRate:    24, // Cinema rate - good balance of quality and CPU
		CameraH264Bitrate:  3,  // 3 Mbps for 1080p24
		CameraMjpegQuality: 35, // 35% - reasonable quality/size balance
	}
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
	defaultConfig := getDefaultConfig()
	config = &defaultConfig

	file, err := os.Open(configPath)
	if err != nil {
		logger.Debug().Msg("default config file doesn't exist, using default")
		configSuccess.Set(1.0)
		configSuccessTime.SetToCurrentTime()
		return
	}
	defer file.Close()

	// load and merge the default config with the user config
	loadedConfig := defaultConfig
	if err := json.NewDecoder(file).Decode(&loadedConfig); err != nil {
		logger.Warn().Err(err).Msg("config file JSON parsing failed")
		configSuccess.Set(0.0)
		return
	}

	// merge the user config with the default config
	if loadedConfig.UsbConfig == nil {
		loadedConfig.UsbConfig = getDefaultConfig().UsbConfig
	}

	if loadedConfig.UsbDevices == nil {
		loadedConfig.UsbDevices = getDefaultConfig().UsbDevices
	}

	if loadedConfig.NetworkConfig == nil {
		loadedConfig.NetworkConfig = getDefaultConfig().NetworkConfig
	}

	if loadedConfig.JigglerConfig == nil {
		loadedConfig.JigglerConfig = getDefaultConfig().JigglerConfig
	}

	// Apply audio defaults for new configs
	if loadedConfig.AudioBitrate == 0 {
		defaults := getDefaultConfig()
		loadedConfig.AudioBitrate = defaults.AudioBitrate
		loadedConfig.AudioComplexity = defaults.AudioComplexity
		loadedConfig.AudioDTXEnabled = defaults.AudioDTXEnabled
		loadedConfig.AudioFECEnabled = defaults.AudioFECEnabled
		loadedConfig.AudioBufferPeriods = defaults.AudioBufferPeriods
		loadedConfig.AudioPacketLossPerc = defaults.AudioPacketLossPerc
	}

	// Apply camera defaults for new configs
	if loadedConfig.CameraResolution == "" {
		defaults := getDefaultConfig()
		loadedConfig.CameraResolution = defaults.CameraResolution
		loadedConfig.CameraFrameRate = defaults.CameraFrameRate
		loadedConfig.CameraH264Bitrate = defaults.CameraH264Bitrate
		loadedConfig.CameraMjpegQuality = defaults.CameraMjpegQuality
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
	return saveConfig(configPath)
}

func SaveBackupConfig() error {
	return saveConfig(configPath + ".bak")
}

func saveConfig(path string) error {
	configLock.Lock()
	defer configLock.Unlock()

	logger.Trace().Str("path", path).Msg("Saving config")

	// fixup old keyboard layout value
	if config.KeyboardLayout == "en_US" {
		config.KeyboardLayout = "en-US"
	}

	file, err := os.Create(path)
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

	logger.Info().Str("path", path).Msg("config saved")
	return nil
}

func ensureConfigLoaded() {
	if config == nil {
		LoadConfig()
	}
}
