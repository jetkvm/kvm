package native

import (
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/rs/zerolog"
)

type Native struct {
	ready                chan struct{}
	l                    *zerolog.Logger
	lD                   *zerolog.Logger
	systemVersion        *semver.Version
	appVersion           *semver.Version
	onVideoStateChange   func(state VideoState)
	onVideoFrameReceived func(frame []byte, duration time.Duration)
}

type NativeOptions struct {
	SystemVersion        *semver.Version
	AppVersion           *semver.Version
	OnVideoStateChange   func(state VideoState)
	OnVideoFrameReceived func(frame []byte, duration time.Duration)
}

func NewNative(opts NativeOptions) *Native {
	onVideoStateChange := opts.OnVideoStateChange
	if onVideoStateChange == nil {
		onVideoStateChange = func(state VideoState) {
			nativeLogger.Info().Msg("video state changed")
		}
	}

	onVideoFrameReceived := opts.OnVideoFrameReceived
	if onVideoFrameReceived == nil {
		onVideoFrameReceived = func(frame []byte, duration time.Duration) {
			nativeLogger.Info().Msg("video frame received")
		}
	}

	return &Native{
		ready:                make(chan struct{}),
		l:                    nativeLogger,
		lD:                   displayLogger,
		systemVersion:        opts.SystemVersion,
		appVersion:           opts.AppVersion,
		onVideoStateChange:   opts.OnVideoStateChange,
		onVideoFrameReceived: opts.OnVideoFrameReceived,
	}
}

func (n *Native) Start() {
	go n.StartNativeVideo()
	go n.HandleVideoChan()
}

func (n *Native) HandleVideoChan() {
	lastFrame := time.Now()

	for {
		frame := <-jkVideoChan
		now := time.Now()
		sinceLastFrame := now.Sub(lastFrame)
		lastFrame = now
		n.onVideoFrameReceived(frame, sinceLastFrame)
	}
}

// func handleCtrlClient(conn net.Conn) {
// 	defer conn.Close()

// 	scopedLogger := nativeLogger.With().
// 		Str("addr", conn.RemoteAddr().String()).
// 		Str("type", "ctrl").
// 		Logger()

// 	scopedLogger.Info().Msg("native ctrl socket client connected")
// 	if ctrlSocketConn != nil {
// 		scopedLogger.Debug().Msg("closing existing native socket connection")
// 		ctrlSocketConn.Close()
// 	}

// 	ctrlSocketConn = conn

// 	// Restore HDMI EDID if applicable
// 	go restoreHdmiEdid()

// 	readBuf := make([]byte, 4096)
// 	for {
// 		n, err := conn.Read(readBuf)
// 		if err != nil {
// 			scopedLogger.Warn().Err(err).Msg("error reading from ctrl sock")
// 			break
// 		}
// 		readMsg := string(readBuf[:n])

// 		ctrlResp := CtrlResponse{}
// 		err = json.Unmarshal([]byte(readMsg), &ctrlResp)
// 		if err != nil {
// 			scopedLogger.Warn().Err(err).Str("data", readMsg).Msg("error parsing ctrl sock msg")
// 			continue
// 		}
// 		scopedLogger.Trace().Interface("data", ctrlResp).Msg("ctrl sock msg")

// 		if ctrlResp.Seq != 0 {
// 			responseChan, ok := ongoingRequests[ctrlResp.Seq]
// 			if ok {
// 				responseChan <- &ctrlResp
// 			}
// 		}
// 		switch ctrlResp.Event {
// 		case "video_input_state":
// 			HandleVideoStateMessage(ctrlResp)
// 		}
// 	}

// 	scopedLogger.Debug().Msg("ctrl sock disconnected")
// }

// func handleVideoClient(conn net.Conn) {
// 	defer conn.Close()

// 	scopedLogger := nativeLogger.With().
// 		Str("addr", conn.RemoteAddr().String()).
// 		Str("type", "video").
// 		Logger()

// 	scopedLogger.Info().Msg("native video socket client connected")

// 	inboundPacket := make([]byte, maxFrameSize)
// 	lastFrame := time.Now()
// 	for {
// 		n, err := conn.Read(inboundPacket)
// 		if err != nil {
// 			scopedLogger.Warn().Err(err).Msg("error during read")
// 			return
// 		}
// 		now := time.Now()
// 		sinceLastFrame := now.Sub(lastFrame)
// 		lastFrame = now
// 		if currentSession != nil {
// 			err := currentSession.VideoTrack.WriteSample(media.Sample{Data: inboundPacket[:n], Duration: sinceLastFrame})
// 			if err != nil {
// 				scopedLogger.Warn().Err(err).Msg("error writing sample")
// 			}
// 		}
// 	}
// }

// // Restore the HDMI EDID value from the config.
// // Called after successful connection to jetkvm_native.
// func restoreHdmiEdid() {
// 	if config.EdidString != "" {
// 		nativeLogger.Info().Str("edid", config.EdidString).Msg("Restoring HDMI EDID")
// 		_, err := CallCtrlAction("set_edid", map[string]interface{}{"edid": config.EdidString})
// 		if err != nil {
// 			nativeLogger.Warn().Err(err).Msg("Failed to restore HDMI EDID")
// 		}
// 	}
// }
