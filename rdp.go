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

var (
	rdpServer     *rdp.Server
	rdpServerOnce sync.Once
)

// GetRDPServer returns the global RDP server instance.
func GetRDPServer() *rdp.Server {
	rdpServerOnce.Do(func() {
		deps := rdp.Dependencies{
			Logger: *rdpLogger,
			Config: &rdpConfigAdapter{},
			HID:    &rdpHIDAdapter{},
			Video:  &rdpVideoAdapter{},
			Audio:  &rdpAudioAdapter{},
			Camera: &rdpCameraAdapter{},
			TLS:    &rdpTLSAdapter{},
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

var rdpVideoSubscribers struct {
	mu   sync.RWMutex
	subs []chan []byte
}

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
	ch := make(chan []byte, 10)
	rdpVideoSubscribers.mu.Lock()
	rdpVideoSubscribers.subs = append(rdpVideoSubscribers.subs, ch)
	rdpVideoSubscribers.mu.Unlock()
	return ch
}

func (a *rdpVideoAdapter) UnsubscribeH264(ch <-chan []byte) {
	rdpVideoSubscribers.mu.Lock()
	defer rdpVideoSubscribers.mu.Unlock()
	for i, sub := range rdpVideoSubscribers.subs {
		if sub == ch {
			rdpVideoSubscribers.subs = append(rdpVideoSubscribers.subs[:i], rdpVideoSubscribers.subs[i+1:]...)
			return
		}
	}
}

// BroadcastRDPH264Frame sends an H.264 frame to all RDP subscribers.
func BroadcastRDPH264Frame(frame []byte) {
	rdpVideoSubscribers.mu.RLock()
	defer rdpVideoSubscribers.mu.RUnlock()

	for _, ch := range rdpVideoSubscribers.subs {
		select {
		case ch <- frame:
		default:
		}
	}
}

var rdpJPEGSubscribers struct {
	mu   sync.RWMutex
	subs []chan []byte
}

func (a *rdpVideoAdapter) SubscribeJPEG() <-chan []byte {
	ch := make(chan []byte, 5)
	rdpJPEGSubscribers.mu.Lock()
	rdpJPEGSubscribers.subs = append(rdpJPEGSubscribers.subs, ch)
	rdpJPEGSubscribers.mu.Unlock()
	return ch
}

func (a *rdpVideoAdapter) UnsubscribeJPEG(ch <-chan []byte) {
	rdpJPEGSubscribers.mu.Lock()
	defer rdpJPEGSubscribers.mu.Unlock()
	for i, sub := range rdpJPEGSubscribers.subs {
		if sub == ch {
			rdpJPEGSubscribers.subs = append(rdpJPEGSubscribers.subs[:i], rdpJPEGSubscribers.subs[i+1:]...)
			return
		}
	}
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
	rdpJPEGSubscribers.mu.RLock()
	defer rdpJPEGSubscribers.mu.RUnlock()

	for _, ch := range rdpJPEGSubscribers.subs {
		select {
		case ch <- frame:
		default:
		}
	}
}

var rdpRGBSubscribers struct {
	mu   sync.RWMutex
	subs []chan rdp.RGBFrame
}

func (a *rdpVideoAdapter) SubscribeRGB() <-chan rdp.RGBFrame {
	ch := make(chan rdp.RGBFrame, 5)
	rdpRGBSubscribers.mu.Lock()
	rdpRGBSubscribers.subs = append(rdpRGBSubscribers.subs, ch)
	rdpRGBSubscribers.mu.Unlock()
	return ch
}

func (a *rdpVideoAdapter) UnsubscribeRGB(ch <-chan rdp.RGBFrame) {
	rdpRGBSubscribers.mu.Lock()
	defer rdpRGBSubscribers.mu.Unlock()
	for i, sub := range rdpRGBSubscribers.subs {
		if sub == ch {
			rdpRGBSubscribers.subs = append(rdpRGBSubscribers.subs[:i], rdpRGBSubscribers.subs[i+1:]...)
			return
		}
	}
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
	rdpRGBSubscribers.mu.RLock()
	defer rdpRGBSubscribers.mu.RUnlock()

	frame := rdp.RGBFrame{
		Data:   data,
		Width:  width,
		Height: height,
		Format: format,
	}

	for _, ch := range rdpRGBSubscribers.subs {
		select {
		case ch <- frame:
		default:
		}
	}
}

type rdpAudioAdapter struct{}

var rdpAudioSubscribers struct {
	mu   sync.RWMutex
	subs []chan []byte
}

func (a *rdpAudioAdapter) Connect() {
	OnRDPAudioConnect()
}

func (a *rdpAudioAdapter) Disconnect() {
	OnRDPAudioDisconnect()
}

func (a *rdpAudioAdapter) SubscribeAudio() <-chan []byte {
	ch := make(chan []byte, 30)
	rdpAudioSubscribers.mu.Lock()
	rdpAudioSubscribers.subs = append(rdpAudioSubscribers.subs, ch)
	rdpAudioSubscribers.mu.Unlock()
	return ch
}

func (a *rdpAudioAdapter) UnsubscribeAudio(ch <-chan []byte) {
	rdpAudioSubscribers.mu.Lock()
	defer rdpAudioSubscribers.mu.Unlock()
	for i, sub := range rdpAudioSubscribers.subs {
		if sub == ch {
			rdpAudioSubscribers.subs = append(rdpAudioSubscribers.subs[:i], rdpAudioSubscribers.subs[i+1:]...)
			return
		}
	}
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

// BroadcastRDPAudio sends audio data to all RDP audio subscribers.
func BroadcastRDPAudio(data []byte) {
	rdpAudioSubscribers.mu.RLock()
	defer rdpAudioSubscribers.mu.RUnlock()

	for _, ch := range rdpAudioSubscribers.subs {
		select {
		case ch <- data:
		default:
		}
	}
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

	if rdpCameraFormatBridgeInstance.stopChan != nil {
		close(rdpCameraFormatBridgeInstance.stopChan)
	}

	outChan := make(chan rdp.CameraFormatInfo, 4)
	stopChan := make(chan struct{})
	rdpCameraFormatBridgeInstance.outChan = outChan
	rdpCameraFormatBridgeInstance.stopChan = stopChan

	go func() {
		defer func() {
			defer func() {
				if r := recover(); r != nil {
					rdpLogger.Debug().Interface("panic", r).Msg("camera format bridge cleanup")
				}
			}()
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

var (
	_ rdp.ConfigProvider = (*rdpConfigAdapter)(nil)
	_ rdp.HIDProvider    = (*rdpHIDAdapter)(nil)
	_ rdp.VideoProvider  = (*rdpVideoAdapter)(nil)
	_ rdp.AudioProvider  = (*rdpAudioAdapter)(nil)
	_ rdp.CameraProvider = (*rdpCameraAdapter)(nil)
	_ rdp.TLSProvider    = (*rdpTLSAdapter)(nil)
)
