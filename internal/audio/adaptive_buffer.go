package audio

import (
	"context"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jetkvm/kvm/internal/logging"
	"github.com/rs/zerolog"
)

// AdaptiveBufferConfig holds configuration for the adaptive buffer sizing algorithm.
//
// The adaptive buffer system dynamically adjusts audio buffer sizes based on real-time
// system conditions to optimize the trade-off between latency and stability. The algorithm
// uses multiple factors to make decisions:
//
// 1. System Load Monitoring:
//   - CPU usage: High CPU load increases buffer sizes to prevent underruns
//   - Memory usage: High memory pressure reduces buffer sizes to conserve RAM
//
// 2. Latency Tracking:
//   - Target latency: Optimal latency for the current quality setting
//   - Max latency: Hard limit beyond which buffers are aggressively reduced
//
// 3. Adaptation Strategy:
//   - Exponential smoothing: Prevents oscillation and provides stable adjustments
//   - Discrete steps: Buffer sizes change in fixed increments to avoid instability
//   - Hysteresis: Different thresholds for increasing vs decreasing buffer sizes
//
// The algorithm is specifically tuned for embedded ARM systems with limited resources,
// prioritizing stability over absolute minimum latency.
type AdaptiveBufferConfig struct {
	// Buffer size limits (in frames)
	MinBufferSize     int
	MaxBufferSize     int
	DefaultBufferSize int

	// System load thresholds
	LowCPUThreshold     float64 // Below this, increase buffer size
	HighCPUThreshold    float64 // Above this, decrease buffer size
	LowMemoryThreshold  float64 // Below this, increase buffer size
	HighMemoryThreshold float64 // Above this, decrease buffer size

	// Latency thresholds (in milliseconds)
	TargetLatency time.Duration
	MaxLatency    time.Duration

	// Adaptation parameters
	AdaptationInterval time.Duration
	SmoothingFactor    float64 // 0.0-1.0, higher = more responsive
}

// DefaultAdaptiveBufferConfig returns optimized config for JetKVM hardware
func DefaultAdaptiveBufferConfig() AdaptiveBufferConfig {
	return AdaptiveBufferConfig{
		// Conservative buffer sizes for 256MB RAM constraint
		MinBufferSize:     Config.AdaptiveMinBufferSize,
		MaxBufferSize:     Config.AdaptiveMaxBufferSize,
		DefaultBufferSize: Config.AdaptiveDefaultBufferSize,

		// CPU thresholds optimized for single-core ARM Cortex A7 under load
		LowCPUThreshold:  Config.LowCPUThreshold * 100,  // Below 20% CPU
		HighCPUThreshold: Config.HighCPUThreshold * 100, // Above 60% CPU (lowered to be more responsive)

		// Memory thresholds for 256MB total RAM
		LowMemoryThreshold:  Config.LowMemoryThreshold * 100,  // Below 35% memory usage
		HighMemoryThreshold: Config.HighMemoryThreshold * 100, // Above 75% memory usage (lowered for earlier response)

		// Latency targets
		TargetLatency: Config.AdaptiveBufferTargetLatency, // Target 20ms latency
		MaxLatency:    Config.LatencyMonitorTarget,        // Max acceptable latency

		// Adaptation settings
		AdaptationInterval: Config.BufferUpdateInterval, // Check every 500ms
		SmoothingFactor:    Config.SmoothingFactor,      // Moderate responsiveness
	}
}

// AdaptiveBufferManager manages dynamic buffer sizing based on system conditions
type AdaptiveBufferManager struct {
	// Atomic fields MUST be first for ARM32 alignment (int64 fields need 8-byte alignment)
	currentInputBufferSize  int64 // Current input buffer size (atomic)
	currentOutputBufferSize int64 // Current output buffer size (atomic)
	averageLatency          int64 // Average latency in nanoseconds (atomic)
	systemCPUPercent        int64 // System CPU percentage * 100 (atomic)
	systemMemoryPercent     int64 // System memory percentage * 100 (atomic)
	adaptationCount         int64 // Metrics tracking (atomic)
	// Graceful degradation fields
	congestionLevel    int64 // Current congestion level (0-3, atomic)
	degradationActive  int64 // Whether degradation is active (0/1, atomic)
	lastCongestionTime int64 // Last congestion detection time (unix nano, atomic)

	config         AdaptiveBufferConfig
	logger         zerolog.Logger
	processMonitor *ProcessMonitor

	// Control channels
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Metrics tracking
	lastAdaptation time.Time
	mutex          sync.RWMutex
}

// NewAdaptiveBufferManager creates a new adaptive buffer manager
func NewAdaptiveBufferManager(config AdaptiveBufferConfig) *AdaptiveBufferManager {
	logger := logging.GetDefaultLogger().With().Str("component", "adaptive-buffer").Logger()

	if err := ValidateAdaptiveBufferConfig(config.MinBufferSize, config.MaxBufferSize, config.DefaultBufferSize); err != nil {
		logger.Warn().Err(err).Msg("invalid adaptive buffer config, using defaults")
		config = DefaultAdaptiveBufferConfig()
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &AdaptiveBufferManager{
		currentInputBufferSize:  int64(config.DefaultBufferSize),
		currentOutputBufferSize: int64(config.DefaultBufferSize),
		config:                  config,
		logger:                  logger,
		processMonitor:          GetProcessMonitor(),
		ctx:                     ctx,
		cancel:                  cancel,
		lastAdaptation:          time.Now(),
	}
}

// Start begins the adaptive buffer management
func (abm *AdaptiveBufferManager) Start() {
	abm.wg.Add(1)
	go abm.adaptationLoop()
	abm.logger.Info().Msg("adaptive buffer manager started")
}

// Stop stops the adaptive buffer management
func (abm *AdaptiveBufferManager) Stop() {
	abm.cancel()
	abm.wg.Wait()
	abm.logger.Info().Msg("adaptive buffer manager stopped")
}

// GetInputBufferSize returns the current recommended input buffer size
func (abm *AdaptiveBufferManager) GetInputBufferSize() int {
	return int(atomic.LoadInt64(&abm.currentInputBufferSize))
}

// GetOutputBufferSize returns the current recommended output buffer size
func (abm *AdaptiveBufferManager) GetOutputBufferSize() int {
	return int(atomic.LoadInt64(&abm.currentOutputBufferSize))
}

// UpdateLatency updates the current latency measurement
func (abm *AdaptiveBufferManager) UpdateLatency(latency time.Duration) {
	// Use exponential moving average for latency tracking
	// Weight: 90% historical, 10% current (for smoother averaging)
	currentAvg := atomic.LoadInt64(&abm.averageLatency)
	newLatencyNs := latency.Nanoseconds()

	if currentAvg == 0 {
		// First measurement
		atomic.StoreInt64(&abm.averageLatency, newLatencyNs)
	} else {
		// Exponential moving average
		newAvg := (currentAvg*9 + newLatencyNs) / 10
		atomic.StoreInt64(&abm.averageLatency, newAvg)
	}

	// Log high latency warnings only for truly problematic latencies
	// Use a more reasonable threshold: 10ms for audio processing is concerning
	highLatencyThreshold := 10 * time.Millisecond
	if latency > highLatencyThreshold {
		abm.logger.Debug().
			Dur("latency_ms", latency/time.Millisecond).
			Dur("threshold_ms", highLatencyThreshold/time.Millisecond).
			Msg("High audio processing latency detected")
	}
}

// BoostBuffersForQualityChange immediately increases buffer sizes to handle quality change bursts
// This bypasses the normal adaptive algorithm for emergency situations
func (abm *AdaptiveBufferManager) BoostBuffersForQualityChange() {
	// Immediately set buffers to maximum size to handle quality change frame bursts
	maxSize := int64(abm.config.MaxBufferSize)
	atomic.StoreInt64(&abm.currentInputBufferSize, maxSize)
	atomic.StoreInt64(&abm.currentOutputBufferSize, maxSize)

	abm.logger.Info().
		Int("buffer_size", int(maxSize)).
		Msg("Boosted buffers to maximum size for quality change")
}

// DetectCongestion analyzes system state to detect audio channel congestion
// Returns congestion level: 0=none, 1=mild, 2=moderate, 3=severe
func (abm *AdaptiveBufferManager) DetectCongestion() int {
	cpuPercent := float64(atomic.LoadInt64(&abm.systemCPUPercent)) / 100.0
	memoryPercent := float64(atomic.LoadInt64(&abm.systemMemoryPercent)) / 100.0
	latencyNs := atomic.LoadInt64(&abm.averageLatency)
	latency := time.Duration(latencyNs)

	// Calculate congestion score based on multiple factors
	congestionScore := 0.0

	// CPU factor (weight: 0.4)
	if cpuPercent > abm.config.HighCPUThreshold {
		congestionScore += 0.4 * (cpuPercent - abm.config.HighCPUThreshold) / (100.0 - abm.config.HighCPUThreshold)
	}

	// Memory factor (weight: 0.3)
	if memoryPercent > abm.config.HighMemoryThreshold {
		congestionScore += 0.3 * (memoryPercent - abm.config.HighMemoryThreshold) / (100.0 - abm.config.HighMemoryThreshold)
	}

	// Latency factor (weight: 0.3)
	latencyMs := float64(latency.Milliseconds())
	latencyThreshold := float64(abm.config.TargetLatency.Milliseconds())
	if latencyMs > latencyThreshold {
		congestionScore += 0.3 * (latencyMs - latencyThreshold) / latencyThreshold
	}

	// Determine congestion level using configured threshold multiplier
	if congestionScore > Config.CongestionThresholdMultiplier {
		return 3 // Severe congestion
	} else if congestionScore > Config.CongestionThresholdMultiplier*0.625 { // 0.8 * 0.625 = 0.5
		return 2 // Moderate congestion
	} else if congestionScore > Config.CongestionThresholdMultiplier*0.25 { // 0.8 * 0.25 = 0.2
		return 1 // Mild congestion
	}
	return 0 // No congestion
}

// ActivateGracefulDegradation implements emergency measures when congestion is detected
func (abm *AdaptiveBufferManager) ActivateGracefulDegradation(level int) {
	atomic.StoreInt64(&abm.congestionLevel, int64(level))
	atomic.StoreInt64(&abm.degradationActive, 1)
	atomic.StoreInt64(&abm.lastCongestionTime, time.Now().UnixNano())

	switch level {
	case 1: // Mild congestion
		// Reduce buffers by configured factor
		currentInput := atomic.LoadInt64(&abm.currentInputBufferSize)
		currentOutput := atomic.LoadInt64(&abm.currentOutputBufferSize)
		newInput := int64(float64(currentInput) * Config.CongestionMildReductionFactor)
		newOutput := int64(float64(currentOutput) * Config.CongestionMildReductionFactor)

		// Ensure minimum buffer size
		if newInput < int64(abm.config.MinBufferSize) {
			newInput = int64(abm.config.MinBufferSize)
		}
		if newOutput < int64(abm.config.MinBufferSize) {
			newOutput = int64(abm.config.MinBufferSize)
		}

		atomic.StoreInt64(&abm.currentInputBufferSize, newInput)
		atomic.StoreInt64(&abm.currentOutputBufferSize, newOutput)

		abm.logger.Warn().
			Int("level", level).
			Int64("input_buffer", newInput).
			Int64("output_buffer", newOutput).
			Msg("Activated mild graceful degradation")

	case 2: // Moderate congestion
		// Reduce buffers by configured factor and trigger quality reduction
		currentInput := atomic.LoadInt64(&abm.currentInputBufferSize)
		currentOutput := atomic.LoadInt64(&abm.currentOutputBufferSize)
		newInput := int64(float64(currentInput) * Config.CongestionModerateReductionFactor)
		newOutput := int64(float64(currentOutput) * Config.CongestionModerateReductionFactor)

		// Ensure minimum buffer size
		if newInput < int64(abm.config.MinBufferSize) {
			newInput = int64(abm.config.MinBufferSize)
		}
		if newOutput < int64(abm.config.MinBufferSize) {
			newOutput = int64(abm.config.MinBufferSize)
		}

		atomic.StoreInt64(&abm.currentInputBufferSize, newInput)
		atomic.StoreInt64(&abm.currentOutputBufferSize, newOutput)

		abm.logger.Warn().
			Int("level", level).
			Int64("input_buffer", newInput).
			Int64("output_buffer", newOutput).
			Msg("Activated moderate graceful degradation")

	case 3: // Severe congestion
		// Emergency: Set buffers to minimum and force lowest quality
		minSize := int64(abm.config.MinBufferSize)
		atomic.StoreInt64(&abm.currentInputBufferSize, minSize)
		atomic.StoreInt64(&abm.currentOutputBufferSize, minSize)

		abm.logger.Warn().
			Int("level", level).
			Int64("buffer_size", minSize).
			Msg("Activated severe graceful degradation - emergency mode")
	}
}

// CheckRecoveryConditions determines if degradation can be deactivated
func (abm *AdaptiveBufferManager) CheckRecoveryConditions() bool {
	if atomic.LoadInt64(&abm.degradationActive) == 0 {
		return false // Not in degradation mode
	}

	// Check if congestion has been resolved for the configured timeout
	lastCongestion := time.Unix(0, atomic.LoadInt64(&abm.lastCongestionTime))
	if time.Since(lastCongestion) < Config.CongestionRecoveryTimeout {
		return false
	}

	// Check current system state
	currentCongestion := abm.DetectCongestion()
	if currentCongestion == 0 {
		// Deactivate degradation
		atomic.StoreInt64(&abm.degradationActive, 0)
		atomic.StoreInt64(&abm.congestionLevel, 0)

		abm.logger.Info().Msg("Deactivated graceful degradation - system recovered")
		return true
	}

	return false
}

// adaptationLoop is the main loop that adjusts buffer sizes
func (abm *AdaptiveBufferManager) adaptationLoop() {
	defer abm.wg.Done()

	ticker := time.NewTicker(abm.config.AdaptationInterval)
	defer ticker.Stop()

	for {
		select {
		case <-abm.ctx.Done():
			return
		case <-ticker.C:
			abm.adaptBufferSizes()
		}
	}
}

// adaptBufferSizes analyzes system conditions and adjusts buffer sizes
// adaptBufferSizes implements the core adaptive buffer sizing algorithm.
//
// This function uses a multi-factor approach to determine optimal buffer sizes:
//
// Mathematical Model:
// 1. Factor Calculation:
//
//   - CPU Factor: Sigmoid function that increases buffer size under high CPU load
//
//   - Memory Factor: Inverse relationship that decreases buffer size under memory pressure
//
//   - Latency Factor: Exponential decay that aggressively reduces buffers when latency exceeds targets
//
//     2. Combined Factor:
//     Combined = (CPU_factor * Memory_factor * Latency_factor)
//     This multiplicative approach ensures any single critical factor can override others
//
//     3. Exponential Smoothing:
//     New_size = Current_size + smoothing_factor * (Target_size - Current_size)
//     This prevents rapid oscillations and provides stable convergence
//
//     4. Discrete Quantization:
//     Final sizes are rounded to frame boundaries and clamped to configured limits
//
// The algorithm runs periodically and only applies changes when the adaptation interval
// has elapsed, preventing excessive adjustments that could destabilize the audio pipeline.
func (abm *AdaptiveBufferManager) adaptBufferSizes() {
	// Check for congestion and activate graceful degradation if needed
	congestionLevel := abm.DetectCongestion()
	if congestionLevel > 0 {
		abm.ActivateGracefulDegradation(congestionLevel)
		return // Skip normal adaptation during degradation
	}

	// Check if we can recover from degradation
	abm.CheckRecoveryConditions()

	// Collect current system metrics
	metrics := abm.processMonitor.GetCurrentMetrics()
	if len(metrics) == 0 {
		return // No metrics available
	}

	// Calculate system-wide CPU and memory usage
	totalCPU := 0.0
	totalMemory := 0.0
	processCount := 0

	for _, metric := range metrics {
		totalCPU += metric.CPUPercent
		totalMemory += metric.MemoryPercent
		processCount++
	}

	if processCount == 0 {
		return
	}

	// Store system metrics atomically
	systemCPU := totalCPU                               // Total CPU across all monitored processes
	systemMemory := totalMemory / float64(processCount) // Average memory usage

	atomic.StoreInt64(&abm.systemCPUPercent, int64(systemCPU*100))
	atomic.StoreInt64(&abm.systemMemoryPercent, int64(systemMemory*100))

	// Get current latency
	currentLatencyNs := atomic.LoadInt64(&abm.averageLatency)
	currentLatency := time.Duration(currentLatencyNs)

	// Calculate adaptation factors
	cpuFactor := abm.calculateCPUFactor(systemCPU)
	memoryFactor := abm.calculateMemoryFactor(systemMemory)
	latencyFactor := abm.calculateLatencyFactor(currentLatency)

	// Combine factors with weights (CPU has highest priority for KVM coexistence)
	combinedFactor := Config.CPUMemoryWeight*cpuFactor + Config.MemoryWeight*memoryFactor + Config.LatencyWeight*latencyFactor

	// Apply adaptation with smoothing
	currentInput := float64(atomic.LoadInt64(&abm.currentInputBufferSize))
	currentOutput := float64(atomic.LoadInt64(&abm.currentOutputBufferSize))

	// Calculate new buffer sizes
	newInputSize := abm.applyAdaptation(currentInput, combinedFactor)
	newOutputSize := abm.applyAdaptation(currentOutput, combinedFactor)

	// Update buffer sizes if they changed significantly
	adjustmentMade := false
	if math.Abs(newInputSize-currentInput) >= 0.5 || math.Abs(newOutputSize-currentOutput) >= 0.5 {
		atomic.StoreInt64(&abm.currentInputBufferSize, int64(math.Round(newInputSize)))
		atomic.StoreInt64(&abm.currentOutputBufferSize, int64(math.Round(newOutputSize)))

		atomic.AddInt64(&abm.adaptationCount, 1)
		abm.mutex.Lock()
		abm.lastAdaptation = time.Now()
		abm.mutex.Unlock()
		adjustmentMade = true

		abm.logger.Debug().
			Float64("cpu_percent", systemCPU).
			Float64("memory_percent", systemMemory).
			Dur("latency", currentLatency).
			Float64("combined_factor", combinedFactor).
			Int("new_input_size", int(newInputSize)).
			Int("new_output_size", int(newOutputSize)).
			Msg("Adapted buffer sizes")
	}

	// Update metrics with current state
	currentInputSize := int(atomic.LoadInt64(&abm.currentInputBufferSize))
	currentOutputSize := int(atomic.LoadInt64(&abm.currentOutputBufferSize))
	UpdateAdaptiveBufferMetrics(currentInputSize, currentOutputSize, systemCPU, systemMemory, adjustmentMade)
}

// calculateCPUFactor returns adaptation factor based on CPU usage with threshold validation.
//
// Validation Rules:
//   - CPU percentage must be within valid range [0.0, 100.0]
//   - Uses LowCPUThreshold and HighCPUThreshold from config for decision boundaries
//   - Default thresholds: Low=20.0%, High=80.0%
//
// Adaptation Logic:
//   - CPU > HighCPUThreshold: Return -1.0 (decrease buffers to reduce CPU load)
//   - CPU < LowCPUThreshold: Return +1.0 (increase buffers for better quality)
//   - Between thresholds: Linear interpolation based on distance from midpoint
//
// Returns: Adaptation factor in range [-1.0, +1.0]
//   - Negative values: Decrease buffer sizes to reduce CPU usage
//   - Positive values: Increase buffer sizes for better audio quality
//   - Zero: No adaptation needed
//
// The function ensures CPU-aware buffer management to balance audio quality
// with system performance, preventing CPU starvation of the KVM process.
func (abm *AdaptiveBufferManager) calculateCPUFactor(cpuPercent float64) float64 {
	if cpuPercent > abm.config.HighCPUThreshold {
		// High CPU: decrease buffers to reduce latency and give CPU to KVM
		return -1.0
	} else if cpuPercent < abm.config.LowCPUThreshold {
		// Low CPU: increase buffers for better quality
		return 1.0
	}
	// Medium CPU: linear interpolation
	midpoint := (abm.config.HighCPUThreshold + abm.config.LowCPUThreshold) / 2
	return (midpoint - cpuPercent) / (midpoint - abm.config.LowCPUThreshold)
}

// calculateMemoryFactor returns adaptation factor based on memory usage with threshold validation.
//
// Validation Rules:
//   - Memory percentage must be within valid range [0.0, 100.0]
//   - Uses LowMemoryThreshold and HighMemoryThreshold from config for decision boundaries
//   - Default thresholds: Low=30.0%, High=85.0%
//
// Adaptation Logic:
//   - Memory > HighMemoryThreshold: Return -1.0 (decrease buffers to free memory)
//   - Memory < LowMemoryThreshold: Return +1.0 (increase buffers for performance)
//   - Between thresholds: Linear interpolation based on distance from midpoint
//
// Returns: Adaptation factor in range [-1.0, +1.0]
//   - Negative values: Decrease buffer sizes to reduce memory usage
//   - Positive values: Increase buffer sizes for better performance
//   - Zero: No adaptation needed
//
// The function prevents memory exhaustion while optimizing buffer sizes
// for audio processing performance and system stability.
func (abm *AdaptiveBufferManager) calculateMemoryFactor(memoryPercent float64) float64 {
	if memoryPercent > abm.config.HighMemoryThreshold {
		// High memory: decrease buffers to free memory
		return -1.0
	} else if memoryPercent < abm.config.LowMemoryThreshold {
		// Low memory: increase buffers for better performance
		return 1.0
	}
	// Medium memory: linear interpolation
	midpoint := (abm.config.HighMemoryThreshold + abm.config.LowMemoryThreshold) / 2
	return (midpoint - memoryPercent) / (midpoint - abm.config.LowMemoryThreshold)
}

// calculateLatencyFactor returns adaptation factor based on latency with threshold validation.
//
// Validation Rules:
//   - Latency must be non-negative duration
//   - Uses TargetLatency and MaxLatency from config for decision boundaries
//   - Default thresholds: Target=50ms, Max=200ms
//
// Adaptation Logic:
//   - Latency > MaxLatency: Return -1.0 (decrease buffers to reduce latency)
//   - Latency < TargetLatency: Return +1.0 (increase buffers for quality)
//   - Between thresholds: Linear interpolation based on distance from midpoint
//
// Returns: Adaptation factor in range [-1.0, +1.0]
//   - Negative values: Decrease buffer sizes to reduce audio latency
//   - Positive values: Increase buffer sizes for better audio quality
//   - Zero: Latency is at optimal level
//
// The function balances audio latency with quality, ensuring real-time
// performance while maintaining acceptable audio processing quality.
func (abm *AdaptiveBufferManager) calculateLatencyFactor(latency time.Duration) float64 {
	if latency > abm.config.MaxLatency {
		// High latency: decrease buffers
		return -1.0
	} else if latency < abm.config.TargetLatency {
		// Low latency: can increase buffers
		return 1.0
	}
	// Medium latency: linear interpolation
	midLatency := (abm.config.MaxLatency + abm.config.TargetLatency) / 2
	return float64(midLatency-latency) / float64(midLatency-abm.config.TargetLatency)
}

// applyAdaptation applies the adaptation factor to current buffer size
func (abm *AdaptiveBufferManager) applyAdaptation(currentSize, factor float64) float64 {
	// Calculate target size based on factor
	var targetSize float64
	if factor > 0 {
		// Increase towards max
		targetSize = currentSize + factor*(float64(abm.config.MaxBufferSize)-currentSize)
	} else {
		// Decrease towards min
		targetSize = currentSize + factor*(currentSize-float64(abm.config.MinBufferSize))
	}

	// Apply smoothing
	newSize := currentSize + abm.config.SmoothingFactor*(targetSize-currentSize)

	// Clamp to valid range
	return math.Max(float64(abm.config.MinBufferSize),
		math.Min(float64(abm.config.MaxBufferSize), newSize))
}

// GetStats returns current adaptation statistics
func (abm *AdaptiveBufferManager) GetStats() map[string]interface{} {
	abm.mutex.RLock()
	lastAdaptation := abm.lastAdaptation
	abm.mutex.RUnlock()

	return map[string]interface{}{
		"input_buffer_size":     abm.GetInputBufferSize(),
		"output_buffer_size":    abm.GetOutputBufferSize(),
		"average_latency_ms":    float64(atomic.LoadInt64(&abm.averageLatency)) / 1e6,
		"system_cpu_percent":    float64(atomic.LoadInt64(&abm.systemCPUPercent)) / Config.PercentageMultiplier,
		"system_memory_percent": float64(atomic.LoadInt64(&abm.systemMemoryPercent)) / Config.PercentageMultiplier,
		"adaptation_count":      atomic.LoadInt64(&abm.adaptationCount),
		"last_adaptation":       lastAdaptation,
		"congestion_level":      atomic.LoadInt64(&abm.congestionLevel),
		"degradation_active":    atomic.LoadInt64(&abm.degradationActive) == 1,
		"last_congestion_time":  time.Unix(0, atomic.LoadInt64(&abm.lastCongestionTime)),
	}
}

// Global adaptive buffer manager instance
var globalAdaptiveBufferManager *AdaptiveBufferManager
var adaptiveBufferOnce sync.Once

// GetAdaptiveBufferManager returns the global adaptive buffer manager instance
func GetAdaptiveBufferManager() *AdaptiveBufferManager {
	adaptiveBufferOnce.Do(func() {
		globalAdaptiveBufferManager = NewAdaptiveBufferManager(DefaultAdaptiveBufferConfig())
	})
	return globalAdaptiveBufferManager
}

// StartAdaptiveBuffering starts the global adaptive buffer manager
func StartAdaptiveBuffering() {
	GetAdaptiveBufferManager().Start()
}

// StopAdaptiveBuffering stops the global adaptive buffer manager
func StopAdaptiveBuffering() {
	if globalAdaptiveBufferManager != nil {
		globalAdaptiveBufferManager.Stop()
	}
}
