package kvm

import (
	"fmt"
	"time"

	"github.com/bluenviron/gortsplib/v5"
	"github.com/bluenviron/gortsplib/v5/pkg/base"
	"github.com/bluenviron/gortsplib/v5/pkg/description"
	"github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/bluenviron/gortsplib/v5/pkg/format/rtph264"
	"github.com/jetkvm/kvm/internal/sync"
)

var rtspServer *RTSPServer

// RTSPServer wraps gortsplib.Server to serve the H.264 capture stream over RTSP.
type RTSPServer struct {
	server *gortsplib.Server
	stream *gortsplib.ServerStream

	media      *description.Media
	h264Format *format.H264
	encoder    *rtph264.Encoder

	// SPS/PPS cache — updated from live NALUs, included in SDP for late-joiners
	mu  sync.RWMutex
	sps []byte
	pps []byte

	// cumulative PTS for RTP timestamps
	pts time.Duration
}

// StartRTSPServer creates and starts the RTSP server on the given port.
func StartRTSPServer(port int) error {
	h264Format := &format.H264{
		PayloadTyp:        96,
		PacketizationMode: 1,
	}

	media := &description.Media{
		Type:    description.MediaTypeVideo,
		Formats: []format.Format{h264Format},
	}

	desc := &description.Session{
		Medias: []*description.Media{media},
	}

	rs := &RTSPServer{
		h264Format: h264Format,
		media:      media,
	}

	rs.server = &gortsplib.Server{
		Handler:     rs,
		RTSPAddress: fmt.Sprintf(":%d", port),
	}

	if err := rs.server.Start(); err != nil {
		return fmt.Errorf("failed to start RTSP server: %w", err)
	}

	rs.stream = &gortsplib.ServerStream{
		Server: rs.server,
		Desc:   desc,
	}
	if err := rs.stream.Initialize(); err != nil {
		rs.server.Close()
		return fmt.Errorf("failed to initialize RTSP stream: %w", err)
	}

	rs.encoder = &rtph264.Encoder{
		PayloadType: 96,
	}
	if err := rs.encoder.Init(); err != nil {
		rs.stream.Close()
		rs.server.Close()
		return fmt.Errorf("failed to init H264 RTP encoder: %w", err)
	}

	rtspServer = rs
	rtspLogger.Info().Int("port", port).Msg("RTSP server started")
	return nil
}

// StopRTSPServer shuts down the RTSP server.
func StopRTSPServer() {
	if rtspServer == nil {
		return
	}
	rtspServer.stream.Close()
	rtspServer.server.Close()
	rtspServer = nil
	rtspLogger.Info().Msg("RTSP server stopped")
}

// WriteNALU receives a raw H.264 Annex B frame and fans it out to all RTSP clients.
func (rs *RTSPServer) WriteNALU(frame []byte, duration time.Duration) {
	rs.pts += duration

	nalus := splitAnnexB(frame)
	if len(nalus) == 0 {
		return
	}

	// Cache SPS/PPS and update the format so the SDP stays current for late-joiners.
	for _, nalu := range nalus {
		if len(nalu) == 0 {
			continue
		}
		naluType := nalu[0] & 0x1F
		switch naluType {
		case 7: // SPS
			rs.mu.Lock()
			rs.sps = make([]byte, len(nalu))
			copy(rs.sps, nalu)
			rs.h264Format.SafeSetParams(rs.sps, rs.pps)
			rs.mu.Unlock()
		case 8: // PPS
			rs.mu.Lock()
			rs.pps = make([]byte, len(nalu))
			copy(rs.pps, nalu)
			rs.h264Format.SafeSetParams(rs.sps, rs.pps)
			rs.mu.Unlock()
		}
	}

	// Encode NALUs into RTP packets and write to all connected RTSP sessions.
	pkts, err := rs.encoder.Encode(nalus)
	if err != nil {
		return
	}

	for _, pkt := range pkts {
		rs.stream.WritePacketRTP(rs.media, pkt) //nolint:errcheck
	}
}

// ---------------------------------------------------------------------------
// gortsplib ServerHandler interface implementations
// ---------------------------------------------------------------------------

func (rs *RTSPServer) OnConnOpen(ctx *gortsplib.ServerHandlerOnConnOpenCtx) {
	rtspLogger.Info().Str("addr", ctx.Conn.NetConn().RemoteAddr().String()).Msg("RTSP client connected")
}

func (rs *RTSPServer) OnConnClose(ctx *gortsplib.ServerHandlerOnConnCloseCtx) {
	rtspLogger.Info().Str("addr", ctx.Conn.NetConn().RemoteAddr().String()).Msg("RTSP client disconnected")
}

func (rs *RTSPServer) OnSessionOpen(ctx *gortsplib.ServerHandlerOnSessionOpenCtx) {
	rtspLogger.Debug().Msg("RTSP session opened")
}

func (rs *RTSPServer) OnSessionClose(ctx *gortsplib.ServerHandlerOnSessionCloseCtx) {
	rtspLogger.Debug().Msg("RTSP session closed")
}

func (rs *RTSPServer) OnDescribe(_ *gortsplib.ServerHandlerOnDescribeCtx) (*base.Response, *gortsplib.ServerStream, error) {
	return &base.Response{StatusCode: base.StatusOK}, rs.stream, nil
}

func (rs *RTSPServer) OnSetup(_ *gortsplib.ServerHandlerOnSetupCtx) (*base.Response, *gortsplib.ServerStream, error) {
	return &base.Response{StatusCode: base.StatusOK}, rs.stream, nil
}

func (rs *RTSPServer) OnPlay(_ *gortsplib.ServerHandlerOnPlayCtx) (*base.Response, error) {
	return &base.Response{StatusCode: base.StatusOK}, nil
}

// ---------------------------------------------------------------------------
// Annex B parser — splits an Annex B byte stream into individual NAL units
// ---------------------------------------------------------------------------

func splitAnnexB(frame []byte) [][]byte {
	var nalus [][]byte
	start := -1
	n := len(frame)

	for i := 0; i <= n-3; i++ {
		if frame[i] == 0 && frame[i+1] == 0 {
			// 4-byte start code 00 00 00 01
			if i <= n-4 && frame[i+2] == 0 && frame[i+3] == 1 {
				if start >= 0 {
					nalus = append(nalus, frame[start:i])
				}
				start = i + 4
				i += 3
				continue
			}
			// 3-byte start code 00 00 01
			if frame[i+2] == 1 {
				if start >= 0 {
					nalus = append(nalus, frame[start:i])
				}
				start = i + 3
				i += 2
				continue
			}
		}
	}

	// Trailing NALU
	if start >= 0 && start < n {
		nalus = append(nalus, frame[start:n])
	}

	// If no start codes were found, treat the entire frame as a single raw NALU.
	if len(nalus) == 0 && n > 0 {
		return [][]byte{frame}
	}

	return nalus
}

// ---------------------------------------------------------------------------
// JSON-RPC methods for RTSP configuration
// ---------------------------------------------------------------------------

type RTSPConfig struct {
	Enabled bool `json:"enabled"`
	Port    int  `json:"port"`
}

func rpcGetRTSPConfig() RTSPConfig {
	return RTSPConfig{
		Enabled: config.RTSPEnabled,
		Port:    config.RTSPPort,
	}
}

func rpcSetRTSPConfig(enabled bool, port int) error {
	if port <= 0 || port > 65535 {
		port = 8554
	}

	wasEnabled := config.RTSPEnabled
	wasPort := config.RTSPPort

	config.RTSPEnabled = enabled
	config.RTSPPort = port
	if err := SaveConfig(); err != nil {
		// Rollback
		config.RTSPEnabled = wasEnabled
		config.RTSPPort = wasPort
		return fmt.Errorf("failed to save config: %w", err)
	}

	// Apply changes live
	if wasEnabled && !enabled {
		StopRTSPServer()
	} else if !wasEnabled && enabled {
		if err := StartRTSPServer(port); err != nil {
			rtspLogger.Error().Err(err).Msg("failed to start RTSP server after config change")
			return err
		}
	} else if enabled && wasPort != port {
		StopRTSPServer()
		if err := StartRTSPServer(port); err != nil {
			rtspLogger.Error().Err(err).Msg("failed to restart RTSP server on new port")
			return err
		}
	}

	return nil
}
