package camera

const cameraLogInterval = 30 // Log every N frames

// HandleCameraFrame handles a camera frame received from the browser.
// This is called by the WebRTC DataChannel handler in the kvm package.
// Browser captures camera frames, encodes to H.264, and sends them here.
// We pass the H.264 frames directly to the UVC streamer (no transcoding).
func (m *Manager) HandleCameraFrame(data []byte, isString bool) {
	// Only process if camera passthrough is enabled AND UVC source is set to camera
	if !m.enabled.Load() || !m.source.IsCamera() {
		return
	}

	// Expect binary H.264 NAL unit data
	if isString {
		if m.camLog != nil {
			m.camLog.Warn().Msg("Received unexpected string data on camera channel")
		}
		return
	}

	if len(data) < 4 {
		return // Too small to be an H.264 NAL unit
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
			Int("h264Size", len(data)).
			Msg("Camera H.264 passthrough stats")
	}

	// Pass H.264 directly to UVC streamer (no transcoding needed!)
	m.HandleCameraH264Frame(data)
}

// OnChannelOpen should be called when the camera DataChannel opens.
func (m *Manager) OnChannelOpen(channelID uint16) {
	if m.camLog != nil {
		m.camLog.Info().Uint16("id", channelID).Msg("Camera DataChannel opened (H.264 mode)")
	}
}

// OnChannelClose should be called when the camera DataChannel closes.
func (m *Manager) OnChannelClose() {
	if m.camLog != nil {
		m.camLog.Info().Msg("Camera DataChannel closed")
	}
}
