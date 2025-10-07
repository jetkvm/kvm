package kvm

import (
	"fmt"
	"io"
	"sync"
	"sync/atomic"

	"github.com/jetkvm/kvm/internal/audio"
	"github.com/jetkvm/kvm/internal/logging"
	"github.com/pion/webrtc/v4"
	"github.com/rs/zerolog"
)

const (
	socketPathOutput = "/var/run/audio_output.sock"
	socketPathInput  = "/var/run/audio_input.sock"
)

var (
	audioMutex           sync.Mutex
	outputSupervisor     *audio.Supervisor
	inputSupervisor      *audio.Supervisor
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

	if err := audio.ExtractEmbeddedBinaries(); err != nil {
		audioLogger.Error().Err(err).Msg("Failed to extract audio binaries")
		return
	}

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

// startAudioSubprocesses starts audio subprocesses and relays (skips already running ones)
func startAudioSubprocesses() error {
	audioMutex.Lock()
	defer audioMutex.Unlock()

	if !audioInitialized {
		audioLogger.Warn().Msg("Audio not initialized, skipping subprocess start")
		return nil
	}

	// Start output audio if not running and enabled
	if outputSource == nil && audioOutputEnabled.Load() {
		alsaDevice := "hw:0,0" // HDMI
		if useUSBForAudioOutput {
			alsaDevice = "hw:1,0" // USB
		}

		ensureConfigLoaded()
		audioMode := config.AudioMode
		if audioMode == "" {
			audioMode = "subprocess" // Default to subprocess
		}

		if audioMode == "in-process" {
			// In-process CGO mode
			outputSource = audio.NewCgoOutputSource(alsaDevice)
			audioLogger.Debug().
				Str("mode", "in-process").
				Str("device", alsaDevice).
				Msg("Audio output configured for in-process mode")
		} else {
			// Subprocess mode (default)
			outputSupervisor = audio.NewSupervisor(
				"audio-output",
				audio.GetAudioOutputBinaryPath(),
				socketPathOutput,
				[]string{
					"ALSA_CAPTURE_DEVICE=" + alsaDevice,
					"OPUS_BITRATE=128000",
					"OPUS_COMPLEXITY=5",
				},
			)

			if err := outputSupervisor.Start(); err != nil {
				audioLogger.Error().Err(err).Msg("Failed to start audio output supervisor")
				outputSupervisor = nil
				return err
			}

			outputSource = audio.NewIPCSource("audio-output", socketPathOutput, 0x4A4B4F55)
			audioLogger.Debug().
				Str("mode", "subprocess").
				Str("device", alsaDevice).
				Msg("Audio output configured for subprocess mode")
		}

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

		audioMode := config.AudioMode
		if audioMode == "" {
			audioMode = "subprocess" // Default to subprocess
		}

		if audioMode == "in-process" {
			// In-process CGO mode
			inputSource = audio.NewCgoInputSource(alsaPlaybackDevice)
			audioLogger.Debug().
				Str("mode", "in-process").
				Str("device", alsaPlaybackDevice).
				Msg("Audio input configured for in-process mode")
		} else {
			// Subprocess mode (default)
			inputSupervisor = audio.NewSupervisor(
				"audio-input",
				audio.GetAudioInputBinaryPath(),
				socketPathInput,
				[]string{
					"ALSA_PLAYBACK_DEVICE=hw:1,0",
					"OPUS_BITRATE=128000",
				},
			)

			if err := inputSupervisor.Start(); err != nil {
				audioLogger.Error().Err(err).Msg("Failed to start input supervisor")
				inputSupervisor = nil
				return err
			}

			inputSource = audio.NewIPCSource("audio-input", socketPathInput, 0x4A4B4D49)
			audioLogger.Debug().
				Str("mode", "subprocess").
				Str("device", alsaPlaybackDevice).
				Msg("Audio input configured for subprocess mode")
		}

		inputRelay = audio.NewInputRelay(inputSource)
		if err := inputRelay.Start(); err != nil {
			audioLogger.Error().Err(err).Msg("Failed to start input relay")
		}
	}

	return nil
}

// stopOutputSubprocessLocked stops output subprocess (assumes mutex is held)
func stopOutputSubprocessLocked() {
	if outputRelay != nil {
		outputRelay.Stop()
		outputRelay = nil
	}
	if outputSource != nil {
		outputSource.Disconnect()
		outputSource = nil
	}
	if outputSupervisor != nil {
		outputSupervisor.Stop()
		outputSupervisor = nil
	}
}

// stopInputSubprocessLocked stops input subprocess (assumes mutex is held)
func stopInputSubprocessLocked() {
	if inputRelay != nil {
		inputRelay.Stop()
		inputRelay = nil
	}
	if inputSource != nil {
		inputSource.Disconnect()
		inputSource = nil
	}
	if inputSupervisor != nil {
		inputSupervisor.Stop()
		inputSupervisor = nil
	}
}

// stopAudioSubprocessesLocked stops all audio subprocesses (assumes mutex is held)
func stopAudioSubprocessesLocked() {
	stopOutputSubprocessLocked()
	stopInputSubprocessLocked()
}

// stopAudioSubprocesses stops all audio subprocesses
func stopAudioSubprocesses() {
	audioMutex.Lock()
	defer audioMutex.Unlock()
	stopAudioSubprocessesLocked()
}

func onWebRTCConnect() {
	count := activeConnections.Add(1)
	if count == 1 {
		if err := startAudioSubprocesses(); err != nil {
			audioLogger.Error().Err(err).Msg("Failed to start audio subprocesses")
		}
	}
}

func onWebRTCDisconnect() {
	count := activeConnections.Add(-1)
	if count == 0 {
		// Stop audio immediately to release HDMI audio device which shares hardware with video device
		stopAudioSubprocesses()
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

	stopOutputSubprocessLocked()

	// Restart if there are active connections
	if activeConnections.Load() > 0 {
		audioMutex.Unlock()
		err := startAudioSubprocesses()
		audioMutex.Lock()
		if err != nil {
			audioLogger.Error().Err(err).Msg("Failed to restart audio output")
			return err
		}
	}

	return nil
}

// SetAudioMode switches between subprocess and in-process audio modes
func SetAudioMode(mode string) error {
	if mode != "subprocess" && mode != "in-process" {
		return fmt.Errorf("invalid audio mode: %s (must be 'subprocess' or 'in-process')", mode)
	}

	audioMutex.Lock()
	defer audioMutex.Unlock()

	ensureConfigLoaded()
	if config.AudioMode == mode {
		return nil // Already in desired mode
	}

	audioLogger.Info().
		Str("old_mode", config.AudioMode).
		Str("new_mode", mode).
		Msg("Switching audio mode")

	// Save new mode to config
	config.AudioMode = mode
	if err := SaveConfig(); err != nil {
		audioLogger.Error().Err(err).Msg("Failed to save config")
		return err
	}

	// Stop all audio (both output and input)
	stopAudioSubprocessesLocked()

	// Restart if there are active connections
	if activeConnections.Load() > 0 {
		audioMutex.Unlock()
		err := startAudioSubprocesses()
		audioMutex.Lock()
		if err != nil {
			audioLogger.Error().Err(err).Msg("Failed to restart audio with new mode")
			return err
		}
	}

	audioLogger.Info().Str("mode", mode).Msg("Audio mode switch completed")
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
			return startAudioSubprocesses()
		}
	} else {
		audioMutex.Lock()
		stopOutputSubprocessLocked()
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
			return startAudioSubprocesses()
		}
	} else {
		audioMutex.Lock()
		stopInputSubprocessLocked()
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
