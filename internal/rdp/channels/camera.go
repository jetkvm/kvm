package channels

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
)

// Camera implements the MS-RDPECAM (RDP Camera Redirection Virtual Channel Extension).
// This channel receives camera frames from the RDP client and forwards them to the UVC gadget.
//
// Protocol reference: https://learn.microsoft.com/en-us/openspecs/windows_protocols/ms-rdpecam/

// Device enumeration channel name (MS-RDPECAM 2.1).
// This is the control channel used for version negotiation and device notifications.
const CameraChannelName = "RDCamera_Device_Enumerator"

// Camera message types (MS-RDPECAM 2.2.1 SHARED_MSG_HEADER).
// The header is 2 bytes: Version (1 byte) + MessageId (1 byte).
const (
	CamMsgSuccessResponse         = 0x01
	CamMsgErrorResponse           = 0x02
	CamMsgSelectVersionRequest    = 0x03
	CamMsgSelectVersionResponse   = 0x04
	CamMsgDeviceAddedNotification = 0x05
	CamMsgDeviceRemovedNotif      = 0x06
	CamMsgActivateDeviceRequest   = 0x07
	CamMsgDeactivateDeviceRequest = 0x08
	CamMsgStreamListRequest       = 0x09
	CamMsgStreamListResponse      = 0x0A
	CamMsgMediaTypeListRequest    = 0x0B
	CamMsgMediaTypeListResponse   = 0x0C
	CamMsgCurrentMediaTypeRequest = 0x0D
	CamMsgCurrentMediaTypeResp    = 0x0E
	CamMsgStartStreamsRequest     = 0x0F
	CamMsgStopStreamsRequest      = 0x10
	CamMsgSampleRequest           = 0x11
	CamMsgSampleResponse          = 0x12
	CamMsgSampleErrorResponse     = 0x13
	CamMsgPropertyListRequest     = 0x14 // v2 only
	CamMsgPropertyListResponse    = 0x15 // v2 only
	CamMsgPropertyValueRequest    = 0x16 // v2 only
	CamMsgPropertyValueResponse   = 0x17 // v2 only
	CamMsgSetPropertyValueReq     = 0x18 // v2 only
)

// Camera protocol versions.
const (
	CamProtocolVersion1 = 0x01
	CamProtocolVersion2 = 0x02
)

// Header size is 2 bytes: Version (1) + MessageId (1).
const CamHeaderSize = 2

// Camera pixel formats (MS-RDPECAM 2.2.2.11 MEDIA_FORMAT_TYPE).
const (
	CamPixelFormatH264  = 0x01 // H.264 video
	CamPixelFormatMJPEG = 0x02 // Motion JPEG
	CamPixelFormatYUY2  = 0x03 // YUY2 (4:2:2)
	CamPixelFormatNV12  = 0x04 // NV12 (4:2:0)
	CamPixelFormatI420  = 0x05 // I420/YV12
	CamPixelFormatRGB24 = 0x06 // RGB 24bpp
	CamPixelFormatRGB32 = 0x07 // RGB 32bpp
)

// Default camera settings.
const (
	CamDefaultWidth     = 1280
	CamDefaultHeight    = 720
	CamDefaultFrameRate = 30
)

// Common errors.
var (
	ErrCamNotReady  = errors.New("camera: channel not ready")
	ErrCamNoCamera  = errors.New("camera: no camera available")
	ErrCamNotActive = errors.New("camera: not activated")
)

// CameraFrameCallback is called when a camera frame is received.
type CameraFrameCallback func(frame []byte, width, height uint32, pixelFormat uint32)

// CameraReadyCallback is called when the camera channel is ready.
type CameraReadyCallback func(c *CameraChannel)

// CameraInfo represents a client camera.
type CameraInfo struct {
	Name               string // Display name (UTF-16LE from DeviceAddedNotification)
	VirtualChannelName string // DVC channel name to open for streaming (ASCII from DeviceAddedNotification)
	Index              uint32
	Formats            []CameraFormat
}

// CameraFormat represents a supported camera format.
// All fields must be preserved exactly as received from the client - the client
// does exact matching on MEDIA_TYPE_DESCRIPTION in StartStreamsRequest.
type CameraFormat struct {
	Width               uint32
	Height              uint32
	FrameRate           uint32 // Computed frame rate for display only
	FrameRateNum        uint32 // Original numerator from client (must send back exactly)
	FrameRateDenom      uint32 // Original denominator from client (must send back exactly)
	PixelAspectRatioNum uint32 // Must preserve exactly
	PixelAspectRatioDen uint32 // Must preserve exactly
	Flags               uint8  // Must preserve exactly
	PixelFormat         uint32
}

// CameraLogFunc is a logging callback for camera channel events.
type CameraLogFunc func(msg string, args ...interface{})

// CameraChannel implements the MS-RDPECAM dynamic virtual channel.
type CameraChannel struct {
	channel *DVCChannel // Device enumeration channel
	manager *DVCManager

	// Callbacks
	onReady CameraReadyCallback
	onFrame CameraFrameCallback
	logger  CameraLogFunc

	// Camera state
	cameras        []CameraInfo
	selectedCamera int
	selectedFormat CameraFormat
	cameraMu       sync.RWMutex

	// Available formats from MediaTypeListResponse (for format selection)
	availableFormats []CameraFormat

	// Stream state
	isActive            atomic.Bool
	streamIndex         uint32
	frameCount          uint32      // For logging
	negotiatedVer       uint8       // Negotiated protocol version
	activeDeviceChannel *DVCChannel // Currently active channel for streaming commands
	pendingOp           uint8       // Tracks what operation we're waiting for response to

	// Channel tracking: we may have multiple channels (enumeration + per-device)
	// When OnChannelOpen is called, we need to know which channel opened
	pendingDeviceChannelName string // Set before creating device channel

	ready atomic.Bool
}

// NewCameraChannel creates a new camera channel.
func NewCameraChannel(manager *DVCManager) *CameraChannel {
	return &CameraChannel{
		manager:        manager,
		selectedCamera: -1,
		negotiatedVer:  CamProtocolVersion1,
	}
}

// SetReadyCallback sets the callback for when the channel is ready.
func (c *CameraChannel) SetReadyCallback(cb CameraReadyCallback) {
	c.onReady = cb
}

// SetFrameCallback sets the callback for receiving camera frames.
func (c *CameraChannel) SetFrameCallback(cb CameraFrameCallback) {
	c.onFrame = cb
}

// SetLogger sets the logging callback for debugging.
func (c *CameraChannel) SetLogger(logger CameraLogFunc) {
	c.logger = logger
}

// log writes a log message if logger is set.
func (c *CameraChannel) log(msg string, args ...interface{}) {
	if c.logger != nil {
		c.logger(msg, args...)
	}
}

// Open opens the camera device enumeration channel.
// Note: This only initiates the channel creation. The actual version negotiation
// starts when OnChannelOpen() is called after the client confirms the channel.
func (c *CameraChannel) Open() error {
	c.log("Opening camera channel: %s", CameraChannelName)
	ch, err := c.manager.CreateChannel(CameraChannelName, c)
	if err != nil {
		c.log("Failed to create camera channel: %v", err)
		return err
	}
	c.channel = ch
	c.log("Camera channel create request sent, waiting for client confirmation")
	// Don't send SelectVersionRequest here - wait for OnChannelOpen callback
	return nil
}

// OnChannelOpen is called when the DVC channel is successfully opened.
// This implements the DVCOpenHandler interface.
//
// This can be called for two different channels:
// 1. Device enumeration channel (RDCamera_Device_Enumerator) - first open
// 2. Device-specific channel (VirtualChannelName from DeviceAddedNotification) - after Activate()
func (c *CameraChannel) OnChannelOpen() {
	// Check if this is a device-specific channel (opened during Activate)
	if c.pendingDeviceChannelName != "" {
		c.log("Camera: device channel %q opened, sending ActivateDeviceRequest", c.pendingDeviceChannelName)
		c.pendingDeviceChannelName = "" // Clear the pending name

		// Per FreeRDP reference, ActivateDeviceRequest is just a header (no device name)
		// because the device is already identified by the channel
		if err := c.sendActivateDeviceRequestHeaderOnly(); err != nil {
			c.log("Camera: failed to send ActivateDeviceRequest: %v", err)
			return
		}

		c.pendingOp = CamMsgActivateDeviceRequest
		return
	}

	// This is the enumeration channel opening
	// NOTE: macOS RDP client errors when server initiates version negotiation.
	// The client expects to send SelectVersionRequest first.
	// Wait for client to initiate, or for DeviceAddedNotification.
	c.log("Camera: enumeration channel opened, waiting for client to initiate version negotiation")
	// Don't send SelectVersionRequest - let client initiate
}

// OnData handles incoming camera data from the DVC.
func (c *CameraChannel) OnData(data []byte) error {
	if len(data) < CamHeaderSize {
		return nil // Drop malformed messages silently
	}

	version := data[0]
	msgType := data[1]
	payload := data[CamHeaderSize:]

	// Hot path: no logging for SampleResponse (frame data)
	switch msgType {
	case CamMsgSelectVersionRequest:
		// Client is sending its own version request - treat as version negotiation
		return c.handleSelectVersionRequest(version, payload)
	case CamMsgSelectVersionResponse:
		return c.handleSelectVersionResponse(version, payload)
	case CamMsgDeviceAddedNotification:
		return c.handleDeviceAddedNotification(version, payload)
	case CamMsgDeviceRemovedNotif:
		return c.handleDeviceRemovedNotification(version, payload)
	case CamMsgSuccessResponse:
		return c.handleSuccessResponse(payload)
	case CamMsgErrorResponse:
		return c.handleErrorResponse(payload)
	case CamMsgStreamListResponse:
		return c.handleStreamListResponse(payload)
	case CamMsgMediaTypeListResponse:
		return c.handleMediaTypeListResponse(payload)
	case CamMsgCurrentMediaTypeResp:
		return c.handleCurrentMediaTypeResponse(payload)
	case CamMsgSampleResponse:
		return c.handleSampleResponse(payload)
	case CamMsgSampleErrorResponse:
		return c.handleSampleErrorResponse(payload)
	default:
		c.log("Camera: unknown message type 0x%02X", msgType)
	}

	return nil
}

// OnClose handles channel close.
func (c *CameraChannel) OnClose() {
	c.log("Camera: channel closed")
	c.ready.Store(false)
	c.isActive.Store(false)
}

// sendSelectVersionRequest sends version selection request to client.
// MS-RDPECAM 2.2.2.1: SelectVersionRequest
func (c *CameraChannel) sendSelectVersionRequest() error {
	// Header (2 bytes) + ClientVersion (1 byte)
	buf := make([]byte, 3)
	buf[0] = CamProtocolVersion1 // Version field in header
	buf[1] = CamMsgSelectVersionRequest
	buf[2] = CamProtocolVersion1 // ClientVersion - request version 1

	c.log("Camera: sending SelectVersionRequest (version=%d)", CamProtocolVersion1)
	return c.channel.SendData(buf)
}

// handleSelectVersionRequest handles when the client sends SelectVersionRequest.
// This can happen if the client wants to initiate version negotiation.
// MS-RDPECAM 2.2.2.1: SelectVersionRequest
func (c *CameraChannel) handleSelectVersionRequest(version uint8, payload []byte) error {
	clientVersion := version // Use header version if no payload
	if len(payload) >= 1 {
		clientVersion = payload[0]
	}

	oldVer := c.negotiatedVer
	c.log("Camera: received SelectVersionRequest from client: header=%d, payload=%v, clientVersion=%d, oldNegotiatedVer=%d",
		version, payload, clientVersion, oldVer)

	// Use the client's version (or minimum of ours and theirs)
	c.negotiatedVer = clientVersion
	if clientVersion > CamProtocolVersion2 {
		c.negotiatedVer = CamProtocolVersion2
	}

	c.log("Camera: negotiated version set to %d (was %d)", c.negotiatedVer, oldVer)

	// Respond with SelectVersionResponse
	return c.sendSelectVersionResponse()
}

// sendSelectVersionResponse sends version response to client.
func (c *CameraChannel) sendSelectVersionResponse() error {
	// Header (2 bytes) + ServerVersion (1 byte)
	buf := make([]byte, 3)
	buf[0] = c.negotiatedVer
	buf[1] = CamMsgSelectVersionResponse
	buf[2] = c.negotiatedVer // ServerVersion

	c.log("Camera: sending SelectVersionResponse (version=%d)", c.negotiatedVer)
	return c.channel.SendData(buf)
}

// handleSelectVersionResponse processes client version response.
// MS-RDPECAM 2.2.2.2: SelectVersionResponse
func (c *CameraChannel) handleSelectVersionResponse(version uint8, payload []byte) error {
	serverVersion := version // Use header version if no payload
	if len(payload) >= 1 {
		serverVersion = payload[0]
	}

	oldVer := c.negotiatedVer
	c.negotiatedVer = serverVersion
	c.log("Camera: version negotiated: %d -> %d (from header=%d payload=%v)", oldVer, serverVersion, version, payload)

	// Now we wait for DeviceAddedNotification messages from the client
	// The client will send these for each available camera
	return nil
}

// handleDeviceAddedNotification processes device added notification from client.
// MS-RDPECAM 2.2.2.3: DeviceAddedNotification
// Format: DeviceName (UTF-16LE null-terminated) + VirtualChannelName (ASCII null-terminated)
// The VirtualChannelName is the DVC channel name the server must open for streaming.
func (c *CameraChannel) handleDeviceAddedNotification(version uint8, payload []byte) error {
	// If we receive DeviceAddedNotification, the client is using version from the header
	// Update our negotiated version if not yet set (client may skip explicit negotiation)
	if version > 0 && c.negotiatedVer != version {
		c.log("Camera: DeviceAddedNotification header version=%d, updating negotiatedVer from %d", version, c.negotiatedVer)
		c.negotiatedVer = version
	}

	if len(payload) < 4 { // Minimum: 2 bytes DeviceName null + 2 bytes VirtualChannelName
		c.log("Camera: DeviceAddedNotification too short (%d bytes)", len(payload))
		return nil
	}

	// Parse DeviceName (UTF-16LE null-terminated)
	// Find the null terminator in UTF-16LE (two zero bytes)
	deviceNameEnd := -1
	for i := 0; i < len(payload)-1; i += 2 {
		if payload[i] == 0 && payload[i+1] == 0 {
			deviceNameEnd = i + 2 // Include the null terminator
			break
		}
	}
	if deviceNameEnd < 0 || deviceNameEnd >= len(payload) {
		c.log("Camera: DeviceAddedNotification: could not find DeviceName null terminator")
		return nil
	}

	deviceName := utf16ToStr(payload[:deviceNameEnd])

	// Parse VirtualChannelName (ASCII null-terminated) after DeviceName
	virtualChannelBytes := payload[deviceNameEnd:]
	virtualChannelName := ""
	for i, b := range virtualChannelBytes {
		if b == 0 {
			virtualChannelName = string(virtualChannelBytes[:i])
			break
		}
	}
	if virtualChannelName == "" && len(virtualChannelBytes) > 0 {
		// No null terminator found, use entire remaining bytes
		virtualChannelName = string(virtualChannelBytes)
	}

	c.log("Camera: DeviceAddedNotification: name=%q virtualChannel=%q", deviceName, virtualChannelName)

	c.cameraMu.Lock()
	camIndex := uint32(len(c.cameras))

	cam := CameraInfo{
		Name:               deviceName,
		VirtualChannelName: virtualChannelName,
		Index:              camIndex,
	}
	c.cameras = append(c.cameras, cam)

	// If this is the first camera, select it
	if c.selectedCamera < 0 {
		c.selectedCamera = 0
	}
	c.cameraMu.Unlock()

	c.log("Camera: device added [%d]: name=%q virtualChannel=%q", camIndex, deviceName, virtualChannelName)

	// Mark as ready when we have at least one camera
	if !c.ready.Load() {
		c.ready.Store(true)
		c.log("Camera: channel ready with %d camera(s)", len(c.cameras))
		if c.onReady != nil {
			c.onReady(c)
		}
	}

	return nil
}

// handleDeviceRemovedNotification processes device removed notification.
// MS-RDPECAM 2.2.2.4: DeviceRemovedNotification
func (c *CameraChannel) handleDeviceRemovedNotification(version uint8, payload []byte) error {
	deviceName := utf16ToStr(payload)
	c.log("Camera: device removed: %s", deviceName)

	c.cameraMu.Lock()
	defer c.cameraMu.Unlock()

	// Find and remove the camera
	for i, cam := range c.cameras {
		if cam.Name == deviceName {
			c.cameras = append(c.cameras[:i], c.cameras[i+1:]...)
			break
		}
	}

	// Reset selection if needed
	if len(c.cameras) == 0 {
		c.selectedCamera = -1
		c.ready.Store(false)
	} else if c.selectedCamera >= len(c.cameras) {
		c.selectedCamera = 0
	}

	return nil
}

// handleSuccessResponse processes success response.
// This is received after ActivateDeviceRequest or StartStreamsRequest succeeds.
func (c *CameraChannel) handleSuccessResponse(payload []byte) error {
	c.log("Camera: received SuccessResponse (pendingOp=0x%02X)", c.pendingOp)

	// Check what operation we were waiting for
	switch c.pendingOp {
	case CamMsgActivateDeviceRequest:
		c.pendingOp = 0
		c.log("Camera: device activated, sending StreamListRequest")
		return c.sendStreamListRequest()
	case CamMsgStartStreamsRequest:
		c.pendingOp = 0
		c.log("Camera: streaming started, sending first SampleRequest")
		return c.sendSampleRequest(uint8(c.streamIndex))
	}

	c.pendingOp = 0
	return nil
}

// handleErrorResponse processes error response.
func (c *CameraChannel) handleErrorResponse(payload []byte) error {
	errorCode := uint32(0)
	if len(payload) >= 4 {
		errorCode = binary.LittleEndian.Uint32(payload[0:4])
	}
	c.log("Camera: received ErrorResponse, code=%d", errorCode)
	return nil
}

// handleStreamListResponse processes stream list response.
// Per FreeRDP reference: STREAM_DESCRIPTION is 5 bytes each
// (FrameSourceTypes:2 + StreamCategory:1 + Selected:1 + CanBeShared:1)
func (c *CameraChannel) handleStreamListResponse(payload []byte) error {
	const streamDescSize = 5
	numStreams := len(payload) / streamDescSize

	c.log("Camera: StreamListResponse, numStreams=%d (payload=%d bytes)", numStreams, len(payload))

	// Log each stream description
	for i := 0; i < numStreams && i*streamDescSize+streamDescSize <= len(payload); i++ {
		offset := i * streamDescSize
		frameSourceTypes := binary.LittleEndian.Uint16(payload[offset : offset+2])
		streamCategory := payload[offset+2]
		selected := payload[offset+3]
		canBeShared := payload[offset+4]
		c.log("Camera: stream[%d] frameSourceTypes=0x%04X category=%d selected=%d canBeShared=%d",
			i, frameSourceTypes, streamCategory, selected, canBeShared)
	}

	// After getting stream list, request media types for stream 0
	if numStreams > 0 {
		return c.sendMediaTypeListRequest(0)
	}
	return nil
}

// handleMediaTypeListResponse processes media type list response.
// Per FreeRDP reference: MEDIA_TYPE_DESCRIPTION is 26 bytes each
// (Format:1 + Width:4 + Height:4 + FrameRateNum:4 + FrameRateDenom:4 +
// PixelAspectNum:4 + PixelAspectDenom:4 + Flags:1)
//
// IMPORTANT: The RV1106 SoC does NOT have hardware video decoding (VDEC).
// If the client sends H.264 and the USB host wants MJPEG, we cannot transcode.
// Therefore we MUST prefer MJPEG format when available during negotiation.
func (c *CameraChannel) handleMediaTypeListResponse(payload []byte) error {
	const mediaTypeSize = 26
	numMediaTypes := len(payload) / mediaTypeSize

	c.log("Camera: MediaTypeListResponse, numMediaTypes=%d (payload=%d bytes)", numMediaTypes, len(payload))

	// Parse and store all available formats
	c.cameraMu.Lock()
	c.availableFormats = make([]CameraFormat, 0, numMediaTypes)

	for i := 0; i < numMediaTypes && i*mediaTypeSize+mediaTypeSize <= len(payload); i++ {
		offset := i * mediaTypeSize
		format := payload[offset]
		width := binary.LittleEndian.Uint32(payload[offset+1 : offset+5])
		height := binary.LittleEndian.Uint32(payload[offset+5 : offset+9])
		frameRateNum := binary.LittleEndian.Uint32(payload[offset+9 : offset+13])
		frameRateDenom := binary.LittleEndian.Uint32(payload[offset+13 : offset+17])
		pixelAspectNum := binary.LittleEndian.Uint32(payload[offset+17 : offset+21])
		pixelAspectDenom := binary.LittleEndian.Uint32(payload[offset+21 : offset+25])
		flags := payload[offset+25]

		// Calculate frame rate for display
		frameRate := uint32(30)
		if frameRateDenom > 0 {
			frameRate = frameRateNum / frameRateDenom
		}

		f := CameraFormat{
			Width:               width,
			Height:              height,
			FrameRate:           frameRate,
			FrameRateNum:        frameRateNum,
			FrameRateDenom:      frameRateDenom,
			PixelAspectRatioNum: pixelAspectNum,
			PixelAspectRatioDen: pixelAspectDenom,
			Flags:               flags,
			PixelFormat:         uint32(format),
		}
		c.availableFormats = append(c.availableFormats, f)

		// Log formats for debugging
		if i < 10 {
			c.log("Camera: mediaType[%d] format=%s %dx%d@%d/%dfps",
				i, pixelFormatName(uint32(format)), width, height, frameRateNum, frameRateDenom)
		}
	}

	// Log ALL available formats at warn level for debugging
	hasMJPEG := false
	hasH264 := false
	c.log("Camera: ========== ALL AVAILABLE FORMATS ==========")
	for i, f := range c.availableFormats {
		c.log("Camera: FORMAT[%d]: %s %dx%d @ %d/%d fps (flags=0x%02X)",
			i, pixelFormatName(f.PixelFormat), f.Width, f.Height,
			f.FrameRateNum, f.FrameRateDenom, f.Flags)
		if f.PixelFormat == CamPixelFormatMJPEG {
			hasMJPEG = true
		}
		if f.PixelFormat == CamPixelFormatH264 {
			hasH264 = true
		}
	}
	c.log("Camera: ============================================")
	c.cameraMu.Unlock()

	c.log("Camera: SUMMARY: MJPEG=%v H264=%v total=%d formats", hasMJPEG, hasH264, len(c.availableFormats))

	// Request current media type for stream 0 (we'll override in handleCurrentMediaTypeResponse)
	return c.sendCurrentMediaTypeRequest(0)
}

// handleCurrentMediaTypeResponse processes current media type response.
// Per FreeRDP reference: Response is just MEDIA_TYPE_DESCRIPTION (26 bytes total)
// Format:1 + Width:4 + Height:4 + FrameRateNum:4 + FrameRateDenom:4 +
// PixelAspectNum:4 + PixelAspectDenom:4 + Flags:1
//
// IMPORTANT: This function may override the client's default format with MJPEG
// if available, since the RV1106 cannot decode H.264 (no VDEC hardware).
func (c *CameraChannel) handleCurrentMediaTypeResponse(payload []byte) error {
	if len(payload) < 26 {
		c.log("Camera: CurrentMediaTypeResponse too short (%d bytes, need 26)", len(payload))
		return nil
	}

	format := payload[0]
	width := binary.LittleEndian.Uint32(payload[1:5])
	height := binary.LittleEndian.Uint32(payload[5:9])
	frameRateNum := binary.LittleEndian.Uint32(payload[9:13])
	frameRateDenom := binary.LittleEndian.Uint32(payload[13:17])
	pixelAspectNum := binary.LittleEndian.Uint32(payload[17:21])
	pixelAspectDenom := binary.LittleEndian.Uint32(payload[21:25])
	flags := payload[25]

	// Calculate frame rate for display (handle division by zero)
	frameRate := uint32(30) // default
	if frameRateDenom > 0 {
		frameRate = frameRateNum / frameRateDenom
	}

	c.log("Camera: MEDIA_TYPE_DESCRIPTION raw: format=0x%02X width=%d height=%d frameRate=%d/%d pixelAspect=%d/%d flags=0x%02X",
		format, width, height, frameRateNum, frameRateDenom, pixelAspectNum, pixelAspectDenom, flags)

	c.cameraMu.Lock()

	// Start with the client's default format
	c.selectedFormat = CameraFormat{
		Width:               width,
		Height:              height,
		FrameRate:           frameRate,
		FrameRateNum:        frameRateNum,   // Preserve original - client requires exact match
		FrameRateDenom:      frameRateDenom, // Preserve original - client requires exact match
		PixelAspectRatioNum: pixelAspectNum, // Preserve original - client requires exact match
		PixelAspectRatioDen: pixelAspectDenom,
		Flags:               flags,
		PixelFormat:         uint32(format),
	}

	// Check if we should override with MJPEG format
	// The RV1106 has NO video decoder (VDEC), so if client defaults to H.264
	// and MJPEG is available, we MUST use MJPEG to avoid transcoding issues.
	if format == CamPixelFormatH264 {
		// Look for an MJPEG format with similar resolution
		mjpegFormat := c.findBestMJPEGFormat(width, height)
		if mjpegFormat != nil {
			c.log("Camera: OVERRIDING H.264 with MJPEG (RV1106 has no VDEC hardware)")
			c.log("Camera: selected MJPEG format: %dx%d@%d/%dfps",
				mjpegFormat.Width, mjpegFormat.Height, mjpegFormat.FrameRateNum, mjpegFormat.FrameRateDenom)
			c.selectedFormat = *mjpegFormat
		} else {
			c.log("Camera: WARNING - client only supports H.264, no MJPEG available")
			c.log("Camera: H.264 frames will require software decoding (may be slow)")
		}
	}

	c.streamIndex = 0 // We always request stream 0
	c.cameraMu.Unlock()

	c.log("Camera: selected format=%s %dx%d@%d/%dfps (=%dfps)",
		pixelFormatName(c.selectedFormat.PixelFormat),
		c.selectedFormat.Width, c.selectedFormat.Height,
		c.selectedFormat.FrameRateNum, c.selectedFormat.FrameRateDenom, c.selectedFormat.FrameRate)

	// Now start streaming for stream 0
	c.pendingOp = CamMsgStartStreamsRequest
	return c.sendStartStreamsRequest(0)
}

// findBestMJPEGFormat finds the best MJPEG format from available formats.
// Prefers formats with similar resolution to the target, then highest frame rate.
// Must be called with cameraMu held.
func (c *CameraChannel) findBestMJPEGFormat(targetWidth, targetHeight uint32) *CameraFormat {
	var best *CameraFormat
	var bestScore int64 = -1

	for i := range c.availableFormats {
		f := &c.availableFormats[i]
		if f.PixelFormat != CamPixelFormatMJPEG {
			continue
		}

		// Score based on resolution match and frame rate
		// Lower resolution difference = higher score
		// Higher frame rate = higher score (secondary)
		widthDiff := int64(f.Width) - int64(targetWidth)
		heightDiff := int64(f.Height) - int64(targetHeight)
		if widthDiff < 0 {
			widthDiff = -widthDiff
		}
		if heightDiff < 0 {
			heightDiff = -heightDiff
		}

		// Score: lower diff is better, frame rate breaks ties
		// Use negative diff so higher score = better match
		resScore := int64(10000000) - (widthDiff*1000 + heightDiff*1000)
		score := resScore + int64(f.FrameRate)

		if best == nil || score > bestScore {
			best = f
			bestScore = score
		}
	}

	return best
}

// handleSampleResponse processes camera frame data.
// MS-RDPECAM 2.2.4.2: SampleResponse
// HOT PATH: Called for every frame. Logging disabled for performance.
//
// NOTE: macOS Microsoft Remote Desktop client does NOT follow MS-RDPECAM spec exactly.
// It sends: StreamIndex(1) + raw H.264 data (no timestamp, no size).
// We detect H.264 by looking for NAL start codes after the first byte.
func (c *CameraChannel) handleSampleResponse(payload []byte) error {
	if len(payload) == 0 {
		return nil
	}

	// Check for H.264 NAL start code in the payload
	// macOS RDP client sends: StreamIndex(1) + raw H.264 data
	// So NAL start code (00 00 00 01 or 00 00 01) should be at position 1, not 0
	h264StartOffset := -1

	// Check at position 1 (after StreamIndex byte)
	if len(payload) >= 5 && payload[1] == 0x00 && payload[2] == 0x00 {
		if payload[3] == 0x00 && payload[4] == 0x01 {
			h264StartOffset = 1
		} else if payload[3] == 0x01 {
			h264StartOffset = 1
		}
	}

	// Check at position 0 (raw H.264 without StreamIndex)
	if h264StartOffset < 0 && len(payload) >= 4 && payload[0] == 0x00 && payload[1] == 0x00 {
		if payload[2] == 0x00 && payload[3] == 0x01 {
			h264StartOffset = 0
		} else if payload[2] == 0x01 {
			h264StartOffset = 0
		}
	}

	if h264StartOffset >= 0 {
		// macOS client sends H.264 frames with minimal header (just StreamIndex)
		frameData := payload[h264StartOffset:]
		c.frameCount++

		if c.onFrame != nil {
			c.cameraMu.RLock()
			fmt := c.selectedFormat
			c.cameraMu.RUnlock()
			c.onFrame(frameData, fmt.Width, fmt.Height, CamPixelFormatH264)
		}

		// Request next sample if still active
		if c.isActive.Load() {
			return c.sendSampleRequest(0)
		}
		return nil
	}

	// Standard MS-RDPECAM SampleResponse parsing
	var headerSize, sampleSizeOffset int
	if c.negotiatedVer >= CamProtocolVersion2 {
		headerSize = 13 // StreamIndex(1) + Timestamp(8) + SampleSize(4)
		sampleSizeOffset = 9
	} else {
		headerSize = 5 // StreamIndex(1) + SampleSize(4)
		sampleSizeOffset = 1
	}

	if len(payload) < headerSize {
		return nil
	}

	streamIndex := payload[0]
	sampleSize := binary.LittleEndian.Uint32(payload[sampleSizeOffset : sampleSizeOffset+4])

	if len(payload) < headerSize+int(sampleSize) {
		return nil
	}

	frameData := payload[headerSize : headerSize+int(sampleSize)]
	c.frameCount++

	if c.onFrame != nil && len(frameData) > 0 {
		c.cameraMu.RLock()
		fmt := c.selectedFormat
		c.cameraMu.RUnlock()
		c.onFrame(frameData, fmt.Width, fmt.Height, fmt.PixelFormat)
	}

	// Request next sample if still active
	if c.isActive.Load() {
		return c.sendSampleRequest(streamIndex)
	}

	return nil
}

// handleSampleErrorResponse processes sample error response.
func (c *CameraChannel) handleSampleErrorResponse(payload []byte) error {
	errorCode := uint32(0)
	if len(payload) >= 5 {
		// StreamIndex(1) + ErrorCode(4)
		errorCode = binary.LittleEndian.Uint32(payload[1:5])
	}
	c.log("Camera: SampleErrorResponse, errorCode=%d", errorCode)
	return nil
}

// sendStreamListRequest requests the list of streams for a device.
func (c *CameraChannel) sendStreamListRequest() error {
	ch := c.activeDeviceChannel
	if ch == nil {
		ch = c.channel
	}
	if ch == nil {
		return ErrCamNotReady
	}
	// Header (2 bytes) only
	buf := make([]byte, 2)
	buf[0] = c.negotiatedVer
	buf[1] = CamMsgStreamListRequest

	c.log("Camera: sending StreamListRequest")
	return ch.SendData(buf)
}

// sendMediaTypeListRequest requests the list of media types for a stream.
func (c *CameraChannel) sendMediaTypeListRequest(streamIndex uint8) error {
	ch := c.activeDeviceChannel
	if ch == nil {
		ch = c.channel // Fallback to enumeration channel (shouldn't happen)
	}
	// Header (2 bytes) + StreamIndex (1 byte)
	buf := make([]byte, 3)
	buf[0] = c.negotiatedVer
	buf[1] = CamMsgMediaTypeListRequest
	buf[2] = streamIndex

	c.log("Camera: sending MediaTypeListRequest for stream %d", streamIndex)
	return ch.SendData(buf)
}

// sendCurrentMediaTypeRequest requests the current media type for a stream.
func (c *CameraChannel) sendCurrentMediaTypeRequest(streamIndex uint8) error {
	ch := c.activeDeviceChannel
	if ch == nil {
		ch = c.channel
	}
	// Header (2 bytes) + StreamIndex (1 byte)
	buf := make([]byte, 3)
	buf[0] = c.negotiatedVer
	buf[1] = CamMsgCurrentMediaTypeRequest
	buf[2] = streamIndex

	c.log("Camera: sending CurrentMediaTypeRequest for stream %d", streamIndex)
	return ch.SendData(buf)
}

// sendStartStreamsRequest starts streaming from specified streams.
// Per FreeRDP reference, the format is:
// Header (2 bytes) + N * START_STREAM_INFO (27 bytes each)
// where START_STREAM_INFO = StreamIndex(1) + MEDIA_TYPE_DESCRIPTION(26)
func (c *CameraChannel) sendStartStreamsRequest(streamIndex uint8) error {
	ch := c.activeDeviceChannel
	if ch == nil {
		ch = c.channel
	}

	c.cameraMu.RLock()
	fmt := c.selectedFormat
	c.cameraMu.RUnlock()

	// Header (2 bytes) + 1 stream info (27 bytes)
	buf := make([]byte, 2+27)
	buf[0] = c.negotiatedVer
	buf[1] = CamMsgStartStreamsRequest

	// START_STREAM_INFO for stream 0
	offset := 2
	buf[offset] = streamIndex // StreamIndex
	offset++

	// MEDIA_TYPE_DESCRIPTION (26 bytes) - must match EXACTLY what client reported
	buf[offset] = uint8(fmt.PixelFormat) // Format
	offset++
	binary.LittleEndian.PutUint32(buf[offset:offset+4], fmt.Width)
	offset += 4
	binary.LittleEndian.PutUint32(buf[offset:offset+4], fmt.Height)
	offset += 4
	binary.LittleEndian.PutUint32(buf[offset:offset+4], fmt.FrameRateNum) // Must use original value
	offset += 4
	binary.LittleEndian.PutUint32(buf[offset:offset+4], fmt.FrameRateDenom) // Must use original value
	offset += 4
	binary.LittleEndian.PutUint32(buf[offset:offset+4], fmt.PixelAspectRatioNum) // Must use original value
	offset += 4
	binary.LittleEndian.PutUint32(buf[offset:offset+4], fmt.PixelAspectRatioDen) // Must use original value
	offset += 4
	buf[offset] = fmt.Flags // Must use original value

	c.streamIndex = uint32(streamIndex)
	c.isActive.Store(true)

	c.log("Camera: sending StartStreamsRequest for stream %d (format=%s %dx%d@%d/%dfps)",
		streamIndex, pixelFormatName(fmt.PixelFormat), fmt.Width, fmt.Height, fmt.FrameRateNum, fmt.FrameRateDenom)
	return ch.SendData(buf)
}

// sendStopStreamsRequest stops streaming.
func (c *CameraChannel) sendStopStreamsRequest() error {
	ch := c.activeDeviceChannel
	if ch == nil {
		ch = c.channel
	}
	// Header (2 bytes) + NumStreams (1 byte) + StreamIndex (1 byte)
	buf := make([]byte, 4)
	buf[0] = c.negotiatedVer
	buf[1] = CamMsgStopStreamsRequest
	buf[2] = 1                        // NumStreams
	buf[3] = uint8(c.streamIndex)     // StreamIndex

	c.log("Camera: sending StopStreamsRequest for stream %d", c.streamIndex)
	return ch.SendData(buf)
}

// sendSampleRequest requests the next camera frame.
func (c *CameraChannel) sendSampleRequest(streamIndex uint8) error {
	ch := c.activeDeviceChannel
	if ch == nil {
		ch = c.channel
	}
	// Header (2 bytes) + StreamIndex (1 byte)
	buf := make([]byte, 3)
	buf[0] = c.negotiatedVer
	buf[1] = CamMsgSampleRequest
	buf[2] = streamIndex

	return ch.SendData(buf)
}

// Activate activates the camera and starts streaming.
// Per MS-RDPECAM (and FreeRDP reference), the flow is:
// 1. Open a DVC channel using the VirtualChannelName from DeviceAddedNotification
// 2. Send ActivateDeviceRequest (header only) on that device channel
// 3. All streaming commands go through the device channel
func (c *CameraChannel) Activate() error {
	if !c.ready.Load() {
		return ErrCamNotReady
	}

	c.cameraMu.RLock()
	cameraIndex := c.selectedCamera
	if cameraIndex < 0 || cameraIndex >= len(c.cameras) {
		c.cameraMu.RUnlock()
		return ErrCamNoCamera
	}
	cam := c.cameras[cameraIndex]
	c.cameraMu.RUnlock()

	c.log("Camera: activating camera %d: name=%q virtualChannel=%q", cameraIndex, cam.Name, cam.VirtualChannelName)

	if cam.VirtualChannelName == "" {
		c.log("Camera: ERROR - no VirtualChannelName in DeviceAddedNotification")
		return ErrCamNoCamera
	}

	// Open a separate DVC channel for this specific device
	// This channel is used for all streaming commands
	c.log("Camera: opening device DVC channel: %s", cam.VirtualChannelName)

	// Set pending channel name so OnChannelOpen knows this is a device channel
	c.pendingDeviceChannelName = cam.VirtualChannelName

	deviceCh, err := c.manager.CreateChannel(cam.VirtualChannelName, c)
	if err != nil {
		c.pendingDeviceChannelName = "" // Clear on error
		c.log("Camera: failed to create device channel %q: %v", cam.VirtualChannelName, err)
		return err
	}

	c.activeDeviceChannel = deviceCh
	c.log("Camera: device channel create request sent, waiting for channel open confirmation")

	// Don't send ActivateDeviceRequest yet - wait for OnChannelOpen callback
	// which will be called when the DVC channel is confirmed open by the client
	return nil
}

// sendActivateDeviceRequestHeaderOnly sends ActivateDeviceRequest with just the header.
// Per FreeRDP reference, when using device-specific channels, the device is already
// identified by the channel, so no device name is needed.
func (c *CameraChannel) sendActivateDeviceRequestHeaderOnly() error {
	ch := c.activeDeviceChannel
	if ch == nil {
		return ErrCamNotReady
	}

	// Header only (2 bytes) - no device name needed on device-specific channel
	buf := make([]byte, 2)
	buf[0] = c.negotiatedVer
	buf[1] = CamMsgActivateDeviceRequest

	c.log("Camera: sending ActivateDeviceRequest (header only)")
	return ch.SendData(buf)
}

// Deactivate stops camera streaming.
func (c *CameraChannel) Deactivate() error {
	if !c.isActive.Load() {
		return nil
	}

	c.isActive.Store(false)
	return c.sendStopStreamsRequest()
}

// ActivateWithFormat activates the camera with a specific pixel format.
// Note: In MS-RDPECAM, the format is negotiated via MediaTypeList, not directly requested.
// This method will activate and use whatever format the camera/client provides.
func (c *CameraChannel) ActivateWithFormat(format uint32) error {
	c.log("Camera: ActivateWithFormat called with format %s (note: format negotiated by client)",
		pixelFormatName(format))
	return c.Activate()
}

// pixelFormatName returns the human-readable name for a pixel format.
func pixelFormatName(format uint32) string {
	switch format {
	case CamPixelFormatH264:
		return "H264"
	case CamPixelFormatMJPEG:
		return "MJPEG"
	case CamPixelFormatYUY2:
		return "YUY2"
	case CamPixelFormatNV12:
		return "NV12"
	case CamPixelFormatI420:
		return "I420"
	case CamPixelFormatRGB24:
		return "RGB24"
	case CamPixelFormatRGB32:
		return "RGB32"
	default:
		return fmt.Sprintf("Unknown(0x%02X)", format)
	}
}

// IsReady returns true if the channel is ready.
func (c *CameraChannel) IsReady() bool {
	return c.ready.Load()
}

// IsActive returns true if camera streaming is active.
func (c *CameraChannel) IsActive() bool {
	return c.isActive.Load()
}

// GetCameras returns the list of available cameras.
func (c *CameraChannel) GetCameras() []CameraInfo {
	c.cameraMu.RLock()
	defer c.cameraMu.RUnlock()
	result := make([]CameraInfo, len(c.cameras))
	copy(result, c.cameras)
	return result
}

// SelectCamera selects a camera by index.
func (c *CameraChannel) SelectCamera(index int) error {
	c.cameraMu.Lock()
	defer c.cameraMu.Unlock()

	if index < 0 || index >= len(c.cameras) {
		return ErrCamNoCamera
	}

	c.selectedCamera = index
	return nil
}

// GetSelectedFormat returns the current camera format.
func (c *CameraChannel) GetSelectedFormat() CameraFormat {
	c.cameraMu.RLock()
	defer c.cameraMu.RUnlock()
	return c.selectedFormat
}

// Close closes the camera channel.
func (c *CameraChannel) Close() error {
	_ = c.Deactivate() // Ignore error on close
	c.ready.Store(false)

	if c.channel != nil {
		return c.channel.Close()
	}
	return nil
}

// utf16ToStr converts UTF-16LE bytes to a Go string.
func utf16ToStr(data []byte) string {
	if len(data) < 2 {
		return ""
	}

	// Simple UTF-16LE to ASCII conversion (for basic ASCII names)
	result := make([]byte, 0, len(data)/2)
	for i := 0; i < len(data)-1; i += 2 {
		ch := uint16(data[i]) | uint16(data[i+1])<<8
		if ch == 0 {
			break
		}
		if ch < 128 {
			result = append(result, byte(ch))
		}
	}
	return string(result)
}
