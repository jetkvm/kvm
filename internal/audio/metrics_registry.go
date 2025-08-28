//go:build cgo

package audio

import (
	"sync"
	"sync/atomic"
	"time"
)

// MetricsRegistry provides a centralized source of truth for all audio metrics
// This eliminates duplication between session-specific and global managers
type MetricsRegistry struct {
	mu                sync.RWMutex
	audioMetrics      AudioMetrics
	audioInputMetrics AudioInputMetrics
	audioConfig       AudioConfig
	microphoneConfig  AudioConfig
	lastUpdate        int64 // Unix timestamp
}

var (
	globalMetricsRegistry *MetricsRegistry
	registryOnce          sync.Once
)

// GetMetricsRegistry returns the global metrics registry instance
func GetMetricsRegistry() *MetricsRegistry {
	registryOnce.Do(func() {
		globalMetricsRegistry = &MetricsRegistry{
			lastUpdate: time.Now().Unix(),
		}
	})
	return globalMetricsRegistry
}

// UpdateAudioMetrics updates the centralized audio output metrics
func (mr *MetricsRegistry) UpdateAudioMetrics(metrics AudioMetrics) {
	mr.mu.Lock()
	mr.audioMetrics = metrics
	mr.lastUpdate = time.Now().Unix()
	mr.mu.Unlock()

	// Update Prometheus metrics directly to avoid circular dependency
	UpdateAudioMetrics(convertAudioMetricsToUnified(metrics))
}

// UpdateAudioInputMetrics updates the centralized audio input metrics
func (mr *MetricsRegistry) UpdateAudioInputMetrics(metrics AudioInputMetrics) {
	mr.mu.Lock()
	mr.audioInputMetrics = metrics
	mr.lastUpdate = time.Now().Unix()
	mr.mu.Unlock()

	// Update Prometheus metrics directly to avoid circular dependency
	UpdateMicrophoneMetrics(convertAudioInputMetricsToUnified(metrics))
}

// UpdateAudioConfig updates the centralized audio configuration
func (mr *MetricsRegistry) UpdateAudioConfig(config AudioConfig) {
	mr.mu.Lock()
	mr.audioConfig = config
	mr.lastUpdate = time.Now().Unix()
	mr.mu.Unlock()

	// Update Prometheus metrics directly
	UpdateAudioConfigMetrics(config)
}

// UpdateMicrophoneConfig updates the centralized microphone configuration
func (mr *MetricsRegistry) UpdateMicrophoneConfig(config AudioConfig) {
	mr.mu.Lock()
	mr.microphoneConfig = config
	mr.lastUpdate = time.Now().Unix()
	mr.mu.Unlock()

	// Update Prometheus metrics directly
	UpdateMicrophoneConfigMetrics(config)
}

// GetAudioMetrics returns the current audio output metrics
func (mr *MetricsRegistry) GetAudioMetrics() AudioMetrics {
	mr.mu.RLock()
	defer mr.mu.RUnlock()
	return mr.audioMetrics
}

// GetAudioInputMetrics returns the current audio input metrics
func (mr *MetricsRegistry) GetAudioInputMetrics() AudioInputMetrics {
	mr.mu.RLock()
	defer mr.mu.RUnlock()
	return mr.audioInputMetrics
}

// GetAudioConfig returns the current audio configuration
func (mr *MetricsRegistry) GetAudioConfig() AudioConfig {
	mr.mu.RLock()
	defer mr.mu.RUnlock()
	return mr.audioConfig
}

// GetMicrophoneConfig returns the current microphone configuration
func (mr *MetricsRegistry) GetMicrophoneConfig() AudioConfig {
	mr.mu.RLock()
	defer mr.mu.RUnlock()
	return mr.microphoneConfig
}

// GetLastUpdate returns the timestamp of the last metrics update
func (mr *MetricsRegistry) GetLastUpdate() time.Time {
	timestamp := atomic.LoadInt64(&mr.lastUpdate)
	return time.Unix(timestamp, 0)
}

// StartMetricsCollector starts a background goroutine to collect metrics
func (mr *MetricsRegistry) StartMetricsCollector() {
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			// Collect from session-specific manager if available
			if sessionProvider := GetSessionProvider(); sessionProvider != nil && sessionProvider.IsSessionActive() {
				if inputManager := sessionProvider.GetAudioInputManager(); inputManager != nil {
					metrics := inputManager.GetMetrics()
					mr.UpdateAudioInputMetrics(metrics)
				}
			} else {
				// Fallback to global manager if no session is active
				globalManager := getAudioInputManager()
				metrics := globalManager.GetMetrics()
				mr.UpdateAudioInputMetrics(metrics)
			}

			// Collect audio output metrics directly from global metrics variable to avoid circular dependency
			audioMetrics := AudioMetrics{
				FramesReceived:  atomic.LoadInt64(&metrics.FramesReceived),
				FramesDropped:   atomic.LoadInt64(&metrics.FramesDropped),
				BytesProcessed:  atomic.LoadInt64(&metrics.BytesProcessed),
				ConnectionDrops: atomic.LoadInt64(&metrics.ConnectionDrops),
				LastFrameTime:   metrics.LastFrameTime,
				AverageLatency:  metrics.AverageLatency,
			}
			mr.UpdateAudioMetrics(audioMetrics)

			// Collect configuration directly from global variables to avoid circular dependency
			mr.UpdateAudioConfig(currentConfig)
			mr.UpdateMicrophoneConfig(currentMicrophoneConfig)
		}
	}()
}
