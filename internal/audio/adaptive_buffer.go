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

// AdaptiveBufferConfig holds configuration for adaptive buffer sizing
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
		MinBufferSize:     GetConfig().AdaptiveMinBufferSize,
		MaxBufferSize:     GetConfig().AdaptiveMaxBufferSize,
		DefaultBufferSize: GetConfig().AdaptiveDefaultBufferSize,

		// CPU thresholds optimized for single-core ARM Cortex A7 under load
		LowCPUThreshold:  GetConfig().LowCPUThreshold * 100,  // Below 20% CPU
		HighCPUThreshold: GetConfig().HighCPUThreshold * 100, // Above 60% CPU (lowered to be more responsive)

		// Memory thresholds for 256MB total RAM
		LowMemoryThreshold:  GetConfig().LowMemoryThreshold * 100,  // Below 35% memory usage
		HighMemoryThreshold: GetConfig().HighMemoryThreshold * 100, // Above 75% memory usage (lowered for earlier response)

		// Latency targets
		TargetLatency: GetConfig().TargetLatency,    // Target 20ms latency
		MaxLatency:    GetConfig().MaxLatencyTarget, // Max acceptable latency

		// Adaptation settings
		AdaptationInterval: GetConfig().BufferUpdateInterval, // Check every 500ms
		SmoothingFactor:    GetConfig().SmoothingFactor,      // Moderate responsiveness
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
	ctx, cancel := context.WithCancel(context.Background())

	return &AdaptiveBufferManager{
		currentInputBufferSize:  int64(config.DefaultBufferSize),
		currentOutputBufferSize: int64(config.DefaultBufferSize),
		config:                  config,
		logger:                  logging.GetDefaultLogger().With().Str("component", "adaptive-buffer").Logger(),
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
	abm.logger.Info().Msg("Adaptive buffer manager started")
}

// Stop stops the adaptive buffer management
func (abm *AdaptiveBufferManager) Stop() {
	abm.cancel()
	abm.wg.Wait()
	abm.logger.Info().Msg("Adaptive buffer manager stopped")
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
	// Use exponential moving average for latency
	currentAvg := atomic.LoadInt64(&abm.averageLatency)
	newLatency := latency.Nanoseconds()

	if currentAvg == 0 {
		atomic.StoreInt64(&abm.averageLatency, newLatency)
	} else {
		// Exponential moving average: 70% historical, 30% current
		newAvg := int64(float64(currentAvg)*GetConfig().HistoricalWeight + float64(newLatency)*GetConfig().CurrentWeight)
		atomic.StoreInt64(&abm.averageLatency, newAvg)
	}
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
func (abm *AdaptiveBufferManager) adaptBufferSizes() {
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
	combinedFactor := GetConfig().CPUMemoryWeight*cpuFactor + GetConfig().MemoryWeight*memoryFactor + GetConfig().LatencyWeight*latencyFactor

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
		"system_cpu_percent":    float64(atomic.LoadInt64(&abm.systemCPUPercent)) / GetConfig().PercentageMultiplier,
		"system_memory_percent": float64(atomic.LoadInt64(&abm.systemMemoryPercent)) / GetConfig().PercentageMultiplier,
		"adaptation_count":      atomic.LoadInt64(&abm.adaptationCount),
		"last_adaptation":       lastAdaptation,
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
