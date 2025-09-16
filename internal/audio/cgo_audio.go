//go:build cgo

package audio

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

/*
#cgo CFLAGS: -I$HOME/.jetkvm/audio-libs/alsa-lib-$ALSA_VERSION/include -I$HOME/.jetkvm/audio-libs/opus-$OPUS_VERSION/include -I$HOME/.jetkvm/audio-libs/opus-$OPUS_VERSION/celt
#cgo LDFLAGS: -L$HOME/.jetkvm/audio-libs/alsa-lib-$ALSA_VERSION/src/.libs -lasound -L$HOME/.jetkvm/audio-libs/opus-$OPUS_VERSION/.libs -lopus -lm -ldl -static

#include "c/audio.c"
*/
import "C"

// Optimized Go wrappers with reduced overhead
var (
	// Base error types for wrapping with context
	errAudioInitFailed   = errors.New("failed to init ALSA/Opus")
	errAudioReadEncode   = errors.New("audio read/encode error")
	errAudioDecodeWrite  = errors.New("audio decode/write error")
	errAudioPlaybackInit = errors.New("failed to init ALSA playback/Opus decoder")
	errEmptyBuffer       = errors.New("empty buffer")
	errNilBuffer         = errors.New("nil buffer")
	errInvalidBufferPtr  = errors.New("invalid buffer pointer")
)

// Error creation functions with enhanced context
func newBufferTooSmallError(actual, required int) error {
	baseErr := fmt.Errorf("buffer too small: got %d bytes, need at least %d bytes", actual, required)
	return WrapWithMetadata(baseErr, "cgo_audio", "buffer_validation", map[string]interface{}{
		"actual_size":   actual,
		"required_size": required,
		"error_type":    "buffer_undersize",
	})
}

func newBufferTooLargeError(actual, max int) error {
	baseErr := fmt.Errorf("buffer too large: got %d bytes, maximum allowed %d bytes", actual, max)
	return WrapWithMetadata(baseErr, "cgo_audio", "buffer_validation", map[string]interface{}{
		"actual_size": actual,
		"max_size":    max,
		"error_type":  "buffer_oversize",
	})
}

func newAudioInitError(cErrorCode int) error {
	baseErr := fmt.Errorf("%w: C error code %d", errAudioInitFailed, cErrorCode)
	return WrapWithMetadata(baseErr, "cgo_audio", "initialization", map[string]interface{}{
		"c_error_code": cErrorCode,
		"error_type":   "init_failure",
		"severity":     "critical",
	})
}

func newAudioPlaybackInitError(cErrorCode int) error {
	baseErr := fmt.Errorf("%w: C error code %d", errAudioPlaybackInit, cErrorCode)
	return WrapWithMetadata(baseErr, "cgo_audio", "playback_init", map[string]interface{}{
		"c_error_code": cErrorCode,
		"error_type":   "playback_init_failure",
		"severity":     "high",
	})
}

func newAudioReadEncodeError(cErrorCode int) error {
	baseErr := fmt.Errorf("%w: C error code %d", errAudioReadEncode, cErrorCode)
	return WrapWithMetadata(baseErr, "cgo_audio", "read_encode", map[string]interface{}{
		"c_error_code": cErrorCode,
		"error_type":   "read_encode_failure",
		"severity":     "medium",
	})
}

func newAudioDecodeWriteError(cErrorCode int) error {
	baseErr := fmt.Errorf("%w: C error code %d", errAudioDecodeWrite, cErrorCode)
	return WrapWithMetadata(baseErr, "cgo_audio", "decode_write", map[string]interface{}{
		"c_error_code": cErrorCode,
		"error_type":   "decode_write_failure",
		"severity":     "medium",
	})
}

func cgoAudioInit() error {
	// Get cached config and ensure it's updated
	cache := GetCachedConfig()
	cache.Update()

	// Update C constants from cached config (atomic access, no locks)
	C.update_audio_constants(
		C.int(cache.opusBitrate.Load()),
		C.int(cache.opusComplexity.Load()),
		C.int(cache.opusVBR.Load()),
		C.int(cache.opusVBRConstraint.Load()),
		C.int(cache.opusSignalType.Load()),
		C.int(cache.opusBandwidth.Load()),
		C.int(cache.opusDTX.Load()),
		C.int(16), // LSB depth for improved bit allocation
		C.int(cache.sampleRate.Load()),
		C.int(cache.channels.Load()),
		C.int(cache.frameSize.Load()),
		C.int(cache.maxPacketSize.Load()),
		C.int(Config.CGOUsleepMicroseconds),
		C.int(Config.CGOMaxAttempts),
		C.int(Config.CGOMaxBackoffMicroseconds),
	)

	result := C.jetkvm_audio_capture_init()
	if result != 0 {
		return newAudioInitError(int(result))
	}
	return nil
}

func cgoAudioClose() {
	C.jetkvm_audio_capture_close()
}

// AudioConfigCache provides a comprehensive caching system for audio configuration
type AudioConfigCache struct {
	// Atomic int64 fields MUST be first for ARM32 alignment (8-byte alignment required)
	minFrameDuration         atomic.Int64 // Store as nanoseconds
	maxFrameDuration         atomic.Int64 // Store as nanoseconds
	maxLatency               atomic.Int64 // Store as nanoseconds
	minMetricsUpdateInterval atomic.Int64 // Store as nanoseconds
	maxMetricsUpdateInterval atomic.Int64 // Store as nanoseconds
	restartWindow            atomic.Int64 // Store as nanoseconds
	restartDelay             atomic.Int64 // Store as nanoseconds
	maxRestartDelay          atomic.Int64 // Store as nanoseconds

	// Atomic int32 fields for lock-free access to frequently used values
	minReadEncodeBuffer  atomic.Int32
	maxDecodeWriteBuffer atomic.Int32
	maxPacketSize        atomic.Int32
	maxPCMBufferSize     atomic.Int32
	opusBitrate          atomic.Int32
	opusComplexity       atomic.Int32
	opusVBR              atomic.Int32
	opusVBRConstraint    atomic.Int32
	opusSignalType       atomic.Int32
	opusBandwidth        atomic.Int32
	opusDTX              atomic.Int32
	sampleRate           atomic.Int32
	channels             atomic.Int32
	frameSize            atomic.Int32

	// Additional cached values for validation functions
	maxAudioFrameSize atomic.Int32
	maxChannels       atomic.Int32
	minOpusBitrate    atomic.Int32
	maxOpusBitrate    atomic.Int32

	// Socket and buffer configuration values
	socketMaxBuffer          atomic.Int32
	socketMinBuffer          atomic.Int32
	inputProcessingTimeoutMS atomic.Int32
	maxRestartAttempts       atomic.Int32

	// Mutex for updating the cache
	mutex       sync.RWMutex
	lastUpdate  time.Time
	cacheExpiry time.Duration
	initialized atomic.Bool

	// Pre-allocated errors to avoid allocations in hot path
	bufferTooSmallReadEncode  error
	bufferTooLargeDecodeWrite error
}

// Global audio config cache instance
var globalAudioConfigCache = &AudioConfigCache{
	cacheExpiry: 30 * time.Second, // Increased from 10s to 30s to further reduce cache updates
}

// GetCachedConfig returns the global audio config cache instance
func GetCachedConfig() *AudioConfigCache {
	return globalAudioConfigCache
}

// Update refreshes the cached config values if needed
func (c *AudioConfigCache) Update() {
	// Fast path: if cache is initialized and not expired, return immediately
	if c.initialized.Load() {
		c.mutex.RLock()
		cacheExpired := time.Since(c.lastUpdate) > c.cacheExpiry
		c.mutex.RUnlock()
		if !cacheExpired {
			return
		}
	}

	// Slow path: update cache
	c.mutex.Lock()
	defer c.mutex.Unlock()

	// Double-check after acquiring lock
	if !c.initialized.Load() || time.Since(c.lastUpdate) > c.cacheExpiry {
		// Update atomic values for lock-free access - CGO values
		c.minReadEncodeBuffer.Store(int32(Config.MinReadEncodeBuffer))
		c.maxDecodeWriteBuffer.Store(int32(Config.MaxDecodeWriteBuffer))
		c.maxPacketSize.Store(int32(Config.CGOMaxPacketSize))
		c.maxPCMBufferSize.Store(int32(Config.MaxPCMBufferSize))
		c.opusBitrate.Store(int32(Config.CGOOpusBitrate))
		c.opusComplexity.Store(int32(Config.CGOOpusComplexity))
		c.opusVBR.Store(int32(Config.CGOOpusVBR))
		c.opusVBRConstraint.Store(int32(Config.CGOOpusVBRConstraint))
		c.opusSignalType.Store(int32(Config.CGOOpusSignalType))
		c.opusBandwidth.Store(int32(Config.CGOOpusBandwidth))
		c.opusDTX.Store(int32(Config.CGOOpusDTX))
		c.sampleRate.Store(int32(Config.CGOSampleRate))
		c.channels.Store(int32(Config.CGOChannels))
		c.frameSize.Store(int32(Config.CGOFrameSize))

		// Update additional validation values
		c.maxAudioFrameSize.Store(int32(Config.MaxAudioFrameSize))
		c.maxChannels.Store(int32(Config.MaxChannels))
		c.minFrameDuration.Store(int64(Config.MinFrameDuration))
		c.maxFrameDuration.Store(int64(Config.MaxFrameDuration))
		c.minOpusBitrate.Store(int32(Config.MinOpusBitrate))
		c.maxOpusBitrate.Store(int32(Config.MaxOpusBitrate))

		// Pre-allocate common errors
		c.bufferTooSmallReadEncode = newBufferTooSmallError(0, Config.MinReadEncodeBuffer)
		c.bufferTooLargeDecodeWrite = newBufferTooLargeError(Config.MaxDecodeWriteBuffer+1, Config.MaxDecodeWriteBuffer)

		c.lastUpdate = time.Now()
		c.initialized.Store(true)

		c.lastUpdate = time.Now()
		c.initialized.Store(true)

		// Update the global validation cache as well
		if cachedMaxFrameSize != 0 {
			cachedMaxFrameSize = Config.MaxAudioFrameSize
		}
	}
}

// GetMinReadEncodeBuffer returns the cached MinReadEncodeBuffer value
func (c *AudioConfigCache) GetMinReadEncodeBuffer() int {
	return int(c.minReadEncodeBuffer.Load())
}

// GetMaxDecodeWriteBuffer returns the cached MaxDecodeWriteBuffer value
func (c *AudioConfigCache) GetMaxDecodeWriteBuffer() int {
	return int(c.maxDecodeWriteBuffer.Load())
}

// GetMaxPacketSize returns the cached MaxPacketSize value
func (c *AudioConfigCache) GetMaxPacketSize() int {
	return int(c.maxPacketSize.Load())
}

// GetMaxPCMBufferSize returns the cached MaxPCMBufferSize value
func (c *AudioConfigCache) GetMaxPCMBufferSize() int {
	return int(c.maxPCMBufferSize.Load())
}

// GetBufferTooSmallError returns the pre-allocated buffer too small error
func (c *AudioConfigCache) GetBufferTooSmallError() error {
	return c.bufferTooSmallReadEncode
}

// GetBufferTooLargeError returns the pre-allocated buffer too large error
func (c *AudioConfigCache) GetBufferTooLargeError() error {
	return c.bufferTooLargeDecodeWrite
}

// Removed duplicate config caching system - using AudioConfigCache instead

// updateCacheIfNeeded updates cache only if expired to avoid overhead
func updateCacheIfNeeded(cache *AudioConfigCache) {
	if cache.initialized.Load() {
		cache.mutex.RLock()
		cacheExpired := time.Since(cache.lastUpdate) > cache.cacheExpiry
		cache.mutex.RUnlock()
		if cacheExpired {
			cache.Update()
		}
	} else {
		cache.Update()
	}
}

func cgoAudioReadEncode(buf []byte) (int, error) {
	// Minimal buffer validation - assume caller provides correct size
	if len(buf) == 0 {
		return 0, errEmptyBuffer
	}

	// Direct CGO call - hotpath optimization
	n := C.jetkvm_audio_read_encode(unsafe.Pointer(&buf[0]))

	// Fast path for success
	if n > 0 {
		return int(n), nil
	}

	// Error handling with static errors
	if n < 0 {
		if n == -1 {
			return 0, errAudioInitFailed
		}
		return 0, errAudioReadEncode
	}

	return 0, nil
}

// Audio playback functions
func cgoAudioPlaybackInit() error {
	// Get cached config and ensure it's updated
	cache := GetCachedConfig()
	cache.Update()

	// No need to update C constants here as they're already set in cgoAudioInit

	ret := C.jetkvm_audio_playback_init()
	if ret != 0 {
		return newAudioPlaybackInitError(int(ret))
	}
	return nil
}

func cgoAudioPlaybackClose() {
	C.jetkvm_audio_playback_close()
}

func cgoAudioDecodeWrite(buf []byte) (int, error) {
	// Minimal validation - assume caller provides correct size
	if len(buf) == 0 {
		return 0, errEmptyBuffer
	}

	// Direct CGO call - hotpath optimization
	n := int(C.jetkvm_audio_decode_write(unsafe.Pointer(&buf[0]), C.int(len(buf))))

	// Fast path for success
	if n >= 0 {
		return n, nil
	}

	// Error handling with static errors
	if n == -1 {
		return 0, errAudioInitFailed
	}
	return 0, errAudioDecodeWrite
}

// updateOpusEncoderParams dynamically updates OPUS encoder parameters
func updateOpusEncoderParams(bitrate, complexity, vbr, vbrConstraint, signalType, bandwidth, dtx int) error {
	result := C.update_opus_encoder_params(
		C.int(bitrate),
		C.int(complexity),
		C.int(vbr),
		C.int(vbrConstraint),
		C.int(signalType),
		C.int(bandwidth),
		C.int(dtx),
	)
	if result != 0 {
		return fmt.Errorf("failed to update OPUS encoder parameters: C error code %d", result)
	}
	return nil
}

// Buffer pool for reusing buffers in CGO functions
var (
	// Simple buffer pool for PCM data
	pcmBufferPool = NewAudioBufferPool(Config.MaxPCMBufferSize)

	// Track buffer pool usage
	cgoBufferPoolGets atomic.Int64
	cgoBufferPoolPuts atomic.Int64
	// Batch processing statistics - only enabled in debug builds
	batchProcessingCount atomic.Int64
	batchFrameCount      atomic.Int64
	batchProcessingTime  atomic.Int64
	// Batch time tracking removed
)

// GetBufferFromPool gets a buffer from the pool with at least the specified capacity
func GetBufferFromPool(minCapacity int) []byte {
	cgoBufferPoolGets.Add(1)
	// Use simple fixed-size buffer for PCM data
	return pcmBufferPool.Get()
}

// ReturnBufferToPool returns a buffer to the pool
func ReturnBufferToPool(buf []byte) {
	cgoBufferPoolPuts.Add(1)
	pcmBufferPool.Put(buf)
}

// ReadEncodeWithPooledBuffer reads audio data and encodes it using a buffer from the pool
func ReadEncodeWithPooledBuffer() ([]byte, int, error) {
	cache := GetCachedConfig()
	updateCacheIfNeeded(cache)

	bufferSize := cache.GetMinReadEncodeBuffer()
	if bufferSize == 0 {
		bufferSize = 1500
	}

	buf := GetBufferFromPool(bufferSize)
	n, err := cgoAudioReadEncode(buf)
	if err != nil {
		ReturnBufferToPool(buf)
		return nil, 0, err
	}

	return buf[:n], n, nil
}

// DecodeWriteWithPooledBuffer decodes and writes audio data using a pooled buffer
func DecodeWriteWithPooledBuffer(data []byte) (int, error) {
	if len(data) == 0 {
		return 0, errEmptyBuffer
	}

	cache := GetCachedConfig()
	updateCacheIfNeeded(cache)

	maxPacketSize := cache.GetMaxPacketSize()
	if len(data) > maxPacketSize {
		return 0, newBufferTooLargeError(len(data), maxPacketSize)
	}

	pcmBuffer := GetBufferFromPool(cache.GetMaxPCMBufferSize())
	defer ReturnBufferToPool(pcmBuffer)

	return CGOAudioDecodeWrite(data, pcmBuffer)
}

// GetBatchProcessingStats returns statistics about batch processing
func GetBatchProcessingStats() (count, frames, avgTimeUs int64) {
	count = batchProcessingCount.Load()
	frames = batchFrameCount.Load()
	totalTime := batchProcessingTime.Load()

	// Calculate average time per batch
	if count > 0 {
		avgTimeUs = totalTime / count
	}

	return count, frames, avgTimeUs
}

// cgoAudioDecodeWriteWithBuffers decodes opus data and writes to PCM buffer
// This implementation uses separate buffers for opus data and PCM output
func cgoAudioDecodeWriteWithBuffers(opusData []byte, pcmBuffer []byte) (int, error) {
	// Validate input
	if len(opusData) == 0 {
		return 0, errEmptyBuffer
	}
	if len(pcmBuffer) == 0 {
		return 0, errEmptyBuffer
	}

	// Get cached config
	cache := GetCachedConfig()
	// Only update cache if expired - avoid unnecessary overhead
	// Use proper locking to avoid race condition
	if cache.initialized.Load() {
		cache.mutex.RLock()
		cacheExpired := time.Since(cache.lastUpdate) > cache.cacheExpiry
		cache.mutex.RUnlock()
		if cacheExpired {
			cache.Update()
		}
	} else {
		cache.Update()
	}

	// Ensure data doesn't exceed max packet size
	maxPacketSize := cache.GetMaxPacketSize()
	if len(opusData) > maxPacketSize {
		return 0, newBufferTooLargeError(len(opusData), maxPacketSize)
	}

	// Direct CGO call with minimal overhead - unsafe.Pointer(&slice[0]) is never nil for non-empty slices
	n := int(C.jetkvm_audio_decode_write(unsafe.Pointer(&opusData[0]), C.int(len(opusData))))

	// Fast path for success case
	if n >= 0 {
		return n, nil
	}

	// Handle error cases with static error codes to reduce allocations
	switch n {
	case -1:
		return 0, errAudioInitFailed
	case -2:
		return 0, errAudioDecodeWrite
	default:
		return 0, newAudioDecodeWriteError(n)
	}
}

// Optimized CGO function aliases - use direct function calls to reduce overhead
// These are now direct function aliases instead of variable assignments
func CGOAudioInit() error                               { return cgoAudioInit() }
func CGOAudioClose()                                    { cgoAudioClose() }
func CGOAudioReadEncode(buf []byte) (int, error)        { return cgoAudioReadEncode(buf) }
func CGOAudioPlaybackInit() error                       { return cgoAudioPlaybackInit() }
func CGOAudioPlaybackClose()                            { cgoAudioPlaybackClose() }
func CGOAudioDecodeWriteLegacy(buf []byte) (int, error) { return cgoAudioDecodeWrite(buf) }
func CGOAudioDecodeWrite(opusData []byte, pcmBuffer []byte) (int, error) {
	return cgoAudioDecodeWriteWithBuffers(opusData, pcmBuffer)
}
func CGOUpdateOpusEncoderParams(bitrate, complexity, vbr, vbrConstraint, signalType, bandwidth, dtx int) error {
	return updateOpusEncoderParams(bitrate, complexity, vbr, vbrConstraint, signalType, bandwidth, dtx)
}
