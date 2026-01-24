// Package channels implements RDP virtual channels.
package channels

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"unicode/utf16"
)

// CLIPRDR is a STATIC virtual channel (like RDPSND), not a DVC.
// It implements the MS-RDPECLIP protocol for clipboard redirection.
// This implementation only supports client-to-server (unidirectional) clipboard.
const ClipboardChannelName = "cliprdr"

// CLIPRDR message types (MS-RDPECLIP 2.2.1).
const (
	CBMonitorReady     = 0x0001
	CBFormatList       = 0x0002
	CBFormatListResp   = 0x0003
	CBFormatDataReq    = 0x0004
	CBFormatDataResp   = 0x0005
	CBTempDirectory    = 0x0006
	CBClipCaps         = 0x0007
	CBFileContentsReq  = 0x0008
	CBFileContentsResp = 0x0009
	CBLockClipData     = 0x000A
	CBUnlockClipData   = 0x000B
)

// CLIPRDR message flags.
const (
	CBResponseOK   = 0x0001
	CBResponseFail = 0x0002
	CBASCIINames   = 0x0004
)

// Clipboard format IDs.
const (
	CFText        = 1  // CF_TEXT (ANSI)
	CFUnicodeText = 13 // CF_UNICODETEXT (UTF-16LE)
)

// General capability flags.
const (
	CBUseLongFormatNames    = 0x00000002
	CBStreamFileClipEnabled = 0x00000004
	CBFileClipNoFilePaths   = 0x00000008
	CBCanLockClipData       = 0x00000010
	CBHugeFileSupport       = 0x00000020
)

// File contents request flags (MS-RDPECLIP 2.2.5.3).
const (
	FileContentsSize  = 0x00000001
	FileContentsRange = 0x00000002
)

// FileGroupDescriptorW format name.
const FileGroupDescriptorW = "FileGroupDescriptorW"

// FileDescriptor represents a file in the clipboard (MS-RDPECLIP 2.2.5.2.3.1).
// Total size: 592 bytes per descriptor.
type FileDescriptor struct {
	Flags          uint32
	FileAttributes uint32
	LastWriteTime  uint64 // FILETIME
	FileSizeHigh   uint32
	FileSizeLow    uint32
	FileName       string
}

// FileSize returns the 64-bit file size.
func (f *FileDescriptor) FileSize() uint64 {
	return uint64(f.FileSizeHigh)<<32 | uint64(f.FileSizeLow)
}

// ClipboardFile represents a file being transferred.
type ClipboardFile struct {
	Descriptor FileDescriptor
	TempPath   string   // Path to temp file on disk
	Received   uint64   // Bytes received so far
	Complete   bool
	file       *os.File // Open file handle during transfer (nil when complete)
}

// FileTransferCallback is called when file transfer completes.
type FileTransferCallback func(files []*ClipboardFile)

// Capability set type.
const (
	CBCapsTypeGeneral = 0x0001
)

// ClipboardChannel implements the CLIPRDR static virtual channel.
type ClipboardChannel struct {
	sendFunc func(data []byte) error
	logger   func(format string, args ...any)

	// Stored clipboard text (UTF-8)
	clipboardText []byte
	clipboardMu   sync.Mutex

	// Client capabilities
	useLongFormatNames    bool
	streamFileClipEnabled bool
	clientVersion         uint32

	// File transfer state
	fileTransferEnabled bool
	files               []*ClipboardFile
	filesMu             sync.Mutex
	tempDir             string
	currentFileIndex    int
	currentStreamID     uint32
	fileTransferCb      FileTransferCallback

	// File format ID (dynamically assigned by client)
	fileGroupDescriptorFormatID uint32

	// Track when file transfer is in progress
	fileTransferInProgress bool

	// Ready state (atomic for thread-safe access)
	ready atomic.Bool
}

// NewClipboardChannel creates a new CLIPRDR channel.
func NewClipboardChannel(sendFunc func([]byte) error) *ClipboardChannel {
	return &ClipboardChannel{
		sendFunc: sendFunc,
		tempDir:  os.TempDir(),
	}
}

// EnableFileTransfer enables file clipboard support.
func (c *ClipboardChannel) EnableFileTransfer(enabled bool) {
	c.fileTransferEnabled = enabled
}

// SetFileTransferCallback sets the callback for completed file transfers.
func (c *ClipboardChannel) SetFileTransferCallback(cb FileTransferCallback) {
	c.fileTransferCb = cb
}

// SetTempDir sets the temporary directory for file storage.
func (c *ClipboardChannel) SetTempDir(dir string) {
	c.tempDir = dir
}

// SetLogger sets the logging function.
func (c *ClipboardChannel) SetLogger(logger func(format string, args ...any)) {
	c.logger = logger
}

func (c *ClipboardChannel) log(format string, args ...any) {
	if c.logger != nil {
		c.logger(format, args...)
	}
}

// Start sends initial Capabilities and Monitor Ready PDUs.
func (c *ClipboardChannel) Start() error {
	// Send Clipboard Capabilities first
	if err := c.sendCapabilities(); err != nil {
		return err
	}
	// Send Monitor Ready to indicate server is ready
	return c.sendMonitorReady()
}

func (c *ClipboardChannel) sendCapabilities() error {
	// CLIPRDR_CAPS (MS-RDPECLIP 2.2.2.1)
	// Header: msgType(2) + msgFlags(2) + dataLen(4) = 8 bytes
	// Capabilities: cCapabilitiesSets(2) + pad(2) = 4 bytes
	// General cap: capType(2) + capLen(2) + version(4) + flags(4) = 12 bytes
	// Total: 8 + 4 + 12 = 24 bytes

	buf := make([]byte, 24)

	// Header
	binary.LittleEndian.PutUint16(buf[0:2], CBClipCaps)
	binary.LittleEndian.PutUint16(buf[2:4], 0)  // msgFlags
	binary.LittleEndian.PutUint32(buf[4:8], 16) // dataLen (4 + 12)

	// Capabilities header
	binary.LittleEndian.PutUint16(buf[8:10], 1) // cCapabilitiesSets
	// pad[10:12] = 0

	// General capability set
	binary.LittleEndian.PutUint16(buf[12:14], CBCapsTypeGeneral) // capabilitySetType
	binary.LittleEndian.PutUint16(buf[14:16], 12)                // lengthCapability
	binary.LittleEndian.PutUint32(buf[16:20], 2)                 // version (CB_CAPS_VERSION_2)

	// Capability flags - include file transfer if enabled
	flags := uint32(CBUseLongFormatNames)
	if c.fileTransferEnabled {
		flags |= CBStreamFileClipEnabled | CBFileClipNoFilePaths
	}
	binary.LittleEndian.PutUint32(buf[20:24], flags)

	c.log("CLIPRDR: sending capabilities (fileTransfer=%v)", c.fileTransferEnabled)
	return c.sendFunc(buf)
}

func (c *ClipboardChannel) sendMonitorReady() error {
	// CLIPRDR_MONITOR_READY (MS-RDPECLIP 2.2.2.2)
	buf := make([]byte, 8)
	binary.LittleEndian.PutUint16(buf[0:2], CBMonitorReady)
	binary.LittleEndian.PutUint16(buf[2:4], 0) // msgFlags
	binary.LittleEndian.PutUint32(buf[4:8], 0) // dataLen

	c.ready.Store(true)
	c.log("CLIPRDR: sending monitor ready")
	return c.sendFunc(buf)
}

// HandlePDU processes incoming clipboard PDUs.
func (c *ClipboardChannel) HandlePDU(data []byte) error {
	if len(data) < 8 {
		c.log("CLIPRDR: PDU too short (%d bytes)", len(data))
		return nil
	}

	msgType := binary.LittleEndian.Uint16(data[0:2])
	msgFlags := binary.LittleEndian.Uint16(data[2:4])
	dataLen := binary.LittleEndian.Uint32(data[4:8])

	c.log("CLIPRDR: received PDU type=0x%04X flags=0x%04X dataLen=%d totalLen=%d",
		msgType, msgFlags, dataLen, len(data))

	payload := data[8:]
	if uint32(len(payload)) < dataLen {
		c.log("CLIPRDR: payload too short: have %d, need %d", len(payload), dataLen)
		return nil
	}
	payload = payload[:dataLen]

	switch msgType {
	case CBClipCaps:
		c.log("CLIPRDR: processing CB_CLIP_CAPS")
		return c.handleCapabilities(payload)
	case CBFormatList:
		c.log("CLIPRDR: processing CB_FORMAT_LIST (client copied something)")
		return c.handleFormatList(payload, msgFlags)
	case CBFormatDataResp:
		c.log("CLIPRDR: processing CB_FORMAT_DATA_RESPONSE")
		return c.handleFormatDataResponse(payload, msgFlags)
	case CBFileContentsResp:
		c.log("CLIPRDR: processing CB_FILE_CONTENTS_RESPONSE")
		return c.handleFileContentsResponse(payload, msgFlags)
	case CBLockClipData:
		c.log("CLIPRDR: received lock clipboard (ignored)")
		return nil
	case CBUnlockClipData:
		c.log("CLIPRDR: received unlock clipboard (ignored)")
		return nil
	default:
		c.log("CLIPRDR: unhandled message type 0x%04X", msgType)
		return nil
	}
}

func (c *ClipboardChannel) handleCapabilities(data []byte) error {
	if len(data) < 4 {
		return nil
	}

	numCapSets := binary.LittleEndian.Uint16(data[0:2])
	c.log("CLIPRDR: received capabilities, %d capability sets", numCapSets)

	// Parse capability sets
	pos := 4 // Skip cCapabilitiesSets(2) + pad(2)
	for i := uint16(0); i < numCapSets && pos+4 <= len(data); i++ {
		capType := binary.LittleEndian.Uint16(data[pos : pos+2])
		capLen := binary.LittleEndian.Uint16(data[pos+2 : pos+4])

		if capType == CBCapsTypeGeneral && capLen >= 12 && pos+int(capLen) <= len(data) {
			c.clientVersion = binary.LittleEndian.Uint32(data[pos+4 : pos+8])
			flags := binary.LittleEndian.Uint32(data[pos+8 : pos+12])
			c.useLongFormatNames = (flags & CBUseLongFormatNames) != 0
			c.streamFileClipEnabled = (flags & CBStreamFileClipEnabled) != 0
			c.log("CLIPRDR: client version=%d, longNames=%v, fileClip=%v",
				c.clientVersion, c.useLongFormatNames, c.streamFileClipEnabled)
		}

		pos += int(capLen)
	}

	return nil
}

func (c *ClipboardChannel) handleFormatList(data []byte, msgFlags uint16) error {
	// Client announces clipboard formats available
	c.log("CLIPRDR: received format list (%d bytes, flags=0x%04X)", len(data), msgFlags)

	// Reset file transfer state - close any open file handles first
	c.filesMu.Lock()
	for _, f := range c.files {
		c.closeFileHandle(f)
	}
	c.files = nil
	c.fileGroupDescriptorFormatID = 0
	c.filesMu.Unlock()

	// Clear any stored text - new clipboard content replaces old
	c.clipboardMu.Lock()
	c.clipboardText = nil
	c.clipboardMu.Unlock()

	// Parse format list to check what's available
	var hasText, hasFiles bool
	useLong := c.useLongFormatNames || (msgFlags&CBASCIINames) == 0
	c.log("CLIPRDR: parsing format list (useLongNames=%v, fileTransferEnabled=%v, streamFileClipEnabled=%v)",
		useLong, c.fileTransferEnabled, c.streamFileClipEnabled)

	if useLong {
		// Long format names (UTF-16LE null-terminated strings)
		hasText, hasFiles = c.parseFormatListLong(data)
	} else {
		// Short format names (32-byte fixed ASCII names)
		// Note: Short format doesn't support file transfers
		hasText = c.parseFormatListShort(data)
	}

	c.log("CLIPRDR: format list parsed: hasText=%v, hasFiles=%v", hasText, hasFiles)

	// Send Format List Response (acknowledge)
	resp := make([]byte, 8)
	binary.LittleEndian.PutUint16(resp[0:2], CBFormatListResp)
	binary.LittleEndian.PutUint16(resp[2:4], CBResponseOK)
	binary.LittleEndian.PutUint32(resp[4:8], 0)

	c.log("CLIPRDR: sending format list response")
	if err := c.sendFunc(resp); err != nil {
		c.log("CLIPRDR: failed to send format list response: %v", err)
		return err
	}
	c.log("CLIPRDR: format list response sent successfully")

	// Prefer files over text if both available and file transfer enabled
	if hasFiles && c.fileTransferEnabled && c.streamFileClipEnabled {
		c.log("CLIPRDR: requesting FileGroupDescriptorW (formatID=%d)", c.fileGroupDescriptorFormatID)
		c.filesMu.Lock()
		c.fileTransferInProgress = true
		c.filesMu.Unlock()
		return c.requestFormatData(c.fileGroupDescriptorFormatID)
	}

	// Fall back to text
	if hasText {
		c.log("CLIPRDR: requesting CF_UNICODETEXT")
		return c.requestFormatData(CFUnicodeText)
	}

	c.log("CLIPRDR: no supported formats in clipboard")

	return nil
}

func (c *ClipboardChannel) parseFormatListLong(data []byte) (hasText, hasFiles bool) {
	// Long format: formatId(4) + formatName(UTF-16LE, null-terminated)
	pos := 0
	for pos+4 <= len(data) {
		formatID := binary.LittleEndian.Uint32(data[pos : pos+4])
		pos += 4

		// Find null terminator (UTF-16LE)
		nameStart := pos
		for pos+2 <= len(data) {
			if data[pos] == 0 && data[pos+1] == 0 {
				pos += 2
				break
			}
			pos += 2
		}

		// Check for text formats
		if formatID == CFText || formatID == CFUnicodeText {
			hasText = true
		}

		// Check format name
		if nameStart < pos-2 {
			name := string(c.utf16LEToUTF8(data[nameStart : pos-2]))
			c.log("CLIPRDR: format %d: %s", formatID, name)

			// Check for FileGroupDescriptorW
			if name == FileGroupDescriptorW {
				hasFiles = true
				c.fileGroupDescriptorFormatID = formatID
			}
		}
	}
	return hasText, hasFiles
}

func (c *ClipboardChannel) parseFormatListShort(data []byte) (hasText bool) {
	// Short format: formatId(4) + formatName(32 bytes ASCII, null-padded)
	// Note: Short format doesn't support file transfers (requires long format names)
	for pos := 0; pos+36 <= len(data); pos += 36 {
		formatID := binary.LittleEndian.Uint32(data[pos : pos+4])
		if formatID == CFText || formatID == CFUnicodeText {
			hasText = true
		}
	}
	return hasText
}

func (c *ClipboardChannel) requestFormatData(formatID uint32) error {
	// CLIPRDR_FORMAT_DATA_REQUEST (MS-RDPECLIP 2.2.5.1)
	buf := make([]byte, 12)
	binary.LittleEndian.PutUint16(buf[0:2], CBFormatDataReq)
	binary.LittleEndian.PutUint16(buf[2:4], 0) // msgFlags
	binary.LittleEndian.PutUint32(buf[4:8], 4) // dataLen
	binary.LittleEndian.PutUint32(buf[8:12], formatID)
	return c.sendFunc(buf)
}

func (c *ClipboardChannel) handleFormatDataResponse(data []byte, msgFlags uint16) error {
	// Check for failure
	if (msgFlags & CBResponseFail) != 0 {
		c.log("CLIPRDR: format data request failed")
		return nil
	}

	if len(data) == 0 {
		c.log("CLIPRDR: received empty clipboard data")
		return nil
	}

	// Check if this is a file list response (FileGroupDescriptorW)
	// File list starts with count (4 bytes) followed by FILEDESCRIPTOR structs
	if c.fileGroupDescriptorFormatID != 0 && len(data) >= 4 {
		count := binary.LittleEndian.Uint32(data[0:4])
		// Each FILEDESCRIPTOR is 592 bytes, check if data size matches
		expectedSize := 4 + count*592
		if uint32(len(data)) >= expectedSize && count > 0 && count < 1000 {
			return c.handleFileListResponse(data)
		}
	}

	// Otherwise treat as text (UTF-16LE, convert to UTF-8)
	text := c.utf16LEToUTF8(data)

	c.log("CLIPRDR: received clipboard text (%d bytes UTF-8)", len(text))

	// Store for later typing
	c.clipboardMu.Lock()
	c.clipboardText = text
	c.clipboardMu.Unlock()

	return nil
}

func (c *ClipboardChannel) utf16LEToUTF8(data []byte) []byte {
	// Handle odd length
	if len(data)%2 != 0 {
		data = data[:len(data)-1]
	}

	// Remove null terminators
	for len(data) >= 2 && data[len(data)-1] == 0 && data[len(data)-2] == 0 {
		data = data[:len(data)-2]
	}

	if len(data) == 0 {
		return nil
	}

	// Convert UTF-16LE to UTF-8
	u16s := make([]uint16, len(data)/2)
	for i := range u16s {
		u16s[i] = binary.LittleEndian.Uint16(data[i*2:])
	}

	runes := utf16.Decode(u16s)
	return []byte(string(runes))
}

// GetClipboardText returns stored clipboard text for typing.
// Returns nil if no clipboard text is available.
func (c *ClipboardChannel) GetClipboardText() []byte {
	c.clipboardMu.Lock()
	defer c.clipboardMu.Unlock()

	if len(c.clipboardText) == 0 {
		return nil
	}

	// Return copy
	text := make([]byte, len(c.clipboardText))
	copy(text, c.clipboardText)
	return text
}

// ClearClipboardText clears the stored clipboard text.
func (c *ClipboardChannel) ClearClipboardText() {
	c.clipboardMu.Lock()
	c.clipboardText = nil
	c.clipboardMu.Unlock()
}

// IsReady returns true if the channel has completed initialization.
func (c *ClipboardChannel) IsReady() bool {
	return c.ready.Load()
}

// IsFileTransferInProgress returns true if a file transfer is currently in progress.
func (c *ClipboardChannel) IsFileTransferInProgress() bool {
	c.filesMu.Lock()
	defer c.filesMu.Unlock()
	return c.fileTransferInProgress
}

// handleFileListResponse parses the FileGroupDescriptorW format data.
func (c *ClipboardChannel) handleFileListResponse(data []byte) error {
	count := binary.LittleEndian.Uint32(data[0:4])
	c.log("CLIPRDR: received file list with %d files", count)

	c.filesMu.Lock()
	defer c.filesMu.Unlock()

	// Parse file descriptors (592 bytes each)
	c.files = make([]*ClipboardFile, 0, count)
	pos := 4
	for i := uint32(0); i < count && pos+592 <= len(data); i++ {
		desc := c.parseFileDescriptor(data[pos : pos+592])
		c.log("CLIPRDR: file[%d]: %s (%d bytes)", i, desc.FileName, desc.FileSize())

		// Create temp file path with secure random token
		token, err := randomToken(8)
		if err != nil {
			c.log("CLIPRDR: failed to generate secure token for file %d: %v", i, err)
			return err
		}
		tempPath := filepath.Join(c.tempDir, "jkvm-clip-"+token+"-"+sanitizeFileName(desc.FileName))

		c.files = append(c.files, &ClipboardFile{
			Descriptor: desc,
			TempPath:   tempPath,
		})
		pos += 592
	}

	// Start requesting file contents
	if len(c.files) > 0 {
		c.currentFileIndex = 0
		c.currentStreamID = 1
		return c.requestFileSize(0)
	}

	return nil
}

// parseFileDescriptor parses a CLIPRDR_FILEDESCRIPTOR (592 bytes).
func (c *ClipboardChannel) parseFileDescriptor(data []byte) FileDescriptor {
	desc := FileDescriptor{
		Flags:          binary.LittleEndian.Uint32(data[0:4]),
		FileAttributes: binary.LittleEndian.Uint32(data[36:40]),
		LastWriteTime:  binary.LittleEndian.Uint64(data[56:64]),
		FileSizeHigh:   binary.LittleEndian.Uint32(data[64:68]),
		FileSizeLow:    binary.LittleEndian.Uint32(data[68:72]),
	}

	// FileName is at offset 72, 520 bytes (260 UTF-16LE chars)
	desc.FileName = string(c.utf16LEToUTF8(data[72:592]))

	return desc
}

// requestFileSize sends a CB_FILECONTENTS_REQUEST for file size.
func (c *ClipboardChannel) requestFileSize(fileIndex int) error {
	// CB_FILECONTENTS_REQUEST (MS-RDPECLIP 2.2.5.3)
	buf := make([]byte, 36) // 8 header + 28 data

	binary.LittleEndian.PutUint16(buf[0:2], CBFileContentsReq)
	binary.LittleEndian.PutUint16(buf[2:4], 0)  // msgFlags
	binary.LittleEndian.PutUint32(buf[4:8], 28) // dataLen

	binary.LittleEndian.PutUint32(buf[8:12], c.currentStreamID)  // streamId
	binary.LittleEndian.PutUint32(buf[12:16], uint32(fileIndex)) // lindex
	binary.LittleEndian.PutUint32(buf[16:20], FileContentsSize)  // dwFlags
	binary.LittleEndian.PutUint32(buf[20:24], 0)                 // nPositionLow
	binary.LittleEndian.PutUint32(buf[24:28], 0)                 // nPositionHigh
	binary.LittleEndian.PutUint32(buf[28:32], 8)                 // cbRequested (8 bytes for size)
	binary.LittleEndian.PutUint32(buf[32:36], 0)                 // clipDataId (optional)

	c.log("CLIPRDR: requesting size for file %d", fileIndex)
	return c.sendFunc(buf)
}

// requestFileRange sends a CB_FILECONTENTS_REQUEST for file data.
func (c *ClipboardChannel) requestFileRange(fileIndex int, offset, size uint64) error {
	buf := make([]byte, 36)

	binary.LittleEndian.PutUint16(buf[0:2], CBFileContentsReq)
	binary.LittleEndian.PutUint16(buf[2:4], 0)
	binary.LittleEndian.PutUint32(buf[4:8], 28)

	binary.LittleEndian.PutUint32(buf[8:12], c.currentStreamID)
	binary.LittleEndian.PutUint32(buf[12:16], uint32(fileIndex))
	binary.LittleEndian.PutUint32(buf[16:20], FileContentsRange)
	binary.LittleEndian.PutUint32(buf[20:24], uint32(offset))     // nPositionLow
	binary.LittleEndian.PutUint32(buf[24:28], uint32(offset>>32)) // nPositionHigh
	binary.LittleEndian.PutUint32(buf[28:32], uint32(size))       // cbRequested
	binary.LittleEndian.PutUint32(buf[32:36], 0)

	c.log("CLIPRDR: requesting range for file %d: offset=%d, size=%d", fileIndex, offset, size)
	return c.sendFunc(buf)
}

// handleFileContentsResponse handles CB_FILECONTENTS_RESPONSE.
func (c *ClipboardChannel) handleFileContentsResponse(data []byte, msgFlags uint16) error {
	if (msgFlags & CBResponseFail) != 0 {
		c.log("CLIPRDR: file contents request failed")
		return nil
	}

	if len(data) < 4 {
		return nil
	}

	streamID := binary.LittleEndian.Uint32(data[0:4])
	payload := data[4:]

	c.filesMu.Lock()
	defer c.filesMu.Unlock()

	if c.currentFileIndex >= len(c.files) {
		c.log("CLIPRDR: unexpected file contents response (no active transfer)")
		return nil
	}

	file := c.files[c.currentFileIndex]

	// Check if this is a size response (8 bytes)
	if len(payload) == 8 && file.Received == 0 {
		fileSize := binary.LittleEndian.Uint64(payload)
		c.log("CLIPRDR: file %d size confirmed: %d bytes", c.currentFileIndex, fileSize)

		// Update descriptor if needed
		file.Descriptor.FileSizeLow = uint32(fileSize)
		file.Descriptor.FileSizeHigh = uint32(fileSize >> 32)

		// Request first chunk
		c.currentStreamID++
		chunkSize := uint64(65536)
		if fileSize < chunkSize {
			chunkSize = fileSize
		}
		if fileSize > 0 {
			return c.requestFileRange(c.currentFileIndex, 0, chunkSize)
		}
		// Empty file - mark complete and move to next
		file.Complete = true
		return c.processNextFile()
	}

	// This is file data
	c.log("CLIPRDR: received %d bytes for file %d (streamID=%d)", len(payload), c.currentFileIndex, streamID)

	// Write to temp file
	if err := c.appendToFile(file, payload); err != nil {
		c.log("CLIPRDR: failed to write file data: %v", err)
		// Clean up state on write failure to allow retry
		c.closeFileHandle(file)
		c.fileTransferInProgress = false
		return err
	}

	file.Received += uint64(len(payload))
	totalSize := file.Descriptor.FileSize()

	// Check if file is complete
	if file.Received >= totalSize {
		c.closeFileHandle(file) // Close the file handle
		file.Complete = true
		c.log("CLIPRDR: file %d complete (%d bytes)", c.currentFileIndex, file.Received)
		return c.processNextFile()
	}

	// Request next chunk
	c.currentStreamID++
	remaining := totalSize - file.Received
	chunkSize := uint64(65536)
	if remaining < chunkSize {
		chunkSize = remaining
	}
	return c.requestFileRange(c.currentFileIndex, file.Received, chunkSize)
}

// appendToFile writes data to the file's temp path using a cached file handle.
func (c *ClipboardChannel) appendToFile(file *ClipboardFile, data []byte) error {
	// Open file handle if not already open
	if file.file == nil {
		f, err := os.OpenFile(file.TempPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		file.file = f
	}

	_, err := file.file.Write(data)
	return err
}

// closeFileHandle closes the file's cached handle.
func (c *ClipboardChannel) closeFileHandle(file *ClipboardFile) {
	if file.file != nil {
		file.file.Close()
		file.file = nil
	}
}

// processNextFile moves to the next file or completes the transfer.
func (c *ClipboardChannel) processNextFile() error {
	c.currentFileIndex++

	if c.currentFileIndex >= len(c.files) {
		// All files complete
		c.log("CLIPRDR: all %d files transferred", len(c.files))
		c.fileTransferInProgress = false
		if c.fileTransferCb != nil {
			// Call callback outside lock with panic recovery
			files := c.files
			cb := c.fileTransferCb
			go func() {
				defer func() {
					if r := recover(); r != nil {
						c.log("CLIPRDR: file transfer callback panicked: %v", r)
					}
				}()
				cb(files)
			}()
		}
		return nil
	}

	// Request next file's size
	c.currentStreamID++
	return c.requestFileSize(c.currentFileIndex)
}

// GetFiles returns the list of transferred files.
func (c *ClipboardChannel) GetFiles() []*ClipboardFile {
	c.filesMu.Lock()
	defer c.filesMu.Unlock()

	result := make([]*ClipboardFile, len(c.files))
	copy(result, c.files)
	return result
}

// CleanupFiles removes all temporary files.
func (c *ClipboardChannel) CleanupFiles() {
	c.filesMu.Lock()
	defer c.filesMu.Unlock()

	for _, f := range c.files {
		c.closeFileHandle(f) // Close any open handle
		if f.TempPath != "" {
			if err := os.Remove(f.TempPath); err != nil && !os.IsNotExist(err) {
				c.log("CLIPRDR: failed to remove temp file %s: %v", f.TempPath, err)
			}
		}
	}
	c.files = nil
}

// Common errors for clipboard operations.
var (
	ErrRandomTokenFailed = errors.New("clipboard: crypto/rand failed to generate token")
)

// Helper functions

func randomToken(n int) (string, error) {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// SECURITY: crypto/rand failures indicate a serious system issue
		return "", ErrRandomTokenFailed
	}
	for i := range b {
		b[i] = chars[b[i]%byte(len(chars))]
	}
	return string(b), nil
}

func sanitizeFileName(name string) string {
	// Remove path separators and null bytes
	result := make([]byte, 0, len(name))
	for _, c := range []byte(name) {
		if c == '/' || c == '\\' || c == 0 {
			continue
		}
		result = append(result, c)
	}
	if len(result) == 0 {
		return "file"
	}
	if len(result) > 200 {
		result = result[:200]
	}
	return string(result)
}
