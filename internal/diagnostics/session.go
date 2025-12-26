package diagnostics

// LogSessionInfo logs WebRTC session diagnostics.
func (d *Diagnostics) LogSessionInfo() {
	d.logger.Info().Msg("--- Session Info ---")

	if d.options.GetSessionInfo == nil {
		d.logger.Warn().Msg("session diagnostics not available")
		return
	}

	info := d.options.GetSessionInfo()
	d.logger.Info().
		Int("active_sessions", info.ActiveSessions).
		Bool("has_current_session", info.HasCurrentSession).
		Msg("WebRTC sessions")

	// Log WebRTC connection states if a session is active
	if info.HasCurrentSession {
		d.logger.Info().
			Str("ice_connection_state", info.ICEConnectionState).
			Str("signaling_state", info.SignalingState).
			Str("connection_state", info.ConnectionState).
			Msg("WebRTC peer connection")

		// Log data channels
		for _, dc := range info.DataChannels {
			d.logger.Info().
				Str("label", dc.Label).
				Str("state", dc.State).
				Msg("WebRTC data channel")
		}
	}
}
