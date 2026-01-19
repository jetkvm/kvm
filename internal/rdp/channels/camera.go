package channels

import (
	"encoding/binary"
	"errors"
	"sync"
	"sync/atomic"
)

// Camera implements the MS-RDPECAM (RDP Camera Redirection Virtual Channel Extension).
// This channel receives camera frames from the RDP client and forwards them to the UVC gadget.

// Camera channel name.
const CameraChannelName = "RDPCAM"

// Camera message types (MS-RDPECAM 2.2.1).
const (
	CamMsgSelectVersion = 0x01
	CamMsgSelectList    = 0x02
	CamMsgActivate      = 0x03
	CamMsgDeactivate    = 0x04
	CamMsgSample        = 0x05
	CamMsgSuccess       = 0x06
	CamMsgError         = 0x07
	CamMsgMediaType     = 0x08
	CamMsgProperty      = 0x09
	CamMsgPropertyValue = 0x0A
	CamMsgStreamOpen    = 0x0B
	CamMsgStreamReady   = 0x0C
	CamMsgStreamClose   = 0x0D
	CamMsgSampleRequest = 0x0E
	CamMsgSampleResp    = 0x0F
	CamMsgError2        = 0x10
)

// Camera version.
const CamVersion = 0x00010000 // Version 1.0

// Camera sizes.
const (
	CamHeaderSize    = 4  // msgType(1) + version(1) + reserved(2)
	CamSelectVersion = 8  // msgType(4) + version(4)
	CamActivateSize  = 24 // msgType(4) + ... (variable)
	CamSampleHdrSize = 20 // msgType(4) + streamIndex(4) + sampleIndex(4) + timestamp(8)
)

// Camera pixel formats.
const (
	CamPixelFormatNV12  = 0x3231564E // 'NV12'
	CamPixelFormatI420  = 0x30323449 // 'I420'
	CamPixelFormatYUY2  = 0x32595559 // 'YUY2'
	CamPixelFormatMJPEG = 0x47504A4D // 'MJPG'
	CamPixelFormatH264  = 0x34363248 // 'H264'
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
	Name     string
	DeviceID string
	Index    uint32
	Formats  []CameraFormat
}

// CameraFormat represents a supported camera format.
type CameraFormat struct {
	Width       uint32
	Height      uint32
	FrameRate   uint32
	PixelFormat uint32
}

// CameraChannel implements the MS-RDPECAM dynamic virtual channel.
type CameraChannel struct {
	channel *DVCChannel
	manager *DVCManager

	// Callbacks
	onReady CameraReadyCallback
	onFrame CameraFrameCallback

	// Camera state
	cameras        []CameraInfo
	selectedCamera int
	selectedFormat CameraFormat
	cameraMu       sync.RWMutex

	// Stream state
	isActive    atomic.Bool
	streamIndex uint32

	ready atomic.Bool
}

// NewCameraChannel creates a new camera channel.
func NewCameraChannel(manager *DVCManager) *CameraChannel {
	return &CameraChannel{
		manager:        manager,
		selectedCamera: -1,
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

// Open opens the camera channel.
func (c *CameraChannel) Open() error {
	ch, err := c.manager.CreateChannel(CameraChannelName, c)
	if err != nil {
		return err
	}
	c.channel = ch

	// Send version selection
	return c.sendSelectVersion()
}

// OnData handles incoming camera data from the DVC.
func (c *CameraChannel) OnData(data []byte) error {
	if len(data) < 4 {
		return nil
	}

	msgType := binary.LittleEndian.Uint32(data[0:4])

	switch msgType {
	case CamMsgSelectVersion:
		return c.handleSelectVersion(data[4:])
	case CamMsgSelectList:
		return c.handleSelectList(data[4:])
	case CamMsgSuccess:
		return c.handleSuccess(data[4:])
	case CamMsgError, CamMsgError2:
		return c.handleError(data[4:])
	case CamMsgMediaType:
		return c.handleMediaType(data[4:])
	case CamMsgStreamReady:
		return c.handleStreamReady(data[4:])
	case CamMsgSampleResp:
		return c.handleSampleResp(data[4:])
	}

	return nil
}

// OnClose handles channel close.
func (c *CameraChannel) OnClose() {
	c.ready.Store(false)
	c.isActive.Store(false)
}

// sendSelectVersion sends version selection to client.
func (c *CameraChannel) sendSelectVersion() error {
	buf := make([]byte, 8)
	binary.LittleEndian.PutUint32(buf[0:4], CamMsgSelectVersion)
	binary.LittleEndian.PutUint32(buf[4:8], CamVersion)
	return c.channel.SendData(buf)
}

// handleSelectVersion processes client version response.
func (c *CameraChannel) handleSelectVersion(data []byte) error {
	if len(data) < 4 {
		return nil
	}

	// clientVersion := binary.LittleEndian.Uint32(data[0:4])
	// Accept any version

	// Request camera list
	return c.sendSelectList()
}

// sendSelectList requests the list of available cameras.
func (c *CameraChannel) sendSelectList() error {
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf[0:4], CamMsgSelectList)
	return c.channel.SendData(buf)
}

// handleSelectList processes the list of available cameras.
func (c *CameraChannel) handleSelectList(data []byte) error {
	if len(data) < 4 {
		return nil
	}

	numCameras := binary.LittleEndian.Uint32(data[0:4])
	pos := 4

	c.cameraMu.Lock()
	defer c.cameraMu.Unlock()

	c.cameras = make([]CameraInfo, 0, numCameras)

	// Parse camera info (simplified - actual format is more complex)
	for i := uint32(0); i < numCameras && pos < len(data); i++ {
		cam := CameraInfo{
			Index: i,
		}

		// Read device name (null-terminated UTF-16LE)
		nameStart := pos
		for pos < len(data)-1 {
			if data[pos] == 0 && data[pos+1] == 0 {
				break
			}
			pos += 2
		}
		if nameStart < pos {
			cam.Name = utf16ToStr(data[nameStart:pos])
		}
		pos += 2 // Skip null terminator

		c.cameras = append(c.cameras, cam)
	}

	if len(c.cameras) == 0 {
		return ErrCamNoCamera
	}

	// Select first camera by default
	c.selectedCamera = 0
	c.ready.Store(true)

	// Notify ready
	if c.onReady != nil {
		c.onReady(c)
	}

	return nil
}

// handleSuccess processes success response.
func (c *CameraChannel) handleSuccess(_ []byte) error {
	// Success message - operation completed
	return nil
}

// handleError processes error response.
func (c *CameraChannel) handleError(_ []byte) error {
	// Error - log and continue
	return nil
}

// handleMediaType processes media type information.
func (c *CameraChannel) handleMediaType(data []byte) error {
	if len(data) < 16 {
		return nil
	}

	// Parse media type
	// streamIndex := binary.LittleEndian.Uint32(data[0:4])
	pixelFormat := binary.LittleEndian.Uint32(data[4:8])
	width := binary.LittleEndian.Uint32(data[8:12])
	height := binary.LittleEndian.Uint32(data[12:16])

	c.cameraMu.Lock()
	c.selectedFormat = CameraFormat{
		Width:       width,
		Height:      height,
		PixelFormat: pixelFormat,
		FrameRate:   CamDefaultFrameRate,
	}
	c.cameraMu.Unlock()

	return nil
}

// handleStreamReady processes stream ready notification.
func (c *CameraChannel) handleStreamReady(_ []byte) error {
	c.isActive.Store(true)

	// Start requesting samples
	return c.sendSampleRequest()
}

// handleSampleResp processes camera frame data.
func (c *CameraChannel) handleSampleResp(data []byte) error {
	if len(data) < 16 {
		return nil
	}

	// streamIndex := binary.LittleEndian.Uint32(data[0:4])
	// sampleIndex := binary.LittleEndian.Uint32(data[4:8])
	// timestamp := binary.LittleEndian.Uint64(data[8:16])
	frameData := data[16:]

	if c.onFrame != nil && len(frameData) > 0 {
		c.cameraMu.RLock()
		fmt := c.selectedFormat
		c.cameraMu.RUnlock()

		c.onFrame(frameData, fmt.Width, fmt.Height, fmt.PixelFormat)
	}

	// Request next sample if still active
	if c.isActive.Load() {
		return c.sendSampleRequest()
	}

	return nil
}

// Activate activates the camera and starts streaming.
func (c *CameraChannel) Activate() error {
	if !c.ready.Load() {
		return ErrCamNotReady
	}

	c.cameraMu.RLock()
	cameraIndex := c.selectedCamera
	c.cameraMu.RUnlock()

	if cameraIndex < 0 {
		return ErrCamNoCamera
	}

	return c.sendActivate(uint32(cameraIndex))
}

// sendActivate sends camera activation request.
func (c *CameraChannel) sendActivate(cameraIndex uint32) error {
	// Simplified activate message
	// Full format includes media type selection
	buf := make([]byte, 24)
	binary.LittleEndian.PutUint32(buf[0:4], CamMsgActivate)
	binary.LittleEndian.PutUint32(buf[4:8], cameraIndex)
	binary.LittleEndian.PutUint32(buf[8:12], CamDefaultWidth)
	binary.LittleEndian.PutUint32(buf[12:16], CamDefaultHeight)
	binary.LittleEndian.PutUint32(buf[16:20], CamDefaultFrameRate)
	binary.LittleEndian.PutUint32(buf[20:24], CamPixelFormatNV12)

	return c.channel.SendData(buf)
}

// sendSampleRequest requests the next camera frame.
func (c *CameraChannel) sendSampleRequest() error {
	buf := make([]byte, 8)
	binary.LittleEndian.PutUint32(buf[0:4], CamMsgSampleRequest)
	binary.LittleEndian.PutUint32(buf[4:8], c.streamIndex)
	return c.channel.SendData(buf)
}

// Deactivate stops camera streaming.
func (c *CameraChannel) Deactivate() error {
	if !c.isActive.Load() {
		return nil
	}

	c.isActive.Store(false)
	return c.sendDeactivate()
}

// sendDeactivate sends camera deactivation request.
func (c *CameraChannel) sendDeactivate() error {
	buf := make([]byte, 8)
	binary.LittleEndian.PutUint32(buf[0:4], CamMsgDeactivate)
	binary.LittleEndian.PutUint32(buf[4:8], c.streamIndex)
	return c.channel.SendData(buf)
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
