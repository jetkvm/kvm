package kvm

import (
	"io"
	"sync/atomic"

	"github.com/jetkvm/kvm/internal/camera"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v4"
)

var (
	cameraManager     *camera.Manager
	currentVideoTrack atomic.Pointer[string] // Track ID of current camera video track
)

// initCameraManager initializes the camera manager.
// Must be called after gadget is initialized.
func initCameraManager() {
	cameraManager = camera.NewManager(camera.Config{
		UVCLogger:    uvcLog,
		CameraLogger: cameraLog,
		Gadget:       gadget,
	})
}

// setCameraEnabled enables or disables camera passthrough.
func setCameraEnabled(enabled bool) {
	if cameraManager != nil {
		cameraManager.SetEnabled(enabled)
	}
}

// isCameraEnabled returns whether camera passthrough is enabled.
func isCameraEnabled() bool {
	if cameraManager != nil {
		return cameraManager.IsEnabled()
	}
	return false
}

// setUVCSource sets the video source for UVC output.
func setUVCSource(source camera.Source) {
	if cameraManager != nil {
		cameraManager.SetSource(source)
	}
}

// getUVCSource returns the current UVC video source.
func getUVCSource() camera.Source {
	if cameraManager != nil {
		return cameraManager.GetSource()
	}
	return camera.SourceHDMI
}

// handleCameraVideoTrack handles incoming H.264 video from the browser camera.
// This is called when the browser sends camera video over the WebRTC video track.
// We depacketize the H.264 RTP packets and pass NAL units directly to UVC.
func handleCameraVideoTrack(track *webrtc.TrackRemote) {
	if cameraManager == nil {
		cameraLog.Warn().Msg("Camera manager not initialized, ignoring video track")
		return
	}

	trackID := track.ID()
	currentVideoTrack.Store(&trackID)

	cameraLog.Debug().
		Str("track_id", trackID).
		Str("codec", track.Codec().MimeType).
		Msg("Camera video track started")

	// H.264 depacketizer to reassemble NAL units from RTP packets
	depacketizer := &codecs.H264Packet{}

	// Buffer to accumulate NAL units into complete frames
	// We'll send complete access units (frames) to UVC
	var frameBuffer []byte
	var lastTimestamp uint32

	for {
		// Check if we've been superseded by another track
		currentTrackID := currentVideoTrack.Load()
		if currentTrackID != nil && *currentTrackID != trackID {
			cameraLog.Debug().
				Str("track_id", trackID).
				Str("current_track_id", *currentTrackID).
				Msg("Camera video track handler exiting - superseded")
			return
		}

		// Read RTP packet
		rtpPacket, _, err := track.ReadRTP()
		if err != nil {
			if err == io.EOF {
				cameraLog.Debug().Str("track_id", trackID).Msg("Camera video track ended")
				return
			}
			cameraLog.Warn().Err(err).Msg("Failed to read RTP packet from camera track")
			continue
		}

		// Skip if camera passthrough is disabled or source is not camera
		if !cameraManager.IsEnabled() || !cameraManager.IsSourceCamera() {
			continue
		}

		// Depacketize H.264 NAL unit from RTP
		nalUnit, err := depacketizer.Unmarshal(rtpPacket.Payload)
		if err != nil {
			cameraLog.Trace().Err(err).Msg("Failed to depacketize H.264")
			continue
		}

		if len(nalUnit) == 0 {
			continue
		}

		// Accumulate NAL units for the same timestamp (same frame)
		if rtpPacket.Timestamp != lastTimestamp && len(frameBuffer) > 0 {
			// New frame started, send the previous frame to UVC
			cameraManager.HandleCameraH264Frame(frameBuffer)
			frameBuffer = nil
		}
		lastTimestamp = rtpPacket.Timestamp

		// Add Annex B start code and NAL unit to frame buffer
		frameBuffer = appendNALU(frameBuffer, nalUnit)

		// If this is the last packet of the frame (marker bit set), send immediately
		if rtpPacket.Marker {
			cameraManager.HandleCameraH264Frame(frameBuffer)
			frameBuffer = nil
		}
	}
}

// appendNALU appends a NAL unit with Annex B start code to the buffer
func appendNALU(buf []byte, nalu []byte) []byte {
	// Use 4-byte start code for first NALU, 3-byte for subsequent
	if len(buf) == 0 {
		buf = append(buf, 0x00, 0x00, 0x00, 0x01)
	} else {
		buf = append(buf, 0x00, 0x00, 0x01)
	}
	return append(buf, nalu...)
}
