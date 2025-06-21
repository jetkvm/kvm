package native

import (
	"github.com/Masterminds/semver/v3"
	"github.com/rs/zerolog"
)

type Native struct {
	ready         chan struct{}
	l             *zerolog.Logger
	lD            *zerolog.Logger
	SystemVersion *semver.Version
	AppVersion    *semver.Version
}

func NewNative(systemVersion *semver.Version, appVersion *semver.Version) *Native {
	return &Native{
		ready:         make(chan struct{}),
		l:             nativeLogger,
		lD:            displayLogger,
		SystemVersion: systemVersion,
		AppVersion:    appVersion,
	}
}

func (n *Native) Start() {
	go n.StartNativeVideo()
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
