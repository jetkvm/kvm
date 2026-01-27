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
	"github.com/jetkvm/kvm/internal/meshvpn"
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
	MeshVPNConfig        *meshvpn.Config      `json:"meshvpn_config,omitempty"`
	DefaultLogLevel      string               `json:"default_log_level"`
	// LogLevelOverrides contains log level configuration.
	// Format: "LEVEL" for global, or "subsystem:LEVEL,subsystem:LEVEL" for per-subsystem.
	// Examples: "DEBUG", "rdp:TRACE,vnc:DEBUG", "INFO,rdp:TRACE"
	LogLevelOverrides    string  `json:"log_level_overrides"`
	VideoSleepAfterSec   int     `json:"video_sleep_after_sec"`
	VideoQualityFactor   float64 `json:"video_quality_factor"`
	AudioInputAutoEnable bool    `json:"audio_input_auto_enable"`
	AudioOutputEnabled   bool    `json:"audio_output_enabled"`
	AudioBitrate         int     `json:"audio_bitrate"`    // kbps (64-256)
	AudioComplexity      int     `json:"audio_complexity"` // 0-10
	AudioDTXEnabled      bool    `json:"audio_dtx_enabled"`
	AudioFECEnabled      bool    `json:"audio_fec_enabled"`
	AudioBufferPeriods   int     `json:"audio_buffer_periods"`   // 2-48
	AudioPacketLossPerc  int     `json:"audio_packet_loss_perc"` // 0-100
	NativeMaxRestart     uint    `json:"native_max_restart_attempts"`

	// Camera/UVC settings
	CameraResolution   string `json:"camera_resolution"`    // "1080p", "720p", "480p"
	CameraFrameRate    int    `json:"camera_frame_rate"`    // 15, 24, 30
	CameraH264Bitrate  int    `json:"camera_h264_bitrate"`  // 1-10 Mbps
	CameraMjpegQuality int    `json:"camera_mjpeg_quality"` // 0-100%

	// VNC settings
	VNCEnabled          bool   `json:"vnc_enabled"`
	VNCPort             int    `json:"vnc_port"`              // default: 5900
	VNCQuality          int    `json:"vnc_quality"`           // JPEG quality 1-99, default: 80
	VNCUseTLS           bool   `json:"vnc_use_tls"`           // Use TLS for VNC (VeNCrypt) - when enabled and available, rejects insecure connections
	VNCPassword         string `json:"vnc_password"`          // Separate VNC password (if not using same password)
	VNCUseSamePassword  bool   `json:"vnc_use_same_password"` // Use same password as local auth
	LocalAuthPassword   string `json:"local_auth_password"`   // Plaintext password for VNC auth
	VNCPasteDelayMs     int    `json:"vnc_paste_delay_ms"`    // Delay per keystroke in ms (0-50), default: 2
	VNCMaxConnections   int    `json:"vnc_max_connections"`   // Max concurrent VNC connections (1-10), default: 3
	VNCClipboardEnabled bool   `json:"vnc_clipboard_enabled"` // Enable clipboard-as-keystrokes, default: true

	// RDP settings
	RDPEnabled          bool   `json:"rdp_enabled"`
	RDPPort             int    `json:"rdp_port"`              // default: 3389
	RDPMaxConnections   int    `json:"rdp_max_connections"`   // Max concurrent RDP connections (1-10), default: 3
	RDPUseTLS           bool   `json:"rdp_use_tls"`           // Use TLS for RDP - when enabled and available, provides NLA security
	RDPVideoEnabled     bool   `json:"rdp_video_enabled"`     // Enable H.264 video via RDPGFX, default: true
	RDPAudioEnabled     bool   `json:"rdp_audio_enabled"`     // Enable audio output to client, default: true
	RDPMicEnabled       bool   `json:"rdp_mic_enabled"`       // Enable microphone input from client, default: true
	RDPCameraEnabled           bool `json:"rdp_camera_enabled"`            // Enable webcam redirection from client, default: false
	RDPCameraTranscodeEnabled  bool `json:"rdp_camera_transcode_enabled"`  // Enable H.264→MJPEG software transcode for camera (BETA, high CPU), default: false
	RDPClipboardEnabled     bool   `json:"rdp_clipboard_enabled"`      // Enable clipboard-as-keystrokes, default: true
	RDPPasteDelayMs         int    `json:"rdp_paste_delay_ms"`         // Delay per keystroke in ms (0-50), default: 0
	RDPTargetOS             string `json:"rdp_target_os"`              // Target OS for clipboard: "windows", "macos", "linux", default: "windows"
	RDPClipboardMode        string `json:"rdp_clipboard_mode"`         // Clipboard mode: "text", "base64-markers", "base64-script", default: "text"
	RDPFileTransferEnabled  bool   `json:"rdp_file_transfer_enabled"`  // Enable file clipboard transfer, default: false
	RDPFileTransferMethod   string `json:"rdp_file_transfer_method"`   // Transfer method: "auto", "network", "usb", "base64", default: "auto"
	RDPFileTransferMaxMB      int    `json:"rdp_file_transfer_max_mb"`      // Max file size in MB, default: 100
	RDPFileTransferTTLSec     int    `json:"rdp_file_transfer_ttl_sec"`     // File TTL in seconds before expiry, default: 300 (5 min)
	RDPFileTransferCleanupSec int    `json:"rdp_file_transfer_cleanup_sec"` // Cleanup interval in seconds, default: 60
	RDPNetworkCmdWindows      string `json:"rdp_network_cmd_windows"`       // Download command for Windows. Placeholders: {url}, {filename}
	RDPNetworkCmdLinux      string `json:"rdp_network_cmd_linux"`      // Download command for Linux. Placeholders: {url}, {filename}
	RDPNetworkCmdMacOS      string `json:"rdp_network_cmd_macos"`      // Download command for macOS. Placeholders: {url}, {filename}
	RDPBase64CmdWindows     string `json:"rdp_base64_cmd_windows"`     // Decode command for Windows. Placeholders: {data}, {filename}
	RDPBase64CmdLinux       string `json:"rdp_base64_cmd_linux"`       // Decode command for Linux. Placeholders: {data}, {filename}
	RDPBase64CmdMacOS       string `json:"rdp_base64_cmd_macos"`       // Decode command for macOS. Placeholders: {data}, {filename}
	RDPUsername             string `json:"rdp_username"`               // Username for RDP authentication (any username allowed if empty)
	RDPDomain               string `json:"rdp_domain"`                 // Domain for RDP authentication (any domain allowed if empty)

	// Native mode: "subprocess" (default, crash-isolated) or "direct" (more efficient, no subprocess)
	// Direct mode is more resource-efficient but native crashes will bring down the whole app.
	NativeMode string `json:"native_mode"`
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
		DefaultLogLevel:      "WARN",
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

		// VNC defaults
		VNCEnabled:          false,
		VNCPort:             5900,
		VNCQuality:          80,
		VNCPasteDelayMs:     0, // No delay - fastest typing speed
		VNCMaxConnections:   3,
		VNCClipboardEnabled: true,

		// RDP defaults
		RDPEnabled:          false,
		RDPPort:             3389,
		RDPMaxConnections:   3,
		RDPUseTLS:           true, // Enable TLS by default for security
		RDPVideoEnabled:     true,
		RDPAudioEnabled:     true,
		RDPMicEnabled:       true,
		RDPCameraEnabled:    false, // Camera redirection off by default
		RDPClipboardEnabled:    true,
		RDPPasteDelayMs:        0,         // No delay - fastest typing speed
		RDPTargetOS:            "windows", // Most common target
		RDPClipboardMode:       "text",    // Plain text only (skip non-typeable chars)
		RDPFileTransferEnabled: true, // File transfer enabled by default
		RDPFileTransferMethod:  "auto",
		RDPFileTransferMaxMB:   100,
		// Command templates - empty means use built-in defaults
		RDPNetworkCmdWindows: "",
		RDPNetworkCmdLinux:   "",
		RDPNetworkCmdMacOS:   "",
		RDPBase64CmdWindows:  "",
		RDPBase64CmdLinux:    "",
		RDPBase64CmdMacOS:    "",

		// Native mode: "subprocess" is crash-isolated (default), "direct" is more efficient
		NativeMode: "subprocess",
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

	// Apply VNC defaults for new configs
	if loadedConfig.VNCPort == 0 {
		defaults := getDefaultConfig()
		loadedConfig.VNCPort = defaults.VNCPort
		loadedConfig.VNCQuality = defaults.VNCQuality
	}
	// Apply VNC defaults for configs created before these settings existed
	// VNCPasteDelayMs: 0 is a valid value (fastest), so we use -1 or check differently
	// Since 0 is valid (no delay), we don't need migration - new default is 2ms
	if loadedConfig.VNCMaxConnections == 0 {
		loadedConfig.VNCMaxConnections = 3
	}
	// VNCClipboardEnabled defaults to true; only set if config is being created fresh
	// (existing configs with explicit false should be preserved)

	// Apply RDP defaults for new configs
	if loadedConfig.RDPPort == 0 {
		defaults := getDefaultConfig()
		loadedConfig.RDPPort = defaults.RDPPort
		loadedConfig.RDPMaxConnections = defaults.RDPMaxConnections
		loadedConfig.RDPUseTLS = defaults.RDPUseTLS
		loadedConfig.RDPVideoEnabled = defaults.RDPVideoEnabled
		loadedConfig.RDPAudioEnabled = defaults.RDPAudioEnabled
		loadedConfig.RDPMicEnabled = defaults.RDPMicEnabled
		// RDPCameraEnabled defaults to false, so no migration needed
	}
	if loadedConfig.RDPMaxConnections == 0 {
		loadedConfig.RDPMaxConnections = 3
	}

	// fixup old keyboard layout value
	if loadedConfig.KeyboardLayout == "en_US" {
		loadedConfig.KeyboardLayout = "en-US"
	}

	// Migrate old verbose log level to sensible default
	if loadedConfig.DefaultLogLevel == "INFO" {
		loadedConfig.DefaultLogLevel = "WARN"
	}

	config = &loadedConfig

	logging.GetRootLogger().UpdateLogLevel(config.DefaultLogLevel)
	logging.GetRootLogger().SetSubsystemLevels(config.LogLevelOverrides)

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
