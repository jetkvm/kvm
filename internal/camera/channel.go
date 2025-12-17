package camera

const cameraLogInterval = 30 // Log every N frames

// HandleCameraFrame handles a camera frame received from the browser.
// This is called by the WebRTC DataChannel handler in the kvm package.
// Browser captures camera frames, encodes to JPEG, and sends them here.
// We simply pass the JPEG frames directly to the UVC streamer (no transcoding).
func (m *Manager) HandleCameraFrame(data []byte, isString bool) {
	// Only process if camera passthrough is enabled AND UVC source is set to camera
	if !m.enabled.Load() || !m.source.IsCamera() {
		return
	}

	// Expect binary JPEG data
	if isString {
		if m.camLog != nil {
			m.camLog.Warn().Msg("Received unexpected string data on camera channel")
		}
		return
	}

	if len(data) < 2 {
		return // Too small to be a JPEG
	}

	// Quick JPEG header check (FFD8)
	if data[0] != 0xFF || data[1] != 0xD8 {
		if m.camLog != nil {
			m.camLog.Debug().Msg("Received non-JPEG data on camera channel")
		}
		return
	}

	// Update frame stats
	m.frameMu.Lock()
	m.frameCount++
	shouldLog := m.frameCount-m.lastLogFrame >= cameraLogInterval
	if shouldLog {
		m.lastLogFrame = m.frameCount
	}
	frameCount := m.frameCount
	m.frameMu.Unlock()

	if shouldLog && m.camLog != nil {
		m.camLog.Info().
			Int("frames", frameCount).
			Int("jpegSize", len(data)).
			Msg("Camera passthrough stats")
	}

	// Pass JPEG directly to UVC streamer (no transcoding needed!)
	// This is the same path used by HDMI loopback
	m.HandleMjpegFrame(data)
}

// OnChannelOpen should be called when the camera DataChannel opens.
func (m *Manager) OnChannelOpen(channelID uint16) {
	if m.camLog != nil {
		m.camLog.Info().Uint16("id", channelID).Msg("Camera DataChannel opened")
	}
}

// OnChannelClose should be called when the camera DataChannel closes.
func (m *Manager) OnChannelClose() {
	if m.camLog != nil {
		m.camLog.Info().Msg("Camera DataChannel closed")
	}
}
