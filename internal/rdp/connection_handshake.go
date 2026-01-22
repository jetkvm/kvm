package rdp

import (
	"bufio"
	"crypto/tls"
	"crypto/x509"
	"fmt"
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

	// Temporary WARN-level logging for debugging
	c.server.deps.Logger.Warn().
		Str("cookie", cr.Cookie).
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
		} else if cr.RequestsCredSSP() {
			// CredSSP without password - will work in permissive mode (may fail with some clients)
			selectedProto = protocol.ProtocolCredSSP
			c.server.deps.Logger.Debug().Msg("RDP: selecting CredSSP (permissive mode)")
		} else if cr.RequestsTLS() {
			// TLS without password - no authentication
			selectedProto = protocol.ProtocolTLS
			c.server.deps.Logger.Debug().Msg("RDP: selecting TLS (no password)")
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

	// Log the negotiation details
	c.server.deps.Logger.Warn().
		Uint8("negFlags", negFlags).
		Uint32("selectedProto", selectedProto).
		Msg("RDP: X.224 negotiation flags")

	// Log the exact bytes being sent for debugging
	c.server.deps.Logger.Warn().
		Int("ccLen", len(cc)).
		Str("ccHex", fmt.Sprintf("%x", cc)).
		Msg("RDP: X.224 Connection Confirm bytes")

	if err := protocol.WriteTPKT(c.conn, cc); err != nil {
		return fmt.Errorf("write connection confirm: %w", err)
	}

	// Store the selected protocol for later use in MCS Connect
	c.selectedProtocol = selectedProto
	// Store the client's original requested protocols for SC_CORE
	if cr.NegReq != nil {
		c.clientRequestedProtocols = cr.NegReq.RequestedProto
	}

	// Temporary WARN-level logging for debugging
	c.server.deps.Logger.Warn().
		Uint32("selectedProtocol", selectedProto).
		Bool("negRspIncluded", clientSentNegReq).
		Msg("RDP: X.224 connection confirm sent")

	// If TLS or CredSSP was negotiated, upgrade to TLS first
	if selectedProto == protocol.ProtocolTLS || selectedProto == protocol.ProtocolCredSSP {
		c.server.deps.Logger.Warn().Msg("RDP: upgrading connection to TLS")

		// Set deadline before TLS handshake
		if err := c.conn.SetDeadline(time.Now().Add(protocol.HandshakeTimeout)); err != nil {
			return fmt.Errorf("set TLS deadline: %w", err)
		}

		// CredSSP requires Go's *tls.Conn for TLS session binding in pubKeyAuth.
		// For plain TLS mode, we can use hardware-accelerated TLS if available.
		if selectedProto == protocol.ProtocolCredSSP {
			// CredSSP mode: use Go's crypto/tls (required for session binding)
			credsspConn, err := c.server.deps.TLS.UpgradeServerConnForCredSSP(c.conn)
			if err != nil {
				return fmt.Errorf("TLS handshake failed: %w", err)
			}

			state := credsspConn.ConnectionState()
			c.server.deps.Logger.Warn().
				Str("version", tlsVersionString(state.Version)).
				Str("cipher", tls.CipherSuiteName(state.CipherSuite)).
				Bool("hwAccel", false).
				Msg("RDP: TLS handshake complete (CredSSP mode)")

			c.server.deps.Logger.Warn().Msg("RDP: starting CredSSP/NLA authentication")

			// Type assert to *tls.Conn for CredSSP handler
			goTLSConn, ok := credsspConn.(*tls.Conn)
			if !ok {
				return fmt.Errorf("CredSSP requires Go's *tls.Conn")
			}

			handler := credssp.NewHandler(goTLSConn)
			handler.SetDebugLog(func(format string, args ...any) {
				c.server.deps.Logger.Warn().Msgf(format, args...)
			})

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
			// For a TLS server, we need to get the certificate we sent to the client
			if c.server.deps.TLS != nil {
				// Get server certificate using the TLS provider
				serverCert := c.server.deps.TLS.GetServerCertificate(state.ServerName)
				if serverCert != nil && len(serverCert.Certificate) > 0 {
					// Parse the certificate to get the public key info
					if parsedCert, parseErr := x509.ParseCertificate(serverCert.Certificate[0]); parseErr == nil {
						handler.SetServerPublicKey(parsedCert.RawSubjectPublicKeyInfo)
						handler.SetServerCertificateDER(serverCert.Certificate[0])
						c.server.deps.Logger.Debug().
							Int("pubKeyLen", len(parsedCert.RawSubjectPublicKeyInfo)).
							Int("certDERLen", len(serverCert.Certificate[0])).
							Msg("RDP: set server public key for CredSSP")
					}
				}
			}

			username, err := handler.Authenticate()
			if err != nil {
				return fmt.Errorf("CredSSP authentication failed: %w", err)
			}

			c.server.deps.Logger.Warn().
				Str("username", username).
				Msg("RDP: CredSSP/NLA authentication complete")

			// Replace connection and reader with TLS versions
			c.conn = credsspConn
			c.reader = bufio.NewReader(credsspConn)
		} else {
			// Plain TLS mode: use hardware-accelerated TLS if available
			tlsConn, err := c.server.deps.TLS.UpgradeServerConn(c.conn)
			if err != nil {
				return fmt.Errorf("TLS handshake failed: %w", err)
			}

			c.server.deps.Logger.Warn().
				Str("version", tlsConn.GetProtocolVersion()).
				Str("cipher", tlsConn.GetCipherName()).
				Bool("hwAccel", tlsConn.IsHardwareAccelerated()).
				Msg("RDP: TLS handshake complete")

			// Replace connection and reader with TLS versions
			c.conn = tlsConn
			c.reader = bufio.NewReader(tlsConn)
		}
	}

	// Clear both read and write deadlines that were set during TLS handshake.
	// IMPORTANT: SetDeadline clears both, while SetReadDeadline only clears read.
	// If we only clear read deadline, writes will start failing after HandshakeTimeout (30s).
	if err := c.conn.SetDeadline(time.Time{}); err != nil {
		return err
	}

	c.phase = PhaseBasicSettings
	return nil
}

// tlsVersionString returns a human-readable TLS version string.
func tlsVersionString(version uint16) string {
	switch version {
	case tls.VersionTLS10:
		return "TLS 1.0"
	case tls.VersionTLS11:
		return "TLS 1.1"
	case tls.VersionTLS12:
		return "TLS 1.2"
	case tls.VersionTLS13:
		return "TLS 1.3"
	default:
		return fmt.Sprintf("unknown (0x%04x)", version)
	}
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
		c.server.deps.Logger.Warn().Err(err).Msg("RDP: failed to parse MCS Connect-Initial")
		return fmt.Errorf("parse connect-initial: %w", err)
	}
	c.server.deps.Logger.Warn().
		Int("userDataLen", len(ci.UserData)).
		Msg("RDP: MCS Connect-Initial parsed successfully")

	// Log client's domain parameters for debugging
	c.server.deps.Logger.Warn().
		Int("targetMaxChannels", ci.TargetParams.MaxChannelIDs).
		Int("targetMaxUsers", ci.TargetParams.MaxUserIDs).
		Int("targetMaxPDU", ci.TargetParams.MaxMCSPDUSize).
		Int("minMaxChannels", ci.MinParams.MaxChannelIDs).
		Int("minMaxUsers", ci.MinParams.MaxUserIDs).
		Int("maxMaxChannels", ci.MaxParams.MaxChannelIDs).
		Int("maxMaxUsers", ci.MaxParams.MaxUserIDs).
		Msg("RDP: client domain parameters")

	// Parse GCC user data
	ccr, err := protocol.ParseConferenceCreateRequest(ci.UserData)
	if err != nil {
		c.server.deps.Logger.Warn().Err(err).Msg("RDP: failed to parse GCC data, continuing")
	}

	// Log what client data blocks were parsed
	if ccr != nil {
		c.server.deps.Logger.Warn().
			Bool("hasCoreData", ccr.CoreData != nil).
			Bool("hasSecurityData", ccr.SecurityData != nil).
			Bool("hasNetworkData", ccr.NetworkData != nil).
			Bool("hasMsgChannelData", ccr.MsgChannelData != nil).
			Msg("RDP: client GCC data blocks")

		if ccr.CoreData != nil {
			c.server.deps.Logger.Warn().
				Uint16("earlyCapFlags", ccr.CoreData.EarlyCapabilityFlags).
				Uint32("version", ccr.CoreData.Version).
				Msg("RDP: client core data details")
		}
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
	}

	// Extract channel definitions
	if ccr != nil && ccr.NetworkData != nil {
		// Log ALL raw channels from client for debugging
		rawNames := make([]string, len(ccr.NetworkData.Channels))
		for i, ch := range ccr.NetworkData.Channels {
			rawNames[i] = fmt.Sprintf("[%d]=%q", i, ch.Name.String())
		}
		c.server.deps.Logger.Warn().
			Uint32("rawChannelCount", ccr.NetworkData.ChannelCount).
			Int("parsedChannels", len(ccr.NetworkData.Channels)).
			Strs("rawChannelNames", rawNames).
			Msg("RDP: client NetworkData (raw)")

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

		// Log filtered channel info
		channelNames := make([]string, len(c.channels))
		for i, ch := range c.channels {
			channelNames[i] = ch.Name
		}
		c.server.deps.Logger.Warn().
			Int("filteredCount", len(c.channels)).
			Strs("filteredNames", channelNames).
			Msg("RDP: client requested virtual channels (after filtering)")
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
		EarlyCapFlags:        protocol.EarlyCapDynamicDst | protocol.EarlyCapSkipChannelJoin, // Modern capabilities
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

	// Don't send SC_MULTITRANSPORT - some clients (Jump Desktop) don't recognize
	// this optional block type (0x0C08 = 3080 decimal) and fail with
	// "Unknown server header type: 3080". We don't support UDP transport anyway.
	gccResponse := protocol.BuildConferenceCreateResponse(serverCore, serverNetwork, serverSecurity, nil)
	domainParams := protocol.DefaultDomainParameters()

	// Build the full MCS Connect-Response for logging
	mcsResponse := protocol.BuildConnectResponse(protocol.MCSResultSuccessful, 0, domainParams, gccResponse)

	c.server.deps.Logger.Warn().
		Int("gccResponseLen", len(gccResponse)).
		Uint32("clientReqProto", clientReqProto).
		Uint16("mcsChannelID", serverNetwork.MCSChannelID).
		Uint16("channelCount", serverNetwork.ChannelCount).
		Interface("channelIDs", serverNetwork.ChannelIDs).
		Uint16("msgChannelID", serverNetwork.MsgChannelID).
		Str("gccResponseHex", fmt.Sprintf("% 02X", gccResponse)).
		Int("mcsResponseLen", len(mcsResponse)).
		Str("mcsResponseHex", fmt.Sprintf("% 02X", mcsResponse)).
		Msg("RDP: building MCS Connect-Response")

	if err := protocol.WriteX224Data(c.conn, mcsResponse); err != nil {
		return fmt.Errorf("write connect-response: %w", err)
	}

	c.server.deps.Logger.Warn().Msg("RDP: sent MCS Connect-Response successfully")

	// Clear deadline
	if err := c.conn.SetReadDeadline(time.Time{}); err != nil {
		return err
	}

	c.phase = PhaseChannelConnection
	return nil
}

// handleMCSChannelSetup handles MCS Erect Domain, Attach User, and Channel Join.
func (c *Connection) handleMCSChannelSetup() error {
	c.server.deps.Logger.Warn().Str("remote", c.RemoteAddr()).
		Msg("RDP: starting MCS channel setup, waiting for Erect Domain Request...")

	// Set deadline
	if err := c.conn.SetReadDeadline(time.Now().Add(protocol.NegotiationTimeout)); err != nil {
		return err
	}

	// 1. Receive Erect Domain Request
	data, err := protocol.ReadX224Data(c.reader)
	if err != nil {
		c.server.deps.Logger.Warn().Err(err).Msg("RDP: failed to read Erect Domain Request")
		return fmt.Errorf("read erect domain: %w", err)
	}
	c.server.deps.Logger.Warn().Int("dataLen", len(data)).Msg("RDP: received data for Erect Domain")

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
		c.server.deps.Logger.Warn().Err(err).Msg("RDP: failed to read Attach User Request")
		return fmt.Errorf("read attach user: %w", err)
	}

	c.server.deps.Logger.Warn().
		Int("dataLen", len(data)).
		Str("dataHex", fmt.Sprintf("% X", data)).
		Msg("RDP: received Attach User Request")

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

	c.server.deps.Logger.Warn().
		Uint16("userID", c.userID).
		Int("confirmLen", len(confirm)).
		Str("confirmHex", fmt.Sprintf("% X", confirm)).
		Msg("RDP: sending Attach User Confirm")

	if err := protocol.WriteMCSPDU(c.conn, confirm); err != nil {
		return fmt.Errorf("write attach user confirm: %w", err)
	}

	c.server.deps.Logger.Warn().Uint16("userID", c.userID).
		Msg("RDP: sent attach user confirm successfully")

	// 3. Handle Channel Join Requests
	// Client will join: user channel, I/O channel, and virtual channels
	// Note: c.channels only contains channels with non-empty names (filtered earlier)
	expectedJoins := 2 + len(c.channels) // user + IO + virtual channels
	if c.msgChannelID != 0 {
		expectedJoins++ // message channel
	}

	c.server.deps.Logger.Warn().
		Int("expectedJoins", expectedJoins).
		Int("virtualChannels", len(c.channels)).
		Uint16("msgChannelID", c.msgChannelID).
		Msg("RDP: waiting for channel join requests")

	for i := range expectedJoins {
		c.server.deps.Logger.Warn().Int("joinIndex", i).Msg("RDP: waiting for channel join request")
		data, err = protocol.ReadX224Data(c.reader)
		if err != nil {
			c.server.deps.Logger.Warn().Err(err).Int("joinIndex", i).Msg("RDP: failed to read channel join")
			return fmt.Errorf("read channel join: %w", err)
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
