package kvm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net"
	"strings"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/gin-gonic/gin"
	"github.com/pion/webrtc/v4"
)

type Session struct {
	peerConnection           *webrtc.PeerConnection
	VideoTrack               *webrtc.TrackLocalStaticSample
	ControlChannel           *webrtc.DataChannel
	RPCChannel               *webrtc.DataChannel
	HidChannel               *webrtc.DataChannel
	DiskChannel              *webrtc.DataChannel
	shouldUmountVirtualMedia bool
}

type SessionConfig struct {
	ICEServers []string
	LocalIP    string
	IsCloud    bool
	ws         *websocket.Conn
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

func newSession(config SessionConfig) (*Session, error) {
	webrtcSettingEngine := webrtc.SettingEngine{}
	iceServer := webrtc.ICEServer{}

	if config.IsCloud {
		if config.ICEServers == nil {
			logger.Info("ICE Servers not provided by cloud")
		} else {
			iceServer.URLs = config.ICEServers
			logger.Infof("Using ICE Servers provided by cloud: %v", iceServer.URLs)
		}

		if config.LocalIP == "" || net.ParseIP(config.LocalIP) == nil {
			logger.Infof("Local IP address %v not provided or invalid, won't set NAT1To1IPs", config.LocalIP)
		} else {
			webrtcSettingEngine.SetNAT1To1IPs([]string{config.LocalIP}, webrtc.ICECandidateTypeSrflx)
			logger.Infof("Setting NAT1To1IPs to %s", config.LocalIP)
		}
	}

	api := webrtc.NewAPI(webrtc.WithSettingEngine(webrtcSettingEngine))
	peerConnection, err := api.NewPeerConnection(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{iceServer},
	})
	if err != nil {
		return nil, err
	}
	session := &Session{peerConnection: peerConnection}

	peerConnection.OnDataChannel(func(d *webrtc.DataChannel) {
		logger.Infof("New DataChannel %s %d", d.Label(), d.ID())
		switch d.Label() {
		case "rpc":
			session.RPCChannel = d
			d.OnMessage(func(msg webrtc.DataChannelMessage) {
				go onRPCMessage(msg, session)
			})
			triggerOTAStateUpdate()
			triggerVideoStateUpdate()
			triggerUSBStateUpdate()
		case "disk":
			session.DiskChannel = d
			d.OnMessage(onDiskMessage)
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
		return nil, err
	}

	rtpSender, err := peerConnection.AddTrack(session.VideoTrack)
	if err != nil {
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
		logger.Infof("Our WebRTC peerConnection has a new ICE candidate: %v", candidate)
		if candidate != nil {
			err := wsjson.Write(context.Background(), config.ws, gin.H{"type": "new-ice-candidate", "data": candidate.ToJSON()})
			if err != nil {
				logger.Errorf("failed to write new-ice-candidate to WebRTC signaling channel: %v", err)
			}
		}
	})

	peerConnection.OnICEConnectionStateChange(func(connectionState webrtc.ICEConnectionState) {
		logger.Infof("Connection State has changed %s", connectionState)
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
			_ = peerConnection.Close()
		}
		if connectionState == webrtc.ICEConnectionStateClosed {
			if session == currentSession {
				currentSession = nil
			}
			if session.shouldUmountVirtualMedia {
				err := rpcUnmountImage()
				logger.Debugf("unmount image failed on connection close %v", err)
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

var actionSessions = 0

func onActiveSessionsChanged() {
	requestDisplayUpdate()
}

func onFirstSessionConnected() {
	_ = writeCtrlAction("start_video")
}

func onLastSessionDisconnected() {
	_ = writeCtrlAction("stop_video")
}
