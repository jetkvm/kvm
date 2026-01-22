package channels

import (
	"encoding/binary"
	"fmt"
)

// Camera channel message handlers.
// This file contains functions that parse and handle incoming camera messages.

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
func (c *CameraChannel) handleDeviceRemovedNotification(_ uint8, payload []byte) error {
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
func (c *CameraChannel) handleSuccessResponse(_ []byte) error {
	c.log("Camera: received SuccessResponse (pendingOp=0x%02X)", c.pendingOp)

	// Check what operation we were waiting for
	switch c.pendingOp {
	case CamMsgActivateDeviceRequest:
		c.pendingOp = 0
		c.log("Camera: device activated, sending StreamListRequest")
		return c.sendStreamListRequest()
	case CamMsgStartStreamsRequest:
		c.pendingOp = 0
		// Cache format for hot path (avoids lock per frame)
		c.cameraMu.RLock()
		c.activeFormat = c.selectedFormat
		c.cameraMu.RUnlock()
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
	c.log("Camera: ERROR - client returned error code %d", errorCode)
	return fmt.Errorf("camera: client error code %d", errorCode)
}

// mediaTypeDescriptionSize is the size of MEDIA_TYPE_DESCRIPTION in bytes.
const mediaTypeDescriptionSize = 26

// parseMediaTypeDescription parses a MEDIA_TYPE_DESCRIPTION structure from the given buffer.
// Returns nil if the buffer is too short.
func parseMediaTypeDescription(data []byte) *CameraFormat {
	if len(data) < mediaTypeDescriptionSize {
		return nil
	}

	frameRateNum := binary.LittleEndian.Uint32(data[9:13])
	frameRateDenom := binary.LittleEndian.Uint32(data[13:17])

	// Calculate display frame rate (handle division by zero)
	frameRate := uint32(30)
	if frameRateDenom > 0 {
		frameRate = frameRateNum / frameRateDenom
	}

	return &CameraFormat{
		PixelFormat:         uint32(data[0]),
		Width:               binary.LittleEndian.Uint32(data[1:5]),
		Height:              binary.LittleEndian.Uint32(data[5:9]),
		FrameRateNum:        frameRateNum,
		FrameRateDenom:      frameRateDenom,
		FrameRate:           frameRate,
		PixelAspectRatioNum: binary.LittleEndian.Uint32(data[17:21]),
		PixelAspectRatioDen: binary.LittleEndian.Uint32(data[21:25]),
		Flags:               data[25],
	}
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
// Per FreeRDP reference: MEDIA_TYPE_DESCRIPTION is 26 bytes each.
//
// IMPORTANT: The RV1106 SoC does NOT have hardware video decoding (VDEC).
// If the client sends H.264 and the USB host wants MJPEG, we cannot transcode.
// Therefore we MUST prefer MJPEG format when available during negotiation.
func (c *CameraChannel) handleMediaTypeListResponse(payload []byte) error {
	numMediaTypes := len(payload) / mediaTypeDescriptionSize

	c.log("Camera: MediaTypeListResponse, numMediaTypes=%d (payload=%d bytes)", numMediaTypes, len(payload))

	// Parse and store all available formats
	c.cameraMu.Lock()
	c.availableFormats = make([]CameraFormat, 0, numMediaTypes)

	for i := 0; i < numMediaTypes; i++ {
		offset := i * mediaTypeDescriptionSize
		f := parseMediaTypeDescription(payload[offset:])
		if f == nil {
			break
		}
		c.availableFormats = append(c.availableFormats, *f)

		// Log formats for debugging
		if i < 10 {
			c.log("Camera: mediaType[%d] format=%s %dx%d@%d/%dfps",
				i, pixelFormatName(f.PixelFormat), f.Width, f.Height, f.FrameRateNum, f.FrameRateDenom)
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
// Per FreeRDP reference: Response is just MEDIA_TYPE_DESCRIPTION (26 bytes total).
//
// IMPORTANT: This function may override the client's default format with MJPEG
// if available, since the RV1106 cannot decode H.264 (no VDEC hardware).
func (c *CameraChannel) handleCurrentMediaTypeResponse(payload []byte) error {
	f := parseMediaTypeDescription(payload)
	if f == nil {
		c.log("Camera: CurrentMediaTypeResponse too short (%d bytes, need %d)", len(payload), mediaTypeDescriptionSize)
		return nil
	}

	c.log("Camera: MEDIA_TYPE_DESCRIPTION raw: format=0x%02X width=%d height=%d frameRate=%d/%d pixelAspect=%d/%d flags=0x%02X",
		f.PixelFormat, f.Width, f.Height, f.FrameRateNum, f.FrameRateDenom, f.PixelAspectRatioNum, f.PixelAspectRatioDen, f.Flags)

	c.cameraMu.Lock()

	// Start with the client's default format
	c.selectedFormat = *f

	// Check if we should override with MJPEG format
	// The RV1106 has NO video decoder (VDEC), so if client defaults to H.264
	// and MJPEG is available, we MUST use MJPEG to avoid transcoding issues.
	if f.PixelFormat == CamPixelFormatH264 {
		// Look for an MJPEG format with similar resolution
		mjpegFormat := c.findBestMJPEGFormat(f.Width, f.Height)
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

// hasNALStartCode checks if the buffer at the given offset contains an H.264 NAL start code.
// NAL start codes are either 00 00 00 01 (4-byte) or 00 00 01 (3-byte).
func hasNALStartCode(data []byte, offset int) bool {
	if offset+3 > len(data) {
		return false
	}
	if data[offset] != 0x00 || data[offset+1] != 0x00 {
		return false
	}
	// Check for 3-byte start code (00 00 01)
	if data[offset+2] == 0x01 {
		return true
	}
	// Check for 4-byte start code (00 00 00 01)
	return offset+4 <= len(data) && data[offset+2] == 0x00 && data[offset+3] == 0x01
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
	// So NAL start code should be at position 1 (after StreamIndex) or 0 (raw H.264)
	h264StartOffset := -1
	if hasNALStartCode(payload, 1) {
		h264StartOffset = 1
	} else if hasNALStartCode(payload, 0) {
		h264StartOffset = 0
	}

	if h264StartOffset >= 0 {
		// macOS client sends H.264 frames with minimal header (just StreamIndex)
		frameData := payload[h264StartOffset:]
		c.frameCount++

		if c.onFrame != nil {
			// Use cached activeFormat (set when streaming started) to avoid lock per frame
			c.onFrame(frameData, c.activeFormat.Width, c.activeFormat.Height, CamPixelFormatH264)
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
		// Use cached activeFormat (set when streaming started) to avoid lock per frame
		c.onFrame(frameData, c.activeFormat.Width, c.activeFormat.Height, c.activeFormat.PixelFormat)
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
	c.log("Camera: ERROR - sample error from client, code=%d", errorCode)
	c.isActive.Store(false) // Stop streaming on error
	return fmt.Errorf("camera: sample error code %d", errorCode)
}

// getActiveChannel returns the channel to use for sending commands.
// It prefers the device-specific channel if available, falling back to the enumeration channel.
func (c *CameraChannel) getActiveChannel() *DVCChannel {
	if c.activeDeviceChannel != nil {
		return c.activeDeviceChannel
	}
	return c.channel
}
