package kvm

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jetkvm/kvm/internal/logging"
	"github.com/pion/webrtc/v4"
)

type Session struct {
	PeerConnection           *webrtc.PeerConnection
	VideoTrack               *webrtc.TrackLocalStaticSample
	ControlChannel           *webrtc.DataChannel
	RPCChannel               *webrtc.DataChannel
	HidChannel               *webrtc.DataChannel
	DiskChannel              *webrtc.DataChannel
	shouldUmountVirtualMedia bool
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
	if err = s.PeerConnection.SetRemoteDescription(offer); err != nil {
		return "", err
	}

	// Create answer
	answer, err := s.PeerConnection.CreateAnswer(nil)
	if err != nil {
		return "", err
	}

	// Create channel that is blocked until ICE Gathering is complete
	gatherComplete := webrtc.GatheringCompletePromise(s.PeerConnection)

	// Sets the LocalDescription, and starts our UDP listeners
	if err = s.PeerConnection.SetLocalDescription(answer); err != nil {
		return "", err
	}

	// Block until ICE Gathering is complete, disabling trickle ICE
	// we do this because we only can exchange one signaling message
	// in a production application you should exchange ICE Candidates via OnICECandidate
	<-gatherComplete

	localDescription, err := json.Marshal(s.PeerConnection.LocalDescription())
	if err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(localDescription), nil
}

func NewSession() (*Session, error) {
	peerConnection, err := webrtc.NewPeerConnection(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{{}},
	})
	if err != nil {
		return nil, err
	}
	session := &Session{PeerConnection: peerConnection}

	peerConnection.OnDataChannel(func(d *webrtc.DataChannel) {
		fmt.Printf("New DataChannel %s %d\n", d.Label(), d.ID())
		switch d.Label() {
		case "rpc":
			session.RPCChannel = d
			d.OnMessage(func(msg webrtc.DataChannelMessage) {
				go OnRPCMessage(msg, session)
			})
			TriggerOTAStateUpdate()
			TriggerVideoStateUpdate()
			TriggerUSBStateUpdate()
		case "disk":
			session.DiskChannel = d
			d.OnMessage(OnDiskMessage)
		case "terminal":
			HandleTerminalChannel(d)
		default:
			if strings.HasPrefix(d.Label(), UploadIdPrefix) {
				go HandleUploadChannel(d)
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

	peerConnection.OnICEConnectionStateChange(func(connectionState webrtc.ICEConnectionState) {
		fmt.Printf("Connection State has changed %s \n", connectionState.String())
		if connectionState == webrtc.ICEConnectionStateConnected {
			if !isConnected {
				isConnected = true
				ActionSessions++
				onActiveSessionsChanged()
				if ActionSessions == 1 {
					onFirstSessionConnected()
				}
			}
		}
		//state changes on closing browser tab disconnected->failed, we need to manually close it
		if connectionState == webrtc.ICEConnectionStateFailed {
			_ = peerConnection.Close()
		}
		if connectionState == webrtc.ICEConnectionStateClosed {
			if session == CurrentSession {
				CurrentSession = nil
			}
			if session.shouldUmountVirtualMedia {
				err := RPCUnmountImage()
				logging.Logger.Debugf("unmount image failed on connection close %v", err)
			}
			if isConnected {
				isConnected = false
				ActionSessions--
				onActiveSessionsChanged()
				if ActionSessions == 0 {
					onLastSessionDisconnected()
				}
			}
		}
	})
	return session, nil
}

var ActionSessions = 0

func onActiveSessionsChanged() {
	RequestDisplayUpdate()
}

func onFirstSessionConnected() {
	_ = writeCtrlAction("start_video")
}

func onLastSessionDisconnected() {
	_ = writeCtrlAction("stop_video")
}
