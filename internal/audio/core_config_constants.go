package audio

import (
	"time"

	"github.com/jetkvm/kvm/internal/logging"
)

// AudioConfigConstants centralizes all hardcoded values used across audio components.
// This configuration system allows runtime tuning of audio performance, quality, and resource usage.
type AudioConfigConstants struct {
	// Audio Quality Presets
	MaxAudioFrameSize int // Maximum audio frame size in bytes (default: 4096)
	MaxPCMBufferSize  int // Maximum PCM buffer size in bytes for separate buffer optimization

	// Opus Encoding Parameters
	OpusBitrate       int // Target bitrate for Opus encoding in bps (default: 128000)
	OpusComplexity    int // Computational complexity 0-10 (default: 10 for best quality)
	OpusVBR           int // Variable Bit Rate: 0=CBR, 1=VBR (default: 1)
	OpusVBRConstraint int // VBR constraint: 0=unconstrained, 1=constrained (default: 0)
	OpusDTX           int // Discontinuous Transmission: 0=disabled, 1=enabled (default: 0)

	// Audio Parameters
	SampleRate    int // Audio sampling frequency in Hz (default: 48000)
	Channels      int // Number of audio channels: 1=mono, 2=stereo (default: 2)
	FrameSize     int // Samples per audio frame (default: 960 for 20ms at 48kHz)
	MaxPacketSize int // Maximum encoded packet size in bytes (default: 4000)

	// Audio Quality Bitrates (kbps)
	AudioQualityLowOutputBitrate    int // Low-quality output bitrate (default: 32)
	AudioQualityLowInputBitrate     int // Low-quality input bitrate (default: 16)
	AudioQualityMediumOutputBitrate int // Medium-quality output bitrate (default: 64)
	AudioQualityMediumInputBitrate  int // Medium-quality input bitrate (default: 32)
	AudioQualityHighOutputBitrate   int // High-quality output bitrate (default: 128)
	AudioQualityHighInputBitrate    int // High-quality input bitrate (default: 64)
	AudioQualityUltraOutputBitrate  int // Ultra-quality output bitrate (default: 192)
	AudioQualityUltraInputBitrate   int // Ultra-quality input bitrate (default: 96)

	// Audio Quality Sample Rates (Hz)
	AudioQualityLowSampleRate    int // Low-quality sample rate (default: 22050)
	AudioQualityMediumSampleRate int // Medium-quality sample rate (default: 44100)
	AudioQualityMicLowSampleRate int // Low-quality microphone sample rate (default: 16000)

	// Audio Quality Frame Sizes
	AudioQualityLowFrameSize    time.Duration // Low-quality frame duration (default: 40ms)
	AudioQualityMediumFrameSize time.Duration // Medium-quality frame duration (default: 20ms)
	AudioQualityHighFrameSize   time.Duration // High-quality frame duration (default: 20ms)

	AudioQualityUltraFrameSize time.Duration // Ultra-quality frame duration (default: 10ms)

	// Audio Quality Channels
	AudioQualityLowChannels    int // Low-quality channel count (default: 1)
	AudioQualityMediumChannels int // Medium-quality channel count (default: 2)
	AudioQualityHighChannels   int // High-quality channel count (default: 2)
	AudioQualityUltraChannels  int // Ultra-quality channel count (default: 2)

	// Audio Quality OPUS Encoder Parameters
	AudioQualityLowOpusComplexity int // Low-quality OPUS complexity (default: 1)
	AudioQualityLowOpusVBR        int // Low-quality OPUS VBR setting (default: 0)
	AudioQualityLowOpusSignalType int // Low-quality OPUS signal type (default: 3001)
	AudioQualityLowOpusBandwidth  int // Low-quality OPUS bandwidth (default: 1101)
	AudioQualityLowOpusDTX        int // Low-quality OPUS DTX setting (default: 1)

	AudioQualityMediumOpusComplexity int // Medium-quality OPUS complexity (default: 5)
	AudioQualityMediumOpusVBR        int // Medium-quality OPUS VBR setting (default: 1)
	AudioQualityMediumOpusSignalType int // Medium-quality OPUS signal type (default: 3002)
	AudioQualityMediumOpusBandwidth  int // Medium-quality OPUS bandwidth (default: 1103)
	AudioQualityMediumOpusDTX        int // Medium-quality OPUS DTX setting (default: 0)

	AudioQualityHighOpusComplexity int // High-quality OPUS complexity (default: 8)
	AudioQualityHighOpusVBR        int // High-quality OPUS VBR setting (default: 1)
	AudioQualityHighOpusSignalType int // High-quality OPUS signal type (default: 3002)
	AudioQualityHighOpusBandwidth  int // High-quality OPUS bandwidth (default: 1104)
	AudioQualityHighOpusDTX        int // High-quality OPUS DTX setting (default: 0)

	AudioQualityUltraOpusComplexity int // Ultra-quality OPUS complexity (default: 10)
	AudioQualityUltraOpusVBR        int // Ultra-quality OPUS VBR setting (default: 1)
	AudioQualityUltraOpusSignalType int // Ultra-quality OPUS signal type (default: 3002)
	AudioQualityUltraOpusBandwidth  int // Ultra-quality OPUS bandwidth (default: 1105)
	AudioQualityUltraOpusDTX        int // Ultra-quality OPUS DTX setting (default: 0)

	// CGO Audio Constants
	CGOOpusBitrate int // Native Opus encoder bitrate in bps (default: 96000)

	CGOOpusComplexity    int // Computational complexity for native Opus encoder (0-10)
	CGOOpusVBR           int // Variable Bit Rate in native Opus encoder (0=CBR, 1=VBR)
	CGOOpusVBRConstraint int // Constrained VBR in native encoder (0/1)
	CGOOpusSignalType    int // Signal type hint for native Opus encoder
	CGOOpusBandwidth     int // Frequency bandwidth for native Opus encoder
	CGOOpusDTX           int // Discontinuous Transmission in native encoder (0/1)
	CGOSampleRate        int // Sample rate for native audio processing (Hz)
	CGOChannels          int // Channel count for native audio processing
	CGOFrameSize         int // Frame size for native Opus processing (samples)
	CGOMaxPacketSize     int // Maximum packet size for native encoding (bytes)

	// Input IPC Constants
	InputIPCSampleRate int // Sample rate for input IPC audio processing (Hz)
	InputIPCChannels   int // Channel count for input IPC audio processing
	InputIPCFrameSize  int // Frame size for input IPC processing (samples)

	// Output IPC Constants
	OutputMaxFrameSize int // Maximum frame size for output processing (bytes)
	OutputHeaderSize   int // Size of output message headers (bytes)

	OutputMessagePoolSize int // Output message pool size (128)

	// Socket Buffer Constants
	SocketOptimalBuffer int // Optimal socket buffer size (128KB)
	SocketMaxBuffer     int // Maximum socket buffer size (256KB)
	SocketMinBuffer     int // Minimum socket buffer size (32KB)

	// Process Management
	MaxRestartAttempts int           // Maximum restart attempts (5)
	RestartWindow      time.Duration // Restart attempt window (5m)
	RestartDelay       time.Duration // Initial restart delay (2s)
	MaxRestartDelay    time.Duration // Maximum restart delay (30s)

	// Buffer Management

	PreallocSize                 int
	MaxPoolSize                  int
	MessagePoolSize              int
	OptimalSocketBuffer          int
	MaxSocketBuffer              int
	MinSocketBuffer              int
	ChannelBufferSize            int
	AudioFramePoolSize           int
	PageSize                     int
	InitialBufferFrames          int
	BytesToMBDivisor             int
	MinReadEncodeBuffer          int
	MaxDecodeWriteBuffer         int
	MinBatchSizeForThreadPinning int
	GoroutineMonitorInterval     time.Duration
	MagicNumber                  uint32
	MaxFrameSize                 int
	WriteTimeout                 time.Duration
	HeaderSize                   int
	MetricsUpdateInterval        time.Duration
	WarmupSamples                int
	MetricsChannelBuffer         int
	LatencyHistorySize           int
	MaxCPUPercent                float64
	MinCPUPercent                float64
	DefaultClockTicks            float64
	DefaultMemoryGB              int
	MaxWarmupSamples             int
	WarmupCPUSamples             int
	LogThrottleIntervalSec       int
	MinValidClockTicks           int
	MaxValidClockTicks           int
	CPUFactor                    float64
	MemoryFactor                 float64
	LatencyFactor                float64

	// Adaptive Buffer Configuration
	AdaptiveMinBufferSize     int // Minimum buffer size in frames for adaptive buffering
	AdaptiveMaxBufferSize     int // Maximum buffer size in frames for adaptive buffering
	AdaptiveDefaultBufferSize int // Default buffer size in frames for adaptive buffering

	// Timing Configuration
	RetryDelay              time.Duration // Retry delay
	MaxRetryDelay           time.Duration // Maximum retry delay
	BackoffMultiplier       float64       // Backoff multiplier
	MaxConsecutiveErrors    int           // Maximum consecutive errors
	DefaultSleepDuration    time.Duration // 100ms
	ShortSleepDuration      time.Duration // 10ms
	LongSleepDuration       time.Duration // 200ms
	DefaultTickerInterval   time.Duration // 100ms
	BufferUpdateInterval    time.Duration // 500ms
	InputSupervisorTimeout  time.Duration // 5s
	OutputSupervisorTimeout time.Duration // 5s
	BatchProcessingDelay    time.Duration // 10ms

	AdaptiveOptimizerStability time.Duration // 10s
	LatencyMonitorTarget       time.Duration // 50ms

	// Adaptive Buffer Configuration
	// LowCPUThreshold defines CPU usage threshold for buffer size reduction.
	LowCPUThreshold float64 // 20% CPU threshold for buffer optimization

	// HighCPUThreshold defines CPU usage threshold for buffer size increase.
	HighCPUThreshold               float64       // 60% CPU threshold
	LowMemoryThreshold             float64       // 50% memory threshold
	HighMemoryThreshold            float64       // 75% memory threshold
	AdaptiveBufferTargetLatency    time.Duration // 20ms target latency
	CooldownPeriod                 time.Duration // 30s cooldown period
	RollbackThreshold              time.Duration // 300ms rollback threshold
	AdaptiveOptimizerLatencyTarget time.Duration // 50ms latency target
	MaxLatencyThreshold            time.Duration // 200ms max latency
	JitterThreshold                time.Duration // 20ms jitter threshold
	LatencyOptimizationInterval    time.Duration // 5s optimization interval
	LatencyAdaptiveThreshold       float64       // 0.8 adaptive threshold
	MicContentionTimeout           time.Duration // 200ms contention timeout
	PreallocPercentage             int           // 20% preallocation percentage
	BackoffStart                   time.Duration // 50ms initial backoff

	InputMagicNumber uint32 // Magic number for input IPC messages (0x4A4B4D49 "JKMI")

	OutputMagicNumber uint32 // Magic number for output IPC messages (0x4A4B4F55 "JKOU")

	// Calculation Constants
	PercentageMultiplier    float64 // Multiplier for percentage calculations (100.0)
	AveragingWeight         float64 // Weight for weighted averaging (0.7)
	ScalingFactor           float64 // General scaling factor (1.5)
	SmoothingFactor         float64 // Smoothing factor for adaptive buffers (0.3)
	CPUMemoryWeight         float64 // Weight for CPU factor in calculations (0.5)
	MemoryWeight            float64 // Weight for memory factor (0.3)
	LatencyWeight           float64 // Weight for latency factor (0.2)
	PoolGrowthMultiplier    int     // Multiplier for pool size growth (2)
	LatencyScalingFactor    float64 // Scaling factor for latency calculations (2.0)
	OptimizerAggressiveness float64 // Aggressiveness level for optimization (0.7)

	// CGO Audio Processing Constants
	CGOUsleepMicroseconds int // Sleep duration for CGO usleep calls (1000μs)

	CGOPCMBufferSize            int     // PCM buffer size for CGO audio processing
	CGONanosecondsPerSecond     float64 // Nanoseconds per second conversion
	FrontendOperationDebounceMS int     // Frontend operation debounce delay
	FrontendSyncDebounceMS      int     // Frontend sync debounce delay
	FrontendSampleRate          int     // Frontend sample rate
	FrontendRetryDelayMS        int     // Frontend retry delay
	FrontendShortDelayMS        int     // Frontend short delay
	FrontendLongDelayMS         int     // Frontend long delay
	FrontendSyncDelayMS         int     // Frontend sync delay
	FrontendMaxRetryAttempts    int     // Frontend max retry attempts
	FrontendAudioLevelUpdateMS  int     // Frontend audio level update interval
	FrontendFFTSize             int     // Frontend FFT size
	FrontendAudioLevelMax       int     // Frontend max audio level
	FrontendReconnectIntervalMS int     // Frontend reconnect interval
	FrontendSubscriptionDelayMS int     // Frontend subscription delay
	FrontendDebugIntervalMS     int     // Frontend debug interval

	// Process Monitoring Constants
	ProcessMonitorDefaultMemoryGB int     // Default memory size for fallback (4GB)
	ProcessMonitorKBToBytes       int     // KB to bytes conversion factor (1024)
	ProcessMonitorDefaultClockHz  float64 // Default system clock frequency (250.0 Hz)
	ProcessMonitorFallbackClockHz float64 // Fallback clock frequency (1000.0 Hz)
	ProcessMonitorTraditionalHz   float64 // Traditional system clock frequency (100.0 Hz)

	// Batch Processing Constants
	BatchProcessorFramesPerBatch         int           // Frames processed per batch (4)
	BatchProcessorTimeout                time.Duration // Batch processing timeout (5ms)
	BatchProcessorMaxQueueSize           int           // Maximum batch queue size (16)
	BatchProcessorAdaptiveThreshold      float64       // Adaptive batch sizing threshold (0.8)
	BatchProcessorThreadPinningThreshold int           // Thread pinning threshold (8 frames)

	// Output Streaming Constants
	OutputStreamingFrameIntervalMS int // Output frame interval (20ms for 50 FPS)

	// IPC Constants
	IPCInitialBufferFrames int // Initial IPC buffer size (500 frames)

	EventTimeoutSeconds            int
	EventTimeFormatString          string
	EventSubscriptionDelayMS       int
	InputProcessingTimeoutMS       int
	AdaptiveBufferCPUMultiplier    int
	AdaptiveBufferMemoryMultiplier int
	InputSocketName                string
	OutputSocketName               string
	AudioInputComponentName        string
	AudioOutputComponentName       string
	AudioServerComponentName       string
	AudioRelayComponentName        string
	AudioEventsComponentName       string

	TestSocketTimeout          time.Duration
	TestBufferSize             int
	TestRetryDelay             time.Duration
	LatencyHistogramMaxSamples int
	LatencyPercentile50        int
	LatencyPercentile95        int
	LatencyPercentile99        int
	BufferPoolMaxOperations    int
	HitRateCalculationBase     float64
	MaxLatency                 time.Duration
	MinMetricsUpdateInterval   time.Duration
	MaxMetricsUpdateInterval   time.Duration
	MinSampleRate              int
	MaxSampleRate              int
	MaxChannels                int

	// CGO Constants
	CGOMaxBackoffMicroseconds int // Maximum CGO backoff time (500ms)
	CGOMaxAttempts            int // Maximum CGO retry attempts (5)

	// Frame Duration Validation
	MinFrameDuration time.Duration // Minimum frame duration (10ms)
	MaxFrameDuration time.Duration // Maximum frame duration (100ms)

	// Valid Sample Rates
	// Validation Constants
	ValidSampleRates   []int         // Supported sample rates (8kHz to 48kHz)
	MinOpusBitrate     int           // Minimum Opus bitrate (6000 bps)
	MaxOpusBitrate     int           // Maximum Opus bitrate (510000 bps)
	MaxValidationTime  time.Duration // Validation timeout (5s)
	MinFrameSize       int           // Minimum frame size (64 bytes)
	FrameSizeTolerance int           // Frame size tolerance (512 bytes)

	// Latency Histogram Buckets
	LatencyBucket10ms  time.Duration // 10ms latency bucket
	LatencyBucket25ms  time.Duration // 25ms latency bucket
	LatencyBucket50ms  time.Duration // 50ms latency bucket
	LatencyBucket100ms time.Duration // 100ms latency bucket
	LatencyBucket250ms time.Duration // 250ms latency bucket
	LatencyBucket500ms time.Duration // 500ms latency bucket
	LatencyBucket1s    time.Duration // 1s latency bucket
	LatencyBucket2s    time.Duration // 2s latency bucket

	MaxAudioProcessorWorkers int
	MaxAudioReaderWorkers    int
	AudioProcessorQueueSize  int
	AudioReaderQueueSize     int
	WorkerMaxIdleTime        time.Duration

	// Connection Retry Configuration
	MaxConnectionAttempts   int           // Maximum connection retry attempts
	ConnectionRetryDelay    time.Duration // Initial connection retry delay
	MaxConnectionRetryDelay time.Duration // Maximum connection retry delay
	ConnectionBackoffFactor float64       // Connection retry backoff factor
	ConnectionTimeoutDelay  time.Duration // Connection timeout for each attempt
	ReconnectionInterval    time.Duration // Interval for automatic reconnection attempts
	HealthCheckInterval     time.Duration // Health check interval for connections
}

// DefaultAudioConfig returns the default configuration constants
// These values are carefully chosen based on JetKVM's embedded ARM environment,
// real-time audio requirements, and extensive testing for optimal performance.
func DefaultAudioConfig() *AudioConfigConstants {
	return &AudioConfigConstants{
		// Audio Quality Presets
		MaxAudioFrameSize: 4096,
		MaxPCMBufferSize:  8192, // Default PCM buffer size (2x MaxAudioFrameSize for safety)

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

		AudioQualityLowOutputBitrate:    32,
		AudioQualityLowInputBitrate:     16,
		AudioQualityMediumOutputBitrate: 48,
		AudioQualityMediumInputBitrate:  24,
		AudioQualityHighOutputBitrate:   64,
		AudioQualityHighInputBitrate:    32,
		AudioQualityUltraOutputBitrate:  96,
		AudioQualityUltraInputBitrate:   48,
		AudioQualityLowSampleRate:       48000,
		AudioQualityMediumSampleRate:    48000,
		AudioQualityMicLowSampleRate:    16000,
		AudioQualityLowFrameSize:        20 * time.Millisecond,
		AudioQualityMediumFrameSize:     20 * time.Millisecond,
		AudioQualityHighFrameSize:       20 * time.Millisecond,

		AudioQualityUltraFrameSize: 20 * time.Millisecond, // Ultra-quality frame duration

		// Audio Quality Channels
		AudioQualityLowChannels:    1, // Mono for low quality
		AudioQualityMediumChannels: 2, // Stereo for medium quality
		AudioQualityHighChannels:   2, // Stereo for high quality
		AudioQualityUltraChannels:  2, // Stereo for ultra quality

		// Audio Quality OPUS Parameters
		AudioQualityLowOpusComplexity: 0,    // Low complexity
		AudioQualityLowOpusVBR:        1,    // VBR enabled
		AudioQualityLowOpusSignalType: 3001, // OPUS_SIGNAL_VOICE
		AudioQualityLowOpusBandwidth:  1101, // OPUS_BANDWIDTH_NARROWBAND
		AudioQualityLowOpusDTX:        1,    // DTX enabled

		AudioQualityMediumOpusComplexity: 1,    // Low complexity
		AudioQualityMediumOpusVBR:        1,    // VBR enabled
		AudioQualityMediumOpusSignalType: 3001, // OPUS_SIGNAL_VOICE
		AudioQualityMediumOpusBandwidth:  1102, // OPUS_BANDWIDTH_MEDIUMBAND
		AudioQualityMediumOpusDTX:        1,    // DTX enabled

		AudioQualityHighOpusComplexity: 2,    // Medium complexity
		AudioQualityHighOpusVBR:        1,    // VBR enabled
		AudioQualityHighOpusSignalType: 3002, // OPUS_SIGNAL_MUSIC
		AudioQualityHighOpusBandwidth:  1103, // OPUS_BANDWIDTH_WIDEBAND
		AudioQualityHighOpusDTX:        0,    // DTX disabled

		AudioQualityUltraOpusComplexity: 3,    // Higher complexity
		AudioQualityUltraOpusVBR:        1,    // VBR enabled
		AudioQualityUltraOpusSignalType: 3002, // OPUS_SIGNAL_MUSIC
		AudioQualityUltraOpusBandwidth:  1103, // OPUS_BANDWIDTH_WIDEBAND
		AudioQualityUltraOpusDTX:        0,    // DTX disabled

		// CGO Audio Constants - Optimized for RV1106 native audio processing
		CGOOpusBitrate:       64000, // Reduced for RV1106 efficiency
		CGOOpusComplexity:    2,     // Minimal complexity for RV1106
		CGOOpusVBR:           1,
		CGOOpusVBRConstraint: 1,
		CGOOpusSignalType:    3002, // OPUS_SIGNAL_MUSIC
		CGOOpusBandwidth:     1103, // OPUS_BANDWIDTH_WIDEBAND for RV1106
		CGOOpusDTX:           0,
		CGOSampleRate:        48000,
		CGOChannels:          2,
		CGOFrameSize:         960,
		CGOMaxPacketSize:     1200, // Reduced for RV1106 memory efficiency

		// Input IPC Constants
		InputIPCSampleRate: 48000, // Input IPC sample rate (48kHz)
		InputIPCChannels:   2,     // Input IPC channels (stereo)
		InputIPCFrameSize:  960,   // Input IPC frame size (960 samples)

		// Output IPC Constants
		OutputMaxFrameSize: 4096, // Maximum output frame size
		OutputHeaderSize:   17,   // Output frame header size

		OutputMessagePoolSize: 128, // Output message pool size

		// Socket Buffer Constants
		SocketOptimalBuffer: 131072, // 128KB optimal socket buffer
		SocketMaxBuffer:     262144, // 256KB maximum socket buffer
		SocketMinBuffer:     32768,  // 32KB minimum socket buffer

		// Process Management
		MaxRestartAttempts: 5, // Maximum restart attempts

		RestartWindow:   5 * time.Minute,  // Time window for restart attempt counting
		RestartDelay:    1 * time.Second,  // Initial delay before restart attempts
		MaxRestartDelay: 30 * time.Second, // Maximum delay for exponential backoff

		// Buffer Management
		PreallocSize:         1024 * 1024, // 1MB buffer preallocation
		MaxPoolSize:          100,         // Maximum object pool size
		MessagePoolSize:      1024,        // Significantly increased message pool for quality change bursts
		OptimalSocketBuffer:  262144,      // 256KB optimal socket buffer
		MaxSocketBuffer:      1048576,     // 1MB maximum socket buffer
		MinSocketBuffer:      8192,        // 8KB minimum socket buffer
		ChannelBufferSize:    2048,        // Significantly increased channel buffer for quality change bursts
		AudioFramePoolSize:   1500,        // Audio frame object pool size
		PageSize:             4096,        // Memory page size for alignment
		InitialBufferFrames:  1000,        // Increased initial buffer size during startup
		BytesToMBDivisor:     1024 * 1024, // Byte to megabyte conversion
		MinReadEncodeBuffer:  1276,        // Minimum CGO read/encode buffer
		MaxDecodeWriteBuffer: 4096,        // Maximum CGO decode/write buffer

		// IPC Configuration - Balanced for stability
		MagicNumber:  0xDEADBEEF,             // IPC message validation header
		MaxFrameSize: 4096,                   // Maximum audio frame size (4KB)
		WriteTimeout: 1000 * time.Millisecond, // Further increased timeout to handle quality change bursts
		HeaderSize:   8,                      // IPC message header size

		// Monitoring and Metrics - Balanced for stability
		MetricsUpdateInterval: 1000 * time.Millisecond, // Stable metrics collection frequency
		WarmupSamples:         10,                      // Adequate warmup samples for accuracy
		MetricsChannelBuffer:  100,                     // Adequate metrics data channel buffer
		LatencyHistorySize:    100,                     // Adequate latency measurements to keep

		// Process Monitoring Constants
		MaxCPUPercent:          100.0, // Maximum CPU percentage
		MinCPUPercent:          0.01,  // Minimum CPU percentage
		DefaultClockTicks:      250.0, // Default clock ticks for embedded ARM systems
		DefaultMemoryGB:        8,     // Default memory in GB
		MaxWarmupSamples:       3,     // Maximum warmup samples
		WarmupCPUSamples:       2,     // CPU warmup samples
		LogThrottleIntervalSec: 10,    // Log throttle interval in seconds
		MinValidClockTicks:     50,    // Minimum valid clock ticks
		MaxValidClockTicks:     1000,  // Maximum valid clock ticks

		// Performance Tuning
		CPUFactor:     0.7, // CPU weight in performance calculations
		MemoryFactor:  0.8, // Memory weight in performance calculations
		LatencyFactor: 0.9, // Latency weight in performance calculations

		// Error Handling
		RetryDelay:           100 * time.Millisecond, // Initial retry delay
		MaxRetryDelay:        5 * time.Second,        // Maximum retry delay
		BackoffMultiplier:    2.0,                    // Exponential backoff multiplier
		MaxConsecutiveErrors: 5,                      // Consecutive error threshold

		// Connection Retry Configuration
		MaxConnectionAttempts:   15,                    // Maximum connection retry attempts
		ConnectionRetryDelay:    50 * time.Millisecond, // Initial connection retry delay
		MaxConnectionRetryDelay: 2 * time.Second,       // Maximum connection retry delay
		ConnectionBackoffFactor: 1.5,                   // Connection retry backoff factor
		ConnectionTimeoutDelay:  5 * time.Second,       // Connection timeout for each attempt
		ReconnectionInterval:    30 * time.Second,      // Interval for automatic reconnection attempts
		HealthCheckInterval:     10 * time.Second,      // Health check interval for connections

		// Timing Constants - Optimized for quality change stability
		DefaultSleepDuration:       100 * time.Millisecond, // Balanced polling interval
		ShortSleepDuration:         10 * time.Millisecond,  // Balanced high-frequency polling
		LongSleepDuration:          200 * time.Millisecond, // Balanced background task delay
		DefaultTickerInterval:      100 * time.Millisecond, // Balanced periodic task interval
		BufferUpdateInterval:       250 * time.Millisecond, // Faster buffer size update frequency
		InputSupervisorTimeout:     5 * time.Second,        // Input monitoring timeout
		OutputSupervisorTimeout:    5 * time.Second,        // Output monitoring timeout
		BatchProcessingDelay:       5 * time.Millisecond,   // Reduced batch processing delay
		AdaptiveOptimizerStability: 5 * time.Second,        // Faster adaptive stability period

		LatencyMonitorTarget: 50 * time.Millisecond, // Balanced target latency for monitoring

		// Adaptive Buffer Configuration - Optimized for low latency
		LowCPUThreshold:             0.30,
		HighCPUThreshold:            0.70,
		LowMemoryThreshold:          0.60,
		HighMemoryThreshold:         0.80,
		AdaptiveBufferTargetLatency: 10 * time.Millisecond, // Aggressive target latency for responsiveness

		// Adaptive Buffer Size Configuration - Optimized for quality change bursts
		AdaptiveMinBufferSize:     128, // Significantly increased minimum to handle bursts
		AdaptiveMaxBufferSize:     512, // Much higher maximum for quality changes
		AdaptiveDefaultBufferSize: 256, // Higher default for stability

		// Adaptive Optimizer Configuration - Faster response
		CooldownPeriod:                 15 * time.Second,       // Reduced cooldown period
		RollbackThreshold:              200 * time.Millisecond, // Lower rollback threshold
		AdaptiveOptimizerLatencyTarget: 30 * time.Millisecond,  // Reduced latency target

		// Latency Monitor Configuration - More aggressive monitoring
		MaxLatencyThreshold:         150 * time.Millisecond, // Lower max latency threshold
		JitterThreshold:             15 * time.Millisecond,  // Reduced jitter threshold
		LatencyOptimizationInterval: 3 * time.Second,        // More frequent optimization
		LatencyAdaptiveThreshold:    0.7,                    // More aggressive adaptive threshold

		// Microphone Contention Configuration
		MicContentionTimeout: 200 * time.Millisecond,

		// Buffer Pool Configuration
		PreallocPercentage: 20,

		// Sleep and Backoff Configuration
		BackoffStart: 50 * time.Millisecond,

		// Protocol Magic Numbers
		InputMagicNumber:  0x4A4B4D49, // "JKMI" (JetKVM Microphone Input)
		OutputMagicNumber: 0x4A4B4F55, // "JKOU" (JetKVM Output)

		// Calculation Constants
		PercentageMultiplier: 100.0, // Standard percentage conversion (0.5 * 100 = 50%)
		AveragingWeight:      0.7,   // Weight for smoothing values (70% recent, 30% historical)
		ScalingFactor:        1.5,   // General scaling factor for adaptive adjustments

		SmoothingFactor:         0.3, // For adaptive buffer smoothing
		CPUMemoryWeight:         0.5, // CPU factor weight in combined calculations
		MemoryWeight:            0.3, // Memory factor weight in combined calculations
		LatencyWeight:           0.2, // Latency factor weight in combined calculations
		PoolGrowthMultiplier:    2,   // Pool growth multiplier
		LatencyScalingFactor:    2.0, // Latency ratio scaling factor
		OptimizerAggressiveness: 0.7, // Optimizer aggressiveness factor

		// CGO Audio Processing Constants - Balanced for stability
		CGOUsleepMicroseconds:   1000,         // 1000 microseconds (1ms) for stable CGO usleep calls
		CGOPCMBufferSize:        1920,         // 1920 samples for PCM buffer (max 2ch*960)
		CGONanosecondsPerSecond: 1000000000.0, // 1000000000.0 for nanosecond conversions

		// Frontend Constants - Balanced for stability
		FrontendOperationDebounceMS: 1000,  // 1000ms debounce for frontend operations
		FrontendSyncDebounceMS:      1000,  // 1000ms debounce for sync operations
		FrontendSampleRate:          48000, // 48000Hz sample rate for frontend audio
		FrontendRetryDelayMS:        500,   // 500ms retry delay
		FrontendShortDelayMS:        200,   // 200ms short delay
		FrontendLongDelayMS:         300,   // 300ms long delay
		FrontendSyncDelayMS:         500,   // 500ms sync delay
		FrontendMaxRetryAttempts:    3,     // 3 maximum retry attempts
		FrontendAudioLevelUpdateMS:  100,   // 100ms audio level update interval
		FrontendFFTSize:             256,   // 256 FFT size for audio analysis
		FrontendAudioLevelMax:       100,   // 100 maximum audio level
		FrontendReconnectIntervalMS: 3000,  // 3000ms reconnect interval
		FrontendSubscriptionDelayMS: 100,   // 100ms subscription delay
		FrontendDebugIntervalMS:     5000,  // 5000ms debug interval

		// Process Monitor Constants
		ProcessMonitorDefaultMemoryGB: 4,      // 4GB default memory for fallback
		ProcessMonitorKBToBytes:       1024,   // 1024 conversion factor
		ProcessMonitorDefaultClockHz:  250.0,  // 250.0 Hz default for ARM systems
		ProcessMonitorFallbackClockHz: 1000.0, // 1000.0 Hz fallback clock
		ProcessMonitorTraditionalHz:   100.0,  // 100.0 Hz traditional clock

		// Batch Processing Constants - Optimized for quality change bursts
		BatchProcessorFramesPerBatch:         16,                    // Larger batches for quality changes
		BatchProcessorTimeout:                20 * time.Millisecond, // Longer timeout for bursts
		BatchProcessorMaxQueueSize:           64,                    // Larger queue for quality changes
		BatchProcessorAdaptiveThreshold:      0.6,                   // Lower threshold for faster adaptation
		BatchProcessorThreadPinningThreshold: 8,                     // Lower threshold for better performance

		// Output Streaming Constants - Balanced for stability
		OutputStreamingFrameIntervalMS: 20, // 20ms frame interval (50 FPS) for stability

		// IPC Constants
		IPCInitialBufferFrames: 500, // 500 frames for initial buffer

		// Event Constants - Balanced for stability
		EventTimeoutSeconds:      2,                          // 2 seconds for event timeout
		EventTimeFormatString:    "2006-01-02T15:04:05.000Z", // "2006-01-02T15:04:05.000Z" time format
		EventSubscriptionDelayMS: 100,                        // 100ms subscription delay

		// Goroutine Pool Configuration
		MaxAudioProcessorWorkers: 16,               // 16 workers for audio processing tasks
		MaxAudioReaderWorkers:    8,                // 8 workers for audio reading tasks
		AudioProcessorQueueSize:  64,               // 64 tasks queue size for processor pool
		AudioReaderQueueSize:     32,               // 32 tasks queue size for reader pool
		WorkerMaxIdleTime:        60 * time.Second, // 60s maximum idle time before worker termination

		// Input Processing Constants - Balanced for stability
		InputProcessingTimeoutMS: 10, // 10ms processing timeout threshold

		// Adaptive Buffer Constants
		AdaptiveBufferCPUMultiplier:    100, // 100 multiplier for CPU percentage
		AdaptiveBufferMemoryMultiplier: 100, // 100 multiplier for memory percentage

		// Socket Names
		InputSocketName:  "audio_input.sock",  // Socket name for audio input IPC
		OutputSocketName: "audio_output.sock", // Socket name for audio output IPC

		// Component Names
		AudioInputComponentName:  "audio-input",  // Component name for input logging
		AudioOutputComponentName: "audio-output", // Component name for output logging
		AudioServerComponentName: "audio-server", // Component name for server logging
		AudioRelayComponentName:  "audio-relay",  // Component name for relay logging
		AudioEventsComponentName: "audio-events", // Component name for events logging

		// Test Configuration
		TestSocketTimeout: 100 * time.Millisecond, // 100ms timeout for test socket operations
		TestBufferSize:    4096,                   // 4096 bytes buffer size for test operations
		TestRetryDelay:    200 * time.Millisecond, // 200ms delay between test retry attempts

		// Latency Histogram Configuration
		LatencyHistogramMaxSamples: 1000, // 1000 samples for latency tracking
		LatencyPercentile50:        50,   // 50th percentile calculation factor
		LatencyPercentile95:        95,   // 95th percentile calculation factor
		LatencyPercentile99:        99,   // 99th percentile calculation factor

		// Buffer Pool Efficiency Constants
		BufferPoolMaxOperations: 1000,  // 1000 operations for efficiency tracking
		HitRateCalculationBase:  100.0, // 100.0 base for hit rate percentage calculation

		// Validation Constants
		MaxLatency:               500 * time.Millisecond, // 500ms maximum allowed latency
		MinMetricsUpdateInterval: 100 * time.Millisecond, // 100ms minimum metrics update interval
		MaxMetricsUpdateInterval: 10 * time.Second,       // 10s maximum metrics update interval
		MinSampleRate:            8000,                   // 8kHz minimum sample rate
		MaxSampleRate:            48000,                  // 48kHz maximum sample rate
		MaxChannels:              8,                      // 8 maximum audio channels

		// CGO Constants
		CGOMaxBackoffMicroseconds: 500000, // 500ms maximum backoff in microseconds
		CGOMaxAttempts:            5,      // 5 maximum retry attempts

		// Validation Frame Size Limits
		MinFrameDuration: 10 * time.Millisecond,  // 10ms minimum frame duration
		MaxFrameDuration: 100 * time.Millisecond, // 100ms maximum frame duration

		// Valid Sample Rates
		ValidSampleRates: []int{8000, 12000, 16000, 22050, 24000, 44100, 48000}, // Supported sample rates

		// Opus Bitrate Validation Constants
		MinOpusBitrate: 6000,   // 6000 bps minimum Opus bitrate
		MaxOpusBitrate: 510000, // 510000 bps maximum Opus bitrate

		// Validation Configuration
		MaxValidationTime:  5 * time.Second, // 5s maximum validation timeout
		MinFrameSize:       1,               // 1 byte minimum frame size (allow small frames)
		FrameSizeTolerance: 512,             // 512 bytes frame size tolerance

		// Removed device health monitoring configuration - functionality not used

		// Latency Histogram Bucket Configuration
		LatencyBucket10ms:  10 * time.Millisecond,  // 10ms latency bucket
		LatencyBucket25ms:  25 * time.Millisecond,  // 25ms latency bucket
		LatencyBucket50ms:  50 * time.Millisecond,  // 50ms latency bucket
		LatencyBucket100ms: 100 * time.Millisecond, // 100ms latency bucket
		LatencyBucket250ms: 250 * time.Millisecond, // 250ms latency bucket
		LatencyBucket500ms: 500 * time.Millisecond, // 500ms latency bucket
		LatencyBucket1s:    1 * time.Second,        // 1s latency bucket
		LatencyBucket2s:    2 * time.Second,        // 2s latency bucket

		// Batch Audio Processing Configuration
		MinBatchSizeForThreadPinning: 5, // Minimum batch size to pin thread

		// Goroutine Monitoring Configuration
		GoroutineMonitorInterval: 30 * time.Second, // 30s monitoring interval

		// Performance Configuration Flags - Production optimizations

	}
}

// Global configuration instance
var Config = DefaultAudioConfig()

// UpdateConfig allows runtime configuration updates
func UpdateConfig(newConfig *AudioConfigConstants) {
	// Validate the new configuration before applying it
	if err := ValidateAudioConfigConstants(newConfig); err != nil {
		// Log validation error and keep current configuration
		logger := logging.GetDefaultLogger().With().Str("component", "AudioConfig").Logger()
		logger.Error().Err(err).Msg("Configuration validation failed, keeping current configuration")
		return
	}

	Config = newConfig
	logger := logging.GetDefaultLogger().With().Str("component", "AudioConfig").Logger()
	logger.Info().Msg("Audio configuration updated successfully")
}

// GetConfig returns the current configuration
func GetConfig() *AudioConfigConstants {
	return Config
}
