package audio

import "time"

// GetMetricsUpdateInterval returns the current metrics update interval from centralized config
func GetMetricsUpdateInterval() time.Duration {
	return GetConfig().MetricsUpdateInterval
}

// SetMetricsUpdateInterval sets the metrics update interval in centralized config
func SetMetricsUpdateInterval(interval time.Duration) {
	config := GetConfig()
	config.MetricsUpdateInterval = interval
	UpdateConfig(config)
}
