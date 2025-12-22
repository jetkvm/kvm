package kvm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jetkvm/kvm/internal/diagnostics"
	"github.com/jetkvm/kvm/internal/hidrpc"
	"github.com/jetkvm/kvm/internal/logging"
	"github.com/jetkvm/kvm/internal/usbgadget"
	"github.com/jetkvm/kvm/internal/utils"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/gin-gonic/gin"
	"github.com/pion/ice/v4"
	"github.com/pion/webrtc/v4"
	"github.com/rs/zerolog"
)

type Session struct {
	peerConnection           *webrtc.PeerConnection
	VideoTrack               *webrtc.TrackLocalStaticSample
	ControlChannel           *webrtc.DataChannel
	RPCChannel               *webrtc.DataChannel
	HidChannel               *webrtc.DataChannel
	shouldUmountVirtualMedia bool

	rpcQueue chan webrtc.DataChannelMessage

	hidRPCAvailable          bool
	lastKeepAliveArrivalTime time.Time  // Track when last keep-alive packet arrived
	lastTimerResetTime       time.Time  // Track when auto-release timer was last reset
	keepAliveJitterLock      sync.Mutex // Protect jitter compensation timing state
	hidQueueLock             sync.Mutex
	hidQueue                 []chan hidQueueMessage

	keysDownStateQueue chan usbgadget.KeysDownState
}

var activeSessions atomic.Int32

func incrActiveSessions() int32 {
	return activeSessions.Add(1)
}

func decrActiveSessions() int32 {
	return activeSessions.Add(-1)
}

func getActiveSessions() int32 {
	return activeSessions.Load()
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
	s.lastKeepAliveArrivalTime = time.Time{} // Reset keep-alive timing tracking
	s.lastTimerResetTime = time.Time{}       // Reset auto-release timer tracking
	s.keepAliveJitterLock.Unlock()
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

func (s *Session) startupSession() {
	s.rpcQueue = make(chan webrtc.DataChannelMessage, 256)
	s.initQueues()
	s.initKeysDownStateQueue()

	go func() {
		for msg := range s.rpcQueue {
			// TODO: only use goroutine if the task is asynchronous
			go onRPCMessage(msg, s)
		}
	}()

	for i := 0; i < len(s.hidQueue); i++ {
		go s.handleQueue(i)
	}
}

func (s *Session) initQueues() {
	s.hidQueueLock.Lock()
	defer s.hidQueueLock.Unlock()

	s.hidQueue = make([]chan hidQueueMessage, hidrpc.MaximumQueues)
	for i := 0; i < hidrpc.MaximumQueues; i++ {
		q := make(chan hidQueueMessage, 256)
		s.hidQueue[i] = q
	}
}

func (s *Session) shutdownSession() {
	// Stop RPC processor
	if s.rpcQueue != nil {
		close(s.rpcQueue)
		s.rpcQueue = nil
	}

	// Stop HID RPC processors
	if s.hidQueue != nil {
		for i := 0; i < len(s.hidQueue); i++ {
			if s.hidQueue[i] != nil {
				close(s.hidQueue[i])
				s.hidQueue[i] = nil
			}
		}
		s.hidQueue = nil
	}

	if s.keysDownStateQueue != nil {
		close(s.keysDownStateQueue)
		s.keysDownStateQueue = nil
	}

	if s.shouldUmountVirtualMedia {
		go func() {
			if err := rpcUnmountImage(); err != nil {
				logger.Warn().Err(err).Msg("unmount image failed on connection close")
			}
		}()
	}

	if s.ControlChannel != nil {
		go s.ControlChannel.GracefulClose()
		s.ControlChannel = nil
	}

	if s.RPCChannel != nil {
		go s.RPCChannel.GracefulClose()
		s.RPCChannel = nil
	}

	if s.HidChannel != nil {
		go s.HidChannel.GracefulClose()
		s.HidChannel = nil
	}

	s.hidRPCAvailable = false

	// TODO what about the other channels?

	if s.VideoTrack != nil {
		// there's no Close() on this, just set to nil
		s.VideoTrack = nil
	}

	if s.peerConnection != nil {
		go s.peerConnection.GracefulClose()
		s.peerConnection = nil
	}
}

func (s *Session) handleQueue(index int) {
	for msg := range s.hidQueue[index] {
		onHidMessage(msg, s, index)
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
		hidRPCLogger.Error().Msg("dropping keys down state update; queue full")
	}
}

func getOnHidMessageHandler(session *Session, l *zerolog.Logger, channel string) func(msg webrtc.DataChannelMessage) {
	return func(msg webrtc.DataChannelMessage) {
		logger := l.With().Str("channel", channel).Interface("msg", msg).Logger()

		if msg.IsString {
			logger.Warn().Msg("received string data in HID RPC message handler")
			return
		}

		dataLength := len(msg.Data)
		logger = logger.With().Int("length", dataLength).Logger()

		// only log data if the log level is debug or lower
		if logger.GetLevel() <= zerolog.DebugLevel {
			logger = logger.With().Object("data", utils.ByteSlice(msg.Data)).Logger()
		}

		if dataLength < 1 {
			logger.Warn().Msg("received empty data in HID RPC message handler")
			return
		}

		// Enqueue to ensure ordered processing
		queueIndex := hidrpc.GetQueueIndex(hidrpc.MessageType(msg.Data[0]))
		logger = logger.With().Int("queueIndex", queueIndex).Logger()

		if queueIndex >= len(session.hidQueue) || queueIndex < 0 {
			logger.Warn().Msg("received data in HID RPC message handler, but queue index not found")
			queueIndex = 3
		}

		queue := session.hidQueue[queueIndex]
		if queue != nil {
			queue <- hidQueueMessage{
				DataChannelMessage: msg,
				channel:            channel,
			}
			logger.Trace().Msg("queued HID RPC message")
		} else {
			logger.Warn().Msg("received data in HID RPC message handler, but queue is nil")
			return
		}
	}
}

func newSession(config SessionConfig) (*Session, error) {
	webrtcSettingEngine := webrtc.SettingEngine{
		LoggerFactory: logging.GetPionDefaultLoggerFactory(),
	}

	mDNSNetworkTypes := make([]webrtc.NetworkType, 0)
	if config.MDNSMode == "auto" || config.MDNSMode == "ipv4_only" {
		mDNSNetworkTypes = append(mDNSNetworkTypes, webrtc.NetworkTypeUDP4)
	}
	if config.MDNSMode == "auto" || config.MDNSMode == "ipv6_only" {
		mDNSNetworkTypes = append(mDNSNetworkTypes, webrtc.NetworkTypeUDP6)
	}

	if len(mDNSNetworkTypes) > 0 {
		webrtcSettingEngine.SetNetworkTypes(mDNSNetworkTypes)
		webrtcSettingEngine.SetICEMulticastDNSMode(ice.MulticastDNSModeQueryOnly)
	} else {
		webrtcSettingEngine.SetICEMulticastDNSMode(ice.MulticastDNSModeDisabled)
	}

	iceServer := webrtc.ICEServer{}

	var logger = webrtcLogger
	if config.Logger != nil {
		l := config.Logger.With().Str("component", "webrtc").Logger()
		logger = &l
	}

	if config.IsCloud {
		if config.ICEServers == nil {
			logger.Info().Msg("ICE Servers not provided by cloud")
		} else {
			iceServer.URLs = config.ICEServers
			logger.Info().Strs("iceServers", iceServer.URLs).Msg("Using ICE Servers provided by cloud")
		}

		if config.LocalIP == "" || net.ParseIP(config.LocalIP) == nil {
			logger.Info().Str("localIP", config.LocalIP).Msg("Local IP address not provided or invalid, won't set NAT1To1IPs")
		} else {
			webrtcSettingEngine.SetNAT1To1IPs([]string{config.LocalIP}, webrtc.ICECandidateTypeSrflx)
			logger.Info().Str("localIP", config.LocalIP).Msg("Setting NAT1To1IPs")
		}
	}

	api := webrtc.NewAPI(webrtc.WithSettingEngine(webrtcSettingEngine))
	peerConnection, err := api.NewPeerConnection(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{iceServer},
	})
	if err != nil {
		logger.Warn().Err(err).Msg("Failed to create PeerConnection")
		return nil, err
	}

	session := &Session{peerConnection: peerConnection}
	session.startupSession()

	peerConnection.OnDataChannel(func(d *webrtc.DataChannel) {
		defer func() {
			if r := recover(); r != nil {
				logger.Error().Interface("recovered", r).Msg("Recovered from panic in DataChannel handler")
			}
		}()

		logger := logger.With().Str("label", d.Label()).Uint16("id", *d.ID()).Logger()
		logger.Info().Msg("New DataChannel")

		switch d.Label() {
		case "hidrpc":
			session.HidChannel = d
			d.OnMessage(getOnHidMessageHandler(session, &logger, "hidrpc"))
		// we won't send anything over the unreliable channels
		case "hidrpc-unreliable-ordered":
			d.OnMessage(getOnHidMessageHandler(session, &logger, "hidrpc-unreliable-ordered"))
		case "hidrpc-unreliable-nonordered":
			d.OnMessage(getOnHidMessageHandler(session, &logger, "hidrpc-unreliable-nonordered"))
		case "rpc":
			session.RPCChannel = d
			d.OnMessage(func(msg webrtc.DataChannelMessage) {
				// Enqueue to ensure ordered processing
				session.rpcQueue <- msg
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
		default:
			if strings.HasPrefix(d.Label(), uploadIdPrefix) {
				go handleUploadChannel(d)
			}
		}
	})

	session.VideoTrack, err = webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264}, "video", "kvm")
	if err != nil {
		logger.Warn().Err(err).Msg("Failed to create VideoTrack")
		return nil, err
	}

	rtpSender, err := peerConnection.AddTrack(session.VideoTrack)
	if err != nil {
		logger.Warn().Err(err).Msg("Failed to add VideoTrack to PeerConnection")
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

	peerConnection.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		logger := logger.With().Interface("candidate", candidate).Logger()
		logger.Info().Msg("WebRTC peerConnection has a new ICE candidate")
		if candidate != nil && config.ws != nil {
			err := wsjson.Write(context.Background(), config.ws, gin.H{"type": "new-ice-candidate", "data": candidate.ToJSON()})
			if err != nil {
				logger.Warn().Err(err).Msg("failed to write new-ice-candidate to WebRTC signaling channel")
			}
		}
	})

	peerConnection.OnICEConnectionStateChange(func(connectionState webrtc.ICEConnectionState) {
		logger := logger.With().Stringer("connectionState", connectionState).Logger()
		logger.Info().Msg("ICE Connection State has changed")
		if connectionState == webrtc.ICEConnectionStateConnected {
			if !isConnected {
				isConnected = true
				onActiveSessionsChanged()
				if incrActiveSessions() == 1 {
					logger.Info().Msg("first session connected, starting video stream")
					onFirstSessionConnected()
				}
			}
		}
		//state changes on closing browser tab disconnected->failed, we need to manually close it
		if connectionState == webrtc.ICEConnectionStateFailed {
			logger.Debug().Msg("ICE Connection State is failed, closing peerConnection")
			_ = peerConnection.Close()
		}
		if connectionState == webrtc.ICEConnectionStateClosed {
			logger.Debug().Msg("ICE Connection State is closed, shutting down session")
			if session == currentSession {
				// Cancel any ongoing keyboard macro when session closes
				_ = cancelKeyboardMacro()
				currentSession = nil
			}

			session.shutdownSession()

			if isConnected {
				isConnected = false
				onActiveSessionsChanged()
				if decrActiveSessions() == 0 {
					logger.Info().Msg("last session disconnected, stopping video stream")
					onLastSessionDisconnected()
				}
			}
		}
	})
	return session, nil
}

func onActiveSessionsChanged() {
	notifyFailsafeMode(currentSession)
	requestDisplayUpdate(true, "active_sessions_changed")
}

func onFirstSessionConnected() {
	notifyFailsafeMode(currentSession)
	_ = nativeInstance.VideoStart()
	stopVideoSleepModeTicker()
}

func onLastSessionDisconnected() {
	_ = nativeInstance.VideoStop()
	startVideoSleepModeTicker()
}
