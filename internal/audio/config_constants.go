package audio

import "time"

// AudioConfigConstants centralizes all hardcoded values used across audio components
type AudioConfigConstants struct {
	// Audio Quality Presets
	MaxAudioFrameSize int

	// Opus Encoding Parameters
	OpusBitrate       int
	OpusComplexity    int
	OpusVBR           int
	OpusVBRConstraint int
	OpusDTX           int

	// Audio Parameters
	SampleRate    int
	Channels      int
	FrameSize     int
	MaxPacketSize int

	// Process Management
	MaxRestartAttempts int
	RestartWindow      time.Duration
	RestartDelay       time.Duration
	MaxRestartDelay    time.Duration

	// Buffer Management
	PreallocSize        int
	MaxPoolSize         int
	MessagePoolSize     int
	OptimalSocketBuffer int
	MaxSocketBuffer     int
	MinSocketBuffer     int

	// IPC Configuration
	MagicNumber      uint32
	MaxFrameSize     int
	WriteTimeout     time.Duration
	MaxDroppedFrames int
	HeaderSize       int

	// Monitoring and Metrics
	MetricsUpdateInterval time.Duration
	EMAAlpha              float64
	WarmupSamples         int
	LogThrottleInterval   time.Duration
	MetricsChannelBuffer  int

	// Performance Tuning
	CPUFactor           float64
	MemoryFactor        float64
	LatencyFactor       float64
	InputSizeThreshold  int
	OutputSizeThreshold int
	TargetLevel         float64

	// Priority Scheduling
	AudioHighPriority   int
	AudioMediumPriority int
	AudioLowPriority    int
	NormalPriority      int
	NiceValue           int

	// Error Handling
	MaxConsecutiveErrors int
	MaxRetryAttempts     int
}

// DefaultAudioConfig returns the default configuration constants
func DefaultAudioConfig() *AudioConfigConstants {
	return &AudioConfigConstants{
		// Audio Quality Presets
		MaxAudioFrameSize: 4096,

		// Opus Encoding Parameters
		OpusBitrate:       128000,
		OpusComplexity:    10,
		OpusVBR:           1,
		OpusVBRConstraint: 0,
		OpusDTX:           0,

		// Audio Parameters
		SampleRate:    48000,
		Channels:      2,
		FrameSize:     960,
		MaxPacketSize: 4000,

		// Process Management
		MaxRestartAttempts: 5,
		RestartWindow:      5 * time.Minute,
		RestartDelay:       2 * time.Second,
		MaxRestartDelay:    30 * time.Second,

		// Buffer Management
		PreallocSize:        1024 * 1024, // 1MB
		MaxPoolSize:         100,
		MessagePoolSize:     100,
		OptimalSocketBuffer: 262144,  // 256KB
		MaxSocketBuffer:     1048576, // 1MB
		MinSocketBuffer:     8192,    // 8KB

		// IPC Configuration
		MagicNumber:      0xDEADBEEF,
		MaxFrameSize:     4096,
		WriteTimeout:     5 * time.Second,
		MaxDroppedFrames: 10,
		HeaderSize:       8,

		// Monitoring and Metrics
		MetricsUpdateInterval: 1000 * time.Millisecond,
		EMAAlpha:              0.1,
		WarmupSamples:         10,
		LogThrottleInterval:   5 * time.Second,
		MetricsChannelBuffer:  100,

		// Performance Tuning
		CPUFactor:           0.7,
		MemoryFactor:        0.8,
		LatencyFactor:       0.9,
		InputSizeThreshold:  1024,
		OutputSizeThreshold: 2048,
		TargetLevel:         0.5,

		// Priority Scheduling
		AudioHighPriority:   -10,
		AudioMediumPriority: -5,
		AudioLowPriority:    0,
		NormalPriority:      0,
		NiceValue:           -10,

		// Error Handling
		MaxConsecutiveErrors: 5,
		MaxRetryAttempts:     3,
	}
}

// Global configuration instance
var audioConfigInstance = DefaultAudioConfig()

// UpdateConfig allows runtime configuration updates
func UpdateConfig(newConfig *AudioConfigConstants) {
	audioConfigInstance = newConfig
}

// GetConfig returns the current configuration
func GetConfig() *AudioConfigConstants {
	return audioConfigInstance
}
