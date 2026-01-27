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

	// Count available format types and find native (largest) H.264 resolution
	// macOS RDP client ignores format selection and always sends native resolution
	hasMJPEG := false
	hasH264 := false
	var largestH264 *CameraFormat
	for i := range c.availableFormats {
		f := &c.availableFormats[i]
		if f.PixelFormat == CamPixelFormatMJPEG {
			hasMJPEG = true
		}
		if f.PixelFormat == CamPixelFormatH264 {
			hasH264 = true
			// Track largest H.264 resolution (by pixel count)
			if largestH264 == nil || (f.Width*f.Height > largestH264.Width*largestH264.Height) {
				largestH264 = f
			}
		}
	}
	// Store native H.264 format for use when receiving H.264 frames
	if largestH264 != nil {
		c.nativeH264Format = *largestH264
		c.log("Camera: native H.264 format: %dx%d@%dfps", largestH264.Width, largestH264.Height, largestH264.FrameRate)
	}
	c.cameraMu.Unlock()

	c.log("Camera: %d formats available (MJPEG=%v H264=%v)", len(c.availableFormats), hasMJPEG, hasH264)

	// Request current media type for stream 0 (we'll override in handleCurrentMediaTypeResponse)
	return c.sendCurrentMediaTypeRequest(0)
}

// handleCurrentMediaTypeResponse processes current media type response.
// Per FreeRDP reference: Response is just MEDIA_TYPE_DESCRIPTION (26 bytes total).
//
// IMPORTANT: This function may override the client's default format with a more
// suitable one for the RV1106, which has NO video decoder (VDEC) hardware.
//
// Format preference order (best to worst for RV1106):
// 1. MJPEG - Can pass through directly to UVC gadget
// 2. NV12/I420/YUY2 - Raw YUV, only needs HW MJPEG encoding (VENC available)
// 3. H.264 - Requires software decoding (BETA, CPU-intensive)
func (c *CameraChannel) handleCurrentMediaTypeResponse(payload []byte) error {
	f := parseMediaTypeDescription(payload)
	if f == nil {
		c.log("Camera: CurrentMediaTypeResponse too short (%d bytes, need %d)", len(payload), mediaTypeDescriptionSize)
		return nil
	}

	c.cameraMu.Lock()

	// Use host's requested format as target (from USB host via UVC gadget)
	// Fall back to client's default if host hasn't set a preference
	targetWidth := c.hostRequestedFormat.Width
	targetHeight := c.hostRequestedFormat.Height
	targetFPS := c.hostRequestedFormat.FrameRate
	if targetWidth == 0 || targetHeight == 0 {
		targetWidth = f.Width
		targetHeight = f.Height
	}
	if targetFPS == 0 {
		targetFPS = f.FrameRate
	}

	// Start with the client's default format
	c.selectedFormat = *f

	// Check if we should try to find a better format match for any format type
	// This handles the case where the client defaults to e.g. MJPEG 1080p@60 but host wants 480p@30
	if f.Width != targetWidth || f.Height != targetHeight || f.FrameRate != targetFPS {
		betterFormat := c.findBestFormatByPixelType(targetWidth, targetHeight, targetFPS, f.PixelFormat)
		if betterFormat != nil && (betterFormat.Width != f.Width || betterFormat.Height != f.Height || betterFormat.FrameRate != f.FrameRate) {
			c.selectedFormat = *betterFormat
		}
	}

	// The RV1106 has NO video decoder (VDEC), so we prefer formats that don't
	// require H.264 decoding. Try to find better formats in order of preference.
	if f.PixelFormat == CamPixelFormatH264 {
		// Priority 1: MJPEG - Can pass through directly
		if mjpegFormat := c.findBestMJPEGFormat(targetWidth, targetHeight, targetFPS); mjpegFormat != nil {
			c.selectedFormat = *mjpegFormat
		} else if yuvFormat := c.findBestRawYUVFormat(targetWidth, targetHeight, targetFPS); yuvFormat != nil {
			// Priority 2: Raw YUV - Only needs HW MJPEG encoding
			c.selectedFormat = *yuvFormat
		} else {
			// Priority 3: H.264 - Requires software decoding (BETA)
			if h264Format := c.findBestFormatByPixelType(targetWidth, targetHeight, targetFPS, CamPixelFormatH264); h264Format != nil {
				c.selectedFormat = *h264Format
			}
			c.log("Camera: H.264 only - will use SW decode (BETA)")
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
// Prefers formats matching target resolution and FPS.
// Must be called with cameraMu held.
func (c *CameraChannel) findBestMJPEGFormat(targetWidth, targetHeight, targetFPS uint32) *CameraFormat {
	return c.findBestFormatByPixelType(targetWidth, targetHeight, targetFPS, CamPixelFormatMJPEG)
}

// findBestRawYUVFormat finds the best raw YUV format from available formats.
// Prefers NV12 (native for RV1106 VENC), then I420, then YUY2.
// Must be called with cameraMu held.
func (c *CameraChannel) findBestRawYUVFormat(targetWidth, targetHeight, targetFPS uint32) *CameraFormat {
	// Priority: NV12 > I420 > YUY2
	// NV12 is native for RV1106 VENC, I420 needs simple reformat, YUY2 is 4:2:2
	yuvFormats := []uint32{CamPixelFormatNV12, CamPixelFormatI420, CamPixelFormatYUY2}

	for _, pixelFmt := range yuvFormats {
		if f := c.findBestFormatByPixelType(targetWidth, targetHeight, targetFPS, pixelFmt); f != nil {
			return f
		}
	}
	return nil
}

// findBestFormatByPixelType finds the best format of a specific pixel type.
// Prefers formats matching target resolution and FPS (>= target FPS preferred).
// Must be called with cameraMu held.
func (c *CameraChannel) findBestFormatByPixelType(targetWidth, targetHeight, targetFPS, pixelFormat uint32) *CameraFormat {
	var best *CameraFormat
	var bestScore int64 = -1

	for i := range c.availableFormats {
		f := &c.availableFormats[i]
		if f.PixelFormat != pixelFormat {
			continue
		}

		// Score based on resolution match (primary) and FPS match (secondary)
		widthDiff := int64(f.Width) - int64(targetWidth)
		heightDiff := int64(f.Height) - int64(targetHeight)
		if widthDiff < 0 {
			widthDiff = -widthDiff
		}
		if heightDiff < 0 {
			heightDiff = -heightDiff
		}

		// FPS scoring: prefer formats that meet or exceed target FPS
		// Formats below target get a penalty, formats at/above get a bonus
		var fpsScore int64
		if f.FrameRate >= targetFPS {
			// At or above target: small bonus, closer to target is better
			fpsDiff := int64(f.FrameRate) - int64(targetFPS)
			fpsScore = 1000 - fpsDiff // Slight preference for exact match over higher
		} else {
			// Below target: penalty proportional to how far below
			fpsDiff := int64(targetFPS) - int64(f.FrameRate)
			fpsScore = -fpsDiff * 10 // Penalize being below target
		}

		// Score: resolution match is most important, then FPS
		resScore := int64(10000000) - (widthDiff*1000 + heightDiff*1000)
		score := resScore + fpsScore

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
			// Use nativeH264Format dimensions - macOS ignores format selection and
			// always sends native camera resolution regardless of what we requested.
			// Note: Unlocked read is intentional for performance in hot path.
			// Worst case is one frame with slightly stale dimensions during format change.
			w, h := c.nativeH264Format.Width, c.nativeH264Format.Height
			if w == 0 || h == 0 {
				// Fallback to activeFormat if native not set
				w, h = c.activeFormat.Width, c.activeFormat.Height
			}
			if w == 0 || h == 0 {
				// Final fallback to defaults if still zero
				w, h = CamDefaultWidth, CamDefaultHeight
			}
			c.onFrame(frameData, w, h, CamPixelFormatH264)
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
