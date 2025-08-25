package audio

import "time"

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

	MaxConsecutiveErrors int
	MaxRetryAttempts     int

	// Timing Constants
	DefaultSleepDuration       time.Duration // 100ms
	ShortSleepDuration         time.Duration // 10ms
	LongSleepDuration          time.Duration // 200ms
	DefaultTickerInterval      time.Duration // 100ms
	BufferUpdateInterval       time.Duration // 500ms
	StatsUpdateInterval        time.Duration // 5s
	SupervisorTimeout          time.Duration // 10s
	InputSupervisorTimeout     time.Duration // 5s
	ShortTimeout               time.Duration // 5ms
	MediumTimeout              time.Duration // 50ms
	BatchProcessingDelay       time.Duration // 10ms
	AdaptiveOptimizerStability time.Duration // 10s
	MaxLatencyTarget           time.Duration // 50ms
	LatencyMonitorTarget       time.Duration // 50ms

	// Adaptive Buffer Configuration
	LowCPUThreshold     float64       // 20% CPU threshold
	HighCPUThreshold    float64       // 60% CPU threshold
	LowMemoryThreshold  float64       // 50% memory threshold
	HighMemoryThreshold float64       // 75% memory threshold
	TargetLatency       time.Duration // 20ms target latency

	// Adaptive Optimizer Configuration
	CooldownPeriod    time.Duration // 30s cooldown period
	RollbackThreshold time.Duration // 300ms rollback threshold
	LatencyTarget     time.Duration // 50ms latency target

	// Latency Monitor Configuration
	MaxLatencyThreshold time.Duration // 200ms max latency
	JitterThreshold     time.Duration // 20ms jitter threshold

	// Microphone Contention Configuration
	MicContentionTimeout time.Duration // 200ms contention timeout

	// Priority Scheduler Configuration
	MinNiceValue int // -20 minimum nice value
	MaxNiceValue int // 19 maximum nice value

	// Buffer Pool Configuration
	PreallocPercentage      int // 20% preallocation percentage
	InputPreallocPercentage int // 30% input preallocation percentage

	// Exponential Moving Average Configuration
	HistoricalWeight float64 // 70% historical weight
	CurrentWeight    float64 // 30% current weight

	// Sleep and Backoff Configuration
	CGOSleepMicroseconds int           // 50000 microseconds (50ms)
	BackoffStart         time.Duration // 50ms initial backoff

	// Protocol Magic Numbers
	InputMagicNumber  uint32 // 0x4A4B4D49 "JKMI" (JetKVM Microphone Input)
	OutputMagicNumber uint32 // 0x4A4B4F55 "JKOU" (JetKVM Output)

	// Calculation Constants
	PercentageMultiplier    float64 // 100.0 for percentage calculations
	AveragingWeight         float64 // 0.7 for weighted averaging calculations
	ScalingFactor           float64 // 1.5 for scaling calculations
	SmoothingFactor         float64 // 0.3 for adaptive buffer smoothing
	CPUMemoryWeight         float64 // 0.5 for CPU factor in combined calculations
	MemoryWeight            float64 // 0.3 for memory factor in combined calculations
	LatencyWeight           float64 // 0.2 for latency factor in combined calculations
	PoolGrowthMultiplier    int     // 2x growth multiplier for pool sizes
	LatencyScalingFactor    float64 // 2.0 for latency ratio scaling
	OptimizerAggressiveness float64 // 0.7 for optimizer aggressiveness
}

// DefaultAudioConfig returns the default configuration constants
// These values are carefully chosen based on JetKVM's embedded ARM environment,
// real-time audio requirements, and extensive testing for optimal performance.
func DefaultAudioConfig() *AudioConfigConstants {
	return &AudioConfigConstants{
		// Audio Quality Presets
		// Rationale: 4096 bytes accommodates largest expected audio frames with safety margin
		// for high-quality audio while preventing buffer overruns on embedded systems
		MaxAudioFrameSize: 4096,

		// Opus Encoding Parameters
		// Rationale: 128kbps provides excellent quality for KVM audio while maintaining
		// reasonable bandwidth usage over network connections
		OpusBitrate: 128000,
		// Rationale: Maximum complexity (10) ensures best quality encoding, acceptable
		// on modern ARM processors with sufficient CPU headroom
		OpusComplexity: 10,
		// Rationale: VBR enabled (1) optimizes bitrate based on audio content complexity,
		// reducing bandwidth for simple audio while maintaining quality for complex audio
		OpusVBR: 1,
		// Rationale: Unconstrained VBR (0) allows maximum bitrate flexibility for
		// optimal quality-bandwidth balance in varying network conditions
		OpusVBRConstraint: 0,
		// Rationale: DTX disabled (0) ensures consistent audio stream for KVM applications
		// where silence detection might interfere with system audio monitoring
		OpusDTX: 0,

		// Audio Parameters
		// Rationale: 48kHz sample rate is professional audio standard, provides full
		// frequency range (up to 24kHz) for accurate system audio reproduction
		SampleRate: 48000,
		// Rationale: Stereo (2 channels) captures full system audio including spatial
		// information and stereo effects common in modern operating systems
		Channels: 2,
		// Rationale: 960 samples = 20ms frames at 48kHz, optimal balance between
		// latency (low enough for real-time) and efficiency (reduces packet overhead)
		FrameSize: 960,
		// Rationale: 4000 bytes accommodates compressed Opus frames with overhead,
		// prevents packet fragmentation while allowing for quality variations
		MaxPacketSize: 4000,

		// Audio Quality Bitrates (kbps)
		// Rationale: Low quality optimized for bandwidth-constrained connections,
		// output higher than input as system audio typically more complex than microphone
		AudioQualityLowOutputBitrate: 32,
		AudioQualityLowInputBitrate:  16,
		// Rationale: Medium quality balances bandwidth and quality for typical usage,
		// suitable for most KVM scenarios with reasonable network connections
		AudioQualityMediumOutputBitrate: 64,
		AudioQualityMediumInputBitrate:  32,
		// Rationale: High quality for excellent audio fidelity, matches default Opus
		// bitrate for professional applications requiring clear audio reproduction
		AudioQualityHighOutputBitrate: 128,
		AudioQualityHighInputBitrate:  64,
		// Rationale: Ultra quality for audiophile-grade reproduction, suitable for
		// high-bandwidth connections where audio quality is paramount
		AudioQualityUltraOutputBitrate: 192,
		AudioQualityUltraInputBitrate:  96,

		// Audio Quality Sample Rates (Hz)
		// Rationale: 22.05kHz captures frequencies up to 11kHz, sufficient for speech
		// and basic audio while minimizing processing load on constrained connections
		AudioQualityLowSampleRate: 22050,
		// Rationale: 44.1kHz CD-quality standard, captures full audible range up to
		// 22kHz, excellent balance of quality and processing requirements
		AudioQualityMediumSampleRate: 44100,
		// Rationale: 16kHz optimized for voice/microphone input, captures speech
		// frequencies (300-8000Hz) efficiently while reducing bandwidth
		AudioQualityMicLowSampleRate: 16000,

		// Audio Quality Frame Sizes
		// Rationale: 40ms frames reduce processing overhead for low-quality mode,
		// acceptable latency increase for bandwidth-constrained scenarios
		AudioQualityLowFrameSize: 40 * time.Millisecond,
		// Rationale: 20ms frames provide good balance of latency and efficiency,
		// standard for real-time audio applications
		AudioQualityMediumFrameSize: 20 * time.Millisecond,
		AudioQualityHighFrameSize:   20 * time.Millisecond,
		// Rationale: 10ms frames minimize latency for ultra-responsive audio,
		// suitable for applications requiring immediate audio feedback
		AudioQualityUltraFrameSize: 10 * time.Millisecond,

		// Audio Quality Channels
		// Rationale: Mono (1 channel) reduces bandwidth by 50% for low-quality mode,
		// acceptable for basic audio monitoring where stereo separation not critical
		AudioQualityLowChannels: 1,
		// Rationale: Stereo (2 channels) preserves spatial audio information for
		// medium/high/ultra quality modes, essential for modern system audio
		AudioQualityMediumChannels: 2,
		AudioQualityHighChannels:   2,
		AudioQualityUltraChannels:  2,

		// CGO Audio Constants
		// Rationale: 96kbps provides good quality for CGO layer while being more
		// conservative than main Opus bitrate, suitable for embedded processing
		CGOOpusBitrate: 96000,
		// Rationale: Lower complexity (3) reduces CPU load in CGO layer while
		// maintaining acceptable quality for real-time processing
		CGOOpusComplexity: 3,
		// Rationale: VBR enabled (1) allows bitrate adaptation based on content
		// complexity, optimizing bandwidth usage in CGO processing
		CGOOpusVBR: 1,
		// Rationale: Constrained VBR (1) limits bitrate variations for more
		// predictable processing load in embedded environment
		CGOOpusVBRConstraint: 1,
		// Rationale: OPUS_SIGNAL_MUSIC (3) optimizes encoding for general audio
		// content including system sounds, music, and mixed audio
		CGOOpusSignalType: 3, // OPUS_SIGNAL_MUSIC
		// Rationale: OPUS_BANDWIDTH_FULLBAND (1105) enables full 20kHz bandwidth
		// for complete audio spectrum reproduction
		CGOOpusBandwidth: 1105, // OPUS_BANDWIDTH_FULLBAND
		// Rationale: DTX disabled (0) ensures consistent audio stream without
		// silence detection that could interfere with system audio monitoring
		CGOOpusDTX: 0,
		// Rationale: 48kHz sample rate matches main audio parameters for
		// consistency and professional audio quality
		CGOSampleRate: 48000,
		// Rationale: Stereo (2 channels) maintains spatial audio information
		// throughout the CGO processing pipeline
		CGOChannels: 2,
		// Rationale: 960 samples (20ms at 48kHz) matches main frame size for
		// consistent timing and efficient processing
		CGOFrameSize: 960,
		// Rationale: 1500 bytes accommodates Ethernet MTU constraints while
		// providing sufficient space for compressed audio packets
		CGOMaxPacketSize: 1500,

		// Input IPC Constants
		// Rationale: 48kHz sample rate ensures high-quality microphone input
		// capture matching system audio output for consistent quality
		InputIPCSampleRate: 48000,
		// Rationale: Stereo (2 channels) captures spatial microphone information
		// and maintains compatibility with stereo input devices
		InputIPCChannels: 2,
		// Rationale: 960 samples (20ms) provides optimal balance between latency
		// and processing efficiency for real-time microphone input
		InputIPCFrameSize: 960,

		// Output IPC Constants
		// Rationale: 4096 bytes accommodates largest audio frames with safety
		// margin, preventing buffer overruns in IPC communication
		OutputMaxFrameSize: 4096,
		// Rationale: 10ms timeout provides quick response for real-time audio
		// while preventing blocking in high-performance scenarios
		OutputWriteTimeout: 10 * time.Millisecond,
		// Rationale: Allow up to 50 dropped frames before error handling,
		// providing resilience against temporary network/processing issues
		OutputMaxDroppedFrames: 50,
		// Rationale: 17-byte header provides sufficient space for frame metadata
		// including timestamps, sequence numbers, and format information
		OutputHeaderSize: 17,
		// Rationale: 128 message pool size balances memory usage with throughput
		// for efficient audio streaming without excessive buffering
		OutputMessagePoolSize: 128,

		// Socket Buffer Constants
		// Rationale: 128KB optimal buffer provides good balance between memory
		// usage and network throughput for typical audio streaming scenarios
		SocketOptimalBuffer: 131072, // 128KB
		// Rationale: 256KB maximum buffer accommodates burst traffic and high
		// bitrate audio while preventing excessive memory consumption
		SocketMaxBuffer: 262144, // 256KB
		// Rationale: 32KB minimum buffer ensures adequate buffering for basic
		// audio streaming while minimizing memory footprint
		SocketMinBuffer: 32768, // 32KB

		// Scheduling Policy Constants
		// Rationale: SCHED_NORMAL (0) provides standard time-sharing scheduling
		// suitable for non-critical audio processing tasks
		SchedNormal: 0,
		// Rationale: SCHED_FIFO (1) provides real-time first-in-first-out
		// scheduling for critical audio processing requiring deterministic timing
		SchedFIFO: 1,
		// Rationale: SCHED_RR (2) provides real-time round-robin scheduling
		// for balanced real-time processing with time slicing
		SchedRR: 2,

		// Real-time Priority Levels
		// Rationale: Priority 80 ensures audio processing gets highest CPU
		// priority for latency-critical operations without starving system
		RTAudioHighPriority: 80,
		// Rationale: Priority 60 provides elevated priority for important
		// audio tasks while allowing higher priority system operations
		RTAudioMediumPriority: 60,
		// Rationale: Priority 40 provides moderate real-time priority for
		// audio tasks that need responsiveness but aren't latency-critical
		RTAudioLowPriority: 40,
		// Rationale: Priority 0 represents normal scheduling priority for
		// non-real-time audio processing tasks
		RTNormalPriority: 0,

		// Process Management
		// Rationale: 5 restart attempts provides resilience against transient
		// failures while preventing infinite restart loops
		MaxRestartAttempts: 5,
		// Rationale: 5-minute restart window allows recovery from temporary
		// issues while resetting attempt counter for long-term stability
		RestartWindow: 5 * time.Minute,
		// Rationale: 1-second initial restart delay prevents rapid restart
		// cycles while allowing quick recovery from brief failures
		RestartDelay: 1 * time.Second,
		// Rationale: 30-second maximum delay prevents excessive wait times
		// while implementing exponential backoff for persistent failures
		MaxRestartDelay: 30 * time.Second,

		// Buffer Management
		// Rationale: 1MB preallocation provides substantial buffer space for
		// high-throughput audio processing while remaining reasonable for embedded systems
		PreallocSize: 1024 * 1024, // 1MB
		// Rationale: 100 pool size limits memory usage while providing adequate
		// object pooling for efficient memory management
		MaxPoolSize: 100,
		// Rationale: 256 message pool size balances memory usage with message
		// throughput for efficient IPC communication
		MessagePoolSize: 256,
		// Rationale: 256KB optimal socket buffer provides good network performance
		// for audio streaming without excessive memory consumption
		OptimalSocketBuffer: 262144, // 256KB
		// Rationale: 1MB maximum socket buffer accommodates burst traffic and
		// high-bitrate audio while preventing excessive memory usage
		MaxSocketBuffer: 1048576, // 1MB
		// Rationale: 8KB minimum socket buffer ensures basic network buffering
		// while minimizing memory footprint for low-bandwidth scenarios
		MinSocketBuffer: 8192, // 8KB
		// Rationale: 500 channel buffer size provides adequate buffering for
		// inter-goroutine communication without blocking
		ChannelBufferSize: 500, // Channel buffer size for processing
		// Rationale: 1500 audio frame pool size accommodates frame reuse for
		// efficient memory management in high-throughput scenarios
		AudioFramePoolSize: 1500, // Audio frame pool size
		// Rationale: 4096-byte page size aligns with system memory pages for
		// optimal memory allocation and cache performance
		PageSize: 4096, // Memory page size
		// Rationale: 500 initial buffer frames provides adequate startup buffering
		// without excessive memory allocation during initialization
		InitialBufferFrames: 500, // Initial buffer size in frames
		// Rationale: 1024*1024 divisor provides standard MB conversion for
		// memory usage calculations and reporting
		BytesToMBDivisor: 1024 * 1024, // Divisor for converting bytes to MB
		// Rationale: 1276 bytes minimum buffer accommodates smallest CGO audio
		// read/encode operations while ensuring adequate processing space
		MinReadEncodeBuffer: 1276, // Minimum buffer size for CGO audio read/encode
		// Rationale: 4096 bytes maximum buffer provides sufficient space for
		// largest CGO audio decode/write operations without excessive allocation
		MaxDecodeWriteBuffer: 4096, // Maximum buffer size for CGO audio decode/write

		// IPC Configuration
		// Rationale: 0xDEADBEEF magic number provides distinctive header for
		// IPC message validation and debugging purposes
		MagicNumber: 0xDEADBEEF,
		// Rationale: 4096 bytes maximum frame size accommodates largest audio
		// frames while preventing excessive memory allocation
		MaxFrameSize: 4096,
		// Rationale: 5-second write timeout provides reasonable wait time for
		// IPC operations while preventing indefinite blocking
		WriteTimeout: 5 * time.Second,
		// Rationale: Allow up to 10 dropped frames before error handling,
		// balancing audio continuity with quality maintenance
		MaxDroppedFrames: 10,
		// Rationale: 8-byte header provides sufficient space for basic IPC
		// metadata including message type and size information
		HeaderSize: 8,

		// Monitoring and Metrics
		// Rationale: 1-second metrics update interval provides timely monitoring
		// without excessive overhead for performance tracking
		MetricsUpdateInterval: 1000 * time.Millisecond,
		// Rationale: 0.1 EMA alpha provides smooth exponential moving average
		// with 90% weight on historical data for stable metrics
		EMAAlpha: 0.1,
		// Rationale: 10 warmup samples allow metrics to stabilize before
		// making optimization decisions based on performance data
		WarmupSamples: 10,
		// Rationale: 5-second log throttle interval prevents log spam while
		// ensuring important events are captured for debugging
		LogThrottleInterval: 5 * time.Second,
		// Rationale: 100 metrics channel buffer provides adequate buffering
		// for metrics collection without blocking performance monitoring
		MetricsChannelBuffer: 100,
		// Rationale: 100 latency measurements provide sufficient history for
		// statistical analysis and trend detection
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

		// Performance Tuning
		CPUThresholdLow:      0.20,
		CPUThresholdMedium:   0.60,
		CPUThresholdHigh:     0.75,
		MemoryThresholdLow:   0.30,
		MemoryThresholdMed:   0.60,
		MemoryThresholdHigh:  0.80,
		LatencyThresholdLow:  20 * time.Millisecond,
		LatencyThresholdHigh: 50 * time.Millisecond,
		CPUFactor:            0.7,
		MemoryFactor:         0.8,
		LatencyFactor:        0.9,
		InputSizeThreshold:   1024,
		OutputSizeThreshold:  2048,
		TargetLevel:          0.5,

		// Priority Scheduling
		AudioHighPriority:   -10,
		AudioMediumPriority: -5,
		AudioLowPriority:    0,
		NormalPriority:      0,
		NiceValue:           -10,

		// Error Handling
		MaxRetries:           3,
		RetryDelay:           100 * time.Millisecond,
		MaxRetryDelay:        5 * time.Second,
		BackoffMultiplier:    2.0,
		ErrorChannelSize:     50,
		MaxConsecutiveErrors: 5,
		MaxRetryAttempts:     3,

		// Timing Constants
		DefaultSleepDuration:       100 * time.Millisecond,
		ShortSleepDuration:         10 * time.Millisecond,
		LongSleepDuration:          200 * time.Millisecond,
		DefaultTickerInterval:      100 * time.Millisecond,
		BufferUpdateInterval:       500 * time.Millisecond,
		StatsUpdateInterval:        5 * time.Second,
		SupervisorTimeout:          10 * time.Second,
		InputSupervisorTimeout:     5 * time.Second,
		ShortTimeout:               5 * time.Millisecond,
		MediumTimeout:              50 * time.Millisecond,
		BatchProcessingDelay:       10 * time.Millisecond,
		AdaptiveOptimizerStability: 10 * time.Second,
		MaxLatencyTarget:           50 * time.Millisecond,
		LatencyMonitorTarget:       50 * time.Millisecond,

		// Adaptive Buffer Configuration
		LowCPUThreshold:     0.20,
		HighCPUThreshold:    0.60,
		LowMemoryThreshold:  0.50,
		HighMemoryThreshold: 0.75,
		TargetLatency:       20 * time.Millisecond,

		// Adaptive Optimizer Configuration
		CooldownPeriod:    30 * time.Second,
		RollbackThreshold: 300 * time.Millisecond,
		LatencyTarget:     50 * time.Millisecond,

		// Latency Monitor Configuration
		MaxLatencyThreshold: 200 * time.Millisecond,
		JitterThreshold:     20 * time.Millisecond,

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
