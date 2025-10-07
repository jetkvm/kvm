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
	audioMutex           sync.Mutex
	outputSource         audio.AudioSource
	inputSource          audio.AudioSource
	outputRelay          *audio.OutputRelay
	inputRelay           *audio.InputRelay
	audioInitialized     bool
	activeConnections    atomic.Int32
	audioLogger          zerolog.Logger
	currentAudioTrack    *webrtc.TrackLocalStaticSample
	inputTrackHandling   atomic.Bool
	useUSBForAudioOutput bool
	audioOutputEnabled   atomic.Bool
	audioInputEnabled    atomic.Bool
)

func initAudio() {
	audioLogger = logging.GetDefaultLogger().With().Str("component", "audio-manager").Logger()

	// Load audio output source from config
	ensureConfigLoaded()
	useUSBForAudioOutput = config.AudioOutputSource == "usb"

	// Enable both by default
	audioOutputEnabled.Store(true)
	audioInputEnabled.Store(true)

	audioLogger.Debug().
		Str("source", config.AudioOutputSource).
		Msg("Audio subsystem initialized")
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
		alsaDevice := "hw:0,0" // HDMI
		if useUSBForAudioOutput {
			alsaDevice = "hw:1,0" // USB
		}

		// Create CGO audio source
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
	if inputSource == nil && audioInputEnabled.Load() && config.UsbDevices != nil && config.UsbDevices.Audio {
		alsaPlaybackDevice := "hw:1,0" // USB speakers

		// Create CGO audio source
		inputSource = audio.NewCgoInputSource(alsaPlaybackDevice)

		inputRelay = audio.NewInputRelay(inputSource)
		if err := inputRelay.Start(); err != nil {
			audioLogger.Error().Err(err).Msg("Failed to start input relay")
		}
	}

	return nil
}

// stopOutputLocked stops output audio (assumes mutex is held)
func stopOutputLocked() {
	if outputRelay != nil {
		outputRelay.Stop()
		outputRelay = nil
	}
	if outputSource != nil {
		outputSource.Disconnect()
		outputSource = nil
	}
}

// stopInputLocked stops input audio (assumes mutex is held)
func stopInputLocked() {
	if inputRelay != nil {
		inputRelay.Stop()
		inputRelay = nil
	}
	if inputSource != nil {
		inputSource.Disconnect()
		inputSource = nil
	}
}

// stopAudioLocked stops all audio (assumes mutex is held)
func stopAudioLocked() {
	stopOutputLocked()
	stopInputLocked()
}

// stopAudio stops all audio
func stopAudio() {
	audioMutex.Lock()
	defer audioMutex.Unlock()
	stopAudioLocked()
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
	defer audioMutex.Unlock()

	currentAudioTrack = audioTrack

	if outputRelay != nil {
		outputRelay.Stop()
		outputRelay = nil
	}

	if outputSource != nil {
		outputRelay = audio.NewOutputRelay(outputSource, audioTrack)
		if err := outputRelay.Start(); err != nil {
			audioLogger.Error().Err(err).Msg("Failed to start output relay")
		}
	}
}

// SetAudioOutputSource switches between HDMI and USB audio output
func SetAudioOutputSource(useUSB bool) error {
	audioMutex.Lock()
	defer audioMutex.Unlock()

	if useUSBForAudioOutput == useUSB {
		return nil
	}

	audioLogger.Info().
		Bool("old_usb", useUSBForAudioOutput).
		Bool("new_usb", useUSB).
		Msg("Switching audio output source")

	useUSBForAudioOutput = useUSB

	ensureConfigLoaded()
	if useUSB {
		config.AudioOutputSource = "usb"
	} else {
		config.AudioOutputSource = "hdmi"
	}
	if err := SaveConfig(); err != nil {
		audioLogger.Error().Err(err).Msg("Failed to save config")
		return err
	}

	stopOutputLocked()

	// Restart if there are active connections
	if activeConnections.Load() > 0 {
		audioMutex.Unlock()
		err := startAudio()
		audioMutex.Lock()
		if err != nil {
			audioLogger.Error().Err(err).Msg("Failed to restart audio output")
			return err
		}
	}

	return nil
}

func setPendingInputTrack(track *webrtc.TrackRemote) {
	audioMutex.Lock()
	defer audioMutex.Unlock()

	// Start input track handler only once per WebRTC session
	if inputTrackHandling.CompareAndSwap(false, true) {
		go handleInputTrackForSession(track)
	}
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
		audioMutex.Lock()
		stopOutputLocked()
		audioMutex.Unlock()
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
		audioMutex.Lock()
		stopInputLocked()
		audioMutex.Unlock()
	}

	return nil
}

// handleInputTrackForSession runs for the entire WebRTC session lifetime
// It continuously reads from the track and sends to whatever relay is currently active
func handleInputTrackForSession(track *webrtc.TrackRemote) {
	defer inputTrackHandling.Store(false)

	audioLogger.Debug().
		Str("codec", track.Codec().MimeType).
		Str("track_id", track.ID()).
		Msg("starting session-lifetime track handler")

	for {
		// Read RTP packet (must always read to keep track alive)
		rtpPacket, _, err := track.ReadRTP()
		if err != nil {
			if err == io.EOF {
				audioLogger.Debug().Msg("audio track ended")
				return
			}
			audioLogger.Warn().Err(err).Msg("failed to read RTP packet")
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

		// Get source in single mutex operation (hot path optimization)
		audioMutex.Lock()
		source := inputSource
		audioMutex.Unlock()

		if source == nil {
			continue // No relay, drop frame but keep reading
		}

		if !source.IsConnected() {
			if err := source.Connect(); err != nil {
				continue
			}
		}

		if err := source.WriteMessage(0, opusData); err != nil {
			source.Disconnect()
		}
	}
}
