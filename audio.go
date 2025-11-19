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
	audioInputEnabled.Store(true)

	audioLogger.Debug().Msg("Audio subsystem initialized")
	audioInitialized = true
}

func getAudioConfig() audio.AudioConfig {
	cfg := audio.DefaultAudioConfig()

	// Validate and apply bitrate
	if config.AudioBitrate >= 64 && config.AudioBitrate <= 256 {
		cfg.Bitrate = uint16(config.AudioBitrate)
	} else if config.AudioBitrate != 0 {
		audioLogger.Warn().Int("bitrate", config.AudioBitrate).Uint16("default", cfg.Bitrate).Msg("Invalid audio bitrate, using default")
	}

	// Validate and apply complexity
	if config.AudioComplexity >= 0 && config.AudioComplexity <= 10 {
		cfg.Complexity = uint8(config.AudioComplexity)
	} else {
		audioLogger.Warn().Int("complexity", config.AudioComplexity).Uint8("default", cfg.Complexity).Msg("Invalid audio complexity, using default")
	}

	// Apply boolean flags directly
	cfg.DTXEnabled = config.AudioDTXEnabled
	cfg.FECEnabled = config.AudioFECEnabled

	// Validate and apply buffer periods
	if config.AudioBufferPeriods >= 2 && config.AudioBufferPeriods <= 24 {
		cfg.BufferPeriods = uint8(config.AudioBufferPeriods)
	} else if config.AudioBufferPeriods != 0 {
		audioLogger.Warn().Int("buffer_periods", config.AudioBufferPeriods).Uint8("default", cfg.BufferPeriods).Msg("Invalid buffer periods, using default")
	}

	// Validate and apply sample rate using a map for valid rates
	validRates := map[int]bool{32000: true, 44100: true, 48000: true, 96000: true}
	if validRates[config.AudioSampleRate] {
		cfg.SampleRate = uint32(config.AudioSampleRate)
	} else if config.AudioSampleRate != 0 {
		audioLogger.Warn().Int("sample_rate", config.AudioSampleRate).Uint32("default", cfg.SampleRate).Msg("Invalid sample rate, using default")
	}

	// Validate and apply packet loss percentage
	if config.AudioPacketLossPerc >= 0 && config.AudioPacketLossPerc <= 100 {
		cfg.PacketLossPerc = uint8(config.AudioPacketLossPerc)
	} else {
		audioLogger.Warn().Int("packet_loss_perc", config.AudioPacketLossPerc).Uint8("default", cfg.PacketLossPerc).Msg("Invalid packet loss percentage, using default")
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

	if audioOutputEnabled.Load() && currentAudioTrack != nil {
		startOutputAudioUnderMutex(getAlsaDevice(config.AudioOutputSource))
	}

	if audioInputEnabled.Load() && config.UsbDevices != nil && config.UsbDevices.Audio {
		startInputAudioUnderMutex(getAlsaDevice("usb"))
	}

	return nil
}

func startOutputAudioUnderMutex(alsaOutputDevice string) {
	newSource := audio.NewCgoOutputSource(alsaOutputDevice, getAudioConfig())
	oldSource := outputSource.Swap(&newSource)
	newRelay := audio.NewOutputRelay(&newSource, currentAudioTrack)
	oldRelay := outputRelay.Swap(newRelay)

	if oldRelay != nil {
		oldRelay.Stop()
	}

	if oldSource != nil {
		(*oldSource).Disconnect()
	}

	if err := newRelay.Start(); err != nil {
		audioLogger.Error().Err(err).Str("alsaOutputDevice", alsaOutputDevice).Msg("Failed to start audio output relay")
	}
}

func startInputAudioUnderMutex(alsaPlaybackDevice string) {
	newSource := audio.NewCgoInputSource(alsaPlaybackDevice, getAudioConfig())
	oldSource := inputSource.Swap(&newSource)
	newRelay := audio.NewInputRelay(&newSource)
	oldRelay := inputRelay.Swap(newRelay)

	if oldRelay != nil {
		oldRelay.Stop()
	}

	if oldSource != nil {
		(*oldSource).Disconnect()
	}

	if err := newRelay.Start(); err != nil {
		audioLogger.Error().Err(err).Str("alsaPlaybackDevice", alsaPlaybackDevice).Msg("Failed to start input relay")
	}
}

// stopAudioComponents safely stops and cleans up audio components
func stopAudioComponents(relay *atomic.Pointer[audio.OutputRelay], source *atomic.Pointer[audio.AudioSource]) {
	audioMutex.Lock()
	oldRelay := relay.Swap(nil)
	oldSource := source.Swap(nil)
	audioMutex.Unlock()

	if oldRelay != nil {
		oldRelay.Stop()
	}
	if oldSource != nil {
		(*oldSource).Disconnect()
	}
}

// stopAudioComponentsInput safely stops and cleans up input audio components
func stopAudioComponentsInput(relay *atomic.Pointer[audio.InputRelay], source *atomic.Pointer[audio.AudioSource]) {
	audioMutex.Lock()
	oldRelay := relay.Swap(nil)
	oldSource := source.Swap(nil)
	audioMutex.Unlock()

	if oldRelay != nil {
		oldRelay.Stop()
	}
	if oldSource != nil {
		(*oldSource).Disconnect()
	}
}

func stopOutputAudio() {
	stopAudioComponents(&outputRelay, &outputSource)
}

func stopInputAudio() {
	stopAudioComponentsInput(&inputRelay, &inputSource)
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

	// Stop output without mutex (already holding audioMutex)
	outRelay := outputRelay.Swap(nil)
	outSource := outputSource.Swap(nil)
	if outRelay != nil {
		outRelay.Stop()
	}
	if outSource != nil {
		(*outSource).Disconnect()
	}

	currentAudioTrack = audioTrack

	// Start audio without taking mutex again (already holding audioMutex)
	if audioInitialized && activeConnections.Load() > 0 && audioOutputEnabled.Load() && currentAudioTrack != nil {
		startOutputAudioUnderMutex(getAlsaDevice(config.AudioOutputSource))
	}
}

func setPendingInputTrack(track *webrtc.TrackRemote) {
	trackID := track.ID()
	currentInputTrack.Store(&trackID)
	go handleInputTrackForSession(track)
}

func SetAudioOutputEnabled(enabled bool) error {
	if audioOutputEnabled.Swap(enabled) == enabled {
		return nil
	}

	if enabled && activeConnections.Load() > 0 {
		return startAudio()
	}
	stopOutputAudio()
	return nil
}

func SetAudioInputEnabled(enabled bool) error {
	if audioInputEnabled.Swap(enabled) == enabled {
		return nil
	}

	if enabled && activeConnections.Load() > 0 {
		return startAudio()
	}
	stopInputAudio()
	return nil
}

func SetAudioOutputSource(source string) error {
	if source != "hdmi" && source != "usb" {
		return nil
	}

	ensureConfigLoaded()
	if config.AudioOutputSource == source {
		return nil
	}

	config.AudioOutputSource = source

	go func() {
		stopOutputAudio()
		if err := startAudio(); err != nil {
			audioLogger.Error().Err(err).Str("source", source).Msg("Failed to start audio output after source change")
		}
	}()

	go func() {
		if err := SaveConfig(); err != nil {
			audioLogger.Error().Err(err).Msg("Failed to save config after audio source change")
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
