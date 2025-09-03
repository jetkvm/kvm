package audio

import (
	"context"
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
	AudioEventMuteChanged     AudioEventType = "audio-mute-changed"
	AudioEventMicrophoneState AudioEventType = "microphone-state-changed"
	AudioEventDeviceChanged   AudioEventType = "audio-device-changed"
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

// MicrophoneStateData represents microphone state data
type MicrophoneStateData struct {
	Running       bool `json:"running"`
	SessionActive bool `json:"session_active"`
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
}

// createAudioEvent creates an AudioEvent
func createAudioEvent(eventType AudioEventType, data interface{}) AudioEvent {
	return AudioEvent{
		Type: eventType,
		Data: data,
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
