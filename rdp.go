package kvm

import (
	"crypto/tls"
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jetkvm/kvm/internal/camera"
	cryptotls "github.com/jetkvm/kvm/internal/crypto/tls"
	"github.com/jetkvm/kvm/internal/keyboard"
	"github.com/jetkvm/kvm/internal/rdp"
)

// RDP frame subscribers using generic Subscriber pattern
var (
	rdpH264Subscribers Subscriber[[]byte]
	rdpJPEGSubscribers Subscriber[[]byte]
	rdpRGBSubscribers  Subscriber[rdp.RGBFrame]
	rdpAudioSubs       Subscriber[[]byte]
)

var (
	rdpServer     *rdp.Server
	rdpServerOnce sync.Once
)

// GetRDPServer returns the global RDP server instance.
func GetRDPServer() *rdp.Server {
	rdpServerOnce.Do(func() {
		// Determine if TLS is available for clipboard file server
		tlsEnabled := config.TLSMode == "self-signed" || config.TLSMode == "custom"

		deps := rdp.Dependencies{
			Logger:         *rdpLogger,
			Config:         &rdpConfigAdapter{},
			HID:            &rdpHIDAdapter{},
			Video:          &rdpVideoAdapter{},
			Audio:          &rdpAudioAdapter{},
			Camera:         &rdpCameraAdapter{},
			TLS:            &rdpTLSAdapter{},
			TLSEnabled:     tlsEnabled,
			GetCertificate: getCertificate,
			USBStorage:     &rdpUSBStorageAdapter{},
			ClipboardStore: GetClipboardStore(),
			// Track RDP sessions for sleep mode prevention
			OnSessionStart: func() {
				count := incrActiveSessions()
				rdpLogger.Debug().Int("activeSessions", count).Msg("RDP: session started, incremented active sessions")
			},
			OnSessionEnd: func() {
				count := decrActiveSessions()
				rdpLogger.Debug().Int("activeSessions", count).Msg("RDP: session ended, decremented active sessions")
			},
		}
		rdpServer = rdp.NewServer(deps)
	})
	return rdpServer
}

type rdpConfigAdapter struct{}

func (a *rdpConfigAdapter) GetRDPEnabled() bool {
	return config.RDPEnabled
}

func (a *rdpConfigAdapter) GetRDPPort() int {
	return config.RDPPort
}

func (a *rdpConfigAdapter) GetRDPMaxConnections() int {
	return config.RDPMaxConnections
}

func (a *rdpConfigAdapter) GetRDPClipboardEnabled() bool {
	return config.RDPClipboardEnabled
}

func (a *rdpConfigAdapter) GetRDPVideoEnabled() bool {
	return config.RDPVideoEnabled
}

func (a *rdpConfigAdapter) GetRDPAudioEnabled() bool {
	return config.RDPAudioEnabled
}

func (a *rdpConfigAdapter) GetRDPMicEnabled() bool {
	return config.RDPMicEnabled
}

func (a *rdpConfigAdapter) GetRDPCameraEnabled() bool {
	return config.RDPCameraEnabled
}

func (a *rdpConfigAdapter) GetRDPCameraTranscodeEnabled() bool {
	return config.RDPCameraTranscodeEnabled
}

func (a *rdpConfigAdapter) GetCameraFrameRate() int {
	if config.CameraFrameRate <= 0 {
		return cameraDefaultFPS
	}
	return config.CameraFrameRate
}

func (a *rdpConfigAdapter) GetCameraMjpegQuality() int {
	if config.CameraMjpegQuality <= 0 {
		return cameraDefaultMjpegQual
	}
	return config.CameraMjpegQuality
}

func (a *rdpConfigAdapter) GetTLSMode() string {
	return config.TLSMode
}

func (a *rdpConfigAdapter) GetHashedPassword() string {
	return config.HashedPassword
}

func (a *rdpConfigAdapter) GetLocalAuthPassword() string {
	return config.LocalAuthPassword
}

func (a *rdpConfigAdapter) GetRDPUsername() string {
	return config.RDPUsername
}

func (a *rdpConfigAdapter) GetRDPDomain() string {
	return config.RDPDomain
}

func (a *rdpConfigAdapter) GetRDPTargetOS() string {
	if config.RDPTargetOS == "" {
		return "windows"
	}
	return config.RDPTargetOS
}

func (a *rdpConfigAdapter) GetRDPFileTransferEnabled() bool {
	return config.RDPFileTransferEnabled
}

func (a *rdpConfigAdapter) GetRDPFileTransferMethod() string {
	if config.RDPFileTransferMethod == "" {
		return "auto"
	}
	return config.RDPFileTransferMethod
}

func (a *rdpConfigAdapter) GetRDPFileTransferMaxMB() int {
	if config.RDPFileTransferMaxMB == 0 {
		return 100
	}
	return config.RDPFileTransferMaxMB
}

func (a *rdpConfigAdapter) GetRDPFileTransferTTLSec() int {
	if config.RDPFileTransferTTLSec == 0 {
		return 300 // 5 minutes default
	}
	return config.RDPFileTransferTTLSec
}

func (a *rdpConfigAdapter) GetRDPFileTransferCleanupSec() int {
	if config.RDPFileTransferCleanupSec == 0 {
		return 60 // 1 minute default
	}
	return config.RDPFileTransferCleanupSec
}

func (a *rdpConfigAdapter) GetRDPNetworkCmdWindows() string {
	return config.RDPNetworkCmdWindows
}

func (a *rdpConfigAdapter) GetRDPNetworkCmdLinux() string {
	return config.RDPNetworkCmdLinux
}

func (a *rdpConfigAdapter) GetRDPNetworkCmdMacOS() string {
	return config.RDPNetworkCmdMacOS
}

func (a *rdpConfigAdapter) GetRDPBase64CmdWindows() string {
	return config.RDPBase64CmdWindows
}

func (a *rdpConfigAdapter) GetRDPBase64CmdLinux() string {
	return config.RDPBase64CmdLinux
}

func (a *rdpConfigAdapter) GetRDPBase64CmdMacOS() string {
	return config.RDPBase64CmdMacOS
}

type rdpHIDAdapter struct{}

func (a *rdpHIDAdapter) KeypressReport(hidCode uint8, pressed bool) error {
	return rpcKeypressReport(hidCode, pressed)
}

func (a *rdpHIDAdapter) AbsMouseReport(x, y int, buttons byte) error {
	return rpcAbsMouseReport(x, y, buttons)
}

func (a *rdpHIDAdapter) WheelReport(vertical, horizontal int8) error {
	return rpcWheelReport(vertical, horizontal)
}

func (a *rdpHIDAdapter) KeyboardMacro(text string) error {
	if len(text) > keyboard.MaxClipboardSize {
		rdpLogger.Warn().
			Int("size", len(text)).
			Int("maxSize", keyboard.MaxClipboardSize).
			Msg("clipboard text exceeds maximum size, truncating")
		text = text[:keyboard.MaxClipboardSize]
	}

	if len(text) == 0 {
		return nil
	}

	mode := keyboard.ClipboardMode(config.RDPClipboardMode)
	if mode == "" {
		mode = keyboard.ClipboardModeText
	}
	targetOS := keyboard.TargetOS(config.RDPTargetOS)
	if targetOS == "" {
		targetOS = keyboard.TargetOSWindows
	}

	preparedText, encoded := keyboard.PrepareClipboardText([]byte(text), mode, targetOS)
	if preparedText == "" {
		rdpLogger.Warn().
			Str("mode", string(mode)).
			Str("targetOS", string(targetOS)).
			Int("inputLen", len(text)).
			Msg("clipboard text preparation resulted in empty string - text may contain only unsupported characters")
		return nil
	}

	if encoded {
		rdpLogger.Info().
			Str("mode", string(mode)).
			Str("targetOS", string(targetOS)).
			Int("originalLen", len(text)).
			Int("encodedLen", len(preparedText)).
			Msg("clipboard content encoded")
	}

	totalDelay := config.RDPPasteDelayMs
	if totalDelay < 0 {
		totalDelay = 0
	}
	pressDelay, releaseDelay := keyboard.ComputeDelays(totalDelay)

	steps, skipped := keyboard.TextToMacroSteps(preparedText, "en-US", pressDelay, releaseDelay)
	if len(steps) == 0 {
		rdpLogger.Warn().
			Int("inputLen", len(preparedText)).
			Msg("no keyboard macro steps generated - text may contain only unmappable characters")
		return nil
	}

	if skipped > 0 {
		rdpLogger.Debug().Int("skipped", skipped).Msg("characters skipped during paste")
	}

	return rpcExecuteKeyboardMacro(steps)
}

func (a *rdpHIDAdapter) IsKeyboardMacroInProgress() bool {
	return isKeyboardMacroInProgress()
}

func (a *rdpHIDAdapter) CancelKeyboardMacro() {
	cancelKeyboardMacro()
}

type rdpVideoAdapter struct{}

func (a *rdpVideoAdapter) GetResolution() (width, height uint16) {
	w, h := lastVideoState.Width, lastVideoState.Height
	if w > 0 && h > 0 {
		return uint16(w), uint16(h)
	}
	return 1920, 1080 // Default
}

func (a *rdpVideoAdapter) StartVideo() error {
	if nativeInstance == nil {
		return nil
	}
	return nativeInstance.VideoStart()
}

func (a *rdpVideoAdapter) StopVideo() error {
	// Video is shared with WebRTC - native layer manages lifecycle
	return nil
}

func (a *rdpVideoAdapter) SubscribeH264() <-chan []byte {
	return rdpH264Subscribers.Subscribe(10)
}

func (a *rdpVideoAdapter) UnsubscribeH264(ch <-chan []byte) {
	rdpH264Subscribers.Unsubscribe(ch)
}

// BroadcastRDPH264Frame sends an H.264 frame to all RDP subscribers.
func BroadcastRDPH264Frame(frame []byte) {
	rdpH264Subscribers.Broadcast(frame)
}

func (a *rdpVideoAdapter) SubscribeJPEG() <-chan []byte {
	return rdpJPEGSubscribers.Subscribe(5)
}

func (a *rdpVideoAdapter) UnsubscribeJPEG(ch <-chan []byte) {
	rdpJPEGSubscribers.Unsubscribe(ch)
}

func (a *rdpVideoAdapter) StartJPEGEncoder(quality int) error {
	if nativeInstance == nil {
		return nil
	}
	return nativeInstance.JpegStart(quality)
}

func (a *rdpVideoAdapter) StopJPEGEncoder() error {
	if nativeInstance == nil {
		return nil
	}
	return nativeInstance.JpegStop()
}

// BroadcastRDPJPEGFrame sends a JPEG frame to all RDP JPEG subscribers.
func BroadcastRDPJPEGFrame(frame []byte) {
	rdpJPEGSubscribers.Broadcast(frame)
}

func (a *rdpVideoAdapter) SubscribeRGB() <-chan rdp.RGBFrame {
	return rdpRGBSubscribers.Subscribe(5)
}

func (a *rdpVideoAdapter) UnsubscribeRGB(ch <-chan rdp.RGBFrame) {
	rdpRGBSubscribers.Unsubscribe(ch)
}

func (a *rdpVideoAdapter) StartRGBEncoder() error {
	if nativeInstance == nil {
		return nil
	}
	return nativeInstance.RgbStart()
}

func (a *rdpVideoAdapter) StopRGBEncoder() error {
	if nativeInstance == nil {
		return nil
	}
	return nativeInstance.RgbStop()
}

func (a *rdpVideoAdapter) RequestKeyframe() {
	if nativeInstance == nil {
		return
	}
	if err := nativeInstance.VideoRequestKeyframe(); err != nil {
		rdpLogger.Debug().Err(err).Msg("keyframe request failed")
	}
}

// BroadcastRDPRGBFrame sends a video frame to all RDP RGB subscribers.
func BroadcastRDPRGBFrame(data []byte, width, height uint32, format rdp.RGBFrameFormat) {
	rdpRGBSubscribers.Broadcast(rdp.RGBFrame{
		Data:   data,
		Width:  width,
		Height: height,
		Format: format,
	})
}

type rdpAudioAdapter struct{}

func (a *rdpAudioAdapter) Connect() {
	OnRDPAudioConnect()
}

func (a *rdpAudioAdapter) Disconnect() {
	OnRDPAudioDisconnect()
}

func (a *rdpAudioAdapter) SubscribeAudio() <-chan []byte {
	return rdpAudioSubs.Subscribe(30)
}

func (a *rdpAudioAdapter) UnsubscribeAudio(ch <-chan []byte) {
	rdpAudioSubs.Unsubscribe(ch)
}

var monoBufferPool = sync.Pool{
	New: func() interface{} {
		buf := make([]byte, 960)
		return &buf
	},
}

func (a *rdpAudioAdapter) PlayAudio(data []byte) error {
	// AUDIN channel sends 16-bit stereo PCM per MS-RDPEAI 2.2.3.1
	// USB audio gadget expects mono - convert by averaging L+R
	if len(data) < 4 {
		return nil
	}

	bufPtr := monoBufferPool.Get().(*[]byte)
	monoBuf := *bufPtr

	stereoFrames := len(data) / 4
	monoBytes := stereoFrames * 2

	if monoBytes > cap(monoBuf) {
		monoBuf = make([]byte, monoBytes)
	} else {
		monoBuf = monoBuf[:monoBytes]
	}

	for i := 0; i < stereoFrames; i++ {
		srcIdx := i * 4
		dstIdx := i * 2
		left := int16(data[srcIdx]) | int16(data[srcIdx+1])<<8
		right := int16(data[srcIdx+2]) | int16(data[srcIdx+3])<<8
		mono := int16((int32(left) + int32(right)) / 2)
		monoBuf[dstIdx] = byte(mono)
		monoBuf[dstIdx+1] = byte(mono >> 8)
	}

	err := WriteInputPCM(monoBuf)

	*bufPtr = monoBuf[:cap(monoBuf)]
	monoBufferPool.Put(bufPtr)

	return err
}

func (a *rdpAudioAdapter) EnableAudioInput() error {
	return SetAudioInputEnabled(true)
}

// HasRDPAudioSubscribers returns true if there are RDP audio subscribers.
// Used to skip PCM processing when no RDP clients need audio.
func HasRDPAudioSubscribers() bool {
	return rdpAudioSubs.HasSubscribers()
}

// BroadcastRDPAudio sends audio data to all RDP audio subscribers.
func BroadcastRDPAudio(data []byte) {
	rdpAudioSubs.Broadcast(data)
}

type rdpCameraAdapter struct{}

// MS-RDPECAM pixel format codes
const (
	rdpCamPixelFormatH264  = 0x01
	rdpCamPixelFormatMJPEG = 0x02
	rdpCamPixelFormatYUY2  = 0x03
	rdpCamPixelFormatNV12  = 0x04
	rdpCamPixelFormatI420  = 0x05
)

// Camera default values and logging intervals
const (
	cameraDefaultFPS        = 24    // Default frame rate when not specified
	cameraDefaultMjpegQual  = 35    // Default MJPEG quality (0-100)
	cameraLogWarningEvery   = 500   // Log transcode errors every N frames
	cameraLogDropEvery      = 200   // Log queue drops every N frames
	cameraFrameQueueSize    = 8     // Buffer for slow software H.264 decode (~250ms latency at 30fps)
	cameraBufferInitialSize = 512 * 1024 // 512KB - typical H.264 frame size
)

// cameraFrame represents a camera frame waiting to be transcoded.
type cameraFrame struct {
	data        []byte
	poolBuf     *[]byte // Reference to pooled buffer for return
	width       uint32
	height      uint32
	pixelFormat uint32
}

// Frame buffer pool to reduce GC pressure - reuse buffers across frames
var cameraBufferPool = sync.Pool{
	New: func() interface{} {
		buf := make([]byte, cameraBufferInitialSize)
		return &buf
	},
}

// cameraBufferMaxSize is the maximum buffer size to keep in the pool.
// Buffers larger than this are discarded to avoid pool pollution from occasional large frames.
const cameraBufferMaxSize = 1024 * 1024 // 1MB

var (
	cameraFrameCount             atomic.Uint32
	cameraH264WarningLogged      atomic.Bool
	cameraTranscodeWarningLogged atomic.Bool
	cameraTranscodeInitialized   atomic.Bool
	cameraTranscodeInitFailed    atomic.Bool // Set when init fails to prevent repeated attempts
	cameraTranscodeMu            sync.Mutex
	cameraFrameQueue             chan cameraFrame
	cameraFrameQueueStop         chan struct{}
	cameraFrameQueueDone         chan struct{} // Signals worker goroutine has exited
	cameraFrameQueueRunning      atomic.Bool
	cameraFrameDropped           atomic.Uint64
	cameraLastFrameTime          atomic.Pointer[time.Time] // For rate limiting (monotonic)
)

// initCameraTranscoder initializes the H.264 to MJPEG transcoder if enabled.
// inputWidth/inputHeight: source resolution from RDP client
// outputWidth/outputHeight: target resolution for USB host (0 = same as input)
// hostFPS: FPS requested by USB host (will be capped by config.CameraFrameRate)
// Returns true if transcoder was initialized or is already running.
func initCameraTranscoder(inputWidth, inputHeight, outputWidth, outputHeight, hostFPS uint32) bool {
	// Check if transcode is enabled in config
	if !config.RDPCameraTranscodeEnabled {
		return false
	}

	// Don't retry if initialization already failed (e.g., subprocess mode doesn't support it)
	if cameraTranscodeInitFailed.Load() {
		return false
	}

	// Fast path: already initialized
	if cameraTranscodeInitialized.Load() {
		if nativeInstance != nil && nativeInstance.TranscodeIsRunning() {
			return true
		}
	}

	cameraTranscodeMu.Lock()
	defer cameraTranscodeMu.Unlock()

	// Double-check after acquiring lock
	if cameraTranscodeInitialized.Load() {
		if nativeInstance != nil && nativeInstance.TranscodeIsRunning() {
			return true
		}
	}

	if nativeInstance == nil {
		return false
	}

	// FPS: use min(host_requested, config_cap) to respect both
	configFPS := uint32(config.CameraFrameRate)
	if configFPS <= 0 {
		configFPS = cameraDefaultFPS
	}
	fps := hostFPS
	if fps == 0 || fps > configFPS {
		fps = configFPS
	}

	quality := uint32(config.CameraMjpegQuality)
	if quality <= 0 {
		quality = cameraDefaultMjpegQual
	}

	rdpLogger.Debug().
		Uint32("in", inputWidth).
		Uint32("out", outputWidth).
		Uint32("fps", fps).
		Uint32("q", quality).
		Msg("Transcode init")

	// Initialize transcoder with callback that sends MJPEG to camera manager
	err := nativeInstance.TranscodeInit(inputWidth, inputHeight, outputWidth, outputHeight, fps, quality, func(jpegData []byte) {
		mgr := cameraManagerPtr.Load()
		if mgr != nil && mgr.IsEnabled() {
			mgr.HandleCameraMjpegFrame(jpegData)
		}
	})

	if err != nil {
		rdpLogger.Warn().Err(err).Msg("RDP: failed to initialize transcoder")
		// Mark as failed to prevent repeated attempts (especially in subprocess mode)
		cameraTranscodeInitFailed.Store(true)
		return false
	}

	cameraTranscodeInitialized.Store(true)
	cameraTranscodeInitFailed.Store(false) // Reset failed flag on success
	return true
}

// startCameraFrameWorker starts the async frame processing goroutine.
// The worker runs at lower effective priority by yielding after each frame,
// ensuring HID and video handlers get CPU time even during heavy transcoding.
func startCameraFrameWorker() {
	if cameraFrameQueueRunning.Load() {
		return
	}

	cameraTranscodeMu.Lock()
	defer cameraTranscodeMu.Unlock()

	if cameraFrameQueueRunning.Load() {
		return
	}

	cameraFrameQueue = make(chan cameraFrame, cameraFrameQueueSize)
	cameraFrameQueueStop = make(chan struct{})
	cameraFrameQueueDone = make(chan struct{})
	cameraFrameQueueRunning.Store(true)

	go func() {
		defer close(cameraFrameQueueDone)
		rdpLogger.Debug().Msg("Camera worker start")
		for {
			select {
			case <-cameraFrameQueueStop:
				rdpLogger.Debug().Msg("Camera worker stop")
				return
			case frame := <-cameraFrameQueue:
				processCameraFrameSync(frame)
				// Yield CPU to allow HID/video goroutines to run
				// This is critical - transcoding is CPU-heavy and would starve other work
				runtime.Gosched()
			}
		}
	}()
}

// stopCameraFrameWorker stops the async frame processing goroutine.
func stopCameraFrameWorker() {
	if !cameraFrameQueueRunning.Load() {
		return
	}

	cameraTranscodeMu.Lock()
	defer cameraTranscodeMu.Unlock()

	if !cameraFrameQueueRunning.Load() {
		return
	}

	close(cameraFrameQueueStop)
	cameraFrameQueueRunning.Store(false)

	// Wait for worker to exit before draining to avoid race condition
	// where worker and drain loop both try to process the same frame
	<-cameraFrameQueueDone

	// Drain any remaining frames, returning pooled buffers
	for {
		select {
		case frame := <-cameraFrameQueue:
			returnCameraBuffer(frame.poolBuf)
		default:
			return
		}
	}
}

// returnCameraBuffer returns a buffer to the pool if it's not oversized.
// Buffers larger than cameraBufferMaxSize are discarded to avoid pool pollution.
func returnCameraBuffer(bufPtr *[]byte) {
	if bufPtr == nil {
		return
	}
	if cap(*bufPtr) <= cameraBufferMaxSize {
		cameraBufferPool.Put(bufPtr)
	}
	// Oversized buffers are left for GC
}

// processCameraFrameSync processes a single camera frame (called from worker goroutine).
func processCameraFrameSync(frame cameraFrame) {
	// Always return buffer to pool when done (unless oversized)
	defer returnCameraBuffer(frame.poolBuf)

	mgr := cameraManagerPtr.Load()
	if mgr == nil || !mgr.IsEnabled() {
		return
	}

	currentFmt := mgr.GetCurrentFormat()
	hostWantsMJPEG := currentFmt != nil && currentFmt.Codec == camera.CodecMJPEG

	// Get host's requested output dimensions and FPS for RGA scaling
	var outW, outH, hostFPS uint32
	if currentFmt != nil {
		outW, outH = uint32(currentFmt.Width), uint32(currentFmt.Height)
		hostFPS = uint32(currentFmt.FrameRate)
	}

	switch frame.pixelFormat {
	case rdpCamPixelFormatMJPEG:
		mgr.HandleCameraMjpegFrame(frame.data)

	case rdpCamPixelFormatNV12:
		if hostWantsMJPEG && initCameraTranscoder(frame.width, frame.height, outW, outH, hostFPS) {
			if err := nativeInstance.TranscodeFeedNV12(frame.data); err != nil {
				count := cameraFrameCount.Add(1)
				if count == 1 || count%1000 == 0 {
					rdpLogger.Debug().Err(err).Uint32("n", count).Msg("NV12 encode err")
				}
			}
		}

	case rdpCamPixelFormatI420:
		if hostWantsMJPEG && initCameraTranscoder(frame.width, frame.height, outW, outH, hostFPS) {
			if err := nativeInstance.TranscodeFeedI420(frame.data); err != nil {
				count := cameraFrameCount.Add(1)
				if count == 1 || count%1000 == 0 {
					rdpLogger.Debug().Err(err).Uint32("n", count).Msg("I420 encode err")
				}
			}
		}

	case rdpCamPixelFormatYUY2:
		if hostWantsMJPEG && initCameraTranscoder(frame.width, frame.height, outW, outH, hostFPS) {
			if err := nativeInstance.TranscodeFeedYUY2(frame.data); err != nil {
				count := cameraFrameCount.Add(1)
				if count == 1 || count%1000 == 0 {
					rdpLogger.Debug().Err(err).Uint32("n", count).Msg("YUY2 encode err")
				}
			}
		}

	case rdpCamPixelFormatH264:
		if hostWantsMJPEG {
			if initCameraTranscoder(frame.width, frame.height, outW, outH, hostFPS) {
				if err := nativeInstance.TranscodeFeedH264(frame.data); err != nil {
					count := cameraFrameCount.Add(1)
					if !cameraTranscodeWarningLogged.Load() || count%cameraLogWarningEvery == 0 {
						rdpLogger.Warn().Err(err).Uint32("n", count).Msg("Transcode err")
						cameraTranscodeWarningLogged.Store(true)
					}
				}
				return
			}
			// Transcode not enabled or failed - drop frame
			count := cameraFrameCount.Add(1)
			if !cameraH264WarningLogged.Load() || count%cameraLogWarningEvery == 0 {
				rdpLogger.Warn().Uint32("n", count).Msg("H.264 dropped - enable transcode")
				cameraH264WarningLogged.Store(true)
			}
			return
		}
		mgr.HandleCameraH264Frame(frame.data)

	default:
		count := cameraFrameCount.Add(1)
		if count == 1 || count%cameraLogWarningEvery == 0 {
			rdpLogger.Warn().Uint32("fmt", frame.pixelFormat).Msg("Unsupported format")
		}
	}
}

// shutdownCameraTranscoder shuts down the transcoder if running.
func shutdownCameraTranscoder() {
	// Stop frame worker first
	stopCameraFrameWorker()

	cameraTranscodeMu.Lock()
	defer cameraTranscodeMu.Unlock()

	if !cameraTranscodeInitialized.Load() {
		return
	}

	if nativeInstance != nil {
		nativeInstance.TranscodeShutdown()
	}
	cameraTranscodeInitialized.Store(false)
	cameraTranscodeWarningLogged.Store(false)
	// Reset failed flag to allow retry on next connection
	cameraTranscodeInitFailed.Store(false)
	rdpLogger.Info().Msg("RDP: transcoder shutdown")
}

func (a *rdpCameraAdapter) SendFrame(data []byte, width, height uint32, pixelFormat uint32) error {
	mgr := cameraManagerPtr.Load()
	if mgr == nil || !mgr.IsEnabled() {
		return nil
	}

	currentFmt := mgr.GetCurrentFormat()
	hostWantsMJPEG := currentFmt != nil && currentFmt.Codec == camera.CodecMJPEG

	// Fast path for MJPEG - no transcoding needed, process synchronously
	if pixelFormat == rdpCamPixelFormatMJPEG {
		mgr.HandleCameraMjpegFrame(data)
		return nil
	}

	// Fast path for H.264 when host supports it - no transcoding needed
	if pixelFormat == rdpCamPixelFormatH264 && !hostWantsMJPEG {
		mgr.HandleCameraH264Frame(data)
		return nil
	}

	// Transcoding required - use async queue to avoid blocking RDP message loop
	// This is critical for RDP HID to work while camera is active

	// Rate limiting: drop frames that come faster than target FPS
	// This prevents memory allocation and GC pressure from excess frames
	// Uses monotonic time (time.Time) to avoid issues with clock adjustments
	targetFPS := uint32(config.CameraFrameRate)
	if targetFPS == 0 {
		targetFPS = cameraDefaultFPS
	}
	if currentFmt != nil && currentFmt.FrameRate > 0 && uint32(currentFmt.FrameRate) < targetFPS {
		targetFPS = uint32(currentFmt.FrameRate)
	}
	minFrameInterval := time.Second / time.Duration(targetFPS)
	now := time.Now()
	if lastFrame := cameraLastFrameTime.Load(); lastFrame != nil {
		if now.Sub(*lastFrame) < minFrameInterval {
			// Frame arrived too soon - drop it before allocating memory
			cameraFrameDropped.Add(1)
			return nil
		}
	}
	// Don't store timestamp yet - only after successful queue

	if !cameraFrameQueueRunning.Load() {
		startCameraFrameWorker()
	}

	// Get buffer from pool to reduce GC pressure
	bufPtr := cameraBufferPool.Get().(*[]byte)
	buf := *bufPtr

	// Handle buffer sizing without polluting pool with oversized buffers
	if cap(buf) < len(data) {
		// Need larger buffer - return pooled buffer and allocate new one
		cameraBufferPool.Put(bufPtr)
		buf = make([]byte, len(data))
		bufPtr = &buf
	} else {
		buf = buf[:len(data)]
	}
	copy(buf, data)
	*bufPtr = buf

	frame := cameraFrame{
		data:        buf,
		poolBuf:     bufPtr,
		width:       width,
		height:      height,
		pixelFormat: pixelFormat,
	}

	// Non-blocking send - drop frame if queue is full
	select {
	case cameraFrameQueue <- frame:
		// Frame queued successfully - NOW update timestamp
		cameraLastFrameTime.Store(&now)
	default:
		// Queue full - return buffer (unless oversized) and drop frame
		// Don't update timestamp so next frame gets a chance
		returnCameraBuffer(bufPtr)
		dropped := cameraFrameDropped.Add(1)
		if dropped == 1 || dropped%cameraLogDropEvery == 0 {
			rdpLogger.Warn().Uint64("dropped", dropped).Msg("Camera queue full - CPU overloaded or FPS too high")
		}
	}

	return nil
}

func (a *rdpCameraAdapter) IsConnected() bool {
	mgr := cameraManagerPtr.Load()
	return mgr != nil && mgr.IsEnabled()
}

func (a *rdpCameraAdapter) SetEnabled(enabled bool) {
	if mgr := cameraManagerPtr.Load(); mgr != nil {
		mgr.SetEnabled(enabled)
	}
}

func (a *rdpCameraAdapter) IsEnabled() bool {
	mgr := cameraManagerPtr.Load()
	if mgr == nil {
		return false
	}
	return mgr.IsEnabled()
}

type rdpCameraFormatBridge struct {
	mu       sync.Mutex
	outChan  chan rdp.CameraFormatInfo
	stopChan chan struct{}
}

var rdpCameraFormatBridgeInstance rdpCameraFormatBridge

func (a *rdpCameraAdapter) SubscribeFormatChanges() <-chan rdp.CameraFormatInfo {
	mgr := cameraManagerPtr.Load()
	if mgr == nil {
		return nil
	}

	srcChan := mgr.SubscribeFormatChanges()
	if srcChan == nil {
		return nil
	}

	rdpCameraFormatBridgeInstance.mu.Lock()
	defer rdpCameraFormatBridgeInstance.mu.Unlock()

	// Close existing stop channel to terminate any previous goroutine
	if rdpCameraFormatBridgeInstance.stopChan != nil {
		close(rdpCameraFormatBridgeInstance.stopChan)
		rdpCameraFormatBridgeInstance.stopChan = nil // Prevent double-close
	}

	outChan := make(chan rdp.CameraFormatInfo, 4)
	stopChan := make(chan struct{})
	rdpCameraFormatBridgeInstance.outChan = outChan
	rdpCameraFormatBridgeInstance.stopChan = stopChan

	go func() {
		defer func() {
			if r := recover(); r != nil {
				rdpLogger.Error().Interface("panic", r).Msg("camera format bridge panic")
			}
			close(outChan)
		}()

		for {
			select {
			case <-stopChan:
				return
			case fmt, ok := <-srcChan:
				if !ok {
					return
				}
				select {
				case <-stopChan:
					return
				case outChan <- rdp.CameraFormatInfo{
					Codec:     string(fmt.Codec),
					Width:     fmt.Width,
					Height:    fmt.Height,
					FrameRate: fmt.FrameRate,
				}:
				}
			}
		}
	}()

	return outChan
}

func (a *rdpCameraAdapter) UnsubscribeFormatChanges() {
	// Shutdown transcoder when camera session ends
	shutdownCameraTranscoder()

	rdpCameraFormatBridgeInstance.mu.Lock()
	if rdpCameraFormatBridgeInstance.stopChan != nil {
		close(rdpCameraFormatBridgeInstance.stopChan)
		rdpCameraFormatBridgeInstance.stopChan = nil
	}
	rdpCameraFormatBridgeInstance.outChan = nil
	rdpCameraFormatBridgeInstance.mu.Unlock()

	if mgr := cameraManagerPtr.Load(); mgr != nil {
		mgr.UnsubscribeFormatChanges()
	}
}

type rdpTLSAdapter struct {
	lastCert   *tls.Certificate
	lastCertMu sync.Mutex
}

func (a *rdpTLSAdapter) UpgradeServerConn(conn net.Conn) (rdp.TLSConn, error) {
	tlsConfig := cryptotls.RDPConfig()
	tlsConfig.GetCertificate = func(info *tls.ClientHelloInfo) (*tls.Certificate, error) {
		cert, err := getCertificate(info)
		if err == nil && cert != nil {
			a.lastCertMu.Lock()
			a.lastCert = cert
			a.lastCertMu.Unlock()
		}
		return cert, err
	}
	return cryptotls.Server(conn, tlsConfig)
}

func (a *rdpTLSAdapter) IsHardwareAccelerated() bool {
	return cryptotls.IsHardwareAvailable()
}

func (a *rdpTLSAdapter) HardwareEngine() string {
	return cryptotls.HardwareEngine()
}

func (a *rdpTLSAdapter) GetServerCertificate(serverName string) *tls.Certificate {
	a.lastCertMu.Lock()
	cert := a.lastCert
	a.lastCertMu.Unlock()

	if cert != nil {
		return cert
	}

	rdpLogger.Warn().Str("serverName", serverName).Msg("no captured cert, using fallback")
	hello := &tls.ClientHelloInfo{ServerName: serverName}
	cert, err := getCertificate(hello)
	if err != nil {
		rdpLogger.Debug().Err(err).Msg("failed to get server certificate")
		return nil
	}
	return cert
}

func initRDPServer() error {
	if !config.RDPEnabled {
		rdpLogger.Info().Msg("RDP server disabled")
		return nil
	}

	cryptotls.Init()

	// Configure clipboard store with TTL and cleanup settings
	ttlSec := config.RDPFileTransferTTLSec
	if ttlSec == 0 {
		ttlSec = 300 // 5 minutes default
	}
	cleanupSec := config.RDPFileTransferCleanupSec
	if cleanupSec == 0 {
		cleanupSec = 60 // 1 minute default
	}
	GetClipboardStore().Configure(ttlSec, cleanupSec)

	server := GetRDPServer()
	server.SetPort(config.RDPPort)

	rdpLogger.Info().
		Int("port", config.RDPPort).
		Bool("tls", config.RDPUseTLS).
		Bool("hwCrypto", cryptotls.IsHardwareAvailable()).
		Str("hwEngine", cryptotls.HardwareEngine()).
		Bool("video", config.RDPVideoEnabled).
		Bool("audio", config.RDPAudioEnabled).
		Bool("mic", config.RDPMicEnabled).
		Bool("camera", config.RDPCameraEnabled).
		Int("maxConnections", config.RDPMaxConnections).
		Msg("initializing RDP server")

	if err := server.Start(); err != nil {
		rdpLogger.Error().Err(err).Msg("failed to start RDP server")
		return err
	}

	return nil
}

func BroadcastRDPFrame(frame []byte) {
	if rdpServer != nil {
		rdpServer.BroadcastFrame(frame)
	}
}

func UpdateRDPVideoState(width, height uint16) {
	if rdpServer != nil {
		rdpServer.UpdateVideoState(width, height)
	}
}

type rdpUSBStorageAdapter struct{}

func (a *rdpUSBStorageAdapter) IsAvailable() bool {
	// USB storage is available if nothing is currently mounted
	state, err := rpcGetVirtualMediaState()
	if err != nil {
		rdpLogger.Debug().Err(err).Msg("failed to get virtual media state, assuming USB storage unavailable")
		return false
	}
	return state == nil
}

func (a *rdpUSBStorageAdapter) MountFile(filename string) error {
	return rpcMountWithStorage(filename, Disk)
}

func (a *rdpUSBStorageAdapter) Unmount() error {
	return rpcUnmountImage()
}

func (a *rdpUSBStorageAdapter) GetImagesFolder() string {
	return imagesFolder
}

var (
	_ rdp.ConfigProvider     = (*rdpConfigAdapter)(nil)
	_ rdp.HIDProvider        = (*rdpHIDAdapter)(nil)
	_ rdp.VideoProvider      = (*rdpVideoAdapter)(nil)
	_ rdp.AudioProvider      = (*rdpAudioAdapter)(nil)
	_ rdp.CameraProvider     = (*rdpCameraAdapter)(nil)
	_ rdp.TLSProvider        = (*rdpTLSAdapter)(nil)
	_ rdp.USBStorageProvider = (*rdpUSBStorageAdapter)(nil)
)
