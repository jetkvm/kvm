package kvm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net"
	"strings"
	"time"

	"github.com/jetkvm/kvm/internal/diagnostics"
	"github.com/jetkvm/kvm/internal/hidrpc"
	"github.com/jetkvm/kvm/internal/logging"
	"github.com/jetkvm/kvm/internal/playoutdelay"
	"github.com/jetkvm/kvm/internal/sync"
	"github.com/jetkvm/kvm/internal/usbgadget"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/gin-gonic/gin"
	"github.com/pion/ice/v4"
	"github.com/pion/interceptor"
	"github.com/pion/webrtc/v4"
	"github.com/rs/zerolog"
)

type Session struct {
	peerConnection           *webrtc.PeerConnection
	VideoTrack               *webrtc.TrackLocalStaticSample
	AudioTrack               *webrtc.TrackLocalStaticSample
	ControlChannel           *webrtc.DataChannel
	RPCChannel               *webrtc.DataChannel
	HidChannel               *webrtc.DataChannel
	shouldUmountVirtualMedia bool

	rpcQueue chan webrtc.DataChannelMessage

	hidRPCAvailable          bool
	lastKeepAliveArrivalTime time.Time  // Track when last keep-alive packet arrived
	lastTimerResetTime       time.Time  // Track when auto-release timer was last reset
	keepAliveJitterLock      sync.Mutex // Protect jitter compensation timing state
	hidQueue                 []chan hidQueueMessage

	keysDownStateQueue chan usbgadget.KeysDownState
	done               chan struct{}
	closeOnce          sync.Once

	codecMimeType string
}

var (
	actionSessions      int = 0
	activeSessionsMutex     = &sync.Mutex{}
)

func incrActiveSessions() int {
	activeSessionsMutex.Lock()
	defer activeSessionsMutex.Unlock()

	actionSessions++
	return actionSessions
}

func decrActiveSessions() int {
	activeSessionsMutex.Lock()
	defer activeSessionsMutex.Unlock()

	actionSessions--
	return actionSessions
}

func getActiveSessions() int {
	activeSessionsMutex.Lock()
	defer activeSessionsMutex.Unlock()

	return actionSessions
}

// GetDiagnosticsInfo returns WebRTC diagnostic info for the diagnostics package.
func (s *Session) GetDiagnosticsInfo() diagnostics.SessionInfo {
	info := diagnostics.SessionInfo{
		HasCurrentSession: true,
	}

	if s.peerConnection != nil {
		pc := s.peerConnection
		info.ICEConnectionState = pc.ICEConnectionState().String()
		info.SignalingState = pc.SignalingState().String()
		info.ConnectionState = pc.ConnectionState().String()

		var channels []diagnostics.DataChannelInfo
		if s.ControlChannel != nil {
			channels = append(channels, diagnostics.DataChannelInfo{
				Label: s.ControlChannel.Label(),
				State: s.ControlChannel.ReadyState().String(),
			})
		}
		if s.RPCChannel != nil {
			channels = append(channels, diagnostics.DataChannelInfo{
				Label: s.RPCChannel.Label(),
				State: s.RPCChannel.ReadyState().String(),
			})
		}
		if s.HidChannel != nil {
			channels = append(channels, diagnostics.DataChannelInfo{
				Label: s.HidChannel.Label(),
				State: s.HidChannel.ReadyState().String(),
			})
		}
		info.DataChannels = channels
	}

	return info
}

func (s *Session) resetKeepAliveTime() {
	s.keepAliveJitterLock.Lock()
	defer s.keepAliveJitterLock.Unlock()
	s.lastKeepAliveArrivalTime = time.Time{} // Reset keep-alive timing tracking
	s.lastTimerResetTime = time.Time{}       // Reset auto-release timer tracking
}

type hidQueueMessage struct {
	webrtc.DataChannelMessage
	channel string
}

type SessionConfig struct {
	ICEServers []string
	LocalIP    string
	IsCloud    bool
	ws         *websocket.Conn
	Logger     *zerolog.Logger
	MDNSMode   string
}

// negotiateAudioCodec returns the audio MIME type to use, or "" if the browser
// offer advertises no supported audio codec.
func negotiateAudioCodec(offerSDP string) string {
	upper := strings.ToUpper(offerSDP)
	switch {
	case strings.Contains(upper, "G722/8000"):
		return webrtc.MimeTypeG722
	case strings.Contains(upper, "PCMU/8000"):
		return webrtc.MimeTypePCMU
	}
	return ""
}

// attachAudioTrack adds an outgoing audio track when the device config has
// audio enabled AND the browser advertised a codec we support. No-op
// otherwise; the SDP answer just leaves the audio m-line inactive.
func (s *Session) attachAudioTrack(offerSDP string) error {
	if !config.AudioEnabled {
		webrtcLogger.Debug().Msg("audio disabled by device config")
		return nil
	}
	audioMime := negotiateAudioCodec(offerSDP)
	if audioMime == "" {
		webrtcLogger.Warn().Msg("browser offer has no supported audio codec; audio disabled")
		return nil
	}
	track, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: audioMime, ClockRate: 8000}, "audio", "kvm")
	if err != nil {
		return err
	}
	sender, err := s.peerConnection.AddTrack(track)
	if err != nil {
		return err
	}
	s.AudioTrack = track
	webrtcLogger.Info().Str("codec", audioMime).Msg("audio track enabled")
	go drainRTCP(sender)
	return nil
}

// drainRTCP reads and discards RTCP packets from a sender. Required for NACK
// handling on outgoing tracks; the sender stops on connection close.
func drainRTCP(sender *webrtc.RTPSender) {
	buf := make([]byte, 1500)
	for {
		if _, _, err := sender.Read(buf); err != nil {
			return
		}
	}
}

// resolveCodec picks the video codec based on user preference and browser support.
// Always validates against the browser's SDP offer to prevent negotiation failure.
func resolveCodec(offerSDP string) string {
	browserSupportsH265 := strings.Contains(strings.ToUpper(offerSDP), "H265")

	switch config.VideoCodecPreference {
	case "h265":
		if browserSupportsH265 {
			return webrtc.MimeTypeH265
		}
		logger.Warn().Msg("H.265 preferred but browser does not support it, falling back to H.264")
		return webrtc.MimeTypeH264
	case "h264":
		return webrtc.MimeTypeH264
	default: // "auto" or ""
		if browserSupportsH265 {
			return webrtc.MimeTypeH265
		}
		return webrtc.MimeTypeH264
	}
}

func (s *Session) ExchangeOffer(offerStr string) (string, error) {
	b, err := base64.StdEncoding.DecodeString(offerStr)
	if err != nil {
		return "", err
	}
	offer := webrtc.SessionDescription{}
	err = json.Unmarshal(b, &offer)
	if err != nil {
		return "", err
	}

	codec := resolveCodec(offer.SDP)
	s.codecMimeType = codec

	s.VideoTrack, err = webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: codec}, "video", "kvm")
	if err != nil {
		return "", err
	}

	rtpSender, err := s.peerConnection.AddTrack(s.VideoTrack)
	if err != nil {
		return "", err
	}

	go drainRTCP(rtpSender)

	if err := s.attachAudioTrack(offer.SDP); err != nil {
		return "", err
	}

	// Set the remote SessionDescription
	if err = s.peerConnection.SetRemoteDescription(offer); err != nil {
		return "", err
	}

	// Create answer
	answer, err := s.peerConnection.CreateAnswer(nil)
	if err != nil {
		return "", err
	}

	// Sets the LocalDescription, and starts our UDP listeners
	if err = s.peerConnection.SetLocalDescription(answer); err != nil {
		return "", err
	}

	localDescription, err := json.Marshal(s.peerConnection.LocalDescription())
	if err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(localDescription), nil
}

func (s *Session) initQueues() {
	s.hidQueue = make([]chan hidQueueMessage, 0)
	for i := 0; i < 4; i++ {
		s.hidQueue = append(s.hidQueue, make(chan hidQueueMessage, 256))
	}
}

func (s *Session) handleHidQueue(queue <-chan hidQueueMessage) {
	for {
		select {
		case <-s.done:
			return
		default:
		}

		select {
		case <-s.done:
			return
		case msg := <-queue:
			onHidMessage(msg, s)
		}
	}
}

func (s *Session) enqueueHidMessage(queueIndex int, msg hidQueueMessage) bool {
	if s == nil || s.isClosed() {
		return false
	}

	if queueIndex >= len(s.hidQueue) || queueIndex < 0 {
		return false
	}

	queue := s.hidQueue[queueIndex]
	if queue == nil {
		return false
	}

	select {
	case queue <- msg:
		return true
	case <-s.done:
		return false
	}
}

const keysDownStateQueueSize = 64

func (s *Session) initKeysDownStateQueue() {
	// serialise outbound key state reports so unreliable links can't stall input handling
	queue := make(chan usbgadget.KeysDownState, keysDownStateQueueSize)
	s.keysDownStateQueue = queue
	go s.handleKeysDownStateQueue(queue)
}

func (s *Session) handleKeysDownStateQueue(queue <-chan usbgadget.KeysDownState) {
	for {
		select {
		case <-s.done:
			return
		default:
		}

		select {
		case <-s.done:
			return
		case state := <-queue:
			s.reportHidRPCKeysDownState(state)
		}
	}
}

func (s *Session) enqueueKeysDownState(state usbgadget.KeysDownState) {
	if s == nil || s.isClosed() {
		return
	}

	if s.keysDownStateQueue == nil {
		return
	}

	select {
	case s.keysDownStateQueue <- state:
	default:
		hidRPCLogger.Warn().Msg("dropping keys down state update; queue full")
	}
}

func (s *Session) enqueueRPCMessage(msg webrtc.DataChannelMessage) bool {
	if s == nil || s.rpcQueue == nil || s.isClosed() {
		return false
	}

	select {
	case s.rpcQueue <- msg:
		return true
	case <-s.done:
		return false
	}
}

func (s *Session) isClosed() bool {
	select {
	case <-s.done:
		return true
	default:
		return false
	}
}

func (s *Session) close() {
	s.closeOnce.Do(func() {
		close(s.done)
	})
}

func getOnHidMessageHandler(session *Session, scopedLogger *zerolog.Logger, channel string) func(msg webrtc.DataChannelMessage) {
	return func(msg webrtc.DataChannelMessage) {
		l := scopedLogger.With().
			Str("channel", channel).
			Int("length", len(msg.Data)).
			Logger()
		// only log data if the log level is debug or lower
		if scopedLogger.GetLevel() > zerolog.DebugLevel {
			l = l.With().Str("data", string(msg.Data)).Logger()
		}

		if msg.IsString {
			l.Warn().Msg("received string data in HID RPC message handler")
			return
		}

		if len(msg.Data) < 1 {
			l.Warn().Msg("received empty data in HID RPC message handler")
			return
		}

		l.Trace().Msg("received data in HID RPC message handler")

		// Enqueue to ensure ordered processing
		queueIndex := hidrpc.GetQueueIndex(hidrpc.MessageType(msg.Data[0]))
		if queueIndex >= len(session.hidQueue) || queueIndex < 0 {
			l.Warn().Int("queueIndex", queueIndex).Msg("received data in HID RPC message handler, but queue index not found")
			queueIndex = 3
		}

		if ok := session.enqueueHidMessage(queueIndex, hidQueueMessage{
			DataChannelMessage: msg,
			channel:            channel,
		}); !ok {
			l.Warn().Int("queueIndex", queueIndex).Msg("received data in HID RPC message handler, but queue is nil")
			return
		}
	}
}

func newSession(config SessionConfig) (*Session, error) {
	webrtcSettingEngine := webrtc.SettingEngine{
		LoggerFactory: logging.GetPionDefaultLoggerFactory(),
	}

	if config.MDNSMode != "" && config.MDNSMode != "disabled" {
		webrtcSettingEngine.SetICEMulticastDNSMode(ice.MulticastDNSModeQueryOnly)
	} else {
		webrtcSettingEngine.SetICEMulticastDNSMode(ice.MulticastDNSModeDisabled)
	}

	iceServer := webrtc.ICEServer{}

	var scopedLogger *zerolog.Logger
	if config.Logger != nil {
		l := config.Logger.With().Str("component", "webrtc").Logger()
		scopedLogger = &l
	} else {
		scopedLogger = webrtcLogger
	}

	if config.IsCloud {
		if config.ICEServers == nil {
			scopedLogger.Info().Msg("ICE Servers not provided by cloud")
		} else {
			iceServer.URLs = config.ICEServers
			scopedLogger.Info().Interface("iceServers", iceServer.URLs).Msg("Using ICE Servers provided by cloud")
		}

		if config.LocalIP == "" || net.ParseIP(config.LocalIP) == nil {
			scopedLogger.Info().Str("localIP", config.LocalIP).Msg("Local IP address not provided or invalid, won't set ICEAddressRewriteRules")
		} else {
			err := webrtcSettingEngine.SetICEAddressRewriteRules(
				webrtc.ICEAddressRewriteRule{
					CIDR:            "0.0.0.0/0",
					External:        []string{config.LocalIP},
					Mode:            webrtc.ICEAddressRewriteAppend,
					AsCandidateType: webrtc.ICECandidateTypeSrflx,
				},
			)
			if err != nil {
				scopedLogger.Warn().Err(err).Str("localIP", config.LocalIP).Msg("Failed to set ICEAddressRewriteRules")
			} else {
				scopedLogger.Info().Str("localIP", config.LocalIP).Msg("Set ICEAddressRewriteRules for local IP")
			}
		}
	}

	mediaEngine := &webrtc.MediaEngine{}
	if err := mediaEngine.RegisterDefaultCodecs(); err != nil {
		scopedLogger.Warn().Err(err).Msg("Failed to register default codecs")
		return nil, err
	}
	// Negotiate the playout-delay RTP header extension on both audio and
	// video. The interceptor below stamps min=max=0 on every outgoing
	// packet so Chrome's receive-side jitter buffer can't ratchet upward.
	// Audio is registered too because Chrome's AV-sync layer pulls video
	// up to whatever the audio jitter buffer is — pinning video alone
	// isn't enough when the USB UAC1 capture path has any inherent
	// latency.
	for _, kind := range []webrtc.RTPCodecType{webrtc.RTPCodecTypeVideo, webrtc.RTPCodecTypeAudio} {
		if err := mediaEngine.RegisterHeaderExtension(
			webrtc.RTPHeaderExtensionCapability{URI: playoutdelay.URI},
			kind,
		); err != nil {
			scopedLogger.Warn().Err(err).Msg("Failed to register playout-delay header extension")
			return nil, err
		}
	}
	interceptorRegistry := &interceptor.Registry{}
	if err := webrtc.RegisterDefaultInterceptors(mediaEngine, interceptorRegistry); err != nil {
		scopedLogger.Warn().Err(err).Msg("Failed to register default interceptors")
		return nil, err
	}
	interceptorRegistry.Add(playoutdelay.NewFactory())

	api := webrtc.NewAPI(
		webrtc.WithSettingEngine(webrtcSettingEngine),
		webrtc.WithMediaEngine(mediaEngine),
		webrtc.WithInterceptorRegistry(interceptorRegistry),
	)
	peerConnection, err := api.NewPeerConnection(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{iceServer},
	})
	if err != nil {
		scopedLogger.Warn().Err(err).Msg("Failed to create PeerConnection")
		return nil, err
	}

	session := &Session{
		peerConnection: peerConnection,
		done:           make(chan struct{}),
		rpcQueue:       make(chan webrtc.DataChannelMessage, 256),
	}
	session.initQueues()
	session.initKeysDownStateQueue()

	rpcQueue := session.rpcQueue
	go func() {
		for {
			select {
			case <-session.done:
				return
			default:
			}

			select {
			case <-session.done:
				return
			case msg := <-rpcQueue:
				// TODO: only use goroutine if the task is asynchronous
				go onRPCMessage(msg, session)
			}
		}
	}()

	for _, queue := range session.hidQueue {
		go session.handleHidQueue(queue)
	}

	peerConnection.OnDataChannel(func(d *webrtc.DataChannel) {
		defer func() {
			if r := recover(); r != nil {
				scopedLogger.Error().Interface("error", r).Msg("Recovered from panic in DataChannel handler")
			}
		}()

		scopedLogger.Info().Str("label", d.Label()).Uint16("id", *d.ID()).Msg("New DataChannel")

		switch d.Label() {
		case "hidrpc":
			session.HidChannel = d
			d.OnMessage(getOnHidMessageHandler(session, scopedLogger, "hidrpc"))
		// we won't send anything over the unreliable channels
		case "hidrpc-unreliable-ordered":
			d.OnMessage(getOnHidMessageHandler(session, scopedLogger, "hidrpc-unreliable-ordered"))
		case "hidrpc-unreliable-nonordered":
			d.OnMessage(getOnHidMessageHandler(session, scopedLogger, "hidrpc-unreliable-nonordered"))
		case "rpc":
			session.RPCChannel = d
			d.OnMessage(func(msg webrtc.DataChannelMessage) {
				// Enqueue to ensure ordered processing
				session.enqueueRPCMessage(msg)
			})
			// Wait for channel to be open before sending initial state
			d.OnOpen(func() {
				triggerOTAStateUpdate(otaState.ToRPCState())
				triggerVideoStateUpdate()
				triggerUSBStateUpdate()
				notifyFailsafeMode(session)
			})
		case "terminal":
			handleTerminalChannel(d)
		case "serial":
			handleSerialChannel(d)
		case "cdcacm":
			handleCDCACMChannel(d)
		default:
			if strings.HasPrefix(d.Label(), uploadIdPrefix) {
				go handleUploadChannel(d)
			}
		}
	})

	var isConnected bool

	peerConnection.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		scopedLogger.Info().Interface("candidate", candidate).Msg("WebRTC peerConnection has a new ICE candidate")
		if candidate != nil && config.ws != nil {
			err := wsjson.Write(context.Background(), config.ws, gin.H{"type": "new-ice-candidate", "data": candidate.ToJSON()})
			if err != nil {
				scopedLogger.Warn().Err(err).Msg("failed to write new-ice-candidate to WebRTC signaling channel")
			}
		}
	})

	peerConnection.OnICEConnectionStateChange(func(connectionState webrtc.ICEConnectionState) {
		scopedLogger.Info().Str("connectionState", connectionState.String()).Msg("ICE Connection State has changed")
		if connectionState == webrtc.ICEConnectionStateConnected {
			if !isConnected {
				isConnected = true
				onActiveSessionsChanged()
				if incrActiveSessions() == 1 {
					onFirstSessionConnected()
				}
				onSessionConnected(session)
				if mqttManager != nil {
					mqttManager.publishSessionsState()
				}
			}
		}
		//state changes on closing browser tab disconnected->failed, we need to manually close it
		if connectionState == webrtc.ICEConnectionStateDisconnected ||
			connectionState == webrtc.ICEConnectionStateFailed {
			scopedLogger.Debug().Str("state", connectionState.String()).Msg("ICE connection lost, closing peerConnection")
			_ = peerConnection.Close()
		}
		if connectionState == webrtc.ICEConnectionStateClosed {
			scopedLogger.Debug().Msg("ICE Connection State is closed, unmounting virtual media")
			if session == currentSession {
				// Cancel any ongoing keyboard report multi when session closes
				cancelKeyboardMacro()
				// Stop pending auto-release timers (avoids unnecessary work),
				// then clear all keys. keyboardMutex inside KeyboardReport
				// serialises with any auto-release goroutine already in flight,
				// so the clear is guaranteed to be the final state.
				gadget.CancelAllAutoReleaseTimers()
				_ = rpcKeyboardReport(0, keyboardClearStateKeys)
				currentSession = nil
			}
			session.close()

			// Release audio capture if this session owned it; otherwise the
			// goroutine would keep writing samples to a now-dead track.
			stopAudioIfOwner(session.AudioTrack)

			if session.shouldUmountVirtualMedia {
				if err := rpcUnmountImage(); err != nil {
					scopedLogger.Warn().Err(err).Msg("unmount image failed on connection close")
				}
			}
			if isConnected {
				isConnected = false
				onActiveSessionsChanged()
				if decrActiveSessions() == 0 {
					scopedLogger.Info().Msg("last session disconnected, stopping video stream")
					onLastSessionDisconnected()
				}
				if mqttManager != nil {
					mqttManager.publishSessionsState()
				}
			}
		}
	})
	return session, nil
}

func onActiveSessionsChanged() {
	notifyFailsafeMode(currentSession)
	requestDisplayUpdate(false, "active_sessions_changed")
}

// onFirstSessionConnected runs once on the 0→1 active-session edge. Video
// capture is a shared pipeline; starting it again on a handoff connect (count
// 1→2) would issue redundant native start calls and re-run the sleep-mode
// re-lock wait while video is already streaming.
func onFirstSessionConnected() {
	stopVideoSleepModeTicker()
	_ = nativeInstance.VideoStart()
}

// onSessionConnected runs per session when ICE reaches Connected. Uses the
// session parameter directly rather than the currentSession global — that
// global is assigned by the caller AFTER ExchangeOffer returns, and ICE
// connected can fire before then, racing the assignment.
func onSessionConnected(session *Session) {
	notifyFailsafeMode(session)
	if session.codecMimeType == webrtc.MimeTypeH265 {
		_ = nativeInstance.VideoSetCodecType(1)
	} else {
		_ = nativeInstance.VideoSetCodecType(0)
	}
	if session.AudioTrack != nil {
		startAudio(session.AudioTrack)
	}
}

func onLastSessionDisconnected() {
	// Safety net: ensure all keys are released when the last session disconnects
	_ = rpcKeyboardReport(0, keyboardClearStateKeys)
	stopAudio()
	_ = nativeInstance.VideoStop()
	startVideoSleepModeTicker()
}
