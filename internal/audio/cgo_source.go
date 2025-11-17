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
	"unsafe"

	"github.com/jetkvm/kvm/internal/logging"
	"github.com/rs/zerolog"
)

const (
	ipcMaxFrameSize = 1500
)

type CgoSource struct {
	direction   string
	alsaDevice  string
	initialized bool
	connected   bool
	mu          sync.Mutex
	logger      zerolog.Logger
	opusBuf     []byte
	config      AudioConfig
}

func NewCgoOutputSource(alsaDevice string) *CgoSource {
	logger := logging.GetDefaultLogger().With().Str("component", "audio-output-cgo").Logger()

	return &CgoSource{
		direction:  "output",
		alsaDevice: alsaDevice,
		logger:     logger,
		opusBuf:    make([]byte, ipcMaxFrameSize),
		config:     DefaultAudioConfig(),
	}
}

func NewCgoInputSource(alsaDevice string) *CgoSource {
	logger := logging.GetDefaultLogger().With().Str("component", "audio-input-cgo").Logger()

	return &CgoSource{
		direction:  "input",
		alsaDevice: alsaDevice,
		logger:     logger,
		opusBuf:    make([]byte, ipcMaxFrameSize),
		config:     DefaultAudioConfig(),
	}
}

func (c *CgoSource) SetConfig(cfg AudioConfig) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.config = cfg
}

func (c *CgoSource) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.connected {
		return nil
	}

	if c.direction == "output" {
		os.Setenv("ALSA_CAPTURE_DEVICE", c.alsaDevice)

		dtx := C.uchar(0)
		if c.config.DTXEnabled {
			dtx = C.uchar(1)
		}
		fec := C.uchar(0)
		if c.config.FECEnabled {
			fec = C.uchar(1)
		}

		c.logger.Debug().
			Uint16("bitrate_kbps", c.config.Bitrate).
			Uint8("complexity", c.config.Complexity).
			Bool("dtx", c.config.DTXEnabled).
			Bool("fec", c.config.FECEnabled).
			Uint8("buffer_periods", c.config.BufferPeriods).
			Str("alsa_device", c.alsaDevice).
			Msg("Initializing audio capture")

		C.update_audio_constants(
			C.uint(uint32(c.config.Bitrate)*1000),
			C.uchar(c.config.Complexity),
			C.uint(48000),
			C.uchar(2),
			C.ushort(960),
			C.ushort(1500),
			C.uint(1000),
			C.uchar(5),
			C.uint(500000),
			dtx,
			fec,
			C.uchar(c.config.BufferPeriods),
		)

		rc := C.jetkvm_audio_capture_init()
		if rc != 0 {
			c.logger.Error().Int("rc", int(rc)).Msg("Failed to initialize audio capture")
			return fmt.Errorf("jetkvm_audio_capture_init failed: %d", rc)
		}
	} else {
		os.Setenv("ALSA_PLAYBACK_DEVICE", c.alsaDevice)

		C.update_audio_decoder_constants(
			C.uint(48000),
			C.uchar(2),
			C.ushort(960),
			C.ushort(1500),
			C.uint(1000),
			C.uchar(5),
			C.uint(500000),
			C.uchar(c.config.BufferPeriods),
		)

		rc := C.jetkvm_audio_playback_init()
		if rc != 0 {
			c.logger.Error().Int("rc", int(rc)).Msg("Failed to initialize audio playback")
			return fmt.Errorf("jetkvm_audio_playback_init failed: %d", rc)
		}
	}

	c.connected = true
	c.initialized = true
	return nil
}

func (c *CgoSource) Disconnect() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return
	}

	if c.direction == "output" {
		C.jetkvm_audio_capture_close()
	} else {
		C.jetkvm_audio_playback_close()
	}

	c.connected = false
}

func (c *CgoSource) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connected
}

func (c *CgoSource) ReadMessage() (uint8, []byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return 0, nil, fmt.Errorf("not connected")
	}

	if c.direction != "output" {
		return 0, nil, fmt.Errorf("ReadMessage only supported for output direction")
	}

	opusSize := C.jetkvm_audio_read_encode(unsafe.Pointer(&c.opusBuf[0]))
	if opusSize < 0 {
		return 0, nil, fmt.Errorf("jetkvm_audio_read_encode failed: %d", opusSize)
	}

	if opusSize == 0 {
		return 0, nil, nil
	}

	if int(opusSize) > len(c.opusBuf) {
		return 0, nil, fmt.Errorf("opus packet too large: %d > %d", opusSize, len(c.opusBuf))
	}

	return ipcMsgTypeOpus, c.opusBuf[:opusSize], nil
}

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
		return nil
	}

	if len(payload) == 0 {
		return nil
	}

	rc := C.jetkvm_audio_decode_write(unsafe.Pointer(&payload[0]), C.int(len(payload)))
	if rc < 0 {
		return fmt.Errorf("jetkvm_audio_decode_write failed: %d", rc)
	}

	return nil
}
