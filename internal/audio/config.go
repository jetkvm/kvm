package audio

import "time"

// MonitoringConfig contains configuration constants for audio monitoring
type MonitoringConfig struct {
	// MetricsUpdateInterval defines how often metrics are collected and broadcast
	MetricsUpdateInterval time.Duration
}

// DefaultMonitoringConfig returns the default monitoring configuration
func DefaultMonitoringConfig() MonitoringConfig {
	return MonitoringConfig{
		MetricsUpdateInterval: 1000 * time.Millisecond, // 1 second interval
	}
}

// Global monitoring configuration instance
var monitoringConfig = DefaultMonitoringConfig()

// GetMetricsUpdateInterval returns the current metrics update interval
func GetMetricsUpdateInterval() time.Duration {
	return monitoringConfig.MetricsUpdateInterval
}

// SetMetricsUpdateInterval sets the metrics update interval
func SetMetricsUpdateInterval(interval time.Duration) {
	monitoringConfig.MetricsUpdateInterval = interval
}
