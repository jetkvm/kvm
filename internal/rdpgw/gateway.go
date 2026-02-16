package rdpgw

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
)

// Gateway handles MS-TSGU connections and proxies RDP traffic to the local server.
type Gateway struct {
	Logger        *zerolog.Logger
	RDPServerAddr string // e.g. "127.0.0.1:3389"
	UDPPort       uint16 // advertised UDP port for ShortPath (0 = disabled)
	CheckAuth     func(cookie string) bool

	// HandleRDP, when set, receives a net.Conn wrapping the MS-TSGU transport
	// so the RDP server reads/writes directly through the gateway (no TCP dial,
	// no inner TLS). If nil, falls back to TCP dial + forwardData.
	HandleRDP func(conn net.Conn)

	tunnelIDSeq  atomic.Uint32
	channelIDSeq atomic.Uint32
}

// HandleConnection processes a single MS-TSGU session over the given transport.
// This implements the full state machine: handshake → tunnel → auth → channel → data.
func (g *Gateway) HandleConnection(ctx context.Context, t Transport, remoteAddr string) error {
	log := g.Logger.With().Str("remote", remoteAddr).Logger()
	log.Info().Msg("RD Gateway connection started")

	defer func() {
		t.Close()
		log.Info().Msg("RD Gateway connection ended")
	}()

	state := stateInitialized

	var (
		tunnelID      uint32
		channelID     uint32
		paaNegotiated bool // whether PAA cookie auth was negotiated in handshake
	)

	for state != stateClosed {
		pktType, payload, err := t.ReadPacket()
		if err != nil {
			if state == stateOpened {
				// Normal disconnect during data forwarding
				log.Debug().Err(err).Msg("read error during data phase")
			} else {
				log.Warn().Err(err).Int("state", int(state)).Msg("read error")
			}
			return err
		}

		// Hex dump every packet during handshake phase for protocol analysis
		if state != stateOpened {
			preview := payload
			if len(preview) > 128 {
				preview = preview[:128]
			}
			log.Debug().
				Uint16("pktType", pktType).
				Int("payloadLen", len(payload)).
				Int("state", int(state)).
				Str("hex", hex.EncodeToString(preview)).
				Msg("recv packet")
		}

		switch pktType {
		case pktTypeHandshakeRequest:
			if state != stateInitialized {
				return fmt.Errorf("unexpected handshake request in state %d", state)
			}

			extAuth, err := parseHandshakeRequest(payload)
			if err != nil {
				return fmt.Errorf("parse handshake: %w", err)
			}

			log.Debug().Uint16("extAuth", extAuth).Msg("handshake request")

			// Echo back the auth capabilities we support (PAA only)
			negotiatedAuth := extAuth & httpExtendedAuthPAA
			paaNegotiated = negotiatedAuth&httpExtendedAuthPAA != 0
			resp := buildHandshakeResponse(negotiatedAuth)
			if err := t.WritePacket(pktTypeHandshakeResponse, resp); err != nil {
				return fmt.Errorf("send handshake response: %w", err)
			}
			log.Debug().Bool("paaNegotiated", paaNegotiated).Msg("handshake response sent")
			state = stateHandshake

		case pktTypeTunnelCreate:
			if state != stateHandshake {
				return fmt.Errorf("unexpected tunnel create in state %d", state)
			}

			caps, cookie, err := parseTunnelCreate(payload)
			if err != nil {
				return fmt.Errorf("parse tunnel create: %w", err)
			}

			log.Debug().
				Uint32("caps", caps).
				Int("cookieLen", len(cookie)).
				Bool("paaNegotiated", paaNegotiated).
				Str("cookiePreview", truncateStr(cookie, 8)).
				Msg("tunnel create")

			// Validate PAA cookie (only when PAA was negotiated in handshake).
			// When PAA is not negotiated (e.g. Windows App on macOS sends extAuth=0),
			// skip gateway-level auth — the RDP server's NLA/CredSSP will authenticate.
			if paaNegotiated && g.CheckAuth != nil && !g.CheckAuth(cookie) {
				log.Warn().Msg("PAA cookie authentication failed")
				// Send error response
				errBuf := make([]byte, 10)
				binary.LittleEndian.PutUint32(errBuf[2:6], errorCodeDenied)
				_ = t.WritePacket(pktTypeTunnelResponse, errBuf)
				return fmt.Errorf("authentication failed")
			}
			if !paaNegotiated {
				log.Info().Msg("PAA not negotiated — skipping gateway auth (RDP NLA will authenticate)")
			}

			tunnelID = g.tunnelIDSeq.Add(1)

			// Advertise UDP transport capability if we have a UDP port
			serverCaps := caps
			if g.UDPPort > 0 {
				serverCaps |= httpCapabilityUDPTransport
			}

			resp := buildTunnelResponse(tunnelID, serverCaps)
			if err := t.WritePacket(pktTypeTunnelResponse, resp); err != nil {
				return fmt.Errorf("send tunnel response: %w", err)
			}
			state = stateTunnel

		case pktTypeTunnelAuth:
			if state != stateTunnel {
				return fmt.Errorf("unexpected tunnel auth in state %d", state)
			}

			clientName, err := parseTunnelAuth(payload)
			if err != nil {
				return fmt.Errorf("parse tunnel auth: %w", err)
			}

			log.Debug().Str("clientName", clientName).Msg("tunnel auth")

			resp := buildTunnelAuthResponse(httpTunnelAuthRedirectFlagsDisable, 0)
			if err := t.WritePacket(pktTypeTunnelAuthResponse, resp); err != nil {
				return fmt.Errorf("send tunnel auth response: %w", err)
			}
			state = stateAuthorized

		case pktTypeChannelCreate:
			if state != stateAuthorized {
				return fmt.Errorf("unexpected channel create in state %d", state)
			}

			server, port, err := parseChannelCreate(payload)
			if err != nil {
				return fmt.Errorf("parse channel create: %w", err)
			}

			log.Info().
				Str("server", server).
				Uint16("port", port).
				Uint32("tunnelID", tunnelID).
				Msg("channel create — connecting to RDP server")

			channelID = g.channelIDSeq.Add(1)

			// Direct pipe: wrap transport as net.Conn and hand to RDP server.
			// Eliminates TCP loopback, inner TLS, and 2 forwarding goroutines.
			if g.HandleRDP != nil {
				resp := buildChannelResponse(channelID, g.UDPPort)
				if err := t.WritePacket(pktTypeChannelResponse, resp); err != nil {
					return fmt.Errorf("send channel response: %w", err)
				}

				log.Info().
					Uint32("channelID", channelID).
					Msg("channel opened — direct pipe to RDP server")

				conn := newTSGUConn(t)
				g.HandleRDP(conn)
				return nil
			}

			// Fallback: TCP dial + bidirectional forwarding
			dialCtx, dialCancel := context.WithTimeout(ctx, 5*time.Second)
			rdpConn, err := (&net.Dialer{}).DialContext(dialCtx, "tcp", g.RDPServerAddr)
			dialCancel()
			if err != nil {
				log.Error().Err(err).Str("addr", g.RDPServerAddr).Msg("failed to connect to RDP server")
				errBuf := make([]byte, 8)
				binary.LittleEndian.PutUint32(errBuf[0:4], errorCodeDenied)
				_ = t.WritePacket(pktTypeChannelResponse, errBuf)
				return fmt.Errorf("dial RDP server: %w", err)
			}

			resp := buildChannelResponse(channelID, g.UDPPort)
			if err := t.WritePacket(pktTypeChannelResponse, resp); err != nil {
				rdpConn.Close()
				return fmt.Errorf("send channel response: %w", err)
			}

			log.Info().
				Uint32("channelID", channelID).
				Uint16("udpPort", g.UDPPort).
				Msg("channel opened — starting data forwarding")

			return g.forwardData(ctx, t, rdpConn, &log)

		case pktTypeData:
			if state != stateOpened {
				return fmt.Errorf("unexpected data in state %d", state)
			}
			// This shouldn't happen — forwardData handles the data phase

		case pktTypeKeepAlive:
			log.Trace().Msg("keepalive")
			// Respond with keepalive
			if err := t.WritePacket(pktTypeKeepAlive, nil); err != nil {
				return fmt.Errorf("send keepalive: %w", err)
			}

		case pktTypeCloseChannel:
			log.Info().Msg("close channel request")
			resp := buildCloseChannelResponse()
			_ = t.WritePacket(pktTypeCloseChannelResponse, resp)
			state = stateClosed

		default:
			log.Warn().Uint16("pktType", pktType).Int("payloadLen", len(payload)).Msg("unknown packet type")
		}
	}

	return nil
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// forwardData performs bidirectional forwarding between the gateway transport and RDP server.
// Client → Server: extract DATA payload (2-byte length prefix + RDP data)
// Server → Client: wrap in PKT_TYPE_DATA (2-byte length prefix)
func (g *Gateway) forwardData(ctx context.Context, t Transport, rdpConn net.Conn, log *zerolog.Logger) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer rdpConn.Close()

	var wg sync.WaitGroup
	var firstErr atomic.Pointer[error]

	setErr := func(err error) {
		firstErr.CompareAndSwap(nil, &err)
		cancel()
	}

	// Server → Client: read from RDP server, wrap in DATA packets, send to client.
	// Read directly into an offset buffer so the DATA payload (2-byte length prefix + data)
	// is built in-place without a separate allocation or copy.
	wg.Add(1)
	go func() {
		defer wg.Done()
		// Max RDP data per read: 16384 (client HLW buffer) - 8 (MS-TSGU header) - 2 (len prefix) = 16374.
		// We allocate [2 + 16374] and read at offset 2 so the length prefix + data form the payload.
		const maxData = 16374
		buf := make([]byte, 2+maxData)
		for {
			n, err := rdpConn.Read(buf[2:])
			if err != nil {
				if err != io.EOF && ctx.Err() == nil {
					log.Debug().Err(err).Msg("RDP server read error")
				}
				setErr(err)
				return
			}

			binary.LittleEndian.PutUint16(buf[0:2], uint16(n))
			if err := t.WritePacket(pktTypeData, buf[:2+n]); err != nil {
				log.Debug().Err(err).Msg("client write error")
				setErr(err)
				return
			}
		}
	}()

	// Client → Server: read DATA packets from client, extract RDP data, write to server
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			pktType, payload, err := t.ReadPacket()
			if err != nil {
				if ctx.Err() == nil {
					log.Debug().Err(err).Msg("client read error")
				}
				setErr(err)
				return
			}

			switch pktType {
			case pktTypeData:
				if len(payload) < 2 {
					continue
				}
				dataLen := int(binary.LittleEndian.Uint16(payload[0:2]))
				if 2+dataLen > len(payload) {
					dataLen = len(payload) - 2
				}
				data := payload[2 : 2+dataLen]

				if _, err := rdpConn.Write(data); err != nil {
					log.Debug().Err(err).Msg("RDP server write error")
					setErr(err)
					return
				}

			case pktTypeKeepAlive:
				_ = t.WritePacket(pktTypeKeepAlive, nil)

			case pktTypeCloseChannel:
				log.Info().Msg("client closed channel")
				_ = t.WritePacket(pktTypeCloseChannelResponse, buildCloseChannelResponse())
				setErr(io.EOF)
				return

			default:
				log.Debug().Uint16("pktType", pktType).Msg("unexpected packet in data phase")
			}
		}
	}()

	wg.Wait()

	if ep := firstErr.Load(); ep != nil && *ep != io.EOF {
		return *ep
	}
	return nil
}
