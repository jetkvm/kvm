package audio

import (
	"runtime"
	"sync/atomic"
	"time"

	"github.com/jetkvm/kvm/internal/logging"
)

// GoroutineMonitor tracks goroutine count and provides cleanup mechanisms
type GoroutineMonitor struct {
	baselineCount   int
	peakCount       int
	lastCount       int
	monitorInterval time.Duration
	lastCheck       time.Time
	enabled         int32
}

// Global goroutine monitor instance
var globalGoroutineMonitor *GoroutineMonitor

// NewGoroutineMonitor creates a new goroutine monitor
func NewGoroutineMonitor(monitorInterval time.Duration) *GoroutineMonitor {
	if monitorInterval <= 0 {
		monitorInterval = 30 * time.Second
	}

	// Get current goroutine count as baseline
	baselineCount := runtime.NumGoroutine()

	return &GoroutineMonitor{
		baselineCount:   baselineCount,
		peakCount:       baselineCount,
		lastCount:       baselineCount,
		monitorInterval: monitorInterval,
		lastCheck:       time.Now(),
	}
}

// Start begins goroutine monitoring
func (gm *GoroutineMonitor) Start() {
	if !atomic.CompareAndSwapInt32(&gm.enabled, 0, 1) {
		return // Already running
	}

	go gm.monitorLoop()
}

// Stop stops goroutine monitoring
func (gm *GoroutineMonitor) Stop() {
	atomic.StoreInt32(&gm.enabled, 0)
}

// monitorLoop periodically checks goroutine count
func (gm *GoroutineMonitor) monitorLoop() {
	logger := logging.GetDefaultLogger().With().Str("component", "goroutine-monitor").Logger()
	logger.Info().Int("baseline", gm.baselineCount).Msg("goroutine monitor started")

	for atomic.LoadInt32(&gm.enabled) == 1 {
		time.Sleep(gm.monitorInterval)
		gm.checkGoroutineCount()
	}

	logger.Info().Msg("goroutine monitor stopped")
}

// checkGoroutineCount checks current goroutine count and logs if it exceeds thresholds
func (gm *GoroutineMonitor) checkGoroutineCount() {
	currentCount := runtime.NumGoroutine()
	gm.lastCount = currentCount

	// Update peak count if needed
	if currentCount > gm.peakCount {
		gm.peakCount = currentCount
	}

	// Calculate growth since baseline
	growth := currentCount - gm.baselineCount
	growthPercent := float64(growth) / float64(gm.baselineCount) * 100

	// Log warning if growth exceeds thresholds
	logger := logging.GetDefaultLogger().With().Str("component", "goroutine-monitor").Logger()

	// Different log levels based on growth severity
	if growthPercent > 30 {
		// Severe growth - trigger cleanup
		logger.Warn().Int("current", currentCount).Int("baseline", gm.baselineCount).
			Int("growth", growth).Float64("growth_percent", growthPercent).
			Msg("excessive goroutine growth detected - triggering cleanup")

		// Force garbage collection to clean up unused resources
		runtime.GC()

		// Force cleanup of goroutine buffer cache
		cleanupGoroutineCache()
	} else if growthPercent > 20 {
		// Moderate growth - just log warning
		logger.Warn().Int("current", currentCount).Int("baseline", gm.baselineCount).
			Int("growth", growth).Float64("growth_percent", growthPercent).
			Msg("significant goroutine growth detected")
	} else if growthPercent > 10 {
		// Minor growth - log info
		logger.Info().Int("current", currentCount).Int("baseline", gm.baselineCount).
			Int("growth", growth).Float64("growth_percent", growthPercent).
			Msg("goroutine growth detected")
	}

	// Update last check time
	gm.lastCheck = time.Now()
}

// GetGoroutineStats returns current goroutine statistics
func (gm *GoroutineMonitor) GetGoroutineStats() map[string]interface{} {
	return map[string]interface{}{
		"current_count":  gm.lastCount,
		"baseline_count": gm.baselineCount,
		"peak_count":     gm.peakCount,
		"growth":         gm.lastCount - gm.baselineCount,
		"growth_percent": float64(gm.lastCount-gm.baselineCount) / float64(gm.baselineCount) * 100,
		"last_check":     gm.lastCheck,
	}
}

// GetGoroutineMonitor returns the global goroutine monitor instance
func GetGoroutineMonitor() *GoroutineMonitor {
	if globalGoroutineMonitor == nil {
		globalGoroutineMonitor = NewGoroutineMonitor(GetConfig().GoroutineMonitorInterval)
	}
	return globalGoroutineMonitor
}

// StartGoroutineMonitoring starts the global goroutine monitor
func StartGoroutineMonitoring() {
	cachedConfig := GetCachedConfig()
	if !cachedConfig.GetEnableGoroutineMonitoring() {
		return
	}

	monitor := GetGoroutineMonitor()
	monitor.Start()
}

// StopGoroutineMonitoring stops the global goroutine monitor
func StopGoroutineMonitoring() {
	if globalGoroutineMonitor != nil {
		globalGoroutineMonitor.Stop()
	}
}
