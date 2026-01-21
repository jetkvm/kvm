// RDP Server Bridge
//
// This file provides the bridge between the kvm package and the internal/rdp package.
// It implements the dependency interfaces and provides initialization functions.

package kvm

import (
	"crypto/tls"
	"net"
	"sync"

	"github.com/jetkvm/kvm/internal/audio"
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
		// Create Go TLS config for CredSSP (requires session binding)
		goTLSConfig := &tls.Config{
			GetCertificate: getCertificate,
			MinVersion:     tls.VersionTLS12,
			MaxVersion:     tls.VersionTLS12, // CredSSP requires TLS 1.2
			CipherSuites: []uint16{
				tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
				tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,
				tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
				tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,
			},
			CurvePreferences: []tls.CurveID{
				tls.X25519,
				tls.CurveP256,
			},
		}

		deps := rdp.Dependencies{
			Logger: *rdpLogger,
			Config: &rdpConfigAdapter{},
			HID:    &rdpHIDAdapter{},
			Video:  &rdpVideoAdapter{},
			Audio:  &rdpAudioAdapter{},
			Camera: &rdpCameraAdapter{},
			TLS:    &rdpTLSAdapter{goConfig: goTLSConfig},
		}
		rdpServer = rdp.NewServer(deps)
	})
	return rdpServer
}

// rdpConfigAdapter adapts kvm config to rdp.ConfigProvider interface.
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

func (a *rdpConfigAdapter) GetTLSMode() string {
	return config.TLSMode
}

func (a *rdpConfigAdapter) GetHashedPassword() string {
	return config.HashedPassword
}

// rdpHIDAdapter adapts HID RPC calls to rdp.HIDProvider interface.
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
	// Check text size limit
	if len(text) > keyboard.MaxClipboardSize {
		return nil // Silently ignore oversized clipboard
	}

	// Get clipboard mode and target OS from config
	mode := keyboard.ClipboardMode(config.RDPClipboardMode)
	if mode == "" {
		mode = keyboard.ClipboardModeText
	}
	targetOS := keyboard.TargetOS(config.RDPTargetOS)
	if targetOS == "" {
		targetOS = keyboard.TargetOSWindows
	}

	// Prepare clipboard text based on mode (handles encoding if needed)
	preparedText, encoded := keyboard.PrepareClipboardText([]byte(text), mode, targetOS)
	if preparedText == "" {
		return nil // Nothing to type (binary content in text mode)
	}

	if encoded {
		rdpLogger.Info().
			Str("mode", string(mode)).
			Str("targetOS", string(targetOS)).
			Int("originalLen", len(text)).
			Int("encodedLen", len(preparedText)).
			Msg("RDP: clipboard content encoded for typing")
	}

	// Get configurable delay
	totalDelay := config.RDPPasteDelayMs
	if totalDelay < 0 {
		totalDelay = 0
	}
	pressDelay, releaseDelay := keyboard.ComputeDelays(totalDelay)

	// Convert text to keyboard macro steps using shared keyboard package
	steps, skipped := keyboard.TextToMacroSteps(preparedText, "en-US", pressDelay, releaseDelay)
	if len(steps) == 0 {
		return nil
	}

	if skipped > 0 {
		rdpLogger.Debug().
			Int("skipped", skipped).
			Msg("RDP: some characters skipped during paste")
	}

	return rpcExecuteKeyboardMacro(steps)
}

func (a *rdpHIDAdapter) IsKeyboardMacroInProgress() bool {
	return isKeyboardMacroInProgress()
}

func (a *rdpHIDAdapter) CancelKeyboardMacro() {
	cancelKeyboardMacro()
}

// rdpVideoAdapter adapts video capture to rdp.VideoProvider interface.
type rdpVideoAdapter struct{}

// Global video subscriber management for RDP
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
	// Video is shared with WebRTC, so we don't stop it when RDP disconnects.
	// The native system will stop video automatically when there are no active sessions.
	return nil
}

func (a *rdpVideoAdapter) SubscribeH264() <-chan []byte {
	ch := make(chan []byte, 10) // Buffered channel for frames

	rdpVideoSubscribers.mu.Lock()
	rdpVideoSubscribers.subs = append(rdpVideoSubscribers.subs, ch)
	rdpVideoSubscribers.mu.Unlock()

	return ch
}

func (a *rdpVideoAdapter) UnsubscribeH264() {
	// Clean up would need more context about which channel to remove
	// For now, this is a no-op - in a real implementation we'd track the channel
}

// BroadcastRDPH264Frame sends an H.264 frame to all RDP subscribers.
// This should be called from native.go's OnVideoFrameReceived.
func BroadcastRDPH264Frame(frame []byte) {
	rdpVideoSubscribers.mu.RLock()
	defer rdpVideoSubscribers.mu.RUnlock()

	for _, ch := range rdpVideoSubscribers.subs {
		select {
		case ch <- frame:
		default:
			// Channel full, drop frame
		}
	}
}

// Global JPEG video subscriber management for RDP bitmap mode
var rdpJPEGSubscribers struct {
	mu   sync.RWMutex
	subs []chan []byte
}

func (a *rdpVideoAdapter) SubscribeJPEG() <-chan []byte {
	ch := make(chan []byte, 5) // Smaller buffer for JPEG (lower rate)

	rdpJPEGSubscribers.mu.Lock()
	rdpJPEGSubscribers.subs = append(rdpJPEGSubscribers.subs, ch)
	rdpJPEGSubscribers.mu.Unlock()

	return ch
}

func (a *rdpVideoAdapter) UnsubscribeJPEG() {
	// Clean up would need more context about which channel to remove
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
// Used for bitmap mode fallback when RDPGFX is not supported.
func BroadcastRDPJPEGFrame(frame []byte) {
	rdpJPEGSubscribers.mu.RLock()
	defer rdpJPEGSubscribers.mu.RUnlock()

	for _, ch := range rdpJPEGSubscribers.subs {
		select {
		case ch <- frame:
		default:
			// Channel full, drop frame
		}
	}
}

// Global RGB video subscriber management for RDP bitmap mode (RGA hardware acceleration)
var rdpRGBSubscribers struct {
	mu   sync.RWMutex
	subs []chan rdp.RGBFrame
}

func (a *rdpVideoAdapter) SubscribeRGB() <-chan rdp.RGBFrame {
	ch := make(chan rdp.RGBFrame, 5) // Buffered for non-blocking delivery

	rdpRGBSubscribers.mu.Lock()
	rdpRGBSubscribers.subs = append(rdpRGBSubscribers.subs, ch)
	rdpRGBSubscribers.mu.Unlock()

	return ch
}

func (a *rdpVideoAdapter) UnsubscribeRGB() {
	// Clean up would need more context about which channel to remove
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

// BroadcastRDPRGBFrame sends a raw BGRX frame to all RDP RGB subscribers.
// Used for bitmap mode with RGA hardware acceleration.
func BroadcastRDPRGBFrame(data []byte, width, height uint32) {
	rdpRGBSubscribers.mu.RLock()
	defer rdpRGBSubscribers.mu.RUnlock()

	frame := rdp.RGBFrame{
		Data:   data,
		Width:  width,
		Height: height,
	}

	for _, ch := range rdpRGBSubscribers.subs {
		select {
		case ch <- frame:
		default:
			// Channel full, drop frame
		}
	}
}

// rdpAudioAdapter adapts audio to rdp.AudioProvider interface.
type rdpAudioAdapter struct{}

// Global audio subscriber management for RDP
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
	ch := make(chan []byte, 30) // Buffered channel for audio

	rdpAudioSubscribers.mu.Lock()
	rdpAudioSubscribers.subs = append(rdpAudioSubscribers.subs, ch)
	rdpAudioSubscribers.mu.Unlock()

	return ch
}

func (a *rdpAudioAdapter) UnsubscribeAudio() {
	// Clean up would need more context
}

func (a *rdpAudioAdapter) PlayAudio(data []byte) error {
	// Forward RDP client microphone audio to USB audio gadget
	// The AUDIN channel sends 16-bit PCM, stereo, 48kHz which we write directly
	return audio.WritePCM(data)
}

func (a *rdpAudioAdapter) EnableAudioInput() error {
	// Enable audio input when RDP client's AUDIN channel becomes ready
	// This automatically starts the USB audio gadget playback for mic passthrough
	return SetAudioInputEnabled(true)
}

// BroadcastRDPAudio sends audio data to all RDP audio subscribers.
// This should be called from the HDMI audio capture system.
func BroadcastRDPAudio(data []byte) {
	rdpAudioSubscribers.mu.RLock()
	defer rdpAudioSubscribers.mu.RUnlock()

	for _, ch := range rdpAudioSubscribers.subs {
		select {
		case ch <- data:
		default:
			// Channel full, drop audio
		}
	}
}

// rdpCameraAdapter adapts UVC camera to rdp.CameraProvider interface.
type rdpCameraAdapter struct{}

// Camera pixel format FourCC codes (must match internal/rdp/channels/camera.go)
const (
	rdpCamPixelFormatMJPEG = 0x47504A4D // 'MJPG'
	rdpCamPixelFormatH264  = 0x34363248 // 'H264'
)

func (a *rdpCameraAdapter) SendFrame(data []byte, width, height uint32, pixelFormat uint32) error {
	mgr := cameraManagerPtr.Load()
	if mgr == nil || !mgr.IsEnabled() {
		return nil
	}

	// Route based on pixel format
	switch pixelFormat {
	case rdpCamPixelFormatH264:
		mgr.HandleCameraH264Frame(data)
	case rdpCamPixelFormatMJPEG:
		mgr.HandleCameraMjpegFrame(data)
	default:
		// NV12, I420, YUY2 need conversion - skip for now
		// In the future, could add software conversion to MJPEG
		return nil
	}
	return nil
}

func (a *rdpCameraAdapter) IsConnected() bool {
	mgr := cameraManagerPtr.Load()
	if mgr == nil {
		return false
	}
	return mgr.IsStreaming()
}

func (a *rdpCameraAdapter) SetEnabled(enabled bool) {
	mgr := cameraManagerPtr.Load()
	if mgr != nil {
		mgr.SetEnabled(enabled)
		rdpLogger.Warn().Bool("enabled", enabled).Msg("RDP: camera passthrough state changed")
	}
}

func (a *rdpCameraAdapter) IsEnabled() bool {
	mgr := cameraManagerPtr.Load()
	if mgr == nil {
		return false
	}
	return mgr.IsEnabled()
}

// rdpTLSAdapter provides TLS connection upgrading for RDP with hardware acceleration.
type rdpTLSAdapter struct {
	// goConfig is kept for CredSSP which requires Go's *tls.Conn for session binding
	goConfig *tls.Config
}

func (a *rdpTLSAdapter) UpgradeServerConn(conn net.Conn) (rdp.TLSConn, error) {
	// Use hardware-accelerated TLS for plain TLS mode
	tlsConfig := cryptotls.RDPConfig()
	tlsConfig.GetCertificate = getCertificate
	return cryptotls.Server(conn, tlsConfig)
}

func (a *rdpTLSAdapter) UpgradeServerConnForCredSSP(conn net.Conn) (rdp.CredSSPTLSConn, error) {
	// CredSSP requires Go's *tls.Conn for TLS session binding in pubKeyAuth
	tlsConn := tls.Server(conn, a.goConfig)
	if err := tlsConn.Handshake(); err != nil {
		return nil, err
	}
	return tlsConn, nil
}

func (a *rdpTLSAdapter) IsHardwareAccelerated() bool {
	return cryptotls.IsHardwareAvailable()
}

func (a *rdpTLSAdapter) HardwareEngine() string {
	return cryptotls.HardwareEngine()
}

// initRDPServer initializes and starts the RDP server if enabled.
// Returns an error if the server fails to start.
func initRDPServer() error {
	if !config.RDPEnabled {
		rdpLogger.Info().Msg("RDP server disabled in configuration")
		return nil
	}

	// Initialize TLS subsystem early to check hardware crypto availability
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

// BroadcastRDPFrame sends an H.264 frame to all RDP clients.
func BroadcastRDPFrame(frame []byte) {
	if rdpServer != nil {
		rdpServer.BroadcastFrame(frame)
	}
}

// UpdateRDPVideoState updates the RDP server with current video resolution.
func UpdateRDPVideoState(width, height uint16) {
	if rdpServer != nil {
		rdpServer.UpdateVideoState(width, height)
	}
}

// Compile-time interface checks
var (
	_ rdp.ConfigProvider = (*rdpConfigAdapter)(nil)
	_ rdp.HIDProvider    = (*rdpHIDAdapter)(nil)
	_ rdp.VideoProvider  = (*rdpVideoAdapter)(nil)
	_ rdp.AudioProvider  = (*rdpAudioAdapter)(nil)
	_ rdp.CameraProvider = (*rdpCameraAdapter)(nil)
	_ rdp.TLSProvider    = (*rdpTLSAdapter)(nil)
)
