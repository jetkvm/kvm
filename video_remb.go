package kvm

import (
	"math"
	"sync"

	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v4"
)

// The hardware encoder is shared. Honour the lowest estimate among receivers
// and remove a receiver's limit when its sender closes.
var videoREMB = struct {
	sync.Mutex
	estimates map[*webrtc.RTPSender]uint32
}{estimates: make(map[*webrtc.RTPSender]uint32)}

func updateVideoREMB(sender *webrtc.RTPSender, bitrate uint32) {
	videoREMB.Lock()
	defer videoREMB.Unlock()
	if bitrate == 0 {
		delete(videoREMB.estimates, sender)
	} else {
		videoREMB.estimates[sender] = bitrate
	}
	var limit uint32
	for _, estimate := range videoREMB.estimates {
		if limit == 0 || estimate < limit {
			limit = estimate
		}
	}
	if err := nativeInstance.VideoSetREMB(limit); err != nil {
		webrtcLogger.Warn().Err(err).Msg("failed to update receiver bitrate limit")
	}
}

func receiverVideoBitrate(packet *rtcp.ReceiverEstimatedMaximumBitrate, ssrc uint32) uint32 {
	bitrate := float64(packet.Bitrate)
	if math.IsNaN(bitrate) || math.IsInf(bitrate, 0) || bitrate < 0 {
		return 0
	}
	for _, source := range packet.SSRCs {
		if source == ssrc {
			// Zero is a valid receiver estimate; reserve zero internally for reset.
			return uint32(max(1, min(bitrate, math.MaxUint32)))
		}
	}
	return 0
}

func readVideoRTCP(sender *webrtc.RTPSender) {
	defer updateVideoREMB(sender, 0)
	for {
		packets, _, err := sender.ReadRTCP()
		if err != nil {
			return
		}
		params := sender.GetParameters()
		for _, packet := range packets {
			remb, ok := packet.(*rtcp.ReceiverEstimatedMaximumBitrate)
			if !ok {
				continue
			}
			for _, encoding := range params.Encodings {
				if bitrate := receiverVideoBitrate(remb, uint32(encoding.SSRC)); bitrate != 0 {
					updateVideoREMB(sender, bitrate)
					break
				}
			}
		}
	}
}
