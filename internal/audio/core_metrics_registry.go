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

			// Collect audio output metrics from global audio output manager
			// Note: We need to get metrics from the actual audio output system
			// For now, we'll use the global metrics variable from quality_presets.go
			globalAudioMetrics := GetGlobalAudioMetrics()
			mr.UpdateAudioMetrics(globalAudioMetrics)
		}
	}()
}
