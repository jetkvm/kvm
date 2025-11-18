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
	} else {
		return "hw:1,0"
	}
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
	// config is already loaded
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
	if config.AudioSampleRate == 32000 || config.AudioSampleRate == 44100 || config.AudioSampleRate == 48000 || config.AudioSampleRate == 96000 {
		cfg.SampleRate = uint32(config.AudioSampleRate)
	}
	if config.AudioPacketLossPerc >= 0 && config.AudioPacketLossPerc <= 100 {
		cfg.PacketLossPerc = uint8(config.AudioPacketLossPerc)
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

func stopOutputAudio() {
	audioMutex.Lock()
	outRelay := outputRelay.Swap(nil)
	outSource := outputSource.Swap(nil)
	audioMutex.Unlock()

	if outRelay != nil {
		outRelay.Stop()
	}
	if outSource != nil {
		(*outSource).Disconnect()
	}
}

func stopInputAudio() {
	audioMutex.Lock()
	inRelay := inputRelay.Swap(nil)
	inSource := inputSource.Swap(nil)
	audioMutex.Unlock()

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
	if count <= 0 {
		// Stop audio immediately to release HDMI audio device which shares hardware with video device
		stopAudio()
	}
}

func setAudioTrack(audioTrack *webrtc.TrackLocalStaticSample) {
	setAudioTrackMutex.Lock()
	defer setAudioTrackMutex.Unlock()

	stopOutputAudio()

	currentAudioTrack = audioTrack

	if err := startAudio(); err != nil {
		audioLogger.Error().Err(err).Msg("Failed to start with new audio track")
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

	stopOutputAudio()
	config.AudioOutputSource = source

	if err := startAudio(); err != nil {
		audioLogger.Error().Err(err).Str("source", source).Msg("Failed to start audio output after source change")
	}

	return SaveConfig()
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
		currentTrackID := currentInputTrack.Load()
		if currentTrackID != nil && *currentTrackID != myTrackID {
			trackLogger.Debug().
				Str("current_track_id", *currentTrackID).
				Msg("input track handler exiting - superseded")
			return
		}

		rtpPacket, _, err := track.ReadRTP()
		if err != nil {
			if err == io.EOF {
				trackLogger.Debug().Msg("input track ended")
				return
			}
			trackLogger.Warn().Err(err).Msg("failed to read RTP packet")
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
