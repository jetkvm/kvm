package kvm

import (
	"context"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/jetkvm/kvm/internal/audio"
	"github.com/rs/zerolog"
)

// AudioEventType represents different types of audio events
type AudioEventType string

const (
	AudioEventMuteChanged       AudioEventType = "audio-mute-changed"
	AudioEventMetricsUpdate     AudioEventType = "audio-metrics-update"
	AudioEventMicrophoneState   AudioEventType = "microphone-state-changed"
	AudioEventMicrophoneMetrics AudioEventType = "microphone-metrics-update"
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
	FramesReceived  int64  `json:"frames_received"`
	FramesDropped   int64  `json:"frames_dropped"`
	BytesProcessed  int64  `json:"bytes_processed"`
	LastFrameTime   string `json:"last_frame_time"`
	ConnectionDrops int64  `json:"connection_drops"`
	AverageLatency  string `json:"average_latency"`
}

// MicrophoneStateData represents microphone state data
type MicrophoneStateData struct {
	Running       bool `json:"running"`
	SessionActive bool `json:"session_active"`
}

// MicrophoneMetricsData represents microphone metrics data
type MicrophoneMetricsData struct {
	FramesSent      int64  `json:"frames_sent"`
	FramesDropped   int64  `json:"frames_dropped"`
	BytesProcessed  int64  `json:"bytes_processed"`
	LastFrameTime   string `json:"last_frame_time"`
	ConnectionDrops int64  `json:"connection_drops"`
	AverageLatency  string `json:"average_latency"`
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

// InitializeAudioEventBroadcaster initializes the global audio event broadcaster
func InitializeAudioEventBroadcaster() {
	audioEventOnce.Do(func() {
		l := logger.With().Str("component", "audio-events").Logger()
		audioEventBroadcaster = &AudioEventBroadcaster{
			subscribers: make(map[string]*AudioEventSubscriber),
			logger:      &l,
		}

		// Start metrics broadcasting goroutine
		go audioEventBroadcaster.startMetricsBroadcasting()
	})
}

// GetAudioEventBroadcaster returns the singleton audio event broadcaster
func GetAudioEventBroadcaster() *AudioEventBroadcaster {
	audioEventOnce.Do(func() {
		l := logger.With().Str("component", "audio-events").Logger()
		audioEventBroadcaster = &AudioEventBroadcaster{
			subscribers: make(map[string]*AudioEventSubscriber),
			logger:      &l,
		}

		// Start metrics broadcasting goroutine
		go audioEventBroadcaster.startMetricsBroadcasting()
	})
	return audioEventBroadcaster
}

// Subscribe adds a WebSocket connection to receive audio events
func (aeb *AudioEventBroadcaster) Subscribe(connectionID string, conn *websocket.Conn, ctx context.Context, logger *zerolog.Logger) {
	aeb.mutex.Lock()
	defer aeb.mutex.Unlock()

	aeb.subscribers[connectionID] = &AudioEventSubscriber{
		conn:   conn,
		ctx:    ctx,
		logger: logger,
	}

	aeb.logger.Info().Str("connectionID", connectionID).Msg("audio events subscription added")

	// Send initial state to new subscriber
	go aeb.sendInitialState(connectionID)
}

// Unsubscribe removes a WebSocket connection from audio events
func (aeb *AudioEventBroadcaster) Unsubscribe(connectionID string) {
	aeb.mutex.Lock()
	defer aeb.mutex.Unlock()

	delete(aeb.subscribers, connectionID)
	aeb.logger.Info().Str("connectionID", connectionID).Msg("audio events subscription removed")
}

// BroadcastAudioMuteChanged broadcasts audio mute state changes
func (aeb *AudioEventBroadcaster) BroadcastAudioMuteChanged(muted bool) {
	event := AudioEvent{
		Type: AudioEventMuteChanged,
		Data: AudioMuteData{Muted: muted},
	}
	aeb.broadcast(event)
}

// BroadcastMicrophoneStateChanged broadcasts microphone state changes
func (aeb *AudioEventBroadcaster) BroadcastMicrophoneStateChanged(running, sessionActive bool) {
	event := AudioEvent{
		Type: AudioEventMicrophoneState,
		Data: MicrophoneStateData{
			Running:       running,
			SessionActive: sessionActive,
		},
	}
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
		Data: AudioMuteData{Muted: audio.IsAudioMuted()},
	}
	aeb.sendToSubscriber(subscriber, muteEvent)

	// Send current microphone state
	sessionActive := currentSession != nil
	var running bool
	if sessionActive && currentSession.AudioInputManager != nil {
		running = currentSession.AudioInputManager.IsRunning()
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

// sendCurrentMetrics sends current audio and microphone metrics to a subscriber
func (aeb *AudioEventBroadcaster) sendCurrentMetrics(subscriber *AudioEventSubscriber) {
	// Send audio metrics
	audioMetrics := audio.GetAudioMetrics()
	audioMetricsEvent := AudioEvent{
		Type: AudioEventMetricsUpdate,
		Data: AudioMetricsData{
			FramesReceived:  audioMetrics.FramesReceived,
			FramesDropped:   audioMetrics.FramesDropped,
			BytesProcessed:  audioMetrics.BytesProcessed,
			LastFrameTime:   audioMetrics.LastFrameTime.Format("2006-01-02T15:04:05.000Z"),
			ConnectionDrops: audioMetrics.ConnectionDrops,
			AverageLatency:  audioMetrics.AverageLatency.String(),
		},
	}
	aeb.sendToSubscriber(subscriber, audioMetricsEvent)

	// Send microphone metrics
	if currentSession != nil && currentSession.AudioInputManager != nil {
		micMetrics := currentSession.AudioInputManager.GetMetrics()
		micMetricsEvent := AudioEvent{
			Type: AudioEventMicrophoneMetrics,
			Data: MicrophoneMetricsData{
				FramesSent:      micMetrics.FramesSent,
				FramesDropped:   micMetrics.FramesDropped,
				BytesProcessed:  micMetrics.BytesProcessed,
				LastFrameTime:   micMetrics.LastFrameTime.Format("2006-01-02T15:04:05.000Z"),
				ConnectionDrops: micMetrics.ConnectionDrops,
				AverageLatency:  micMetrics.AverageLatency.String(),
			},
		}
		aeb.sendToSubscriber(subscriber, micMetricsEvent)
	}
}

// startMetricsBroadcasting starts a goroutine that periodically broadcasts metrics
func (aeb *AudioEventBroadcaster) startMetricsBroadcasting() {
	ticker := time.NewTicker(2 * time.Second) // Same interval as current polling
	defer ticker.Stop()

	for range ticker.C {
		aeb.mutex.RLock()
		subscriberCount := len(aeb.subscribers)
		aeb.mutex.RUnlock()

		// Only broadcast if there are subscribers
		if subscriberCount == 0 {
			continue
		}

		// Broadcast audio metrics
		audioMetrics := audio.GetAudioMetrics()
		audioMetricsEvent := AudioEvent{
			Type: AudioEventMetricsUpdate,
			Data: AudioMetricsData{
				FramesReceived:  audioMetrics.FramesReceived,
				FramesDropped:   audioMetrics.FramesDropped,
				BytesProcessed:  audioMetrics.BytesProcessed,
				LastFrameTime:   audioMetrics.LastFrameTime.Format("2006-01-02T15:04:05.000Z"),
				ConnectionDrops: audioMetrics.ConnectionDrops,
				AverageLatency:  audioMetrics.AverageLatency.String(),
			},
		}
		aeb.broadcast(audioMetricsEvent)

		// Broadcast microphone metrics if available
		if currentSession != nil && currentSession.AudioInputManager != nil {
			micMetrics := currentSession.AudioInputManager.GetMetrics()
			micMetricsEvent := AudioEvent{
				Type: AudioEventMicrophoneMetrics,
				Data: MicrophoneMetricsData{
					FramesSent:      micMetrics.FramesSent,
					FramesDropped:   micMetrics.FramesDropped,
					BytesProcessed:  micMetrics.BytesProcessed,
					LastFrameTime:   micMetrics.LastFrameTime.Format("2006-01-02T15:04:05.000Z"),
					ConnectionDrops: micMetrics.ConnectionDrops,
					AverageLatency:  micMetrics.AverageLatency.String(),
				},
			}
			aeb.broadcast(micMetricsEvent)
		}
	}
}

// broadcast sends an event to all subscribers
func (aeb *AudioEventBroadcaster) broadcast(event AudioEvent) {
	aeb.mutex.RLock()
	defer aeb.mutex.RUnlock()

	for connectionID, subscriber := range aeb.subscribers {
		go func(id string, sub *AudioEventSubscriber) {
			if !aeb.sendToSubscriber(sub, event) {
				// Remove failed subscriber
				aeb.mutex.Lock()
				delete(aeb.subscribers, id)
				aeb.mutex.Unlock()
				aeb.logger.Warn().Str("connectionID", id).Msg("removed failed audio events subscriber")
			}
		}(connectionID, subscriber)
	}
}

// sendToSubscriber sends an event to a specific subscriber
func (aeb *AudioEventBroadcaster) sendToSubscriber(subscriber *AudioEventSubscriber, event AudioEvent) bool {
	ctx, cancel := context.WithTimeout(subscriber.ctx, 5*time.Second)
	defer cancel()

	err := wsjson.Write(ctx, subscriber.conn, event)
	if err != nil {
		subscriber.logger.Warn().Err(err).Msg("failed to send audio event to subscriber")
		return false
	}

	return true
}
