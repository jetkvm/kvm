package audio

import (
	"time"

	"github.com/rs/zerolog"
)

// AudioLoggerStandards provides standardized logging patterns for audio components
type AudioLoggerStandards struct {
	logger    zerolog.Logger
	component string
}

// NewAudioLogger creates a new standardized logger for an audio component
func NewAudioLogger(logger zerolog.Logger, component string) *AudioLoggerStandards {
	return &AudioLoggerStandards{
		logger:    logger.With().Str("component", component).Logger(),
		component: component,
	}
}

// Component Lifecycle Logging

// LogComponentStarting logs component initialization start
func (als *AudioLoggerStandards) LogComponentStarting() {
	als.logger.Debug().Msg("starting component")
}

// LogComponentStarted logs successful component start
func (als *AudioLoggerStandards) LogComponentStarted() {
	als.logger.Debug().Msg("component started successfully")
}

// LogComponentStopping logs component shutdown start
func (als *AudioLoggerStandards) LogComponentStopping() {
	als.logger.Debug().Msg("stopping component")
}

// LogComponentStopped logs successful component stop
func (als *AudioLoggerStandards) LogComponentStopped() {
	als.logger.Debug().Msg("component stopped")
}

// LogComponentReady logs component ready state
func (als *AudioLoggerStandards) LogComponentReady() {
	als.logger.Info().Msg("component ready")
}

// Error Logging with Context

// LogError logs a general error with context
func (als *AudioLoggerStandards) LogError(err error, msg string) {
	als.logger.Error().Err(err).Msg(msg)
}

// LogErrorWithContext logs an error with additional context fields
func (als *AudioLoggerStandards) LogErrorWithContext(err error, msg string, fields map[string]interface{}) {
	event := als.logger.Error().Err(err)
	for key, value := range fields {
		event = event.Interface(key, value)
	}
	event.Msg(msg)
}

// LogValidationError logs validation failures with specific context
func (als *AudioLoggerStandards) LogValidationError(err error, validationType string, value interface{}) {
	als.logger.Error().Err(err).
		Str("validation_type", validationType).
		Interface("invalid_value", value).
		Msg("validation failed")
}

// LogConnectionError logs connection-related errors
func (als *AudioLoggerStandards) LogConnectionError(err error, endpoint string, retryCount int) {
	als.logger.Error().Err(err).
		Str("endpoint", endpoint).
		Int("retry_count", retryCount).
		Msg("connection failed")
}

// LogProcessError logs process-related errors with PID context
func (als *AudioLoggerStandards) LogProcessError(err error, pid int, msg string) {
	als.logger.Error().Err(err).
		Int("pid", pid).
		Msg(msg)
}

// Performance and Metrics Logging

// LogPerformanceMetrics logs standardized performance metrics
func (als *AudioLoggerStandards) LogPerformanceMetrics(metrics map[string]interface{}) {
	event := als.logger.Info()
	for key, value := range metrics {
		event = event.Interface(key, value)
	}
	event.Msg("performance metrics")
}

// LogLatencyMetrics logs latency-specific metrics
func (als *AudioLoggerStandards) LogLatencyMetrics(current, average, max time.Duration, jitter time.Duration) {
	als.logger.Info().
		Dur("current_latency", current).
		Dur("average_latency", average).
		Dur("max_latency", max).
		Dur("jitter", jitter).
		Msg("latency metrics")
}

// LogFrameMetrics logs frame processing metrics
func (als *AudioLoggerStandards) LogFrameMetrics(processed, dropped int64, rate float64) {
	als.logger.Info().
		Int64("frames_processed", processed).
		Int64("frames_dropped", dropped).
		Float64("processing_rate", rate).
		Msg("frame processing metrics")
}

// LogBufferMetrics logs buffer utilization metrics
func (als *AudioLoggerStandards) LogBufferMetrics(size, used, peak int, utilizationPercent float64) {
	als.logger.Info().
		Int("buffer_size", size).
		Int("buffer_used", used).
		Int("buffer_peak", peak).
		Float64("utilization_percent", utilizationPercent).
		Msg("buffer metrics")
}

// Warning Logging

// LogWarning logs a general warning
func (als *AudioLoggerStandards) LogWarning(msg string) {
	als.logger.Warn().Msg(msg)
}

// LogWarningWithError logs a warning with error context
func (als *AudioLoggerStandards) LogWarningWithError(err error, msg string) {
	als.logger.Warn().Err(err).Msg(msg)
}

// LogThresholdWarning logs warnings when thresholds are exceeded
func (als *AudioLoggerStandards) LogThresholdWarning(metric string, current, threshold interface{}, msg string) {
	als.logger.Warn().
		Str("metric", metric).
		Interface("current_value", current).
		Interface("threshold", threshold).
		Msg(msg)
}

// LogRetryWarning logs retry attempts with context
func (als *AudioLoggerStandards) LogRetryWarning(operation string, attempt, maxAttempts int, delay time.Duration) {
	als.logger.Warn().
		Str("operation", operation).
		Int("attempt", attempt).
		Int("max_attempts", maxAttempts).
		Dur("retry_delay", delay).
		Msg("retrying operation")
}

// LogRecoveryWarning logs recovery from error conditions
func (als *AudioLoggerStandards) LogRecoveryWarning(condition string, duration time.Duration) {
	als.logger.Warn().
		Str("condition", condition).
		Dur("recovery_time", duration).
		Msg("recovered from error condition")
}

// Debug and Trace Logging

// LogDebug logs debug information
func (als *AudioLoggerStandards) LogDebug(msg string) {
	als.logger.Debug().Msg(msg)
}

// LogDebugWithFields logs debug information with structured fields
func (als *AudioLoggerStandards) LogDebugWithFields(msg string, fields map[string]interface{}) {
	event := als.logger.Debug()
	for key, value := range fields {
		event = event.Interface(key, value)
	}
	event.Msg(msg)
}

// LogOperationTrace logs operation tracing for debugging
func (als *AudioLoggerStandards) LogOperationTrace(operation string, duration time.Duration, success bool) {
	als.logger.Debug().
		Str("operation", operation).
		Dur("duration", duration).
		Bool("success", success).
		Msg("operation trace")
}

// LogDataFlow logs data flow for debugging
func (als *AudioLoggerStandards) LogDataFlow(source, destination string, bytes int, frameCount int) {
	als.logger.Debug().
		Str("source", source).
		Str("destination", destination).
		Int("bytes", bytes).
		Int("frame_count", frameCount).
		Msg("data flow")
}

// Configuration and State Logging

// LogConfigurationChange logs configuration updates
func (als *AudioLoggerStandards) LogConfigurationChange(configType string, oldValue, newValue interface{}) {
	als.logger.Info().
		Str("config_type", configType).
		Interface("old_value", oldValue).
		Interface("new_value", newValue).
		Msg("configuration changed")
}

// LogStateTransition logs component state changes
func (als *AudioLoggerStandards) LogStateTransition(fromState, toState string, reason string) {
	als.logger.Info().
		Str("from_state", fromState).
		Str("to_state", toState).
		Str("reason", reason).
		Msg("state transition")
}

// LogResourceAllocation logs resource allocation/deallocation
func (als *AudioLoggerStandards) LogResourceAllocation(resourceType string, allocated bool, amount interface{}) {
	level := als.logger.Debug()
	if allocated {
		level.Str("action", "allocated")
	} else {
		level.Str("action", "deallocated")
	}
	level.Str("resource_type", resourceType).
		Interface("amount", amount).
		Msg("resource allocation")
}

// Network and IPC Logging

// LogConnectionEvent logs connection lifecycle events
func (als *AudioLoggerStandards) LogConnectionEvent(event, endpoint string, connectionID string) {
	als.logger.Info().
		Str("event", event).
		Str("endpoint", endpoint).
		Str("connection_id", connectionID).
		Msg("connection event")
}

// LogIPCEvent logs IPC communication events
func (als *AudioLoggerStandards) LogIPCEvent(event, socketPath string, bytes int) {
	als.logger.Debug().
		Str("event", event).
		Str("socket_path", socketPath).
		Int("bytes", bytes).
		Msg("IPC event")
}

// LogNetworkStats logs network statistics
func (als *AudioLoggerStandards) LogNetworkStats(sent, received int64, latency time.Duration, packetLoss float64) {
	als.logger.Info().
		Int64("bytes_sent", sent).
		Int64("bytes_received", received).
		Dur("network_latency", latency).
		Float64("packet_loss_percent", packetLoss).
		Msg("network statistics")
}

// Process and System Logging

// LogProcessEvent logs process lifecycle events
func (als *AudioLoggerStandards) LogProcessEvent(event string, pid int, exitCode *int) {
	event_log := als.logger.Info().
		Str("event", event).
		Int("pid", pid)
	if exitCode != nil {
		event_log = event_log.Int("exit_code", *exitCode)
	}
	event_log.Msg("process event")
}

// LogSystemResource logs system resource usage
func (als *AudioLoggerStandards) LogSystemResource(cpuPercent, memoryMB float64, goroutines int) {
	als.logger.Info().
		Float64("cpu_percent", cpuPercent).
		Float64("memory_mb", memoryMB).
		Int("goroutines", goroutines).
		Msg("system resources")
}

// LogPriorityChange logs thread priority changes
func (als *AudioLoggerStandards) LogPriorityChange(tid, oldPriority, newPriority int, policy string) {
	als.logger.Debug().
		Int("tid", tid).
		Int("old_priority", oldPriority).
		Int("new_priority", newPriority).
		Str("policy", policy).
		Msg("thread priority changed")
}

// Utility Functions

// GetLogger returns the underlying zerolog.Logger for advanced usage
func (als *AudioLoggerStandards) GetLogger() zerolog.Logger {
	return als.logger
}

// WithFields returns a new logger with additional persistent fields
func (als *AudioLoggerStandards) WithFields(fields map[string]interface{}) *AudioLoggerStandards {
	event := als.logger.With()
	for key, value := range fields {
		event = event.Interface(key, value)
	}
	return &AudioLoggerStandards{
		logger:    event.Logger(),
		component: als.component,
	}
}

// WithSubComponent creates a logger for a sub-component
func (als *AudioLoggerStandards) WithSubComponent(subComponent string) *AudioLoggerStandards {
	return &AudioLoggerStandards{
		logger:    als.logger.With().Str("sub_component", subComponent).Logger(),
		component: als.component + "." + subComponent,
	}
}