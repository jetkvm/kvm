package audio

import (
	"time"

	"github.com/jetkvm/kvm/internal/logging"
)

// AudioConfigConstants centralizes all hardcoded values used across audio components.
// This configuration system allows runtime tuning of audio performance, quality, and resource usage.
// Each constant is documented with its purpose, usage location, and impact on system behavior.
type AudioConfigConstants struct {
	// Audio Quality Presets
	// MaxAudioFrameSize defines the maximum size of an audio frame in bytes.
	// Used in: buffer_pool.go, adaptive_buffer.go
	// Impact: Higher values allow larger audio chunks but increase memory usage and latency.
	// Typical range: 1024-8192 bytes. Default 4096 provides good balance.
	MaxAudioFrameSize int

	// Opus Encoding Parameters - Core codec settings for audio compression
	// OpusBitrate sets the target bitrate for Opus encoding in bits per second.
	// Used in: cgo_audio.go for encoder initialization
	// Impact: Higher bitrates improve audio quality but increase bandwidth usage.
	// Range: 6000-510000 bps. 128000 (128kbps) provides high quality for most use cases.
	OpusBitrate int

	// OpusComplexity controls the computational complexity of Opus encoding (0-10).
	// Used in: cgo_audio.go for encoder configuration
	// Impact: Higher values improve quality but increase CPU usage and encoding latency.
	// Range: 0-10. Value 10 provides best quality, 0 fastest encoding.
	OpusComplexity int

	// OpusVBR enables Variable Bit Rate encoding (0=CBR, 1=VBR).
	// Used in: cgo_audio.go for encoder mode selection
	// Impact: VBR (1) adapts bitrate to content complexity, improving efficiency.
	// CBR (0) maintains constant bitrate for predictable bandwidth usage.
	OpusVBR int

	// OpusVBRConstraint enables constrained VBR mode (0=unconstrained, 1=constrained).
	// Used in: cgo_audio.go when VBR is enabled
	// Impact: Constrained VBR (1) limits bitrate variation for more predictable bandwidth.
	// Unconstrained (0) allows full bitrate adaptation for optimal quality.
	OpusVBRConstraint int

	// OpusDTX enables Discontinuous Transmission (0=disabled, 1=enabled).
	// Used in: cgo_audio.go for encoder optimization
	// Impact: DTX (1) reduces bandwidth during silence but may cause audio artifacts.
	// Disabled (0) maintains constant transmission for consistent quality.
	OpusDTX int

	// Audio Parameters - Fundamental audio stream characteristics
	// SampleRate defines the number of audio samples per second in Hz.
	// Used in: All audio processing components
	// Impact: Higher rates improve frequency response but increase processing load.
	// Common values: 16000 (voice), 44100 (CD quality), 48000 (professional).
	SampleRate int

	// Channels specifies the number of audio channels (1=mono, 2=stereo).
	// Used in: All audio processing and encoding/decoding operations
	// Impact: Stereo (2) provides spatial audio but doubles bandwidth and processing.
	// Mono (1) reduces resource usage but loses spatial information.
	Channels int

	// FrameSize defines the number of samples per audio frame.
	// Used in: Opus encoding/decoding, buffer management
	// Impact: Larger frames reduce overhead but increase latency.
	// Must match Opus frame sizes: 120, 240, 480, 960, 1920, 2880 samples.
	FrameSize int

	// MaxPacketSize sets the maximum size of encoded audio packets in bytes.
	// Used in: Network transmission, buffer allocation
	// Impact: Larger packets reduce network overhead but increase burst bandwidth.
	// Should accommodate worst-case Opus output plus protocol headers.
	MaxPacketSize int

	// Audio Quality Bitrates - Predefined quality presets for different use cases
	// These bitrates are used in audio.go for quality level selection
	// Impact: Higher bitrates improve audio fidelity but increase bandwidth usage

	// AudioQualityLowOutputBitrate defines bitrate for low-quality audio output (kbps).
	// Used in: audio.go for bandwidth-constrained scenarios
	// Impact: Minimal bandwidth usage but reduced audio quality. Suitable for voice-only.
	// Default 32kbps provides acceptable voice quality with very low bandwidth.
	AudioQualityLowOutputBitrate int

	// AudioQualityLowInputBitrate defines bitrate for low-quality audio input (kbps).
	// Used in: audio.go for microphone input in low-bandwidth scenarios
	// Impact: Reduces upload bandwidth but may affect voice clarity.
	// Default 16kbps suitable for basic voice communication.
	AudioQualityLowInputBitrate int

	// AudioQualityMediumOutputBitrate defines bitrate for medium-quality audio output (kbps).
	// Used in: audio.go for balanced quality/bandwidth scenarios
	// Impact: Good balance between quality and bandwidth usage.
	// Default 64kbps provides clear voice and acceptable music quality.
	AudioQualityMediumOutputBitrate int

	// AudioQualityMediumInputBitrate defines bitrate for medium-quality audio input (kbps).
	// Used in: audio.go for microphone input with balanced quality
	// Impact: Better voice quality than low setting with moderate bandwidth usage.
	// Default 32kbps suitable for clear voice communication.
	AudioQualityMediumInputBitrate int

	// AudioQualityHighOutputBitrate defines bitrate for high-quality audio output (kbps).
	// Used in: audio.go for high-fidelity audio scenarios
	// Impact: Excellent audio quality but higher bandwidth requirements.
	// Default 128kbps provides near-CD quality for music and crystal-clear voice.
	AudioQualityHighOutputBitrate int

	// AudioQualityHighInputBitrate defines bitrate for high-quality audio input (kbps).
	// Used in: audio.go for high-quality microphone capture
	// Impact: Superior voice quality but increased upload bandwidth usage.
	// Default 64kbps suitable for professional voice communication.
	AudioQualityHighInputBitrate int

	// AudioQualityUltraOutputBitrate defines bitrate for ultra-high-quality audio output (kbps).
	// Used in: audio.go for maximum quality scenarios
	// Impact: Maximum audio fidelity but highest bandwidth consumption.
	// Default 192kbps provides studio-quality audio for critical applications.
	AudioQualityUltraOutputBitrate int

	// AudioQualityUltraInputBitrate defines bitrate for ultra-high-quality audio input (kbps).
	// Used in: audio.go for maximum quality microphone capture
	// Impact: Best possible voice quality but maximum upload bandwidth usage.
	// Default 96kbps suitable for broadcast-quality voice communication.
	AudioQualityUltraInputBitrate int

	// Audio Quality Sample Rates - Frequency sampling rates for different quality levels
	// Used in: audio.go for configuring audio capture and playback sample rates
	// Impact: Higher sample rates capture more frequency detail but increase processing load

	// AudioQualityLowSampleRate defines sample rate for low-quality audio (Hz).
	// Used in: audio.go for bandwidth-constrained scenarios
	// Impact: Reduces frequency response but minimizes processing and bandwidth.
	// Default 22050Hz captures frequencies up to 11kHz, adequate for voice.
	AudioQualityLowSampleRate int

	// AudioQualityMediumSampleRate defines sample rate for medium-quality audio (Hz).
	// Used in: audio.go for balanced quality scenarios
	// Impact: Good frequency response with moderate processing requirements.
	// Default 44100Hz (CD quality) captures frequencies up to 22kHz.
	AudioQualityMediumSampleRate int

	// AudioQualityMicLowSampleRate defines sample rate for low-quality microphone input (Hz).
	// Used in: audio.go for microphone capture in constrained scenarios
	// Impact: Optimized for voice communication with minimal processing overhead.
	// Default 16000Hz captures voice frequencies (300-3400Hz) efficiently.
	AudioQualityMicLowSampleRate int

	// Audio Quality Frame Sizes - Duration of audio frames for different quality levels
	// Used in: audio.go for configuring Opus frame duration
	// Impact: Larger frames reduce overhead but increase latency and memory usage

	// AudioQualityLowFrameSize defines frame duration for low-quality audio.
	// Used in: audio.go for low-latency scenarios with minimal processing
	// Impact: Longer frames reduce CPU overhead but increase audio latency.
	// Default 40ms provides good efficiency for voice communication.
	AudioQualityLowFrameSize time.Duration

	// AudioQualityMediumFrameSize defines frame duration for medium-quality audio.
	// Used in: audio.go for balanced latency and efficiency
	// Impact: Moderate frame size balances latency and processing efficiency.
	// Default 20ms provides good balance for most applications.
	AudioQualityMediumFrameSize time.Duration

	// AudioQualityHighFrameSize defines frame duration for high-quality audio.
	// Used in: audio.go for high-quality scenarios
	// Impact: Optimized frame size for high-quality encoding efficiency.
	// Default 20ms maintains low latency while supporting high bitrates.
	AudioQualityHighFrameSize time.Duration

	// AudioQualityUltraFrameSize defines frame duration for ultra-quality audio.
	// Used in: audio.go for maximum quality scenarios
	// Impact: Smaller frames reduce latency but increase processing overhead.
	// Default 10ms provides minimal latency for real-time applications.
	AudioQualityUltraFrameSize time.Duration

	// Audio Quality Channels - Channel configuration for different quality levels
	// Used in: audio.go for configuring mono/stereo audio
	// Impact: Stereo doubles bandwidth and processing but provides spatial audio

	// AudioQualityLowChannels defines channel count for low-quality audio.
	// Used in: audio.go for bandwidth-constrained scenarios
	// Impact: Mono (1) minimizes bandwidth and processing for voice communication.
	// Default 1 (mono) suitable for voice-only applications.
	AudioQualityLowChannels int

	// AudioQualityMediumChannels defines channel count for medium-quality audio.
	// Used in: audio.go for balanced quality scenarios
	// Impact: Stereo (2) provides spatial audio with moderate bandwidth increase.
	// Default 2 (stereo) suitable for general audio applications.
	AudioQualityMediumChannels int

	// AudioQualityHighChannels defines channel count for high-quality audio.
	// Used in: audio.go for high-fidelity scenarios
	// Impact: Stereo (2) essential for high-quality music and spatial audio.
	// Default 2 (stereo) required for full audio experience.
	AudioQualityHighChannels int

	// AudioQualityUltraChannels defines channel count for ultra-quality audio.
	// Used in: audio.go for maximum quality scenarios
	// Impact: Stereo (2) mandatory for studio-quality audio reproduction.
	// Default 2 (stereo) provides full spatial audio fidelity.
	AudioQualityUltraChannels int

	// CGO Audio Constants - Low-level C library configuration for audio processing
	// These constants are passed to C code in cgo_audio.go for native audio operations
	// Impact: Direct control over native audio library behavior and performance

	// CGOOpusBitrate sets the bitrate for native Opus encoder (bits per second).
	// Used in: cgo_audio.go update_audio_constants() function
	// Impact: Controls quality vs bandwidth tradeoff in native encoding.
	// Default 96000 (96kbps) provides good quality for real-time applications.
	CGOOpusBitrate int

	// CGOOpusComplexity sets computational complexity for native Opus encoder (0-10).
	// Used in: cgo_audio.go for native encoder configuration
	// Impact: Higher values improve quality but increase CPU usage in C code.
	// Default 3 balances quality and performance for embedded systems.
	CGOOpusComplexity int

	// CGOOpusVBR enables Variable Bit Rate in native Opus encoder (0=CBR, 1=VBR).
	// Used in: cgo_audio.go for native encoder mode selection
	// Impact: VBR (1) adapts bitrate dynamically for better efficiency.
	// Default 1 (VBR) provides optimal bandwidth utilization.
	CGOOpusVBR int

	// CGOOpusVBRConstraint enables constrained VBR in native encoder (0/1).
	// Used in: cgo_audio.go when VBR is enabled
	// Impact: Constrains bitrate variation for more predictable bandwidth.
	// Default 1 (constrained) provides controlled bandwidth usage.
	CGOOpusVBRConstraint int

	// CGOOpusSignalType specifies signal type hint for native Opus encoder.
	// Used in: cgo_audio.go for encoder optimization
	// Impact: Optimizes encoder for specific content type (voice vs music).
	// Values: 3=OPUS_SIGNAL_MUSIC for general audio, 2=OPUS_SIGNAL_VOICE for speech.
	CGOOpusSignalType int

	// CGOOpusBandwidth sets frequency bandwidth for native Opus encoder.
	// Used in: cgo_audio.go for encoder frequency range configuration
	// Impact: Controls frequency range vs bitrate efficiency.
	// Default 1105 (OPUS_BANDWIDTH_FULLBAND) uses full 20kHz bandwidth.
	CGOOpusBandwidth int

	// CGOOpusDTX enables Discontinuous Transmission in native encoder (0/1).
	// Used in: cgo_audio.go for bandwidth optimization during silence
	// Impact: Reduces bandwidth during silence but may cause audio artifacts.
	// Default 0 (disabled) maintains consistent audio quality.
	CGOOpusDTX int

	// CGOSampleRate defines sample rate for native audio processing (Hz).
	// Used in: cgo_audio.go for ALSA and Opus configuration
	// Impact: Must match system audio capabilities and Opus requirements.
	// Default 48000Hz provides professional audio quality.
	CGOSampleRate int

	// CGOChannels defines channel count for native audio processing.
	// Used in: cgo_audio.go for ALSA device and Opus encoder configuration
	// Impact: Must match audio hardware capabilities and application needs.
	// Default 2 (stereo) provides full spatial audio support.
	CGOChannels int

	// CGOFrameSize defines frame size for native Opus processing (samples).
	// Used in: cgo_audio.go for Opus encoder/decoder frame configuration
	// Impact: Must be valid Opus frame size, affects latency and efficiency.
	// Default 960 samples (20ms at 48kHz) balances latency and efficiency.
	CGOFrameSize int

	// CGOMaxPacketSize defines maximum packet size for native encoding (bytes).
	// Used in: cgo_audio.go for buffer allocation in C code
	// Impact: Must accommodate worst-case Opus output to prevent buffer overruns.
	// Default 1500 bytes handles typical Opus output with safety margin.
	CGOMaxPacketSize int

	// Input IPC Constants - Configuration for audio input inter-process communication
	// Used in: input_ipc.go for microphone audio capture and processing
	// Impact: Controls audio input quality and processing efficiency

	// InputIPCSampleRate defines sample rate for input IPC audio processing (Hz).
	// Used in: input_ipc.go for microphone capture configuration
	// Impact: Must match microphone capabilities and encoding requirements.
	// Default 48000Hz provides professional quality microphone input.
	InputIPCSampleRate int

	// InputIPCChannels defines channel count for input IPC audio processing.
	// Used in: input_ipc.go for microphone channel configuration
	// Impact: Stereo (2) captures spatial audio, mono (1) reduces processing.
	// Default 2 (stereo) supports full microphone array capabilities.
	InputIPCChannels int

	// InputIPCFrameSize defines frame size for input IPC processing (samples).
	// Used in: input_ipc.go for microphone frame processing
	// Impact: Larger frames reduce overhead but increase input latency.
	// Default 960 samples (20ms at 48kHz) balances latency and efficiency.
	InputIPCFrameSize int

	// Output IPC Constants - Configuration for audio output inter-process communication
	// Used in: output_streaming.go for audio playback and streaming
	// Impact: Controls audio output quality, latency, and reliability

	// OutputMaxFrameSize defines maximum frame size for output processing (bytes).
	// Used in: output_streaming.go for buffer allocation and frame processing
	// Impact: Larger frames allow bigger audio chunks but increase memory usage.
	// Default 4096 bytes accommodates typical audio frames with safety margin.
	OutputMaxFrameSize int

	// OutputWriteTimeout defines timeout for output write operations.
	// Used in: output_streaming.go for preventing blocking on slow audio devices
	// Impact: Shorter timeouts improve responsiveness but may cause audio drops.
	// Default 10ms prevents blocking while allowing device response time.
	OutputWriteTimeout time.Duration

	// OutputMaxDroppedFrames defines maximum consecutive dropped frames before error.
	// Used in: output_streaming.go for audio quality monitoring
	// Impact: Higher values tolerate more audio glitches but may degrade experience.
	// Default 50 frames allows brief interruptions while detecting serious issues.
	OutputMaxDroppedFrames int

	// OutputHeaderSize defines size of output message headers (bytes).
	// Used in: output_streaming.go for message parsing and buffer allocation
	// Impact: Must match actual header size to prevent parsing errors.
	// Default 17 bytes matches current output message header format.
	OutputHeaderSize int

	// OutputMessagePoolSize defines size of output message object pool.
	// Used in: output_streaming.go for memory management and performance
	// Impact: Larger pools reduce allocation overhead but increase memory usage.
	// Default 128 messages provides good balance for typical workloads.
	OutputMessagePoolSize int

	// Socket Buffer Constants - Network socket buffer configuration for audio streaming
	// Used in: socket_buffer.go for optimizing network performance
	// Impact: Controls network throughput, latency, and memory usage

	// SocketOptimalBuffer defines optimal socket buffer size (bytes).
	// Used in: socket_buffer.go for default socket buffer configuration
	// Impact: Balances throughput and memory usage for typical audio streams.
	// Default 131072 (128KB) provides good performance for most scenarios.
	SocketOptimalBuffer int

	// SocketMaxBuffer defines maximum socket buffer size (bytes).
	// Used in: socket_buffer.go for high-throughput scenarios
	// Impact: Larger buffers improve throughput but increase memory usage and latency.
	// Default 262144 (256KB) handles high-bitrate audio without excessive memory.
	SocketMaxBuffer int

	// SocketMinBuffer defines minimum socket buffer size (bytes).
	// Used in: socket_buffer.go for low-memory scenarios
	// Impact: Smaller buffers reduce memory usage but may limit throughput.
	// Default 32768 (32KB) provides minimum viable buffer for audio streaming.
	SocketMinBuffer int

	// Scheduling Policy Constants - Linux process scheduling policies for audio threads
	// Used in: process_monitor.go for configuring thread scheduling behavior
	// Impact: Controls how audio threads are scheduled by the Linux kernel

	// SchedNormal defines normal (CFS) scheduling policy.
	// Used in: process_monitor.go for non-critical audio threads
	// Impact: Standard time-sharing scheduling, may cause audio latency under load.
	// Value 0 corresponds to SCHED_NORMAL in Linux kernel.
	SchedNormal int

	// SchedFIFO defines First-In-First-Out real-time scheduling policy.
	// Used in: process_monitor.go for critical audio threads requiring deterministic timing
	// Impact: Provides real-time scheduling but may starve other processes if misused.
	// Value 1 corresponds to SCHED_FIFO in Linux kernel.
	SchedFIFO int

	// SchedRR defines Round-Robin real-time scheduling policy.
	// Used in: process_monitor.go for real-time threads that should share CPU time
	// Impact: Real-time scheduling with time slicing, balances determinism and fairness.
	// Value 2 corresponds to SCHED_RR in Linux kernel.
	SchedRR int

	// Real-time Priority Levels - Priority values for real-time audio thread scheduling
	// Used in: process_monitor.go for setting thread priorities
	// Impact: Higher priorities get more CPU time but may affect system responsiveness

	// RTAudioHighPriority defines highest priority for critical audio threads.
	// Used in: process_monitor.go for time-critical audio processing (encoding/decoding)
	// Impact: Ensures audio threads get CPU time but may impact system responsiveness.
	// Default 80 provides high priority without completely starving other processes.
	RTAudioHighPriority int

	// RTAudioMediumPriority defines medium priority for important audio threads.
	// Used in: process_monitor.go for audio I/O and buffering operations
	// Impact: Good priority for audio operations while maintaining system balance.
	// Default 60 provides elevated priority for audio without extreme impact.
	RTAudioMediumPriority int

	// RTAudioLowPriority defines low priority for background audio threads.
	// Used in: process_monitor.go for audio monitoring and metrics collection
	// Impact: Ensures audio background tasks run without impacting critical operations.
	// Default 40 provides some priority elevation while remaining background.
	RTAudioLowPriority int

	// RTNormalPriority defines normal priority (no real-time scheduling).
	// Used in: process_monitor.go for non-critical audio threads
	// Impact: Standard scheduling priority, no special real-time guarantees.
	// Default 0 uses normal kernel scheduling without real-time privileges.
	RTNormalPriority int

	// Process Management - Configuration for audio process lifecycle management
	// Used in: supervisor.go for managing audio process restarts and recovery
	// Impact: Controls system resilience and recovery from audio process failures

	// MaxRestartAttempts defines maximum number of restart attempts for failed processes.
	// Used in: supervisor.go for limiting restart attempts to prevent infinite loops
	// Impact: Higher values increase resilience but may mask persistent problems.
	// Default 5 attempts allows recovery from transient issues while detecting persistent failures.
	MaxRestartAttempts int

	// RestartWindow defines time window for counting restart attempts.
	// Used in: supervisor.go for restart attempt rate limiting
	// Impact: Longer windows allow more restart attempts but slower failure detection.
	// Default 5 minutes provides reasonable window for transient issue recovery.
	RestartWindow time.Duration

	// RestartDelay defines initial delay before restarting failed processes.
	// Used in: supervisor.go for implementing restart backoff strategy
	// Impact: Longer delays reduce restart frequency but increase recovery time.
	// Default 2 seconds allows brief recovery time without excessive delay.
	RestartDelay time.Duration

	// MaxRestartDelay defines maximum delay between restart attempts.
	// Used in: supervisor.go for capping exponential backoff delays
	// Impact: Prevents excessively long delays while maintaining backoff benefits.
	// Default 30 seconds caps restart delays at reasonable maximum.
	MaxRestartDelay time.Duration

	// Buffer Management - Memory buffer configuration for audio processing
	// Used across multiple components for memory allocation and performance optimization
	// Impact: Controls memory usage, allocation efficiency, and processing performance

	// PreallocSize defines size of preallocated memory pools (bytes).
	// Used in: buffer_pool.go for initial memory pool allocation
	// Impact: Larger pools reduce allocation overhead but increase memory usage.
	// Default 1MB (1024*1024) provides good balance for typical audio workloads.
	PreallocSize int

	// MaxPoolSize defines maximum number of objects in memory pools.
	// Used in: buffer_pool.go for limiting pool growth
	// Impact: Larger pools reduce allocation frequency but increase memory usage.
	// Default 100 objects provides good balance between performance and memory.
	MaxPoolSize int

	// MessagePoolSize defines size of message object pools.
	// Used in: Various IPC components for message allocation
	// Impact: Larger pools reduce allocation overhead for message passing.
	// Default 256 messages handles typical message throughput efficiently.
	MessagePoolSize int

	// OptimalSocketBuffer defines optimal socket buffer size (bytes).
	// Used in: socket_buffer.go for default socket configuration
	// Impact: Balances network throughput and memory usage.
	// Default 262144 (256KB) provides good performance for audio streaming.
	OptimalSocketBuffer int

	// MaxSocketBuffer defines maximum socket buffer size (bytes).
	// Used in: socket_buffer.go for high-throughput scenarios
	// Impact: Larger buffers improve throughput but increase memory and latency.
	// Default 1048576 (1MB) handles high-bitrate streams without excessive memory.
	MaxSocketBuffer int

	// MinSocketBuffer defines minimum socket buffer size (bytes).
	// Used in: socket_buffer.go for memory-constrained scenarios
	// Impact: Smaller buffers reduce memory but may limit throughput.
	// Default 8192 (8KB) provides minimum viable buffer for audio streaming.
	MinSocketBuffer int

	// ChannelBufferSize defines buffer size for Go channels in audio processing.
	// Used in: Various components for inter-goroutine communication
	// Impact: Larger buffers reduce blocking but increase memory usage and latency.
	// Default 500 messages provides good balance for audio processing pipelines.
	ChannelBufferSize int

	// AudioFramePoolSize defines size of audio frame object pools.
	// Used in: buffer_pool.go for audio frame allocation
	// Impact: Larger pools reduce allocation overhead for frame processing.
	// Default 1500 frames handles typical audio frame throughput efficiently.
	AudioFramePoolSize int

	// PageSize defines memory page size for alignment and allocation (bytes).
	// Used in: buffer_pool.go for memory-aligned allocations
	// Impact: Must match system page size for optimal memory performance.
	// Default 4096 bytes matches typical Linux page size.
	PageSize int

	// InitialBufferFrames defines initial buffer size in audio frames.
	// Used in: adaptive_buffer.go for initial buffer allocation
	// Impact: Larger initial buffers reduce early reallocations but increase startup memory.
	// Default 500 frames provides good starting point for most audio scenarios.
	InitialBufferFrames int

	// BytesToMBDivisor defines divisor for converting bytes to megabytes.
	// Used in: memory_metrics.go for memory usage reporting
	// Impact: Must be 1024*1024 for accurate binary megabyte conversion.
	// Default 1048576 (1024*1024) provides standard binary MB conversion.
	BytesToMBDivisor int

	// MinReadEncodeBuffer defines minimum buffer size for CGO audio read/encode (bytes).
	// Used in: cgo_audio.go for native audio processing buffer allocation
	// Impact: Must accommodate minimum audio frame size to prevent buffer underruns.
	// Default 1276 bytes handles minimum Opus frame with safety margin.
	MinReadEncodeBuffer int

	// MaxDecodeWriteBuffer defines maximum buffer size for CGO audio decode/write (bytes).
	// Used in: cgo_audio.go for native audio processing buffer allocation
	// Impact: Must accommodate maximum audio frame size to prevent buffer overruns.
	// Default 4096 bytes handles maximum audio frame size with safety margin.
	MaxDecodeWriteBuffer int

	// IPC Configuration - Inter-Process Communication settings for audio components
	// Used in: ipc.go for configuring audio process communication
	// Impact: Controls IPC reliability, performance, and protocol compliance

	// MagicNumber defines magic number for IPC message validation.
	// Used in: ipc.go for message header validation and protocol compliance
	// Impact: Must match expected value to prevent protocol errors.
	// Default 0xDEADBEEF provides distinctive pattern for message validation.
	MagicNumber uint32

	// MaxFrameSize defines maximum frame size for IPC messages (bytes).
	// Used in: ipc.go for message size validation and buffer allocation
	// Impact: Must accommodate largest expected audio frame to prevent truncation.
	// Default 4096 bytes handles typical audio frames with safety margin.
	MaxFrameSize int

	// WriteTimeout defines timeout for IPC write operations.
	// Used in: ipc.go for preventing blocking on slow IPC operations
	// Impact: Shorter timeouts improve responsiveness but may cause message drops.
	// Default 5 seconds allows for system load while preventing indefinite blocking.
	WriteTimeout time.Duration

	// MaxDroppedFrames defines maximum consecutive dropped frames before error.
	// Used in: ipc.go for IPC quality monitoring
	// Impact: Higher values tolerate more IPC issues but may mask problems.
	// Default 10 frames allows brief interruptions while detecting serious issues.
	MaxDroppedFrames int

	// HeaderSize defines size of IPC message headers (bytes).
	// Used in: ipc.go for message parsing and buffer allocation
	// Impact: Must match actual header size to prevent parsing errors.
	// Default 8 bytes matches current IPC message header format.
	HeaderSize int

	// Monitoring and Metrics - Configuration for audio performance monitoring
	// Used in: metrics.go, latency_monitor.go for performance tracking
	// Impact: Controls monitoring accuracy, overhead, and data retention

	// MetricsUpdateInterval defines frequency of metrics collection and reporting.
	// Used in: metrics.go for periodic metrics updates
	// Impact: Shorter intervals provide more accurate monitoring but increase overhead.
	// Default 1000ms (1 second) provides good balance between accuracy and performance.
	MetricsUpdateInterval time.Duration

	// EMAAlpha defines smoothing factor for Exponential Moving Average calculations.
	// Used in: metrics.go for smoothing performance metrics
	// Impact: Higher values respond faster to changes but are more sensitive to noise.
	// Default 0.1 provides good smoothing while maintaining responsiveness.
	EMAAlpha float64

	// WarmupSamples defines number of samples to collect before reporting metrics.
	// Used in: metrics.go for avoiding inaccurate initial measurements
	// Impact: More samples improve initial accuracy but delay metric availability.
	// Default 10 samples provides good initial accuracy without excessive delay.
	WarmupSamples int

	// LogThrottleInterval defines minimum interval between similar log messages.
	// Used in: Various components for preventing log spam
	// Impact: Longer intervals reduce log volume but may miss important events.
	// Default 5 seconds prevents log flooding while maintaining visibility.
	LogThrottleInterval time.Duration

	// MetricsChannelBuffer defines buffer size for metrics data channels.
	// Used in: metrics.go for metrics data collection pipelines
	// Impact: Larger buffers reduce blocking but increase memory usage and latency.
	// Default 100 metrics provides good balance for metrics collection.
	MetricsChannelBuffer int

	// LatencyHistorySize defines number of latency measurements to retain.
	// Used in: latency_monitor.go for latency trend analysis
	// Impact: More history improves trend analysis but increases memory usage.
	// Default 100 measurements provides good history for analysis.
	LatencyHistorySize int

	// Process Monitoring Constants - System resource monitoring configuration
	// Used in: process_monitor.go for monitoring CPU, memory, and system resources
	// Impact: Controls resource monitoring accuracy and system compatibility

	// MaxCPUPercent defines maximum valid CPU percentage value.
	// Used in: process_monitor.go for CPU usage validation
	// Impact: Values above this are considered invalid and filtered out.
	// Default 100.0 represents 100% CPU usage as maximum valid value.
	MaxCPUPercent float64

	// MinCPUPercent defines minimum valid CPU percentage value.
	// Used in: process_monitor.go for CPU usage validation
	// Impact: Values below this are considered noise and filtered out.
	// Default 0.01 (0.01%) filters out measurement noise while preserving low usage.
	MinCPUPercent float64

	// DefaultClockTicks defines default system clock ticks per second.
	// Used in: process_monitor.go for CPU time calculations on embedded systems
	// Impact: Must match system configuration for accurate CPU measurements.
	// Default 250.0 matches typical embedded ARM system configuration.
	DefaultClockTicks float64

	// DefaultMemoryGB defines default system memory size in gigabytes.
	// Used in: process_monitor.go for memory percentage calculations
	// Impact: Should match actual system memory for accurate percentage calculations.
	// Default 8 GB represents typical JetKVM system memory configuration.
	DefaultMemoryGB int

	// MaxWarmupSamples defines maximum number of warmup samples for monitoring.
	// Used in: process_monitor.go for initial measurement stabilization
	// Impact: More samples improve initial accuracy but delay monitoring start.
	// Default 3 samples provides quick stabilization without excessive delay.
	MaxWarmupSamples int

	// WarmupCPUSamples defines number of CPU samples for warmup period.
	// Used in: process_monitor.go for CPU measurement stabilization
	// Impact: More samples improve CPU measurement accuracy during startup.
	// Default 2 samples provides basic CPU measurement stabilization.
	WarmupCPUSamples int

	// LogThrottleIntervalSec defines log throttle interval in seconds.
	// Used in: process_monitor.go for controlling monitoring log frequency
	// Impact: Longer intervals reduce log volume but may miss monitoring events.
	// Default 10 seconds provides reasonable monitoring log frequency.
	LogThrottleIntervalSec int

	// MinValidClockTicks defines minimum valid system clock ticks value.
	// Used in: process_monitor.go for system clock validation
	// Impact: Values below this indicate system configuration issues.
	// Default 50 ticks represents minimum reasonable system clock configuration.
	MinValidClockTicks int

	// MaxValidClockTicks defines maximum valid system clock ticks value.
	// Used in: process_monitor.go for system clock validation
	// Impact: Values above this indicate system configuration issues.
	// Default 1000 ticks represents maximum reasonable system clock configuration.
	MaxValidClockTicks int

	// Performance Tuning - Thresholds for adaptive audio quality and resource management
	// Used in: adaptive_optimizer.go, quality_manager.go for performance optimization
	// Impact: Controls when audio quality adjustments are triggered based on system load

	// CPUThresholdLow defines CPU usage threshold for low system load.
	// Used in: adaptive_optimizer.go for triggering quality improvements
	// Impact: Below this threshold, audio quality can be increased safely.
	// Default 20% allows quality improvements when system has spare capacity.
	CPUThresholdLow float64

	// CPUThresholdMedium defines CPU usage threshold for medium system load.
	// Used in: adaptive_optimizer.go for maintaining current quality
	// Impact: Between low and medium thresholds, quality remains stable.
	// Default 60% represents balanced system load where quality should be maintained.
	CPUThresholdMedium float64

	// CPUThresholdHigh defines CPU usage threshold for high system load.
	// Used in: adaptive_optimizer.go for triggering quality reductions
	// Impact: Above this threshold, audio quality is reduced to preserve performance.
	// Default 75% prevents system overload by reducing audio processing demands.
	CPUThresholdHigh float64

	// MemoryThresholdLow defines memory usage threshold for low memory pressure.
	// Used in: adaptive_optimizer.go for memory-based quality decisions
	// Impact: Below this threshold, memory-intensive audio features can be enabled.
	// Default 30% allows enhanced features when memory is abundant.
	MemoryThresholdLow float64

	// MemoryThresholdMed defines memory usage threshold for medium memory pressure.
	// Used in: adaptive_optimizer.go for balanced memory management
	// Impact: Between low and medium thresholds, memory usage is monitored closely.
	// Default 60% represents moderate memory pressure requiring careful management.
	MemoryThresholdMed float64

	// MemoryThresholdHigh defines memory usage threshold for high memory pressure.
	// Used in: adaptive_optimizer.go for aggressive memory conservation
	// Impact: Above this threshold, memory usage is minimized by reducing quality.
	// Default 80% triggers aggressive memory conservation to prevent system issues.
	MemoryThresholdHigh float64

	// LatencyThresholdLow defines acceptable latency for high-quality audio.
	// Used in: adaptive_optimizer.go for latency-based quality decisions
	// Impact: Below this threshold, audio quality can be maximized.
	// Default 20ms represents excellent latency allowing maximum quality.
	LatencyThresholdLow time.Duration

	// LatencyThresholdHigh defines maximum acceptable latency before quality reduction.
	// Used in: adaptive_optimizer.go for preventing excessive audio delay
	// Impact: Above this threshold, quality is reduced to improve latency.
	// Default 50ms represents maximum acceptable latency for real-time audio.
	LatencyThresholdHigh time.Duration

	// CPUFactor defines weighting factor for CPU usage in performance calculations.
	// Used in: adaptive_optimizer.go for balancing CPU impact in optimization decisions
	// Impact: Higher values make CPU usage more influential in performance tuning.
	// Default 0.5 provides balanced CPU consideration in optimization algorithms.
	CPUFactor float64

	// MemoryFactor defines weighting factor for memory usage in performance calculations.
	// Used in: adaptive_optimizer.go for balancing memory impact in optimization decisions
	// Impact: Higher values make memory usage more influential in performance tuning.
	// Default 0.3 provides moderate memory consideration in optimization algorithms.
	MemoryFactor float64

	// LatencyFactor defines weighting factor for latency in performance calculations.
	// Used in: adaptive_optimizer.go for balancing latency impact in optimization decisions
	// Impact: Higher values make latency more influential in performance tuning.
	// Default 0.2 provides latency consideration while prioritizing CPU and memory.
	LatencyFactor float64

	// InputSizeThreshold defines threshold for input buffer size optimization (bytes).
	// Used in: adaptive_buffer.go for determining when to resize input buffers
	// Impact: Lower values trigger more frequent resizing, higher values reduce overhead.
	// Default 1024 bytes provides good balance for typical audio input scenarios.
	InputSizeThreshold int

	// OutputSizeThreshold defines threshold for output buffer size optimization (bytes).
	// Used in: adaptive_buffer.go for determining when to resize output buffers
	// Impact: Lower values trigger more frequent resizing, higher values reduce overhead.
	// Default 2048 bytes accommodates larger output buffers typical in audio processing.
	OutputSizeThreshold int

	// TargetLevel defines target performance level for optimization algorithms.
	// Used in: adaptive_optimizer.go for setting optimization goals
	// Impact: Higher values aim for better performance but may increase resource usage.
	// Default 0.8 (80%) provides good performance while maintaining system stability.
	TargetLevel float64

	// Adaptive Buffer Configuration - Controls dynamic buffer sizing for optimal performance
	// Used in: adaptive_buffer.go for dynamic buffer management
	// Impact: Controls buffer size adaptation based on system load and latency

	// AdaptiveMinBufferSize defines minimum buffer size in frames for adaptive buffering.
	// Used in: adaptive_buffer.go DefaultAdaptiveBufferConfig()
	// Impact: Lower values reduce latency but may cause underruns under high load.
	// Default 3 frames provides stability while maintaining low latency.
	AdaptiveMinBufferSize int

	// AdaptiveMaxBufferSize defines maximum buffer size in frames for adaptive buffering.
	// Used in: adaptive_buffer.go DefaultAdaptiveBufferConfig()
	// Impact: Higher values handle load spikes but increase maximum latency.
	// Default 20 frames accommodates high load scenarios without excessive latency.
	AdaptiveMaxBufferSize int

	// AdaptiveDefaultBufferSize defines default buffer size in frames for adaptive buffering.
	// Used in: adaptive_buffer.go DefaultAdaptiveBufferConfig()
	// Impact: Starting point for buffer adaptation, affects initial latency.
	// Default 6 frames balances initial latency with adaptation headroom.
	AdaptiveDefaultBufferSize int

	// Priority Scheduling - Real-time priority configuration for audio processing
	// Used in: priority_scheduler.go for setting process and thread priorities
	// Impact: Controls audio processing priority relative to other system processes

	// AudioHighPriority defines highest real-time priority for critical audio processing.
	// Used in: priority_scheduler.go for time-critical audio operations
	// Impact: Ensures audio processing gets CPU time even under high system load.
	// Default 90 provides high priority while leaving room for system-critical tasks.
	AudioHighPriority int

	// AudioMediumPriority defines medium real-time priority for standard audio processing.
	// Used in: priority_scheduler.go for normal audio operations
	// Impact: Balances audio performance with system responsiveness.
	// Default 70 provides good audio performance without monopolizing CPU.
	AudioMediumPriority int

	// AudioLowPriority defines low real-time priority for background audio tasks.
	// Used in: priority_scheduler.go for non-critical audio operations
	// Impact: Allows audio processing while yielding to higher priority tasks.
	// Default 50 provides background processing capability.
	AudioLowPriority int

	// NormalPriority defines standard system priority for non-real-time tasks.
	// Used in: priority_scheduler.go for utility and monitoring tasks
	// Impact: Standard priority level for non-time-critical operations.
	// Default 0 uses standard Linux process priority.
	NormalPriority int

	// NiceValue defines process nice value for CPU scheduling priority.
	// Used in: priority_scheduler.go for adjusting process scheduling priority
	// Impact: Lower values increase priority, higher values decrease priority.
	// Default -10 provides elevated priority for audio processes.
	NiceValue int

	// Error Handling - Configuration for error recovery and retry mechanisms
	// Used in: error_handler.go, retry_manager.go for robust error handling
	// Impact: Controls system resilience and recovery behavior

	// MaxRetries defines maximum number of retry attempts for failed operations.
	// Used in: retry_manager.go for limiting retry attempts
	// Impact: More retries improve success rate but may delay error reporting.
	// Default 3 retries provides good balance between persistence and responsiveness.
	MaxRetries int

	// RetryDelay defines initial delay between retry attempts.
	// Used in: retry_manager.go for spacing retry attempts
	// Impact: Longer delays reduce system load but slow recovery.
	// Default 100ms provides quick retries while avoiding excessive load.
	RetryDelay time.Duration

	// MaxRetryDelay defines maximum delay between retry attempts.
	// Used in: retry_manager.go for capping exponential backoff
	// Impact: Prevents excessively long delays while maintaining backoff benefits.
	// Default 5 seconds caps retry delays at reasonable maximum.
	MaxRetryDelay time.Duration

	// BackoffMultiplier defines multiplier for exponential backoff retry delays.
	// Used in: retry_manager.go for calculating progressive retry delays
	// Impact: Higher values increase delays more aggressively between retries.
	// Default 2.0 doubles delay each retry, providing standard exponential backoff.
	BackoffMultiplier float64

	// ErrorChannelSize defines buffer size for error reporting channels.
	// Used in: error_handler.go for error message queuing
	// Impact: Larger buffers prevent error loss but increase memory usage.
	// Default 50 errors provides adequate buffering for error bursts.
	ErrorChannelSize int

	// MaxConsecutiveErrors defines maximum consecutive errors before escalation.
	// Used in: error_handler.go for error threshold monitoring
	// Impact: Lower values trigger faster escalation, higher values tolerate more errors.
	// Default 5 errors provides tolerance for transient issues while detecting problems.
	MaxConsecutiveErrors int

	// MaxRetryAttempts defines maximum total retry attempts across all operations.
	// Used in: retry_manager.go for global retry limit enforcement
	// Impact: Higher values improve success rate but may delay failure detection.
	// Default 10 attempts provides comprehensive retry coverage.
	MaxRetryAttempts int

	// Timing Constants - Core timing configuration for audio processing operations
	// Used in: Various components for timing control and synchronization
	// Impact: Controls timing behavior, responsiveness, and system stability

	// DefaultSleepDuration defines standard sleep duration for general operations.
	// Used in: Various components for standard timing delays
	// Impact: Balances CPU usage with responsiveness in polling operations.
	// Default 100ms provides good balance for most timing scenarios.
	DefaultSleepDuration time.Duration // 100ms

	// ShortSleepDuration defines brief sleep duration for time-sensitive operations.
	// Used in: Real-time components for minimal delays
	// Impact: Reduces latency but increases CPU usage in tight loops.
	// Default 10ms provides minimal delay for responsive operations.
	ShortSleepDuration time.Duration // 10ms

	// LongSleepDuration defines extended sleep duration for background operations.
	// Used in: Background tasks and cleanup operations
	// Impact: Reduces CPU usage but increases response time for background tasks.
	// Default 200ms provides efficient background operation timing.
	LongSleepDuration time.Duration // 200ms

	// DefaultTickerInterval defines standard ticker interval for periodic operations.
	// Used in: Periodic monitoring and maintenance tasks
	// Impact: Controls frequency of periodic operations and resource usage.
	// Default 100ms provides good balance for monitoring tasks.
	DefaultTickerInterval time.Duration // 100ms

	// BufferUpdateInterval defines frequency of buffer status updates.
	// Used in: buffer_pool.go and adaptive_buffer.go for buffer management
	// Impact: More frequent updates improve responsiveness but increase overhead.
	// Default 500ms provides adequate buffer monitoring without excessive overhead.
	BufferUpdateInterval time.Duration // 500ms

	// StatsUpdateInterval defines frequency of statistics collection and reporting.
	// Used in: metrics.go for performance statistics updates
	// Impact: More frequent updates provide better monitoring but increase overhead.
	// Default 5s provides comprehensive statistics without performance impact.
	StatsUpdateInterval time.Duration // 5s

	// SupervisorTimeout defines timeout for supervisor process operations.
	// Used in: supervisor.go for process monitoring and control
	// Impact: Shorter timeouts improve responsiveness but may cause false timeouts.
	// Default 10s provides adequate time for supervisor operations.
	SupervisorTimeout time.Duration // 10s

	// InputSupervisorTimeout defines timeout for input supervisor operations.
	// Used in: input_supervisor.go for input process monitoring
	// Impact: Shorter timeouts improve input responsiveness but may cause false timeouts.
	// Default 5s provides responsive input monitoring.
	InputSupervisorTimeout time.Duration // 5s

	// OutputSupervisorTimeout defines timeout for output supervisor operations.
	// Used in: supervisor.go for output process monitoring
	// Impact: Shorter timeouts improve output responsiveness but may cause false timeouts.
	// Default 5s provides responsive output monitoring.
	OutputSupervisorTimeout time.Duration // 5s

	// ShortTimeout defines brief timeout for time-critical operations.
	// Used in: Real-time audio processing for minimal timeout scenarios
	// Impact: Very short timeouts ensure responsiveness but may cause premature failures.
	// Default 5ms provides minimal timeout for critical operations.
	ShortTimeout time.Duration // 5ms

	// MediumTimeout defines moderate timeout for standard operations.
	// Used in: Standard audio processing operations
	// Impact: Balances responsiveness with operation completion time.
	// Default 50ms provides good balance for most audio operations.
	MediumTimeout time.Duration // 50ms

	// BatchProcessingDelay defines delay between batch processing operations.
	// Used in: batch_audio.go for controlling batch processing timing
	// Impact: Shorter delays improve throughput but increase CPU usage.
	// Default 10ms provides efficient batch processing timing.
	BatchProcessingDelay time.Duration // 10ms

	// AdaptiveOptimizerStability defines stability period for adaptive optimization.
	// Used in: adaptive_optimizer.go for optimization stability control
	// Impact: Longer periods provide more stable optimization but slower adaptation.
	// Default 10s provides good balance between stability and adaptability.
	AdaptiveOptimizerStability time.Duration // 10s

	// MaxLatencyTarget defines maximum acceptable latency target.
	// Used in: latency_monitor.go for latency threshold monitoring
	// Impact: Lower values enforce stricter latency requirements.
	// Default 50ms provides good real-time audio latency target.
	MaxLatencyTarget time.Duration // 50ms

	// LatencyMonitorTarget defines target latency for monitoring and optimization.
	// Used in: latency_monitor.go for latency optimization goals
	// Impact: Lower targets improve audio responsiveness but may increase system load.
	// Default 50ms provides excellent real-time audio performance target.
	LatencyMonitorTarget time.Duration // 50ms

	// Adaptive Buffer Configuration - Thresholds for dynamic buffer adaptation
	// Used in: adaptive_buffer.go for system load-based buffer sizing
	// Impact: Controls when buffer sizes are adjusted based on system conditions

	// LowCPUThreshold defines CPU usage threshold for buffer size reduction.
	// Used in: adaptive_buffer.go for detecting low CPU load conditions
	// Impact: Below this threshold, buffers may be reduced to minimize latency.
	// Default 20% allows buffer optimization during low system load.
	LowCPUThreshold float64 // 20% CPU threshold

	// HighCPUThreshold defines CPU usage threshold for buffer size increase.
	// Used in: adaptive_buffer.go for detecting high CPU load conditions
	// Impact: Above this threshold, buffers are increased to prevent underruns.
	// Default 60% provides early detection of CPU pressure for buffer adjustment.
	HighCPUThreshold float64 // 60% CPU threshold

	// LowMemoryThreshold defines memory usage threshold for buffer optimization.
	// Used in: adaptive_buffer.go for memory-conscious buffer management
	// Impact: Above this threshold, buffer sizes may be reduced to save memory.
	// Default 50% provides early memory pressure detection.
	LowMemoryThreshold float64 // 50% memory threshold

	// HighMemoryThreshold defines memory usage threshold for aggressive optimization.
	// Used in: adaptive_buffer.go for high memory pressure scenarios
	// Impact: Above this threshold, aggressive buffer reduction is applied.
	// Default 75% triggers aggressive memory conservation measures.
	HighMemoryThreshold float64 // 75% memory threshold

	// TargetLatency defines target latency for adaptive buffer optimization.
	// Used in: adaptive_buffer.go for latency-based buffer sizing
	// Impact: Lower targets reduce buffer sizes, higher targets increase stability.
	// Default 20ms provides excellent real-time performance target.
	TargetLatency time.Duration // 20ms target latency

	// Adaptive Optimizer Configuration - Settings for performance optimization
	// Used in: adaptive_optimizer.go for system performance optimization
	// Impact: Controls optimization behavior and stability

	// CooldownPeriod defines minimum time between optimization adjustments.
	// Used in: adaptive_optimizer.go for preventing optimization oscillation
	// Impact: Longer periods provide more stable optimization but slower adaptation.
	// Default 30s prevents rapid optimization changes that could destabilize system.
	CooldownPeriod time.Duration // 30s cooldown period

	// RollbackThreshold defines latency threshold for optimization rollback.
	// Used in: adaptive_optimizer.go for detecting failed optimizations
	// Impact: Lower thresholds trigger faster rollback but may be too sensitive.
	// Default 300ms provides clear indication of optimization failure.
	RollbackThreshold time.Duration // 300ms rollback threshold

	// LatencyTarget defines target latency for optimization goals.
	// Used in: adaptive_optimizer.go for optimization target setting
	// Impact: Lower targets improve responsiveness but may increase system load.
	// Default 50ms provides good balance between performance and stability.
	LatencyTarget time.Duration // 50ms latency target

	// Latency Monitor Configuration - Settings for latency monitoring and analysis
	// Used in: latency_monitor.go for latency tracking and alerting
	// Impact: Controls latency monitoring sensitivity and thresholds

	// MaxLatencyThreshold defines maximum acceptable latency before alerts.
	// Used in: latency_monitor.go for latency violation detection
	// Impact: Lower values provide stricter latency enforcement.
	// Default 200ms defines clear boundary for unacceptable latency.
	MaxLatencyThreshold time.Duration // 200ms max latency

	// JitterThreshold defines maximum acceptable latency variation.
	// Used in: latency_monitor.go for jitter detection and monitoring
	// Impact: Lower values detect smaller latency variations.
	// Default 20ms provides good jitter detection for audio quality.
	JitterThreshold time.Duration // 20ms jitter threshold

	// LatencyOptimizationInterval defines interval for latency optimization cycles.
	// Used in: latency_monitor.go for optimization timing control
	// Impact: Controls frequency of latency optimization adjustments.
	// Default 5s provides balanced optimization without excessive overhead.
	LatencyOptimizationInterval time.Duration // 5s optimization interval

	// LatencyAdaptiveThreshold defines threshold for adaptive latency adjustments.
	// Used in: latency_monitor.go for adaptive optimization decisions
	// Impact: Controls sensitivity of adaptive latency optimization.
	// Default 0.8 (80%) provides good balance between stability and adaptation.
	LatencyAdaptiveThreshold float64 // 0.8 adaptive threshold

	// Microphone Contention Configuration - Settings for microphone access management
	// Used in: mic_contention.go for managing concurrent microphone access
	// Impact: Controls microphone resource sharing and timeout behavior

	// MicContentionTimeout defines timeout for microphone access contention.
	// Used in: mic_contention.go for microphone access arbitration
	// Impact: Shorter timeouts improve responsiveness but may cause access failures.
	// Default 200ms provides reasonable wait time for microphone access.
	MicContentionTimeout time.Duration // 200ms contention timeout

	// Priority Scheduler Configuration - Settings for process priority management
	// Used in: priority_scheduler.go for system priority control
	// Impact: Controls valid range for process priority adjustments

	// MinNiceValue defines minimum (highest priority) nice value.
	// Used in: priority_scheduler.go for priority validation
	// Impact: Lower values allow higher priority but may affect system stability.
	// Default -20 provides maximum priority elevation capability.
	MinNiceValue int // -20 minimum nice value

	// MaxNiceValue defines maximum (lowest priority) nice value.
	// Used in: priority_scheduler.go for priority validation
	// Impact: Higher values allow lower priority for background tasks.
	// Default 19 provides maximum priority reduction capability.
	MaxNiceValue int // 19 maximum nice value

	// Buffer Pool Configuration - Settings for memory pool preallocation
	// Used in: buffer_pool.go for memory pool management
	// Impact: Controls memory preallocation strategy and efficiency

	// PreallocPercentage defines percentage of buffers to preallocate.
	// Used in: buffer_pool.go for initial memory pool sizing
	// Impact: Higher values reduce allocation overhead but increase memory usage.
	// Default 20% provides good balance between performance and memory efficiency.
	PreallocPercentage int // 20% preallocation percentage

	// InputPreallocPercentage defines percentage of input buffers to preallocate.
	// Used in: buffer_pool.go for input-specific memory pool sizing
	// Impact: Higher values improve input performance but increase memory usage.
	// Default 30% provides enhanced input performance with reasonable memory usage.
	InputPreallocPercentage int // 30% input preallocation percentage

	// Exponential Moving Average Configuration - Settings for statistical smoothing
	// Used in: metrics.go and various monitoring components
	// Impact: Controls smoothing behavior for performance metrics

	// HistoricalWeight defines weight given to historical data in EMA calculations.
	// Used in: metrics.go for exponential moving average calculations
	// Impact: Higher values provide more stable metrics but slower response to changes.
	// Default 70% provides good stability while maintaining responsiveness.
	HistoricalWeight float64 // 70% historical weight

	// CurrentWeight defines weight given to current data in EMA calculations.
	// Used in: metrics.go for exponential moving average calculations
	// Impact: Higher values provide faster response but less stability.
	// Default 30% complements historical weight for balanced EMA calculation.
	CurrentWeight float64 // 30% current weight

	// Sleep and Backoff Configuration - Settings for timing and retry behavior
	// Used in: Various components for timing control and retry logic
	// Impact: Controls system timing behavior and retry strategies

	// CGOSleepMicroseconds defines sleep duration for CGO operations.
	// Used in: cgo_audio.go for CGO operation timing
	// Impact: Longer sleeps reduce CPU usage but may increase latency.
	// Default 50000 microseconds (50ms) provides good balance for CGO operations.
	CGOSleepMicroseconds int // 50000 microseconds (50ms)

	// BackoffStart defines initial backoff duration for retry operations.
	// Used in: retry_manager.go for exponential backoff initialization
	// Impact: Longer initial backoff reduces immediate retry pressure.
	// Default 50ms provides reasonable initial retry delay.
	BackoffStart time.Duration // 50ms initial backoff

	// Protocol Magic Numbers - Unique identifiers for IPC message validation
	// Used in: ipc.go, input_ipc.go for message protocol validation
	// Impact: Must match expected values to ensure proper message routing

	// InputMagicNumber defines magic number for input IPC messages.
	// Used in: input_ipc.go for input message validation
	// Impact: Must match expected value to prevent input protocol errors.
	// Default 0x4A4B4D49 "JKMI" (JetKVM Microphone Input) provides distinctive input identifier.
	InputMagicNumber uint32

	// OutputMagicNumber defines magic number for output IPC messages.
	// Used in: ipc.go for output message validation
	// Impact: Must match expected value to prevent output protocol errors.
	// Default 0x4A4B4F55 "JKOU" (JetKVM Output) provides distinctive output identifier.
	OutputMagicNumber uint32

	// Calculation Constants - Mathematical constants for audio processing calculations
	// Used in: Various components for mathematical operations and scaling
	// Impact: Controls precision and behavior of audio processing algorithms

	// PercentageMultiplier defines multiplier for percentage calculations.
	// Used in: metrics.go, process_monitor.go for percentage conversions
	// Impact: Must be 100.0 for accurate percentage calculations.
	// Default 100.0 provides standard percentage calculation base.
	PercentageMultiplier float64

	// AveragingWeight defines weight for weighted averaging calculations.
	// Used in: metrics.go for exponential moving averages
	// Impact: Higher values emphasize historical data more heavily.
	// Default 0.7 provides good balance between stability and responsiveness.
	AveragingWeight float64

	// ScalingFactor defines general scaling factor for calculations.
	// Used in: adaptive_buffer.go for buffer size scaling
	// Impact: Higher values increase scaling aggressiveness.
	// Default 1.5 provides moderate scaling for buffer adjustments.
	ScalingFactor float64

	// SmoothingFactor defines smoothing factor for adaptive buffer calculations.
	// Used in: adaptive_buffer.go for buffer size smoothing
	// Impact: Higher values provide more aggressive smoothing.
	// Default 0.3 provides good smoothing without excessive dampening.
	SmoothingFactor float64

	// CPUMemoryWeight defines weight for CPU factor in combined calculations.
	// Used in: adaptive_optimizer.go for balancing CPU vs memory considerations
	// Impact: Higher values prioritize CPU optimization over memory optimization.
	// Default 0.5 provides equal weighting between CPU and memory factors.
	CPUMemoryWeight float64

	// MemoryWeight defines weight for memory factor in combined calculations.
	// Used in: adaptive_optimizer.go for memory impact weighting
	// Impact: Higher values make memory usage more influential in decisions.
	// Default 0.3 provides moderate memory consideration in optimization.
	MemoryWeight float64

	// LatencyWeight defines weight for latency factor in combined calculations.
	// Used in: adaptive_optimizer.go for latency impact weighting
	// Impact: Higher values prioritize latency optimization over resource usage.
	// Default 0.2 provides latency consideration while prioritizing resources.
	LatencyWeight float64

	// PoolGrowthMultiplier defines multiplier for pool size growth.
	// Used in: buffer_pool.go for pool expansion calculations
	// Impact: Higher values cause more aggressive pool growth.
	// Default 2 provides standard doubling growth pattern.
	PoolGrowthMultiplier int

	// LatencyScalingFactor defines scaling factor for latency ratio calculations.
	// Used in: latency_monitor.go for latency scaling operations
	// Impact: Higher values amplify latency differences in calculations.
	// Default 2.0 provides moderate latency scaling for monitoring.
	LatencyScalingFactor float64

	// OptimizerAggressiveness defines aggressiveness level for optimization algorithms.
	// Used in: adaptive_optimizer.go for optimization behavior control
	// Impact: Higher values cause more aggressive optimization changes.
	// Default 0.7 provides assertive optimization while maintaining stability.
	OptimizerAggressiveness float64

	// CGO Audio Processing Constants - Low-level CGO audio processing configuration
	// Used in: cgo_audio.go for native audio processing operations
	// Impact: Controls CGO audio processing timing and buffer management

	// CGOUsleepMicroseconds defines sleep duration for CGO usleep calls.
	// Used in: cgo_audio.go for CGO operation timing control
	// Impact: Controls timing precision in native audio processing.
	// Default 1000 microseconds (1ms) provides good balance for CGO timing.
	CGOUsleepMicroseconds int

	// CGOPCMBufferSize defines PCM buffer size for CGO audio processing.
	// Used in: cgo_audio.go for native PCM buffer allocation
	// Impact: Must accommodate maximum expected PCM frame size.
	// Default 1920 samples handles maximum 2-channel 960-sample frames.
	CGOPCMBufferSize int

	// CGONanosecondsPerSecond defines nanoseconds per second for time conversions.
	// Used in: cgo_audio.go for time unit conversions in native code
	// Impact: Must be accurate for precise timing calculations.
	// Default 1000000000.0 provides standard nanosecond conversion factor.
	CGONanosecondsPerSecond float64

	// Frontend Constants - Configuration for frontend audio interface
	// Used in: Frontend components for user interface audio controls
	// Impact: Controls frontend audio behavior, timing, and user experience

	// FrontendOperationDebounceMS defines debounce time for frontend operations.
	// Used in: Frontend components for preventing rapid operation triggers
	// Impact: Longer values reduce operation frequency but may feel less responsive.
	// Default 1000ms prevents accidental rapid operations while maintaining usability.
	FrontendOperationDebounceMS int

	// FrontendSyncDebounceMS defines debounce time for sync operations.
	// Used in: Frontend components for sync operation rate limiting
	// Impact: Controls frequency of sync operations to prevent overload.
	// Default 1000ms provides reasonable sync operation spacing.
	FrontendSyncDebounceMS int

	// FrontendSampleRate defines sample rate for frontend audio processing.
	// Used in: Frontend audio components for audio parameter configuration
	// Impact: Must match backend sample rate for proper audio processing.
	// Default 48000Hz provides high-quality audio for frontend display.
	FrontendSampleRate int

	// FrontendRetryDelayMS defines delay between frontend retry attempts.
	// Used in: Frontend components for retry operation timing
	// Impact: Longer delays reduce server load but slow error recovery.
	// Default 500ms provides reasonable retry timing for frontend operations.
	FrontendRetryDelayMS int

	// FrontendShortDelayMS defines short delay for frontend operations.
	// Used in: Frontend components for brief operation delays
	// Impact: Controls timing for quick frontend operations.
	// Default 200ms provides brief delay for responsive operations.
	FrontendShortDelayMS int

	// FrontendLongDelayMS defines long delay for frontend operations.
	// Used in: Frontend components for extended operation delays
	// Impact: Controls timing for slower frontend operations.
	// Default 300ms provides extended delay for complex operations.
	FrontendLongDelayMS int

	// FrontendSyncDelayMS defines delay for frontend sync operations.
	// Used in: Frontend components for sync operation timing
	// Impact: Controls frequency of frontend synchronization.
	// Default 500ms provides good balance for sync operations.
	FrontendSyncDelayMS int

	// FrontendMaxRetryAttempts defines maximum retry attempts for frontend operations.
	// Used in: Frontend components for retry limit enforcement
	// Impact: More attempts improve success rate but may delay error reporting.
	// Default 3 attempts provides good balance between persistence and responsiveness.
	FrontendMaxRetryAttempts int

	// FrontendAudioLevelUpdateMS defines audio level update interval.
	// Used in: Frontend components for audio level meter updates
	// Impact: Shorter intervals provide smoother meters but increase CPU usage.
	// Default 100ms provides smooth audio level visualization.
	FrontendAudioLevelUpdateMS int

	// FrontendFFTSize defines FFT size for frontend audio analysis.
	// Used in: Frontend components for audio spectrum analysis
	// Impact: Larger sizes provide better frequency resolution but increase CPU usage.
	// Default 256 provides good balance for audio visualization.
	FrontendFFTSize int

	// FrontendAudioLevelMax defines maximum audio level value.
	// Used in: Frontend components for audio level scaling
	// Impact: Controls maximum value for audio level displays.
	// Default 100 provides standard percentage-based audio level scale.
	FrontendAudioLevelMax int

	// FrontendReconnectIntervalMS defines interval between reconnection attempts.
	// Used in: Frontend components for connection retry timing
	// Impact: Shorter intervals retry faster but may overload server.
	// Default 3000ms provides reasonable reconnection timing.
	FrontendReconnectIntervalMS int

	// FrontendSubscriptionDelayMS defines delay for subscription operations.
	// Used in: Frontend components for subscription timing
	// Impact: Controls timing for frontend event subscriptions.
	// Default 100ms provides quick subscription establishment.
	FrontendSubscriptionDelayMS int

	// FrontendDebugIntervalMS defines interval for frontend debug output.
	// Used in: Frontend components for debug information timing
	// Impact: Shorter intervals provide more debug info but increase overhead.
	// Default 5000ms provides periodic debug information without excessive output.
	FrontendDebugIntervalMS int

	// Process Monitor Constants - System resource monitoring configuration
	// Used in: process_monitor.go for system resource tracking
	// Impact: Controls process monitoring behavior and system compatibility

	// ProcessMonitorDefaultMemoryGB defines default memory size for fallback calculations.
	// Used in: process_monitor.go when system memory cannot be detected
	// Impact: Should approximate actual system memory for accurate calculations.
	// Default 4GB provides reasonable fallback for typical embedded systems.
	ProcessMonitorDefaultMemoryGB int

	// ProcessMonitorKBToBytes defines conversion factor from kilobytes to bytes.
	// Used in: process_monitor.go for memory unit conversions
	// Impact: Must be 1024 for accurate binary unit conversions.
	// Default 1024 provides standard binary conversion factor.
	ProcessMonitorKBToBytes int

	// ProcessMonitorDefaultClockHz defines default system clock frequency.
	// Used in: process_monitor.go for CPU time calculations on ARM systems
	// Impact: Should match actual system clock for accurate CPU measurements.
	// Default 250.0 Hz matches typical ARM embedded system configuration.
	ProcessMonitorDefaultClockHz float64

	// ProcessMonitorFallbackClockHz defines fallback clock frequency.
	// Used in: process_monitor.go when system clock cannot be detected
	// Impact: Provides fallback for CPU time calculations.
	// Default 1000.0 Hz provides reasonable fallback clock frequency.
	ProcessMonitorFallbackClockHz float64

	// ProcessMonitorTraditionalHz defines traditional system clock frequency.
	// Used in: process_monitor.go for legacy system compatibility
	// Impact: Supports older systems with traditional clock frequencies.
	// Default 100.0 Hz provides compatibility with traditional Unix systems.
	ProcessMonitorTraditionalHz float64

	// Batch Processing Constants - Configuration for audio batch processing
	// Used in: batch_audio.go for batch audio operation control
	// Impact: Controls batch processing efficiency and latency

	// BatchProcessorFramesPerBatch defines number of frames processed per batch.
	// Used in: batch_audio.go for batch size control
	// Impact: Larger batches improve efficiency but increase latency.
	// Default 4 frames provides good balance between efficiency and latency.
	BatchProcessorFramesPerBatch int

	// BatchProcessorTimeout defines timeout for batch processing operations.
	// Used in: batch_audio.go for batch operation timeout control
	// Impact: Shorter timeouts improve responsiveness but may cause timeouts.
	// Default 5ms provides quick batch processing with reasonable timeout.
	BatchProcessorTimeout time.Duration

	// Output Streaming Constants - Configuration for audio output streaming
	// Used in: output_streaming.go for output stream timing control
	// Impact: Controls output streaming frame rate and timing

	// OutputStreamingFrameIntervalMS defines interval between output frames.
	// Used in: output_streaming.go for output frame timing
	// Impact: Shorter intervals provide smoother output but increase CPU usage.
	// Default 20ms provides 50 FPS output rate for smooth audio streaming.
	OutputStreamingFrameIntervalMS int

	// IPC Constants - Inter-Process Communication configuration
	// Used in: ipc.go for IPC buffer management
	// Impact: Controls IPC buffer sizing and performance

	// IPCInitialBufferFrames defines initial buffer size for IPC operations.
	// Used in: ipc.go for initial IPC buffer allocation
	// Impact: Larger buffers reduce allocation overhead but increase memory usage.
	// Default 500 frames provides good initial buffer size for IPC operations.
	IPCInitialBufferFrames int

	// Event Constants - Configuration for event handling and timing
	// Used in: Event handling components for event processing control
	// Impact: Controls event processing timing and format

	// EventTimeoutSeconds defines timeout for event processing operations.
	// Used in: Event handling components for event timeout control
	// Impact: Shorter timeouts improve responsiveness but may cause event loss.
	// Default 2 seconds provides reasonable event processing timeout.
	EventTimeoutSeconds int

	// EventTimeFormatString defines time format string for event timestamps.
	// Used in: Event handling components for timestamp formatting
	// Impact: Must match expected format for proper event processing.
	// Default "2006-01-02T15:04:05.000Z" provides ISO 8601 format with milliseconds.
	EventTimeFormatString string

	// EventSubscriptionDelayMS defines delay for event subscription operations.
	// Used in: Event handling components for subscription timing
	// Impact: Controls timing for event subscription establishment.
	// Default 100ms provides quick event subscription setup.
	EventSubscriptionDelayMS int

	// Input Processing Constants - Configuration for audio input processing
	// Used in: Input processing components for input timing control
	// Impact: Controls input processing timing and thresholds

	// InputProcessingTimeoutMS defines timeout for input processing operations.
	// Used in: Input processing components for processing timeout control
	// Impact: Shorter timeouts improve responsiveness but may cause processing failures.
	// Default 10ms provides quick input processing with minimal timeout.
	InputProcessingTimeoutMS int

	// Adaptive Buffer Constants - Configuration for adaptive buffer calculations
	// Used in: adaptive_buffer.go for buffer adaptation calculations
	// Impact: Controls adaptive buffer scaling and calculations

	// AdaptiveBufferCPUMultiplier defines multiplier for CPU percentage calculations.
	// Used in: adaptive_buffer.go for CPU-based buffer adaptation
	// Impact: Controls scaling factor for CPU influence on buffer sizing.
	// Default 100 provides standard percentage scaling for CPU calculations.
	AdaptiveBufferCPUMultiplier int

	// AdaptiveBufferMemoryMultiplier defines multiplier for memory percentage calculations.
	// Used in: adaptive_buffer.go for memory-based buffer adaptation
	// Impact: Controls scaling factor for memory influence on buffer sizing.
	// Default 100 provides standard percentage scaling for memory calculations.
	AdaptiveBufferMemoryMultiplier int

	// Socket Names - Configuration for IPC socket file names
	// Used in: IPC communication for audio input/output
	// Impact: Controls socket file naming and IPC connection endpoints

	// InputSocketName defines the socket file name for audio input IPC.
	// Used in: input_ipc.go for microphone input communication
	// Impact: Must be unique to prevent conflicts with other audio sockets.
	// Default "audio_input.sock" provides clear identification for input socket.
	InputSocketName string

	// OutputSocketName defines the socket file name for audio output IPC.
	// Used in: ipc.go for audio output communication
	// Impact: Must be unique to prevent conflicts with other audio sockets.
	// Default "audio_output.sock" provides clear identification for output socket.
	OutputSocketName string

	// Component Names - Standardized component identifiers for logging
	// Used in: Logging and monitoring throughout audio system
	// Impact: Provides consistent component identification across logs

	// AudioInputComponentName defines component name for audio input logging.
	// Used in: input_ipc.go and related input processing components
	// Impact: Ensures consistent logging identification for input components.
	// Default "audio-input" provides clear component identification.
	AudioInputComponentName string

	// AudioOutputComponentName defines component name for audio output logging.
	// Used in: ipc.go and related output processing components
	// Impact: Ensures consistent logging identification for output components.
	// Default "audio-output" provides clear component identification.
	AudioOutputComponentName string

	// AudioServerComponentName defines component name for audio server logging.
	// Used in: supervisor.go and server management components
	// Impact: Ensures consistent logging identification for server components.
	// Default "audio-server" provides clear component identification.
	AudioServerComponentName string

	// AudioRelayComponentName defines component name for audio relay logging.
	// Used in: relay.go for audio relay operations
	// Impact: Ensures consistent logging identification for relay components.
	// Default "audio-relay" provides clear component identification.
	AudioRelayComponentName string

	// AudioEventsComponentName defines component name for audio events logging.
	// Used in: events.go for event broadcasting operations
	// Impact: Ensures consistent logging identification for event components.
	// Default "audio-events" provides clear component identification.
	AudioEventsComponentName string

	// Test Configuration - Constants for testing scenarios
	// Used in: Test files for consistent test configuration
	// Impact: Provides standardized test parameters and timeouts

	// TestSocketTimeout defines timeout for test socket operations.
	// Used in: integration_test.go for test socket communication
	// Impact: Prevents test hangs while allowing sufficient time for operations.
	// Default 100ms provides quick test execution with adequate timeout.
	TestSocketTimeout time.Duration

	// TestBufferSize defines buffer size for test operations.
	// Used in: test_utils.go for test buffer allocation
	// Impact: Provides adequate buffer space for test scenarios.
	// Default 4096 bytes matches production buffer sizes for realistic testing.
	TestBufferSize int

	// TestRetryDelay defines delay between test retry attempts.
	// Used in: Test files for retry logic in test scenarios
	// Impact: Provides reasonable delay for test retry operations.
	// Default 200ms allows sufficient time for test state changes.
	TestRetryDelay time.Duration

	// Latency Histogram Configuration - Constants for latency tracking
	// Used in: granular_metrics.go for latency distribution analysis
	// Impact: Controls granularity and accuracy of latency measurements

	// LatencyHistogramMaxSamples defines maximum samples for latency tracking.
	// Used in: granular_metrics.go for latency histogram management
	// Impact: Controls memory usage and accuracy of latency statistics.
	// Default 1000 samples provides good statistical accuracy with reasonable memory usage.
	LatencyHistogramMaxSamples int

	// LatencyPercentile50 defines 50th percentile calculation factor.
	// Used in: granular_metrics.go for median latency calculation
	// Impact: Must be 50 for accurate median calculation.
	// Default 50 provides standard median percentile calculation.
	LatencyPercentile50 int

	// LatencyPercentile95 defines 95th percentile calculation factor.
	// Used in: granular_metrics.go for high-percentile latency calculation
	// Impact: Must be 95 for accurate 95th percentile calculation.
	// Default 95 provides standard high-percentile calculation.
	LatencyPercentile95 int

	// LatencyPercentile99 defines 99th percentile calculation factor.
	// Used in: granular_metrics.go for extreme latency calculation
	// Impact: Must be 99 for accurate 99th percentile calculation.
	// Default 99 provides standard extreme percentile calculation.
	LatencyPercentile99 int

	// BufferPoolMaxOperations defines maximum operations to track for efficiency.
	// Used in: granular_metrics.go for buffer pool efficiency tracking
	// Impact: Controls memory usage and accuracy of efficiency statistics.
	// Default 1000 operations provides good balance of accuracy and memory usage.
	BufferPoolMaxOperations int

	// HitRateCalculationBase defines base value for hit rate percentage calculation.
	// Used in: granular_metrics.go for hit rate percentage calculation
	// Impact: Must be 100 for accurate percentage calculation.
	// Default 100 provides standard percentage calculation base.
	HitRateCalculationBase float64

	// Validation Constants - Configuration for input validation
	// Used in: validation.go for parameter validation
	// Impact: Controls validation thresholds and limits

	// MaxLatency defines maximum allowed latency for audio processing.
	// Used in: validation.go for latency validation
	// Impact: Controls maximum acceptable latency before optimization triggers.
	// Default 200ms provides reasonable upper bound for real-time audio.
	MaxLatency time.Duration

	// MinMetricsUpdateInterval defines minimum allowed metrics update interval.
	// Used in: validation.go for metrics interval validation
	// Impact: Prevents excessive metrics updates that could impact performance.
	// Default 100ms provides reasonable minimum update frequency.
	MinMetricsUpdateInterval time.Duration

	// MaxMetricsUpdateInterval defines maximum allowed metrics update interval.
	// Used in: validation.go for metrics interval validation
	// Impact: Ensures metrics are updated frequently enough for monitoring.
	// Default 30s provides reasonable maximum update interval.
	MaxMetricsUpdateInterval time.Duration

	// MinSampleRate defines minimum allowed audio sample rate.
	// Used in: validation.go for sample rate validation
	// Impact: Ensures sample rate is sufficient for audio quality.
	// Default 8000Hz provides minimum for voice communication.
	MinSampleRate int

	// MaxSampleRate defines maximum allowed audio sample rate.
	// Used in: validation.go for sample rate validation
	// Impact: Prevents excessive sample rates that could impact performance.
	// Default 192000Hz provides upper bound for high-quality audio.
	MaxSampleRate int

	// MaxChannels defines maximum allowed audio channels.
	// Used in: validation.go for channel count validation
	// Impact: Prevents excessive channel counts that could impact performance.
	// Default 8 channels provides reasonable upper bound for multi-channel audio.
	MaxChannels int

	// CGO Constants
	// Used in: cgo_audio.go for CGO operation limits and retry logic
	// Impact: Controls CGO retry behavior and backoff timing

	// CGOMaxBackoffMicroseconds defines maximum backoff time in microseconds for CGO operations.
	// Used in: safe_alsa_open for exponential backoff retry logic
	// Impact: Prevents excessive wait times while allowing device recovery.
	// Default 500000 microseconds (500ms) provides reasonable maximum wait time.
	CGOMaxBackoffMicroseconds int

	// CGOMaxAttempts defines maximum retry attempts for CGO operations.
	// Used in: safe_alsa_open for retry limit enforcement
	// Impact: Prevents infinite retry loops while allowing transient error recovery.
	// Default 5 attempts provides good balance between reliability and performance.
	CGOMaxAttempts int

	// Validation Frame Size Limits
	// Used in: validation_enhanced.go for frame duration validation
	// Impact: Ensures frame sizes are within acceptable bounds for real-time audio

	// MinFrameDuration defines minimum acceptable frame duration.
	// Used in: ValidateAudioConfiguration for frame size validation
	// Impact: Prevents excessively small frames that could impact performance.
	// Default 10ms provides minimum viable frame duration for real-time audio.
	MinFrameDuration time.Duration

	// MaxFrameDuration defines maximum acceptable frame duration.
	// Used in: ValidateAudioConfiguration for frame size validation
	// Impact: Prevents excessively large frames that could impact latency.
	// Default 100ms provides reasonable maximum frame duration.
	MaxFrameDuration time.Duration

	// Valid Sample Rates
	// Used in: validation_enhanced.go for sample rate validation
	// Impact: Defines the set of supported sample rates for audio processing

	// ValidSampleRates defines the list of supported sample rates.
	// Used in: ValidateAudioConfiguration for sample rate validation
	// Impact: Ensures only supported sample rates are used in audio processing.
	// Default rates support common audio standards from voice (8kHz) to professional (48kHz).
	ValidSampleRates []int

	// Opus Bitrate Validation Constants
	// Used in: validation_enhanced.go for bitrate range validation
	// Impact: Ensures bitrate values are within Opus codec specifications

	// MinOpusBitrate defines the minimum valid Opus bitrate in bits per second.
	// Used in: ValidateAudioConfiguration for bitrate validation
	// Impact: Prevents bitrates below Opus codec minimum specification.
	// Default 6000 bps is the minimum supported by Opus codec.
	MinOpusBitrate int

	// MaxOpusBitrate defines the maximum valid Opus bitrate in bits per second.
	// Used in: ValidateAudioConfiguration for bitrate validation
	// Impact: Prevents bitrates above Opus codec maximum specification.
	// Default 510000 bps is the maximum supported by Opus codec.
	MaxOpusBitrate int

	// MaxValidationTime defines the maximum time allowed for validation operations.
	// Used in: GetValidationConfig for timeout control
	// Impact: Prevents validation operations from blocking indefinitely.
	// Default 5s provides reasonable timeout for validation operations.
	MaxValidationTime time.Duration

	// MinFrameSize defines the minimum reasonable audio frame size in bytes.
	// Used in: ValidateAudioFrameComprehensive for frame size validation
	// Impact: Prevents processing of unreasonably small audio frames.
	// Default 64 bytes ensures minimum viable audio data.
	MinFrameSize int

	// FrameSizeTolerance defines the tolerance for frame size validation in bytes.
	// Used in: ValidateAudioFrameComprehensive for frame size matching
	// Impact: Allows reasonable variation in frame sizes due to encoding.
	// Default 512 bytes accommodates typical encoding variations.
	FrameSizeTolerance int

	// Device Health Monitoring Configuration
	// Used in: device_health.go for proactive device monitoring and recovery
	// Impact: Controls health check frequency and recovery thresholds

	// HealthCheckIntervalMS defines interval between device health checks in milliseconds.
	// Used in: DeviceHealthMonitor for periodic health assessment
	// Impact: Lower values provide faster detection but increase CPU usage.
	// Default 5000ms (5s) provides good balance between responsiveness and overhead.
	HealthCheckIntervalMS int

	// HealthRecoveryThreshold defines number of consecutive successful operations
	// required to mark a device as healthy after being unhealthy.
	// Used in: DeviceHealthMonitor for recovery state management
	// Impact: Higher values prevent premature recovery declarations.
	// Default 3 consecutive successes ensures stable recovery.
	HealthRecoveryThreshold int

	// HealthLatencyThresholdMS defines maximum acceptable latency in milliseconds
	// before considering a device unhealthy.
	// Used in: DeviceHealthMonitor for latency-based health assessment
	// Impact: Lower values trigger recovery sooner but may cause false positives.
	// Default 100ms provides reasonable threshold for real-time audio.
	HealthLatencyThresholdMS int

	// HealthErrorRateLimit defines maximum error rate (0.0-1.0) before
	// considering a device unhealthy.
	// Used in: DeviceHealthMonitor for error rate assessment
	// Impact: Lower values trigger recovery sooner for error-prone devices.
	// Default 0.1 (10%) allows some transient errors while detecting problems.
	HealthErrorRateLimit float64

	// Latency Histogram Bucket Configuration
	// Used in: LatencyHistogram for granular latency measurement buckets
	// Impact: Defines the boundaries for latency distribution analysis
	LatencyBucket10ms  time.Duration // 10ms latency bucket
	LatencyBucket25ms  time.Duration // 25ms latency bucket
	LatencyBucket50ms  time.Duration // 50ms latency bucket
	LatencyBucket100ms time.Duration // 100ms latency bucket
	LatencyBucket250ms time.Duration // 250ms latency bucket
	LatencyBucket500ms time.Duration // 500ms latency bucket
	LatencyBucket1s    time.Duration // 1s latency bucket
	LatencyBucket2s    time.Duration // 2s latency bucket
}

// DefaultAudioConfig returns the default configuration constants
// These values are carefully chosen based on JetKVM's embedded ARM environment,
// real-time audio requirements, and extensive testing for optimal performance.
func DefaultAudioConfig() *AudioConfigConstants {
	return &AudioConfigConstants{
		// Audio Quality Presets - Core audio frame and packet size configuration
		// Used in: Throughout audio pipeline for buffer allocation and frame processing
		// Impact: Controls memory usage and prevents buffer overruns

		// MaxAudioFrameSize defines maximum size for audio frames.
		// Used in: Buffer allocation throughout audio pipeline
		// Impact: Prevents buffer overruns while accommodating high-quality audio.
		// Default 4096 bytes provides safety margin for largest expected frames.
		MaxAudioFrameSize: 4096,

		// Opus Encoding Parameters - Configuration for Opus audio codec
		// Used in: Audio encoding/decoding pipeline for quality control
		// Impact: Controls audio quality, bandwidth usage, and encoding performance

		// OpusBitrate defines target bitrate for Opus encoding.
		// Used in: Opus encoder initialization and quality control
		// Impact: Higher bitrates improve quality but increase bandwidth usage.
		// Default 128kbps provides excellent quality with reasonable bandwidth.
		OpusBitrate: 128000,

		// OpusComplexity defines computational complexity for Opus encoding.
		// Used in: Opus encoder for quality vs CPU usage balance
		// Impact: Higher complexity improves quality but increases CPU usage.
		// Default 10 (maximum) ensures best quality on modern ARM processors.
		OpusComplexity: 10,

		// OpusVBR enables variable bitrate encoding.
		// Used in: Opus encoder for adaptive bitrate control
		// Impact: Optimizes bandwidth based on audio content complexity.
		// Default 1 (enabled) reduces bandwidth for simple audio content.
		OpusVBR: 1,

		// OpusVBRConstraint controls VBR constraint mode.
		// Used in: Opus encoder for bitrate variation control
		// Impact: 0=unconstrained allows maximum flexibility for quality.
		// Default 0 provides optimal quality-bandwidth balance.
		OpusVBRConstraint: 0,

		// OpusDTX controls discontinuous transmission.
		// Used in: Opus encoder for silence detection and transmission
		// Impact: Can interfere with system audio monitoring in KVM applications.
		// Default 0 (disabled) ensures consistent audio stream.
		OpusDTX: 0,

		// Audio Parameters - Core audio format configuration
		// Used in: Audio processing pipeline for format consistency
		// Impact: Controls audio quality, compatibility, and processing requirements

		// SampleRate defines audio sampling frequency.
		// Used in: Audio capture, processing, and playback throughout pipeline
		// Impact: Higher rates improve quality but increase processing and bandwidth.
		// Default 48kHz provides professional audio quality with full frequency range.
		SampleRate: 48000,

		// Channels defines number of audio channels.
		// Used in: Audio processing pipeline for channel handling
		// Impact: Stereo preserves spatial information but doubles bandwidth.
		// Default 2 (stereo) captures full system audio including spatial effects.
		Channels: 2,

		// FrameSize defines number of samples per audio frame.
		// Used in: Audio processing for frame-based operations
		// Impact: Larger frames improve efficiency but increase latency.
		// Default 960 samples (20ms at 48kHz) balances latency and efficiency.
		FrameSize: 960,

		// MaxPacketSize defines maximum size for audio packets.
		// Used in: Network transmission and buffer allocation
		// Impact: Must accommodate compressed frames with overhead.
		// Default 4000 bytes prevents fragmentation while allowing quality variations.
		MaxPacketSize: 4000,

		// Audio Quality Bitrates - Preset bitrates for different quality levels
		// Used in: Audio quality management and adaptive bitrate control
		// Impact: Controls bandwidth usage and audio quality for different scenarios

		// AudioQualityLowOutputBitrate defines bitrate for low-quality output audio.
		// Used in: Bandwidth-constrained connections and basic audio monitoring
		// Impact: Minimizes bandwidth while maintaining acceptable quality.
		// Default 32kbps optimized for constrained connections, higher than input.
		AudioQualityLowOutputBitrate: 32,

		// AudioQualityLowInputBitrate defines bitrate for low-quality input audio.
		// Used in: Microphone input in bandwidth-constrained scenarios
		// Impact: Reduces bandwidth for microphone audio which is typically simpler.
		// Default 16kbps sufficient for basic voice input.
		AudioQualityLowInputBitrate: 16,

		// AudioQualityMediumOutputBitrate defines bitrate for medium-quality output.
		// Used in: Typical KVM scenarios with reasonable network connections
		// Impact: Balances bandwidth and quality for most use cases.
		// Default 64kbps provides good quality for standard usage.
		AudioQualityMediumOutputBitrate: 64,

		// AudioQualityMediumInputBitrate defines bitrate for medium-quality input.
		// Used in: Standard microphone input scenarios
		// Impact: Provides good voice quality without excessive bandwidth.
		// Default 32kbps suitable for clear voice communication.
		AudioQualityMediumInputBitrate: 32,

		// AudioQualityHighOutputBitrate defines bitrate for high-quality output.
		// Used in: Professional applications requiring excellent audio fidelity
		// Impact: Provides excellent quality but increases bandwidth usage.
		// Default 128kbps matches professional Opus encoding standards.
		AudioQualityHighOutputBitrate: 128,

		// AudioQualityHighInputBitrate defines bitrate for high-quality input.
		// Used in: High-quality microphone input for professional use
		// Impact: Ensures clear voice reproduction for professional scenarios.
		// Default 64kbps provides excellent voice quality.
		AudioQualityHighInputBitrate: 64,

		// AudioQualityUltraOutputBitrate defines bitrate for ultra-quality output.
		// Used in: Audiophile-grade reproduction and high-bandwidth connections
		// Impact: Maximum quality but requires significant bandwidth.
		// Default 192kbps suitable for high-bandwidth, quality-critical scenarios.
		AudioQualityUltraOutputBitrate: 192,

		// AudioQualityUltraInputBitrate defines bitrate for ultra-quality input.
		// Used in: Professional microphone input requiring maximum quality
		// Impact: Provides audiophile-grade voice quality with high bandwidth.
		// Default 96kbps ensures maximum voice reproduction quality.
		AudioQualityUltraInputBitrate: 96,

		// Audio Quality Sample Rates - Sampling frequencies for different quality levels
		// Used in: Audio capture, processing, and format negotiation
		// Impact: Controls audio frequency range and processing requirements

		// AudioQualityLowSampleRate defines sampling frequency for low-quality audio.
		// Used in: Bandwidth-constrained scenarios and basic audio requirements
		// Impact: Captures frequencies up to 11kHz while minimizing processing load.
		// Default 22.05kHz sufficient for speech and basic audio.
		AudioQualityLowSampleRate: 22050,

		// AudioQualityMediumSampleRate defines sampling frequency for medium-quality audio.
		// Used in: Standard audio scenarios requiring CD-quality reproduction
		// Impact: Captures full audible range up to 22kHz with balanced processing.
		// Default 44.1kHz provides CD-quality standard with excellent balance.
		AudioQualityMediumSampleRate: 44100,

		// AudioQualityMicLowSampleRate defines sampling frequency for low-quality microphone.
		// Used in: Voice/microphone input in bandwidth-constrained scenarios
		// Impact: Captures speech frequencies efficiently while reducing bandwidth.
		// Default 16kHz optimized for speech frequencies (300-8000Hz).
		AudioQualityMicLowSampleRate: 16000,

		// Audio Quality Frame Sizes - Frame durations for different quality levels
		// Used in: Audio processing pipeline for latency and efficiency control
		// Impact: Controls latency vs processing efficiency trade-offs

		// AudioQualityLowFrameSize defines frame duration for low-quality audio.
		// Used in: Bandwidth-constrained scenarios prioritizing efficiency
		// Impact: Reduces processing overhead with acceptable latency increase.
		// Default 40ms provides efficiency for constrained scenarios.
		AudioQualityLowFrameSize: 40 * time.Millisecond,

		// AudioQualityMediumFrameSize defines frame duration for medium-quality audio.
		// Used in: Standard real-time audio applications
		// Impact: Provides good balance of latency and processing efficiency.
		// Default 20ms standard for real-time audio applications.
		AudioQualityMediumFrameSize: 20 * time.Millisecond,

		// AudioQualityHighFrameSize defines frame duration for high-quality audio.
		// Used in: High-quality audio scenarios with balanced requirements
		// Impact: Maintains good latency while ensuring quality processing.
		// Default 20ms provides optimal balance for high-quality scenarios.
		AudioQualityHighFrameSize: 20 * time.Millisecond,

		// AudioQualityUltraFrameSize defines frame duration for ultra-quality audio.
		// Used in: Applications requiring immediate audio feedback
		// Impact: Minimizes latency for ultra-responsive audio processing.
		// Default 10ms ensures minimal latency for immediate feedback.
		AudioQualityUltraFrameSize: 10 * time.Millisecond,

		// Audio Quality Channels - Channel configuration for different quality levels
		// Used in: Audio processing pipeline for channel handling and bandwidth control
		// Impact: Controls spatial audio information and bandwidth requirements

		// AudioQualityLowChannels defines channel count for low-quality audio.
		// Used in: Basic audio monitoring in bandwidth-constrained scenarios
		// Impact: Reduces bandwidth by 50% with acceptable quality trade-off.
		// Default 1 (mono) suitable where stereo separation not critical.
		AudioQualityLowChannels: 1,

		// AudioQualityMediumChannels defines channel count for medium-quality audio.
		// Used in: Standard audio scenarios requiring spatial information
		// Impact: Preserves spatial audio information essential for modern systems.
		// Default 2 (stereo) maintains spatial audio for medium quality.
		AudioQualityMediumChannels: 2,

		// AudioQualityHighChannels defines channel count for high-quality audio.
		// Used in: High-quality audio scenarios requiring full spatial reproduction
		// Impact: Ensures complete spatial audio information for quality scenarios.
		// Default 2 (stereo) preserves spatial information for high quality.
		AudioQualityHighChannels: 2,

		// AudioQualityUltraChannels defines channel count for ultra-quality audio.
		// Used in: Ultra-quality scenarios requiring maximum spatial fidelity
		// Impact: Provides complete spatial audio reproduction for audiophile use.
		// Default 2 (stereo) ensures maximum spatial fidelity for ultra quality.
		AudioQualityUltraChannels: 2,

		// CGO Audio Constants - Configuration for C interop audio processing
		// Used in: CGO audio operations and C library compatibility
		// Impact: Controls quality, performance, and compatibility for C-side processing

		// CGOOpusBitrate defines bitrate for CGO Opus operations.
		// Used in: CGO audio encoding with embedded processing constraints
		// Impact: Conservative bitrate reduces processing load while maintaining quality.
		// Default 96kbps provides good quality suitable for embedded processing.
		CGOOpusBitrate: 96000,

		// CGOOpusComplexity defines complexity for CGO Opus operations.
		// Used in: CGO audio encoding for CPU load management
		// Impact: Lower complexity reduces CPU load while maintaining acceptable quality.
		// Default 3 balances quality and real-time processing requirements.
		CGOOpusComplexity: 3,

		// CGOOpusVBR enables variable bitrate for CGO operations.
		// Used in: CGO audio encoding for adaptive bandwidth optimization
		// Impact: Allows bitrate adaptation based on content complexity.
		// Default 1 (enabled) optimizes bandwidth usage in CGO processing.
		CGOOpusVBR: 1,

		// CGOOpusVBRConstraint controls VBR constraint for CGO operations.
		// Used in: CGO audio encoding for predictable processing load
		// Impact: Limits bitrate variations for more predictable embedded performance.
		// Default 1 (constrained) ensures predictable processing in embedded environment.
		CGOOpusVBRConstraint: 1,

		// CGOOpusSignalType defines signal type for CGO Opus operations.
		// Used in: CGO audio encoding for content-optimized processing
		// Impact: Optimizes encoding for general audio content types.
		// Default 3 (OPUS_SIGNAL_MUSIC) handles system sounds, music, and mixed audio.
		CGOOpusSignalType: 3, // OPUS_SIGNAL_MUSIC

		// CGOOpusBandwidth defines bandwidth for CGO Opus operations.
		// Used in: CGO audio encoding for frequency range control
		// Impact: Enables full audio spectrum reproduction up to 20kHz.
		// Default 1105 (OPUS_BANDWIDTH_FULLBAND) provides complete spectrum coverage.
		CGOOpusBandwidth: 1105, // OPUS_BANDWIDTH_FULLBAND

		// CGOOpusDTX controls discontinuous transmission for CGO operations.
		// Used in: CGO audio encoding for silence detection control
		// Impact: Prevents silence detection interference with system audio monitoring.
		// Default 0 (disabled) ensures consistent audio stream.
		CGOOpusDTX: 0,

		// CGOSampleRate defines sample rate for CGO audio operations.
		// Used in: CGO audio processing for format consistency
		// Impact: Matches main audio parameters for pipeline consistency.
		// Default 48kHz provides professional audio quality and consistency.
		CGOSampleRate: 48000,

		// CGOChannels defines channel count for CGO audio operations.
		// Used in: CGO audio processing for spatial audio handling
		// Impact: Maintains spatial audio information throughout CGO pipeline.
		// Default 2 (stereo) preserves spatial information in CGO processing.
		CGOChannels: 2,

		// CGOFrameSize defines frame size for CGO audio operations.
		// Used in: CGO audio processing for timing consistency
		// Impact: Matches main frame size for consistent timing and efficiency.
		// Default 960 samples (20ms at 48kHz) ensures consistent processing timing.
		CGOFrameSize: 960,

		// CGOMaxPacketSize defines maximum packet size for CGO operations.
		// Used in: CGO audio transmission and buffer allocation
		// Impact: Accommodates Ethernet MTU while providing sufficient packet space.
		// Default 1500 bytes fits Ethernet MTU constraints with compressed audio.
		CGOMaxPacketSize: 1500,

		// Input IPC Constants - Configuration for microphone input IPC
		// Used in: Microphone input processing and IPC communication
		// Impact: Controls quality and compatibility for input audio processing

		// InputIPCSampleRate defines sample rate for input IPC operations.
		// Used in: Microphone input capture and processing
		// Impact: Ensures high-quality input matching system audio output.
		// Default 48kHz provides consistent quality across input/output.
		InputIPCSampleRate: 48000,

		// InputIPCChannels defines channel count for input IPC operations.
		// Used in: Microphone input processing and device compatibility
		// Impact: Captures spatial information and maintains device compatibility.
		// Default 2 (stereo) supports spatial microphone information.
		InputIPCChannels: 2,

		// InputIPCFrameSize defines frame size for input IPC operations.
		// Used in: Real-time microphone input processing
		// Impact: Balances latency and processing efficiency for input.
		// Default 960 samples (20ms) optimal for real-time microphone input.
		InputIPCFrameSize: 960,

		// Output IPC Constants - Configuration for audio output IPC
		// Used in: Audio output processing and IPC communication
		// Impact: Controls performance and reliability for output audio

		// OutputMaxFrameSize defines maximum frame size for output IPC.
		// Used in: Output IPC communication and buffer allocation
		// Impact: Prevents buffer overruns while accommodating large frames.
		// Default 4096 bytes provides safety margin for largest audio frames.
		OutputMaxFrameSize: 4096,

		// OutputWriteTimeout defines timeout for output write operations.
		// Used in: Real-time audio output and blocking prevention
		// Impact: Provides quick response while preventing blocking scenarios.
		// Default 10ms ensures real-time response for high-performance audio.
		OutputWriteTimeout: 10 * time.Millisecond,

		// OutputMaxDroppedFrames defines maximum allowed dropped frames.
		// Used in: Error handling and resilience management
		// Impact: Provides resilience against temporary processing issues.
		// Default 50 frames allows recovery from temporary network/processing issues.
		OutputMaxDroppedFrames: 50,

		// OutputHeaderSize defines size of output frame headers.
		// Used in: Frame metadata and IPC communication
		// Impact: Provides space for timestamps, sequence numbers, and format info.
		// Default 17 bytes sufficient for comprehensive frame metadata.
		OutputHeaderSize: 17,

		// OutputMessagePoolSize defines size of output message pool.
		// Used in: Efficient audio streaming and memory management
		// Impact: Balances memory usage with streaming throughput.
		// Default 128 messages provides efficient streaming without excessive buffering.
		OutputMessagePoolSize: 128,

		// Socket Buffer Constants - Configuration for network socket buffers
		// Used in: Network audio streaming and socket management
		// Impact: Controls buffering capacity and memory usage for audio streaming

		// SocketOptimalBuffer defines optimal socket buffer size.
		// Used in: Network throughput optimization for audio streaming
		// Impact: Provides good balance between memory usage and performance.
		// Default 128KB balances memory usage and network throughput.
		SocketOptimalBuffer: 131072, // 128KB

		// SocketMaxBuffer defines maximum socket buffer size.
		// Used in: Burst traffic handling and high bitrate audio streaming
		// Impact: Accommodates burst traffic without excessive memory consumption.
		// Default 256KB handles high bitrate audio and burst traffic.
		SocketMaxBuffer: 262144, // 256KB

		// SocketMinBuffer defines minimum socket buffer size.
		// Used in: Basic audio streaming and memory-constrained scenarios
		// Impact: Ensures adequate buffering while minimizing memory footprint.
		// Default 32KB provides basic buffering for audio streaming.
		SocketMinBuffer: 32768, // 32KB

		// Scheduling Policy Constants - Configuration for process scheduling
		// Used in: Process scheduling and real-time audio processing
		// Impact: Controls scheduling behavior for audio processing tasks

		// SchedNormal defines standard time-sharing scheduling policy.
		// Used in: Non-critical audio processing tasks
		// Impact: Provides standard scheduling suitable for non-critical tasks.
		// Default 0 (SCHED_NORMAL) for standard time-sharing scheduling.
		SchedNormal: 0,

		// SchedFIFO defines real-time first-in-first-out scheduling policy.
		// Used in: Critical audio processing requiring deterministic timing
		// Impact: Provides deterministic scheduling for latency-critical operations.
		// Default 1 (SCHED_FIFO) for real-time first-in-first-out scheduling.
		SchedFIFO: 1,

		// SchedRR defines real-time round-robin scheduling policy.
		// Used in: Balanced real-time processing with time slicing
		// Impact: Provides real-time scheduling with balanced time slicing.
		// Default 2 (SCHED_RR) for real-time round-robin scheduling.
		SchedRR: 2,

		// Real-time Priority Levels - Configuration for process priorities
		// Used in: Process priority management and CPU scheduling
		// Impact: Controls priority hierarchy for audio system components

		// RTAudioHighPriority defines highest priority for audio processing.
		// Used in: Latency-critical audio operations and CPU priority assignment
		// Impact: Ensures highest CPU priority without starving system processes.
		// Default 80 provides highest priority for latency-critical operations.
		RTAudioHighPriority: 80,

		// RTAudioMediumPriority defines medium priority for audio tasks.
		// Used in: Important audio tasks requiring elevated priority
		// Impact: Provides elevated priority while allowing higher priority operations.
		// Default 60 balances importance with system operation priority.
		RTAudioMediumPriority: 60,

		// RTAudioLowPriority defines low priority for audio tasks.
		// Used in: Audio tasks needing responsiveness but not latency-critical
		// Impact: Provides moderate real-time priority for responsive tasks.
		// Default 40 ensures responsiveness without being latency-critical.
		RTAudioLowPriority: 40,

		// RTNormalPriority defines normal scheduling priority.
		// Used in: Non-real-time audio processing tasks
		// Impact: Provides standard priority for non-real-time operations.
		// Default 0 represents normal scheduling priority.
		RTNormalPriority: 0,

		// Process Management - Configuration for process restart and recovery
		// Used in: Process monitoring and failure recovery systems
		// Impact: Controls resilience and stability of audio processes

		// MaxRestartAttempts defines maximum number of restart attempts.
		// Used in: Process failure recovery and restart logic
		// Impact: Provides resilience against transient failures while preventing loops.
		// Default 5 attempts balances recovery capability with loop prevention.
		MaxRestartAttempts: 5,

		// RestartWindow defines time window for restart attempt counting.
		// Used in: Restart attempt counter reset and long-term stability
		// Impact: Allows recovery from temporary issues while resetting counters.
		// Default 5 minutes provides adequate window for temporary issue recovery.
		RestartWindow: 5 * time.Minute,

		// RestartDelay defines initial delay before restart attempts.
		// Used in: Process restart timing and rapid cycle prevention
		// Impact: Prevents rapid restart cycles while allowing quick recovery.
		// Default 1 second prevents rapid cycles while enabling quick recovery.
		RestartDelay: 1 * time.Second,

		// MaxRestartDelay defines maximum delay for exponential backoff.
		// Used in: Exponential backoff implementation for persistent failures
		// Impact: Prevents excessive wait times while implementing backoff strategy.
		// Default 30 seconds limits wait time while providing backoff for failures.
		MaxRestartDelay: 30 * time.Second,

		// Buffer Management - Configuration for memory buffer allocation
		// Used in: Memory management and buffer allocation systems
		// Impact: Controls memory usage and performance for audio processing

		// PreallocSize defines size for buffer preallocation.
		// Used in: High-throughput audio processing and memory preallocation
		// Impact: Provides substantial buffer space while remaining reasonable for embedded systems.
		// Default 1MB balances throughput capability with embedded system constraints.
		PreallocSize: 1024 * 1024, // 1MB

		// MaxPoolSize defines maximum size for object pools.
		// Used in: Object pooling and efficient memory management
		// Impact: Limits memory usage while providing adequate pooling efficiency.
		// Default 100 objects balances memory usage with pooling benefits.
		MaxPoolSize: 100,

		// MessagePoolSize defines size for message pools.
		// Used in: IPC communication and message throughput optimization
		// Impact: Balances memory usage with message throughput for efficient IPC.
		// Default 256 messages optimizes IPC communication efficiency.
		MessagePoolSize: 256,

		// OptimalSocketBuffer defines optimal socket buffer size.
		// Used in: Network performance optimization for audio streaming
		// Impact: Provides good network performance without excessive memory consumption.
		// Default 256KB balances network performance with memory efficiency.
		OptimalSocketBuffer: 262144, // 256KB

		// MaxSocketBuffer defines maximum socket buffer size.
		// Used in: Burst traffic handling and high-bitrate audio streaming
		// Impact: Accommodates burst traffic while preventing excessive memory usage.
		// Default 1MB handles burst traffic and high-bitrate audio efficiently.
		MaxSocketBuffer: 1048576, // 1MB

		// MinSocketBuffer defines minimum socket buffer size.
		// Used in: Basic network buffering and low-bandwidth scenarios
		// Impact: Ensures basic buffering while minimizing memory footprint.
		// Default 8KB provides basic buffering for low-bandwidth scenarios.
		MinSocketBuffer: 8192, // 8KB

		// ChannelBufferSize defines buffer size for inter-goroutine channels.
		// Used in: Inter-goroutine communication and processing pipelines
		// Impact: Provides adequate buffering without blocking communication.
		// Default 500 ensures smooth inter-goroutine communication.
		ChannelBufferSize: 500, // Channel buffer size for processing

		// AudioFramePoolSize defines size for audio frame object pools.
		// Used in: Frame reuse and efficient memory management
		// Impact: Accommodates frame reuse for high-throughput scenarios.
		// Default 1500 frames optimizes memory management in high-throughput scenarios.
		AudioFramePoolSize: 1500, // Audio frame pool size

		// PageSize defines memory page size for allocation alignment.
		// Used in: Memory allocation and cache performance optimization
		// Impact: Aligns with system memory pages for optimal allocation and cache performance.
		// Default 4096 bytes aligns with standard system memory pages.
		PageSize: 4096, // Memory page size

		// InitialBufferFrames defines initial buffer size during startup.
		// Used in: Startup buffering and initialization memory allocation
		// Impact: Provides adequate startup buffering without excessive allocation.
		// Default 500 frames balances startup buffering with memory efficiency.
		InitialBufferFrames: 500, // Initial buffer size in frames

		// BytesToMBDivisor defines divisor for byte to megabyte conversion.
		// Used in: Memory usage calculations and reporting
		// Impact: Provides standard MB conversion for memory calculations.
		// Default 1024*1024 provides standard megabyte conversion.
		BytesToMBDivisor: 1024 * 1024, // Divisor for converting bytes to MB

		// MinReadEncodeBuffer defines minimum buffer for CGO read/encode operations.
		// Used in: CGO audio read/encode operations and processing space allocation
		// Impact: Accommodates smallest operations while ensuring adequate processing space.
		// Default 1276 bytes ensures adequate space for smallest CGO operations.
		MinReadEncodeBuffer: 1276, // Minimum buffer size for CGO audio read/encode

		// MaxDecodeWriteBuffer defines maximum buffer for CGO decode/write operations.
		// Used in: CGO audio decode/write operations and memory allocation
		// Impact: Provides sufficient space for largest operations without excessive allocation.
		// Default 4096 bytes accommodates largest CGO operations efficiently.
		MaxDecodeWriteBuffer: 4096, // Maximum buffer size for CGO audio decode/write

		// IPC Configuration - Settings for inter-process communication
		// Used in: ipc_manager.go for message validation and processing
		// Impact: Controls IPC message structure and validation mechanisms

		// MagicNumber defines distinctive header for IPC message validation.
		// Used in: ipc_manager.go for message header validation and debugging
		// Impact: Provides reliable message boundary detection and corruption detection
		// Default 0xDEADBEEF provides easily recognizable pattern for debugging
		MagicNumber: 0xDEADBEEF,

		// MaxFrameSize defines maximum size for audio frames in IPC messages.
		// Used in: ipc_manager.go for buffer allocation and message size validation
		// Impact: Prevents excessive memory allocation while accommodating largest frames
		// Default 4096 bytes handles typical audio frame sizes with safety margin
		MaxFrameSize: 4096,

		// WriteTimeout defines maximum wait time for IPC write operations.
		// Used in: ipc_manager.go for preventing indefinite blocking on writes
		// Impact: Balances responsiveness with reliability for IPC operations
		// Default 5 seconds provides reasonable timeout for most system conditions
		WriteTimeout: 5 * time.Second,

		// MaxDroppedFrames defines threshold for dropped frame error handling.
		// Used in: ipc_manager.go for quality degradation detection and recovery
		// Impact: Balances audio continuity with quality maintenance requirements
		// Default 10 frames allows temporary issues while preventing quality loss
		MaxDroppedFrames: 10,

		// HeaderSize defines size of IPC message headers in bytes.
		// Used in: ipc_manager.go for message parsing and buffer management
		// Impact: Determines metadata capacity and parsing efficiency
		// Default 8 bytes provides space for message type and size information
		HeaderSize: 8,

		// Monitoring and Metrics - Settings for performance monitoring and data collection
		// Used in: metrics_collector.go, performance_monitor.go for system monitoring
		// Impact: Controls monitoring frequency and data collection efficiency

		// MetricsUpdateInterval defines frequency of metrics collection updates.
		// Used in: metrics_collector.go for scheduling performance data collection
		// Impact: Balances monitoring timeliness with system overhead
		// Default 1 second provides responsive monitoring without excessive CPU usage
		MetricsUpdateInterval: 1000 * time.Millisecond,

		// EMAAlpha defines smoothing factor for exponential moving averages.
		// Used in: metrics_collector.go for calculating smoothed performance metrics
		// Impact: Controls responsiveness vs stability of metric calculations
		// Default 0.1 provides smooth averaging with 90% weight on historical data
		EMAAlpha: 0.1,

		// WarmupSamples defines number of samples before metrics stabilization.
		// Used in: metrics_collector.go for preventing premature optimization decisions
		// Impact: Ensures metrics accuracy before triggering performance adjustments
		// Default 10 samples allows sufficient data collection for stable metrics
		WarmupSamples: 10,

		// LogThrottleInterval defines minimum time between similar log messages.
		// Used in: logger.go for preventing log spam while capturing important events
		// Impact: Balances debugging information with log file size management
		// Default 5 seconds prevents spam while ensuring critical events are logged
		LogThrottleInterval: 5 * time.Second,

		// MetricsChannelBuffer defines buffer size for metrics data channels.
		// Used in: metrics_collector.go for buffering performance data collection
		// Impact: Prevents blocking of metrics collection during processing spikes
		// Default 100 provides adequate buffering without excessive memory usage
		MetricsChannelBuffer: 100,

		// LatencyHistorySize defines number of latency measurements to retain.
		// Used in: performance_monitor.go for statistical analysis and trend detection
		// Impact: Determines accuracy of latency statistics and memory usage
		// Default 100 measurements provides sufficient history for trend analysis
		LatencyHistorySize: 100, // Number of latency measurements to keep

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

		// Performance Tuning - Thresholds for adaptive performance management
		// Used in: adaptive_optimizer.go, quality_manager.go for performance scaling
		// Impact: Controls when system switches between performance modes

		// CPUThresholdLow defines low CPU usage threshold (20%).
		// Used in: adaptive_optimizer.go for triggering quality increases
		// Impact: Below this threshold, system can increase audio quality/buffer sizes
		// Default 0.20 (20%) ensures conservative quality upgrades with CPU headroom
		CPUThresholdLow: 0.20,

		// CPUThresholdMedium defines medium CPU usage threshold (60%).
		// Used in: adaptive_optimizer.go for balanced performance mode
		// Impact: Between low and medium, system maintains current settings
		// Default 0.60 (60%) provides good balance between quality and performance
		CPUThresholdMedium: 0.60,

		// CPUThresholdHigh defines high CPU usage threshold (75%).
		// Used in: adaptive_optimizer.go for triggering quality reductions
		// Impact: Above this threshold, system reduces quality to prevent overload
		// Default 0.75 (75%) allows high utilization while preventing system stress
		CPUThresholdHigh: 0.75,

		// MemoryThresholdLow defines low memory usage threshold (30%).
		// Used in: adaptive_optimizer.go for memory-based optimizations
		// Impact: Below this, system can allocate larger buffers for better performance
		// Default 0.30 (30%) ensures sufficient memory headroom for buffer expansion
		MemoryThresholdLow: 0.30,

		// MemoryThresholdMed defines medium memory usage threshold (60%).
		// Used in: adaptive_optimizer.go for balanced memory management
		// Impact: Between low and medium, system maintains current buffer sizes
		// Default 0.60 (60%) provides balance between performance and memory efficiency
		MemoryThresholdMed: 0.60,

		// MemoryThresholdHigh defines high memory usage threshold (80%).
		// Used in: adaptive_optimizer.go for triggering memory optimizations
		// Impact: Above this, system reduces buffer sizes to prevent memory pressure
		// Default 0.80 (80%) allows high memory usage while preventing OOM conditions
		MemoryThresholdHigh: 0.80,

		// LatencyThresholdLow defines acceptable low latency threshold (20ms).
		// Used in: adaptive_optimizer.go for latency-based quality adjustments
		// Impact: Below this, system can increase quality as latency is acceptable
		// Default 20ms provides excellent real-time audio experience
		LatencyThresholdLow: 20 * time.Millisecond,

		// LatencyThresholdHigh defines maximum acceptable latency threshold (50ms).
		// Used in: adaptive_optimizer.go for triggering latency optimizations
		// Impact: Above this, system prioritizes latency reduction over quality
		// Default 50ms is the upper limit for acceptable real-time audio latency
		LatencyThresholdHigh: 50 * time.Millisecond,

		// CPUFactor defines weight of CPU usage in performance calculations (0.7).
		// Used in: adaptive_optimizer.go for weighted performance scoring
		// Impact: Higher values make CPU usage more influential in decisions
		// Default 0.7 (70%) emphasizes CPU as primary performance bottleneck
		CPUFactor: 0.7,

		// MemoryFactor defines weight of memory usage in performance calculations (0.8).
		// Used in: adaptive_optimizer.go for weighted performance scoring
		// Impact: Higher values make memory usage more influential in decisions
		// Default 0.8 (80%) emphasizes memory as critical for stability
		MemoryFactor: 0.8,

		// LatencyFactor defines weight of latency in performance calculations (0.9).
		// Used in: adaptive_optimizer.go for weighted performance scoring
		// Impact: Higher values make latency more influential in decisions
		// Default 0.9 (90%) prioritizes latency as most critical for real-time audio
		LatencyFactor: 0.9,

		// InputSizeThreshold defines threshold for input buffer size optimizations (1024 bytes).
		// Used in: input processing for determining when to optimize buffer handling
		// Impact: Larger values delay optimizations but reduce overhead
		// Default 1024 bytes balances optimization frequency with processing efficiency
		InputSizeThreshold: 1024,

		// OutputSizeThreshold defines threshold for output buffer size optimizations (2048 bytes).
		// Used in: output processing for determining when to optimize buffer handling
		// Impact: Larger values delay optimizations but reduce overhead
		// Default 2048 bytes (2x input) accounts for potential encoding expansion
		OutputSizeThreshold: 2048,

		// TargetLevel defines target performance level for adaptive algorithms (0.5).
		// Used in: adaptive_optimizer.go as target for performance balancing
		// Impact: Controls how aggressively system optimizes (0.0=conservative, 1.0=aggressive)
		// Default 0.5 (50%) provides balanced optimization between quality and performance
		TargetLevel: 0.5,

		// Priority Scheduling - Process priority values for real-time audio performance
		// Used in: process management, thread scheduling for audio processing
		// Impact: Controls CPU scheduling priority for audio threads

		// AudioHighPriority defines highest priority for critical audio threads (-10).
		// Used in: Real-time audio processing threads, encoder/decoder threads
		// Impact: Ensures audio threads get CPU time before other processes
		// Default -10 provides high priority without requiring root privileges
		AudioHighPriority: -10,

		// AudioMediumPriority defines medium priority for important audio threads (-5).
		// Used in: Audio buffer management, IPC communication threads
		// Impact: Balances audio performance with system responsiveness
		// Default -5 ensures good performance while allowing other critical tasks
		AudioMediumPriority: -5,

		// AudioLowPriority defines low priority for non-critical audio threads (0).
		// Used in: Metrics collection, logging, cleanup tasks
		// Impact: Prevents non-critical tasks from interfering with audio processing
		// Default 0 (normal priority) for background audio-related tasks
		AudioLowPriority: 0,

		// NormalPriority defines standard system priority (0).
		// Used in: Fallback priority, non-audio system tasks
		// Impact: Standard scheduling behavior for general tasks
		// Default 0 represents normal Linux process priority
		NormalPriority: 0,

		// NiceValue defines default nice value for audio processes (-10).
		// Used in: Process creation, priority adjustment for audio components
		// Impact: Improves audio process scheduling without requiring special privileges
		// Default -10 provides better scheduling while remaining accessible to non-root users
		NiceValue: -10,

		// Error Handling - Configuration for robust error recovery and retry logic
		// Used in: Throughout audio pipeline for handling transient failures
		// Impact: Controls system resilience and recovery behavior

		// MaxRetries defines maximum retry attempts for failed operations (3).
		// Used in: Audio encoding/decoding, IPC communication, file operations
		// Impact: Higher values increase resilience but may delay error detection
		// Default 3 provides good balance between resilience and responsiveness
		MaxRetries: 3,

		// RetryDelay defines initial delay between retry attempts (100ms).
		// Used in: Exponential backoff retry logic across audio components
		// Impact: Shorter delays retry faster but may overwhelm failing resources
		// Default 100ms allows quick recovery while preventing resource flooding
		RetryDelay: 100 * time.Millisecond,

		// MaxRetryDelay defines maximum delay between retry attempts (5s).
		// Used in: Exponential backoff to cap maximum wait time
		// Impact: Prevents indefinitely long delays while maintaining backoff benefits
		// Default 5s ensures reasonable maximum wait time for audio operations
		MaxRetryDelay: 5 * time.Second,

		// BackoffMultiplier defines exponential backoff multiplier (2.0).
		// Used in: Retry logic to calculate increasing delays between attempts
		// Impact: Higher values create longer delays, lower values retry more aggressively
		// Default 2.0 provides standard exponential backoff (100ms, 200ms, 400ms, etc.)
		BackoffMultiplier: 2.0,

		// ErrorChannelSize defines buffer size for error reporting channels (50).
		// Used in: Error collection and reporting across audio components
		// Impact: Larger buffers prevent error loss but use more memory
		// Default 50 accommodates burst errors while maintaining reasonable memory usage
		ErrorChannelSize: 50,

		// MaxConsecutiveErrors defines threshold for consecutive error handling (5).
		// Used in: Error monitoring to detect persistent failure conditions
		// Impact: Lower values trigger failure handling sooner, higher values are more tolerant
		// Default 5 allows for transient issues while detecting persistent problems
		MaxConsecutiveErrors: 5,

		// MaxRetryAttempts defines maximum retry attempts for critical operations (3).
		// Used in: Critical audio operations that require additional retry logic
		// Impact: Provides additional retry layer for mission-critical audio functions
		// Default 3 matches MaxRetries for consistency in retry behavior
		MaxRetryAttempts: 3,

		// Timing Constants - Critical timing values for audio processing coordination
		// Used in: Scheduling, synchronization, and timing-sensitive operations
		// Impact: Controls system responsiveness and timing accuracy

		// DefaultSleepDuration defines standard sleep interval for polling loops (100ms).
		// Used in: General purpose polling, non-critical background tasks
		// Impact: Shorter intervals increase responsiveness but consume more CPU
		// Default 100ms balances responsiveness with CPU efficiency
		DefaultSleepDuration: 100 * time.Millisecond,

		// ShortSleepDuration defines brief sleep interval for tight loops (10ms).
		// Used in: High-frequency polling, real-time audio processing loops
		// Impact: Critical for maintaining low-latency audio processing
		// Default 10ms provides responsive polling while preventing CPU spinning
		ShortSleepDuration: 10 * time.Millisecond,

		// LongSleepDuration defines extended sleep interval for slow operations (200ms).
		// Used in: Background maintenance, non-urgent periodic tasks
		// Impact: Reduces CPU usage for infrequent operations
		// Default 200ms suitable for background tasks that don't need frequent execution
		LongSleepDuration: 200 * time.Millisecond,

		// DefaultTickerInterval defines standard ticker interval for periodic tasks (100ms).
		// Used in: Metrics collection, periodic health checks, status updates
		// Impact: Controls frequency of periodic operations and system monitoring
		// Default 100ms provides good balance between monitoring accuracy and overhead
		DefaultTickerInterval: 100 * time.Millisecond,

		// BufferUpdateInterval defines frequency of buffer status updates (500ms).
		// Used in: Buffer management, adaptive buffer sizing, performance monitoring
		// Impact: Controls how quickly system responds to buffer condition changes
		// Default 500ms allows buffer conditions to stabilize before adjustments
		BufferUpdateInterval: 500 * time.Millisecond,

		// StatsUpdateInterval defines frequency of statistics collection (5s).
		// Used in: Performance metrics, system statistics, monitoring dashboards
		// Impact: Controls granularity of performance monitoring and reporting
		// Default 5s provides meaningful statistics while minimizing collection overhead
		StatsUpdateInterval: 5 * time.Second,

		// SupervisorTimeout defines timeout for supervisor operations (10s).
		// Used in: Process supervision, health monitoring, restart logic
		// Impact: Controls how long to wait before considering operations failed
		// Default 10s allows for slow operations while preventing indefinite hangs
		SupervisorTimeout: 10 * time.Second,

		// InputSupervisorTimeout defines timeout for input supervision (5s).
		// Used in: Input process monitoring, microphone supervision
		// Impact: Controls responsiveness of input failure detection
		// Default 5s (shorter than general supervisor) for faster input recovery
		InputSupervisorTimeout: 5 * time.Second,

		// OutputSupervisorTimeout defines timeout for output supervisor operations.
		// Used in: Output process monitoring, speaker supervision
		// Impact: Controls responsiveness of output failure detection
		// Default 5s (shorter than general supervisor) for faster output recovery
		OutputSupervisorTimeout: 5 * time.Second,

		// ShortTimeout defines brief timeout for quick operations (5ms).
		// Used in: Lock acquisition, quick IPC operations, immediate responses
		// Impact: Critical for maintaining real-time performance
		// Default 5ms prevents blocking while allowing for brief delays
		ShortTimeout: 5 * time.Millisecond,

		// MediumTimeout defines moderate timeout for standard operations (50ms).
		// Used in: Network operations, file I/O, moderate complexity tasks
		// Impact: Balances operation completion time with responsiveness
		// Default 50ms accommodates most standard operations without excessive waiting
		MediumTimeout: 50 * time.Millisecond,

		// BatchProcessingDelay defines delay between batch processing cycles (10ms).
		// Used in: Batch audio frame processing, bulk operations
		// Impact: Controls batch processing frequency and system load
		// Default 10ms maintains high throughput while allowing system breathing room
		BatchProcessingDelay: 10 * time.Millisecond,

		// AdaptiveOptimizerStability defines stability period for adaptive changes (10s).
		// Used in: Adaptive optimization algorithms, performance tuning
		// Impact: Prevents oscillation in adaptive systems
		// Default 10s allows system to stabilize before making further adjustments
		AdaptiveOptimizerStability: 10 * time.Second,

		// MaxLatencyTarget defines maximum acceptable latency target (50ms).
		// Used in: Latency monitoring, performance optimization, quality control
		// Impact: Sets upper bound for acceptable audio latency
		// Default 50ms represents maximum tolerable latency for real-time audio
		MaxLatencyTarget: 50 * time.Millisecond,

		// LatencyMonitorTarget defines target latency for monitoring (50ms).
		// Used in: Latency monitoring systems, performance alerts
		// Impact: Controls when latency warnings and optimizations are triggered
		// Default 50ms matches MaxLatencyTarget for consistent latency management
		LatencyMonitorTarget: 50 * time.Millisecond,

		// Adaptive Buffer Configuration
		LowCPUThreshold:     0.20,
		HighCPUThreshold:    0.60,
		LowMemoryThreshold:  0.50,
		HighMemoryThreshold: 0.75,
		TargetLatency:       20 * time.Millisecond,

		// Adaptive Buffer Size Configuration
		AdaptiveMinBufferSize:     3,  // Minimum 3 frames for stability
		AdaptiveMaxBufferSize:     20, // Maximum 20 frames for high load
		AdaptiveDefaultBufferSize: 6,  // Default 6 frames for balanced performance

		// Adaptive Optimizer Configuration
		CooldownPeriod:    30 * time.Second,
		RollbackThreshold: 300 * time.Millisecond,
		LatencyTarget:     50 * time.Millisecond,

		// Latency Monitor Configuration
		MaxLatencyThreshold:         200 * time.Millisecond,
		JitterThreshold:             20 * time.Millisecond,
		LatencyOptimizationInterval: 5 * time.Second,
		LatencyAdaptiveThreshold:    0.8,

		// Microphone Contention Configuration
		MicContentionTimeout: 200 * time.Millisecond,

		// Priority Scheduler Configuration
		MinNiceValue: -20,
		MaxNiceValue: 19,

		// Buffer Pool Configuration
		PreallocPercentage:      20,
		InputPreallocPercentage: 30,

		// Exponential Moving Average Configuration
		HistoricalWeight: 0.70,
		CurrentWeight:    0.30,

		// Sleep and Backoff Configuration
		CGOSleepMicroseconds: 50000,
		BackoffStart:         50 * time.Millisecond,

		// Protocol Magic Numbers
		InputMagicNumber:  0x4A4B4D49, // "JKMI" (JetKVM Microphone Input)
		OutputMagicNumber: 0x4A4B4F55, // "JKOU" (JetKVM Output)

		// Calculation Constants - Mathematical constants used throughout audio processing
		// Used in: Various components for calculations and conversions
		// Impact: Controls calculation accuracy and algorithm behavior

		// PercentageMultiplier defines multiplier for percentage calculations.
		// Used in: Throughout codebase for converting ratios to percentages
		// Impact: Must be 100 for standard percentage calculations.
		// Default 100 provides standard percentage conversion (0.5 * 100 = 50%).
		PercentageMultiplier: 100.0, // For percentage calculations

		// AveragingWeight defines weight for weighted averaging calculations.
		// Used in: metrics.go, adaptive_optimizer.go for smoothing values
		// Impact: Higher values give more weight to recent measurements.
		// Default 0.7 (70%) emphasizes recent values while maintaining stability.
		AveragingWeight: 0.7, // For weighted averaging calculations

		// ScalingFactor defines general scaling factor for various calculations.
		// Used in: adaptive_optimizer.go, quality_manager.go for scaling adjustments
		// Impact: Controls magnitude of adaptive adjustments and scaling operations.
		// Default 1.5 provides moderate scaling for quality and performance adjustments.
		ScalingFactor: 1.5, // For scaling calculations

		SmoothingFactor:         0.3, // For adaptive buffer smoothing
		CPUMemoryWeight:         0.5, // CPU factor weight in combined calculations
		MemoryWeight:            0.3, // Memory factor weight in combined calculations
		LatencyWeight:           0.2, // Latency factor weight in combined calculations
		PoolGrowthMultiplier:    2,   // Pool growth multiplier
		LatencyScalingFactor:    2.0, // Latency ratio scaling factor
		OptimizerAggressiveness: 0.7, // Optimizer aggressiveness factor

		// CGO Audio Processing Constants
		CGOUsleepMicroseconds:   1000,         // 1000 microseconds (1ms) for CGO usleep calls
		CGOPCMBufferSize:        1920,         // 1920 samples for PCM buffer (max 2ch*960)
		CGONanosecondsPerSecond: 1000000000.0, // 1000000000.0 for nanosecond conversions

		// Frontend Constants
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

		// Batch Processing Constants
		BatchProcessorFramesPerBatch: 4,                    // 4 frames per batch
		BatchProcessorTimeout:        5 * time.Millisecond, // 5ms timeout

		// Output Streaming Constants
		OutputStreamingFrameIntervalMS: 20, // 20ms frame interval (50 FPS)

		// IPC Constants
		IPCInitialBufferFrames: 500, // 500 frames for initial buffer

		// Event Constants
		EventTimeoutSeconds:      2,                          // 2 seconds for event timeout
		EventTimeFormatString:    "2006-01-02T15:04:05.000Z", // "2006-01-02T15:04:05.000Z" time format
		EventSubscriptionDelayMS: 100,                        // 100ms subscription delay

		// Input Processing Constants
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
		MinFrameSize:       64,              // 64 bytes minimum frame size
		FrameSizeTolerance: 512,             // 512 bytes frame size tolerance

		// Device Health Monitoring Configuration
		HealthCheckIntervalMS:    5000, // 5000ms (5s) health check interval
		HealthRecoveryThreshold:  3,    // 3 consecutive successes for recovery
		HealthLatencyThresholdMS: 100,  // 100ms latency threshold for health
		HealthErrorRateLimit:     0.1,  // 10% error rate limit for health

		// Latency Histogram Bucket Configuration
		LatencyBucket10ms:  10 * time.Millisecond,  // 10ms latency bucket
		LatencyBucket25ms:  25 * time.Millisecond,  // 25ms latency bucket
		LatencyBucket50ms:  50 * time.Millisecond,  // 50ms latency bucket
		LatencyBucket100ms: 100 * time.Millisecond, // 100ms latency bucket
		LatencyBucket250ms: 250 * time.Millisecond, // 250ms latency bucket
		LatencyBucket500ms: 500 * time.Millisecond, // 500ms latency bucket
		LatencyBucket1s:    1 * time.Second,        // 1s latency bucket
		LatencyBucket2s:    2 * time.Second,        // 2s latency bucket
	}
}

// Global configuration instance
var audioConfigInstance = DefaultAudioConfig()

// UpdateConfig allows runtime configuration updates
func UpdateConfig(newConfig *AudioConfigConstants) {
	// Validate the new configuration before applying it
	if err := ValidateAudioConfigConstants(newConfig); err != nil {
		// Log validation error and keep current configuration
		logger := logging.GetDefaultLogger().With().Str("component", "AudioConfig").Logger()
		logger.Error().Err(err).Msg("Configuration validation failed, keeping current configuration")
		return
	}

	audioConfigInstance = newConfig
	logger := logging.GetDefaultLogger().With().Str("component", "AudioConfig").Logger()
	logger.Info().Msg("Audio configuration updated successfully")
}

// GetConfig returns the current configuration
func GetConfig() *AudioConfigConstants {
	return audioConfigInstance
}
