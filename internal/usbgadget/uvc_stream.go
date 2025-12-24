//go:build linux

package usbgadget

import (
	"encoding/binary"
	"fmt"
	"sync"
	"syscall"
	"unsafe"
)

const maxFrameSizeBytes int32 = 1536 * 1024

// V4L2 constants
const (
	V4L2_BUF_TYPE_VIDEO_OUTPUT = 2
	V4L2_FIELD_NONE            = 1
	V4L2_MEMORY_MMAP           = 1
	V4L2_MEMORY_USERPTR        = 2

	// Buffer timestamp flags - GStreamer requires MONOTONIC timestamps
	V4L2_BUF_FLAG_TIMESTAMP_MASK      = 0x0000e000
	V4L2_BUF_FLAG_TIMESTAMP_MONOTONIC = 0x00002000

	// V4L2 ioctls (32-bit ARM values)
	VIDIOC_QUERYCAP        = 0x80685600
	VIDIOC_S_FMT           = 0xc0d05605
	VIDIOC_REQBUFS         = 0xc0145608
	VIDIOC_QUERYBUF        = 0xc0445609
	VIDIOC_QBUF            = 0xc044560f
	VIDIOC_DQBUF           = 0xc0445611
	VIDIOC_STREAMON        = 0x40045612
	VIDIOC_STREAMOFF       = 0x40045613
	VIDIOC_SUBSCRIBE_EVENT = 0x4020565a
	VIDIOC_DQEVENT         = 0x80805659
	UVCIOC_SEND_RESPONSE   = 0x40405501

	// UVC events
	V4L2_EVENT_PRIVATE_START = 0x08000000
	UVC_EVENT_CONNECT        = V4L2_EVENT_PRIVATE_START + 0
	UVC_EVENT_DISCONNECT     = V4L2_EVENT_PRIVATE_START + 1
	UVC_EVENT_STREAMON       = V4L2_EVENT_PRIVATE_START + 2
	UVC_EVENT_STREAMOFF      = V4L2_EVENT_PRIVATE_START + 3
	UVC_EVENT_SETUP          = V4L2_EVENT_PRIVATE_START + 4
	UVC_EVENT_DATA           = V4L2_EVENT_PRIVATE_START + 5

	V4L2_PIX_FMT_H264  = 0x34363248 // "H264" fourcc
	V4L2_PIX_FMT_MJPEG = 0x47504A4D // "MJPG" fourcc

	// USB/UVC constants
	USB_TYPE_MASK  = 0x60
	USB_TYPE_CLASS = 0x20
	USB_DIR_IN     = 0x80

	UVC_SET_CUR  = 0x01
	UVC_GET_CUR  = 0x81
	UVC_GET_MIN  = 0x82
	UVC_GET_MAX  = 0x83
	UVC_GET_RES  = 0x84
	UVC_GET_LEN  = 0x85
	UVC_GET_INFO = 0x86
	UVC_GET_DEF  = 0x87

	UVC_VS_PROBE_CONTROL  = 0x01
	UVC_VS_COMMIT_CONTROL = 0x02

	UVC_STREAMING_CONTROL_SIZE = 26
)

type v4l2_capability struct {
	Driver       [16]byte
	Card         [32]byte
	BusInfo      [32]byte
	Version      uint32
	Capabilities uint32
	DeviceCaps   uint32
	Reserved     [3]uint32
}

type v4l2_pix_format struct {
	Width, Height, PixelFormat, Field   uint32
	BytesPerLine, SizeImage, Colorspace uint32
	Priv, Flags, YcbcrEnc, Quantization uint32
	XferFunc                            uint32
}

type v4l2_format struct {
	Type uint32
	_    [4]byte
	Pix  v4l2_pix_format
	_    [200 - unsafe.Sizeof(v4l2_pix_format{})]byte
}

type v4l2_requestbuffers struct {
	Count, Type, Memory, Capabilities uint32
	Flags                             uint8
	Reserved                          [3]uint8
}

type v4l2_timecode struct {
	Type, Flags                     uint32
	Frames, Seconds, Minutes, Hours uint8
	Userbits                        [4]uint8
}

// v4l2_timeval matches kernel struct timeval on 32-bit ARM (linux/arm).
// We define our own to avoid platform-specific differences in syscall.Timeval.
type v4l2_timeval struct {
	Sec  int32
	Usec int32
}

type v4l2_buffer struct {
	Index, Type, BytesUsed, Flags, Field uint32
	Timestamp                            v4l2_timeval
	Timecode                             v4l2_timecode
	Sequence, Memory                     uint32
	M                                    uintptr
	Length, Reserved2                    uint32
	RequestFD                            int32
}

type v4l2_event_subscription struct {
	Type, ID, Flags uint32
	Reserved        [5]uint32
}

type v4l2_event struct {
	Type      uint32
	U         [64]byte
	Pending   uint32
	Sequence  uint32
	Timestamp syscall.Timespec
	ID        uint32
	Reserved  [8]uint32
}

type usb_ctrlrequest struct {
	bRequestType, bRequest  uint8
	wValue, wIndex, wLength uint16
}

type uvc_request_data struct {
	Length int32
	Data   [60]byte
}

type uvc_streaming_control struct {
	bmHint                                                            uint16
	bFormatIndex, bFrameIndex                                         uint8
	dwFrameInterval                                                   uint32
	wKeyFrameRate, wPFrameRate, wCompQuality, wCompWindowSize, wDelay uint16
	dwMaxVideoFrameSize, dwMaxPayloadTransferSize                     uint32
	dwClockFrequency                                                  uint32
	bmFramingInfo, bPreferedVersion, bMinVersion, bMaxVersion         uint8 //nolint:unused // kernel struct fields
}

type UVCStreamer struct {
	devicePath       string
	mu               sync.Mutex
	log              Logger
	buffers          [][]byte
	fd               int
	width            uint32
	height           uint32
	bufferSize       int
	bufCount         int
	curBufIdx        int
	queuedBufs       int
	frameCounter     uint32
	droppedFrames    uint32
	lastDropLogFrame uint32 // Last frame count when we logged drops
	probe            uvc_streaming_control
	commit           uvc_streaming_control
	eventDataBuf     [64]byte
	pendingCtrl      uint8
	streaming        bool
	streamReady      bool
}

type Logger interface {
	Info() LogEvent
	Warn() LogEvent
	Error() LogEvent
	Debug() LogEvent
}

type LogEvent interface {
	Str(key, val string) LogEvent
	Int(key string, val int) LogEvent
	Uint32(key string, val uint32) LogEvent
	Bool(key string, val bool) LogEvent
	Err(err error) LogEvent
	Msg(msg string)
}

// frameResolutions maps UVC frame index to resolution.
// Frame indices are 1-based and match the order defined in ConfigFS:
// MJPEG frames: 1080p=1, 720p_1=2, 480p_2=3
// H.264 frames: 1080p=1, 720p_1=2, 480p_2=3 (same indices within format)
var frameResolutions = map[uint8][2]uint32{
	1: {1920, 1080}, // 1080p
	2: {1280, 720},  // 720p
	3: {640, 480},   // 480p
}

func NewUVCStreamer(devicePath string, log Logger) *UVCStreamer {
	s := &UVCStreamer{
		devicePath: devicePath,
		fd:         -1,
		log:        log,
		probe: uvc_streaming_control{
			bmHint:                   1,
			bFormatIndex:             1,
			bFrameIndex:              1,      // 1080p default
			dwFrameInterval:          333333, // 30fps
			dwMaxVideoFrameSize:      1920 * 1080 * 2,
			dwMaxPayloadTransferSize: 3072,
			dwClockFrequency:         48000000,
			bPreferedVersion:         1,
			bMinVersion:              1,
			bMaxVersion:              1,
		},
	}
	s.commit = s.probe
	return s
}

func (s *UVCStreamer) GetCommittedResolution() (uint32, uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if res, ok := frameResolutions[s.commit.bFrameIndex]; ok {
		return res[0], res[1]
	}
	return 1920, 1080
}

// GetCommittedFrameInterval returns the frame interval negotiated by the host.
// Value is in 100ns units (333333 = 30fps, 166666 = 60fps).
func (s *UVCStreamer) GetCommittedFrameInterval() uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.commit.dwFrameInterval
}

// GetCommittedFrameRate returns the frame rate negotiated by the host in fps.
func (s *UVCStreamer) GetCommittedFrameRate() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.commit.dwFrameInterval == 0 {
		return 30 // Default to 30fps if not set
	}
	return int(10000000 / s.commit.dwFrameInterval)
}

// GetCommittedFormatIndex returns the format index selected by the host.
// Format index is 1-based and corresponds to configfs format order:
// - 1 = MJPEG (if configured)
// - 2 = H.264 (if MJPEG also configured), or 1 if H.264 only
func (s *UVCStreamer) GetCommittedFormatIndex() uint8 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.commit.bFormatIndex
}

// IsH264Format returns true if the committed format is H.264.
// MJPEG is set up first in ConfigFS (setupMJPEGFormat before setupH264Format),
// so MJPEG is format index 1, H.264 is format index 2.
func (s *UVCStreamer) IsH264Format() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	// MJPEG is set up first in ConfigFS, so it's format index 1
	// H.264 is set up second, so it's format index 2
	return s.commit.bFormatIndex == 2
}

func (s *UVCStreamer) Open() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.fd >= 0 {
		return nil
	}

	fd, err := syscall.Open(s.devicePath, syscall.O_RDWR|syscall.O_NONBLOCK, 0)
	if err != nil {
		return fmt.Errorf("failed to open UVC device %s: %w", s.devicePath, err)
	}

	s.fd = fd
	s.log.Debug().Str("device", s.devicePath).Msg("UVC device opened")
	return nil
}

func (s *UVCStreamer) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.fd < 0 {
		return nil
	}

	if s.streaming {
		_ = s.stopStreamingLocked()
	}

	// Clear buffer references - GC will handle cleanup
	s.buffers = nil
	s.bufCount = 0
	s.curBufIdx = 0

	err := syscall.Close(s.fd)
	s.fd = -1
	return err
}

// SetFormatWithCodec sets the V4L2 output format with the specified codec.
// isMjpeg=true for MJPEG, false for H.264.
func (s *UVCStreamer) SetFormatWithCodec(width, height uint32, isMjpeg bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.fd < 0 {
		return fmt.Errorf("UVC device not open")
	}

	s.width = width
	s.height = height
	// Compressed frames are typically much smaller than raw
	// Use 2MB max buffer for 1080p (generous for high bitrate/quality)
	s.bufferSize = 2 * 1024 * 1024

	pixelFormat := uint32(V4L2_PIX_FMT_H264)
	if isMjpeg {
		pixelFormat = V4L2_PIX_FMT_MJPEG
	}

	var v4l2fmt v4l2_format
	v4l2fmt.Type = V4L2_BUF_TYPE_VIDEO_OUTPUT
	v4l2fmt.Pix.Width = width
	v4l2fmt.Pix.Height = height
	v4l2fmt.Pix.PixelFormat = pixelFormat
	v4l2fmt.Pix.Field = V4L2_FIELD_NONE
	v4l2fmt.Pix.SizeImage = uint32(s.bufferSize)

	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(s.fd), VIDIOC_S_FMT, uintptr(unsafe.Pointer(&v4l2fmt)))
	if errno != 0 {
		// S_FMT may fail for UVC gadget but that's okay - the gadget driver
		// automatically uses the format negotiated with the host
		return fmt.Errorf("VIDIOC_S_FMT failed (errno=%d), using default UVC format", errno)
	}

	s.width = v4l2fmt.Pix.Width
	s.height = v4l2fmt.Pix.Height
	if v4l2fmt.Pix.SizeImage > 0 {
		s.bufferSize = int(v4l2fmt.Pix.SizeImage)
	}

	return nil
}

func (s *UVCStreamer) RequestBuffers(count uint32) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.fd < 0 {
		return fmt.Errorf("UVC device not open")
	}

	const bufferSize = 2 * 1024 * 1024

	var req v4l2_requestbuffers
	req.Count = count
	req.Type = V4L2_BUF_TYPE_VIDEO_OUTPUT
	req.Memory = V4L2_MEMORY_USERPTR

	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(s.fd), VIDIOC_REQBUFS, uintptr(unsafe.Pointer(&req)))
	if errno != 0 {
		return fmt.Errorf("VIDIOC_REQBUFS failed: %v", errno)
	}

	s.bufCount = int(req.Count)
	s.buffers = make([][]byte, req.Count)
	s.curBufIdx = 0

	for i := uint32(0); i < req.Count; i++ {
		s.buffers[i] = make([]byte, bufferSize)
	}

	s.log.Info().Int("count", int(req.Count)).Msg("UVC buffers allocated")
	return nil
}

func (s *UVCStreamer) StartStreaming() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.fd < 0 {
		return fmt.Errorf("UVC device not open")
	}
	if s.streaming {
		s.log.Debug().Msg("UVC already streaming, skipping StartStreaming")
		return nil
	}

	s.log.Info().Int("bufCount", s.bufCount).Msg("Starting V4L2 streaming")

	s.queuedBufs = 0
	s.curBufIdx = 0
	s.frameCounter = 0
	s.droppedFrames = 0
	s.lastDropLogFrame = 0
	s.streaming = true

	bufType := uint32(V4L2_BUF_TYPE_VIDEO_OUTPUT)
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(s.fd), VIDIOC_STREAMON, uintptr(unsafe.Pointer(&bufType)))
	if errno != 0 {
		s.streaming = false
		return fmt.Errorf("VIDIOC_STREAMON failed: %v", errno)
	}
	s.streamReady = true

	s.log.Info().Msg("V4L2 STREAMON successful - UVC streaming active")
	return nil
}

func (s *UVCStreamer) StopStreaming() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopStreamingLocked()
}

func (s *UVCStreamer) stopStreamingLocked() error {
	if s.fd < 0 || !s.streaming {
		return nil
	}

	if s.streamReady {
		bufType := uint32(V4L2_BUF_TYPE_VIDEO_OUTPUT)
		_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(s.fd), VIDIOC_STREAMOFF, uintptr(unsafe.Pointer(&bufType)))
		if errno != 0 {
			return fmt.Errorf("VIDIOC_STREAMOFF failed: %v", errno)
		}
	}

	s.streaming = false
	s.streamReady = false
	s.queuedBufs = 0
	s.curBufIdx = 0
	s.frameCounter = 0
	return nil
}

// WriteFrame writes a video frame to the UVC device.
// This is the hot path - optimized for minimal overhead at 1080p@30fps on 32-bit ARM.
func (s *UVCStreamer) WriteFrame(data []byte) error {
	s.mu.Lock()

	// Fast path: check if streaming is active
	if s.fd < 0 || !s.streaming || len(s.buffers) == 0 {
		s.mu.Unlock()
		return nil
	}

	dataLen := len(data)

	// Check USB 2.0 bandwidth limit
	if int32(dataLen) > maxFrameSizeBytes {
		s.droppedFrames++
		// Log periodically (first drop, then every 100 drops)
		if s.droppedFrames == 1 || s.droppedFrames%100 == 0 {
			s.log.Warn().
				Int("frameSize", dataLen).
				Int("maxSize", int(maxFrameSizeBytes)).
				Uint32("totalDropped", s.droppedFrames).
				Msg("Frame too large for USB 2.0 - dropping")
		}
		s.mu.Unlock()
		return nil
	}

	// Get current buffer
	bufIdx := s.curBufIdx
	buf := s.buffers[bufIdx]
	if dataLen > len(buf) {
		s.droppedFrames++
		if s.droppedFrames-s.lastDropLogFrame >= 100 {
			s.lastDropLogFrame = s.droppedFrames
			s.log.Warn().
				Int("frameSize", dataLen).
				Int("bufSize", len(buf)).
				Uint32("totalDropped", s.droppedFrames).
				Msg("Frame exceeds buffer size - dropping")
		}
		s.mu.Unlock()
		return nil
	}

	// Copy frame data to buffer (under lock - prevents race)
	copy(buf, data)

	// Dequeue completed buffers if all are in use
	for s.queuedBufs >= s.bufCount {
		var dqbuf v4l2_buffer
		dqbuf.Type = V4L2_BUF_TYPE_VIDEO_OUTPUT
		dqbuf.Memory = V4L2_MEMORY_USERPTR

		_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(s.fd), VIDIOC_DQBUF, uintptr(unsafe.Pointer(&dqbuf)))
		if errno != 0 {
			s.mu.Unlock()
			if errno == syscall.EAGAIN {
				return nil
			}
			return fmt.Errorf("VIDIOC_DQBUF failed: %v", errno)
		}
		s.queuedBufs--
	}

	s.frameCounter++

	// Frame-counter-based timestamp (avoids time.Now() syscall overhead).
	// UVC hosts use buffer arrival order for timing, not timestamp values.
	// Assumes 30fps nominal - actual frame pacing controlled by source.
	const usecPerFrame = 33333 // 1/30 second in microseconds
	elapsedUsecTotal := int64(s.frameCounter) * usecPerFrame
	elapsedSec := int32(elapsedUsecTotal / 1_000_000)
	elapsedUsec := int32(elapsedUsecTotal % 1_000_000)

	// Prepare V4L2 buffer
	var v4l2buf v4l2_buffer
	v4l2buf.Index = uint32(bufIdx)
	v4l2buf.Type = V4L2_BUF_TYPE_VIDEO_OUTPUT
	v4l2buf.Memory = V4L2_MEMORY_USERPTR
	v4l2buf.Field = V4L2_FIELD_NONE
	v4l2buf.BytesUsed = uint32(dataLen)
	v4l2buf.Length = uint32(len(buf))
	v4l2buf.M = uintptr(unsafe.Pointer(&buf[0]))
	v4l2buf.Timestamp = v4l2_timeval{Sec: elapsedSec, Usec: elapsedUsec}
	v4l2buf.Flags = V4L2_BUF_FLAG_TIMESTAMP_MONOTONIC

	// Queue buffer to V4L2
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(s.fd), VIDIOC_QBUF, uintptr(unsafe.Pointer(&v4l2buf)))
	if errno != 0 {
		s.mu.Unlock()
		return fmt.Errorf("VIDIOC_QBUF failed: %v", errno)
	}

	s.queuedBufs++

	// Advance buffer index (conditional is faster than modulo on ARM)
	s.curBufIdx = bufIdx + 1
	if s.curBufIdx >= s.bufCount {
		s.curBufIdx = 0
	}

	s.mu.Unlock()
	return nil
}

func (s *UVCStreamer) SubscribeEvents() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.fd < 0 {
		return fmt.Errorf("UVC device not open")
	}

	events := []uint32{
		UVC_EVENT_CONNECT, UVC_EVENT_DISCONNECT,
		UVC_EVENT_STREAMON, UVC_EVENT_STREAMOFF,
		UVC_EVENT_SETUP, UVC_EVENT_DATA,
	}

	for _, eventType := range events {
		var sub v4l2_event_subscription
		sub.Type = eventType
		_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(s.fd), VIDIOC_SUBSCRIBE_EVENT, uintptr(unsafe.Pointer(&sub)))
		if errno != 0 {
			return fmt.Errorf("VIDIOC_SUBSCRIBE_EVENT failed for event %d: %v", eventType, errno)
		}
	}
	return nil
}

func (s *UVCStreamer) PollEventsWithData() (uint32, []byte, error) {
	fd := s.fd
	if fd < 0 {
		return 0, nil, fmt.Errorf("UVC device not open")
	}

	var ev v4l2_event
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), VIDIOC_DQEVENT, uintptr(unsafe.Pointer(&ev)))
	if errno != 0 {
		if errno == syscall.EAGAIN || errno == syscall.EWOULDBLOCK || errno == syscall.ENOENT {
			return 0, nil, nil
		}
		return 0, nil, fmt.Errorf("VIDIOC_DQEVENT failed: %v", errno)
	}

	copy(s.eventDataBuf[:], ev.U[:])
	return ev.Type, s.eventDataBuf[:], nil
}

func (s *UVCStreamer) HandleSetupEvent(eventData []byte) error {
	if len(eventData) < 12 {
		return fmt.Errorf("event data too short: %d bytes", len(eventData))
	}

	const ctrlOffset = 4
	ctrl := usb_ctrlrequest{
		bRequestType: eventData[ctrlOffset+0],
		bRequest:     eventData[ctrlOffset+1],
		wValue:       binary.LittleEndian.Uint16(eventData[ctrlOffset+2 : ctrlOffset+4]),
		wIndex:       binary.LittleEndian.Uint16(eventData[ctrlOffset+4 : ctrlOffset+6]),
		wLength:      binary.LittleEndian.Uint16(eventData[ctrlOffset+6 : ctrlOffset+8]),
	}

	if (ctrl.bRequestType & USB_TYPE_MASK) != USB_TYPE_CLASS {
		return s.sendEmptyResponse()
	}

	cs := uint8(ctrl.wValue >> 8)
	entityId := uint8(ctrl.wIndex >> 8)

	if entityId != 0 {
		return s.handleControlUnitRequest(&ctrl, cs)
	}
	return s.handleStreamingRequest(&ctrl, cs)
}

func (s *UVCStreamer) handleControlUnitRequest(ctrl *usb_ctrlrequest, cs uint8) error {
	switch ctrl.bRequest {
	case UVC_GET_INFO:
		return s.sendInfoResponse(0x03)
	case UVC_SET_CUR:
		return s.sendAcceptResponse(int(ctrl.wLength))
	case UVC_GET_CUR, UVC_GET_MIN, UVC_GET_MAX, UVC_GET_DEF, UVC_GET_RES:
		return s.sendZeroResponse(int(ctrl.wLength))
	case UVC_GET_LEN:
		return s.sendLengthResponse(int(ctrl.wLength))
	default:
		return s.sendEmptyResponse()
	}
}

func (s *UVCStreamer) handleStreamingRequest(ctrl *usb_ctrlrequest, cs uint8) error {
	switch ctrl.bRequest {
	case UVC_SET_CUR:
		s.pendingCtrl = cs
		return s.sendAcceptResponse(UVC_STREAMING_CONTROL_SIZE)
	case UVC_GET_CUR:
		if cs == UVC_VS_COMMIT_CONTROL {
			return s.sendCommitControl()
		}
		return s.sendProbeControl()
	case UVC_GET_MIN, UVC_GET_MAX, UVC_GET_DEF:
		return s.sendProbeControl()
	case UVC_GET_RES:
		return s.sendZeroProbeControl()
	case UVC_GET_LEN:
		return s.sendControlLength()
	case UVC_GET_INFO:
		return s.sendControlInfo()
	default:
		return s.sendEmptyResponse()
	}
}

func (s *UVCStreamer) HandleDataEvent(eventData []byte) (bool, error) {
	if s.pendingCtrl == 0 {
		return false, nil
	}

	ctrl := s.pendingCtrl
	s.pendingCtrl = 0

	const dataOffset = 4
	if len(eventData) < dataOffset+4+UVC_STREAMING_CONTROL_SIZE {
		return false, nil
	}

	payloadOffset := dataOffset + 4
	hostCtrl := s.parseStreamingControl(eventData[payloadOffset:])

	if ctrl == UVC_VS_PROBE_CONTROL {
		s.updateProbeFromHost(hostCtrl)
		return false, nil
	}

	if ctrl == UVC_VS_COMMIT_CONTROL {
		s.commit = s.probe
		return true, nil
	}

	return false, nil
}

func (s *UVCStreamer) parseStreamingControl(data []byte) uvc_streaming_control {
	return uvc_streaming_control{
		bmHint:                   binary.LittleEndian.Uint16(data[0:2]),
		bFormatIndex:             data[2],
		bFrameIndex:              data[3],
		dwFrameInterval:          binary.LittleEndian.Uint32(data[4:8]),
		wKeyFrameRate:            binary.LittleEndian.Uint16(data[8:10]),
		wPFrameRate:              binary.LittleEndian.Uint16(data[10:12]),
		wCompQuality:             binary.LittleEndian.Uint16(data[12:14]),
		wCompWindowSize:          binary.LittleEndian.Uint16(data[14:16]),
		wDelay:                   binary.LittleEndian.Uint16(data[16:18]),
		dwMaxVideoFrameSize:      binary.LittleEndian.Uint32(data[18:22]),
		dwMaxPayloadTransferSize: binary.LittleEndian.Uint32(data[22:26]),
	}
}

func (s *UVCStreamer) updateProbeFromHost(host uvc_streaming_control) {
	if host.bFormatIndex > 0 {
		s.probe.bFormatIndex = host.bFormatIndex
	}
	if host.bFrameIndex > 0 {
		s.probe.bFrameIndex = host.bFrameIndex
	}
	if host.dwFrameInterval > 0 {
		s.probe.dwFrameInterval = host.dwFrameInterval
	}
}

func (s *UVCStreamer) sendProbeControl() error {
	return s.sendStreamingControl(&s.probe)
}

func (s *UVCStreamer) sendCommitControl() error {
	return s.sendStreamingControl(&s.commit)
}

func (s *UVCStreamer) sendStreamingControl(ctrl *uvc_streaming_control) error {
	var resp uvc_request_data
	resp.Length = UVC_STREAMING_CONTROL_SIZE

	binary.LittleEndian.PutUint16(resp.Data[0:2], ctrl.bmHint)
	resp.Data[2] = ctrl.bFormatIndex
	resp.Data[3] = ctrl.bFrameIndex
	binary.LittleEndian.PutUint32(resp.Data[4:8], ctrl.dwFrameInterval)
	binary.LittleEndian.PutUint16(resp.Data[8:10], ctrl.wKeyFrameRate)
	binary.LittleEndian.PutUint16(resp.Data[10:12], ctrl.wPFrameRate)
	binary.LittleEndian.PutUint16(resp.Data[12:14], ctrl.wCompQuality)
	binary.LittleEndian.PutUint16(resp.Data[14:16], ctrl.wCompWindowSize)
	binary.LittleEndian.PutUint16(resp.Data[16:18], ctrl.wDelay)
	binary.LittleEndian.PutUint32(resp.Data[18:22], ctrl.dwMaxVideoFrameSize)
	binary.LittleEndian.PutUint32(resp.Data[22:26], ctrl.dwMaxPayloadTransferSize)

	return s.sendResponse(&resp)
}

func (s *UVCStreamer) sendInfoResponse(info uint8) error {
	var resp uvc_request_data
	resp.Length = 1
	resp.Data[0] = info
	return s.sendResponse(&resp)
}

func (s *UVCStreamer) sendZeroResponse(length int) error {
	var resp uvc_request_data
	if length > 60 {
		length = 60
	}
	resp.Length = int32(length)
	return s.sendResponse(&resp)
}

func (s *UVCStreamer) sendLengthResponse(length int) error {
	var resp uvc_request_data
	resp.Length = 2
	binary.LittleEndian.PutUint16(resp.Data[0:2], uint16(length))
	return s.sendResponse(&resp)
}

func (s *UVCStreamer) sendZeroProbeControl() error {
	return s.sendZeroResponse(UVC_STREAMING_CONTROL_SIZE)
}

func (s *UVCStreamer) sendControlLength() error {
	return s.sendLengthResponse(UVC_STREAMING_CONTROL_SIZE)
}

func (s *UVCStreamer) sendControlInfo() error {
	return s.sendInfoResponse(0x03)
}

func (s *UVCStreamer) sendEmptyResponse() error {
	return s.sendZeroResponse(0)
}

func (s *UVCStreamer) sendAcceptResponse(length int) error {
	return s.sendZeroResponse(length)
}

func (s *UVCStreamer) sendResponse(resp *uvc_request_data) error {
	s.mu.Lock()
	fd := s.fd
	s.mu.Unlock()

	if fd < 0 {
		return fmt.Errorf("UVC device not open")
	}

	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), UVCIOC_SEND_RESPONSE, uintptr(unsafe.Pointer(resp)))
	if errno != 0 {
		return fmt.Errorf("UVCIOC_SEND_RESPONSE failed: %v", errno)
	}
	return nil
}

// IsStreaming returns true if streaming is active.
// Note: This is a lock-free read for hot-path performance. Use uvcStreamingFast
// atomic flag in Manager for authoritative streaming state in frame handlers.
func (s *UVCStreamer) IsStreaming() bool { return s.streaming }

// IsOpen returns true if the device is open.
// Note: Lock-free read - fd is only modified under mutex in Open/Close.
func (s *UVCStreamer) IsOpen() bool { return s.fd >= 0 }

func (s *UVCStreamer) IsValid() bool {
	s.mu.Lock()
	fd := s.fd
	s.mu.Unlock()
	if fd < 0 {
		return false
	}
	var cap v4l2_capability
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), VIDIOC_QUERYCAP, uintptr(unsafe.Pointer(&cap)))
	return errno == 0
}
