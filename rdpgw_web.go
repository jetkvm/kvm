package kvm

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/jetkvm/kvm/internal/rdpgw"
)

var (
	rdpGateway     *rdpgw.Gateway
	rdpGatewayOnce sync.Once
	rdpGatewayUDP  *rdpgw.UDPListener
)

func getRDPGateway() *rdpgw.Gateway {
	rdpGatewayOnce.Do(func() {
		rdpGateway = &rdpgw.Gateway{
			Logger:        rdpGatewayLogger,
			RDPServerAddr: fmt.Sprintf("127.0.0.1:%d", loadCfg().RDPPort),
			UDPPort:       getRDPGatewayUDPPort(),
			CheckAuth:     checkRDPGatewayAuth,
			HandleRDP: func(conn net.Conn) {
				GetRDPServer().HandleGatewayConnection(conn)
			},
		}
	})
	return rdpGateway
}

func getRDPGatewayUDPPort() uint16 {
	cfg := loadCfg()
	if cfg.RDPGatewayUDPPort > 0 {
		return uint16(cfg.RDPGatewayUDPPort)
	}
	return 3391 // default
}

// checkRDPGatewayAuth validates the PAA cookie sent during MS-TSGU tunnel creation.
// The cookie format is the plaintext password (same as LocalAuthPassword used for RDP CredSSP).
func checkRDPGatewayAuth(cookie string) bool {
	cfg := loadCfg()

	// noPassword mode: accept any cookie
	if cfg.LocalAuthMode == "noPassword" {
		return true
	}

	// Must have a password configured
	if cfg.LocalAuthPassword == "" {
		rdpGatewayLogger.Warn().Msg("gateway auth failed: no local auth password configured")
		return false
	}

	// The PAA cookie is the plaintext password
	if cookie != cfg.LocalAuthPassword {
		rdpGatewayLogger.Warn().Msg("gateway auth failed: password mismatch")
		return false
	}

	return true
}

// registerRDGatewayRoutes registers the MS-TSGU gateway endpoints as middleware.
// We use middleware instead of r.Any() because the MS-TSGU legacy transport uses
// custom HTTP methods (RDG_OUT_DATA, RDG_IN_DATA) that gin's router doesn't handle.
// Authentication happens inside MS-TSGU via PAA cookie (not gin auth middleware).
func registerRDGatewayRoutes(r *gin.Engine) {
	r.Use(func(c *gin.Context) {
		path := c.Request.URL.Path
		if path == "/remoteDesktopGateway/" || path == "/remoteDesktopGateway" {
			handleRDGateway(c)
			c.Abort()
			return
		}
		c.Next()
	})
}

func handleRDGateway(c *gin.Context) {
	if isWebSocketUpgrade(c.Request) {
		handleRDGatewayWebSocket(c)
	} else {
		handleRDGatewayLegacy(c)
	}
}

func isWebSocketUpgrade(r *http.Request) bool {
	// Per MS-TSGU spec and rdpgw reference implementation: the client sends
	// RDG_OUT_DATA with Connection: upgrade + Upgrade: websocket headers.
	// The method is NOT GET — it's a custom HTTP method. We check headers only,
	// then force the method to GET before calling websocket.Accept().
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket") &&
		strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade")
}

func handleRDGatewayWebSocket(c *gin.Context) {
	gw := getRDPGateway()

	// Per MS-TSGU: client sends RDG_OUT_DATA with upgrade headers.
	// Force method to GET so the WebSocket library accepts the upgrade.
	c.Request.Method = http.MethodGet

	ws, err := websocket.Accept(c.Writer, c.Request, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		rdpGatewayLogger.Warn().Err(err).Msg("gateway WebSocket accept failed")
		return
	}

	ctx := c.Request.Context()
	t := rdpgw.NewWSTransport(ctx, ws)
	defer t.Close()

	if err := gw.HandleConnection(ctx, t, c.ClientIP()); err != nil {
		rdpGatewayLogger.Debug().Err(err).Str("remote", c.ClientIP()).Msg("gateway connection ended")
	}
}

func handleRDGatewayLegacy(c *gin.Context) {
	// Legacy HTTP mode (mstsc.exe, Windows App fallback)
	// Two separate HTTP requests share an Rdg-Connection-Id header:
	// 1. RDG_OUT_DATA arrives first — server→client channel
	// 2. RDG_IN_DATA arrives second — client→server channel
	connID := c.GetHeader("Rdg-Connection-Id")
	if connID == "" {
		rdpGatewayLogger.Warn().Str("method", c.Request.Method).Msg("legacy request missing Rdg-Connection-Id")
		c.Status(http.StatusBadRequest)
		return
	}

	method := c.Request.Method

	if method == "RDG_OUT_DATA" || (method == "GET" && c.GetHeader("Rdg-Connection-Id") != "") {
		handleLegacyOut(c, connID)
		return
	}

	if method == "RDG_IN_DATA" || method == "POST" {
		handleLegacyIn(c, connID)
		return
	}

	rdpGatewayLogger.Warn().Str("method", method).Msg("unknown legacy gateway request method")
	c.Status(http.StatusMethodNotAllowed)
}

// handleLegacyOut handles the RDG_OUT_DATA request (server→client channel).
// Per MS-TSGU spec: the server responds with HTTP 200 plus 10 random seed bytes.
// The connection is then registered for pairing with the RDG_IN_DATA channel.
func handleLegacyOut(c *gin.Context, connID string) {
	conn, rw, err := rdpgw.HijackRaw(c.Writer)
	if err != nil {
		rdpGatewayLogger.Warn().Err(err).Msg("legacy OUT hijack failed")
		return
	}

	rdpGatewayLogger.Debug().Str("connID", connID).Msg("legacy OUT channel opened")

	// Send HTTP 200 with seed bytes (triggers proxy passthrough per MS-TSGU spec)
	if err := rdpgw.SendLegacyAccept(rw, true); err != nil {
		rdpGatewayLogger.Warn().Err(err).Msg("legacy OUT accept failed")
		conn.Close()
		return
	}

	// Register for pairing with the IN channel. The OUT handler returns
	// immediately — the IN handler takes ownership of this connection.
	rdpgw.RegisterLegacyOut(connID, conn)
}

// handleLegacyIn handles the RDG_IN_DATA request (client→server channel).
// The client sends MS-TSGU packets in the HTTP request body using chunked encoding.
func handleLegacyIn(c *gin.Context, connID string) {
	// Pair with the OUT connection
	pending := rdpgw.ClaimLegacyOut(connID)
	if pending == nil {
		rdpGatewayLogger.Warn().Str("connID", connID).Msg("legacy IN: no matching OUT connection")
		c.Status(http.StatusGatewayTimeout)
		return
	}

	conn, rw, err := rdpgw.HijackRaw(c.Writer)
	if err != nil {
		rdpGatewayLogger.Warn().Err(err).Msg("legacy IN hijack failed")
		pending.OutConn.Close()
		return
	}

	rdpGatewayLogger.Debug().Str("connID", connID).Msg("legacy IN channel opened — sending accept")

	// Send HTTP 200 with Content-Length: 0 (signals response is complete)
	if err := rdpgw.SendLegacyAccept(rw, false); err != nil {
		rdpGatewayLogger.Warn().Err(err).Msg("legacy IN accept failed")
		conn.Close()
		pending.OutConn.Close()
		return
	}

	// Drain initial data from the raw connection before starting protocol.
	// Per reference implementation, this reads and discards setup data.
	rdpgw.DrainLegacy(conn)

	rdpGatewayLogger.Debug().Str("connID", connID).Msg("legacy IN drained — starting protocol")

	// Create chunked reader for decoding the client's HTTP chunked request body.
	chunkedReader := rdpgw.NewChunkedReader(rw)

	gw := getRDPGateway()
	t := rdpgw.NewLegacyTransport(chunkedReader, conn, pending.OutConn)
	defer t.Close()

	ctx := context.Background()
	if err := gw.HandleConnection(ctx, t, c.ClientIP()); err != nil {
		rdpGatewayLogger.Debug().Err(err).Str("connID", connID).Msg("legacy gateway connection ended")
	}
}

// startRDPGatewayUDP starts the UDP listener for ShortPath discovery if configured.
func startRDPGatewayUDP() {
	cfg := loadCfg()
	port := cfg.RDPGatewayUDPPort
	if port <= 0 {
		port = 3391
	}

	rdpGatewayUDP = rdpgw.NewUDPListener(rdpGatewayLogger)

	// Determine bind address
	bindAddr := ""
	if cfg.LocalLoopbackOnly {
		bindAddr = "127.0.0.1"
	}

	if err := rdpGatewayUDP.Start(bindAddr, port); err != nil {
		rdpGatewayLogger.Error().Err(err).Int("port", port).Msg("failed to start ShortPath UDP listener")
	}
}
