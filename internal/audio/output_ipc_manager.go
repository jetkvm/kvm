package audio

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/jetkvm/kvm/internal/logging"
)

// AudioOutputIPCManager manages audio output using IPC when enabled
type AudioOutputIPCManager struct {
	*BaseAudioManager
	server *AudioOutputServer
}

// NewAudioOutputIPCManager creates a new IPC-based audio output manager
func NewAudioOutputIPCManager() *AudioOutputIPCManager {
	return &AudioOutputIPCManager{
		BaseAudioManager: NewBaseAudioManager(logging.GetDefaultLogger().With().Str("component", AudioOutputIPCComponent).Logger()),
	}
}

// Start initializes and starts the audio output IPC manager
func (aom *AudioOutputIPCManager) Start() error {
	aom.logComponentStart(AudioOutputIPCComponent)

	// Create and start the IPC server
	server, err := NewAudioOutputServer()
	if err != nil {
		aom.logComponentError(AudioOutputIPCComponent, err, "failed to create IPC server")
		return err
	}

	if err := server.Start(); err != nil {
		aom.logComponentError(AudioOutputIPCComponent, err, "failed to start IPC server")
		return err
	}

	aom.server = server
	aom.setRunning(true)
	aom.logComponentStarted(AudioOutputIPCComponent)

	// Send initial configuration
	config := OutputIPCConfig{
		SampleRate: GetConfig().SampleRate,
		Channels:   GetConfig().Channels,
		FrameSize:  int(GetConfig().AudioQualityMediumFrameSize.Milliseconds()),
	}

	if err := aom.SendConfig(config); err != nil {
		aom.logger.Warn().Err(err).Msg("Failed to send initial configuration")
	}

	return nil
}

// Stop gracefully shuts down the audio output IPC manager
func (aom *AudioOutputIPCManager) Stop() {
	aom.logComponentStop(AudioOutputIPCComponent)

	if aom.server != nil {
		aom.server.Stop()
		aom.server = nil
	}

	aom.setRunning(false)
	aom.resetMetrics()
	aom.logComponentStopped(AudioOutputIPCComponent)
}

// resetMetrics resets all metrics to zero
func (aom *AudioOutputIPCManager) resetMetrics() {
	aom.BaseAudioManager.resetMetrics()
}

// WriteOpusFrame sends an Opus frame to the output server
func (aom *AudioOutputIPCManager) WriteOpusFrame(frame *ZeroCopyAudioFrame) error {
	if !aom.IsRunning() {
		return fmt.Errorf("audio output IPC manager not running")
	}

	if aom.server == nil {
		return fmt.Errorf("audio output server not initialized")
	}

	// Validate frame before processing
	if err := ValidateZeroCopyFrame(frame); err != nil {
		aom.logComponentError(AudioOutputIPCComponent, err, "Frame validation failed")
		return fmt.Errorf("output frame validation failed: %w", err)
	}

	start := time.Now()

	// Send frame to IPC server
	if err := aom.server.SendFrame(frame.Data()); err != nil {
		aom.recordFrameDropped()
		return err
	}

	// Update metrics
	processingTime := time.Since(start)
	aom.recordFrameProcessed(frame.Length())
	aom.updateLatency(processingTime)

	return nil
}

// WriteOpusFrameZeroCopy writes an Opus audio frame using zero-copy optimization
func (aom *AudioOutputIPCManager) WriteOpusFrameZeroCopy(frame *ZeroCopyAudioFrame) error {
	if !aom.IsRunning() {
		return fmt.Errorf("audio output IPC manager not running")
	}

	if aom.server == nil {
		return fmt.Errorf("audio output server not initialized")
	}

	start := time.Now()

	// Extract frame data
	frameData := frame.Data()

	// Send frame to IPC server (zero-copy not available, use regular send)
	if err := aom.server.SendFrame(frameData); err != nil {
		aom.recordFrameDropped()
		return err
	}

	// Update metrics
	processingTime := time.Since(start)
	aom.recordFrameProcessed(len(frameData))
	aom.updateLatency(processingTime)

	return nil
}

// IsReady returns true if the IPC manager is ready to process frames
func (aom *AudioOutputIPCManager) IsReady() bool {
	return aom.IsRunning() && aom.server != nil
}

// GetMetrics returns current audio output metrics
func (aom *AudioOutputIPCManager) GetMetrics() AudioOutputMetrics {
	baseMetrics := aom.getBaseMetrics()
	return AudioOutputMetrics{
		FramesReceived:   atomic.LoadInt64(&baseMetrics.FramesProcessed), // For output, processed = received
		BaseAudioMetrics: baseMetrics,
	}
}

// GetDetailedMetrics returns detailed metrics including server statistics
func (aom *AudioOutputIPCManager) GetDetailedMetrics() (AudioOutputMetrics, map[string]interface{}) {
	metrics := aom.GetMetrics()
	detailed := make(map[string]interface{})

	if aom.server != nil {
		total, dropped, bufferSize := aom.server.GetServerStats()
		detailed["server_total_frames"] = total
		detailed["server_dropped_frames"] = dropped
		detailed["server_buffer_size"] = bufferSize
		detailed["server_frame_rate"] = aom.calculateFrameRate()
	}

	return metrics, detailed
}

// calculateFrameRate calculates the current frame processing rate
func (aom *AudioOutputIPCManager) calculateFrameRate() float64 {
	baseMetrics := aom.getBaseMetrics()
	framesProcessed := atomic.LoadInt64(&baseMetrics.FramesProcessed)
	if framesProcessed == 0 {
		return 0.0
	}

	// Calculate rate based on last frame time
	baseMetrics = aom.getBaseMetrics()
	if baseMetrics.LastFrameTime.IsZero() {
		return 0.0
	}

	elapsed := time.Since(baseMetrics.LastFrameTime)
	if elapsed.Seconds() == 0 {
		return 0.0
	}

	return float64(framesProcessed) / elapsed.Seconds()
}

// SendConfig sends configuration to the IPC server
func (aom *AudioOutputIPCManager) SendConfig(config OutputIPCConfig) error {
	if aom.server == nil {
		return fmt.Errorf("audio output server not initialized")
	}

	// Validate configuration parameters
	if err := ValidateOutputIPCConfig(config.SampleRate, config.Channels, config.FrameSize); err != nil {
		aom.logger.Error().Err(err).Msg("Configuration validation failed")
		return fmt.Errorf("output configuration validation failed: %w", err)
	}

	// Note: AudioOutputServer doesn't have SendConfig method yet
	// This is a placeholder for future implementation
	aom.logger.Info().Interface("config", config).Msg("configuration received")
	return nil
}

// GetServer returns the underlying IPC server (for testing)
func (aom *AudioOutputIPCManager) GetServer() *AudioOutputServer {
	return aom.server
}
