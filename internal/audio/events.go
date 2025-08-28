package audio

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/jetkvm/kvm/internal/logging"
	"github.com/rs/zerolog"
)

// AudioEventType represents different types of audio events
type AudioEventType string

const (
	AudioEventMuteChanged       AudioEventType = "audio-mute-changed"
	AudioEventMetricsUpdate     AudioEventType = "audio-metrics-update"
	AudioEventMicrophoneState   AudioEventType = "microphone-state-changed"
	AudioEventMicrophoneMetrics AudioEventType = "microphone-metrics-update"
	AudioEventProcessMetrics    AudioEventType = "audio-process-metrics"
	AudioEventMicProcessMetrics AudioEventType = "microphone-process-metrics"
	AudioEventDeviceChanged     AudioEventType = "audio-device-changed"
)

// AudioEvent represents a WebSocket audio event
type AudioEvent struct {
	Type AudioEventType `json:"type"`
	Data interface{}    `json:"data"`
}

// AudioMuteData represents audio mute state change data
type AudioMuteData struct {
	Muted bool `json:"muted"`
}

// AudioMetricsData represents audio metrics data
type AudioMetricsData struct {
	FramesReceived   int64                 `json:"frames_received"`
	FramesDropped    int64                 `json:"frames_dropped"`
	BytesProcessed   int64                 `json:"bytes_processed"`
	LastFrameTime    string                `json:"last_frame_time"`
	ConnectionDrops  int64                 `json:"connection_drops"`
	AverageLatency   string                `json:"average_latency"`
	LatencyHistogram *LatencyHistogramData `json:"latency_histogram,omitempty"`
}

// MicrophoneStateData represents microphone state data
type MicrophoneStateData struct {
	Running       bool `json:"running"`
	SessionActive bool `json:"session_active"`
}

// MicrophoneMetricsData represents microphone metrics data
type MicrophoneMetricsData struct {
	FramesSent       int64                 `json:"frames_sent"`
	FramesDropped    int64                 `json:"frames_dropped"`
	BytesProcessed   int64                 `json:"bytes_processed"`
	LastFrameTime    string                `json:"last_frame_time"`
	ConnectionDrops  int64                 `json:"connection_drops"`
	AverageLatency   string                `json:"average_latency"`
	LatencyHistogram *LatencyHistogramData `json:"latency_histogram,omitempty"`
}

// ProcessMetricsData represents process metrics data for WebSocket events
type ProcessMetricsData struct {
	PID           int     `json:"pid"`
	CPUPercent    float64 `json:"cpu_percent"`
	MemoryRSS     int64   `json:"memory_rss"`
	MemoryVMS     int64   `json:"memory_vms"`
	MemoryPercent float64 `json:"memory_percent"`
	Running       bool    `json:"running"`
	ProcessName   string  `json:"process_name"`
}

// AudioDeviceChangedData represents audio device configuration change data
type AudioDeviceChangedData struct {
	Enabled bool   `json:"enabled"`
	Reason  string `json:"reason"`
}

// AudioEventSubscriber represents a WebSocket connection subscribed to audio events
type AudioEventSubscriber struct {
	conn   *websocket.Conn
	ctx    context.Context
	logger *zerolog.Logger
}

// AudioEventBroadcaster manages audio event subscriptions and broadcasting
type AudioEventBroadcaster struct {
	subscribers map[string]*AudioEventSubscriber
	mutex       sync.RWMutex
	logger      *zerolog.Logger
}

var (
	audioEventBroadcaster *AudioEventBroadcaster
	audioEventOnce        sync.Once
)

// initializeBroadcaster creates and initializes the audio event broadcaster
func initializeBroadcaster() {
	l := logging.GetDefaultLogger().With().Str("component", "audio-events").Logger()
	audioEventBroadcaster = &AudioEventBroadcaster{
		subscribers: make(map[string]*AudioEventSubscriber),
		logger:      &l,
	}

	// Start metrics broadcasting goroutine
	go audioEventBroadcaster.startMetricsBroadcasting()

	// Start granular metrics logging with same interval as metrics broadcasting
	// StartGranularMetricsLogging(GetMetricsUpdateInterval()) // Disabled to reduce log pollution
}

// InitializeAudioEventBroadcaster initializes the global audio event broadcaster
func InitializeAudioEventBroadcaster() {
	audioEventOnce.Do(initializeBroadcaster)
}

// GetAudioEventBroadcaster returns the singleton audio event broadcaster
func GetAudioEventBroadcaster() *AudioEventBroadcaster {
	audioEventOnce.Do(initializeBroadcaster)
	return audioEventBroadcaster
}

// Subscribe adds a WebSocket connection to receive audio events
func (aeb *AudioEventBroadcaster) Subscribe(connectionID string, conn *websocket.Conn, ctx context.Context, logger *zerolog.Logger) {
	aeb.mutex.Lock()
	defer aeb.mutex.Unlock()

	// Check if there's already a subscription for this connectionID
	if _, exists := aeb.subscribers[connectionID]; exists {
		aeb.logger.Debug().Str("connectionID", connectionID).Msg("duplicate audio events subscription detected; replacing existing entry")
		// Do NOT close the existing WebSocket connection here because it's shared
		// with the signaling channel. Just replace the subscriber map entry.
		delete(aeb.subscribers, connectionID)
	}

	aeb.subscribers[connectionID] = &AudioEventSubscriber{
		conn:   conn,
		ctx:    ctx,
		logger: logger,
	}

	aeb.logger.Debug().Str("connectionID", connectionID).Msg("audio events subscription added")

	// Send initial state to new subscriber
	go aeb.sendInitialState(connectionID)
}

// Unsubscribe removes a WebSocket connection from audio events
func (aeb *AudioEventBroadcaster) Unsubscribe(connectionID string) {
	aeb.mutex.Lock()
	defer aeb.mutex.Unlock()

	delete(aeb.subscribers, connectionID)
	aeb.logger.Debug().Str("connectionID", connectionID).Msg("audio events subscription removed")
}

// BroadcastAudioMuteChanged broadcasts audio mute state changes
func (aeb *AudioEventBroadcaster) BroadcastAudioMuteChanged(muted bool) {
	event := createAudioEvent(AudioEventMuteChanged, AudioMuteData{Muted: muted})
	aeb.broadcast(event)
}

// BroadcastMicrophoneStateChanged broadcasts microphone state changes
func (aeb *AudioEventBroadcaster) BroadcastMicrophoneStateChanged(running, sessionActive bool) {
	event := createAudioEvent(AudioEventMicrophoneState, MicrophoneStateData{
		Running:       running,
		SessionActive: sessionActive,
	})
	aeb.broadcast(event)
}

// BroadcastAudioDeviceChanged broadcasts audio device configuration changes
func (aeb *AudioEventBroadcaster) BroadcastAudioDeviceChanged(enabled bool, reason string) {
	event := createAudioEvent(AudioEventDeviceChanged, AudioDeviceChangedData{
		Enabled: enabled,
		Reason:  reason,
	})
	aeb.broadcast(event)
}

// sendInitialState sends current audio state to a new subscriber
func (aeb *AudioEventBroadcaster) sendInitialState(connectionID string) {
	aeb.mutex.RLock()
	subscriber, exists := aeb.subscribers[connectionID]
	aeb.mutex.RUnlock()

	if !exists {
		return
	}

	// Send current audio mute state
	muteEvent := AudioEvent{
		Type: AudioEventMuteChanged,
		Data: AudioMuteData{Muted: IsAudioMuted()},
	}
	aeb.sendToSubscriber(subscriber, muteEvent)

	// Send current microphone state using session provider
	sessionProvider := GetSessionProvider()
	sessionActive := sessionProvider.IsSessionActive()
	var running bool
	if sessionActive {
		if inputManager := sessionProvider.GetAudioInputManager(); inputManager != nil {
			running = inputManager.IsRunning()
		}
	}

	micStateEvent := AudioEvent{
		Type: AudioEventMicrophoneState,
		Data: MicrophoneStateData{
			Running:       running,
			SessionActive: sessionActive,
		},
	}
	aeb.sendToSubscriber(subscriber, micStateEvent)

	// Send current metrics
	aeb.sendCurrentMetrics(subscriber)
}

// convertAudioMetricsToEventDataWithLatencyMs converts internal audio metrics to AudioMetricsData with millisecond latency formatting
func convertAudioMetricsToEventDataWithLatencyMs(metrics AudioMetrics) AudioMetricsData {
	// Get histogram data from granular metrics collector
	granularCollector := GetGranularMetricsCollector()
	var histogramData *LatencyHistogramData
	if granularCollector != nil {
		histogramData = granularCollector.GetOutputLatencyHistogram()
	}

	return AudioMetricsData{
		FramesReceived:   metrics.FramesReceived,
		FramesDropped:    metrics.FramesDropped,
		BytesProcessed:   metrics.BytesProcessed,
		LastFrameTime:    metrics.LastFrameTime.Format(GetConfig().EventTimeFormatString),
		ConnectionDrops:  metrics.ConnectionDrops,
		AverageLatency:   fmt.Sprintf("%.1fms", float64(metrics.AverageLatency.Nanoseconds())/1e6),
		LatencyHistogram: histogramData,
	}
}

// convertAudioInputMetricsToEventDataWithLatencyMs converts internal audio input metrics to MicrophoneMetricsData with millisecond latency formatting
func convertAudioInputMetricsToEventDataWithLatencyMs(metrics AudioInputMetrics) MicrophoneMetricsData {
	// Get histogram data from granular metrics collector
	granularCollector := GetGranularMetricsCollector()
	var histogramData *LatencyHistogramData
	if granularCollector != nil {
		histogramData = granularCollector.GetInputLatencyHistogram()
	}

	return MicrophoneMetricsData{
		FramesSent:       metrics.FramesSent,
		FramesDropped:    metrics.FramesDropped,
		BytesProcessed:   metrics.BytesProcessed,
		LastFrameTime:    metrics.LastFrameTime.Format(GetConfig().EventTimeFormatString),
		ConnectionDrops:  metrics.ConnectionDrops,
		AverageLatency:   fmt.Sprintf("%.1fms", float64(metrics.AverageLatency.Nanoseconds())/1e6),
		LatencyHistogram: histogramData,
	}
}

// convertProcessMetricsToEventData converts internal process metrics to ProcessMetricsData for events
func convertProcessMetricsToEventData(metrics ProcessMetrics, running bool) ProcessMetricsData {
	return ProcessMetricsData{
		PID:           metrics.PID,
		CPUPercent:    metrics.CPUPercent,
		MemoryRSS:     metrics.MemoryRSS,
		MemoryVMS:     metrics.MemoryVMS,
		MemoryPercent: metrics.MemoryPercent,
		Running:       running,
		ProcessName:   metrics.ProcessName,
	}
}

// createProcessMetricsData creates ProcessMetricsData from ProcessMetrics with running status
func createProcessMetricsData(metrics *ProcessMetrics, running bool, processName string) ProcessMetricsData {
	if metrics == nil {
		return ProcessMetricsData{
			PID:           0,
			CPUPercent:    0.0,
			MemoryRSS:     0,
			MemoryVMS:     0,
			MemoryPercent: 0.0,
			Running:       false,
			ProcessName:   processName,
		}
	}
	return ProcessMetricsData{
		PID:           metrics.PID,
		CPUPercent:    metrics.CPUPercent,
		MemoryRSS:     metrics.MemoryRSS,
		MemoryVMS:     metrics.MemoryVMS,
		MemoryPercent: metrics.MemoryPercent,
		Running:       running,
		ProcessName:   metrics.ProcessName,
	}
}

// getInactiveProcessMetrics returns ProcessMetricsData for an inactive audio input process
func getInactiveProcessMetrics() ProcessMetricsData {
	return createProcessMetricsData(nil, false, "audio-input-server")
}

// getActiveAudioInputSupervisor safely retrieves the audio input supervisor if session is active
func getActiveAudioInputSupervisor() *AudioInputSupervisor {
	sessionProvider := GetSessionProvider()
	if !sessionProvider.IsSessionActive() {
		return nil
	}

	inputManager := sessionProvider.GetAudioInputManager()
	if inputManager == nil {
		return nil
	}

	return inputManager.GetSupervisor()
}

// createAudioEvent creates an AudioEvent
func createAudioEvent(eventType AudioEventType, data interface{}) AudioEvent {
	return AudioEvent{
		Type: eventType,
		Data: data,
	}
}

func (aeb *AudioEventBroadcaster) getMicrophoneProcessMetrics() ProcessMetricsData {
	inputSupervisor := getActiveAudioInputSupervisor()
	if inputSupervisor == nil {
		return getInactiveProcessMetrics()
	}

	processMetrics := inputSupervisor.GetProcessMetrics()
	if processMetrics == nil {
		return getInactiveProcessMetrics()
	}

	// If process is running but CPU is 0%, it means we're waiting for the second sample
	// to calculate CPU percentage. Return metrics with correct running status.
	if inputSupervisor.IsRunning() && processMetrics.CPUPercent == 0.0 {
		return createProcessMetricsData(processMetrics, true, processMetrics.ProcessName)
	}

	// Subprocess is running, return actual metrics
	return createProcessMetricsData(processMetrics, inputSupervisor.IsRunning(), processMetrics.ProcessName)
}

// sendCurrentMetrics sends current audio and microphone metrics to a subscriber
func (aeb *AudioEventBroadcaster) sendCurrentMetrics(subscriber *AudioEventSubscriber) {
	// Send audio metrics
	audioMetrics := GetAudioMetrics()
	audioMetricsEvent := createAudioEvent(AudioEventMetricsUpdate, convertAudioMetricsToEventDataWithLatencyMs(audioMetrics))
	aeb.sendToSubscriber(subscriber, audioMetricsEvent)

	// Send audio process metrics
	if outputSupervisor := GetAudioOutputSupervisor(); outputSupervisor != nil {
		if processMetrics := outputSupervisor.GetProcessMetrics(); processMetrics != nil {
			audioProcessEvent := createAudioEvent(AudioEventProcessMetrics, convertProcessMetricsToEventData(*processMetrics, outputSupervisor.IsRunning()))
			aeb.sendToSubscriber(subscriber, audioProcessEvent)
		}
	}

	// Send microphone metrics using session provider
	sessionProvider := GetSessionProvider()
	if sessionProvider.IsSessionActive() {
		if inputManager := sessionProvider.GetAudioInputManager(); inputManager != nil {
			micMetrics := inputManager.GetMetrics()
			micMetricsEvent := createAudioEvent(AudioEventMicrophoneMetrics, convertAudioInputMetricsToEventDataWithLatencyMs(micMetrics))
			aeb.sendToSubscriber(subscriber, micMetricsEvent)
		}
	}

	// Send microphone process metrics (always send, even when subprocess is not running)
	micProcessEvent := createAudioEvent(AudioEventMicProcessMetrics, aeb.getMicrophoneProcessMetrics())
	aeb.sendToSubscriber(subscriber, micProcessEvent)
}

// startMetricsBroadcasting starts a goroutine that periodically broadcasts metrics
func (aeb *AudioEventBroadcaster) startMetricsBroadcasting() {
	// Use centralized interval to match process monitor frequency for synchronized metrics
	ticker := time.NewTicker(GetMetricsUpdateInterval())
	defer ticker.Stop()

	for range ticker.C {
		aeb.mutex.RLock()
		subscriberCount := len(aeb.subscribers)

		// Early exit if no subscribers to save CPU
		if subscriberCount == 0 {
			aeb.mutex.RUnlock()
			continue
		}

		// Create a copy for safe iteration
		subscribersCopy := make([]*AudioEventSubscriber, 0, subscriberCount)
		for _, sub := range aeb.subscribers {
			subscribersCopy = append(subscribersCopy, sub)
		}
		aeb.mutex.RUnlock()

		// Pre-check for cancelled contexts to avoid unnecessary work
		activeSubscribers := 0
		for _, sub := range subscribersCopy {
			if sub.ctx.Err() == nil {
				activeSubscribers++
			}
		}

		// Skip metrics gathering if no active subscribers
		if activeSubscribers == 0 {
			continue
		}

		// Broadcast audio metrics
		audioMetrics := GetAudioMetrics()
		audioMetricsEvent := createAudioEvent(AudioEventMetricsUpdate, convertAudioMetricsToEventDataWithLatencyMs(audioMetrics))
		aeb.broadcast(audioMetricsEvent)

		// Broadcast microphone metrics if available using session provider
		sessionProvider := GetSessionProvider()
		if sessionProvider.IsSessionActive() {
			if inputManager := sessionProvider.GetAudioInputManager(); inputManager != nil {
				micMetrics := inputManager.GetMetrics()
				micMetricsEvent := createAudioEvent(AudioEventMicrophoneMetrics, convertAudioInputMetricsToEventDataWithLatencyMs(micMetrics))
				aeb.broadcast(micMetricsEvent)
			}
		}

		// Broadcast audio process metrics
		if outputSupervisor := GetAudioOutputSupervisor(); outputSupervisor != nil {
			if processMetrics := outputSupervisor.GetProcessMetrics(); processMetrics != nil {
				audioProcessEvent := createAudioEvent(AudioEventProcessMetrics, convertProcessMetricsToEventData(*processMetrics, outputSupervisor.IsRunning()))
				aeb.broadcast(audioProcessEvent)
			}
		}

		// Broadcast microphone process metrics (always broadcast, even when subprocess is not running)
		micProcessEvent := createAudioEvent(AudioEventMicProcessMetrics, aeb.getMicrophoneProcessMetrics())
		aeb.broadcast(micProcessEvent)
	}
}

// broadcast sends an event to all subscribers
func (aeb *AudioEventBroadcaster) broadcast(event AudioEvent) {
	aeb.mutex.RLock()
	// Create a copy of subscribers to avoid holding the lock during sending
	subscribersCopy := make(map[string]*AudioEventSubscriber)
	for id, sub := range aeb.subscribers {
		subscribersCopy[id] = sub
	}
	aeb.mutex.RUnlock()

	// Track failed subscribers to remove them after sending
	var failedSubscribers []string

	// Send to all subscribers without holding the lock
	for connectionID, subscriber := range subscribersCopy {
		if !aeb.sendToSubscriber(subscriber, event) {
			failedSubscribers = append(failedSubscribers, connectionID)
		}
	}

	// Remove failed subscribers if any
	if len(failedSubscribers) > 0 {
		aeb.mutex.Lock()
		for _, connectionID := range failedSubscribers {
			delete(aeb.subscribers, connectionID)
			aeb.logger.Warn().Str("connectionID", connectionID).Msg("removed failed audio events subscriber")
		}
		aeb.mutex.Unlock()
	}
}

// sendToSubscriber sends an event to a specific subscriber
func (aeb *AudioEventBroadcaster) sendToSubscriber(subscriber *AudioEventSubscriber, event AudioEvent) bool {
	// Check if subscriber context is already cancelled
	if subscriber.ctx.Err() != nil {
		return false
	}

	ctx, cancel := context.WithTimeout(subscriber.ctx, time.Duration(GetConfig().EventTimeoutSeconds)*time.Second)
	defer cancel()

	err := wsjson.Write(ctx, subscriber.conn, event)
	if err != nil {
		// Don't log network errors for closed connections as warnings, they're expected
		if strings.Contains(err.Error(), "use of closed network connection") ||
			strings.Contains(err.Error(), "connection reset by peer") ||
			strings.Contains(err.Error(), "context canceled") {
			subscriber.logger.Debug().Err(err).Msg("websocket connection closed during audio event send")
		} else {
			subscriber.logger.Warn().Err(err).Msg("failed to send audio event to subscriber")
		}
		return false
	}

	return true
}
