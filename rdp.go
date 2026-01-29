package kvm

import (
	"crypto/tls"
	"net"
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
			Logger:         rdpLogger,
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
	vs := getLastVideoState()
	w, h := vs.Width, vs.Height
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
// The release callback (if provided) is stored in the frame for the consumer
// to call after processing, allowing the buffer to be returned to a pool.
func BroadcastRDPRGBFrame(data []byte, width, height uint32, format rdp.RGBFrameFormat, release func()) {
	rdpRGBSubscribers.Broadcast(rdp.RGBFrame{
		Data:      data,
		Width:     width,
		Height:    height,
		Format:    format,
		OnRelease: release,
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
	ch := rdpAudioSubs.Subscribe(30)
	rdpLogger.Debug().Int32("subscribers", rdpAudioSubs.Count()).Msg("RDP audio subscribed")
	return ch
}

func (a *rdpAudioAdapter) UnsubscribeAudio(ch <-chan []byte) {
	rdpAudioSubs.Unsubscribe(ch)
	rdpLogger.Debug().Int32("subscribers", rdpAudioSubs.Count()).Msg("RDP audio unsubscribed")
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

func (a *rdpAudioAdapter) GetBufferPeriods() int {
	ensureConfigLoaded()
	if config.AudioBufferPeriods >= 2 && config.AudioBufferPeriods <= 48 {
		return config.AudioBufferPeriods
	}
	return 12 // Default
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
	cameraFrameQueueSize    = 4     // Small queue - transcoding is slow, no point buffering
	cameraBufferInitialSize = 256 * 1024 // 256KB - most H.264 frames are smaller
)

// H.264 NAL unit types we care about
const (
	nalTypeSlice    = 1  // Non-IDR slice (P-frame)
	nalTypeIDR      = 5  // IDR slice (I-frame/keyframe)
	nalTypeSEI      = 6  // Supplemental enhancement info (can drop)
	nalTypeSPS      = 7  // Sequence parameter set (critical - decoder needs this)
	nalTypePPS      = 8  // Picture parameter set (needed with SPS)
	nalTypeAUD      = 9  // Access unit delimiter (can drop)
	nalTypeFillerData = 12 // Filler data (can drop)
)

// Frame priority levels for intelligent dropping
const (
	framePriorityCritical = 3 // SPS - never drop, decoder will fail without it
	framePriorityHigh     = 2 // IDR without SPS - prefer not to drop, but can recover
	framePriorityNormal   = 1 // P-frames - can drop freely
	framePriorityLow      = 0 // SEI/AUD/filler - drop first
)

// h264FrameInfo contains the results of analyzing an H.264 frame.
// This struct allows a single NAL scan to extract all needed information,
// avoiding redundant scans of large keyframes (300KB+).
type h264FrameInfo struct {
	priority int    // Frame priority for drop decisions
	hasSPS   bool   // Has SPS NAL (critical for decoder init)
	hasIDR   bool   // Has IDR NAL (keyframe)
	spsPPS   []byte // Extracted SPS/PPS NALs (nil if none found)
}

// analyzeH264Frame performs a single scan of an H.264 frame to extract all
// relevant information: priority level, SPS/IDR presence, and SPS/PPS data.
// This consolidates what was previously 4 separate scanning functions into one,
// reducing CPU overhead for large keyframes from ~4 scans to 1 scan.
//
// The function is optimized to:
// - Stop early once SPS+IDR are found (for priority-only queries)
// - Extract SPS/PPS data in the same pass (avoiding separate extraction scan)
// - Limit scan to first 64KB for priority detection (SPS/IDR are always at frame start)
func analyzeH264Frame(data []byte, extractSPSPPS bool) h264FrameInfo {
	info := h264FrameInfo{priority: framePriorityNormal}

	// Limit scan for priority detection - SPS/IDR NALs are always at frame start
	scanLimit := len(data)
	if !extractSPSPPS && scanLimit > 65536 {
		scanLimit = 65536
	}

	for i := 0; i < scanLimit-4; {
		// Look for NAL start code (0x00 0x00 0x01 or 0x00 0x00 0x00 0x01)
		if data[i] != 0x00 || data[i+1] != 0x00 {
			i++
			continue
		}

		startCodePos := i
		nalOffset := -1
		if data[i+2] == 0x01 {
			nalOffset = i + 3
		} else if data[i+2] == 0x00 && i+3 < len(data) && data[i+3] == 0x01 {
			nalOffset = i + 4
		}

		if nalOffset <= 0 || nalOffset >= len(data) {
			i++
			continue
		}

		nalType := data[nalOffset] & 0x1F

		switch nalType {
		case nalTypeSPS:
			info.hasSPS = true
			info.priority = framePriorityCritical
			if extractSPSPPS {
				// Find end of this NAL and extract
				nalEnd := findNALEnd(data, nalOffset)
				info.spsPPS = append(info.spsPPS, data[startCodePos:nalEnd]...)
				i = nalEnd
				continue
			}
		case nalTypePPS:
			if extractSPSPPS {
				nalEnd := findNALEnd(data, nalOffset)
				info.spsPPS = append(info.spsPPS, data[startCodePos:nalEnd]...)
				i = nalEnd
				continue
			}
		case nalTypeIDR:
			info.hasIDR = true
			if !info.hasSPS && info.priority < framePriorityHigh {
				info.priority = framePriorityHigh
			}
		case nalTypeSEI, nalTypeAUD, nalTypeFillerData:
			if info.priority == framePriorityNormal {
				info.priority = framePriorityLow
			}
		}

		// Early exit if we have what we need and aren't extracting SPS/PPS
		if !extractSPSPPS && info.hasSPS && info.hasIDR {
			return info
		}

		i = nalOffset
	}

	return info
}

// findNALEnd finds the end of a NAL unit (start of next NAL or end of data).
// Helper for analyzeH264Frame to avoid code duplication.
func findNALEnd(data []byte, nalOffset int) int {
	for j := nalOffset + 1; j < len(data)-3; j++ {
		if data[j] == 0x00 && data[j+1] == 0x00 {
			if data[j+2] == 0x01 || (data[j+2] == 0x00 && j+3 < len(data) && data[j+3] == 0x01) {
				return j
			}
		}
	}
	return len(data)
}

// classifyH264Frame analyzes an H.264 frame and returns its priority level.
// Also returns whether it has SPS (needed for decoder init) and if it's a keyframe (IDR).
// This is a convenience wrapper around analyzeH264Frame for callers that don't need SPS/PPS extraction.
func classifyH264Frame(data []byte) (priority int, hasSPS, hasIDR bool) {
	info := analyzeH264Frame(data, false)
	return info.priority, info.hasSPS, info.hasIDR
}

// extractAndCacheSPSPPS extracts SPS and PPS NAL units from an H.264 frame
// and caches them for later use when the transcoder is reinitialized.
// This allows the decoder to bootstrap without waiting for a new SPS from the client.
func extractAndCacheSPSPPS(data []byte) {
	info := analyzeH264Frame(data, true)
	if len(info.spsPPS) > 0 {
		// Make a copy to avoid referencing the original buffer
		cached := make([]byte, len(info.spsPPS))
		copy(cached, info.spsPPS)
		cameraCachedSPSPPS.Store(&cached)
		rdpLogger.Debug().Int("size", len(cached)).Msg("Cached SPS/PPS for decoder reinit")
	}
}

// cameraFrame represents a camera frame waiting to be transcoded.
type cameraFrame struct {
	data        []byte
	poolBuf     *[]byte // Reference to pooled buffer for return
	width       uint32
	height      uint32
	pixelFormat uint32
	priority    int  // Frame priority (0=low/droppable, 3=critical/SPS)
	hasSPS      bool // Has SPS NAL - needed for decoder init
	hasIDR      bool // Has IDR NAL - keyframe for logging
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
	cameraH264NeedKeyframe       atomic.Bool // Set when transcoder needs a keyframe before decoding
	cameraTranscodeMu            sync.Mutex
	cameraTranscodeOutW          atomic.Uint32 // Current transcoder output width
	cameraTranscodeOutH          atomic.Uint32 // Current transcoder output height
	cameraFrameQueue             chan cameraFrame
	cameraFrameQueueStop         chan struct{}
	cameraFrameQueueDone         chan struct{} // Signals worker goroutine has exited
	cameraFrameQueueRunning      atomic.Bool
	cameraFrameDropped           atomic.Uint64
	cameraLastFrameTime          atomic.Pointer[time.Time] // For rate limiting (monotonic)
	cameraCachedSPSPPS           atomic.Pointer[[]byte]    // Cached SPS/PPS NALs for decoder reinit
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

	// Fast path: already initialized with matching resolution
	if cameraTranscodeInitialized.Load() {
		curW := cameraTranscodeOutW.Load()
		curH := cameraTranscodeOutH.Load()
		// Check if resolution changed - need to reinitialize
		if outputWidth != curW || outputHeight != curH {
			rdpLogger.Debug().
				Uint32("oldW", curW).
				Uint32("oldH", curH).
				Uint32("newW", outputWidth).
				Uint32("newH", outputHeight).
				Msg("Transcode resolution changed, reinitializing")
			// Fall through to reinitialize
		} else if nativeInstance != nil && nativeInstance.TranscodeIsRunning() {
			return true
		}
	}

	cameraTranscodeMu.Lock()
	defer cameraTranscodeMu.Unlock()

	// Double-check after acquiring lock - also check resolution
	if cameraTranscodeInitialized.Load() {
		curW := cameraTranscodeOutW.Load()
		curH := cameraTranscodeOutH.Load()
		resolutionChanged := outputWidth != curW || outputHeight != curH
		if !resolutionChanged && nativeInstance != nil && nativeInstance.TranscodeIsRunning() {
			return true
		}
		// Resolution changed or not running - shutdown and reinitialize
		if nativeInstance != nil {
			nativeInstance.TranscodeShutdown()
		}
		cameraTranscodeInitialized.Store(false)
		cameraH264NeedKeyframe.Store(true) // Need keyframe after reinit
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

	// Log init params (0x0 input means H.264 auto-detect from SPS)
	if inputWidth == 0 && inputHeight == 0 {
		rdpLogger.Debug().
			Str("in", "auto").
			Uint32("out", outputWidth).
			Uint32("fps", fps).
			Uint32("q", quality).
			Msg("Transcode init (H.264)")
	} else {
		rdpLogger.Debug().
			Uint32("inW", inputWidth).
			Uint32("inH", inputHeight).
			Uint32("outW", outputWidth).
			Uint32("outH", outputHeight).
			Uint32("fps", fps).
			Uint32("q", quality).
			Msg("Transcode init")
	}

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
	cameraTranscodeOutW.Store(outputWidth) // Track current output resolution
	cameraTranscodeOutH.Store(outputHeight)
	cameraH264NeedKeyframe.Store(true) // Wait for keyframe before starting decode
	return true
}

// Camera transcoding throttle constant.
const (
	// cameraTranscodeThrottleMs is the sleep duration after each transcoded frame.
	// This ensures HID and video handlers get CPU time between frames.
	// CGO calls (OpenH264 decode) block OS threads and don't yield to the Go scheduler,
	// so explicit sleep is required to prevent starving other goroutines.
	// 10ms sleep limits effective throughput to ~100fps max (assuming 0ms processing).
	cameraTranscodeThrottleMs = 10
)

// startCameraFrameWorker starts the async frame processing goroutine.
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
				// Throttle to ensure HID/video get CPU time (see cameraTranscodeThrottleMs)
				time.Sleep(cameraTranscodeThrottleMs * time.Millisecond)
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
			// H.264: pass 0,0 for input dims - actual resolution is in the SPS/PPS
			// which OpenH264 extracts after decoding. Format list from the client
			// (used for frame.width/height) may contain garbage (macOS sends invalid data).
			if initCameraTranscoder(0, 0, outW, outH, hostFPS) {
				frameNum := cameraFrameCount.Load()

				// Debug: log frame info for first few frames (using pre-computed values)
				if frameNum < 10 {
					rdpLogger.Debug().
						Uint32("frame", frameNum).
						Int("size", len(frame.data)).
						Int("priority", frame.priority).
						Bool("hasSPS", frame.hasSPS).
						Bool("hasIDR", frame.hasIDR).
						Msg("H264 frame")
				}

				// Cache SPS/PPS when we see them for later decoder reinit
				if frame.hasSPS {
					extractAndCacheSPSPPS(frame.data)
				}

				// Prepare data to feed to decoder
				dataToFeed := frame.data

				// Wait for SPS (NAL type 7) before feeding to decoder.
				// OpenH264 error 18 (dsNoParamSets | dsRefLost) means decoder doesn't have SPS yet.
				if cameraH264NeedKeyframe.Load() {
					if !frame.hasSPS {
						// No SPS in this frame - try to use cached SPS/PPS
						cached := cameraCachedSPSPPS.Load()
						if cached != nil && len(*cached) > 0 {
							// Prepend cached SPS/PPS to the frame data
							dataToFeed = append(*cached, frame.data...)
							rdpLogger.Debug().
								Uint32("frame", frameNum).
								Int("cachedSize", len(*cached)).
								Msg("Using cached SPS/PPS for decoder reinit")
						} else {
							// No cached SPS/PPS - must wait for one from the stream
							if frameNum < 10 {
								rdpLogger.Debug().Uint32("frame", frameNum).Msg("Waiting for SPS (no cache)")
							}
							return
						}
					}
					// Got SPS (fresh or cached) - clear flag and proceed
					rdpLogger.Debug().Uint32("frame", frameNum).Msg("Got SPS, starting decode")
					cameraH264NeedKeyframe.Store(false)
				}

				if err := nativeInstance.TranscodeFeedH264(dataToFeed); err != nil {
					count := cameraFrameCount.Add(1)
					if !cameraTranscodeWarningLogged.Load() || count%cameraLogWarningEvery == 0 {
						rdpLogger.Warn().Err(err).Uint32("n", count).Msg("Transcode err")
						cameraTranscodeWarningLogged.Store(true)
					}
					// On transcode error, wait for next keyframe to recover
					cameraH264NeedKeyframe.Store(true)
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
	cameraTranscodeOutW.Store(0) // Reset resolution tracking
	cameraTranscodeOutH.Store(0)
	cameraTranscodeWarningLogged.Store(false)
	cameraH264NeedKeyframe.Store(false)
	cameraCachedSPSPPS.Store(nil) // Clear cached SPS/PPS (next connection may have different stream)
	// Reset failed flag to allow retry on next connection
	cameraTranscodeInitFailed.Store(false)
	rdpLogger.Debug().Msg("RDP: transcoder shutdown")
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

	// Transcoding required - classify frame priority for intelligent dropping
	var priority int
	var hasSPS, hasIDR bool
	if pixelFormat == rdpCamPixelFormatH264 && len(data) > 4 {
		priority, hasSPS, hasIDR = classifyH264Frame(data)
	} else {
		priority = framePriorityNormal // Non-H.264 frames can be dropped
	}

	// Adaptive backpressure: check queue depth and drop aggressively when filling up
	// This prevents latency buildup - better to drop frames than accumulate delay
	queueLen := len(cameraFrameQueue)
	queueCap := cap(cameraFrameQueue)
	if queueCap == 0 {
		queueCap = cameraFrameQueueSize // Queue not created yet, use default
	}

	// Drop based on priority and queue fill level:
	// - Queue 75%+ full: drop low priority (SEI/filler)
	// - Queue 50%+ full: drop normal priority (P-frames)
	// - Queue 25%+ full: drop high priority (IDR without SPS) only if very old frame
	// - Never drop critical (SPS) unless absolutely necessary
	if queueLen > 0 {
		fillRatio := float32(queueLen) / float32(queueCap)
		shouldDrop := false

		switch priority {
		case framePriorityLow:
			shouldDrop = fillRatio >= 0.25 // Drop metadata early
		case framePriorityNormal:
			shouldDrop = fillRatio >= 0.50 // Drop P-frames when half full
		case framePriorityHigh:
			shouldDrop = fillRatio >= 0.75 // Prefer keeping IDR frames
		case framePriorityCritical:
			shouldDrop = false // Never proactively drop SPS
		}

		if shouldDrop {
			cameraFrameDropped.Add(1)
			return nil
		}
	}

	// Get current time once for rate limiting and frame timestamp
	// (time.Now() is a syscall, avoid calling it multiple times per frame)
	now := time.Now()

	// Rate limiting for normal/low priority frames
	// Critical/high priority frames skip rate limiting
	if priority <= framePriorityNormal {
		targetFPS := uint32(config.CameraFrameRate)
		if targetFPS == 0 {
			targetFPS = cameraDefaultFPS
		}
		if currentFmt != nil && currentFmt.FrameRate > 0 && uint32(currentFmt.FrameRate) < targetFPS {
			targetFPS = uint32(currentFmt.FrameRate)
		}
		minFrameInterval := time.Second / time.Duration(targetFPS)
		if lastFrame := cameraLastFrameTime.Load(); lastFrame != nil {
			if now.Sub(*lastFrame) < minFrameInterval {
				cameraFrameDropped.Add(1)
				return nil
			}
		}
	}

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
		priority:    priority,
		hasSPS:      hasSPS,
		hasIDR:      hasIDR,
	}

	// For critical frames (SPS), use blocking send with timeout to ensure they get through
	// For other frames, use non-blocking send - ok to drop if queue is full
	if priority == framePriorityCritical {
		select {
		case cameraFrameQueue <- frame:
			cameraLastFrameTime.Store(&now)
		default:
			// Queue full - wait briefly for SPS frame (critical for decoder)
			select {
			case cameraFrameQueue <- frame:
				cameraLastFrameTime.Store(&now)
			case <-time.After(30 * time.Millisecond):
				// Still full after 30ms - drop as last resort
				returnCameraBuffer(bufPtr)
				rdpLogger.Warn().Msg("Camera queue full - SPS frame dropped (decoder will need reset)")
			}
		}
	} else {
		// Non-critical: non-blocking send, ok to drop
		select {
		case cameraFrameQueue <- frame:
			cameraLastFrameTime.Store(&now)
		default:
			returnCameraBuffer(bufPtr)
			dropped := cameraFrameDropped.Add(1)
			if dropped == 1 || dropped%cameraLogDropEvery == 0 {
				rdpLogger.Debug().
					Uint64("dropped", dropped).
					Int("priority", priority).
					Int("queueLen", queueLen).
					Msg("Frame dropped (queue full)")
			}
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
