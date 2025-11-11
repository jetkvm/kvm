package kvm

import (
	"io"
	"sync"
	"sync/atomic"

	"github.com/jetkvm/kvm/internal/audio"
	"github.com/jetkvm/kvm/internal/logging"
	"github.com/pion/webrtc/v4"
	"github.com/rs/zerolog"
)

var (
	audioMutex         sync.Mutex
	outputSource       audio.AudioSource
	inputSource        atomic.Pointer[audio.AudioSource]
	outputRelay        *audio.OutputRelay
	inputRelay         *audio.InputRelay
	audioInitialized   bool
	activeConnections  atomic.Int32
	audioLogger        zerolog.Logger
	currentAudioTrack  *webrtc.TrackLocalStaticSample
	currentInputTrack  atomic.Pointer[string]
	audioOutputEnabled atomic.Bool
	audioInputEnabled  atomic.Bool
)

func initAudio() {
	audioLogger = logging.GetDefaultLogger().With().Str("component", "audio-manager").Logger()

	ensureConfigLoaded()
	audioOutputEnabled.Store(config.AudioOutputEnabled)
	audioInputEnabled.Store(true)

	audioLogger.Debug().Msg("Audio subsystem initialized")
	audioInitialized = true
}

// startAudio starts audio sources and relays (skips already running ones)
func startAudio() error {
	audioMutex.Lock()
	defer audioMutex.Unlock()

	if !audioInitialized {
		audioLogger.Warn().Msg("Audio not initialized, skipping start")
		return nil
	}

	// Start output audio if not running and enabled
	if outputSource == nil && audioOutputEnabled.Load() {
		alsaDevice := "hw:1,0" // USB audio

		outputSource = audio.NewCgoOutputSource(alsaDevice)

		if currentAudioTrack != nil {
			outputRelay = audio.NewOutputRelay(outputSource, currentAudioTrack)
			if err := outputRelay.Start(); err != nil {
				audioLogger.Error().Err(err).Msg("Failed to start audio output relay")
			}
		}
	}

	// Start input audio if not running, USB audio enabled, and input enabled
	ensureConfigLoaded()
	if inputSource.Load() == nil && audioInputEnabled.Load() && config.UsbDevices != nil && config.UsbDevices.Audio {
		alsaPlaybackDevice := "hw:1,0" // USB speakers

		var source audio.AudioSource = audio.NewCgoInputSource(alsaPlaybackDevice)
		inputSource.Store(&source)

		inputRelay = audio.NewInputRelay(source)
		if err := inputRelay.Start(); err != nil {
			audioLogger.Error().Err(err).Msg("Failed to start input relay")
		}
	}

	return nil
}

func stopOutputAudio() {
	audioMutex.Lock()
	outRelay := outputRelay
	outSource := outputSource
	outputRelay = nil
	outputSource = nil
	audioMutex.Unlock()

	if outRelay != nil {
		outRelay.Stop()
	}
	if outSource != nil {
		outSource.Disconnect()
	}
}

func stopInputAudio() {
	audioMutex.Lock()
	inRelay := inputRelay
	inputRelay = nil
	audioMutex.Unlock()

	inSource := inputSource.Swap(nil)

	if inRelay != nil {
		inRelay.Stop()
	}
	if inSource != nil {
		(*inSource).Disconnect()
	}
}

func stopAudio() {
	stopOutputAudio()
	stopInputAudio()
}

func onWebRTCConnect() {
	count := activeConnections.Add(1)
	if count == 1 {
		if err := startAudio(); err != nil {
			audioLogger.Error().Err(err).Msg("Failed to start audio")
		}
	}
}

func onWebRTCDisconnect() {
	count := activeConnections.Add(-1)
	if count == 0 {
		// Stop audio immediately to release HDMI audio device which shares hardware with video device
		stopAudio()
	}
}

func setAudioTrack(audioTrack *webrtc.TrackLocalStaticSample) {
	audioMutex.Lock()
	currentAudioTrack = audioTrack
	oldRelay := outputRelay
	oldSource := outputSource
	outputRelay = nil
	outputSource = nil
	audioMutex.Unlock()

	// Stop relay and disconnect source outside mutex to avoid blocking during CGO calls
	if oldRelay != nil {
		oldRelay.Stop()
	}
	if oldSource != nil {
		oldSource.Disconnect()
	}

	// Create new source and relay for the new track
	audioMutex.Lock()
	if currentAudioTrack != nil && audioOutputEnabled.Load() {
		alsaDevice := "hw:1,0"
		newSource := audio.NewCgoOutputSource(alsaDevice)
		newRelay := audio.NewOutputRelay(newSource, currentAudioTrack)
		outputSource = newSource
		outputRelay = newRelay
	}
	audioMutex.Unlock()

	// Start new relay outside mutex
	audioMutex.Lock()
	relayToStart := outputRelay
	audioMutex.Unlock()

	if relayToStart != nil {
		if err := relayToStart.Start(); err != nil {
			audioLogger.Error().Err(err).Msg("Failed to start output relay")
		}
	}
}

func setPendingInputTrack(track *webrtc.TrackRemote) {
	trackID := track.ID()
	currentInputTrack.Store(&trackID)
	go handleInputTrackForSession(track)
}

// SetAudioOutputEnabled enables or disables audio output
func SetAudioOutputEnabled(enabled bool) error {
	if audioOutputEnabled.Swap(enabled) == enabled {
		return nil // Already in desired state
	}

	if enabled {
		if activeConnections.Load() > 0 {
			return startAudio()
		}
	} else {
		stopOutputAudio()
	}

	return nil
}

// SetAudioInputEnabled enables or disables audio input
func SetAudioInputEnabled(enabled bool) error {
	if audioInputEnabled.Swap(enabled) == enabled {
		return nil // Already in desired state
	}

	if enabled {
		if activeConnections.Load() > 0 {
			return startAudio()
		}
	} else {
		stopInputAudio()
	}

	return nil
}

// handleInputTrackForSession runs for the entire WebRTC session lifetime
// It continuously reads from the track and sends to whatever relay is currently active
func handleInputTrackForSession(track *webrtc.TrackRemote) {
	myTrackID := track.ID()

	audioLogger.Debug().
		Str("codec", track.Codec().MimeType).
		Str("track_id", myTrackID).
		Msg("starting session-lifetime track handler")

	for {
		// Check if we've been superseded by a new track
		currentTrackID := currentInputTrack.Load()
		if currentTrackID != nil && *currentTrackID != myTrackID {
			audioLogger.Debug().
				Str("my_track_id", myTrackID).
				Str("current_track_id", *currentTrackID).
				Msg("audio track handler exiting - superseded by new track")
			return
		}

		// Read RTP packet (must always read to keep track alive)
		rtpPacket, _, err := track.ReadRTP()
		if err != nil {
			if err == io.EOF {
				audioLogger.Debug().Str("track_id", myTrackID).Msg("audio track ended")
				return
			}
			audioLogger.Warn().Err(err).Str("track_id", myTrackID).Msg("failed to read RTP packet")
			continue
		}

		// Extract Opus payload
		opusData := rtpPacket.Payload
		if len(opusData) == 0 {
			continue
		}

		// Only send if input is enabled
		if !audioInputEnabled.Load() {
			continue // Drop frame but keep reading
		}

		// Lock-free source access (hot path optimization)
		source := inputSource.Load()

		if source == nil {
			continue // No relay, drop frame but keep reading
		}

		if !(*source).IsConnected() {
			if err := (*source).Connect(); err != nil {
				continue
			}
		}

		if err := (*source).WriteMessage(0, opusData); err != nil {
			audioLogger.Warn().Err(err).Msg("failed to write audio message")
			(*source).Disconnect()
		}
	}
}
