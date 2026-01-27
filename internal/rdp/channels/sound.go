package channels

import (
	"encoding/binary"
	"errors"
	"sync"
	"sync/atomic"
)

// RDPSND implements MS-RDPEA (Audio Output Virtual Channel Extension).
// Static virtual channel for maximum compatibility.

// RDPSND PDU types (MS-RDPEA 2.2.2).
const (
	SNDCClose       = 0x01
	SNDCWave        = 0x02 // Audio data (legacy)
	SNDCSetVolume   = 0x03
	SNDCSetPitch    = 0x04
	SNDCWaveConfirm = 0x05 // Client acknowledgment
	SNDCTraining    = 0x06
	SNDCFormats     = 0x07 // Format negotiation
	SNDCCryptKey    = 0x08
	SNDCWaveEncrypt = 0x09
	SNDCQualityMode = 0x0C
	SNDCWave2       = 0x0D // Audio data (modern, Windows 8+)
)

// RDPSND version.
const (
	SNDCVersionMajor = 0x06
	SNDCVersionMinor = 0x00
)

// Audio format tags.
const (
	WaveFormatPCM   = 0x0001
	WaveFormatALaw  = 0x0006
	WaveFormatMuLaw = 0x0007
	WaveFormatAAC   = 0xA106 // AAC-LC
)

// RDPSND PDU flags.
const (
	SNDCFlagAlive  = 0x0001
	SNDCFlagVolume = 0x0002
	SNDCFlagPitch  = 0x0004
)

// RDPSND sizes.
const (
	SNDCHeaderSize        = 4  // msgType(1) + reserved(1) + bodySize(2)
	SNDCFormatsHeaderSize = 20 // dwFlags(4) + dwVolume(4) + dwPitch(4) + wDGramPort(2) + wNumberOfFormats(2) + cLastBlockConfirmed(1) + wVersion(2) + bPad(1)
	SNDCAudioFormatSize   = 18 // wFormatTag(2) + nChannels(2) + nSamplesPerSec(4) + nAvgBytesPerSec(4) + nBlockAlign(2) + wBitsPerSample(2) + cbSize(2)
	SNDCWave2HeaderSize   = 12 // wTimeStamp(2) + wFormatNo(2) + cBlockNo(1) + bPad[3](3) + dwAudioTimeStamp(4)
	SNDCWaveConfirmSize   = 4  // wTimeStamp(2) + cConfirmedBlockNo(1) + bPad(1)
)

// Maximum values.
// Note: Each block is ~21ms at 48kHz stereo 16-bit (4096 bytes / 4 bytes per sample / 48000 * 1000).
const (
	SNDCDefaultMaxBlocksPending = 32   // Default maximum audio blocks in flight (~680ms)
	SNDCMinBlocksPending        = 16   // Minimum blocks pending (~340ms)
	SNDCMaxBlocksPending        = 96   // Maximum blocks pending (~2s) - defensive cap
	SNDCBlockSize               = 4096 // Optimal audio block size (matches typical audio buffer)
)

// Preferred format constants are defined in audio_format.go (shared with AUDIN).
// Aliased here for backward compatibility.
const (
	PreferredChannels      = AudioPreferredChannels
	PreferredSampleRate    = AudioPreferredSampleRate
	PreferredBitsPerSample = AudioPreferredBitsPerSample
	PreferredBlockAlign    = AudioPreferredBlockAlign
	PreferredBytesPerSec   = AudioPreferredBytesPerSec
)

// Common errors.
var (
	ErrSoundNotReady     = errors.New("rdpsnd: channel not ready")
	ErrSoundBackpressure = errors.New("rdpsnd: too many blocks pending")
	ErrSoundNoFormat     = errors.New("rdpsnd: no compatible format")
)

// SoundSendFunc is the callback for sending data on the static channel.
type SoundSendFunc func(data []byte) error

// SoundReadyCallback is called when the sound channel is ready to send audio.
type SoundReadyCallback func(s *SoundChannel)

// AudioFormat represents a negotiated audio format.
type AudioFormat struct {
	FormatTag      uint16
	Channels       uint16
	SamplesPerSec  uint32
	AvgBytesPerSec uint32
	BlockAlign     uint16
	BitsPerSample  uint16
}

// SoundChannel implements the RDPSND static channel with optimized audio streaming.
type SoundChannel struct {
	sendFunc SoundSendFunc

	// Callback when channel becomes ready
	onReady SoundReadyCallback

	// Negotiated formats
	formats       []AudioFormat
	selectedIndex int
	selectedFmt   AudioFormat
	formatMu      sync.RWMutex

	// Block tracking for flow control (all atomic for lock-free hot path)
	blockNo          atomic.Uint32 // Wraps at 256, cast to uint8 when used
	blocksPending    atomic.Int32
	lastConfirmed    atomic.Uint32
	timestamp        atomic.Uint32
	maxBlocksPending int32 // Configurable max blocks in flight (scaled by audio buffer config)

	// Pre-allocated buffer for zero-allocation hot path
	// Layout: [Header 4][Wave2Header 12][Audio data up to SNDCBlockSize]
	audioBuf [SNDCHeaderSize + SNDCWave2HeaderSize + SNDCBlockSize]byte

	ready atomic.Bool
}

// NewSoundChannel creates a new RDPSND static channel.
// maxBlocksPending controls flow control - higher values handle more latency but use more memory.
// Pass 0 to use the default (SNDCDefaultMaxBlocksPending).
func NewSoundChannel(sendFunc SoundSendFunc, maxBlocksPending int) *SoundChannel {
	if maxBlocksPending <= 0 {
		maxBlocksPending = SNDCDefaultMaxBlocksPending
	}
	if maxBlocksPending < SNDCMinBlocksPending {
		maxBlocksPending = SNDCMinBlocksPending
	}
	return &SoundChannel{
		sendFunc:         sendFunc,
		selectedIndex:    -1,
		maxBlocksPending: int32(maxBlocksPending),
	}
}

// MaxBlocksPendingFromBufferPeriods calculates the optimal max blocks pending
// based on the audio buffer periods setting. Higher buffer periods indicate
// the user expects higher latency (e.g., Tailscale), so we scale accordingly.
// Formula: clamp(bufferPeriods * 2, 16, 96) - gives ~340ms to ~2s of buffer.
func MaxBlocksPendingFromBufferPeriods(bufferPeriods int) int {
	maxBlocks := bufferPeriods * 2
	if maxBlocks < SNDCMinBlocksPending {
		return SNDCMinBlocksPending
	}
	if maxBlocks > SNDCMaxBlocksPending {
		return SNDCMaxBlocksPending
	}
	return maxBlocks
}

// SetReadyCallback sets the callback to be called when the channel is ready.
func (s *SoundChannel) SetReadyCallback(cb SoundReadyCallback) {
	s.onReady = cb
}

// Start initiates the audio channel by sending server formats.
func (s *SoundChannel) Start() error {
	return s.sendServerFormats()
}

// HandlePDU processes an incoming RDPSND PDU from the client.
func (s *SoundChannel) HandlePDU(data []byte) error {
	if len(data) < SNDCHeaderSize {
		return nil
	}

	msgType := data[0]

	switch msgType {
	case SNDCFormats:
		return s.handleClientFormats(data[SNDCHeaderSize:])
	case SNDCWaveConfirm:
		return s.handleWaveConfirm(data[SNDCHeaderSize:])
	case SNDCTraining:
		return s.handleTraining(data[SNDCHeaderSize:])
	case SNDCQualityMode:
		// Ignore quality mode requests
		return nil
	}

	return nil
}

// sendServerFormats sends the server's supported audio formats.
func (s *SoundChannel) sendServerFormats() error {
	// Build server formats PDU
	// Header(4) + FormatHeader(20) + Format(18) = 42 bytes
	bodySize := SNDCFormatsHeaderSize + SNDCAudioFormatSize
	buf := make([]byte, SNDCHeaderSize+bodySize)

	// Header
	buf[0] = SNDCFormats
	buf[1] = 0 // reserved
	binary.LittleEndian.PutUint16(buf[2:4], uint16(bodySize))

	pos := SNDCHeaderSize

	// Format header
	binary.LittleEndian.PutUint32(buf[pos:pos+4], SNDCFlagAlive)        // dwFlags
	binary.LittleEndian.PutUint32(buf[pos+4:pos+8], 0xFFFF)             // dwVolume (max)
	binary.LittleEndian.PutUint32(buf[pos+8:pos+12], 0)                 // dwPitch
	binary.LittleEndian.PutUint16(buf[pos+12:pos+14], 0)                // wDGramPort
	binary.LittleEndian.PutUint16(buf[pos+14:pos+16], 1)                // wNumberOfFormats
	buf[pos+16] = 0                                                     // cLastBlockConfirmed
	binary.LittleEndian.PutUint16(buf[pos+17:pos+19], SNDCVersionMajor) // wVersion
	buf[pos+19] = 0                                                     // bPad
	pos += SNDCFormatsHeaderSize

	// Audio format: 16-bit PCM, stereo, 48kHz (uses shared helper)
	EncodePreferredWAVEFORMATEX(buf, pos)

	return s.sendFunc(buf)
}

// handleClientFormats processes client format negotiation response.
func (s *SoundChannel) handleClientFormats(data []byte) error {
	if len(data) < SNDCFormatsHeaderSize {
		return nil
	}

	numFormats := binary.LittleEndian.Uint16(data[14:16])

	pos := SNDCFormatsHeaderSize
	s.formatMu.Lock()
	defer s.formatMu.Unlock()

	s.formats = make([]AudioFormat, 0, numFormats)

	// Parse client formats (uses shared WAVEFORMATEX parser)
	for i := uint16(0); i < numFormats && pos+WAVEFORMATEXSize <= len(data); i++ {
		fmt, cbSize, ok := ParseWAVEFORMATEX(data, pos)
		if !ok {
			break
		}
		s.formats = append(s.formats, fmt)
		pos += WAVEFORMATEXSize + int(cbSize)
	}

	// Find best match (uses shared format selection logic)
	var selectedIndex int
	selectedIndex, s.selectedFmt = FindPreferredFormat(s.formats)

	if selectedIndex < 0 {
		return ErrSoundNoFormat
	}

	s.selectedIndex = selectedIndex
	s.ready.Store(true)

	// Notify that channel is ready - run in goroutine to avoid blocking message loop
	// The audio initialization can take time (CGO calls), but we need to let the
	// message loop continue to process DVC create responses
	if s.onReady != nil {
		go s.onReady(s)
	}

	return nil
}

// handleWaveConfirm processes audio block acknowledgment.
func (s *SoundChannel) handleWaveConfirm(data []byte) error {
	if len(data) < SNDCWaveConfirmSize {
		return nil
	}

	confirmedBlock := data[2]

	s.lastConfirmed.Store(uint32(confirmedBlock))

	// Reduce pending count
	pending := s.blocksPending.Load()
	if pending > 0 {
		s.blocksPending.Add(-1)
	}

	return nil
}

// handleTraining processes training PDU (latency measurement).
func (s *SoundChannel) handleTraining(data []byte) error {
	if len(data) < 4 {
		return nil
	}

	// Echo back the training timestamp
	buf := make([]byte, SNDCHeaderSize+4)
	buf[0] = SNDCTraining
	buf[1] = 0
	binary.LittleEndian.PutUint16(buf[2:4], 4)
	copy(buf[SNDCHeaderSize:], data[:4])

	return s.sendFunc(buf)
}

// SendAudio sends audio data to the client.
// Data should be in the negotiated format (typically 16-bit PCM, stereo, 48kHz).
// Returns ErrSoundBackpressure if too many blocks are pending acknowledgment.
//
// HOT PATH: Zero allocations, lock-free atomic operations.
func (s *SoundChannel) SendAudio(data []byte) error {
	if !s.ready.Load() {
		return ErrSoundNotReady
	}

	// Flow control - check pending blocks (lock-free)
	if s.blocksPending.Load() >= s.maxBlocksPending {
		return ErrSoundBackpressure
	}

	// LOCK-FREE: Get block number and increment atomically (wraps at 256)
	blockNo := uint8(s.blockNo.Add(1) - 1) // Add returns new value, we want old value

	// Get timestamp (lock-free)
	timestamp := uint16(s.timestamp.Add(1))

	// Check data size fits in pre-allocated buffer
	if len(data) > SNDCBlockSize {
		data = data[:SNDCBlockSize] // Truncate to max block size
	}

	// ZERO-ALLOCATION: Use pre-allocated buffer
	bodySize := SNDCWave2HeaderSize + len(data)
	totalSize := SNDCHeaderSize + bodySize
	buf := s.audioBuf[:totalSize]

	// Header
	buf[0] = SNDCWave2
	buf[1] = 0
	binary.LittleEndian.PutUint16(buf[2:4], uint16(bodySize))

	pos := SNDCHeaderSize

	// WAVE2 header
	binary.LittleEndian.PutUint16(buf[pos:pos+2], timestamp)                 // wTimeStamp
	binary.LittleEndian.PutUint16(buf[pos+2:pos+4], uint16(s.selectedIndex)) // wFormatNo
	buf[pos+4] = blockNo                                                     // cBlockNo
	buf[pos+5] = 0                                                           // bPad[0]
	buf[pos+6] = 0                                                           // bPad[1]
	buf[pos+7] = 0                                                           // bPad[2]
	binary.LittleEndian.PutUint32(buf[pos+8:pos+12], uint32(timestamp)*1000) // dwAudioTimeStamp (microseconds)
	pos += SNDCWave2HeaderSize

	// Audio data
	copy(buf[pos:], data)

	// Increment pending before sending (lock-free)
	s.blocksPending.Add(1)

	return s.sendFunc(buf)
}

// SendAudioChunked sends large audio data in optimal chunks.
// This is useful when you have a large audio buffer to send.
func (s *SoundChannel) SendAudioChunked(data []byte) error {
	for len(data) > 0 {
		chunkSize := min(len(data), SNDCBlockSize)

		if err := s.SendAudio(data[:chunkSize]); err != nil {
			return err
		}

		data = data[chunkSize:]
	}

	return nil
}

// IsReady returns true if the channel is ready to send audio.
func (s *SoundChannel) IsReady() bool {
	return s.ready.Load()
}

// GetSelectedFormat returns the negotiated audio format.
func (s *SoundChannel) GetSelectedFormat() (AudioFormat, bool) {
	s.formatMu.RLock()
	defer s.formatMu.RUnlock()

	if s.selectedIndex < 0 {
		return AudioFormat{}, false
	}
	return s.selectedFmt, true
}

// GetPendingBlocks returns the number of audio blocks awaiting acknowledgment.
func (s *SoundChannel) GetPendingBlocks() int {
	return int(s.blocksPending.Load())
}

// Close sends the close PDU.
func (s *SoundChannel) Close() error {
	s.ready.Store(false)

	// Send close PDU
	buf := make([]byte, SNDCHeaderSize)
	buf[0] = SNDCClose
	buf[1] = 0
	binary.LittleEndian.PutUint16(buf[2:4], 0)

	return s.sendFunc(buf)
}
