//go:build linux && (arm || arm64)

package audio

/*
#cgo CFLAGS: -O3 -ffast-math -I/opt/jetkvm-audio-libs/alsa-lib-1.2.14/include -I/opt/jetkvm-audio-libs/opus-1.5.2/include
#cgo LDFLAGS: /opt/jetkvm-audio-libs/alsa-lib-1.2.14/src/.libs/libasound.a /opt/jetkvm-audio-libs/opus-1.5.2/.libs/libopus.a -lm -ldl -lpthread

#include <stdlib.h>
#include "c/audio.c"
*/
import "C"
import (
	"fmt"
	"os"
	"sync"

	"github.com/jetkvm/kvm/internal/logging"
	"github.com/rs/zerolog"
)

// CgoSource implements AudioSource via direct CGO calls to C audio functions (in-process)
type CgoSource struct {
	direction   string // "output" or "input"
	alsaDevice  string
	initialized bool
	connected   bool
	mu          sync.Mutex
	logger      zerolog.Logger
	opusBuf     []byte // Reusable buffer for Opus packets
}

// NewCgoOutputSource creates a new CGO audio source for output (HDMI/USB → browser)
func NewCgoOutputSource(alsaDevice string) *CgoSource {
	logger := logging.GetDefaultLogger().With().Str("component", "audio-output-cgo").Logger()

	return &CgoSource{
		direction: "output",
		alsaDevice: alsaDevice,
		logger:     logger,
		opusBuf:    make([]byte, ipcMaxFrameSize),
	}
}

// NewCgoInputSource creates a new CGO audio source for input (browser → USB speakers)
func NewCgoInputSource(alsaDevice string) *CgoSource {
	logger := logging.GetDefaultLogger().With().Str("component", "audio-input-cgo").Logger()

	return &CgoSource{
		direction: "input",
		alsaDevice: alsaDevice,
		logger:     logger,
		opusBuf:    make([]byte, ipcMaxFrameSize),
	}
}

// Connect initializes the C audio subsystem
func (c *CgoSource) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.connected {
		return nil
	}

	// Set ALSA device via environment for C code to read via init_alsa_devices_from_env()
	if c.direction == "output" {
		// Set capture device for output path via environment variable
		os.Setenv("ALSA_CAPTURE_DEVICE", c.alsaDevice)

		// Initialize constants
		C.update_audio_constants(
			C.uint(128000),  // bitrate
			C.uchar(5),      // complexity
			C.uint(48000),   // sample_rate
			C.uchar(2),      // channels
			C.ushort(960),   // frame_size
			C.ushort(1500),  // max_packet_size
			C.uint(1000),    // sleep_us
			C.uchar(5),      // max_attempts
			C.uint(500000),  // max_backoff_us
		)

		// Initialize capture (HDMI/USB → browser)
		rc := C.jetkvm_audio_capture_init()
		if rc != 0 {
			c.logger.Error().Int("rc", int(rc)).Msg("Failed to initialize audio capture")
			return fmt.Errorf("jetkvm_audio_capture_init failed: %d", rc)
		}

		c.logger.Debug().Str("device", c.alsaDevice).Msg("Audio capture initialized")
	} else {
		// Set playback device for input path via environment variable
		os.Setenv("ALSA_PLAYBACK_DEVICE", c.alsaDevice)

		// Initialize decoder constants
		C.update_audio_decoder_constants(
			C.uint(48000),   // sample_rate
			C.uchar(2),      // channels
			C.ushort(960),   // frame_size
			C.ushort(1500),  // max_packet_size
			C.uint(1000),    // sleep_us
			C.uchar(5),      // max_attempts
			C.uint(500000),  // max_backoff_us
		)

		// Initialize playback (browser → USB speakers)
		rc := C.jetkvm_audio_playback_init()
		if rc != 0 {
			c.logger.Error().Int("rc", int(rc)).Msg("Failed to initialize audio playback")
			return fmt.Errorf("jetkvm_audio_playback_init failed: %d", rc)
		}

		c.logger.Debug().Str("device", c.alsaDevice).Msg("Audio playback initialized")
	}

	c.connected = true
	c.initialized = true
	return nil
}

// Disconnect closes the C audio subsystem
func (c *CgoSource) Disconnect() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return
	}

	if c.direction == "output" {
		C.jetkvm_audio_capture_close()
		c.logger.Debug().Msg("Audio capture closed")
	} else {
		C.jetkvm_audio_playback_close()
		c.logger.Debug().Msg("Audio playback closed")
	}

	c.connected = false
}

// IsConnected returns true if currently connected
func (c *CgoSource) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connected
}

// ReadMessage reads the next audio frame from C audio subsystem
// For output path: reads HDMI/USB audio and encodes to Opus
// For input path: not used (input uses WriteMessage instead)
// Returns message type (0 = Opus), payload data, and error
func (c *CgoSource) ReadMessage() (uint8, []byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return 0, nil, fmt.Errorf("not connected")
	}

	if c.direction != "output" {
		return 0, nil, fmt.Errorf("ReadMessage only supported for output direction")
	}

	// Call C function to read HDMI/USB audio and encode to Opus
	// Returns Opus packet size (>0) or error (<0)
	opusSize := C.jetkvm_audio_read_encode(unsafe.Pointer(&c.opusBuf[0]))

	if opusSize < 0 {
		return 0, nil, fmt.Errorf("jetkvm_audio_read_encode failed: %d", opusSize)
	}

	if opusSize == 0 {
		// No data available (silence/DTX)
		return 0, nil, nil
	}

	// Return slice of opusBuf - caller must use immediately
	return ipcMsgTypeOpus, c.opusBuf[:opusSize], nil
}

// WriteMessage writes an Opus packet to the C audio subsystem for playback
// Only used for input path (browser → USB speakers)
func (c *CgoSource) WriteMessage(msgType uint8, payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return fmt.Errorf("not connected")
	}

	if c.direction != "input" {
		return fmt.Errorf("WriteMessage only supported for input direction")
	}

	if msgType != ipcMsgTypeOpus {
		// Ignore non-Opus messages
		return nil
	}

	if len(payload) == 0 {
		return nil
	}

	// Call C function to decode Opus and write to USB speakers
	rc := C.jetkvm_audio_decode_write(unsafe.Pointer(&payload[0]), C.int(len(payload)))

	if rc < 0 {
		return fmt.Errorf("jetkvm_audio_decode_write failed: %d", rc)
	}

	return nil
}
