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

	// Native H.264 format - the largest H.264 resolution the client advertises.
	// macOS RDP client ignores format selection and always sends native resolution,
	// so we use this for H.264 frame dimensions rather than selectedFormat.
	nativeH264Format CameraFormat

	// Host requested format (from USB host via UVC gadget)
	// Used as target resolution when selecting formats from RDP client
	hostRequestedFormat CameraFormat

	// Stream state
	isActive            atomic.Bool
	streamIndex         uint32
	frameCount          uint32      // For logging
	negotiatedVer       uint8       // Negotiated protocol version
	activeDeviceChannel *DVCChannel // Currently active channel for streaming commands
	pendingOp           uint8       // Tracks what operation we're waiting for response to

	// Cached format for hot path (set when streaming starts, avoids lock per frame)
	activeFormat CameraFormat

	// Channel tracking: we may have multiple channels (enumeration + per-device)
	// When OnChannelOpen is called, we need to know which channel opened
	pendingDeviceChannelName string // Set before creating device channel

	// Pre-allocated buffers for zero-allocation hot path
	sampleReqBuf [3]byte // Header(2) + StreamIndex(1) for sendSampleRequest

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

// handleSelectVersionRequest handles when the client sends SelectVersionRequest.
// This can happen if the client wants to initiate version negotiation.
// MS-RDPECAM 2.2.2.1: SelectVersionRequest
// sendStreamListRequest requests the list of streams for a device.
func (c *CameraChannel) sendStreamListRequest() error {
	ch := c.getActiveChannel()
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
	ch := c.getActiveChannel()
	if ch == nil {
		return ErrCamNotReady
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
	ch := c.getActiveChannel()
	if ch == nil {
		return ErrCamNotReady
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
	ch := c.getActiveChannel()
	if ch == nil {
		return ErrCamNotReady
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
	ch := c.getActiveChannel()
	if ch == nil {
		return ErrCamNotReady
	}
	// Header (2 bytes) + NumStreams (1 byte) + StreamIndex (1 byte)
	buf := make([]byte, 4)
	buf[0] = c.negotiatedVer
	buf[1] = CamMsgStopStreamsRequest
	buf[2] = 1                    // NumStreams
	buf[3] = uint8(c.streamIndex) // StreamIndex

	c.log("Camera: sending StopStreamsRequest for stream %d", c.streamIndex)
	return ch.SendData(buf)
}

// sendSampleRequest requests the next camera frame.
// HOT PATH: Called for every frame, uses pre-allocated buffer.
func (c *CameraChannel) sendSampleRequest(streamIndex uint8) error {
	ch := c.getActiveChannel()
	if ch == nil {
		return ErrCamNotReady
	}
	// Use pre-allocated buffer for zero allocations in hot path
	c.sampleReqBuf[0] = c.negotiatedVer
	c.sampleReqBuf[1] = CamMsgSampleRequest
	c.sampleReqBuf[2] = streamIndex

	return ch.SendData(c.sampleReqBuf[:])
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

	// Reset cached formats for clean state on next activation
	c.cameraMu.Lock()
	c.nativeH264Format = CameraFormat{}
	c.activeFormat = CameraFormat{}
	c.cameraMu.Unlock()

	return c.sendStopStreamsRequest()
}

// ActivateWithFormat activates the camera with a specific pixel format and resolution.
// The host's requested format (from USB host via UVC gadget) is used as the target
// when selecting formats from the RDP client's available formats.
// Note: In MS-RDPECAM, the format is negotiated via MediaTypeList, not directly requested.
func (c *CameraChannel) ActivateWithFormat(format uint32, width, height, fps int) error {
	c.cameraMu.Lock()
	c.hostRequestedFormat = CameraFormat{
		PixelFormat:    format,
		Width:          uint32(width),
		Height:         uint32(height),
		FrameRate:      uint32(fps),
		FrameRateNum:   uint32(fps),
		FrameRateDenom: 1,
	}
	c.cameraMu.Unlock()

	c.log("Camera: ActivateWithFormat called - host wants %s %dx%d@%dfps",
		pixelFormatName(format), width, height, fps)
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
