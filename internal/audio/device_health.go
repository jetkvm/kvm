package audio

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jetkvm/kvm/internal/logging"
	"github.com/rs/zerolog"
)

// DeviceHealthStatus represents the health status of an audio device
type DeviceHealthStatus int

const (
	DeviceHealthUnknown DeviceHealthStatus = iota
	DeviceHealthHealthy
	DeviceHealthDegraded
	DeviceHealthFailing
	DeviceHealthCritical
)

func (s DeviceHealthStatus) String() string {
	switch s {
	case DeviceHealthHealthy:
		return "healthy"
	case DeviceHealthDegraded:
		return "degraded"
	case DeviceHealthFailing:
		return "failing"
	case DeviceHealthCritical:
		return "critical"
	default:
		return "unknown"
	}
}

// DeviceHealthMetrics tracks health-related metrics for audio devices
type DeviceHealthMetrics struct {
	// Error tracking
	ConsecutiveErrors int64     `json:"consecutive_errors"`
	TotalErrors       int64     `json:"total_errors"`
	LastErrorTime     time.Time `json:"last_error_time"`
	ErrorRate         float64   `json:"error_rate"` // errors per minute

	// Performance metrics
	AverageLatency time.Duration `json:"average_latency"`
	MaxLatency     time.Duration `json:"max_latency"`
	LatencySpikes  int64         `json:"latency_spikes"`
	Underruns      int64         `json:"underruns"`
	Overruns       int64         `json:"overruns"`

	// Device availability
	LastSuccessfulOp     time.Time `json:"last_successful_op"`
	DeviceDisconnects    int64     `json:"device_disconnects"`
	RecoveryAttempts     int64     `json:"recovery_attempts"`
	SuccessfulRecoveries int64     `json:"successful_recoveries"`

	// Health assessment
	CurrentStatus     DeviceHealthStatus `json:"current_status"`
	StatusLastChanged time.Time          `json:"status_last_changed"`
	HealthScore       float64            `json:"health_score"` // 0.0 to 1.0
}

// DeviceHealthMonitor monitors the health of audio devices and triggers recovery
type DeviceHealthMonitor struct {
	// Atomic fields first for ARM32 alignment
	running           int32
	monitoringEnabled int32

	// Configuration
	checkInterval     time.Duration
	recoveryThreshold int
	latencyThreshold  time.Duration
	errorRateLimit    float64 // max errors per minute

	// State tracking
	captureMetrics  *DeviceHealthMetrics
	playbackMetrics *DeviceHealthMetrics
	mutex           sync.RWMutex

	// Control channels
	ctx      context.Context
	cancel   context.CancelFunc
	stopChan chan struct{}
	doneChan chan struct{}

	// Recovery callbacks
	recoveryCallbacks map[string]func() error
	callbackMutex     sync.RWMutex

	// Logging
	logger zerolog.Logger
	config *AudioConfigConstants
}

// NewDeviceHealthMonitor creates a new device health monitor
func NewDeviceHealthMonitor() *DeviceHealthMonitor {
	ctx, cancel := context.WithCancel(context.Background())
	config := GetConfig()

	return &DeviceHealthMonitor{
		checkInterval:     time.Duration(config.HealthCheckIntervalMS) * time.Millisecond,
		recoveryThreshold: config.HealthRecoveryThreshold,
		latencyThreshold:  time.Duration(config.HealthLatencyThresholdMS) * time.Millisecond,
		errorRateLimit:    config.HealthErrorRateLimit,
		captureMetrics: &DeviceHealthMetrics{
			CurrentStatus: DeviceHealthUnknown,
			HealthScore:   1.0,
		},
		playbackMetrics: &DeviceHealthMetrics{
			CurrentStatus: DeviceHealthUnknown,
			HealthScore:   1.0,
		},
		ctx:               ctx,
		cancel:            cancel,
		stopChan:          make(chan struct{}),
		doneChan:          make(chan struct{}),
		recoveryCallbacks: make(map[string]func() error),
		logger:            logging.GetDefaultLogger().With().Str("component", "device-health-monitor").Logger(),
		config:            config,
	}
}

// Start begins health monitoring
func (dhm *DeviceHealthMonitor) Start() error {
	if !atomic.CompareAndSwapInt32(&dhm.running, 0, 1) {
		return fmt.Errorf("device health monitor already running")
	}

	dhm.logger.Debug().Msg("device health monitor starting")
	atomic.StoreInt32(&dhm.monitoringEnabled, 1)

	go dhm.monitoringLoop()
	return nil
}

// Stop stops health monitoring
func (dhm *DeviceHealthMonitor) Stop() {
	if !atomic.CompareAndSwapInt32(&dhm.running, 1, 0) {
		return
	}

	dhm.logger.Debug().Msg("device health monitor stopping")
	atomic.StoreInt32(&dhm.monitoringEnabled, 0)

	close(dhm.stopChan)
	dhm.cancel()

	// Wait for monitoring loop to finish
	select {
	case <-dhm.doneChan:
		dhm.logger.Debug().Msg("device health monitor stopped")
	case <-time.After(time.Duration(dhm.config.SupervisorTimeout)):
		dhm.logger.Warn().Msg("device health monitor stop timeout")
	}
}

// RegisterRecoveryCallback registers a recovery function for a specific component
func (dhm *DeviceHealthMonitor) RegisterRecoveryCallback(component string, callback func() error) {
	dhm.callbackMutex.Lock()
	defer dhm.callbackMutex.Unlock()
	dhm.recoveryCallbacks[component] = callback
	dhm.logger.Debug().Str("component", component).Msg("registered recovery callback")
}

// RecordError records an error for health tracking
func (dhm *DeviceHealthMonitor) RecordError(deviceType string, err error) {
	if atomic.LoadInt32(&dhm.monitoringEnabled) == 0 {
		return
	}

	dhm.mutex.Lock()
	defer dhm.mutex.Unlock()

	var metrics *DeviceHealthMetrics
	switch deviceType {
	case "capture":
		metrics = dhm.captureMetrics
	case "playback":
		metrics = dhm.playbackMetrics
	default:
		dhm.logger.Warn().Str("device_type", deviceType).Msg("unknown device type for error recording")
		return
	}

	atomic.AddInt64(&metrics.ConsecutiveErrors, 1)
	atomic.AddInt64(&metrics.TotalErrors, 1)
	metrics.LastErrorTime = time.Now()

	// Update error rate (errors per minute)
	if !metrics.LastErrorTime.IsZero() {
		timeSinceFirst := time.Since(metrics.LastErrorTime)
		if timeSinceFirst > 0 {
			metrics.ErrorRate = float64(metrics.TotalErrors) / timeSinceFirst.Minutes()
		}
	}

	dhm.logger.Debug().
		Str("device_type", deviceType).
		Err(err).
		Int64("consecutive_errors", metrics.ConsecutiveErrors).
		Float64("error_rate", metrics.ErrorRate).
		Msg("recorded device error")

	// Trigger immediate health assessment
	dhm.assessDeviceHealth(deviceType, metrics)
}

// RecordSuccess records a successful operation
func (dhm *DeviceHealthMonitor) RecordSuccess(deviceType string) {
	if atomic.LoadInt32(&dhm.monitoringEnabled) == 0 {
		return
	}

	dhm.mutex.Lock()
	defer dhm.mutex.Unlock()

	var metrics *DeviceHealthMetrics
	switch deviceType {
	case "capture":
		metrics = dhm.captureMetrics
	case "playback":
		metrics = dhm.playbackMetrics
	default:
		return
	}

	// Reset consecutive errors on success
	atomic.StoreInt64(&metrics.ConsecutiveErrors, 0)
	metrics.LastSuccessfulOp = time.Now()

	// Improve health score gradually
	if metrics.HealthScore < 1.0 {
		metrics.HealthScore = min(1.0, metrics.HealthScore+0.1)
	}
}

// RecordLatency records operation latency for health assessment
func (dhm *DeviceHealthMonitor) RecordLatency(deviceType string, latency time.Duration) {
	if atomic.LoadInt32(&dhm.monitoringEnabled) == 0 {
		return
	}

	dhm.mutex.Lock()
	defer dhm.mutex.Unlock()

	var metrics *DeviceHealthMetrics
	switch deviceType {
	case "capture":
		metrics = dhm.captureMetrics
	case "playback":
		metrics = dhm.playbackMetrics
	default:
		return
	}

	// Update latency metrics
	if metrics.AverageLatency == 0 {
		metrics.AverageLatency = latency
	} else {
		// Exponential moving average
		metrics.AverageLatency = time.Duration(float64(metrics.AverageLatency)*0.9 + float64(latency)*0.1)
	}

	if latency > metrics.MaxLatency {
		metrics.MaxLatency = latency
	}

	// Track latency spikes
	if latency > dhm.latencyThreshold {
		atomic.AddInt64(&metrics.LatencySpikes, 1)
	}
}

// RecordUnderrun records an audio underrun event
func (dhm *DeviceHealthMonitor) RecordUnderrun(deviceType string) {
	if atomic.LoadInt32(&dhm.monitoringEnabled) == 0 {
		return
	}

	dhm.mutex.Lock()
	defer dhm.mutex.Unlock()

	var metrics *DeviceHealthMetrics
	switch deviceType {
	case "capture":
		metrics = dhm.captureMetrics
	case "playback":
		metrics = dhm.playbackMetrics
	default:
		return
	}

	atomic.AddInt64(&metrics.Underruns, 1)
	dhm.logger.Debug().Str("device_type", deviceType).Msg("recorded audio underrun")
}

// RecordOverrun records an audio overrun event
func (dhm *DeviceHealthMonitor) RecordOverrun(deviceType string) {
	if atomic.LoadInt32(&dhm.monitoringEnabled) == 0 {
		return
	}

	dhm.mutex.Lock()
	defer dhm.mutex.Unlock()

	var metrics *DeviceHealthMetrics
	switch deviceType {
	case "capture":
		metrics = dhm.captureMetrics
	case "playback":
		metrics = dhm.playbackMetrics
	default:
		return
	}

	atomic.AddInt64(&metrics.Overruns, 1)
	dhm.logger.Debug().Str("device_type", deviceType).Msg("recorded audio overrun")
}

// GetHealthMetrics returns current health metrics
func (dhm *DeviceHealthMonitor) GetHealthMetrics() (capture, playback DeviceHealthMetrics) {
	dhm.mutex.RLock()
	defer dhm.mutex.RUnlock()
	return *dhm.captureMetrics, *dhm.playbackMetrics
}

// monitoringLoop runs the main health monitoring loop
func (dhm *DeviceHealthMonitor) monitoringLoop() {
	defer close(dhm.doneChan)

	ticker := time.NewTicker(dhm.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-dhm.stopChan:
			return
		case <-dhm.ctx.Done():
			return
		case <-ticker.C:
			dhm.performHealthCheck()
		}
	}
}

// performHealthCheck performs a comprehensive health check
func (dhm *DeviceHealthMonitor) performHealthCheck() {
	dhm.mutex.Lock()
	defer dhm.mutex.Unlock()

	// Assess health for both devices
	dhm.assessDeviceHealth("capture", dhm.captureMetrics)
	dhm.assessDeviceHealth("playback", dhm.playbackMetrics)

	// Check if recovery is needed
	dhm.checkRecoveryNeeded("capture", dhm.captureMetrics)
	dhm.checkRecoveryNeeded("playback", dhm.playbackMetrics)
}

// assessDeviceHealth assesses the health status of a device
func (dhm *DeviceHealthMonitor) assessDeviceHealth(deviceType string, metrics *DeviceHealthMetrics) {
	previousStatus := metrics.CurrentStatus
	newStatus := dhm.calculateHealthStatus(metrics)

	if newStatus != previousStatus {
		metrics.CurrentStatus = newStatus
		metrics.StatusLastChanged = time.Now()
		dhm.logger.Info().
			Str("device_type", deviceType).
			Str("previous_status", previousStatus.String()).
			Str("new_status", newStatus.String()).
			Float64("health_score", metrics.HealthScore).
			Msg("device health status changed")
	}

	// Update health score
	metrics.HealthScore = dhm.calculateHealthScore(metrics)
}

// calculateHealthStatus determines health status based on metrics
func (dhm *DeviceHealthMonitor) calculateHealthStatus(metrics *DeviceHealthMetrics) DeviceHealthStatus {
	consecutiveErrors := atomic.LoadInt64(&metrics.ConsecutiveErrors)
	totalErrors := atomic.LoadInt64(&metrics.TotalErrors)

	// Critical: Too many consecutive errors or device disconnected recently
	if consecutiveErrors >= int64(dhm.recoveryThreshold) {
		return DeviceHealthCritical
	}

	// Critical: No successful operations in a long time
	if !metrics.LastSuccessfulOp.IsZero() && time.Since(metrics.LastSuccessfulOp) > time.Duration(dhm.config.SupervisorTimeout) {
		return DeviceHealthCritical
	}

	// Failing: High error rate or frequent latency spikes
	if metrics.ErrorRate > dhm.errorRateLimit || atomic.LoadInt64(&metrics.LatencySpikes) > int64(dhm.config.MaxDroppedFrames) {
		return DeviceHealthFailing
	}

	// Degraded: Some errors or performance issues
	if consecutiveErrors > 0 || totalErrors > int64(dhm.config.MaxDroppedFrames/2) || metrics.AverageLatency > dhm.latencyThreshold {
		return DeviceHealthDegraded
	}

	// Healthy: No significant issues
	return DeviceHealthHealthy
}

// calculateHealthScore calculates a numeric health score (0.0 to 1.0)
func (dhm *DeviceHealthMonitor) calculateHealthScore(metrics *DeviceHealthMetrics) float64 {
	score := 1.0

	// Penalize consecutive errors
	consecutiveErrors := atomic.LoadInt64(&metrics.ConsecutiveErrors)
	if consecutiveErrors > 0 {
		score -= float64(consecutiveErrors) * 0.1
	}

	// Penalize high error rate
	if metrics.ErrorRate > 0 {
		score -= min(0.5, metrics.ErrorRate/dhm.errorRateLimit*0.5)
	}

	// Penalize high latency
	if metrics.AverageLatency > dhm.latencyThreshold {
		excess := float64(metrics.AverageLatency-dhm.latencyThreshold) / float64(dhm.latencyThreshold)
		score -= min(0.3, excess*0.3)
	}

	// Penalize underruns/overruns
	underruns := atomic.LoadInt64(&metrics.Underruns)
	overruns := atomic.LoadInt64(&metrics.Overruns)
	if underruns+overruns > 0 {
		score -= min(0.2, float64(underruns+overruns)*0.01)
	}

	return max(0.0, score)
}

// checkRecoveryNeeded checks if recovery is needed and triggers it
func (dhm *DeviceHealthMonitor) checkRecoveryNeeded(deviceType string, metrics *DeviceHealthMetrics) {
	if metrics.CurrentStatus == DeviceHealthCritical {
		dhm.triggerRecovery(deviceType, metrics)
	}
}

// triggerRecovery triggers recovery for a device
func (dhm *DeviceHealthMonitor) triggerRecovery(deviceType string, metrics *DeviceHealthMetrics) {
	atomic.AddInt64(&metrics.RecoveryAttempts, 1)

	dhm.logger.Warn().
		Str("device_type", deviceType).
		Str("status", metrics.CurrentStatus.String()).
		Int64("consecutive_errors", atomic.LoadInt64(&metrics.ConsecutiveErrors)).
		Float64("error_rate", metrics.ErrorRate).
		Msg("triggering device recovery")

	// Try registered recovery callbacks
	dhm.callbackMutex.RLock()
	defer dhm.callbackMutex.RUnlock()

	for component, callback := range dhm.recoveryCallbacks {
		if callback != nil {
			go func(comp string, cb func() error) {
				if err := cb(); err != nil {
					dhm.logger.Error().
						Str("component", comp).
						Str("device_type", deviceType).
						Err(err).
						Msg("recovery callback failed")
				} else {
					atomic.AddInt64(&metrics.SuccessfulRecoveries, 1)
					dhm.logger.Info().
						Str("component", comp).
						Str("device_type", deviceType).
						Msg("recovery callback succeeded")
				}
			}(component, callback)
		}
	}
}

// Global device health monitor instance
var (
	globalDeviceHealthMonitor *DeviceHealthMonitor
	deviceHealthOnce          sync.Once
)

// GetDeviceHealthMonitor returns the global device health monitor
func GetDeviceHealthMonitor() *DeviceHealthMonitor {
	deviceHealthOnce.Do(func() {
		globalDeviceHealthMonitor = NewDeviceHealthMonitor()
	})
	return globalDeviceHealthMonitor
}

// Helper functions for min/max
func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
