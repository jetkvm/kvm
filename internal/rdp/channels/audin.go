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

// AUDIN message types (per MS-RDPEAI 2.2.2).
const (
	AudinMsgVersion      = 0x01 // CYCAP_REQ / SNDIN_VERSION
	AudinMsgFormats      = 0x02 // SNDIN_FORMATS
	AudinMsgOpen         = 0x03 // SNDIN_OPEN
	AudinMsgOpenReply    = 0x04 // SNDIN_OPEN_REPLY
	AudinMsgDataIncoming = 0x05 // SNDIN_DATA_INCOMING (client notification before data)
	AudinMsgData         = 0x06 // SNDIN_DATA (actual audio data)
	AudinMsgFormatChange = 0x07 // SNDIN_FORMATCHANGE
)

// AUDIN version.
const AudinVersion = 0x00000001

// AUDIN sizes.
const (
	AudinHeaderSize     = 1  // msgType(1)
	AudinVersionSize    = 4  // version(4)
	AudinFormatsHdrSize = 8  // numFormats(4) + cbSizeFormatsPacket(4)
	AudinOpenSize       = 26 // frameSize(4) + initialFormat(4) + WAVEFORMATEX(18)
	AudinOpenReplySize  = 4  // result(4)
)

// Default frames per packet: 480 frames for 10ms at 48kHz.
const audinDefaultFramesPerPacket = audioPreferredSampleRate / 100

// Common errors.
var (
	ErrAudinNotReady = errors.New("audin: channel not ready")
	ErrAudinNoFormat = errors.New("audin: no compatible format")
	ErrAudinNotOpen  = errors.New("audin: channel not open")
)

// audinMsgNames maps message types to names for logging (package-level to avoid allocation).
var audinMsgNames = [256]string{
	AudinMsgVersion:      "VERSION",
	AudinMsgFormats:      "FORMATS",
	AudinMsgOpen:         "OPEN",
	AudinMsgOpenReply:    "OPEN_REPLY",
	AudinMsgDataIncoming: "DATA_INCOMING",
	AudinMsgData:         "DATA",
	AudinMsgFormatChange: "FORMAT_CHANGE",
}

// AudinDataCallback is called when audio data is received from the client.
type AudinDataCallback func(data []byte)

// AudinReadyCallback is called when the audin channel is ready to receive audio.
type AudinReadyCallback func(a *AudinChannel)

// AudinCloseCallback is called when the AUDIN channel is closed (by client request or server teardown).
type AudinCloseCallback func()

// AudinLogFunc is a simple logging function for AUDIN events.
type AudinLogFunc func(msg string, args ...any)

// AudinChannel implements the AUDIN dynamic virtual channel.
type AudinChannel struct {
	channel *DVCChannel
	manager *DVCManager

	// Callbacks
	onReady AudinReadyCallback
	onData  AudinDataCallback
	onClose AudinCloseCallback

	// Optional logger for debugging
	logger AudinLogFunc

	// Negotiated formats
	formats       []AudioFormat
	selectedIndex int
	selectedFmt   AudioFormat
	formatMu      sync.RWMutex

	// Channel state
	isOpen          atomic.Bool
	framesPerPacket uint32

	ready atomic.Bool
}

// NewAudinChannel creates a new AUDIN channel.
func NewAudinChannel(manager *DVCManager) *AudinChannel {
	return &AudinChannel{
		manager:         manager,
		selectedIndex:   -1,
		framesPerPacket: audinDefaultFramesPerPacket,
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

// SetLogger sets the debug logger for the AUDIN channel.
func (a *AudinChannel) SetLogger(logger AudinLogFunc) {
	a.logger = logger
}

// Open opens the AUDIN channel.
func (a *AudinChannel) Open() error {
	ch, err := a.manager.CreateChannel(AudinChannelName, a)
	if err != nil {
		return err
	}
	a.channel = ch
	// VERSION will be sent when OnChannelOpen is called
	return nil
}

// OnChannelOpen is called when the DVC channel is successfully created.
// This implements the DVCOpenHandler interface.
func (a *AudinChannel) OnChannelOpen() {
	if a.logger != nil {
		a.logger("AUDIN: channel opened, sending version")
	}
	if err := a.sendVersion(); err != nil {
		if a.logger != nil {
			a.logger("AUDIN: failed to send version: %v", err)
		}
	}
}

// OnData handles incoming AUDIN data from the DVC.
func (a *AudinChannel) OnData(data []byte) error {
	if len(data) < AudinHeaderSize {
		if a.logger != nil {
			a.logger("AUDIN: received data too short: len=%d", len(data))
		}
		return nil
	}

	msgType := data[0]

	// Log non-routine message types for debugging
	// Skip DATA and DATA_INCOMING as they're high-frequency (100/sec)
	if a.logger != nil && msgType != AudinMsgData && msgType != AudinMsgDataIncoming {
		name := audinMsgNames[msgType]
		if name == "" {
			name = "UNKNOWN"
		}
		a.logger("AUDIN: received %s (0x%02X) len=%d isOpen=%v", name, msgType, len(data), a.isOpen.Load())
	}

	switch msgType {
	case AudinMsgVersion:
		return a.handleVersion(data[AudinHeaderSize:])
	case AudinMsgFormats:
		return a.handleFormats(data[AudinHeaderSize:])
	case AudinMsgOpenReply:
		return a.handleOpenReply(data[AudinHeaderSize:])
	case AudinMsgDataIncoming:
		// Client is about to send audio data - this is just a notification
		// No action needed, actual data follows in AudinMsgData
		return nil
	case AudinMsgData:
		return a.handleData(data[AudinHeaderSize:])
	case AudinMsgFormatChange:
		return a.handleFormatChange(data[AudinHeaderSize:])
	default:
		if a.logger != nil {
			a.logger("AUDIN: ignoring unknown message type 0x%02X", msgType)
		}
	}

	return nil
}

// OnClose handles channel close.
func (a *AudinChannel) OnClose() {
	a.ready.Store(false)
	a.isOpen.Store(false)
	if a.onClose != nil {
		a.onClose()
	}
}

// SetCloseCallback sets a callback invoked when the AUDIN channel is closed.
func (a *AudinChannel) SetCloseCallback(cb AudinCloseCallback) {
	a.onClose = cb
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

	clientVersion := binary.LittleEndian.Uint32(data[0:4])
	if a.logger != nil {
		a.logger("AUDIN: received client version %d", clientVersion)
	}

	// After version exchange, send supported formats
	return a.sendFormats()
}

// sendFormats sends the server's supported audio formats.
func (a *AudinChannel) sendFormats() error {
	if a.logger != nil {
		a.logger("AUDIN: sending server formats (PCM stereo 48kHz)")
	}

	// We support one format: 16-bit PCM, stereo, 48kHz (uses shared helper)
	formatData := make([]byte, waveformatexSize)
	encodePreferredWAVEFORMATEX(formatData, 0)

	buf := make([]byte, AudinHeaderSize+AudinFormatsHdrSize+len(formatData))
	buf[0] = AudinMsgFormats
	binary.LittleEndian.PutUint32(buf[1:5], 1)                       // numFormats = 1
	binary.LittleEndian.PutUint32(buf[5:9], uint32(len(formatData))) // cbSizeFormatsPacket
	copy(buf[AudinHeaderSize+AudinFormatsHdrSize:], formatData)

	return a.channel.SendData(buf)
}

// Maximum number of formats we'll accept (sanity limit to prevent OOM).
const AudinMaxFormats = 256

// handleFormats processes the client's format list.
func (a *AudinChannel) handleFormats(data []byte) error {
	// Guard: Don't process FORMATS if we've already completed format negotiation
	// This prevents data corruption from being misinterpreted as a FORMATS message
	if a.ready.Load() {
		if a.logger != nil {
			a.logger("AUDIN: ignoring FORMATS - already negotiated (data corruption?)")
		}
		return nil
	}

	if len(data) < AudinFormatsHdrSize {
		return nil
	}

	numFormats := binary.LittleEndian.Uint32(data[0:4])
	cbSizeFormatsPacket := binary.LittleEndian.Uint32(data[4:8])
	if a.logger != nil {
		a.logger("AUDIN: received client formats: numFormats=%d cbSize=%d dataLen=%d", numFormats, cbSizeFormatsPacket, len(data))
	}

	// Sanity check: prevent OOM from malformed/corrupted data
	if numFormats > AudinMaxFormats {
		if a.logger != nil {
			a.logger("AUDIN: rejecting formats - numFormats=%d exceeds max %d (data corruption?)", numFormats, AudinMaxFormats)
		}
		return nil
	}

	// Note: cbSizeFormatsPacket is supposed to be the size of SoundFormats,
	// but some clients report incorrect values. We proceed with what data we have
	// and let the format parsing handle any truncation.
	availableFormatData := len(data) - AudinFormatsHdrSize
	if a.logger != nil && int(cbSizeFormatsPacket) != availableFormatData {
		a.logger("AUDIN: cbSize=%d doesn't match available data %d (proceeding anyway)", cbSizeFormatsPacket, availableFormatData)
	}

	pos := AudinFormatsHdrSize

	// Parse formats under lock, but release before callbacks to avoid deadlock
	a.formatMu.Lock()
	a.formats = make([]AudioFormat, 0, numFormats)

	// Parse client formats (uses shared WAVEFORMATEX parser)
	for i := uint32(0); i < numFormats && pos+waveformatexSize <= len(data); i++ {
		fmt, cbSize, ok := parseWAVEFORMATEX(data, pos)
		if !ok {
			break
		}
		a.formats = append(a.formats, fmt)
		pos += waveformatexSize + int(cbSize)
	}

	// Find best match (uses shared format selection logic)
	selectedIndex, selectedFmt := findPreferredFormat(a.formats)
	if selectedIndex < 0 {
		a.formatMu.Unlock()
		if a.logger != nil {
			a.logger("AUDIN: no compatible format found")
		}
		return ErrAudinNoFormat
	}

	a.selectedIndex = selectedIndex
	a.selectedFmt = selectedFmt
	a.ready.Store(true)
	a.formatMu.Unlock() // Release lock BEFORE callbacks to avoid deadlock

	if a.logger != nil {
		a.logger("AUDIN: selected format index %d (tag=%d ch=%d rate=%d bits=%d)",
			selectedIndex, selectedFmt.FormatTag, selectedFmt.Channels,
			selectedFmt.SamplesPerSec, selectedFmt.BitsPerSample)
	}

	// Notify ready (outside lock to avoid deadlock with GetSelectedFormat)
	if a.onReady != nil {
		a.onReady(a)
	}

	// Open the audio stream with format index 0 (the only format we advertised)
	// The initialFormat in OPEN refers to the SERVER's format list, not the client's
	return a.sendOpen(0)
}

// sendOpen sends the open request to start audio capture.
func (a *AudinChannel) sendOpen(formatIndex uint32) error {
	if a.logger != nil {
		a.logger("AUDIN: sending OPEN (formatIndex=%d, framesPerPacket=%d)", formatIndex, audioPreferredSampleRate/100)
	}
	buf := make([]byte, AudinHeaderSize+AudinOpenSize)
	buf[0] = AudinMsgOpen

	// Frames per packet (10ms of audio at sample rate)
	// This is the number of FRAMES (samples per channel), not bytes
	framesPerPacket := audioPreferredSampleRate / 100 // 480 frames for 10ms at 48kHz

	pos := AudinHeaderSize
	binary.LittleEndian.PutUint32(buf[pos:pos+4], uint32(framesPerPacket))
	pos += 4
	binary.LittleEndian.PutUint32(buf[pos:pos+4], formatIndex)
	pos += 4

	// WAVEFORMATEX structure (captureFormat) - uses shared helper
	encodePreferredWAVEFORMATEX(buf, pos)

	return a.channel.SendData(buf)
}

// handleOpenReply processes the open reply from the client.
func (a *AudinChannel) handleOpenReply(data []byte) error {
	if len(data) < AudinOpenReplySize {
		return nil
	}

	result := binary.LittleEndian.Uint32(data[0:4])

	if a.logger != nil {
		a.logger("AUDIN: received open reply: result=%d (0=success)", result)
	}

	if result == 0 {
		// Success - client is now sending audio
		a.isOpen.Store(true)
	}

	return nil
}

// handleData processes incoming audio data from the client.
func (a *AudinChannel) handleData(data []byte) error {
	if !a.isOpen.Load() || a.onData == nil || len(data) == 0 {
		return nil
	}
	a.onData(data)
	return nil
}

// handleFormatChange processes format change notification.
func (a *AudinChannel) handleFormatChange(data []byte) error {
	if len(data) < 4 {
		return nil
	}

	newFormat := binary.LittleEndian.Uint32(data[0:4])

	a.formatMu.Lock()
	defer a.formatMu.Unlock()

	// Bounds check to prevent panic from corrupted data
	if a.formats == nil || newFormat >= uint32(len(a.formats)) {
		if a.logger != nil {
			a.logger("AUDIN: ignoring format change to invalid index %d (have %d formats)", newFormat, len(a.formats))
		}
		return nil
	}

	a.selectedIndex = int(newFormat)
	a.selectedFmt = a.formats[newFormat]
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
