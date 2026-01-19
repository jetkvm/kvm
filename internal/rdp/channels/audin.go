package channels

import (
	"encoding/binary"
	"errors"
	"sync"
	"sync/atomic"
)

// AUDIN implements the RDP Audio Input Redirection Virtual Channel Extension (MS-RDPEAI).
// This channel receives microphone audio from the RDP client and forwards it to the UAC gadget.

// AUDIN channel name.
const AudinChannelName = "AUDIO_INPUT"

// AUDIN message types.
const (
	AudinMsgVersion      = 0x01
	AudinMsgFormats      = 0x02
	AudinMsgOpen         = 0x03
	AudinMsgOpenReply    = 0x04
	AudinMsgData         = 0x05
	AudinMsgFormatChange = 0x06
)

// AUDIN version.
const AudinVersion = 0x00000001

// AUDIN sizes.
const (
	AudinHeaderSize     = 1 // msgType(1)
	AudinVersionSize    = 4 // version(4)
	AudinFormatsHdrSize = 4 // numFormats(4)
	AudinOpenSize       = 8 // frameSize(4) + initialFormat(4)
	AudinOpenReplySize  = 4 // result(4)
	AudinDataSize       = 4 // data(variable)
)

// Preferred audio input format: 16-bit PCM, mono/stereo, 48kHz.
const (
	AudinPreferredChannels      = 2
	AudinPreferredSampleRate    = 48000
	AudinPreferredBitsPerSample = 16
	AudinPreferredBlockAlign    = AudinPreferredChannels * (AudinPreferredBitsPerSample / 8)
	AudinPreferredBytesPerSec   = AudinPreferredSampleRate * AudinPreferredBlockAlign
	AudinDefaultFrameSize       = 960 // 10ms at 48kHz stereo 16-bit
)

// Common errors.
var (
	ErrAudinNotReady = errors.New("audin: channel not ready")
	ErrAudinNoFormat = errors.New("audin: no compatible format")
	ErrAudinNotOpen  = errors.New("audin: channel not open")
)

// AudinDataCallback is called when audio data is received from the client.
type AudinDataCallback func(data []byte)

// AudinReadyCallback is called when the audin channel is ready to receive audio.
type AudinReadyCallback func(a *AudinChannel)

// AudinChannel implements the AUDIN dynamic virtual channel.
type AudinChannel struct {
	channel *DVCChannel
	manager *DVCManager

	// Callbacks
	onReady AudinReadyCallback
	onData  AudinDataCallback

	// Negotiated formats
	formats       []AudioFormat
	selectedIndex int
	selectedFmt   AudioFormat
	formatMu      sync.RWMutex

	// Channel state
	isOpen    atomic.Bool
	frameSize uint32

	ready atomic.Bool
}

// NewAudinChannel creates a new AUDIN channel.
func NewAudinChannel(manager *DVCManager) *AudinChannel {
	return &AudinChannel{
		manager:       manager,
		selectedIndex: -1,
		frameSize:     AudinDefaultFrameSize,
	}
}

// SetReadyCallback sets the callback for when the channel is ready.
func (a *AudinChannel) SetReadyCallback(cb AudinReadyCallback) {
	a.onReady = cb
}

// SetDataCallback sets the callback for receiving audio data.
func (a *AudinChannel) SetDataCallback(cb AudinDataCallback) {
	a.onData = cb
}

// Open opens the AUDIN channel.
func (a *AudinChannel) Open() error {
	ch, err := a.manager.CreateChannel(AudinChannelName, a)
	if err != nil {
		return err
	}
	a.channel = ch

	// Send initial version
	return a.sendVersion()
}

// OnData handles incoming AUDIN data from the DVC.
func (a *AudinChannel) OnData(data []byte) error {
	if len(data) < AudinHeaderSize {
		return nil
	}

	msgType := data[0]

	switch msgType {
	case AudinMsgVersion:
		return a.handleVersion(data[AudinHeaderSize:])
	case AudinMsgFormats:
		return a.handleFormats(data[AudinHeaderSize:])
	case AudinMsgOpenReply:
		return a.handleOpenReply(data[AudinHeaderSize:])
	case AudinMsgData:
		return a.handleData(data[AudinHeaderSize:])
	case AudinMsgFormatChange:
		return a.handleFormatChange(data[AudinHeaderSize:])
	}

	return nil
}

// OnClose handles channel close.
func (a *AudinChannel) OnClose() {
	a.ready.Store(false)
	a.isOpen.Store(false)
}

// sendVersion sends the server version to the client.
func (a *AudinChannel) sendVersion() error {
	buf := make([]byte, AudinHeaderSize+AudinVersionSize)
	buf[0] = AudinMsgVersion
	binary.LittleEndian.PutUint32(buf[1:5], AudinVersion)
	return a.channel.SendData(buf)
}

// handleVersion processes the client version response.
func (a *AudinChannel) handleVersion(data []byte) error {
	if len(data) < AudinVersionSize {
		return nil
	}

	// clientVersion := binary.LittleEndian.Uint32(data[0:4])
	// We accept any version >= 1

	// After version exchange, send supported formats
	return a.sendFormats()
}

// sendFormats sends the server's supported audio formats.
func (a *AudinChannel) sendFormats() error {
	// We support one format: 16-bit PCM, stereo, 48kHz
	formatData := make([]byte, SNDCAudioFormatSize)
	binary.LittleEndian.PutUint16(formatData[0:2], WaveFormatPCM)
	binary.LittleEndian.PutUint16(formatData[2:4], AudinPreferredChannels)
	binary.LittleEndian.PutUint32(formatData[4:8], AudinPreferredSampleRate)
	binary.LittleEndian.PutUint32(formatData[8:12], AudinPreferredBytesPerSec)
	binary.LittleEndian.PutUint16(formatData[12:14], AudinPreferredBlockAlign)
	binary.LittleEndian.PutUint16(formatData[14:16], AudinPreferredBitsPerSample)
	binary.LittleEndian.PutUint16(formatData[16:18], 0) // cbSize

	buf := make([]byte, AudinHeaderSize+AudinFormatsHdrSize+len(formatData))
	buf[0] = AudinMsgFormats
	binary.LittleEndian.PutUint32(buf[1:5], 1) // numFormats = 1
	copy(buf[AudinHeaderSize+AudinFormatsHdrSize:], formatData)

	return a.channel.SendData(buf)
}

// handleFormats processes the client's format list.
func (a *AudinChannel) handleFormats(data []byte) error {
	if len(data) < AudinFormatsHdrSize {
		return nil
	}

	numFormats := binary.LittleEndian.Uint32(data[0:4])
	pos := AudinFormatsHdrSize

	a.formatMu.Lock()
	defer a.formatMu.Unlock()

	a.formats = make([]AudioFormat, 0, numFormats)
	selectedIndex := -1

	// Parse client formats
	for i := uint32(0); i < numFormats && pos+SNDCAudioFormatSize <= len(data); i++ {
		fmt := AudioFormat{
			FormatTag:      binary.LittleEndian.Uint16(data[pos : pos+2]),
			Channels:       binary.LittleEndian.Uint16(data[pos+2 : pos+4]),
			SamplesPerSec:  binary.LittleEndian.Uint32(data[pos+4 : pos+8]),
			AvgBytesPerSec: binary.LittleEndian.Uint32(data[pos+8 : pos+12]),
			BlockAlign:     binary.LittleEndian.Uint16(data[pos+12 : pos+14]),
			BitsPerSample:  binary.LittleEndian.Uint16(data[pos+14 : pos+16]),
		}
		cbSize := binary.LittleEndian.Uint16(data[pos+16 : pos+18])

		a.formats = append(a.formats, fmt)

		// Select our preferred format: 16-bit PCM, stereo, 48kHz
		if selectedIndex < 0 &&
			fmt.FormatTag == WaveFormatPCM &&
			fmt.Channels == AudinPreferredChannels &&
			fmt.SamplesPerSec == AudinPreferredSampleRate &&
			fmt.BitsPerSample == AudinPreferredBitsPerSample {
			selectedIndex = int(i)
			a.selectedFmt = fmt
		}

		pos += SNDCAudioFormatSize + int(cbSize)
	}

	// Fallback: accept any PCM format
	if selectedIndex < 0 {
		for i, fmt := range a.formats {
			if fmt.FormatTag == WaveFormatPCM {
				selectedIndex = i
				a.selectedFmt = fmt
				break
			}
		}
	}

	if selectedIndex < 0 {
		return ErrAudinNoFormat
	}

	a.selectedIndex = selectedIndex
	a.ready.Store(true)

	// Notify ready
	if a.onReady != nil {
		a.onReady(a)
	}

	// Open the audio stream with selected format
	return a.sendOpen(uint32(selectedIndex))
}

// sendOpen sends the open request to start audio capture.
func (a *AudinChannel) sendOpen(formatIndex uint32) error {
	buf := make([]byte, AudinHeaderSize+AudinOpenSize)
	buf[0] = AudinMsgOpen

	// Frame size in bytes (10ms of audio)
	a.frameSize = AudinDefaultFrameSize
	if a.selectedFmt.BlockAlign > 0 {
		// Calculate 10ms frame size
		a.frameSize = a.selectedFmt.SamplesPerSec / 100 * uint32(a.selectedFmt.BlockAlign)
	}

	binary.LittleEndian.PutUint32(buf[1:5], a.frameSize)
	binary.LittleEndian.PutUint32(buf[5:9], formatIndex)

	return a.channel.SendData(buf)
}

// handleOpenReply processes the open reply from the client.
func (a *AudinChannel) handleOpenReply(data []byte) error {
	if len(data) < AudinOpenReplySize {
		return nil
	}

	result := binary.LittleEndian.Uint32(data[0:4])

	if result == 0 {
		// Success - client is now sending audio
		a.isOpen.Store(true)
	}

	return nil
}

// handleData processes incoming audio data from the client.
func (a *AudinChannel) handleData(data []byte) error {
	if !a.isOpen.Load() || a.onData == nil {
		return nil
	}

	// Data format: raw PCM audio samples
	if len(data) > 0 && a.onData != nil {
		a.onData(data)
	}

	return nil
}

// handleFormatChange processes format change notification.
func (a *AudinChannel) handleFormatChange(data []byte) error {
	if len(data) < 4 {
		return nil
	}

	newFormat := binary.LittleEndian.Uint32(data[0:4])

	a.formatMu.Lock()
	if int(newFormat) < len(a.formats) {
		a.selectedIndex = int(newFormat)
		a.selectedFmt = a.formats[newFormat]
	}
	a.formatMu.Unlock()

	return nil
}

// IsReady returns true if the channel is ready.
func (a *AudinChannel) IsReady() bool {
	return a.ready.Load()
}

// IsOpen returns true if the audio stream is open.
func (a *AudinChannel) IsOpen() bool {
	return a.isOpen.Load()
}

// GetSelectedFormat returns the negotiated audio format.
func (a *AudinChannel) GetSelectedFormat() (AudioFormat, bool) {
	a.formatMu.RLock()
	defer a.formatMu.RUnlock()

	if a.selectedIndex < 0 {
		return AudioFormat{}, false
	}
	return a.selectedFmt, true
}

// Close closes the audin channel.
func (a *AudinChannel) Close() error {
	a.ready.Store(false)
	a.isOpen.Store(false)

	if a.channel != nil {
		return a.channel.Close()
	}
	return nil
}
