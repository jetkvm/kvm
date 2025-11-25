//go:build linux && (arm || arm64)

package audio

/*
#cgo CFLAGS: -O3 -ffast-math -I/opt/jetkvm-audio-libs/alsa-lib-1.2.14/include -I/opt/jetkvm-audio-libs/opus-1.5.2/include -I/opt/jetkvm-audio-libs/speexdsp-1.2.1/include
#cgo LDFLAGS: /opt/jetkvm-audio-libs/alsa-lib-1.2.14/src/.libs/libasound.a /opt/jetkvm-audio-libs/opus-1.5.2/.libs/libopus.a /opt/jetkvm-audio-libs/speexdsp-1.2.1/libspeexdsp/.libs/libspeexdsp.a -lm -ldl -lpthread

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
	outputDevice bool
	alsaDevice   string
	connected    bool
	mu           sync.Mutex
	logger       zerolog.Logger
	opusBuf      []byte
	config       AudioConfig
}

var _ AudioSource = (*CgoSource)(nil)

func NewCgoOutputSource(alsaDevice string, cfg AudioConfig) AudioSource {
	logger := logging.GetDefaultLogger().With().
		Str("component", "audio-output-cgo").
		Str("alsa_device", alsaDevice).
		Logger()

	return &CgoSource{
		outputDevice: true,
		alsaDevice:   alsaDevice,
		logger:       logger,
		opusBuf:      make([]byte, ipcMaxFrameSize),
		config:       cfg,
	}
}

func NewCgoInputSource(alsaDevice string, cfg AudioConfig) AudioSource {
	logger := logging.GetDefaultLogger().With().
		Str("component", "audio-input-cgo").
		Str("alsa_device", alsaDevice).
		Logger()

	return &CgoSource{
		outputDevice: false,
		alsaDevice:   alsaDevice,
		logger:       logger,
		opusBuf:      make([]byte, ipcMaxFrameSize),
		config:       cfg,
	}
}

func (c *CgoSource) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.connected {
		return nil
	}

	if c.outputDevice {
		return c.connectOutput()
	}
	return c.connectInput()
}

func (c *CgoSource) connectOutput() error {
	if err := os.Setenv("ALSA_CAPTURE_DEVICE", c.alsaDevice); err != nil {
		c.logger.Warn().Err(err).Str("device", c.alsaDevice).Msg("Failed to set ALSA_CAPTURE_DEVICE")
	}

	const sampleRate = 48000
	const frameSize = uint16(sampleRate * 20 / 1000) // 20ms frames

	c.logger.Debug().
		Uint16("bitrate_kbps", c.config.Bitrate).
		Uint8("complexity", c.config.Complexity).
		Bool("dtx", c.config.DTXEnabled).
		Bool("fec", c.config.FECEnabled).
		Uint8("buffer_periods", c.config.BufferPeriods).
		Uint32("sample_rate", sampleRate).
		Uint16("frame_size", uint16(frameSize)).
		Uint8("packet_loss_perc", c.config.PacketLossPerc).
		Msg("Initializing audio capture")

	C.update_audio_constants(
		C.uint(uint32(c.config.Bitrate)*1000),
		C.uchar(c.config.Complexity),
		C.uint(sampleRate),
		C.uchar(2),
		C.ushort(frameSize),
		C.ushort(1500),
		C.uint(1000),
		C.uchar(5),
		C.uint(500000),
		boolToUchar(c.config.DTXEnabled),
		boolToUchar(c.config.FECEnabled),
		C.uchar(c.config.BufferPeriods),
		C.uchar(c.config.PacketLossPerc),
	)

	rc := C.jetkvm_audio_capture_init()
	if rc != 0 {
		c.logger.Error().Int("rc", int(rc)).Msg("Failed to initialize audio capture")
		return fmt.Errorf("jetkvm_audio_capture_init failed: %d", rc)
	}

	c.connected = true
	return nil
}

func (c *CgoSource) connectInput() error {
	if err := os.Setenv("ALSA_PLAYBACK_DEVICE", c.alsaDevice); err != nil {
		c.logger.Warn().Err(err).Str("device", c.alsaDevice).Msg("Failed to set ALSA_PLAYBACK_DEVICE")
	}

	// USB Audio Gadget uses fixed 48kHz sample rate
	const inputSampleRate = 48000
	const frameSize = uint16(inputSampleRate * 20 / 1000) // 20ms frames

	C.update_audio_decoder_constants(
		C.uint(inputSampleRate),
		C.uchar(1), // Mono for USB audio gadget
		C.ushort(uint16(frameSize)),
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

	c.connected = true
	return nil
}

func boolToUchar(b bool) C.uchar {
	if b {
		return C.uchar(1)
	}
	return C.uchar(0)
}

func (c *CgoSource) Disconnect() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return
	}

	if c.outputDevice {
		C.jetkvm_audio_capture_close()
		os.Unsetenv("ALSA_CAPTURE_DEVICE")
	} else {
		C.jetkvm_audio_playback_close()
		os.Unsetenv("ALSA_PLAYBACK_DEVICE")
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

	if !c.outputDevice {
		return 0, nil, fmt.Errorf("ReadMessage only supported for output direction")
	}

	// Hold mutex during C call to prevent race condition with Disconnect().
	// Lock order is consistent (c.mu -> capture_mutex) in all code paths,
	// so this cannot deadlock. The C layer's capture_mutex protects ALSA/codec
	// state, while c.mu protects the connection lifecycle.
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

	// Return a copy to prevent buffer aliasing - the caller may hold this slice
	// while the next ReadMessage overwrites the internal buffer
	result := make([]byte, opusSize)
	copy(result, c.opusBuf[:opusSize])
	return ipcMsgTypeOpus, result, nil
}

func (c *CgoSource) WriteMessage(msgType uint8, payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return fmt.Errorf("not connected")
	}

	if c.outputDevice {
		return fmt.Errorf("WriteMessage only supported for input direction")
	}

	if msgType != ipcMsgTypeOpus {
		return nil
	}

	if len(payload) == 0 {
		return nil
	}

	if len(payload) > 1500 {
		return fmt.Errorf("opus packet too large: %d bytes (max 1500)", len(payload))
	}

	// Hold mutex during C call to prevent race condition with Disconnect().
	// Lock order is consistent (c.mu -> playback_mutex) in all code paths.
	rc := C.jetkvm_audio_decode_write(unsafe.Pointer(&payload[0]), C.int(len(payload)))
	if rc < 0 {
		return fmt.Errorf("jetkvm_audio_decode_write failed: %d", rc)
	}

	return nil
}
