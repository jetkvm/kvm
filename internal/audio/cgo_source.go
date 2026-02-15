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
	logger := logging.GetSubsystemLogger("audio-capture").With().
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
	logger := logging.GetSubsystemLogger("audio-playback").With().
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
		C.uchar(2),
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
		c.logger.Warn().Err(err).Str("device", c.alsaDevice).Msg("failed to set ALSA_PLAYBACK_DEVICE")
	}

	C.update_audio_decoder_constants(
		C.uchar(1), // Mono for USB audio gadget
		C.ushort(1500),
		C.uint(1000),
		C.uchar(5),
		C.uint(500000),
		C.uchar(c.config.BufferPeriods),
	)

	rc := C.jetkvm_audio_playback_init()
	if rc != 0 {
		c.logger.Error().Int("rc", int(rc)).Msg("failed to initialize audio playback")
		return fmt.Errorf("jetkvm_audio_playback_init failed: %d", rc)
	}

	c.connected = true
	c.logger.Debug().Str("device", c.alsaDevice).Msg("audio input connected")
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
		c.logger.Debug().Str("device", c.alsaDevice).Msg("audio already disconnected, skipping")
		return
	}

	// Mark as disconnected first to prevent race conditions
	c.connected = false

	if c.outputDevice {
		c.logger.Debug().Str("device", c.alsaDevice).Msg("closing audio capture")
		C.jetkvm_audio_capture_close()
		os.Unsetenv("ALSA_CAPTURE_DEVICE")
	} else {
		c.logger.Debug().Str("device", c.alsaDevice).Msg("closing audio playback")
		C.jetkvm_audio_playback_close()
		os.Unsetenv("ALSA_PLAYBACK_DEVICE")
	}

	c.logger.Debug().Str("device", c.alsaDevice).Msg("audio disconnected")
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
	if opusSize == -2 {
		return 0, nil, ErrSampleRateChanged
	}
	if opusSize < 0 {
		return 0, nil, fmt.Errorf("jetkvm_audio_read_encode failed: %d", opusSize)
	}

	if opusSize == 0 {
		return 0, nil, nil
	}

	if int(opusSize) > len(c.opusBuf) {
		return 0, nil, fmt.Errorf("opus packet too large: %d > %d", opusSize, len(c.opusBuf))
	}

	// Return a slice of the internal buffer directly. The caller (relayLoop) must
	// consume the data before the next ReadMessage call, as it will be overwritten.
	return ipcMsgTypeOpus, c.opusBuf[:opusSize], nil
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

// pcmBufferPool is used to avoid allocations in GetLastPCM hot path.
// 960 frames * 2 channels * 2 bytes per sample = 3840 bytes
const maxPCMSize = 960 * 2 * 2

var pcmBufferPool = sync.Pool{
	New: func() any {
		return make([]byte, maxPCMSize)
	},
}

// GetLastPCM returns the last captured PCM audio data.
// This retrieves the raw PCM that was captured in the last ReadMessage() call.
// Format: 16-bit signed PCM, stereo interleaved, 48kHz.
// Returns nil if no data is available.
// IMPORTANT: The returned slice is from a pool - caller must not retain it
// after use. Copy the data if you need to keep it.
func GetLastPCM() []byte {
	buf := pcmBufferPool.Get().([]byte)

	size := C.jetkvm_audio_get_last_pcm(unsafe.Pointer(&buf[0]), C.int(maxPCMSize))
	if size <= 0 {
		pcmBufferPool.Put(buf)
		return nil
	}

	// Return the pooled buffer - caller must return it via ReleasePCMBuffer
	return buf[:size]
}

// ReleasePCMBuffer returns a PCM buffer to the pool.
// Must be called after GetLastPCM when done with the data.
func ReleasePCMBuffer(buf []byte) {
	if buf != nil && cap(buf) == maxPCMSize {
		pcmBufferPool.Put(buf[:maxPCMSize])
	}
}

// WritePCM writes raw PCM audio data to the playback device.
// This is used for RDP audio input (client microphone → USB gadget).
// Format: 16-bit signed PCM, mono, 48kHz.
// Returns the number of frames written (0 = skipped/buffer full), or an error.
func WritePCM(pcmData []byte) (int, error) {
	if len(pcmData) == 0 {
		return 0, nil
	}

	rc := C.jetkvm_audio_write_pcm(unsafe.Pointer(&pcmData[0]), C.int(len(pcmData)))
	if rc < 0 {
		return int(rc), fmt.Errorf("jetkvm_audio_write_pcm failed: %d", rc)
	}

	return int(rc), nil
}

// DropPlaybackBuffer drops any pending audio frames in the playback buffer.
// This clears stale audio data that may have accumulated while the host
// wasn't consuming from the USB audio gadget.
//
// Call this when audio input is first enabled to prevent accumulated
// audio from playing back when recording starts on the host.
func DropPlaybackBuffer() error {
	rc := C.jetkvm_audio_playback_drop()
	if rc < 0 {
		return fmt.Errorf("jetkvm_audio_playback_drop failed: %d", rc)
	}
	return nil
}

// SetCLogLevel sets the log level for C audio code.
// Maps zerolog levels (0=panic..6=trace) to C code log filtering.
// Call this when global log level changes to update C code filtering.
func SetCLogLevel(level int) {
	C.jetkvm_audio_set_log_level(C.int(level))
}
