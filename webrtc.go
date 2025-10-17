package kvm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/gin-gonic/gin"
	"github.com/jetkvm/kvm/internal/hidrpc"
	"github.com/jetkvm/kvm/internal/logging"
	"github.com/jetkvm/kvm/internal/usbgadget"
	"github.com/pion/webrtc/v4"
	"github.com/rs/zerolog"
)

// Predefined browser string constants for memory efficiency
var (
	BrowserChrome  = "chrome"
	BrowserFirefox = "firefox"
	BrowserSafari  = "safari"
	BrowserEdge    = "edge"
	BrowserOpera   = "opera"
	BrowserUnknown = "user"
)

type Session struct {
	ID            string
	Mode          SessionMode
	Source        string
	Identity      string
	Nickname      string
	Browser       *string // Pointer to predefined browser string constant for memory efficiency
	CreatedAt     time.Time
	LastActive    time.Time
	LastBroadcast time.Time // Per-session broadcast throttle

	// RPC rate limiting (DoS protection)
	rpcRateLimitMu  sync.Mutex // Protects rate limit fields
	rpcRateLimit    int        // Count of RPCs in current window
	rpcRateLimitWin time.Time  // Start of current rate limit window
	lastBroadcastMu sync.Mutex // Protects LastBroadcast field

	peerConnection           *webrtc.PeerConnection
	VideoTrack               *webrtc.TrackLocalStaticSample
	ControlChannel           *webrtc.DataChannel
	RPCChannel               *webrtc.DataChannel
	HidChannel               *webrtc.DataChannel
	shouldUmountVirtualMedia bool
	flushCandidates          func()          // Callback to flush buffered ICE candidates
	ws                       *websocket.Conn // WebSocket for critical signaling when RPC unavailable
	rpcQueue                 chan webrtc.DataChannelMessage

	hidRPCAvailable          bool
	lastKeepAliveArrivalTime time.Time  // Track when last keep-alive packet arrived
	lastTimerResetTime       time.Time  // Track when auto-release timer was last reset
	keepAliveJitterLock      sync.Mutex // Protect jitter compensation timing state
	hidQueueLock             sync.Mutex
	hidQueue                 []chan hidQueueMessage

	keysDownStateQueue chan usbgadget.KeysDownState
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

func getActiveSessions() int {
	activeSessionsMutex.Lock()
	defer activeSessionsMutex.Unlock()

	return actionSessions
}

// CheckRPCRateLimit checks if the session has exceeded RPC rate limits (DoS protection)
func (s *Session) CheckRPCRateLimit() bool {
	const (
		maxRPCPerSecond = 500 // Increased to support 10+ concurrent sessions with broadcasts and state updates
		rateLimitWindow = time.Second
	)

	s.rpcRateLimitMu.Lock()
	defer s.rpcRateLimitMu.Unlock()

	now := time.Now()
	// Reset window if it has expired
	if now.Sub(s.rpcRateLimitWin) > rateLimitWindow {
		s.rpcRateLimit = 0
		s.rpcRateLimitWin = now
	}

	s.rpcRateLimit++
	if s.rpcRateLimit > maxRPCPerSecond {
		return false // Rate limit exceeded
	}
	return true // Within limits
}

func (s *Session) resetKeepAliveTime() {
	s.keepAliveJitterLock.Lock()
	defer s.keepAliveJitterLock.Unlock()
	s.lastKeepAliveArrivalTime = time.Time{} // Reset keep-alive timing tracking
	s.lastTimerResetTime = time.Time{}       // Reset auto-release timer tracking
}

// sendWebSocketSignal sends critical state changes via WebSocket (fallback when RPC channel stale)
func (s *Session) sendWebSocketSignal(messageType string, data map[string]interface{}) error {
	if s == nil || s.ws == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := wsjson.Write(ctx, s.ws, gin.H{"type": messageType, "data": data})
	if err != nil {
		webrtcLogger.Debug().Err(err).Str("sessionId", s.ID).Msg("Failed to send WebSocket signal")
		return err
	}

	webrtcLogger.Info().Str("sessionId", s.ID).Str("messageType", messageType).Msg("Sent WebSocket signal")
	return nil
}

type hidQueueMessage struct {
	webrtc.DataChannelMessage
	channel string
}

type SessionConfig struct {
	ICEServers []string
	LocalIP    string
	IsCloud    bool
	UserAgent  string // User agent for browser detection and nickname generation
	ws         *websocket.Conn
	Logger     *zerolog.Logger
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
	s.hidQueueLock.Lock()
	defer s.hidQueueLock.Unlock()

	s.hidQueue = make([]chan hidQueueMessage, 0)
	for i := 0; i < 4; i++ {
		q := make(chan hidQueueMessage, 256)
		s.hidQueue = append(s.hidQueue, q)
	}
}

func (s *Session) handleQueues(index int) {
	for msg := range s.hidQueue[index] {
		onHidMessage(msg, s)
	}
}

const keysDownStateQueueSize = 64

func (s *Session) initKeysDownStateQueue() {
	// serialise outbound key state reports so unreliable links can't stall input handling
	s.keysDownStateQueue = make(chan usbgadget.KeysDownState, keysDownStateQueueSize)
	go s.handleKeysDownStateQueue()
}

func (s *Session) handleKeysDownStateQueue() {
	for state := range s.keysDownStateQueue {
		s.reportHidRPCKeysDownState(state)
	}
}

func (s *Session) enqueueKeysDownState(state usbgadget.KeysDownState) {
	if s == nil || s.keysDownStateQueue == nil {
		return
	}

	select {
	case s.keysDownStateQueue <- state:
	default:
		hidRPCLogger.Warn().Msg("dropping keys down state update; queue full")
	}
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

		queue := session.hidQueue[queueIndex]
		if queue != nil {
			queue <- hidQueueMessage{
				DataChannelMessage: msg,
				channel:            channel,
			}
		} else {
			l.Warn().Int("queueIndex", queueIndex).Msg("received data in HID RPC message handler, but queue is nil")
			return
		}
	}
}

func newSession(config SessionConfig) (*Session, error) {
	webrtcSettingEngine := webrtc.SettingEngine{
		LoggerFactory: logging.GetPionDefaultLoggerFactory(),
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
			scopedLogger.Info().Str("localIP", config.LocalIP).Msg("Local IP address not provided or invalid, won't set NAT1To1IPs")
		} else {
			webrtcSettingEngine.SetNAT1To1IPs([]string{config.LocalIP}, webrtc.ICECandidateTypeSrflx)
			scopedLogger.Info().Str("localIP", config.LocalIP).Msg("Setting NAT1To1IPs")
		}
	}

	api := webrtc.NewAPI(webrtc.WithSettingEngine(webrtcSettingEngine))
	peerConnection, err := api.NewPeerConnection(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{iceServer},
	})
	if err != nil {
		scopedLogger.Warn().Err(err).Msg("Failed to create PeerConnection")
		return nil, err
	}

	session := &Session{
		peerConnection: peerConnection,
		Browser:        extractBrowserFromUserAgent(config.UserAgent),
		ws:             config.ws,
	}
	session.rpcQueue = make(chan webrtc.DataChannelMessage, 256)
	session.initQueues()
	session.initKeysDownStateQueue()

	go func() {
		for msg := range session.rpcQueue {
			// TODO: only use goroutine if the task is asynchronous
			go onRPCMessage(msg, session)
		}
	}()

	for i := 0; i < len(session.hidQueue); i++ {
		go session.handleQueues(i)
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
				queueLen := len(session.rpcQueue)
				if queueLen > 200 {
					scopedLogger.Warn().
						Str("sessionID", session.ID).
						Int("queueLen", queueLen).
						Msg("RPC queue approaching capacity")
				}
				session.rpcQueue <- msg
			})
			triggerOTAStateUpdate()
			triggerVideoStateUpdate()
			triggerUSBStateUpdate()
		case "terminal":
			handleTerminalChannel(d, session)
		case "serial":
			handleSerialChannel(d, session)
		default:
			if strings.HasPrefix(d.Label(), uploadIdPrefix) {
				go handleUploadChannel(d)
			}
		}
	})

	session.VideoTrack, err = webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264}, "video", "kvm")
	if err != nil {
		scopedLogger.Warn().Err(err).Msg("Failed to create VideoTrack")
		return nil, err
	}

	rtpSender, err := peerConnection.AddTrack(session.VideoTrack)
	if err != nil {
		scopedLogger.Warn().Err(err).Msg("Failed to add VideoTrack to PeerConnection")
		return nil, err
	}

	// Read incoming RTCP packets
	// Before these packets are returned they are processed by interceptors. For things
	// like NACK this needs to be called.
	go func() {
		rtcpBuf := make([]byte, 1500)
		for {
			if _, _, rtcpErr := rtpSender.Read(rtcpBuf); rtcpErr != nil {
				return
			}
		}
	}()
	var isConnected bool

	// Buffer to hold ICE candidates until answer is sent
	var candidateBuffer []webrtc.ICECandidateInit
	var candidateBufferMutex sync.Mutex
	var answerSent bool

	peerConnection.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		scopedLogger.Info().Interface("candidate", candidate).Msg("WebRTC peerConnection has a new ICE candidate")
		if candidate != nil {
			candidateBufferMutex.Lock()
			if !answerSent {
				// Buffer the candidate until answer is sent
				candidateBuffer = append(candidateBuffer, candidate.ToJSON())
				candidateBufferMutex.Unlock()
				return
			}
			candidateBufferMutex.Unlock()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			err := wsjson.Write(ctx, config.ws, gin.H{"type": "new-ice-candidate", "data": candidate.ToJSON()})
			if err != nil {
				scopedLogger.Warn().Err(err).Msg("failed to write new-ice-candidate to WebRTC signaling channel")
			}
		}
	})

	// Store the callback to flush buffered candidates
	session.flushCandidates = func() {
		candidateBufferMutex.Lock()
		answerSent = true
		// Send all buffered candidates
		for _, candidate := range candidateBuffer {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err := wsjson.Write(ctx, config.ws, gin.H{"type": "new-ice-candidate", "data": candidate})
			cancel()
			if err != nil {
				scopedLogger.Warn().Err(err).Msg("failed to write buffered new-ice-candidate to WebRTC signaling channel")
			}
		}
		candidateBuffer = nil
		candidateBufferMutex.Unlock()
	}

	// Track cleanup state to prevent double cleanup
	var cleanedUp bool
	var cleanupMutex sync.Mutex

	cleanupSession := func(reason string) {
		cleanupMutex.Lock()
		defer cleanupMutex.Unlock()

		if cleanedUp {
			return
		}
		cleanedUp = true

		scopedLogger.Info().
			Str("sessionID", session.ID).
			Str("reason", reason).
			Msg("Cleaning up session")

		// Remove from session manager
		sessionManager.RemoveSession(session.ID)

		// Cancel any ongoing keyboard macro if session has permission
		if session.HasPermission(PermissionPaste) {
			cancelKeyboardMacro()
		}

		// Stop RPC processor
		if session.rpcQueue != nil {
			close(session.rpcQueue)
			session.rpcQueue = nil
		}

		// Stop HID RPC processor
		for i := 0; i < len(session.hidQueue); i++ {
			if session.hidQueue[i] != nil {
				close(session.hidQueue[i])
				session.hidQueue[i] = nil
			}
		}

		if session.keysDownStateQueue != nil {
			close(session.keysDownStateQueue)
			session.keysDownStateQueue = nil
		}

		if session.shouldUmountVirtualMedia {
			if err := rpcUnmountImage(); err != nil {
				scopedLogger.Warn().Err(err).Msg("unmount image failed on connection close")
			}
		}

		if isConnected {
			isConnected = false
			actionSessions--
			onActiveSessionsChanged()
			if actionSessions == 0 {
				onLastSessionDisconnected()
			}
		}
	}

	peerConnection.OnICEConnectionStateChange(func(connectionState webrtc.ICEConnectionState) {
		scopedLogger.Info().
			Str("sessionID", session.ID).
			Str("connectionState", connectionState.String()).
			Msg("ICE Connection State has changed")

		if connectionState == webrtc.ICEConnectionStateConnected {
			if !isConnected {
				isConnected = true
				onActiveSessionsChanged()
				if incrActiveSessions() == 1 {
					onFirstSessionConnected()
				}
			}
		}

		// Handle disconnection and failure states
		if connectionState == webrtc.ICEConnectionStateDisconnected {
			scopedLogger.Info().
				Str("sessionID", session.ID).
				Msg("ICE Connection State is disconnected, connection may recover")
		}

		if connectionState == webrtc.ICEConnectionStateFailed {
			scopedLogger.Info().
				Str("sessionID", session.ID).
				Msg("ICE Connection State is failed, closing peerConnection and cleaning up")
			cleanupSession("ice-failed")
			_ = peerConnection.Close()
		}

		if connectionState == webrtc.ICEConnectionStateClosed {
			scopedLogger.Info().
				Str("sessionID", session.ID).
				Msg("ICE Connection State is closed, cleaning up")
			cleanupSession("ice-closed")
		}
	})
	return session, nil
}

func onActiveSessionsChanged() {
	requestDisplayUpdate(true, "active_sessions_changed")
}

func onFirstSessionConnected() {
	_ = nativeInstance.VideoStart()
	stopVideoSleepModeTicker()
}

func onLastSessionDisconnected() {
	_ = nativeInstance.VideoStop()
	startVideoSleepModeTicker()
}
