package kvm

import (
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jetkvm/kvm/internal/audio"
	"github.com/jetkvm/kvm/internal/logging"
	"github.com/pion/webrtc/v4"
	"github.com/rs/zerolog"
)

var (
	audioMutex         sync.Mutex
	inputSourceMutex   sync.Mutex // Serializes input audio packet handling: connection lifecycle and writes cannot overlap
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
		return "hw:0,0" // TC358743 HDMI audio
	}
	return "hw:1,0" // USB Audio Gadget
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

	if config.AudioBitrate >= 64 && config.AudioBitrate <= 256 {
		cfg.Bitrate = uint16(config.AudioBitrate)
	} else if config.AudioBitrate != 0 {
		audioLogger.Warn().Int("bitrate", config.AudioBitrate).Msg("Invalid audio bitrate, using default")
	}

	if config.AudioComplexity >= 0 && config.AudioComplexity <= 10 {
		cfg.Complexity = uint8(config.AudioComplexity)
	} else if config.AudioComplexity != 0 {
		audioLogger.Warn().Int("complexity", config.AudioComplexity).Msg("Invalid audio complexity, using default")
	}

	if config.AudioBufferPeriods >= 2 && config.AudioBufferPeriods <= 24 {
		cfg.BufferPeriods = uint8(config.AudioBufferPeriods)
	} else if config.AudioBufferPeriods != 0 {
		audioLogger.Warn().Int("buffer_periods", config.AudioBufferPeriods).Msg("Invalid buffer periods, using default")
	}

	if config.AudioPacketLossPerc >= 0 && config.AudioPacketLossPerc <= 100 {
		cfg.PacketLossPerc = uint8(config.AudioPacketLossPerc)
	} else if config.AudioPacketLossPerc != 0 {
		audioLogger.Warn().Int("packet_loss_perc", config.AudioPacketLossPerc).Msg("Invalid packet loss percentage, using default")
	}

	cfg.DTXEnabled = config.AudioDTXEnabled
	cfg.FECEnabled = config.AudioFECEnabled

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

	// Start output audio if enabled and track is available (always uses HDMI)
	if audioOutputEnabled.Load() && currentAudioTrack != nil {
		outputErr = startOutputAudioUnderMutex(getAlsaDevice("hdmi"))
	}

	// Start input audio if enabled and USB audio device is configured
	if audioInputEnabled.Load() && config.UsbDevices != nil && config.UsbDevices.Audio {
		inputErr = startInputAudioUnderMutex(getAlsaDevice("usb"))
	}

	// Return combined errors if any
	if outputErr != nil && inputErr != nil {
		return fmt.Errorf("audio start failed - output: %w, input: %v", outputErr, inputErr)
	}
	return firstError(outputErr, inputErr)
}

func firstError(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
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
	newRelay := audio.NewInputRelay()

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
		if err := startOutputAudioUnderMutex(getAlsaDevice("hdmi")); err != nil {
			audioLogger.Error().Err(err).Msg("Failed to start output audio after track change")
		}
	}
}

func setPendingInputTrack(track *webrtc.TrackRemote) {
	trackID := track.ID()
	currentInputTrack.Store(&trackID)
	go handleInputTrackForSession(track)
}

// SetAudioOutputEnabled blocks up to 5 seconds when enabling.
// Returns error if audio fails to start within timeout.
func SetAudioOutputEnabled(enabled bool) error {
	if audioOutputEnabled.Swap(enabled) == enabled {
		return nil
	}

	if enabled && activeConnections.Load() > 0 {
		// Start audio synchronously with timeout to provide immediate feedback
		done := make(chan error, 1)
		go func() {
			done <- startAudio()
		}()

		select {
		case err := <-done:
			if err != nil {
				audioLogger.Error().Err(err).Msg("Failed to start output audio after enable")
				audioOutputEnabled.Store(false) // Revert state on failure
				return fmt.Errorf("failed to start audio output: %w", err)
			}
			return nil
		case <-time.After(5 * time.Second):
			audioLogger.Error().Msg("Audio output start timed out after 5 seconds")
			audioOutputEnabled.Store(false) // Revert state on timeout
			go stopOutputAudio()            // Clean up any partial initialization asynchronously
			return fmt.Errorf("audio output start timed out after 5 seconds")
		}
	}
	stopOutputAudio()
	return nil
}

// SetAudioInputEnabled blocks up to 5 seconds when enabling.
// Returns error if audio fails to start within timeout.
func SetAudioInputEnabled(enabled bool) error {
	if audioInputEnabled.Swap(enabled) == enabled {
		return nil
	}

	if enabled && activeConnections.Load() > 0 {
		// Start audio synchronously with timeout to provide immediate feedback
		done := make(chan error, 1)
		go func() {
			done <- startAudio()
		}()

		select {
		case err := <-done:
			if err != nil {
				audioLogger.Error().Err(err).Msg("Failed to start input audio after enable")
				audioInputEnabled.Store(false) // Revert state on failure
				return fmt.Errorf("failed to start audio input: %w", err)
			}
			return nil
		case <-time.After(5 * time.Second):
			audioLogger.Error().Msg("Audio input start timed out after 5 seconds")
			audioInputEnabled.Store(false) // Revert state on timeout
			go stopInputAudio()            // Clean up any partial initialization asynchronously
			return fmt.Errorf("audio input start timed out after 5 seconds")
		}
	}
	stopInputAudio()
	return nil
}

// RestartAudioOutput stops and restarts the audio output capture.
// Blocks up to 5 seconds waiting for audio to restart.
// Returns error if restart fails or times out.
func RestartAudioOutput() error {
	audioMutex.Lock()
	hasActiveOutput := audioOutputEnabled.Load() && currentAudioTrack != nil && outputSource.Load() != nil
	audioMutex.Unlock()

	if !hasActiveOutput {
		return nil
	}

	audioLogger.Info().Msg("Restarting audio output")
	stopOutputAudio()

	// Restart with timeout
	done := make(chan error, 1)
	go func() {
		done <- startAudio()
	}()

	select {
	case err := <-done:
		if err != nil {
			audioLogger.Error().Err(err).Msg("Failed to restart audio output")
			return fmt.Errorf("failed to restart audio output: %w", err)
		}
		return nil
	case <-time.After(5 * time.Second):
		audioLogger.Error().Msg("Audio output restart timed out")
		return fmt.Errorf("audio output restart timed out after 5 seconds")
	}
}

func handleInputTrackForSession(track *webrtc.TrackRemote) {
	myTrackID := track.ID()

	trackLogger := audioLogger.With().
		Str("codec", track.Codec().MimeType).
		Str("track_id", myTrackID).
		Logger()

	trackLogger.Debug().Msg("starting input track handler")

	var consecutiveReadErrors int

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
			consecutiveReadErrors++
			if consecutiveReadErrors == 1 || consecutiveReadErrors%100 == 0 {
				trackLogger.Warn().Err(err).Int("consecutive", consecutiveReadErrors).Msg("failed to read RTP packet")
			}
			// Backoff on persistent errors to prevent CPU spin
			if consecutiveReadErrors > 5 {
				time.Sleep(time.Duration(min(consecutiveReadErrors, 10)) * 10 * time.Millisecond)
			}
			continue
		}
		consecutiveReadErrors = 0

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

func processInputPacket(opusData []byte) error {
	inputSourceMutex.Lock()
	defer inputSourceMutex.Unlock()

	source := inputSource.Load()
	if source == nil || *source == nil {
		return nil
	}

	// Lazy connect on first use
	if !(*source).IsConnected() {
		if err := (*source).Connect(); err != nil {
			return err
		}
	}

	// Write opus data, disconnect on error
	if err := (*source).WriteMessage(0, opusData); err != nil {
		(*source).Disconnect()
		return err
	}

	return nil
}
