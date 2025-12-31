package kvm

import (
	"io"
	"sync/atomic"

	"github.com/jetkvm/kvm/internal/camera"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v4"
)

var (
	cameraManagerPtr  atomic.Pointer[camera.Manager]
	currentVideoTrack atomic.Pointer[string] // Track ID of current camera video track
)

// initCameraManager initializes the camera manager.
// Must be called after gadget is initialized.
func initCameraManager() {
	mgr, err := camera.NewManager(camera.Config{
		UVCLogger:    uvcLog,
		CameraLogger: cameraLog,
		Gadget:       gadget,
		OnPanic: func(panicValue interface{}) {
			// Log panic for alerting - the event loop has already logged details
			cameraLog.Error().
				Interface("panic", panicValue).
				Msg("UVC event loop crashed - camera passthrough unavailable until reinit")
		},
	})
	if err != nil {
		cameraLog.Error().Err(err).Msg("Failed to create camera manager")
		return
	}
	cameraManagerPtr.Store(mgr)
}

// setCameraEnabled enables or disables camera passthrough.
func setCameraEnabled(enabled bool) {
	mgr := cameraManagerPtr.Load()
	if mgr == nil {
		cameraLog.Debug().Bool("enabled", enabled).Msg("setCameraEnabled ignored: manager not initialized")
		return
	}
	mgr.SetEnabled(enabled)
}

// isCameraEnabled returns whether camera passthrough is enabled.
func isCameraEnabled() bool {
	if mgr := cameraManagerPtr.Load(); mgr != nil {
		return mgr.IsEnabled()
	}
	return false
}

// depacketizeLogInterval controls periodic logging of depacketization errors.
const depacketizeLogInterval = 100

// h264FrameBufferSize is the pre-allocated buffer for H.264 frame accumulation.
// 1080p I-frames are typically 50-150KB; P-frames are much smaller.
const h264FrameBufferSize = 256 * 1024

// handleCameraVideoTrack handles incoming H.264 video from the browser camera.
// This is called when the browser sends camera video over the WebRTC video track.
// We depacketize the H.264 RTP packets and pass NAL units directly to UVC.
func handleCameraVideoTrack(track *webrtc.TrackRemote) {
	mgr := cameraManagerPtr.Load()
	if mgr == nil {
		cameraLog.Warn().Msg("Camera manager not initialized, ignoring video track")
		return
	}

	trackID := track.ID()
	currentVideoTrack.Store(&trackID)

	cameraLog.Debug().
		Str("track_id", trackID).
		Str("codec", track.Codec().MimeType).
		Msg("Camera video track started")

	depacketizer := &codecs.H264Packet{}
	frameBuffer := make([]byte, 0, h264FrameBufferSize)
	var lastTimestamp uint32
	var depacketErrors uint32

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

		rtpPacket, _, err := track.ReadRTP()
		if err != nil {
			if err == io.EOF {
				cameraLog.Debug().Str("track_id", trackID).Msg("Camera video track ended")
				return
			}
			cameraLog.Warn().Err(err).Msg("Failed to read RTP packet from camera track")
			continue
		}

		if !mgr.IsEnabled() {
			continue
		}

		// Depacketize H.264 NAL unit from RTP
		nalUnit, err := depacketizer.Unmarshal(rtpPacket.Payload)
		if err != nil {
			depacketErrors++
			if depacketErrors == 1 || depacketErrors%depacketizeLogInterval == 0 {
				cameraLog.Warn().Uint32("total_errors", depacketErrors).Err(err).Msg("H.264 depacketization failed")
			}
			continue
		}

		if len(nalUnit) == 0 {
			continue
		}

		// Accumulate NAL units for the same timestamp (same frame)
		if rtpPacket.Timestamp != lastTimestamp && len(frameBuffer) > 0 {
			// New frame started, send the previous frame to UVC
			mgr.HandleCameraH264Frame(frameBuffer)
			frameBuffer = frameBuffer[:0]
		}
		lastTimestamp = rtpPacket.Timestamp

		// Add Annex B start code and NAL unit to frame buffer
		frameBuffer = appendNALU(frameBuffer, nalUnit)

		// If this is the last packet of the frame (marker bit set), send immediately
		if rtpPacket.Marker {
			mgr.HandleCameraH264Frame(frameBuffer)
			frameBuffer = frameBuffer[:0]
		}
	}
}

// appendNALU appends a NAL unit with Annex B start code to the buffer.
// Uses 4-byte start code (00 00 00 01) for first NALU for decoder sync,
// 3-byte (00 00 01) for subsequent NALUs to save bandwidth per H.264 spec.
func appendNALU(buf []byte, nalu []byte) []byte {
	if len(buf) == 0 {
		buf = append(buf, 0x00, 0x00, 0x00, 0x01)
	} else {
		buf = append(buf, 0x00, 0x00, 0x01)
	}
	return append(buf, nalu...)
}
