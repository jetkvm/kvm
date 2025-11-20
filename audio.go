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

var (
	audioMutex         sync.Mutex
	inputSourceMutex   sync.Mutex // Serializes Connect() and WriteMessage() calls to input source
	outputSource       atomic.Pointer[audio.AudioSource]
	inputSource        atomic.Pointer[audio.AudioSource]
	outputRelay        atomic.Pointer[audio.OutputRelay]
	inputRelay         atomic.Pointer[audio.InputRelay]
	audioInitialized   bool
	activeConnections  atomic.Int32
	audioLogger        zerolog.Logger
	currentAudioTrack  *webrtc.TrackLocalStaticSample
	currentInputTrack  atomic.Pointer[string]
	audioOutputEnabled atomic.Bool
	audioInputEnabled  atomic.Bool
)

func getAlsaDevice(source string) string {
	if source == "hdmi" {
		return "hw:0,0"
	}
	return "hw:1,0"
}

func initAudio() {
	audioLogger = logging.GetDefaultLogger().With().Str("component", "audio-manager").Logger()

	ensureConfigLoaded()
	audioOutputEnabled.Store(config.AudioOutputEnabled)
	audioInputEnabled.Store(config.AudioInputAutoEnable)

	audioLogger.Debug().Msg("Audio subsystem initialized")
	audioInitialized = true
}

func getAudioConfig() audio.AudioConfig {
	cfg := audio.DefaultAudioConfig()

	// Helper to validate numeric ranges and return sanitized values
	// Returns (value, true) if valid, (0, false) if invalid
	validateAndApply := func(value int, min int, max int, paramName string) (int, bool) {
		if value >= min && value <= max {
			return value, true
		}
		if value != 0 {
			audioLogger.Warn().Int(paramName, value).Msgf("Invalid %s, using default", paramName)
		}
		return 0, false
	}

	// Validate and apply bitrate
	if bitrate, valid := validateAndApply(config.AudioBitrate, 64, 256, "audio bitrate"); valid {
		cfg.Bitrate = uint16(bitrate)
	}

	// Validate and apply complexity
	if complexity, valid := validateAndApply(config.AudioComplexity, 0, 10, "audio complexity"); valid {
		cfg.Complexity = uint8(complexity)
	}

	cfg.DTXEnabled = config.AudioDTXEnabled
	cfg.FECEnabled = config.AudioFECEnabled

	// Validate and apply buffer periods
	if periods, valid := validateAndApply(config.AudioBufferPeriods, 2, 24, "buffer periods"); valid {
		cfg.BufferPeriods = uint8(periods)
	}

	// Opus-compatible rates only: 8k, 12k, 16k, 24k, 48k
	validRates := map[int]bool{8000: true, 12000: true, 16000: true, 24000: true, 48000: true}
	if validRates[config.AudioSampleRate] {
		cfg.SampleRate = uint32(config.AudioSampleRate)
	} else if config.AudioSampleRate != 0 {
		audioLogger.Warn().Int("sample_rate", config.AudioSampleRate).Uint32("default", cfg.SampleRate).Msg("Invalid sample rate, using default")
	}

	// Validate and apply packet loss percentage
	if pktLoss, valid := validateAndApply(config.AudioPacketLossPerc, 0, 100, "packet loss percentage"); valid {
		cfg.PacketLossPerc = uint8(pktLoss)
	}

	return cfg
}

func startAudio() error {
	audioMutex.Lock()
	defer audioMutex.Unlock()

	if !audioInitialized {
		audioLogger.Warn().Msg("Audio not initialized, skipping start")
		return nil
	}

	if activeConnections.Load() <= 0 {
		audioLogger.Debug().Msg("No active connections, skipping audio start")
		return nil
	}

	ensureConfigLoaded()

	var outputErr, inputErr error
	if audioOutputEnabled.Load() && currentAudioTrack != nil {
		outputErr = startOutputAudioUnderMutex(getAlsaDevice(config.AudioOutputSource))
	}

	if audioInputEnabled.Load() && config.UsbDevices != nil && config.UsbDevices.Audio {
		inputErr = startInputAudioUnderMutex(getAlsaDevice("usb"))
	}

	if outputErr != nil && inputErr != nil {
		return fmt.Errorf("audio start failed - output: %w, input: %v", outputErr, inputErr)
	}
	if outputErr != nil {
		return outputErr
	}
	return inputErr
}

func startOutputAudioUnderMutex(alsaOutputDevice string) error {
	oldRelay := outputRelay.Swap(nil)
	oldSource := outputSource.Swap(nil)

	if oldRelay != nil {
		oldRelay.Stop()
	}
	if oldSource != nil {
		(*oldSource).Disconnect()
	}

	newSource := audio.NewCgoOutputSource(alsaOutputDevice, getAudioConfig())
	newRelay := audio.NewOutputRelay(&newSource, currentAudioTrack)

	if err := newRelay.Start(); err != nil {
		audioLogger.Error().Err(err).Str("alsaOutputDevice", alsaOutputDevice).Msg("Failed to start audio output relay")
		return err
	}

	outputSource.Swap(&newSource)
	outputRelay.Swap(newRelay)
	return nil
}

func startInputAudioUnderMutex(alsaPlaybackDevice string) error {
	oldRelay := inputRelay.Swap(nil)
	oldSource := inputSource.Swap(nil)

	if oldRelay != nil {
		oldRelay.Stop()
	}
	if oldSource != nil {
		(*oldSource).Disconnect()
	}

	newSource := audio.NewCgoInputSource(alsaPlaybackDevice, getAudioConfig())
	newRelay := audio.NewInputRelay(&newSource)

	if err := newRelay.Start(); err != nil {
		audioLogger.Error().Err(err).Str("alsaPlaybackDevice", alsaPlaybackDevice).Msg("Failed to start input relay")
		return err
	}

	inputSource.Swap(&newSource)
	inputRelay.Swap(newRelay)
	return nil
}

func stopOutputAudio() {
	audioMutex.Lock()
	oldRelay := outputRelay.Swap(nil)
	oldSource := outputSource.Swap(nil)
	audioMutex.Unlock()

	if oldRelay != nil {
		oldRelay.Stop()
	}
	if oldSource != nil {
		(*oldSource).Disconnect()
	}
}

func stopInputAudio() {
	audioMutex.Lock()
	oldRelay := inputRelay.Swap(nil)
	oldSource := inputSource.Swap(nil)
	audioMutex.Unlock()

	if oldRelay != nil {
		oldRelay.Stop()
	}
	if oldSource != nil {
		(*oldSource).Disconnect()
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
	if count <= 0 {
		// Stop audio immediately to release HDMI audio device which shares hardware with video device
		stopAudio()
	}
}

func setAudioTrack(audioTrack *webrtc.TrackLocalStaticSample) {
	audioMutex.Lock()
	defer audioMutex.Unlock()

	outRelay := outputRelay.Swap(nil)
	outSource := outputSource.Swap(nil)
	if outRelay != nil {
		outRelay.Stop()
	}
	if outSource != nil {
		(*outSource).Disconnect()
	}

	currentAudioTrack = audioTrack

	if audioInitialized && activeConnections.Load() > 0 && audioOutputEnabled.Load() && currentAudioTrack != nil {
		if err := startOutputAudioUnderMutex(getAlsaDevice(config.AudioOutputSource)); err != nil {
			audioLogger.Error().Err(err).Msg("Failed to start output audio after track change")
		}
	}
}

func setPendingInputTrack(track *webrtc.TrackRemote) {
	trackID := new(string)
	*trackID = track.ID()
	currentInputTrack.Store(trackID)
	go handleInputTrackForSession(track)
}

func SetAudioOutputEnabled(enabled bool) error {
	if audioOutputEnabled.Swap(enabled) == enabled {
		return nil
	}

	if enabled && activeConnections.Load() > 0 {
		go func() {
			if err := startAudio(); err != nil {
				audioLogger.Error().Err(err).Msg("Failed to start output audio after enable")
			}
		}()
		return nil
	}
	stopOutputAudio()
	return nil
}

func SetAudioInputEnabled(enabled bool) error {
	if audioInputEnabled.Swap(enabled) == enabled {
		return nil
	}

	if enabled && activeConnections.Load() > 0 {
		go func() {
			if err := startAudio(); err != nil {
				audioLogger.Error().Err(err).Msg("Failed to start input audio after enable")
			}
		}()
		return nil
	}
	stopInputAudio()
	return nil
}

// SetAudioOutputSource switches between HDMI (hw:0,0) and USB (hw:1,0) audio capture.
//
// The function returns immediately after updating and persisting the config change,
// while the actual audio device switch happens asynchronously in the background:
// - Config save is synchronous to ensure the change persists even if the process crashes
// - Audio restart is async to avoid blocking the RPC caller during ALSA reconfiguration
//
// Note: The HDMI audio device (hw:0,0) can take 30-60 seconds to initialize due to
// TC358743 hardware characteristics. Callers receive success before audio actually switches.
func SetAudioOutputSource(source string) error {
	if source != "hdmi" && source != "usb" {
		return nil
	}

	ensureConfigLoaded()
	if config.AudioOutputSource == source {
		return nil
	}

	config.AudioOutputSource = source

	// Save config synchronously before starting async audio operations
	if err := SaveConfig(); err != nil {
		audioLogger.Error().Err(err).Msg("Failed to save config after audio source change")
		return err
	}

	// Handle audio restart asynchronously
	go func() {
		stopOutputAudio()
		if err := startAudio(); err != nil {
			audioLogger.Error().Err(err).Str("source", source).Msg("Failed to start audio output after source change")
		}
	}()

	return nil
}

func RestartAudioOutput() error {
	audioMutex.Lock()
	hasActiveOutput := audioOutputEnabled.Load() && currentAudioTrack != nil && outputSource.Load() != nil
	audioMutex.Unlock()

	if !hasActiveOutput {
		return nil
	}

	audioLogger.Info().Msg("Restarting audio output")
	stopOutputAudio()
	return startAudio()
}

func handleInputTrackForSession(track *webrtc.TrackRemote) {
	myTrackID := track.ID()

	trackLogger := audioLogger.With().
		Str("codec", track.Codec().MimeType).
		Str("track_id", myTrackID).
		Logger()

	trackLogger.Debug().Msg("starting input track handler")

	for {
		// Check if we've been superseded by another track
		currentTrackID := currentInputTrack.Load()
		if currentTrackID != nil && *currentTrackID != myTrackID {
			trackLogger.Debug().
				Str("current_track_id", *currentTrackID).
				Msg("input track handler exiting - superseded")
			return
		}

		// Read RTP packet
		rtpPacket, _, err := track.ReadRTP()
		if err != nil {
			if err == io.EOF {
				trackLogger.Debug().Msg("input track ended")
				return
			}
			trackLogger.Warn().Err(err).Msg("failed to read RTP packet")
			continue
		}

		// Skip empty payloads
		if len(rtpPacket.Payload) == 0 {
			continue
		}

		// Skip if input is disabled
		if !audioInputEnabled.Load() {
			continue
		}

		// Process the audio packet
		if err := processInputPacket(rtpPacket.Payload); err != nil {
			trackLogger.Warn().Err(err).Msg("failed to process audio packet")
		}
	}
}

// processInputPacket handles writing audio data to the input source
func processInputPacket(opusData []byte) error {
	// Early check to avoid mutex acquisition if source is nil
	if inputSource.Load() == nil {
		return nil
	}

	inputSourceMutex.Lock()
	defer inputSourceMutex.Unlock()

	// Reload source inside mutex to ensure we have the currently active source
	source := inputSource.Load()
	if source == nil {
		return nil
	}

	// Defensive null check - ensure dereferenced pointer is valid
	if *source == nil {
		return nil
	}

	// Ensure source is connected
	if !(*source).IsConnected() {
		if err := (*source).Connect(); err != nil {
			return err
		}
	}

	// Write the message
	if err := (*source).WriteMessage(0, opusData); err != nil {
		(*source).Disconnect()
		return err
	}

	return nil
}
