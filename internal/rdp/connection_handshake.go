package rdp

import (
	"bufio"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"time"

	"github.com/jetkvm/kvm/internal/rdp/credssp"
	"github.com/jetkvm/kvm/internal/rdp/protocol"
)

// RDP connection handshake phase handlers.
// This file contains all functions related to the RDP connection establishment.

// handleX224Connection handles the X.224 connection request/confirm.
func (c *Connection) handleX224Connection() error {
	c.server.deps.Logger.Debug().Str("remote", c.RemoteAddr()).
		Msg("RDP: starting X.224 connection")

	// Set deadline for handshake
	if err := c.conn.SetReadDeadline(time.Now().Add(protocol.HandshakeTimeout)); err != nil {
		return err
	}

	// Read TPKT + X.224 Connection Request
	payload, err := protocol.ReadTPKTPayload(c.reader)
	if err != nil {
		return fmt.Errorf("read connection request: %w", err)
	}

	cr, err := protocol.ParseConnectionRequest(payload)
	if err != nil {
		return fmt.Errorf("parse connection request: %w", err)
	}

	logEvt := c.server.deps.Logger.Debug().
		Str("cookie", cr.Cookie)
	if cr.NegReq != nil {
		logEvt = logEvt.
			Str("requestedProto", fmt.Sprintf("0x%08x", cr.NegReq.RequestedProto)).
			Str("negReqFlags", fmt.Sprintf("0x%02x", cr.NegReq.Flags))
	}
	logEvt.
		Bool("clientRequestsTLS", cr.RequestsTLS()).
		Bool("clientRequestsCredSSP", cr.RequestsCredSSP()).
		Bool("serverTLSEnabled", c.server.tlsEnabled).
		Bool("serverTLSProviderSet", c.server.deps.TLS != nil).
		Msg("RDP: X.224 connection request received")

	// Build and send Connection Confirm
	// Select the best security protocol based on client capabilities and server config:
	// - CredSSP (NLA): Required when password is configured, provides authentication before session
	// - TLS: Only allowed when no password is configured (no authentication)
	// - RDP: Unencrypted fallback (not recommended)
	selectedProto := uint32(protocol.ProtocolRDP)
	if c.server.tlsEnabled && c.server.deps.TLS != nil {
		// Check if we have a password configured for NTLM authentication
		hasPassword := c.server.deps.Config.GetLocalAuthPassword() != ""

		if hasPassword {
			// When password is configured, require CredSSP for authentication
			if cr.RequestsCredSSP() {
				selectedProto = protocol.ProtocolCredSSP
				c.server.deps.Logger.Debug().Msg("RDP: selecting CredSSP (password configured)")
			} else {
				// Client doesn't support CredSSP but password is required - reject connection
				c.server.deps.Logger.Warn().Msg("RDP: rejecting connection - client doesn't support NLA but password authentication is required")
				return fmt.Errorf("client does not support NLA (CredSSP) but password authentication is required")
			}
		} else if cr.RequestsTLS() {
			// Prefer TLS when no password - uses hardware-accelerated crypto
			selectedProto = protocol.ProtocolTLS
			c.server.deps.Logger.Debug().Msg("RDP: selecting TLS (hardware accelerated, no password)")
		} else if cr.RequestsCredSSP() {
			// Fallback to CredSSP if client doesn't support plain TLS
			// Note: CredSSP uses software crypto (Go's crypto/tls)
			selectedProto = protocol.ProtocolCredSSP
			c.server.deps.Logger.Debug().Msg("RDP: selecting CredSSP (software crypto fallback)")
		}
	}

	// Build connection confirm - must include RDP_NEG_RSP if client sent RDP_NEG_REQ
	clientSentNegReq := cr.NegReq != nil
	// Set negotiation flags based on what we support
	// Per MS-RDPBCGR 2.2.1.2.1 (RDP Negotiation Response):
	// - EXTENDED_CLIENT_DATA_SUPPORTED (0x01): Server supports Extended Client Data Blocks
	// - DYNVC_GFX_PROTOCOL_SUPPORTED (0x02): Server supports Graphics Pipeline Extension
	// NOTE: RDP_NEG_REQ flags and RDP_NEG_RSP flags have DIFFERENT meanings!
	// We should NOT intersect them - just set what our server supports.
	negFlags := uint8(protocol.NegFlagExtendedClientDataSupported | protocol.NegFlagDynvcGfxProtocolSupported)
	cc := protocol.BuildConnectionConfirm(selectedProto, negFlags, clientSentNegReq)

	c.server.deps.Logger.Debug().
		Uint8("negFlags", negFlags).
		Uint32("selectedProto", selectedProto).
		Msg("RDP: X.224 negotiation flags")

	if err := protocol.WriteTPKT(c.conn, cc); err != nil {
		return fmt.Errorf("write connection confirm: %w", err)
	}

	// Store the selected protocol for later use in MCS Connect
	c.selectedProtocol = selectedProto
	// Store the client's original requested protocols for SC_CORE
	if cr.NegReq != nil {
		c.clientRequestedProtocols = cr.NegReq.RequestedProto
	}

	c.server.deps.Logger.Debug().
		Uint32("selectedProtocol", selectedProto).
		Bool("negRspIncluded", clientSentNegReq).
		Msg("RDP: X.224 connection confirm sent")

	// If TLS or CredSSP was negotiated, upgrade to TLS first
	if selectedProto == protocol.ProtocolTLS || selectedProto == protocol.ProtocolCredSSP {
		c.server.deps.Logger.Debug().Bool("softwareTLS", c.softwareTLS).
			Msg("RDP: upgrading connection to TLS")

		// Set deadline before TLS handshake
		if err := c.conn.SetDeadline(time.Now().Add(protocol.HandshakeTimeout)); err != nil {
			return fmt.Errorf("set TLS deadline: %w", err)
		}

		// Extract the raw conn from under captureConn (if capturing).
		// TLS must upgrade the raw transport, not the capture wrapper.
		actualConn := c.conn
		if c.capture != nil {
			actualConn = c.capture.Inner()
		}

		var tlsConn TLSConn
		if c.softwareTLS {
			// Gateway connections use Go's software crypto/tls because the
			// in-process tsguConn has no kernel socket fd for OpenSSL's SSL_set_fd().
			tc, err := c.softwareTLSUpgrade(actualConn)
			if err != nil {
				return fmt.Errorf("TLS handshake failed: %w", err)
			}
			tlsConn = tc
		} else {
			// Direct TCP connections use hardware-accelerated OpenSSL via SSL_set_fd().
			tc, err := c.server.deps.TLS.UpgradeServerConn(actualConn)
			if err != nil {
				return fmt.Errorf("TLS handshake failed: %w", err)
			}
			tlsConn = tc
		}

		c.server.deps.Logger.Debug().
			Str("version", tlsConn.GetProtocolVersion()).
			Str("cipher", tlsConn.GetCipherName()).
			Bool("hwAccel", tlsConn.IsHardwareAccelerated()).
			Msg("RDP: TLS handshake complete")

		if selectedProto == protocol.ProtocolCredSSP {
			c.server.deps.Logger.Debug().Msg("RDP: starting CredSSP/NLA authentication")

			// CredSSP accepts any TLSConn (credssp.TLSConn interface), not tied to *tls.Conn
			handler := credssp.NewHandler(tlsConn)
			handler.SetLogger(c.server.deps.Logger)

			// Set password for NTLM validation if configured
			password := c.server.deps.Config.GetLocalAuthPassword()
			if password != "" {
				handler.SetPassword(password)
				c.server.deps.Logger.Debug().Msg("RDP: CredSSP password validation enabled")
			} else {
				c.server.deps.Logger.Debug().Msg("RDP: CredSSP in permissive mode (no password)")
			}

			// Set expected username if configured
			expectedUsername := c.server.deps.Config.GetRDPUsername()
			if expectedUsername != "" {
				handler.SetExpectedUsername(expectedUsername)
				c.server.deps.Logger.Debug().Str("username", expectedUsername).Msg("RDP: CredSSP username validation enabled")
			}

			// Set expected domain if configured
			expectedDomain := c.server.deps.Config.GetRDPDomain()
			if expectedDomain != "" {
				handler.SetExpectedDomain(expectedDomain)
				c.server.deps.Logger.Debug().Str("domain", expectedDomain).Msg("RDP: CredSSP domain validation enabled")
			}

			// Set server's public key for pubKeyAuth computation
			// This is provided externally so CredSSP doesn't need TLS-specific APIs
			if c.server.deps.TLS != nil {
				// Get the local address from the TLS connection to match the certificate
				// that was used during TLS handshake (SelfSigner uses LocalAddr when SNI is empty)
				localHost := ""
				if localAddr := tlsConn.LocalAddr(); localAddr != nil {
					if host, _, err := net.SplitHostPort(localAddr.String()); err == nil {
						localHost = host
					}
				}
				c.server.deps.Logger.Debug().Str("localHost", localHost).Msg("RDP: looking up certificate for CredSSP")
				serverCert := c.server.deps.TLS.GetServerCertificate(localHost)
				if serverCert != nil && len(serverCert.Certificate) > 0 {
					// Parse the certificate to get the public key info
					if parsedCert, parseErr := x509.ParseCertificate(serverCert.Certificate[0]); parseErr == nil {
						handler.SetServerPublicKey(parsedCert.RawSubjectPublicKeyInfo)
						handler.SetServerCertificateDER(serverCert.Certificate[0])
						c.server.deps.Logger.Debug().
							Str("localHost", localHost).
							Int("pubKeyLen", len(parsedCert.RawSubjectPublicKeyInfo)).
							Int("certDERLen", len(serverCert.Certificate[0])).
							Str("certSerial", parsedCert.SerialNumber.String()).
							Msg("RDP: set server public key for CredSSP")
					} else {
						c.server.deps.Logger.Warn().Err(parseErr).Msg("RDP: failed to parse server certificate")
					}
				} else {
					c.server.deps.Logger.Warn().
						Str("localHost", localHost).
						Bool("certNil", serverCert == nil).
						Msg("RDP: failed to get server certificate for CredSSP pubKeyAuth")
				}
			}

			c.server.deps.Logger.Debug().Msg("RDP: starting CredSSP Authenticate()")
			username, err := handler.Authenticate()
			if err != nil {
				c.server.deps.Logger.Debug().Err(err).Msg("RDP: CredSSP authentication failed")
				return fmt.Errorf("CredSSP authentication failed: %w", err)
			}

			c.server.deps.Logger.Info().
				Str("username", username).
				Bool("hwAccel", tlsConn.IsHardwareAccelerated()).
				Msg("RDP: CredSSP/NLA authentication complete")
		}

		// Replace connection and reader with TLS versions.
		// If capturing, swap the inner conn so reads/writes through captureConn
		// are now decrypted TLS data.
		if c.capture != nil {
			c.conn = c.capture.SwapInner(tlsConn)
		} else {
			c.conn = tlsConn
		}
		c.reader = bufio.NewReader(c.conn)
		c.writer = bufio.NewWriterSize(c.conn, writeBufferSize)
	}

	// Clear both read and write deadlines that were set during TLS handshake.
	// IMPORTANT: SetDeadline clears both, while SetReadDeadline only clears read.
	// If we only clear read deadline, writes will start failing after HandshakeTimeout (30s).
	if err := c.conn.SetDeadline(time.Time{}); err != nil {
		return err
	}

	// Initialize buffered writer if not already set (non-TLS path)
	if c.writer == nil {
		c.writer = bufio.NewWriterSize(c.conn, writeBufferSize)
	}

	c.phase = PhaseBasicSettings
	return nil
}

// handleMCSConnect handles the MCS Connect-Initial/Response exchange.
func (c *Connection) handleMCSConnect() error {
	c.server.deps.Logger.Debug().Str("remote", c.RemoteAddr()).
		Msg("RDP: starting MCS connect")

	// Set deadline
	if err := c.conn.SetReadDeadline(time.Now().Add(protocol.NegotiationTimeout)); err != nil {
		return err
	}

	// Read MCS Connect-Initial (wrapped in X.224 Data)
	mcsData, err := protocol.ReadX224Data(c.reader)
	if err != nil {
		return fmt.Errorf("read connect-initial: %w", err)
	}

	ci, err := protocol.ParseConnectInitial(mcsData)
	if err != nil {
		return fmt.Errorf("parse connect-initial: %w", err)
	}
	c.server.deps.Logger.Debug().
		Int("userDataLen", len(ci.UserData)).
		Msg("RDP: MCS Connect-Initial parsed")

	// Parse GCC user data
	ccr, err := protocol.ParseConferenceCreateRequest(ci.UserData)
	if err != nil {
		c.server.deps.Logger.Debug().Err(err).Msg("RDP: failed to parse GCC data, continuing")
	}

	// Extract client info
	if ccr != nil && ccr.CoreData != nil {
		c.clientInfo = &ClientInfo{
			Name:           ccr.CoreData.GetClientName(),
			Version:        ccr.CoreData.RDPVersion(),
			Width:          ccr.CoreData.DesktopWidth,
			Height:         ccr.CoreData.DesktopHeight,
			ColorDepth:     ccr.CoreData.HighColorDepth,
			KeyboardLayout: ccr.CoreData.KeyboardLayout,
			KeyboardType:   ccr.CoreData.KeyboardType,
		}

		c.server.deps.Logger.Info().
			Str("client", c.clientInfo.Name).
			Str("version", c.clientInfo.Version).
			Uint16("width", c.clientInfo.Width).
			Uint16("height", c.clientInfo.Height).
			Msg("RDP: client connected")

		// Update capture metadata with client name
		if c.capture != nil {
			c.capture.SetClientName(c.clientInfo.Name)
		}

		c.server.deps.Logger.Debug().
			Uint32("clientBuild", ccr.CoreData.ClientBuild).
			Uint16("colorDepth", ccr.CoreData.HighColorDepth).
			Str("supportedColorDepths", fmt.Sprintf("0x%04x", ccr.CoreData.SupportedColorDepths)).
			Str("earlyCapFlags", fmt.Sprintf("0x%04x", ccr.CoreData.EarlyCapabilityFlags)).
			Uint8("connectionType", ccr.CoreData.ConnectionType).
			Str("keyboardLayout", fmt.Sprintf("0x%08x", ccr.CoreData.KeyboardLayout)).
			Uint32("keyboardType", ccr.CoreData.KeyboardType).
			Uint32("scaleFactor", ccr.CoreData.DesktopScaleFactor).
			Uint32("deviceScale", ccr.CoreData.DeviceScaleFactor).
			Str("physicalSize", fmt.Sprintf("%dx%dmm", ccr.CoreData.DesktopPhysicalWidth, ccr.CoreData.DesktopPhysicalHeight)).
			Msg("RDP: CS_CORE details")
	}

	if ccr != nil && ccr.SecurityData != nil {
		c.server.deps.Logger.Debug().
			Str("encryptionMethods", fmt.Sprintf("0x%08x", ccr.SecurityData.EncryptionMethods)).
			Str("extEncryptMethods", fmt.Sprintf("0x%08x", ccr.SecurityData.ExtEncryptMethods)).
			Msg("RDP: CS_SECURITY")
	}

	// Extract channel definitions
	if ccr != nil && ccr.NetworkData != nil {
		// Only include channels with non-empty names (per xrdp fix for Jump Desktop)
		// Some clients send empty channel names for disabled channels
		c.channels = make([]ChannelInfo, 0, len(ccr.NetworkData.Channels))
		for _, ch := range ccr.NetworkData.Channels {
			name := ch.Name.String()
			if name != "" {
				c.channels = append(c.channels, ChannelInfo{
					Name:    name,
					Options: ch.Options,
				})
			}
		}

		// Log filtered channel info with options bitmask
		channelDescs := make([]string, len(c.channels))
		for i, ch := range c.channels {
			channelDescs[i] = fmt.Sprintf("%s(0x%08x)", ch.Name, ch.Options)
		}
		c.server.deps.Logger.Debug().
			Int("channelCount", len(c.channels)).
			Strs("channels", channelDescs).
			Msg("RDP: client virtual channels")
	}

	// Build MCS Connect-Response
	// Use RDP 5.0 version for maximum compatibility with older clients like Jump Desktop
	// Jump Desktop is RDP 5.0 and may not handle newer version numbers correctly
	// Per MS-RDPBCGR 2.2.1.4.2, clientRequestedProtocols should be set to
	// the value from the client's CS_CORE. In practice, Windows Desktop seems to
	// expect it to match the original X.224 requested protocols.
	clientReqProto := c.clientRequestedProtocols
	if clientReqProto == 0 {
		clientReqProto = c.selectedProtocol // Fallback
	}
	serverCore := &protocol.ServerCoreData{
		Version:              protocol.RDPVersion104, // RDP 10.4 - needed for modern codecs
		ClientRequestedProto: clientReqProto,
		EarlyCapFlags:        protocol.EarlyCapDynamicDst, // Note: Do NOT set EarlyCapSkipChannelJoin - server expects channel join sequence
	}

	// Assign channel IDs to filtered channels only
	// Note: MS-RDPBCGR says channelCount SHOULD match client, but xrdp only returns
	// IDs for valid channels (per PR #615 Jump Desktop fix)
	baseChannelID := uint16(protocol.ChannelMCSGlobalID + 1) // 1004
	channelIDs := make([]uint16, len(c.channels))
	for i := range c.channels {
		c.channels[i].ID = baseChannelID + uint16(i)
		channelIDs[i] = c.channels[i].ID
		// Populate fast lookup array (lock-free hot path)
		if i < len(c.channelNames) {
			c.channelNames[i] = c.channels[i].Name
		}
	}

	// Determine message channel ID if client requested it
	var msgChannelID uint16
	if ccr != nil && ccr.MsgChannelData != nil {
		// Assign message channel ID after virtual channels
		msgChannelID = baseChannelID + uint16(len(c.channels))
		c.msgChannelID = msgChannelID
		c.server.deps.Logger.Debug().
			Uint32("clientFlags", ccr.MsgChannelData.Flags).
			Uint16("msgChannelID", msgChannelID).
			Msg("RDP: client requested message channel")
	}

	serverNetwork := &protocol.ServerNetworkData{
		MCSChannelID: protocol.ChannelMCSGlobalID,
		ChannelCount: uint16(len(c.channels)), // Only return valid channels
		ChannelIDs:   channelIDs,
		MsgChannelID: msgChannelID,
	}

	// No encryption for now (TLS handles security)
	serverSecurity := &protocol.ServerSecurityData{
		EncryptionMethod: 0,
		EncryptionLevel:  0,
	}

	// Parse client's multitransport capability (CS_MULTITRANSPORT)
	if ccr != nil && ccr.MultitransportData != nil {
		c.clientMultitransportFlags = ccr.MultitransportData.Flags
		c.server.deps.Logger.Debug().
			Str("flags", fmt.Sprintf("0x%08x", c.clientMultitransportFlags)).
			Bool("UDPFECR", c.clientMultitransportFlags&protocol.TransportTypeUDPFECR != 0).
			Bool("UDPFECL", c.clientMultitransportFlags&protocol.TransportTypeUDPFECL != 0).
			Bool("UDP_PREFERRED", c.clientMultitransportFlags&protocol.TransportUDPPreferred != 0).
			Bool("SOFT_SYNC", c.clientMultitransportFlags&protocol.TransportSoftSyncTCPUDP != 0).
			Msg("RDP: client multitransport flags")
	}

	// Send SC_MULTITRANSPORT when UDP transport is fully enabled AND the client
	// requested it. Advertising UDP capability when the client didn't request it
	// or without following through with the Initiate Multitransport Request causes
	// clients (e.g., Windows App) to defer static VC plugin initialization (rdpsnd,
	// cliprdr), breaking audio and clipboard.
	var multitransportData *protocol.ServerMultitransportData
	if c.server.udpEnabled && c.server.multitransportEnabled && c.clientMultitransportFlags != 0 {
		multitransportData = &protocol.ServerMultitransportData{
			Flags: protocol.TransportTypeUDPFECR | protocol.TransportUDPPreferred | protocol.TransportSoftSyncTCPUDP,
		}
		c.server.deps.Logger.Debug().
			Str("serverFlags", fmt.Sprintf("0x%08x", multitransportData.Flags)).
			Msg("RDP: SC_MULTITRANSPORT included=true reason=\"client requested UDP\"")
	} else {
		reason := "client flags=0 (no UDP support)"
		if c.clientMultitransportFlags != 0 && !c.server.udpEnabled {
			reason = "server UDP disabled"
		} else if c.clientMultitransportFlags != 0 && !c.server.multitransportEnabled {
			reason = "server multitransport disabled"
		}
		c.server.deps.Logger.Debug().
			Str("reason", reason).
			Msg("RDP: SC_MULTITRANSPORT included=false")
	}
	gccResponse := protocol.BuildConferenceCreateResponse(serverCore, serverNetwork, serverSecurity, multitransportData)
	domainParams := protocol.DefaultDomainParameters()

	// Build the full MCS Connect-Response
	mcsResponse := protocol.BuildConnectResponse(protocol.MCSResultSuccessful, 0, domainParams, gccResponse)

	if err := protocol.WriteX224Data(c.conn, mcsResponse); err != nil {
		return fmt.Errorf("write connect-response: %w", err)
	}

	c.server.deps.Logger.Debug().Msg("RDP: sent MCS Connect-Response")

	// Clear deadline
	if err := c.conn.SetReadDeadline(time.Time{}); err != nil {
		return err
	}

	c.phase = PhaseChannelConnection
	return nil
}

// handleMCSChannelSetup handles MCS Erect Domain, Attach User, and Channel Join.
func (c *Connection) handleMCSChannelSetup() error {
	c.server.deps.Logger.Debug().Str("remote", c.RemoteAddr()).
		Msg("RDP: starting MCS channel setup")

	// Set deadline
	if err := c.conn.SetReadDeadline(time.Now().Add(protocol.NegotiationTimeout)); err != nil {
		return err
	}

	// 1. Receive Erect Domain Request
	data, err := protocol.ReadX224Data(c.reader)
	if err != nil {
		return fmt.Errorf("read erect domain: %w", err)
	}

	pduType, err := protocol.ParseMCSPDUType(data)
	if err != nil {
		return err
	}
	if pduType != protocol.MCSErectDomainRequest {
		return fmt.Errorf("expected ErectDomainRequest, got %s", pduType)
	}

	c.server.deps.Logger.Debug().Msg("RDP: received erect domain request")

	// 2. Receive Attach User Request
	data, err = protocol.ReadX224Data(c.reader)
	if err != nil {
		return fmt.Errorf("read attach user: %w", err)
	}

	pduType, err = protocol.ParseMCSPDUType(data)
	if err != nil {
		return err
	}
	if pduType != protocol.MCSAttachUserRequest {
		return fmt.Errorf("expected AttachUserRequest, got %s", pduType)
	}

	// Assign user ID and send confirm
	c.userID = protocol.MCSUserIDBase
	confirm := protocol.BuildAttachUserConfirm(protocol.MCSResultSuccessful, c.userID)

	if err := protocol.WriteMCSPDU(c.conn, confirm); err != nil {
		return fmt.Errorf("write attach user confirm: %w", err)
	}

	c.server.deps.Logger.Debug().Uint16("userID", c.userID).
		Msg("RDP: attached user")

	// 3. Handle Channel Join Requests
	// Client will join: user channel, I/O channel, and virtual channels
	// Note: c.channels only contains channels with non-empty names (filtered earlier)
	expectedJoins := 2 + len(c.channels) // user + IO + virtual channels
	if c.msgChannelID != 0 {
		expectedJoins++ // message channel
	}

	for i := range expectedJoins {
		data, err = protocol.ReadX224Data(c.reader)
		if err != nil {
			return fmt.Errorf("read channel join %d: %w", i, err)
		}

		pduType, err = protocol.ParseMCSPDUType(data)
		if err != nil {
			return err
		}
		if pduType != protocol.MCSChannelJoinRequest {
			return fmt.Errorf("expected ChannelJoinRequest, got %s", pduType)
		}

		joinReq, err := protocol.ParseChannelJoinRequest(data)
		if err != nil {
			return err
		}

		// Track I/O channel
		if joinReq.ChannelID == protocol.ChannelMCSGlobalID {
			c.ioChannel = joinReq.ChannelID
		}

		// Send confirm
		confirm := protocol.BuildChannelJoinConfirm(
			protocol.MCSResultSuccessful,
			joinReq.UserID,
			joinReq.ChannelID,
		)
		if err := protocol.WriteMCSPDU(c.conn, confirm); err != nil {
			return fmt.Errorf("write channel join confirm: %w", err)
		}

		c.server.deps.Logger.Debug().
			Uint16("channelID", joinReq.ChannelID).
			Msg("RDP: confirmed channel join")
	}

	// Clear deadline
	if err := c.conn.SetReadDeadline(time.Time{}); err != nil {
		return err
	}

	c.phase = PhaseSecurityExchange
	return nil
}

// softwareTLSUpgrade performs a TLS handshake using Go's standard crypto/tls.
// Used for gateway connections where the underlying net.Conn is an in-process
// pipe (tsguConn) without a kernel socket fd, so OpenSSL's SSL_set_fd() cannot work.
// The conn parameter is the raw transport to upgrade (may differ from c.conn when
// packet capture is active, as c.conn is the captureConn wrapper).
func (c *Connection) softwareTLSUpgrade(conn net.Conn) (TLSConn, error) {
	goConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
		MaxVersion: tls.VersionTLS12, // CredSSP requires TLS 1.2
	}

	if c.server.deps.TLS != nil {
		goConfig.GetCertificate = func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			return c.server.deps.TLS.GetServerCertificate(hello.ServerName), nil
		}
	}

	tlsConn := tls.Server(conn, goConfig)
	if err := tlsConn.Handshake(); err != nil {
		return nil, fmt.Errorf("software TLS handshake failed: %w", err)
	}

	return &softwareTLSConn{Conn: tlsConn}, nil
}

// softwareTLSConn wraps Go's tls.Conn to implement TLSConn.
type softwareTLSConn struct {
	*tls.Conn
}

func (c *softwareTLSConn) GetCipherName() string {
	return tls.CipherSuiteName(c.ConnectionState().CipherSuite)
}

func (c *softwareTLSConn) GetProtocolVersion() string {
	switch c.ConnectionState().Version {
	case tls.VersionTLS12:
		return "TLSv1.2"
	case tls.VersionTLS13:
		return "TLSv1.3"
	default:
		return fmt.Sprintf("TLS 0x%04x", c.ConnectionState().Version)
	}
}

func (c *softwareTLSConn) IsHardwareAccelerated() bool {
	return false
}
