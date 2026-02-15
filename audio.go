package kvm

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
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

// String returns a human-readable name for the audio input owner.
func (o AudioInputOwner) String() string {
	switch o {
	case AudioInputOwnerRDP:
		return "RDP"
	case AudioInputOwnerWebRTC:
		return "WebRTC"
	default:
		return "none"
	}
}

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

	cfg := loadCfg()
	audioOutputEnabled.Store(cfg.AudioOutputEnabled)
	audioInputEnabled.Store(cfg.AudioInputAutoEnable)

	audioLogger.Debug().Msg("Audio subsystem initialized")
	audioInitialized = true
}

func getAudioConfig() audio.AudioConfig {
	acfg := audio.DefaultAudioConfig()
	c := loadCfg()

	if c.AudioBitrate >= 64 && c.AudioBitrate <= 256 {
		acfg.Bitrate = uint16(c.AudioBitrate)
	} else if c.AudioBitrate != 0 {
		audioLogger.Warn().Int("bitrate", c.AudioBitrate).Msg("Invalid audio bitrate, using default")
	}

	if c.AudioComplexity >= 0 && c.AudioComplexity <= 10 {
		acfg.Complexity = uint8(c.AudioComplexity)
	} else if c.AudioComplexity != 0 {
		audioLogger.Warn().Int("complexity", c.AudioComplexity).Msg("Invalid audio complexity, using default")
	}

	if c.AudioBufferPeriods >= 2 && c.AudioBufferPeriods <= 48 {
		acfg.BufferPeriods = uint8(c.AudioBufferPeriods)
	} else if c.AudioBufferPeriods != 0 {
		audioLogger.Warn().Int("buffer_periods", c.AudioBufferPeriods).Msg("Invalid buffer periods, using default")
	}

	if c.AudioPacketLossPerc >= 0 && c.AudioPacketLossPerc <= 100 {
		acfg.PacketLossPerc = uint8(c.AudioPacketLossPerc)
	} else if c.AudioPacketLossPerc != 0 {
		audioLogger.Warn().Int("packet_loss_perc", c.AudioPacketLossPerc).Msg("Invalid packet loss percentage, using default")
	}

	acfg.DTXEnabled = c.AudioDTXEnabled
	acfg.FECEnabled = c.AudioFECEnabled

	return acfg
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
	var outputErr, inputErr error

	// Start output audio if enabled (always uses HDMI)
	// Audio capture works even without WebRTC track - for RDP audio output
	if audioOutputEnabled.Load() {
		outputErr = startOutputAudioUnderMutex(getAlsaDevice("hdmi"))
	}

	// Start input audio if enabled and USB audio device is configured
	cfg := loadCfg()
	if audioInputEnabled.Load() && cfg.UsbDevices != nil && cfg.UsbDevices.Audio {
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
	newSource := audio.NewCgoInputSource(alsaPlaybackDevice, getAudioConfig())
	newRelay := audio.NewInputRelay()

	// Connect the source to initialize ALSA playback device.
	// Try this BEFORE tearing down the old source, so a failure doesn't leave
	// inputSource nil (which makes WritePCM return -1 with no path to recovery).
	if err := newSource.Connect(); err != nil {
		audioLogger.Error().Err(err).Str("device", alsaPlaybackDevice).Msg("failed to connect input source")
		newSource.Disconnect() // Clean up any partial ALSA state
		return err
	}

	if err := newRelay.Start(); err != nil {
		audioLogger.Error().Err(err).Str("device", alsaPlaybackDevice).Msg("failed to start input relay")
		newSource.Disconnect()
		return err
	}

	// New source connected successfully — now tear down the old one.
	oldRelay := inputRelay.Swap(newRelay)
	oldSource := inputSource.Swap(&newSource)

	if oldRelay != nil {
		oldRelay.Stop()
	}
	if oldSource != nil {
		(*oldSource).Disconnect()
	}

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

	// Cancel any pending reboot monitoring — if audio input is being stopped
	// (RDP disconnect, user disabled audio, etc), the reboot is no longer needed.
	audioRebootAfterReplug.Store(false)

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
		audioLogger.Info().Str("owner", owner.String()).Msg("audio input: claimed ownership")
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

	source := inputSource.Load()
	if source == nil || *source == nil {
		return nil
	}

	// Slow path: lazy connect on first use (needs mutex for lifecycle safety)
	if !(*source).IsConnected() {
		inputSourceMutex.Lock()
		// Re-check under lock (source may have changed)
		source = inputSource.Load()
		if source == nil || *source == nil {
			inputSourceMutex.Unlock()
			return nil
		}
		if !(*source).IsConnected() {
			if err := (*source).Connect(); err != nil {
				inputSourceMutex.Unlock()
				trackAudioInputFailureAndRecover()
				return err
			}
		}
		inputSourceMutex.Unlock()
	}

	// Hot path: write without holding mutex (atomic pointer ensures source stability)
	if err := (*source).WriteMessage(0, opusData); err != nil {
		// Error path: disconnect under mutex for lifecycle safety
		inputSourceMutex.Lock()
		source = inputSource.Load()
		if source != nil && *source != nil {
			(*source).Disconnect()
		}
		inputSourceMutex.Unlock()
		trackAudioInputFailureAndRecover()
		return err
	}

	if audioInputFailures.Load() != 0 {
		audioInputFailures.Store(0)
	}

	return nil
}

// Audio input recovery thresholds.
const (
	audioInputFailThreshold  int32 = 5   // Trigger ALSA recovery after 5 consecutive write errors
	audioRecoveryMaxAttempts int32 = 10  // Give up after 10 failed ALSA recovery attempts
	audioRebootSkipThreshold int64 = 50  // Skips after replug to trigger reboot (~5s at ~10 packets/sec)
	audioRebootMarkerPath          = "/userdata/jetkvm/.audio_reboot_ts"
	audioRebootCooldownSecs  int64 = 600 // 10 minutes between audio-triggered reboots
)

// Audio input failure tracking for automatic recovery.
// Uses atomic operations to avoid mutex overhead on the hot path.
var (
	audioInputFailures   atomic.Int32
	audioInputRecovering atomic.Bool
	audioInputWriteOK    atomic.Int64 // Frames actually delivered to ALSA
	audioInputWriteSkip  atomic.Int64 // Frames skipped (buffer full / EAGAIN)

	// Reboot-after-replug: when a genuine USB cable replug occurs and the
	// endpoint stays dead (host doesn't activate alt setting 1), reboot
	// the device as a last resort. Only fires once per replug event,
	// with persistent cooldown to prevent loops.
	audioRebootAfterReplug atomic.Bool
)

// resetAudioInputCounters resets all audio input tracking counters to zero.
// Must be called whenever recovery succeeds or monitoring is restarted.
func resetAudioInputCounters() {
	audioInputFailures.Store(0)
	audioInputWriteOK.Store(0)
	audioInputWriteSkip.Store(0)
}

// trackAudioInputFailureAndRecover increments the failure counter and triggers
// async recovery if the threshold is exceeded and no recovery is already running.
func trackAudioInputFailureAndRecover() {
	failures := audioInputFailures.Add(1)
	if failures >= audioInputFailThreshold && audioInputRecovering.CompareAndSwap(false, true) {
		go recoverAudioInput()
	}
}

// tryLockMutexWithTimeout attempts to acquire a mutex within the given timeout.
// Returns true if the lock was acquired, false if the timeout expired.
// Uses TryLock polling to prevent permanent blocking when a C call
// (e.g. snd_pcm_open) holds the mutex.
func tryLockMutexWithTimeout(mu *sync.Mutex, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if mu.TryLock() {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

// WriteInputPCM writes raw PCM audio data to the input audio device (USB audio gadget).
// This is the hot path for RDP audio input - optimized for minimal overhead.
// Format: 16-bit signed PCM, mono, 48kHz.
func WriteInputPCM(pcmData []byte) error {
	if !tryClaimAudioInput(AudioInputOwnerRDP, rdpAudioInputActive.Load) {
		return nil
	}

	// Fast path: direct write to ALSA without locks or connection checks
	frames, err := audio.WritePCM(pcmData)
	if err == nil {
		// Success - reset failure counter only if non-zero (avoids cache line bounce)
		if audioInputFailures.Load() != 0 {
			audioInputFailures.Store(0)
		}
		if frames > 0 {
			// Data written to ALSA ring buffer. Note: this doesn't confirm
			// the host is consuming — ALSA buffers locally and xrun recovery
			// resets the buffer, allowing writes even with a dead endpoint.
			n := audioInputWriteOK.Add(1)
			if n == 1 {
				audioLogger.Info().Int64("ok", n).Int64("skipped", audioInputWriteSkip.Load()).Int("frames", frames).
					Msg("audio input: first frame delivered")
			}
			// If 100+ frames delivered with zero skips, the endpoint is healthy.
			// Cancel any pending reboot monitoring from a previous replug.
			if n == 100 && audioInputWriteSkip.Load() == 0 && audioRebootAfterReplug.Load() {
				audioRebootAfterReplug.Store(false)
				audioLogger.Info().Msg("audio input: endpoint healthy after replug, reboot monitoring cancelled")
			}
		} else {
			// Frame skipped: ALSA buffer full because host isn't consuming
			// from the USB isochronous endpoint. A healthy endpoint produces
			// zero skips (host drains at production rate). Any significant
			// skip count means the USB endpoint is inactive (host hasn't
			// selected the active alt setting on the AudioStreaming interface).
			skips := audioInputWriteSkip.Add(1)
			if skips == 1 || skips%200 == 0 {
				audioLogger.Warn().Int64("skipped", skips).Int64("ok", audioInputWriteOK.Load()).
					Msg("audio input: buffer full, host not consuming from USB endpoint")
			}
			// After a genuine USB replug, if the endpoint stays dead, reboot
			// as a last resort. Only fires once (CompareAndSwap) with persistent cooldown.
			if skips == audioRebootSkipThreshold && audioRebootAfterReplug.CompareAndSwap(true, false) {
				if canRebootForAudioRecovery() {
					audioLogger.Warn().Int64("skipped", skips).
						Msg("audio input: endpoint dead after USB replug, rebooting device")
					go rebootForAudioRecovery()
				} else {
					audioLogger.Warn().Msg("audio input: endpoint dead after replug, but reboot cooldown active")
				}
			}
		}
		return nil
	}

	// Slow path: failure handling
	trackAudioInputFailureAndRecover()

	return err
}

// recoverAudioInputOnUSBReplug restarts the audio input ALSA device after USB replug.
// When the USB cable is replugged, the UAC1 audio gadget's ALSA device (hw:1,0) becomes
// stale and must be fully closed and reopened. Called from checkUSBState() when USB
// transitions to "configured".
//
// isGenuineReplug is true only for actual cable replugs (not first boot, not
// programmatic resets). When true, enables post-recovery endpoint monitoring:
// if the host never activates the isochronous endpoint (detected by skip count
// in WriteInputPCM), a one-time device reboot is triggered as a last resort.
func recoverAudioInputOnUSBReplug(isGenuineReplug bool) {
	defer func() {
		if r := recover(); r != nil {
			audioLogger.Error().Interface("panic", r).Msg("audio input: USB replug recovery panicked")
		}
	}()

	// Wait for USB audio gadget to be recreated and ALSA device to stabilize.
	// The audio device takes longer than HID devices to be ready after USB enumeration.
	time.Sleep(2 * time.Second)

	// Only recover if audio input is currently active
	if inputSource.Load() == nil {
		return
	}

	audioLogger.Info().Msg("audio input: USB replug detected, restarting ALSA device")

	retryDelay := 500 * time.Millisecond
	for attempt := 1; attempt <= 10; attempt++ {
		if !tryLockMutexWithTimeout(&audioMutex, 5*time.Second) {
			audioLogger.Warn().Int("attempt", attempt).Msg("audio input: USB replug recovery could not acquire mutex, retrying")
			time.Sleep(retryDelay)
			retryDelay = min(retryDelay*2, 5*time.Second)
			continue
		}

		// Re-check under lock - may have been shut down
		if inputSource.Load() == nil {
			audioMutex.Unlock()
			audioLogger.Info().Msg("audio input: aborting USB replug recovery - source was shut down")
			return
		}

		err := startInputAudioUnderMutex(getAlsaDevice("usb"))
		audioMutex.Unlock()

		if err == nil {
			audioLogger.Info().Int("attempt", attempt).Msg("audio input: successfully restarted after USB replug")
			resetAudioInputCounters()

			// Enable reboot monitoring only for genuine cable replugs.
			// If the endpoint stays dead (50+ skips), reboot as last resort.
			if isGenuineReplug {
				audioRebootAfterReplug.Store(true)
				audioLogger.Info().Msg("audio input: monitoring for dead endpoint after replug")
			}
			return
		}

		audioLogger.Warn().Err(err).Int("attempt", attempt).Msg("audio input: USB replug restart attempt failed")
		time.Sleep(retryDelay)
		retryDelay = min(retryDelay*2, 5*time.Second)
	}

	audioLogger.Warn().Msg("audio input: failed to restart after USB replug, handing off to write-failure recovery")

	// Ensure the write-failure recovery loop can be triggered.
	// Set failure count above threshold so the next WritePCM failure starts recoverAudioInput().
	audioInputFailures.Store(audioInputFailThreshold)
	audioInputRecovering.Store(false)
}

// recoverAudioInput attempts to reconnect the audio input device.
// Called asynchronously when consecutive failures exceed threshold.
// Works for both RDP and WebRTC audio input owners.
// Retries with exponential backoff until the device comes back, the session
// ends, or audio input is released. Gives up after audioRecoveryMaxAttempts.
func recoverAudioInput() {
	defer audioInputRecovering.Store(false)

	audioLogger.Warn().
		Int32("failures", audioInputFailures.Load()).
		Str("owner", GetAudioInputOwner().String()).
		Msg("audio input: starting recovery")

	retryDelay := 500 * time.Millisecond
	var attempts int32

	for {
		// Abort if no active connections (audio is being shut down)
		if activeConnections.Load() <= 0 {
			audioLogger.Info().Msg("audio input: aborting recovery - no active connections")
			audioInputFailures.Store(0)
			return
		}

		// Abort if nobody owns audio input anymore
		if GetAudioInputOwner() == AudioInputOwnerNone {
			audioLogger.Info().Msg("audio input: aborting recovery - no owner")
			audioInputFailures.Store(0)
			return
		}

		attempts++

		// After enough failed attempts, give up. A USB gadget reset alone
		// doesn't fix a dead isochronous endpoint — the host must activate
		// the active alt setting on the AudioStreaming interface.
		if attempts >= audioRecoveryMaxAttempts {
			audioLogger.Warn().Int32("attempts", attempts).Msg("audio input: recovery exhausted, giving up")
			audioInputFailures.Store(0)
			return
		}

		if !tryLockMutexWithTimeout(&audioMutex, 5*time.Second) {
			audioLogger.Warn().Int32("attempt", attempts).
				Msg("audio input: recovery could not acquire mutex (held by another goroutine), retrying")
			time.Sleep(retryDelay)
			retryDelay = min(retryDelay*2, 2*time.Second)
			continue
		}

		// Re-check after acquiring lock
		if activeConnections.Load() <= 0 {
			audioMutex.Unlock()
			audioLogger.Info().Msg("audio input: aborting recovery - connections dropped")
			audioInputFailures.Store(0)
			return
		}

		var recovered bool
		var lastErr error
		source := inputSource.Load()
		if source != nil && *source != nil {
			(*source).Disconnect()
			if err := (*source).Connect(); err == nil {
				recovered = true
			} else {
				lastErr = err
			}
		} else {
			if err := startInputAudioUnderMutex(getAlsaDevice("usb")); err == nil {
				recovered = true
			} else {
				lastErr = err
			}
		}

		audioMutex.Unlock()

		if recovered {
			resetAudioInputCounters()
			audioLogger.Info().Int32("attempts", attempts).Msg("audio input: recovery successful")
			return
		}

		audioLogger.Warn().Err(lastErr).Int32("attempt", attempts).Dur("next_retry", retryDelay).
			Msg("audio input: recovery attempt failed")

		// Device not ready yet — wait and retry with exponential backoff.
		time.Sleep(retryDelay)
		retryDelay = min(retryDelay*2, 2*time.Second)
	}
}

// canRebootForAudioRecovery checks the persistent cooldown marker to prevent reboot loops.
// Returns true if no audio-triggered reboot occurred within the cooldown period.
func canRebootForAudioRecovery() bool {
	data, err := os.ReadFile(audioRebootMarkerPath)
	if err != nil {
		return true // No marker file — allow reboot
	}
	ts, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return true // Corrupted marker — allow reboot
	}
	return time.Now().Unix()-ts >= audioRebootCooldownSecs
}

// rebootForAudioRecovery writes a persistent cooldown marker and reboots the device.
func rebootForAudioRecovery() {
	// Write marker BEFORE rebooting so the cooldown is active on next boot
	if err := os.WriteFile(audioRebootMarkerPath, []byte(strconv.FormatInt(time.Now().Unix(), 10)), 0644); err != nil {
		audioLogger.Error().Err(err).Msg("audio input: failed to write reboot cooldown marker")
	}
	audioLogger.Warn().Msg("audio input: rebooting device for audio recovery")
	if err := hwReboot(true, nil, 2*time.Second); err != nil {
		audioLogger.Error().Err(err).Msg("audio input: hwReboot failed")
	}
}

