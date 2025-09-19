package kvm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/gin-gonic/gin"
	"github.com/jetkvm/kvm/internal/audio"
	"github.com/jetkvm/kvm/internal/hidrpc"
	"github.com/jetkvm/kvm/internal/logging"
	"github.com/jetkvm/kvm/internal/usbgadget"
	"github.com/pion/webrtc/v4"
	"github.com/rs/zerolog"
)

type Session struct {
	peerConnection           *webrtc.PeerConnection
	VideoTrack               *webrtc.TrackLocalStaticSample
	AudioTrack               *webrtc.TrackLocalStaticSample
	AudioRtpSender           *webrtc.RTPSender
	ControlChannel           *webrtc.DataChannel
	RPCChannel               *webrtc.DataChannel
	HidChannel               *webrtc.DataChannel
	DiskChannel              *webrtc.DataChannel
	AudioInputManager        *audio.AudioInputManager
	shouldUmountVirtualMedia bool
	micCooldown              time.Duration
	audioFrameChan           chan []byte
	audioStopChan            chan struct{}
	audioWg                  sync.WaitGroup

	rpcQueue chan webrtc.DataChannelMessage

	hidRPCAvailable          bool
	lastKeepAliveArrivalTime time.Time  // Track when last keep-alive packet arrived
	lastTimerResetTime       time.Time  // Track when auto-release timer was last reset
	keepAliveJitterLock      sync.Mutex // Protect jitter compensation timing state
	hidQueueLock             sync.Mutex
	hidQueue                 []chan hidQueueMessage

	keysDownStateQueue chan usbgadget.KeysDownState
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
		peerConnection:    peerConnection,
		AudioInputManager: audio.NewAudioInputManager(),
		micCooldown:       100 * time.Millisecond,
		audioFrameChan:    make(chan []byte, 1000),
		audioStopChan:     make(chan struct{}),
	}

	// Start audio processing goroutine
	session.startAudioProcessor(*logger)

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
				// Enqueue to ensure ordered processing
				session.rpcQueue <- msg
			})
			triggerOTAStateUpdate()
			triggerVideoStateUpdate()
			triggerUSBStateUpdate()
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

	session.VideoTrack, err = webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264}, "video", "kvm-video")
	if err != nil {
		scopedLogger.Warn().Err(err).Msg("Failed to create VideoTrack")
		return nil, err
	}

	session.AudioTrack, err = webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus}, "audio", "kvm-audio")
	if err != nil {
		scopedLogger.Warn().Err(err).Msg("Failed to add VideoTrack to PeerConnection")
		return nil, err
	}

	// Update the audio relay with the new WebRTC audio track asynchronously
	// This prevents blocking during session creation and avoids mutex deadlocks
	audio.UpdateAudioRelayTrackAsync(session.AudioTrack)

	videoRtpSender, err := peerConnection.AddTrack(session.VideoTrack)
	if err != nil {
		return nil, err
	}

	// Add bidirectional audio transceiver for microphone input
	audioTransceiver, err := peerConnection.AddTransceiverFromTrack(session.AudioTrack, webrtc.RTPTransceiverInit{
		Direction: webrtc.RTPTransceiverDirectionSendrecv,
	})
	if err != nil {
		return nil, err
	}
	audioRtpSender := audioTransceiver.Sender()
	session.AudioRtpSender = audioRtpSender

	// Handle incoming audio track (microphone from browser)
	peerConnection.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		scopedLogger.Info().Str("codec", track.Codec().MimeType).Str("id", track.ID()).Msg("Got remote track")

		if track.Kind() == webrtc.RTPCodecTypeAudio && track.Codec().MimeType == webrtc.MimeTypeOpus {
			scopedLogger.Info().Msg("Processing incoming audio track for microphone input")

			go func() {
				// Lock to OS thread to isolate RTP processing
				runtime.LockOSThread()
				defer runtime.UnlockOSThread()

				for {
					rtpPacket, _, err := track.ReadRTP()
					if err != nil {
						scopedLogger.Debug().Err(err).Msg("Error reading RTP packet from audio track")
						return
					}

					// Extract Opus payload from RTP packet
					opusPayload := rtpPacket.Payload
					if len(opusPayload) > 0 {
						// Send to buffered channel for processing
						select {
						case session.audioFrameChan <- opusPayload:
							// Frame sent successfully
						default:
							// Channel is full, drop the frame
							scopedLogger.Warn().Msg("Audio frame channel full, dropping frame")
						}
					}
				}
			}()
		}
	})

	// Read incoming RTCP packets
	// Before these packets are returned they are processed by interceptors. For things
	// like NACK this needs to be called.
	go drainRtpSender(videoRtpSender)
	go drainRtpSender(audioRtpSender)

	var isConnected bool

	peerConnection.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		scopedLogger.Info().Interface("candidate", candidate).Msg("WebRTC peerConnection has a new ICE candidate")
		if candidate != nil {
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
				actionSessions++
				onActiveSessionsChanged()
				if actionSessions == 1 {
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
			if session == currentSession {
				// Cancel any ongoing keyboard report multi when session closes
				cancelKeyboardMacro()
				currentSession = nil
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

			close(session.keysDownStateQueue)
			session.keysDownStateQueue = nil

			if session.shouldUmountVirtualMedia {
				if err := rpcUnmountImage(); err != nil {
					scopedLogger.Warn().Err(err).Msg("unmount image failed on connection close")
				}
			}
			// Stop audio processing and input manager
			session.stopAudioProcessor()
			if session.AudioInputManager != nil {
				session.AudioInputManager.Stop()
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
	})
	return session, nil
}

// startAudioProcessor starts the dedicated audio processing goroutine
func (s *Session) startAudioProcessor(logger zerolog.Logger) {
	s.audioWg.Add(1)
	go func() {
		defer s.audioWg.Done()
		logger.Debug().Msg("Audio processor goroutine started")

		for {
			select {
			case frame := <-s.audioFrameChan:
				if s.AudioInputManager != nil {
					// Check if audio input manager is ready before processing frames
					if s.AudioInputManager.IsReady() {
						err := s.AudioInputManager.WriteOpusFrame(frame)
						if err != nil {
							logger.Warn().Err(err).Msg("Failed to write Opus frame to audio input manager")
						}
					} else {
						// Audio input manager not ready, drop frame silently
						// This prevents the "client not connected" errors during startup
						logger.Debug().Msg("Audio input manager not ready, dropping frame")
					}
				}
			case <-s.audioStopChan:
				logger.Debug().Msg("Audio processor goroutine stopping")
				return
			}
		}
	}()
}

// stopAudioProcessor stops the audio processing goroutine
func (s *Session) stopAudioProcessor() {
	close(s.audioStopChan)
	s.audioWg.Wait()
}

// ReplaceAudioTrack replaces the current audio track with a new one
func (s *Session) ReplaceAudioTrack(newTrack *webrtc.TrackLocalStaticSample) error {
	if s.AudioRtpSender == nil {
		return fmt.Errorf("audio RTP sender not available")
	}

	// Replace the track using the RTP sender
	if err := s.AudioRtpSender.ReplaceTrack(newTrack); err != nil {
		return fmt.Errorf("failed to replace audio track: %w", err)
	}

	// Update the session's audio track reference
	s.AudioTrack = newTrack
	return nil
}

func drainRtpSender(rtpSender *webrtc.RTPSender) {
	// Lock to OS thread to isolate RTCP processing
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	rtcpBuf := make([]byte, 1500)
	for {
		if _, _, err := rtpSender.Read(rtcpBuf); err != nil {
			return
		}
	}
}

var actionSessions = 0

func onActiveSessionsChanged() {
	requestDisplayUpdate(true)
}

func onFirstSessionConnected() {
	_ = writeCtrlAction("start_video")
}

func onLastSessionDisconnected() {
	_ = writeCtrlAction("stop_video")
}
