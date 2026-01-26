package kvm

import (
	"crypto/tls"
	"net"
	"sync"
	"sync/atomic"

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
	rdpCamPixelFormatH264  = 0x01 // H.264
	rdpCamPixelFormatMJPEG = 0x02 // Motion JPEG
	rdpCamPixelFormatYUY2  = 0x03 // YUY2 (4:2:2)
	rdpCamPixelFormatNV12  = 0x04 // NV12 (4:2:0)
	rdpCamPixelFormatI420  = 0x05 // I420/YV12
)

var (
	cameraFrameCount        atomic.Uint32
	cameraH264WarningLogged atomic.Bool
)

func (a *rdpCameraAdapter) SendFrame(data []byte, width, height uint32, pixelFormat uint32) error {
	mgr := cameraManagerPtr.Load()
	if mgr == nil || !mgr.IsEnabled() {
		return nil
	}

	switch pixelFormat {
	case rdpCamPixelFormatH264:
		currentFmt := mgr.GetCurrentFormat()
		if currentFmt != nil && currentFmt.Codec == camera.CodecMJPEG {
			count := cameraFrameCount.Add(1)
			if !cameraH264WarningLogged.Load() || count%300 == 0 {
				rdpLogger.Warn().
					Uint32("width", width).
					Uint32("height", height).
					Uint32("frameCount", count).
					Msg("RDP: H.264 frame dropped - host wants MJPEG, no hardware transcoder")
				cameraH264WarningLogged.Store(true)
			}
			return nil
		}
		mgr.HandleCameraH264Frame(data)
	case rdpCamPixelFormatMJPEG:
		mgr.HandleCameraMjpegFrame(data)
	default:
		count := cameraFrameCount.Add(1)
		if count == 1 || count%300 == 0 {
			rdpLogger.Warn().
				Uint32("pixelFormat", pixelFormat).
				Uint32("frameCount", count).
				Msg("RDP: camera frame dropped - unsupported pixel format")
		}
		return nil
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
				rdpLogger.Debug().Interface("panic", r).Msg("camera format bridge panic")
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
