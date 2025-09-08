package audio

import "time"

// GetMetricsUpdateInterval returns the current metrics update interval from centralized config
func GetMetricsUpdateInterval() time.Duration {
	return Config.MetricsUpdateInterval
}

// SetMetricsUpdateInterval sets the metrics update interval in centralized config
func SetMetricsUpdateInterval(interval time.Duration) {
	config := Config
	config.MetricsUpdateInterval = interval
	UpdateConfig(config)
}
