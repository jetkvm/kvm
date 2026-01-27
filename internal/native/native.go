package native

import (
	"os"
	"sync"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/jetkvm/kvm/internal/diagnostics"
	"github.com/rs/zerolog"
)

type Native struct {
	ready                chan struct{}
	l                    *zerolog.Logger
	lD                   *zerolog.Logger
	systemVersion        *semver.Version
	appVersion           *semver.Version
	displayRotation      uint16
	defaultQualityFactor float64
	onVideoStateChange   func(state VideoState)
	onVideoFrameReceived func(frame []byte, duration time.Duration)
	onJpegFrameReceived  func(frame []byte)
	onRGBFrameReceived   func(frame RGBFrame)
	onIndevEvent         func(event string)
	onRpcEvent           func(event string)
	sleepModeSupported   bool
	videoLock            sync.Mutex
	screenLock           sync.Mutex
	extraLock            sync.Mutex
}

type NativeOptions struct {
	SystemVersion        *semver.Version
	AppVersion           *semver.Version
	DisplayRotation      uint16
	DefaultQualityFactor float64
	MaxRestartAttempts   uint
	OnVideoStateChange   func(state VideoState)
	OnVideoFrameReceived func(frame []byte, duration time.Duration)
	OnJpegFrameReceived  func(frame []byte)
	OnRGBFrameReceived   func(frame RGBFrame)
	OnIndevEvent         func(event string)
	OnRpcEvent           func(event string)
	OnNativeRestart      func()
	// GetSessionInfo returns session diagnostics for crash logging.
	GetSessionInfo func() diagnostics.SessionInfo
}

type VideoStreamingStatus uint8

const (
	VideoStreamingStatusActive   VideoStreamingStatus = 1
	VideoStreamingStatusStopping VideoStreamingStatus = 2 // video is stopping, but not yet stopped
	VideoStreamingStatusInactive VideoStreamingStatus = 0
)

func (s VideoStreamingStatus) String() string {
	switch s {
	case VideoStreamingStatusActive:
		return "active"
	case VideoStreamingStatusStopping:
		return "stopping"
	case VideoStreamingStatusInactive:
		return "inactive"
	}
	return "unknown"
}

func NewNative(opts NativeOptions) *Native {
	pid := os.Getpid()
	nativeSubLogger := nativeLogger.With().Int("pid", pid).Str("scope", "native").Logger()
	displaySubLogger := displayLogger.With().Int("pid", pid).Str("scope", "native").Logger()

	onVideoStateChange := opts.OnVideoStateChange
	if onVideoStateChange == nil {
		onVideoStateChange = func(state VideoState) {
			nativeLogger.Info().Interface("state", state).Msg("video state changed")
		}
	}

	onVideoFrameReceived := opts.OnVideoFrameReceived
	if onVideoFrameReceived == nil {
		onVideoFrameReceived = func(frame []byte, duration time.Duration) {
			nativeLogger.Trace().Interface("frame", frame).Dur("duration", duration).Msg("video frame received")
		}
	}

	onIndevEvent := opts.OnIndevEvent
	if onIndevEvent == nil {
		onIndevEvent = func(event string) {
			nativeLogger.Info().Str("event", event).Msg("indev event")
		}
	}

	onRpcEvent := opts.OnRpcEvent
	if onRpcEvent == nil {
		onRpcEvent = func(event string) {
			nativeLogger.Info().Str("event", event).Msg("rpc event")
		}
	}

	onJpegFrameReceived := opts.OnJpegFrameReceived
	if onJpegFrameReceived == nil {
		onJpegFrameReceived = func(frame []byte) {
			nativeLogger.Trace().Int("frameSize", len(frame)).Msg("JPEG frame received")
		}
	}

	onRGBFrameReceived := opts.OnRGBFrameReceived
	if onRGBFrameReceived == nil {
		onRGBFrameReceived = func(frame RGBFrame) {
			nativeLogger.Trace().Int("frameSize", len(frame.Data)).Uint32("width", frame.Width).Uint32("height", frame.Height).Msg("RGB frame received")
		}
	}

	sleepModeSupported := isSleepModeSupported()

	defaultQualityFactor := opts.DefaultQualityFactor
	if defaultQualityFactor <= 0 || defaultQualityFactor > 1 {
		defaultQualityFactor = 1.0
	}

	return &Native{
		ready:                make(chan struct{}),
		l:                    &nativeSubLogger,
		lD:                   &displaySubLogger,
		systemVersion:        opts.SystemVersion,
		appVersion:           opts.AppVersion,
		displayRotation:      opts.DisplayRotation,
		defaultQualityFactor: defaultQualityFactor,
		onVideoStateChange:   onVideoStateChange,
		onVideoFrameReceived: onVideoFrameReceived,
		onJpegFrameReceived:  onJpegFrameReceived,
		onRGBFrameReceived:   onRGBFrameReceived,
		onIndevEvent:         onIndevEvent,
		onRpcEvent:           onRpcEvent,
		sleepModeSupported:   sleepModeSupported,
		videoLock:            sync.Mutex{},
		screenLock:           sync.Mutex{},
	}
}

func (n *Native) Start() error {
	// set up singleton
	setInstance(n)
	setUpNativeHandlers()

	// start the native video
	go n.handleLogChan()
	go n.handleVideoStateChan()
	go n.handleVideoFrameChan()
	go n.handleJpegFrameChan()
	go n.handleRGBFrameChan()
	go n.handleIndevEventChan()
	go n.handleRpcEventChan()

	n.initUI()
	go n.tickUI()

	if err := videoInit(n.defaultQualityFactor); err != nil {
		n.l.Error().Err(err).Msg("failed to initialize video")
		return err
	}

	close(n.ready)
	return nil
}

// DoNotUseThisIsForCrashTestingOnly
// will crash the program in cgo code
func (n *Native) DoNotUseThisIsForCrashTestingOnly() {
	defer func() {
		if r := recover(); r != nil {
			n.l.Error().Msg("recovered from crash")
		}
	}()

	crash()
}

// GetLVGLVersion returns the LVGL version
func GetLVGLVersion() string {
	return uiGetLVGLVersion()
}

// JPEG encoder public API

// StartJPEGEncoder starts the hardware JPEG encoder with the given quality (1-99)
func StartJPEGEncoder(quality int) error {
	return jpegStart(quality)
}

// StopJPEGEncoder stops the hardware JPEG encoder
func StopJPEGEncoder() {
	jpegStop()
}

// SetJPEGQuality sets the JPEG encoder quality (1-99)
func SetJPEGQuality(quality int) error {
	return jpegSetQuality(quality)
}

// GetJPEGQuality returns the current JPEG encoder quality
func GetJPEGQuality() int {
	return jpegGetQuality()
}

// IsJPEGEncoderRunning returns true if the JPEG encoder is running
func IsJPEGEncoderRunning() bool {
	return jpegIsRunning()
}

// GetJPEGFrameChannel returns the channel for receiving JPEG frames
func GetJPEGFrameChannel() <-chan []byte {
	return jpegFrameChan
}

// Native instance methods that implement NativeInterface

// JpegStart starts the hardware JPEG encoder with the given quality (1-99)
func (n *Native) JpegStart(quality int) error {
	return jpegStart(quality)
}

// JpegStop stops the hardware JPEG encoder
func (n *Native) JpegStop() error {
	jpegStop()
	return nil
}

// JpegSetQuality sets the JPEG encoder quality (1-99)
func (n *Native) JpegSetQuality(quality int) error {
	return jpegSetQuality(quality)
}

// JpegGetQuality returns the current JPEG encoder quality
func (n *Native) JpegGetQuality() (int, error) {
	return jpegGetQuality(), nil
}

// JpegIsRunning returns true if the JPEG encoder is running
func (n *Native) JpegIsRunning() (bool, error) {
	return jpegIsRunning(), nil
}

// VideoRequestKeyframe requests an IDR (keyframe) from the H.264 encoder
func (n *Native) VideoRequestKeyframe() error {
	return videoRequestKeyframe()
}

// RGA RGB encoder public API (hardware YUV to BGRX conversion)

// RgbStart starts the RGA RGB encoder for hardware YUV to BGRX conversion
func (n *Native) RgbStart() error {
	return rgbStart()
}

// RgbStop stops the RGA RGB encoder
func (n *Native) RgbStop() error {
	rgbStop()
	return nil
}

// RgbIsRunning returns true if the RGA RGB encoder is running
func (n *Native) RgbIsRunning() (bool, error) {
	return rgbIsRunning(), nil
}

// H.264 to MJPEG Transcoder API (BETA feature)
// Used for camera redirection when RDP client only sends H.264

// TranscodeInit initializes the H.264 to MJPEG transcoder.
// inputWidth/inputHeight: source resolution (0 = auto-detect for H.264)
// outputWidth/outputHeight: target resolution for RGA scaling (0 = same as input)
// outputCb is called with MJPEG frame data for each successfully transcoded frame.
// WARNING: This is a BETA feature with high CPU usage (~80-100% on Cortex-A7).
func (n *Native) TranscodeInit(inputWidth, inputHeight, outputWidth, outputHeight, fps, quality uint32, outputCb func([]byte)) error {
	return transcodeInit(inputWidth, inputHeight, outputWidth, outputHeight, fps, quality, outputCb)
}

// TranscodeShutdown shuts down the transcoder and releases all resources.
func (n *Native) TranscodeShutdown() {
	transcodeShutdown()
}

// TranscodeIsRunning returns true if the transcoder is initialized and running.
func (n *Native) TranscodeIsRunning() bool {
	return transcodeIsRunning()
}

// TranscodeFeedH264 feeds an H.264 NAL unit or access unit to the transcoder.
// Returns error if transcoder is not running or if decode fails.
// This is the slowest path as it requires software H.264 decoding.
func (n *Native) TranscodeFeedH264(data []byte) error {
	return transcodeFeedH264(data)
}

// TranscodeFeedNV12 sends NV12 data directly to the hardware MJPEG encoder.
// This is the fastest path - no conversion needed.
func (n *Native) TranscodeFeedNV12(data []byte) error {
	return transcodeFeedNV12(data)
}

// TranscodeFeedI420 converts I420 to NV12 using NEON, then hardware encodes.
func (n *Native) TranscodeFeedI420(data []byte) error {
	return transcodeFeedI420(data)
}

// TranscodeFeedYUY2 converts YUY2 to NV12 using NEON, then hardware encodes.
func (n *Native) TranscodeFeedYUY2(data []byte) error {
	return transcodeFeedYUY2(data)
}

// GetRGBFrameChannel returns the channel for receiving raw RGB frames
func GetRGBFrameChannel() <-chan RGBFrame {
	return rgbFrameChan
}
