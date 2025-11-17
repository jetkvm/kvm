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
	setAudioTrackMutex sync.Mutex // Prevents concurrent setAudioTrack() calls
	inputSourceMutex   sync.Mutex // Serializes Connect() and WriteMessage() calls to input source
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

func getAudioConfig() audio.AudioConfig {
	ensureConfigLoaded()
	cfg := audio.DefaultAudioConfig()
	if config.AudioBitrate >= 64 && config.AudioBitrate <= 256 {
		cfg.Bitrate = uint16(config.AudioBitrate)
	}
	if config.AudioComplexity >= 0 && config.AudioComplexity <= 10 {
		cfg.Complexity = uint8(config.AudioComplexity)
	}
	cfg.DTXEnabled = config.AudioDTXEnabled
	cfg.FECEnabled = config.AudioFECEnabled
	if config.AudioBufferPeriods >= 2 && config.AudioBufferPeriods <= 24 {
		cfg.BufferPeriods = uint8(config.AudioBufferPeriods)
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

	if outputSource == nil && audioOutputEnabled.Load() && currentAudioTrack != nil {
		ensureConfigLoaded()
		alsaDevice := "hw:1,0"
		if config.AudioOutputSource == "hdmi" {
			alsaDevice = "hw:0,0"
		}

		source := audio.NewCgoOutputSource(alsaDevice)
		source.SetConfig(getAudioConfig())
		outputSource = source
		outputRelay = audio.NewOutputRelay(outputSource, currentAudioTrack)
		if err := outputRelay.Start(); err != nil {
			audioLogger.Error().Err(err).Msg("Failed to start audio output relay")
		}
	}

	ensureConfigLoaded()
	if inputSource.Load() == nil && audioInputEnabled.Load() && config.UsbDevices != nil && config.UsbDevices.Audio {
		alsaPlaybackDevice := "hw:1,0"

		source := audio.NewCgoInputSource(alsaPlaybackDevice)
		source.SetConfig(getAudioConfig())
		var audioSource audio.AudioSource = source
		inputSource.Store(&audioSource)

		inputRelay = audio.NewInputRelay(audioSource)
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
	setAudioTrackMutex.Lock()
	defer setAudioTrackMutex.Unlock()

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

	audioMutex.Lock()
	if currentAudioTrack != nil && audioOutputEnabled.Load() {
		ensureConfigLoaded()
		alsaDevice := "hw:1,0"
		if config.AudioOutputSource == "hdmi" {
			alsaDevice = "hw:0,0"
		}
		newSource := audio.NewCgoOutputSource(alsaDevice)
		newSource.SetConfig(getAudioConfig())
		newRelay := audio.NewOutputRelay(newSource, currentAudioTrack)
		outputSource = newSource
		outputRelay = newRelay
	}
	audioMutex.Unlock()

	// Capture relay reference and start it outside mutex
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

func SetAudioOutputEnabled(enabled bool) error {
	if audioOutputEnabled.Swap(enabled) == enabled {
		return nil
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

func SetAudioInputEnabled(enabled bool) error {
	if audioInputEnabled.Swap(enabled) == enabled {
		return nil
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

func SetAudioOutputSource(source string) error {
	if source != "hdmi" && source != "usb" {
		return nil
	}

	ensureConfigLoaded()
	if config.AudioOutputSource == source {
		return nil
	}

	config.AudioOutputSource = source

	stopOutputAudio()

	if audioOutputEnabled.Load() && activeConnections.Load() > 0 && currentAudioTrack != nil {
		alsaDevice := "hw:1,0"
		if source == "hdmi" {
			alsaDevice = "hw:0,0"
		}

		newSource := audio.NewCgoOutputSource(alsaDevice)
		newSource.SetConfig(getAudioConfig())
		newRelay := audio.NewOutputRelay(newSource, currentAudioTrack)

		audioMutex.Lock()
		outputSource = newSource
		outputRelay = newRelay
		audioMutex.Unlock()

		if err := newRelay.Start(); err != nil {
			audioLogger.Error().Err(err).Str("source", source).Msg("Failed to start audio relay with new source")
		}
	}

	return SaveConfig()
}

func RestartAudioOutput() {
	audioMutex.Lock()
	hasActiveOutput := outputSource != nil && currentAudioTrack != nil && audioOutputEnabled.Load()
	audioMutex.Unlock()

	if !hasActiveOutput {
		return
	}

	audioLogger.Info().Msg("Restarting audio output")

	stopOutputAudio()

	ensureConfigLoaded()
	alsaDevice := "hw:1,0"
	if config.AudioOutputSource == "hdmi" {
		alsaDevice = "hw:0,0"
	}

	newSource := audio.NewCgoOutputSource(alsaDevice)
	newSource.SetConfig(getAudioConfig())
	newRelay := audio.NewOutputRelay(newSource, currentAudioTrack)

	audioMutex.Lock()
	outputSource = newSource
	outputRelay = newRelay
	audioMutex.Unlock()

	if err := newRelay.Start(); err != nil {
		audioLogger.Error().Err(err).Msg("Failed to restart audio output")
	}
}

func handleInputTrackForSession(track *webrtc.TrackRemote) {
	myTrackID := track.ID()

	audioLogger.Debug().
		Str("codec", track.Codec().MimeType).
		Str("track_id", myTrackID).
		Msg("starting input track handler")

	for {
		currentTrackID := currentInputTrack.Load()
		if currentTrackID != nil && *currentTrackID != myTrackID {
			audioLogger.Debug().
				Str("my_track_id", myTrackID).
				Str("current_track_id", *currentTrackID).
				Msg("input track handler exiting - superseded")
			return
		}

		rtpPacket, _, err := track.ReadRTP()
		if err != nil {
			if err == io.EOF {
				audioLogger.Debug().Str("track_id", myTrackID).Msg("input track ended")
				return
			}
			audioLogger.Warn().Err(err).Str("track_id", myTrackID).Msg("failed to read RTP packet")
			continue
		}

		opusData := rtpPacket.Payload
		if len(opusData) == 0 {
			continue
		}

		if !audioInputEnabled.Load() {
			continue
		}

		source := inputSource.Load()
		if source == nil {
			continue
		}

		inputSourceMutex.Lock()

		if !(*source).IsConnected() {
			if err := (*source).Connect(); err != nil {
				inputSourceMutex.Unlock()
				continue
			}
		}

		if err := (*source).WriteMessage(0, opusData); err != nil {
			audioLogger.Warn().Err(err).Msg("failed to write audio message")
			(*source).Disconnect()
		}

		inputSourceMutex.Unlock()
	}
}
