package kvm

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/jetkvm/kvm/internal/osservices"
	"github.com/jetkvm/kvm/internal/osservices/edid"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
)

const (
	// guestFrameCapacity is the largest access unit of service video.
	guestFrameCapacity = 4 * 1024 * 1024
	// guestFrameQueue frames wait for the WebRTC track at most; while it is
	// full, the session holds the USB read.
	guestFrameQueue = 8
)

type guestFrame struct {
	buf      *[]byte
	length   int
	pts90k   uint32
	keyframe bool
	codec    osservices.Codec
	fps      uint32
}

// guestVideo shows service video: it presents the KVM display through the
// EDID, keeps the hardware capture off meanwhile, and writes the access
// units to the browser's video track.
type guestVideo struct {
	s *osServicesService

	// owned: service video owns the display, from DisplayPresent until
	// DisplayRestore completed. presented: its EDID is on the target.
	owned     atomic.Bool
	presented atomic.Bool
	edid      atomic.Value // string

	// Display changes run in order on one goroutine, outside the session
	// lock.
	ops chan func()

	frames   chan guestFrame
	reserved atomic.Int32
	slot     *[]byte
	pool     sync.Pool

	// The browser's keyframe request: a generation the session takes, and
	// whether delta frames wait for a keyframe.
	keyMu        sync.Mutex
	keyframeGen  uint32
	nextGen      uint32
	needKeyframe atomic.Bool
}

func (v *guestVideo) init(s *osServicesService) {
	v.s = s
	v.edid.Store("")
	v.ops = make(chan func(), 32)
	v.frames = make(chan guestFrame, guestFrameQueue)
	v.pool.New = func() any {
		b := make([]byte, 0, 256*1024)
		return &b
	}
	go func() {
		for op := range v.ops {
			op()
		}
	}()
	go v.writer()
}

// tick reports the KVM display to the session; called with the session lock
// held.
func (v *guestVideo) tick(session *osservices.Session) {
	session.DisplayStatus(v.presented.Load(), v.owned.Load())
	session.DisplayHeld(false)
}

func (v *guestVideo) wantKeyframe() {
	v.keyMu.Lock()
	defer v.keyMu.Unlock()
	v.needKeyframe.Store(true)
	if v.keyframeGen == 0 {
		v.nextGen++
		if v.nextGen == 0 {
			v.nextGen = 1
		}
		v.keyframeGen = v.nextGen
	}
}

// --- VideoPlatform (called with the session lock held) ---------------------------

func (s *osServicesService) DisplayPresent(width, height, serial uint32) bool {
	if !edid.Supported(int(width), int(height)) {
		return false
	}
	data, err := edid.Build(int(width), int(height), serial)
	if err != nil {
		return false
	}
	v := &s.video
	v.presented.Store(false)
	v.owned.Store(true)
	v.edid.Store(edid.Hex(data))
	v.ops <- func() {
		stopNativeVideo()
		if err := reapplyHostDisplayAdvertisement("os_services_present"); err != nil {
			osServicesLogger.Warn().Err(err).Msg("cannot present the KVM display")
			v.edid.Store("")
			v.owned.Store(false)
			return
		}
		osServicesLogger.Info().Uint32("width", width).Uint32("height", height).Msg("KVM display presented for service video")
		v.presented.Store(true)
	}
	return true
}

func (s *osServicesService) DisplayRestore() {
	v := &s.video
	v.edid.Store("")
	v.ops <- func() {
		if err := reapplyHostDisplayAdvertisement("os_services_restore"); err != nil {
			osServicesLogger.Warn().Err(err).Msg("cannot restore the hardware capture's EDID")
		}
		v.presented.Store(false)
		v.owned.Store(false)
		if session := currentSession; session != nil && getActiveSessions() > 0 {
			startNativeVideoForSession(session)
		}
		osServicesLogger.Info().Msg("hardware capture restored")
		s.StateChanged()
	}
}

func (s *osServicesService) VideoStream(width, height, fps uint32, codec osservices.Codec) int {
	osServicesLogger.Info().Uint32("width", width).Uint32("height", height).Uint32("fps", fps).
		Str("codec", codec.String()).Msg("service video stream")
	return guestFrameCapacity
}

func (s *osServicesService) VideoStopped() {
	s.video.wantKeyframe()
}

func (s *osServicesService) FrameReserve(length int) ([]byte, bool) {
	v := &s.video
	session := currentSession
	if session == nil || session.VideoTrack == nil {
		return nil, false
	}
	if int(v.reserved.Load())+len(v.frames) >= guestFrameQueue {
		return nil, true
	}
	buf := v.pool.Get().(*[]byte)
	if cap(*buf) < length {
		b := make([]byte, length)
		buf = &b
	}
	*buf = (*buf)[:length]
	v.reserved.Add(1)
	v.slot = buf
	return *buf, false
}

func (s *osServicesService) FrameCommit(frame *osservices.VideoFrame) bool {
	v := &s.video
	buf := v.slot
	v.slot = nil
	v.reserved.Add(-1)
	if buf == nil {
		return false
	}
	select {
	case v.frames <- guestFrame{
		buf:      buf,
		length:   len(frame.Data),
		pts90k:   frame.PTS90k,
		keyframe: frame.Keyframe,
		codec:    frame.Codec,
		fps:      frame.FPS,
	}:
		return true
	default:
		v.pool.Put(buf)
		return false
	}
}

func (s *osServicesService) FrameCancel(int) {
	v := &s.video
	if v.slot != nil {
		v.pool.Put(v.slot)
		v.slot = nil
		v.reserved.Add(-1)
	}
}

func (s *osServicesService) VideoGap() { s.video.wantKeyframe() }

func (s *osServicesService) KeyframeWanted() uint32 {
	v := &s.video
	v.keyMu.Lock()
	defer v.keyMu.Unlock()
	return v.keyframeGen
}

func (s *osServicesService) KeyframeDone(generation uint32, delivered bool) {
	v := &s.video
	v.keyMu.Lock()
	defer v.keyMu.Unlock()
	if delivered && generation == v.keyframeGen {
		v.keyframeGen = 0
	}
}

// writer writes the access units to the browser's video track. Until a
// keyframe arrives, delta frames do not go to the browser.
func (v *guestVideo) writer() {
	var lastPTS uint32
	havePTS := false
	for f := range v.frames {
		session := currentSession
		data := (*f.buf)[:f.length]
		wantCodec := osservices.CodecH264
		if session != nil && session.codecMimeType == webrtc.MimeTypeH265 {
			wantCodec = osservices.CodecH265
		}
		switch {
		case session == nil || session.VideoTrack == nil || f.codec != wantCodec || !v.owned.Load():
		case !f.keyframe && v.needKeyframe.Load():
		default:
			if f.keyframe {
				v.needKeyframe.Store(false)
			}
			frameDuration := time.Second / time.Duration(max(f.fps, 1))
			duration := frameDuration
			if havePTS {
				delta := time.Duration(f.pts90k-lastPTS) * time.Second / 90000
				if delta > 0 && delta <= time.Second {
					duration = delta
				}
			}
			lastPTS = f.pts90k
			havePTS = true
			if err := session.VideoTrack.WriteSample(media.Sample{Data: data, Duration: duration}); err != nil {
				osServicesLogger.Debug().Err(err).Msg("cannot write service video")
			}
		}
		v.pool.Put(f.buf)
		v.s.link.Wake()
	}
}
