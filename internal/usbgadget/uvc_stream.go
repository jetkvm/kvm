//go:build linux

package usbgadget

import (
	"encoding/binary"
	"fmt"
	"os"
	"sync"
	"syscall"
	"unsafe"
)

// V4L2 constants for UVC gadget output
const (
	// V4L2 buffer types
	V4L2_BUF_TYPE_VIDEO_OUTPUT = 2

	// V4L2 field types
	V4L2_FIELD_NONE = 1

	// V4L2 memory types
	V4L2_MEMORY_MMAP    = 1
	V4L2_MEMORY_USERPTR = 2

	// V4L2 ioctls (using _IOWR macro values)
	// Note: QUERYBUF/QBUF/DQBUF use v4l2_buffer struct which has different sizes
	// on 32-bit (68 bytes) vs 64-bit (88 bytes) due to unsigned long in union m
	// These values are for 32-bit ARM (size=0x44=68 in ioctl encoding)
	VIDIOC_QUERYCAP          = 0x80685600
	VIDIOC_S_FMT             = 0xc0d05605
	VIDIOC_G_FMT             = 0xc0d05604
	VIDIOC_REQBUFS           = 0xc0145608
	VIDIOC_QUERYBUF          = 0xc0445609 // 32-bit: struct size 68, _IOWR('V', 9, 68)
	VIDIOC_QBUF              = 0xc044560f // 32-bit: struct size 68
	VIDIOC_DQBUF             = 0xc0445611 // 32-bit: struct size 68
	VIDIOC_STREAMON          = 0x40045612
	VIDIOC_STREAMOFF         = 0x40045613
	VIDIOC_SUBSCRIBE_EVENT   = 0x4020565a
	VIDIOC_UNSUBSCRIBE_EVENT = 0x4020565b
	VIDIOC_DQEVENT           = 0x80805659

	// UVC specific ioctls
	// _IOW('U', 1, struct uvc_request_data) where uvc_request_data is 64 bytes (4+60)
	// Formula: (1 << 30) | (64 << 16) | ('U' << 8) | 1 = 0x40405501
	UVCIOC_SEND_RESPONSE = 0x40405501

	// UVC specific event types (from kernel include/uapi/linux/videodev2.h and drivers/usb/gadget/function/uvc.h)
	V4L2_EVENT_PRIVATE_START = 0x08000000
	UVC_EVENT_CONNECT        = V4L2_EVENT_PRIVATE_START + 0 // 0x08000000
	UVC_EVENT_DISCONNECT     = V4L2_EVENT_PRIVATE_START + 1 // 0x08000001
	UVC_EVENT_STREAMON       = V4L2_EVENT_PRIVATE_START + 2 // 0x08000002
	UVC_EVENT_STREAMOFF      = V4L2_EVENT_PRIVATE_START + 3 // 0x08000003
	UVC_EVENT_SETUP          = V4L2_EVENT_PRIVATE_START + 4 // 0x08000004
	UVC_EVENT_DATA           = V4L2_EVENT_PRIVATE_START + 5 // 0x08000005

	// V4L2 pixel formats
	V4L2_PIX_FMT_MJPEG = 0x47504a4d // 'MJPG'

	// USB request types
	USB_TYPE_MASK       = 0x60
	USB_TYPE_STANDARD   = 0x00
	USB_TYPE_CLASS      = 0x20
	USB_RECIP_MASK      = 0x1f
	USB_RECIP_INTERFACE = 0x01
	USB_DIR_IN          = 0x80

	// UVC interface types
	UVC_INTF_CONTROL   = 0
	UVC_INTF_STREAMING = 1

	// UVC class-specific requests
	UVC_SET_CUR  = 0x01
	UVC_GET_CUR  = 0x81
	UVC_GET_MIN  = 0x82
	UVC_GET_MAX  = 0x83
	UVC_GET_RES  = 0x84
	UVC_GET_LEN  = 0x85
	UVC_GET_INFO = 0x86
	UVC_GET_DEF  = 0x87

	// UVC streaming interface control selectors
	UVC_VS_PROBE_CONTROL  = 0x01
	UVC_VS_COMMIT_CONTROL = 0x02
)

// V4L2 structures
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
	Width        uint32
	Height       uint32
	PixelFormat  uint32
	Field        uint32
	BytesPerLine uint32
	SizeImage    uint32
	Colorspace   uint32
	Priv         uint32
	Flags        uint32
	YcbcrEnc     uint32
	Quantization uint32
	XferFunc     uint32
}

type v4l2_format struct {
	Type uint32
	_    [4]byte // padding
	Pix  v4l2_pix_format
	_    [200 - unsafe.Sizeof(v4l2_pix_format{})]byte // padding to match kernel struct size
}

type v4l2_requestbuffers struct {
	Count        uint32
	Type         uint32
	Memory       uint32
	Capabilities uint32
	Flags        uint8
	Reserved     [3]uint8
}

type v4l2_timecode struct {
	Type     uint32
	Flags    uint32
	Frames   uint8
	Seconds  uint8
	Minutes  uint8
	Hours    uint8
	Userbits [4]uint8
}

// v4l2_buffer matches the kernel struct for 32-bit ARM
// Total size: 68 bytes on 32-bit ARM
type v4l2_buffer struct {
	Index     uint32          // offset 0
	Type      uint32          // offset 4
	BytesUsed uint32          // offset 8
	Flags     uint32          // offset 12
	Field     uint32          // offset 16
	Timestamp syscall.Timeval // offset 20, 8 bytes on 32-bit (tv_sec + tv_usec)
	Timecode  v4l2_timecode   // offset 28, 16 bytes
	Sequence  uint32          // offset 44
	Memory    uint32          // offset 48
	M         uintptr         // offset 52, union: offset or userptr - 4 bytes on 32-bit ARM
	Length    uint32          // offset 56
	Reserved2 uint32          // offset 60
	RequestFD int32           // offset 64, total size 68
}

type v4l2_event_subscription struct {
	Type     uint32
	ID       uint32
	Flags    uint32
	Reserved [5]uint32
}

type v4l2_event struct {
	Type      uint32
	U         [64]byte // union of various event data
	Pending   uint32
	Sequence  uint32
	Timestamp syscall.Timespec
	ID        uint32
	Reserved  [8]uint32
}

// USB control request structure (from linux/usb/ch9.h)
type usb_ctrlrequest struct {
	bRequestType uint8
	bRequest     uint8
	wValue       uint16
	wIndex       uint16
	wLength      uint16
}

// UVC request data for UVCIOC_SEND_RESPONSE
type uvc_request_data struct {
	Length int32
	Data   [60]byte
}

// UVC streaming control structure
// UVC 1.0: 26 bytes, UVC 1.1: 34 bytes, UVC 1.5: 48 bytes
// We use UVC 1.0 (26 bytes) for maximum compatibility
const UVC_STREAMING_CONTROL_SIZE = 26

type uvc_streaming_control struct {
	bmHint                   uint16
	bFormatIndex             uint8
	bFrameIndex              uint8
	dwFrameInterval          uint32
	wKeyFrameRate            uint16
	wPFrameRate              uint16
	wCompQuality             uint16
	wCompWindowSize          uint16
	wDelay                   uint16
	dwMaxVideoFrameSize      uint32
	dwMaxPayloadTransferSize uint32
	// UVC 1.0 ends here (26 bytes)
	// Fields below are UVC 1.1+ only
	dwClockFrequency uint32
	bmFramingInfo    uint8
	bPreferedVersion uint8
	bMinVersion      uint8
	bMaxVersion      uint8
}

// UVCStreamer handles writing MJPEG frames to the UVC gadget device
type UVCStreamer struct {
	devicePath string
	fd         int
	width      uint32
	height     uint32
	streaming  bool
	mu         sync.Mutex
	log        Logger
	bufferSize int        // size of each buffer in bytes
	bufCount   int        // number of buffers allocated
	buffers    [][]byte   // buffer slices (mmap'd or userptr)
	bufOffsets []uint32   // buffer offsets for MMAP
	curBufIdx  int        // current buffer index for round-robin
	queuedBufs int        // number of buffers currently queued with driver
	useUserPtr bool       // true if using USERPTR mode instead of MMAP

	// UVC control state
	probe       uvc_streaming_control
	commit      uvc_streaming_control
	pendingCtrl uint8 // control selector for pending SET_CUR
}

// Logger interface for UVC streaming
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

// Frame resolution lookup table (matches ConfigFS configuration)
// bFrameIndex -> (width, height)
var frameResolutions = map[uint8][2]uint32{
	1: {1280, 720},  // 720p
	2: {1920, 1080}, // 1080p
}

// NewUVCStreamer creates a new UVC streamer
func NewUVCStreamer(devicePath string, log Logger) *UVCStreamer {
	s := &UVCStreamer{
		devicePath: devicePath,
		fd:         -1,
		log:        log,
	}
	// Initialize default probe control (720p30 MJPEG)
	s.probe = uvc_streaming_control{
		bmHint:                   1,      // dwFrameInterval hint
		bFormatIndex:             1,      // MJPEG format
		bFrameIndex:              1,      // 720p frame
		dwFrameInterval:          333333, // 30fps (in 100ns units)
		wKeyFrameRate:            0,
		wPFrameRate:              0,
		wCompQuality:             0,
		wCompWindowSize:          0,
		wDelay:                   0,
		dwMaxVideoFrameSize:      1280 * 720 * 2, // Max frame size
		dwMaxPayloadTransferSize: 3072,           // Max payload per transfer
		dwClockFrequency:         48000000,       // 48MHz
		bmFramingInfo:            0,
		bPreferedVersion:         1,
		bMinVersion:              1,
		bMaxVersion:              1,
	}
	s.commit = s.probe
	return s
}

// GetCommittedResolution returns the width and height from the committed frame index
func (s *UVCStreamer) GetCommittedResolution() (uint32, uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if res, ok := frameResolutions[s.commit.bFrameIndex]; ok {
		return res[0], res[1]
	}
	// Default to 720p if unknown frame index
	return 1280, 720
}

// Open opens the UVC device
func (s *UVCStreamer) Open() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.fd >= 0 {
		return nil // Already open
	}

	fd, err := syscall.Open(s.devicePath, syscall.O_RDWR|syscall.O_NONBLOCK, 0)
	if err != nil {
		return fmt.Errorf("failed to open UVC device %s: %w", s.devicePath, err)
	}

	s.fd = fd
	s.log.Info().Str("device", s.devicePath).Msg("UVC device opened")
	return nil
}

// Close closes the UVC device
func (s *UVCStreamer) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.fd < 0 {
		return nil
	}

	if s.streaming {
		s.stopStreamingLocked()
	}

	// Unmap MMAP buffers before closing (only if not using USERPTR)
	if !s.useUserPtr {
		for _, buf := range s.buffers {
			if buf != nil {
				syscall.Munmap(buf)
			}
		}
	}
	s.buffers = nil
	s.bufOffsets = nil
	s.bufCount = 0
	s.curBufIdx = 0
	s.useUserPtr = false

	err := syscall.Close(s.fd)
	s.fd = -1
	s.log.Info().Msg("UVC device closed")
	return err
}

// SetFormat sets the video format for UVC output
// Note: UVC gadget devices don't support VIDIOC_S_FMT from userspace - the format
// is negotiated by the host. This function just stores the expected dimensions
// and calculates a buffer size for frame data.
func (s *UVCStreamer) SetFormat(width, height uint32) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.fd < 0 {
		return fmt.Errorf("UVC device not open")
	}

	// Store the expected format dimensions
	s.width = width
	s.height = height
	// Conservative buffer size for MJPEG (typically 1/4 to 1/2 of uncompressed)
	// Using width * height to be safe for high-quality MJPEG frames
	s.bufferSize = int(width * height)

	// Try S_FMT but don't fail if not supported (UVC gadget doesn't support it)
	var v4l2fmt v4l2_format
	v4l2fmt.Type = V4L2_BUF_TYPE_VIDEO_OUTPUT
	v4l2fmt.Pix.Width = width
	v4l2fmt.Pix.Height = height
	v4l2fmt.Pix.PixelFormat = V4L2_PIX_FMT_MJPEG
	v4l2fmt.Pix.Field = 1 // V4L2_FIELD_NONE
	v4l2fmt.Pix.SizeImage = uint32(s.bufferSize)

	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(s.fd), VIDIOC_S_FMT, uintptr(unsafe.Pointer(&v4l2fmt)))
	if errno != 0 {
		// S_FMT not supported is normal for UVC gadget devices
		s.log.Debug().
			Uint32("width", s.width).
			Uint32("height", s.height).
			Int("bufferSize", s.bufferSize).
			Msg("UVC S_FMT not supported (expected for UVC gadget), using defaults")
	} else {
		// If S_FMT succeeded, use the returned values
		s.width = v4l2fmt.Pix.Width
		s.height = v4l2fmt.Pix.Height
		if v4l2fmt.Pix.SizeImage > 0 {
			s.bufferSize = int(v4l2fmt.Pix.SizeImage)
		}
	}

	s.log.Info().
		Uint32("width", s.width).
		Uint32("height", s.height).
		Int("bufferSize", s.bufferSize).
		Msg("UVC format configured")

	return nil
}

// RequestBuffers allocates buffers for streaming using USERPTR mode
// This allows us to control buffer size regardless of V4L2 format
func (s *UVCStreamer) RequestBuffers(count uint32) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.fd < 0 {
		return fmt.Errorf("UVC device not open")
	}

	// Use a large buffer size for MJPEG frames (2MB should handle 1080p)
	const bufferSize = 2 * 1024 * 1024

	// Request USERPTR buffers from the kernel
	var req v4l2_requestbuffers
	req.Count = count
	req.Type = V4L2_BUF_TYPE_VIDEO_OUTPUT
	req.Memory = V4L2_MEMORY_USERPTR

	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(s.fd), VIDIOC_REQBUFS, uintptr(unsafe.Pointer(&req)))
	if errno != 0 {
		return fmt.Errorf("VIDIOC_REQBUFS (USERPTR) failed: %v", errno)
	}

	s.bufCount = int(req.Count)
	s.buffers = make([][]byte, req.Count)
	s.bufOffsets = nil // Not used for USERPTR
	s.curBufIdx = 0
	s.useUserPtr = true

	// Allocate userspace buffers
	for i := uint32(0); i < req.Count; i++ {
		s.buffers[i] = make([]byte, bufferSize)
		s.log.Info().
			Uint32("index", i).
			Int("size", bufferSize).
			Msg("UVC USERPTR buffer allocated")
	}

	s.log.Info().Int("count", int(req.Count)).Int("bufferSize", bufferSize).Msg("UVC USERPTR buffers allocated")
	return nil
}

// StartStreaming starts UVC video output
func (s *UVCStreamer) StartStreaming() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.fd < 0 {
		return fmt.Errorf("UVC device not open")
	}

	if s.streaming {
		return nil
	}

	// For OUTPUT devices, STREAMON first, then QBUF filled buffers
	bufType := uint32(V4L2_BUF_TYPE_VIDEO_OUTPUT)
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(s.fd), VIDIOC_STREAMON, uintptr(unsafe.Pointer(&bufType)))
	if errno != 0 {
		return fmt.Errorf("VIDIOC_STREAMON failed: %v", errno)
	}

	s.streaming = true
	s.curBufIdx = 0  // Start with first buffer
	s.queuedBufs = 0 // No buffers queued yet
	s.log.Info().Msg("UVC streaming started")
	return nil
}

// StopStreaming stops UVC video output
func (s *UVCStreamer) StopStreaming() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.stopStreamingLocked()
}

func (s *UVCStreamer) stopStreamingLocked() error {
	if s.fd < 0 || !s.streaming {
		return nil
	}

	bufType := uint32(V4L2_BUF_TYPE_VIDEO_OUTPUT)
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(s.fd), VIDIOC_STREAMOFF, uintptr(unsafe.Pointer(&bufType)))
	if errno != 0 {
		return fmt.Errorf("VIDIOC_STREAMOFF failed: %v", errno)
	}

	s.streaming = false
	s.queuedBufs = 0 // STREAMOFF dequeues all buffers
	s.curBufIdx = 0
	s.log.Info().Msg("UVC streaming stopped")
	return nil
}

// WriteFrame writes an MJPEG frame to the UVC device using MMAP buffers
// Workflow for VIDEO_OUTPUT: fill buffer -> QBUF -> (async) DQBUF when done
func (s *UVCStreamer) WriteFrame(data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.fd < 0 {
		return fmt.Errorf("UVC device not open")
	}

	if !s.streaming {
		return fmt.Errorf("UVC streaming not active")
	}

	if len(s.buffers) == 0 {
		return fmt.Errorf("no buffers allocated")
	}

	buf := s.buffers[s.curBufIdx]

	// Skip frames that are too large for the buffer
	if len(data) > len(buf) {
		// Don't log every dropped frame to avoid log spam
		return nil // Drop frame silently
	}

	// Determine memory type
	memType := uint32(V4L2_MEMORY_MMAP)
	if s.useUserPtr {
		memType = V4L2_MEMORY_USERPTR
	}

	// If all buffers are queued, we must DQBUF one first (blocking)
	for s.queuedBufs >= s.bufCount {
		var dqbuf v4l2_buffer
		dqbuf.Type = V4L2_BUF_TYPE_VIDEO_OUTPUT
		dqbuf.Memory = memType

		_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(s.fd), VIDIOC_DQBUF, uintptr(unsafe.Pointer(&dqbuf)))
		if errno != 0 {
			if errno == syscall.EAGAIN {
				// No buffer ready yet, drop this frame
				return nil
			}
			return fmt.Errorf("VIDIOC_DQBUF failed: %v", errno)
		}
		s.queuedBufs--
	}

	// Fill buffer with frame data
	bufIdx := s.curBufIdx
	copy(buf, data)

	// QBUF: Send filled buffer to driver
	var v4l2buf v4l2_buffer
	v4l2buf.Index = uint32(bufIdx)
	v4l2buf.Type = V4L2_BUF_TYPE_VIDEO_OUTPUT
	v4l2buf.Memory = memType
	v4l2buf.Field = V4L2_FIELD_NONE
	v4l2buf.BytesUsed = uint32(len(data))
	v4l2buf.Length = uint32(len(buf))

	// For USERPTR, set the buffer pointer
	if s.useUserPtr {
		v4l2buf.M = uintptr(unsafe.Pointer(&buf[0]))
	}

	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(s.fd), VIDIOC_QBUF, uintptr(unsafe.Pointer(&v4l2buf)))
	if errno != 0 {
		return fmt.Errorf("VIDIOC_QBUF failed for buffer %d: %v", bufIdx, errno)
	}

	s.queuedBufs++

	// Move to next buffer (round-robin)
	s.curBufIdx = (s.curBufIdx + 1) % s.bufCount

	return nil
}

// SubscribeEvents subscribes to UVC events
func (s *UVCStreamer) SubscribeEvents() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.fd < 0 {
		return fmt.Errorf("UVC device not open")
	}

	// Subscribe to UVC events
	events := []uint32{
		UVC_EVENT_CONNECT,
		UVC_EVENT_DISCONNECT,
		UVC_EVENT_STREAMON,
		UVC_EVENT_STREAMOFF,
		UVC_EVENT_SETUP,
		UVC_EVENT_DATA,
	}

	for _, eventType := range events {
		var sub v4l2_event_subscription
		sub.Type = eventType

		_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(s.fd), VIDIOC_SUBSCRIBE_EVENT, uintptr(unsafe.Pointer(&sub)))
		if errno != 0 {
			s.log.Warn().Uint32("event", eventType).Err(fmt.Errorf("%v", errno)).Msg("Failed to subscribe to UVC event")
		}
	}

	s.log.Info().Msg("Subscribed to UVC events")
	return nil
}

// PollEvents polls for UVC events (non-blocking)
// Returns event type or 0 if no event
func (s *UVCStreamer) PollEvents() (uint32, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.fd < 0 {
		return 0, fmt.Errorf("UVC device not open")
	}

	var ev v4l2_event
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(s.fd), VIDIOC_DQEVENT, uintptr(unsafe.Pointer(&ev)))
	if errno != 0 {
		if errno == syscall.EAGAIN || errno == syscall.EWOULDBLOCK {
			return 0, nil // No event available
		}
		return 0, fmt.Errorf("VIDIOC_DQEVENT failed: %v", errno)
	}

	return ev.Type, nil
}

// PollEventsWithData polls for UVC events and returns event data
// For SETUP events, returns the USB control request data
func (s *UVCStreamer) PollEventsWithData() (uint32, []byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.fd < 0 {
		return 0, nil, fmt.Errorf("UVC device not open")
	}

	var ev v4l2_event
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(s.fd), VIDIOC_DQEVENT, uintptr(unsafe.Pointer(&ev)))
	if errno != 0 {
		if errno == syscall.EAGAIN || errno == syscall.EWOULDBLOCK {
			return 0, nil, nil // No event available
		}
		return 0, nil, fmt.Errorf("VIDIOC_DQEVENT failed: %v", errno)
	}

	// Return a copy of the event data (first 64 bytes contain union data)
	data := make([]byte, 64)
	copy(data, ev.U[:])

	return ev.Type, data, nil
}

// pollfd structure for poll() syscall
type pollfd struct {
	fd      int32
	events  int16
	revents int16
}

const (
	POLLIN  = 0x0001
	POLLPRI = 0x0002 // For V4L2 events
)

// WaitForEventWithTimeout waits for a UVC event with timeout (in milliseconds)
// Returns event type, data, and error. Returns 0, nil, nil on timeout.
func (s *UVCStreamer) WaitForEventWithTimeout(timeoutMs int) (uint32, []byte, error) {
	s.mu.Lock()
	fd := s.fd
	s.mu.Unlock()

	if fd < 0 {
		return 0, nil, fmt.Errorf("UVC device not open")
	}

	// Use poll() to wait for POLLPRI (V4L2 events use priority data)
	pfd := pollfd{
		fd:     int32(fd),
		events: POLLPRI,
	}

	// SYS_POLL on ARM is 168
	const SYS_POLL = 168
	n, _, errno := syscall.Syscall(SYS_POLL, uintptr(unsafe.Pointer(&pfd)), 1, uintptr(timeoutMs))
	if errno != 0 {
		if errno == syscall.EINTR {
			return 0, nil, nil // Interrupted, treat as timeout
		}
		return 0, nil, fmt.Errorf("poll failed: %v", errno)
	}

	if n == 0 {
		// Timeout - no event
		return 0, nil, nil
	}

	// Event available - log revents for debugging
	s.log.Debug().
		Int("fd", int(pfd.fd)).
		Int("revents", int(pfd.revents)).
		Msg("poll() returned with events")

	// Event available, dequeue it
	return s.PollEventsWithData()
}

// HandleSetupEvent processes a UVC SETUP event and sends appropriate response
// This is critical for UVC enumeration - host will reset device if not responded
func (s *UVCStreamer) HandleSetupEvent(eventData []byte) error {
	// The v4l2_event.u contains uvc_event. On this platform, the usb_ctrlrequest
	// is at offset 4 within the event data (first 4 bytes are padding/type field).
	// struct usb_ctrlrequest is packed: bRequestType(1) + bRequest(1) + wValue(2) + wIndex(2) + wLength(2)
	if len(eventData) < 12 {
		return fmt.Errorf("event data too short: %d bytes", len(eventData))
	}

	// Parse USB control request from event data at offset 4
	// (first 4 bytes appear to be padding or a type field on this platform)
	const ctrlOffset = 4
	ctrl := usb_ctrlrequest{
		bRequestType: eventData[ctrlOffset+0],
		bRequest:     eventData[ctrlOffset+1],
		wValue:       binary.LittleEndian.Uint16(eventData[ctrlOffset+2 : ctrlOffset+4]),
		wIndex:       binary.LittleEndian.Uint16(eventData[ctrlOffset+4 : ctrlOffset+6]),
		wLength:      binary.LittleEndian.Uint16(eventData[ctrlOffset+6 : ctrlOffset+8]),
	}

	s.log.Debug().
		Uint32("bRequestType", uint32(ctrl.bRequestType)).
		Uint32("bRequest", uint32(ctrl.bRequest)).
		Uint32("wValue", uint32(ctrl.wValue)).
		Uint32("wIndex", uint32(ctrl.wIndex)).
		Uint32("wLength", uint32(ctrl.wLength)).
		Msg("UVC SETUP request received")

	// Check if this is a class-specific request
	if (ctrl.bRequestType & USB_TYPE_MASK) != USB_TYPE_CLASS {
		s.log.Debug().Msg("Not a class request, sending empty response")
		return s.sendEmptyResponse()
	}

	// Get control selector from wValue high byte
	cs := uint8(ctrl.wValue >> 8)
	// Get entity/unit ID from wIndex high byte
	entityId := uint8(ctrl.wIndex >> 8)
	// Get interface from wIndex low byte
	intf := uint8(ctrl.wIndex & 0xff)

	s.log.Debug().
		Uint32("cs", uint32(cs)).
		Uint32("entityId", uint32(entityId)).
		Uint32("intf", uint32(intf)).
		Bool("isInput", ctrl.bRequestType&USB_DIR_IN != 0).
		Msg("UVC class request details")

	// If entityId is non-zero, this is a control interface request to a specific unit
	// (camera terminal, processing unit, etc.)
	if entityId != 0 {
		return s.handleControlUnitRequest(&ctrl, cs, entityId)
	}

	// Entity ID 0 means this is a streaming interface request (VS_PROBE/VS_COMMIT)
	return s.handleStreamingRequest(&ctrl, cs)
}

// handleControlUnitRequest handles UVC control interface requests (camera terminal, processing unit)
func (s *UVCStreamer) handleControlUnitRequest(ctrl *usb_ctrlrequest, cs uint8, entityId uint8) error {
	s.log.Debug().
		Uint32("entityId", uint32(entityId)).
		Uint32("cs", uint32(cs)).
		Uint32("request", uint32(ctrl.bRequest)).
		Msg("Control unit request")

	switch ctrl.bRequest {
	case UVC_GET_INFO:
		// Return capabilities bitmap: bit 0 = GET supported, bit 1 = SET supported
		// For most controls, we support GET and SET
		return s.sendInfoResponse(0x03) // GET and SET supported

	case UVC_SET_CUR:
		// Host is setting a control value - accept the data transfer
		s.log.Debug().Uint32("cs", uint32(cs)).Msg("Control unit SET_CUR, accepting data")
		return s.sendAcceptResponse(int(ctrl.wLength))

	case UVC_GET_CUR, UVC_GET_MIN, UVC_GET_MAX, UVC_GET_DEF:
		// For control unit requests, return zeros with the requested length
		// This satisfies the host without implementing actual camera controls
		return s.sendZeroResponse(int(ctrl.wLength))

	case UVC_GET_RES:
		// Resolution (step size) - return 1 for most controls
		return s.sendZeroResponse(int(ctrl.wLength))

	case UVC_GET_LEN:
		// Return the length of the control value
		return s.sendLengthResponse(int(ctrl.wLength))

	default:
		s.log.Debug().Uint32("request", uint32(ctrl.bRequest)).Msg("Unknown control unit request")
		return s.sendEmptyResponse()
	}
}

// handleStreamingRequest handles UVC streaming interface control requests
func (s *UVCStreamer) handleStreamingRequest(ctrl *usb_ctrlrequest, cs uint8) error {
	switch ctrl.bRequest {
	case UVC_SET_CUR:
		// Host is setting a value - we'll receive it in a DATA event
		// Store which control is being set so we can handle the DATA event
		s.pendingCtrl = cs
		s.log.Debug().Uint32("cs", uint32(cs)).Msg("UVC SET_CUR request, waiting for DATA")
		// For SET_CUR, we must indicate we accept the data by responding with
		// the expected data length. This is critical for the USB handshake.
		return s.sendAcceptResponse(UVC_STREAMING_CONTROL_SIZE)

	case UVC_GET_CUR:
		return s.handleGetCurrent(cs)

	case UVC_GET_MIN, UVC_GET_MAX, UVC_GET_DEF:
		// For MIN/MAX/DEF, return the probe control (we support one format)
		return s.sendProbeControl()

	case UVC_GET_RES:
		// Resolution control - return zeros (we don't support resolution changes)
		return s.sendZeroProbeControl()

	case UVC_GET_LEN:
		// Return length of streaming control structure
		return s.sendControlLength()

	case UVC_GET_INFO:
		// Return supported requests bitmap
		return s.sendControlInfo()

	default:
		s.log.Warn().Uint32("request", uint32(ctrl.bRequest)).Msg("Unknown UVC request")
		return s.sendEmptyResponse()
	}
}

// handleGetCurrent handles GET_CUR requests
func (s *UVCStreamer) handleGetCurrent(cs uint8) error {
	switch cs {
	case UVC_VS_PROBE_CONTROL:
		s.log.Debug().Msg("GET_CUR PROBE")
		return s.sendProbeControl()
	case UVC_VS_COMMIT_CONTROL:
		s.log.Debug().Msg("GET_CUR COMMIT")
		return s.sendCommitControl()
	default:
		s.log.Warn().Uint32("cs", uint32(cs)).Msg("Unknown control selector for GET_CUR")
		return s.sendEmptyResponse()
	}
}

// HandleDataEvent processes UVC DATA event (receives data from SET_CUR)
// Returns true if this was a COMMIT event (streaming should be prepared)
func (s *UVCStreamer) HandleDataEvent(eventData []byte) (bool, error) {
	wasCommit := false

	if s.pendingCtrl == 0 {
		s.log.Debug().Msg("DATA event without pending control")
		return false, nil
	}

	ctrl := s.pendingCtrl
	s.pendingCtrl = 0

	s.log.Debug().
		Uint32("pendingCtrl", uint32(ctrl)).
		Int("eventDataLen", len(eventData)).
		Msg("UVC DATA event received")

	// The uvc_request_data structure is:
	// - length: int32 (4 bytes)
	// - data: [60]byte
	// Like SETUP events, DATA events have 4-byte padding at the start
	const dataOffset = 4
	if len(eventData) >= dataOffset+4 {
		// Data follows the length field (UVC 1.0 streaming control is 26 bytes)
		payloadOffset := dataOffset + 4
		available := len(eventData) - payloadOffset

		if available >= UVC_STREAMING_CONTROL_SIZE {
			// Parse the streaming control from host
			hostCtrl := s.parseStreamingControl(eventData[payloadOffset:])

			switch ctrl {
			case UVC_VS_PROBE_CONTROL:
				// Update our probe control with host's values
				s.updateProbeFromHost(hostCtrl)
				s.log.Debug().
					Uint32("formatIndex", uint32(s.probe.bFormatIndex)).
					Uint32("frameIndex", uint32(s.probe.bFrameIndex)).
					Msg("PROBE updated from host")

			case UVC_VS_COMMIT_CONTROL:
				// Commit the negotiated parameters
				s.commit = s.probe
				wasCommit = true
				s.log.Info().
					Uint32("formatIndex", uint32(s.commit.bFormatIndex)).
					Uint32("frameIndex", uint32(s.commit.bFrameIndex)).
					Msg("UVC parameters committed")
			}
		} else {
			s.log.Warn().
				Int("available", available).
				Int("required", UVC_STREAMING_CONTROL_SIZE).
				Msg("DATA payload too short for streaming control")
		}
	} else {
		s.log.Warn().
			Int("eventDataLen", len(eventData)).
			Msg("DATA event too short")
	}

	return wasCommit, nil
}

// parseStreamingControl parses a uvc_streaming_control from bytes
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
		dwClockFrequency:         binary.LittleEndian.Uint32(data[26:30]),
		bmFramingInfo:            data[30],
		bPreferedVersion:         data[31],
		bMinVersion:              data[32],
		bMaxVersion:              data[33],
	}
}

// updateProbeFromHost updates probe control with host's requested values
func (s *UVCStreamer) updateProbeFromHost(host uvc_streaming_control) {
	// Accept format/frame index from host if valid
	if host.bFormatIndex > 0 {
		s.probe.bFormatIndex = host.bFormatIndex
	}
	if host.bFrameIndex > 0 {
		s.probe.bFrameIndex = host.bFrameIndex
	}
	// Accept frame interval from host
	if host.dwFrameInterval > 0 {
		s.probe.dwFrameInterval = host.dwFrameInterval
	}
}

// sendProbeControl sends the current probe control as response
func (s *UVCStreamer) sendProbeControl() error {
	return s.sendStreamingControl(&s.probe)
}

// sendCommitControl sends the current commit control as response
func (s *UVCStreamer) sendCommitControl() error {
	return s.sendStreamingControl(&s.commit)
}

// sendStreamingControl sends a streaming control structure as UVCIOC_SEND_RESPONSE
// Uses UVC 1.0 format (26 bytes) for maximum compatibility
func (s *UVCStreamer) sendStreamingControl(ctrl *uvc_streaming_control) error {
	var resp uvc_request_data
	resp.Length = UVC_STREAMING_CONTROL_SIZE // 26 bytes for UVC 1.0

	s.log.Debug().
		Uint32("bFormatIndex", uint32(ctrl.bFormatIndex)).
		Uint32("bFrameIndex", uint32(ctrl.bFrameIndex)).
		Uint32("dwFrameInterval", ctrl.dwFrameInterval).
		Msg("Sending streaming control response")

	// Serialize the control structure (UVC 1.0 - 26 bytes)
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
	// UVC 1.0 stops here at 26 bytes

	return s.sendResponse(&resp)
}

// sendInfoResponse sends a 1-byte info/capabilities response
func (s *UVCStreamer) sendInfoResponse(info uint8) error {
	var resp uvc_request_data
	resp.Length = 1
	resp.Data[0] = info
	return s.sendResponse(&resp)
}

// sendZeroResponse sends a response filled with zeros of the specified length
func (s *UVCStreamer) sendZeroResponse(length int) error {
	var resp uvc_request_data
	if length > 60 {
		length = 60 // Max size of Data array
	}
	resp.Length = int32(length)
	// Data is already zeroed
	return s.sendResponse(&resp)
}

// sendLengthResponse sends a 2-byte length response
func (s *UVCStreamer) sendLengthResponse(length int) error {
	var resp uvc_request_data
	resp.Length = 2
	binary.LittleEndian.PutUint16(resp.Data[0:2], uint16(length))
	return s.sendResponse(&resp)
}

// sendZeroProbeControl sends a zeroed streaming control (for GET_RES)
func (s *UVCStreamer) sendZeroProbeControl() error {
	var resp uvc_request_data
	resp.Length = UVC_STREAMING_CONTROL_SIZE // 26 bytes for UVC 1.0
	// Data is already zeroed
	return s.sendResponse(&resp)
}

// sendControlLength sends the control length (for GET_LEN)
func (s *UVCStreamer) sendControlLength() error {
	var resp uvc_request_data
	resp.Length = 2
	binary.LittleEndian.PutUint16(resp.Data[0:2], UVC_STREAMING_CONTROL_SIZE) // 26 bytes for UVC 1.0
	return s.sendResponse(&resp)
}

// sendControlInfo sends supported requests bitmap (for GET_INFO)
func (s *UVCStreamer) sendControlInfo() error {
	var resp uvc_request_data
	resp.Length = 1
	resp.Data[0] = 0x03 // GET and SET supported
	return s.sendResponse(&resp)
}

// sendEmptyResponse sends an empty response (for unsupported requests)
func (s *UVCStreamer) sendEmptyResponse() error {
	var resp uvc_request_data
	resp.Length = 0
	return s.sendResponse(&resp)
}

// sendAcceptResponse sends a response accepting an OUT transfer with the expected data length
// This is used for SET_CUR requests to indicate we're ready to receive the data
func (s *UVCStreamer) sendAcceptResponse(length int) error {
	var resp uvc_request_data
	resp.Length = int32(length)
	// Data bytes remain zero - we're just indicating the expected length
	return s.sendResponse(&resp)
}

// sendResponse sends UVCIOC_SEND_RESPONSE ioctl
func (s *UVCStreamer) sendResponse(resp *uvc_request_data) error {
	s.mu.Lock()
	fd := s.fd
	s.mu.Unlock()

	if fd < 0 {
		return fmt.Errorf("UVC device not open")
	}

	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), UVCIOC_SEND_RESPONSE, uintptr(unsafe.Pointer(resp)))
	if errno != 0 {
		s.log.Warn().Err(fmt.Errorf("%v", errno)).Int("length", int(resp.Length)).Msg("UVCIOC_SEND_RESPONSE failed")
		return fmt.Errorf("UVCIOC_SEND_RESPONSE failed: %v", errno)
	}
	s.log.Debug().Int("length", int(resp.Length)).Msg("UVC response sent")
	return nil
}

// IsStreaming returns whether streaming is active
func (s *UVCStreamer) IsStreaming() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.streaming
}

// IsOpen returns whether the device is open
func (s *UVCStreamer) IsOpen() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fd >= 0
}

// IsValid checks if the device is still valid by attempting a capability query
func (s *UVCStreamer) IsValid() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.fd < 0 {
		return false
	}

	// Try VIDIOC_QUERYCAP to check if device is still valid
	var cap v4l2_capability
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(s.fd), VIDIOC_QUERYCAP, uintptr(unsafe.Pointer(&cap)))
	return errno == 0
}

// Reopen closes and reopens the device (useful for recovery after USB reset)
func (s *UVCStreamer) Reopen() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Close existing fd if any
	if s.fd >= 0 {
		s.streaming = false

		// Unmap all buffers before closing
		for _, buf := range s.buffers {
			if buf != nil {
				syscall.Munmap(buf)
			}
		}
		s.buffers = nil
		s.bufOffsets = nil
		s.bufCount = 0
		s.curBufIdx = 0

		syscall.Close(s.fd)
		s.fd = -1
	}

	// Reopen the device
	fd, err := syscall.Open(s.devicePath, syscall.O_RDWR|syscall.O_NONBLOCK, 0)
	if err != nil {
		return fmt.Errorf("failed to reopen UVC device %s: %w", s.devicePath, err)
	}

	s.fd = fd
	s.log.Info().Str("device", s.devicePath).Msg("UVC device reopened")
	return nil
}

// GetFd returns the file descriptor for use with select/poll
func (s *UVCStreamer) GetFd() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fd
}

// findUVCDevice finds the UVC gadget video device
func findUVCDevice() (string, error) {
	// Try common UVC gadget device paths
	paths := []string{"/dev/video11", "/dev/video10", "/dev/video9"}

	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			// Check if it's a UVC gadget device by reading its name
			nameFile := fmt.Sprintf("/sys/class/video4linux/%s/name", path[5:])
			data, err := os.ReadFile(nameFile)
			if err == nil {
				name := string(data)
				if containsAny(name, []string{"gadget", "dwc3", "uvc"}) {
					return path, nil
				}
			}
		}
	}

	// Fallback: check all video devices
	for i := 0; i < 20; i++ {
		path := fmt.Sprintf("/dev/video%d", i)
		if _, err := os.Stat(path); err == nil {
			nameFile := fmt.Sprintf("/sys/class/video4linux/video%d/name", i)
			data, err := os.ReadFile(nameFile)
			if err == nil {
				name := string(data)
				if containsAny(name, []string{"gadget", "dwc3", "uvc"}) {
					return path, nil
				}
			}
		}
	}

	return "", fmt.Errorf("no UVC gadget device found")
}

func containsAny(s string, substrs []string) bool {
	for _, sub := range substrs {
		if len(s) >= len(sub) {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
}

// Helper for converting uint32 to bytes (for ioctl)
func uint32ToBytes(v uint32) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, v)
	return b
}
