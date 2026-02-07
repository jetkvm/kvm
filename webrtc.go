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

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/gin-gonic/gin"
	"github.com/jetkvm/kvm/internal/crypto"
	"github.com/jetkvm/kvm/internal/diagnostics"
	"github.com/jetkvm/kvm/internal/hidrpc"
	"github.com/jetkvm/kvm/internal/logging"
	"github.com/jetkvm/kvm/internal/usbgadget"
	"github.com/pion/ice/v4"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
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
	hidQueueLock             sync.Mutex
	hidQueue                 []chan hidQueueMessage

	keysDownStateQueue chan usbgadget.KeysDownState

	// Pre-allocated sample to avoid allocation per video frame
	videoSample media.Sample
}

// WriteVideoFrame writes a video frame without allocating a new Sample struct.
func (s *Session) WriteVideoFrame(frame []byte, duration time.Duration) error {
	s.videoSample.Data = frame
	s.videoSample.Duration = duration
	return s.VideoTrack.WriteSample(s.videoSample)
}

var (
	activeSessions atomic.Int32

	// logHardwareCryptoOnce ensures we log hardware crypto status only once
	logHardwareCryptoOnce sync.Once
)

func incrActiveSessions() int {
	return int(activeSessions.Add(1))
}

func decrActiveSessions() int {
	for {
		old := activeSessions.Load()
		if old <= 0 {
			return 0
		}
		if activeSessions.CompareAndSwap(old, old-1) {
			return int(old - 1)
		}
	}
}

func getActiveSessions() int {
	return int(activeSessions.Load())
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

func (s *Session) initKeysDownStateQueue(logger *zerolog.Logger) {
	// serialise outbound key state reports so unreliable links can't stall input handling
	s.keysDownStateQueue = make(chan usbgadget.KeysDownState, keysDownStateQueueSize)
	logging.SafeGo(logger, "WEBRTC_KEYS_STATE", s.handleKeysDownStateQueue)
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
		// Recover from send-on-closed-channel if ICE closes during callback
		defer func() {
			if r := recover(); r != nil {
				scopedLogger.Debug().Interface("recover", r).Msg("HID queue send recovered (connection closing)")
			}
		}()
		if msg.IsString {
			scopedLogger.Warn().Str("channel", channel).Msg("received string data in HID RPC message handler")
			return
		}

		if len(msg.Data) < 1 {
			scopedLogger.Warn().Str("channel", channel).Msg("received empty data in HID RPC message handler")
			return
		}

		if scopedLogger.GetLevel() <= zerolog.TraceLevel {
			scopedLogger.Trace().
				Str("channel", channel).
				Int("length", len(msg.Data)).
				Str("data", string(msg.Data)).
				Msg("received data in HID RPC message handler")
		}

		// Enqueue to ensure ordered processing
		queueIndex := hidrpc.GetQueueIndex(hidrpc.MessageType(msg.Data[0]))
		if queueIndex >= len(session.hidQueue) || queueIndex < 0 {
			scopedLogger.Warn().Str("channel", channel).Int("queueIndex", queueIndex).Msg("received data in HID RPC message handler, but queue index not found")
			queueIndex = 3
		}

		queue := session.hidQueue[queueIndex]
		if queue != nil {
			queue <- hidQueueMessage{
				DataChannelMessage: msg,
				channel:            channel,
			}
		} else {
			scopedLogger.Warn().Str("channel", channel).Int("queueIndex", queueIndex).Msg("received data in HID RPC message handler, but queue is nil")
			return
		}
	}
}

func newSession(config SessionConfig) (*Session, error) {
	webrtcSettingEngine := webrtc.SettingEngine{
		LoggerFactory: logging.GetPionDefaultLoggerFactory(),
	}

	// Use hardware-accelerated cipher suites for DTLS if available
	// This offloads AES-GCM encryption/decryption to the RV1106 crypto engine
	webrtcSettingEngine.SetDTLSCustomerCipherSuites(crypto.HardwareCipherSuites)

	// Note: SRTP uses AES-CM-HMAC-SHA1 (default) rather than AES-GCM because
	// GHASH (used in GCM) is slower than SHA1 in pure software on ARM without
	// PMULL instructions. Hardware SRTP would require forking pion/srtp.

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
					Mode:            webrtc.ICEAddressRewriteReplace,
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

	api := webrtc.NewAPI(webrtc.WithSettingEngine(webrtcSettingEngine))
	peerConnection, err := api.NewPeerConnection(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{iceServer},
	})
	if err != nil {
		scopedLogger.Warn().Err(err).Msg("Failed to create PeerConnection")
		return nil, err
	}

	session := &Session{peerConnection: peerConnection}
	session.rpcQueue = make(chan webrtc.DataChannelMessage, 256)
	session.initQueues()
	session.initKeysDownStateQueue(scopedLogger)

	// Cleanup goroutines and resources if newSession fails after this point.
	// The queue consumer goroutines (spawned below) block on range over channels,
	// so we must close the channels to unblock them on error.
	cleanupOnError := true
	defer func() {
		if cleanupOnError {
			close(session.rpcQueue)
			for i := range session.hidQueue {
				close(session.hidQueue[i])
			}
			close(session.keysDownStateQueue)
			_ = peerConnection.Close()
		}
	}()

	// Log hardware crypto status once (after first DTLS-enabled session is created)
	logHardwareCryptoOnce.Do(func() {
		if err := crypto.HardwareCryptoError(); err != nil {
			scopedLogger.Warn().Err(err).Msg("Hardware crypto unavailable, using software AES-GCM for DTLS")
		} else {
			scopedLogger.Info().Msg("Using hardware-accelerated AES-GCM for DTLS")
		}
	})

	logging.SafeGo(scopedLogger, "WEBRTC_RPC_QUEUE", func() {
		for msg := range session.rpcQueue {
			onRPCMessage(msg, session)
		}
	})

	for i := 0; i < len(session.hidQueue); i++ {
		idx := i
		logging.SafeGo(scopedLogger, "WEBRTC_HID_QUEUE", func() { session.handleQueues(idx) })
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
				// Recover from send-on-closed-channel if ICE closes during callback
				defer func() {
					if r := recover(); r != nil {
						scopedLogger.Debug().Interface("recover", r).Msg("RPC queue send recovered (connection closing)")
					}
				}()
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
		scopedLogger.Warn().Err(err).Msg("Failed to create VideoTrack")
		return nil, err
	}

	// Use sendrecv transceiver to allow browser to send camera video back
	videoTransceiver, err := peerConnection.AddTransceiverFromTrack(session.VideoTrack, webrtc.RTPTransceiverInit{
		Direction: webrtc.RTPTransceiverDirectionSendrecv,
	})
	if err != nil {
		scopedLogger.Warn().Err(err).Msg("Failed to add VideoTrack transceiver")
		return nil, err
	}

	// Set codec preferences to prefer H.264 for incoming camera video
	// This is required for UVC passthrough since we can't transcode VP8/VP9 to H.264
	videoCodecs := videoTransceiver.Receiver().GetParameters().Codecs
	var h264Codecs []webrtc.RTPCodecParameters
	var otherCodecs []webrtc.RTPCodecParameters
	for _, codec := range videoCodecs {
		if strings.Contains(strings.ToLower(codec.MimeType), "h264") {
			h264Codecs = append(h264Codecs, codec)
		} else {
			otherCodecs = append(otherCodecs, codec)
		}
	}
	// Put H.264 codecs first to prefer them
	preferredCodecs := append(h264Codecs, otherCodecs...)
	if len(preferredCodecs) > 0 {
		if err := videoTransceiver.SetCodecPreferences(preferredCodecs); err != nil {
			scopedLogger.Warn().Err(err).Msg("Failed to set H.264 codec preference (non-fatal)")
		} else {
			scopedLogger.Info().Int("h264_count", len(h264Codecs)).Msg("Set H.264 as preferred codec for incoming video")
		}
	}

	// Read incoming RTCP packets
	// Before these packets are returned they are processed by interceptors. For things
	// like NACK this needs to be called.
	go func() {
		rtcpBuf := make([]byte, 1500)
		rtpSender := videoTransceiver.Sender()
		for {
			if _, _, rtcpErr := rtpSender.Read(rtcpBuf); rtcpErr != nil {
				// Log RTCP reader exit - silent exit here causes video quality degradation
				// as NACK/retransmission stops working
				scopedLogger.Debug().Err(rtcpErr).Msg("RTCP reader exiting")
				return
			}
		}
	}()

	session.AudioTrack, err = webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus},
		"audio",
		"kvm-audio",
	)
	if err != nil {
		scopedLogger.Warn().Err(err).Msg("Failed to create AudioTrack (non-fatal)")
		session.AudioTrack = nil
	} else {
		_, err = peerConnection.AddTransceiverFromTrack(session.AudioTrack, webrtc.RTPTransceiverInit{
			Direction: webrtc.RTPTransceiverDirectionSendrecv,
		})
		if err != nil {
			scopedLogger.Warn().Err(err).Msg("Failed to add AudioTrack transceiver (non-fatal)")
			session.AudioTrack = nil
		} else {
			setAudioTrack(session.AudioTrack)

			peerConnection.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
				codec := track.Codec()
				scopedLogger.Info().
					Str("codec", codec.MimeType).
					Str("track_id", track.ID()).
					Str("kind", track.Kind().String()).
					Msg("Received incoming track from browser")

				switch track.Kind() {
				case webrtc.RTPCodecTypeAudio:
					// Store audio track for connection when audio starts
					// OnTrack fires during SDP exchange, before ICE connection completes
					setPendingInputTrack(track)
				case webrtc.RTPCodecTypeVideo:
					// Handle incoming camera video from browser for UVC passthrough
					handleCameraVideoTrack(track)
				}
			})

			scopedLogger.Info().Msg("Audio tracks configured successfully")
		}
	}

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
			}
		}
		//state changes on closing browser tab disconnected->failed, we need to manually close it
		if connectionState == webrtc.ICEConnectionStateFailed {
			scopedLogger.Debug().Msg("ICE Connection State is failed, closing peerConnection")
			_ = peerConnection.Close()
		}
		if connectionState == webrtc.ICEConnectionStateClosed {
			scopedLogger.Debug().Msg("ICE Connection State is closed, unmounting virtual media")
			// Only clear currentSession if this is actually the current session
			// This prevents race condition where old session closes after new one connects
			if currentSession.CompareAndSwap(session, nil) {
				// Cancel any ongoing keyboard report multi when session closes
				cancelKeyboardMacro()
			}
			// Stop RPC processor
			if session.rpcQueue != nil {
				close(session.rpcQueue)
				session.rpcQueue = nil
			}

			// Stop HID RPC processor
			for i := 0; i < len(session.hidQueue); i++ {
				close(session.hidQueue[i])
				session.hidQueue[i] = nil
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
				onActiveSessionsChanged()
				if decrActiveSessions() == 0 {
					scopedLogger.Info().Msg("last session disconnected, stopping video stream")
					onLastSessionDisconnected()
				}
			}
		}
	})

	cleanupOnError = false
	return session, nil
}

func onActiveSessionsChanged() {
	notifyFailsafeMode(currentSession.Load())
	requestDisplayUpdate(true, "active_sessions_changed")
}

func onFirstSessionConnected() {
	notifyFailsafeMode(currentSession.Load())
	_ = nativeInstance.VideoStart()
	onWebRTCConnect()
	stopVideoSleepModeTicker()
}

func onLastSessionDisconnected() {
	_ = nativeInstance.VideoStop()
	onWebRTCDisconnect()
	startVideoSleepModeTicker()
}
