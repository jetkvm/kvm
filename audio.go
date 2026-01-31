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

// AudioInputOwner tracks which client has claimed audio input (first-come-first-served)
type AudioInputOwner int32

const (
	AudioInputOwnerNone   AudioInputOwner = 0
	AudioInputOwnerWebRTC AudioInputOwner = 1
	AudioInputOwnerRDP    AudioInputOwner = 2
)

var audioNopLogger = zerolog.Nop()

var (
	audioMutex         sync.Mutex
	inputSourceMutex   sync.Mutex // Serializes input audio packet handling: connection lifecycle and writes cannot overlap
	outputSource       atomic.Pointer[audio.AudioSource]
	inputSource        atomic.Pointer[audio.AudioSource]
	outputRelay        atomic.Pointer[audio.OutputRelay]
	inputRelay         atomic.Pointer[audio.InputRelay]
	audioInitialized   bool
	activeConnections  atomic.Int32
	audioLogger        = &audioNopLogger
	currentAudioTrack  *webrtc.TrackLocalStaticSample
	currentInputTrack  atomic.Pointer[string]
	audioOutputEnabled atomic.Bool
	audioInputEnabled  atomic.Bool

	// Audio input ownership - first-come-first-served
	// Only one client can send audio input at a time
	audioInputOwner atomic.Int32

	// Connection state tracking - prevents stale data from claiming ownership
	// after disconnect (e.g., buffered audio arriving after connection closes)
	rdpAudioInputActive    atomic.Bool
	webrtcAudioInputActive atomic.Bool
)

func getAlsaDevice(source string) string {
	if source == "hdmi" {
		return "hw:0,0" // TC358743 HDMI audio
	}
	return "hw:1,0" // USB Audio Gadget
}

// ClaimAudioInput attempts to claim audio input for the specified owner.
// Returns true if claimed successfully, false if already claimed by another.
func ClaimAudioInput(owner AudioInputOwner) bool {
	return audioInputOwner.CompareAndSwap(int32(AudioInputOwnerNone), int32(owner))
}

// ReleaseAudioInput releases audio input ownership if currently owned by the specified owner.
// Returns true if ownership was released, false if not owned by the specified owner.
func ReleaseAudioInput(owner AudioInputOwner) bool {
	return audioInputOwner.CompareAndSwap(int32(owner), int32(AudioInputOwnerNone))
}

// GetAudioInputOwner returns the current audio input owner.
func GetAudioInputOwner() AudioInputOwner {
	return AudioInputOwner(audioInputOwner.Load())
}

func initAudio() {
	audioLogger = logging.GetSubsystemLogger("audio")

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

	if config.AudioBufferPeriods >= 2 && config.AudioBufferPeriods <= 48 {
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

	return startAudioUnderMutex()
}

// startAudioForce starts audio without checking activeConnections.
// Used when relay is nil but we know a connection is active.
func startAudioForce() error {
	audioMutex.Lock()
	defer audioMutex.Unlock()

	if !audioInitialized {
		audioLogger.Warn().Msg("Audio not initialized, skipping start")
		return nil
	}

	return startAudioUnderMutex()
}

func startAudioUnderMutex() error {
	ensureConfigLoaded()

	var outputErr, inputErr error

	// Start output audio if enabled (always uses HDMI)
	// Audio capture works even without WebRTC track - for RDP audio output
	if audioOutputEnabled.Load() {
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

	// Set PCM callback for RDP audio output (with fast-path check)
	newRelay.SetPCMEnabledCheck(HasRDPAudioSubscribers)
	newRelay.SetPCMCallback(func(pcm []byte) {
		BroadcastRDPAudio(pcm)
	})

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

	// Connect the source to initialize ALSA playback device
	if err := newSource.Connect(); err != nil {
		audioLogger.Error().Err(err).Str("device", alsaPlaybackDevice).Msg("failed to connect input source")
		return err
	}

	if err := newRelay.Start(); err != nil {
		audioLogger.Error().Err(err).Str("device", alsaPlaybackDevice).Msg("failed to start input relay")
		newSource.Disconnect()
		return err
	}

	inputSource.Swap(&newSource)
	inputRelay.Swap(newRelay)

	// Drop any stale audio in the playback buffer.
	// This prevents accumulated audio from playing back when the host starts recording.
	// See: https://github.com/jetkvm/kvm/pull/718 (Marvur's observation)
	if err := audio.DropPlaybackBuffer(); err != nil {
		audioLogger.Warn().Err(err).Msg("failed to drop playback buffer")
	}

	audioLogger.Debug().Str("device", alsaPlaybackDevice).Msg("audio input started")
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

// incrementConnectionsAndEnsureAudio increments the active connection counter,
// fixes any corruption, and ensures the audio relay is running.
func incrementConnectionsAndEnsureAudio() {
	count := activeConnections.Add(1)
	if count <= 0 {
		activeConnections.Store(1)
	}
	if outputRelay.Load() == nil {
		if err := startAudioForce(); err != nil {
			audioLogger.Error().Err(err).Msg("failed to start audio")
		}
	}
}

func onWebRTCConnect() {
	incrementConnectionsAndEnsureAudio()
}

func onWebRTCDisconnect() {
	// Mark WebRTC audio input as inactive FIRST - prevents stale buffered data
	// from re-claiming ownership after we release it
	webrtcAudioInputActive.Store(false)

	// Clear input track to stop the track handler from processing/claiming
	currentInputTrack.Store(nil)

	// Release WebRTC audio input ownership - this allows RDP to claim if connected
	if ReleaseAudioInput(AudioInputOwnerWebRTC) {
		audioLogger.Debug().Msg("audio input: ownership released by WebRTC")
	}

	// Clear the WebRTC audio track since the connection is closing
	// This prevents the relay from trying to write to a closed track
	setAudioTrack(nil)

	// Decrement counter but prevent going negative
	count := activeConnections.Add(-1)
	if count < 0 {
		activeConnections.Store(0)
		count = 0
	}

	if count == 0 {
		// Stop audio immediately to release HDMI audio device which shares hardware with video device
		stopAudio()
	}
}

// OnRDPAudioConnect is called when an RDP client needs audio.
// This allows audio capture to start for RDP even without WebRTC.
func OnRDPAudioConnect() {
	rdpAudioInputActive.Store(true)
	incrementConnectionsAndEnsureAudio()
	audioLogger.Debug().Int32("connections", activeConnections.Load()).Msg("RDP audio connected")
}

// OnRDPAudioDisconnect is called when an RDP client disconnects audio.
func OnRDPAudioDisconnect() {
	// Mark RDP audio input as inactive FIRST - prevents stale buffered data
	// from re-claiming ownership after we release it
	rdpAudioInputActive.Store(false)

	// Release audio input ownership if RDP owned it - this allows WebRTC to claim if connected
	if ReleaseAudioInput(AudioInputOwnerRDP) {
		audioLogger.Debug().Msg("RDP released audio input")
	}

	// Decrement counter but prevent going negative
	count := activeConnections.Add(-1)
	if count < 0 {
		activeConnections.Store(0)
		count = 0
	}
	audioLogger.Debug().Int32("connections", count).Msg("RDP audio disconnected")

	if count == 0 {
		stopAudio()
	}
}

func setAudioTrack(audioTrack *webrtc.TrackLocalStaticSample) {
	audioMutex.Lock()
	defer audioMutex.Unlock()

	currentAudioTrack = audioTrack

	// If relay is already running (e.g., RDP connected first), just update the track
	// without stopping/restarting - this allows seamless WebRTC + RDP concurrent use
	if relay := outputRelay.Load(); relay != nil {
		relay.SetAudioTrack(audioTrack)
		audioLogger.Debug().Msg("Updated WebRTC audio track on existing relay")
		return
	}

	// No relay running - start one if there are active connections
	if audioInitialized && activeConnections.Load() > 0 && audioOutputEnabled.Load() {
		if err := startOutputAudioUnderMutex(getAlsaDevice("hdmi")); err != nil {
			audioLogger.Error().Err(err).Msg("Failed to start output audio after track change")
		}
	}
}

func setPendingInputTrack(track *webrtc.TrackRemote) {
	webrtcAudioInputActive.Store(true)
	trackID := track.ID()
	currentInputTrack.Store(&trackID)
	go handleInputTrackForSession(track)
}

// SetAudioOutputEnabled blocks up to 5 seconds when enabling.
// Returns error if audio fails to start within timeout.
func SetAudioOutputEnabled(enabled bool) error {
	wasEnabled := audioOutputEnabled.Swap(enabled)

	if enabled && activeConnections.Load() > 0 {
		// Check if output relay is actually running (handles case where relay died or was stopped)
		relay := outputRelay.Load()
		needsRestart := !wasEnabled || relay == nil || !relay.IsRunning()

		if !needsRestart {
			return nil
		}

		// Start audio synchronously with timeout to provide immediate feedback
		done := make(chan error, 1)
		go func() {
			done <- startAudio()
		}()

		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()

		select {
		case err := <-done:
			if err != nil {
				audioLogger.Error().Err(err).Msg("Failed to start output audio after enable")
				audioOutputEnabled.Store(false) // Revert state on failure
				return fmt.Errorf("failed to start audio output: %w", err)
			}
			return nil
		case <-timer.C:
			audioLogger.Error().Msg("Audio output start timed out after 5 seconds")
			audioOutputEnabled.Store(false) // Revert state on timeout
			go stopOutputAudio()            // Clean up any partial initialization asynchronously
			return fmt.Errorf("audio output start timed out after 5 seconds")
		}
	}

	if wasEnabled && !enabled {
		stopOutputAudio()
	}
	return nil
}

// SetAudioInputEnabled blocks up to 5 seconds when enabling.
// Returns error if audio fails to start within timeout.
// When enabled is true, this forces audio input to start regardless of config.UsbDevices.Audio
// and even if activeConnections is 0 (AUDIN may be ready before RDPSND).
func SetAudioInputEnabled(enabled bool) error {
	wasEnabled := audioInputEnabled.Swap(enabled)

	if enabled {
		// Check if input source is actually running (handles case where stopAudio was called)
		needsRestart := !wasEnabled || inputSource.Load() == nil

		if !needsRestart {
			return nil
		}

		// Start audio input directly (bypassing config check) since this is an explicit enable request.
		// This allows RDP AUDIN to work even if config.UsbDevices.Audio is false.
		// Note: we don't check activeConnections here because AUDIN channel may become ready
		// before RDPSND (which calls Audio.Connect() to increment activeConnections).
		done := make(chan error, 1)
		go func() {
			audioMutex.Lock()
			err := startInputAudioUnderMutex(getAlsaDevice("usb"))
			audioMutex.Unlock()
			done <- err
		}()

		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()

		select {
		case err := <-done:
			if err != nil {
				audioLogger.Error().Err(err).Msg("Failed to start input audio after enable")
				audioInputEnabled.Store(false) // Revert state on failure
				return fmt.Errorf("failed to start audio input: %w", err)
			}
			return nil
		case <-timer.C:
			audioLogger.Error().Msg("Audio input start timed out after 5 seconds")
			audioInputEnabled.Store(false) // Revert state on timeout
			go stopInputAudio()            // Clean up any partial initialization asynchronously
			return fmt.Errorf("audio input start timed out after 5 seconds")
		}
	}

	if wasEnabled && !enabled {
		stopInputAudio()
	}
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

	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()

	select {
	case err := <-done:
		if err != nil {
			audioLogger.Error().Err(err).Msg("Failed to restart audio output")
			return fmt.Errorf("failed to restart audio output: %w", err)
		}
		return nil
	case <-timer.C:
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

	// Release audio input ownership when this handler exits
	defer func() {
		if ReleaseAudioInput(AudioInputOwnerWebRTC) {
			trackLogger.Debug().Msg("WebRTC released audio input via track handler exit")
		}
	}()

	var consecutiveReadErrors int

	for {
		// Check if we've been superseded by another track or connection closed
		currentTrackID := currentInputTrack.Load()
		if currentTrackID == nil {
			trackLogger.Debug().Msg("input track handler exiting - connection closed")
			return
		}
		if *currentTrackID != myTrackID {
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
			// Exit on persistent errors - connection is likely dead
			if consecutiveReadErrors >= 500 {
				trackLogger.Error().Int("consecutive", consecutiveReadErrors).Msg("too many consecutive read errors, exiting track handler")
				return
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

// tryClaimAudioInput checks the activity flag, then tries to claim audio input
// ownership for the given owner. Returns true if the caller should proceed with
// writing audio data, false if the packet should be dropped.
func tryClaimAudioInput(owner AudioInputOwner, isActive func() bool) bool {
	if !isActive() {
		return false
	}
	current := GetAudioInputOwner()
	if current == AudioInputOwnerNone {
		if !ClaimAudioInput(owner) {
			return false
		}
		ownerName := "WebRTC"
		if owner == AudioInputOwnerRDP {
			ownerName = "RDP"
		}
		audioLogger.Info().Msgf("audio input: %s claimed ownership", ownerName)
		if err := audio.DropPlaybackBuffer(); err != nil {
			audioLogger.Warn().Err(err).Msg("audio input: failed to drop playback buffer")
		}
		return true
	}
	return current == owner
}

func processInputPacket(opusData []byte) error {
	if !tryClaimAudioInput(AudioInputOwnerWebRTC, webrtcAudioInputActive.Load) {
		return nil
	}

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

// Audio input failure tracking for automatic recovery.
// Uses atomic operations to avoid mutex overhead on the hot path.
var (
	audioInputFailures      atomic.Int32
	audioInputRecovering    atomic.Bool
	audioInputFailThreshold int32 = 5 // Trigger recovery after 5 consecutive failures
)

// WriteInputPCM writes raw PCM audio data to the input audio device (USB audio gadget).
// This is the hot path for RDP audio input - optimized for minimal overhead.
// Format: 16-bit signed PCM, mono, 48kHz.
func WriteInputPCM(pcmData []byte) error {
	if !tryClaimAudioInput(AudioInputOwnerRDP, rdpAudioInputActive.Load) {
		return nil
	}

	// Fast path: direct write to ALSA without locks or connection checks
	err := audio.WritePCM(pcmData)
	if err == nil {
		// Success - reset failure counter only if non-zero (avoids cache line bounce)
		if audioInputFailures.Load() != 0 {
			audioInputFailures.Store(0)
		}
		return nil
	}

	// Slow path: failure handling
	failures := audioInputFailures.Add(1)
	if failures >= audioInputFailThreshold && audioInputRecovering.CompareAndSwap(false, true) {
		// Trigger async recovery - don't block the audio path
		go recoverAudioInput()
	}

	return err
}

// recoverAudioInput attempts to reconnect the audio input device.
// Called asynchronously when consecutive failures exceed threshold.
// Retries with exponential backoff until the device comes back, the session
// ends, or audio input ownership changes. This handles USB cable unplug/replug
// where the ALSA device disappears temporarily and reappears later.
func recoverAudioInput() {
	defer audioInputRecovering.Store(false)

	audioLogger.Warn().Int32("failures", audioInputFailures.Load()).Msg("audio input: starting recovery")

	retryDelay := 500 * time.Millisecond

	for {
		// Abort if no active connections (audio is being shut down)
		if activeConnections.Load() <= 0 {
			audioLogger.Debug().Msg("audio input: aborting recovery - no active connections")
			audioInputFailures.Store(0)
			return
		}

		// Abort if we no longer own audio input
		if GetAudioInputOwner() != AudioInputOwnerRDP {
			audioLogger.Debug().Msg("audio input: aborting recovery - not RDP owner")
			audioInputFailures.Store(0)
			return
		}

		// Try to reconnect under the audio mutex
		audioMutex.Lock()

		// Re-check after acquiring lock
		if activeConnections.Load() <= 0 {
			audioMutex.Unlock()
			audioLogger.Debug().Msg("audio input: aborting recovery - connections dropped")
			audioInputFailures.Store(0)
			return
		}

		var recovered bool
		source := inputSource.Load()
		if source != nil && *source != nil {
			(*source).Disconnect()
			if err := (*source).Connect(); err == nil {
				recovered = true
			}
		} else {
			if err := startInputAudioUnderMutex(getAlsaDevice("usb")); err == nil {
				recovered = true
			}
		}

		audioMutex.Unlock()

		if recovered {
			audioInputFailures.Store(0)
			audioLogger.Info().Msg("audio input: recovery successful")
			return
		}

		// Device not ready yet — wait and retry with exponential backoff
		time.Sleep(retryDelay)
		retryDelay = min(retryDelay*2, 30*time.Second)
	}
}
