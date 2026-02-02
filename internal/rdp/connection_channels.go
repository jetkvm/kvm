package rdp

import (
	"encoding/binary"
	"time"

	"github.com/jetkvm/kvm/internal/rdp/channels"
	"github.com/jetkvm/kvm/internal/rdp/protocol"
)

// Dynamic Virtual Channel (MS-RDPEDYC) and static channel management.
// Initializes RDPGFX (MS-RDPEGFX), AUDIN (MS-RDPEAI), camera (MS-RDPECAM).

func (c *Connection) initDynamicChannels() error {
	// Find static channel IDs
	c.channelsMu.RLock()
	for _, ch := range c.channels {
		c.server.deps.Logger.Debug().Str("channel", ch.Name).Uint16("id", ch.ID).Msg("RDP: found static channel")
		switch ch.Name {
		case "drdynvc":
			c.drdynvcID = ch.ID
		case "rdpsnd":
			c.rdpsndID = ch.ID
		case "cliprdr":
			c.cliprdrdID = ch.ID
			c.server.deps.Logger.Debug().Uint16("id", ch.ID).Msg("RDP: cliprdr channel found")
		}
	}
	c.channelsMu.RUnlock()

	// NOTE: Channel initialization is deferred until AFTER DVC setup
	// to avoid interfering with DVC capability exchange.
	// RDPSND is still needed for audio output even when DVC is available.

	if c.drdynvcID == 0 {
		// Client doesn't support dynamic channels
		// Initialize RDPSND and clipboard now
		if c.rdpsndID != 0 {
			c.initSoundChannel()
		}
		if c.cliprdrdID != 0 {
			c.initClipboardChannel()
		}
		return nil
	}

	// Create DVC manager with send callback
	c.dvcManager = channels.NewDVCManager(func(data []byte) error {
		return c.sendDVCData(data)
	})

	// Enable DVC logger for debugging capability exchange
	c.dvcManager.SetLogger(func(msg string, channel string, channelID uint32, args ...interface{}) {
		c.server.deps.Logger.Debug().
			Str("channel", channel).
			Uint32("channelID", channelID).
			Msgf("DVC: "+msg, args...)
	})

	// Set up synchronous callback for when capability response is received.
	c.dvcManager.SetOnCapabilityReceived(func() {
		c.initDVCChannelsSync()
	})

	// Send capability request
	if err := c.dvcManager.SendCapabilityRequest(); err != nil {
		return err
	}
	c.server.deps.Logger.Debug().Msg("RDP: DVC capability request sent")

	return nil
}

// initDVCChannelsSync creates DVC channels synchronously.
// Called from the capability response handler in the message loop context.
func (c *Connection) initDVCChannelsSync() {
	// Initialize RDPSND for audio output (static channel, not DVC)
	// Only initialize if audio is enabled in config
	if c.rdpsndID != 0 {
		if !c.server.deps.Config.GetRDPAudioEnabled() {
			c.server.deps.Logger.Debug().Msg("RDP: audio output disabled in config, skipping RDPSND channel")
		} else {
			c.initSoundChannel()
		}
	}

	// Initialize clipboard channel AFTER DVC capability exchange completes
	// to avoid any interference
	if c.cliprdrdID != 0 {
		c.initClipboardChannel()
	}

	// Create RDPGFX channel only if video is enabled in config
	if !c.server.deps.Config.GetRDPVideoEnabled() {
		c.server.deps.Logger.Debug().Msg("RDP: H.264 video disabled in config, skipping RDPGFX channel")
	} else {
		c.gfxChannel = channels.NewGFXChannel(c.dvcManager)

		// Set logger for debugging capability negotiation
		c.gfxChannel.SetLogger(func(msg string, args ...interface{}) {
			c.server.deps.Logger.Debug().Msgf(msg, args...)
		})

		// Set callback to initialize surface when channel is ready
		c.gfxChannel.SetReadyCallback(func(g *channels.GFXChannel) {
			w, h := c.GetResolution()
			c.server.deps.Logger.Debug().
				Uint16("width", w).
				Uint16("height", h).
				Bool("avc420", g.SupportsAVC420()).
				Bool("avc444", g.SupportsAVC444()).
				Msg("RDP: RDPGFX channel ready")

			if err := g.Initialize(w, h); err != nil {
				c.server.deps.Logger.Warn().Err(err).Msg("RDP: failed to initialize GFX surface")
				return
			}

			// Mark RDPGFX as supported
			c.gfxSupported.Store(true)

			// Start video capture now that the channel is ready
			if c.server.deps.Video != nil {
				if err := c.server.deps.Video.StartVideo(); err != nil {
					c.server.deps.Logger.Warn().Err(err).Msg("RDP: failed to start video capture")
				} else {
					c.server.deps.Logger.Debug().Msg("RDP: video capture started (RDPGFX)")
				}
			}
		})

		if err := c.gfxChannel.Open(); err != nil {
			c.server.deps.Logger.Debug().Err(err).Msg("RDP: failed to open RDPGFX channel")
			// Non-fatal - we can still work without graphics channel
		}
	}

	// Create AUDIN channel for microphone input (only if mic is enabled in config)
	if !c.server.deps.Config.GetRDPMicEnabled() {
		c.server.deps.Logger.Info().Msg("RDP: microphone input disabled in config, skipping AUDIN channel")
	} else {
		c.audinChannel = channels.NewAudinChannel(c.dvcManager)

		// Set logger for debugging
		c.audinChannel.SetLogger(func(msg string, args ...interface{}) {
			c.server.deps.Logger.Debug().Msgf(msg, args...)
		})

		// Log AUDIN channel close at WARN level for diagnostic visibility
		c.audinChannel.SetCloseCallback(func() {
			c.server.deps.Logger.Warn().Msg("RDP: AUDIN channel closed (mic data will stop)")
		})

		// Set ready callback for AUDIN
		c.audinChannel.SetReadyCallback(func(a *channels.AudinChannel) {
			fmt, ok := a.GetSelectedFormat()
			if !ok {
				return
			}

			c.server.deps.Logger.Info().
				Uint16("channels", fmt.Channels).
				Uint32("sampleRate", fmt.SamplesPerSec).
				Uint16("bitsPerSample", fmt.BitsPerSample).
				Msg("RDP: AUDIN channel ready for microphone input")

			// Automatically enable audio input when RDP client has mic enabled
			// This initializes the USB audio gadget playback without requiring manual UI toggle
			if c.server.deps.Audio != nil {
				if err := c.server.deps.Audio.EnableAudioInput(); err != nil {
					c.server.deps.Logger.Warn().Err(err).Msg("RDP: failed to enable audio input for AUDIN")
				}
			}
		})

		// Create AUDIN async buffer and processing goroutine
		// This prevents AUDIN data processing from blocking the DVC message loop
		// which would delay GFX frame ACKs and cause video stuttering
		c.audinDataChan = make(chan *audinPooledBuffer, 30) // Buffer for ~300ms at 10ms packets
		c.audinStopCh = make(chan struct{})
		go c.audinDataLoop()

		// Set data callback to forward audio to buffer (non-blocking)
		c.audinChannel.SetDataCallback(func(data []byte) {
			// Get pooled buffer - eliminates ~715MB/hour of allocations
			bufPtr := audinBufferPool.Get().(*[]byte)
			buf := *bufPtr

			// Ensure buffer is large enough (rare path for >2KB packets)
			if len(data) > cap(buf) {
				audinBufferPool.Put(bufPtr) // Return undersized buffer
				buf = make([]byte, len(data))
				bufPtr = &buf
			}

			// Copy data to pooled buffer
			buf = buf[:len(data)]
			copy(buf, data)

			pooled := &audinPooledBuffer{Data: buf, buf: bufPtr}

			// Non-blocking send - drop if buffer is full
			select {
			case c.audinDataChan <- pooled:
				// Data queued successfully
			default:
				// Buffer full, drop and return to pool
				pooled.Release()
			}
		})

		if err := c.audinChannel.Open(); err != nil {
			c.server.deps.Logger.Debug().Err(err).Msg("RDP: failed to open AUDIN channel")
			// Non-fatal - we can still work without audio input
		}
	}

	// Create camera channel for webcam redirection (only if camera is enabled in config)
	if !c.server.deps.Config.GetRDPCameraEnabled() {
		c.server.deps.Logger.Info().Msg("RDP: camera redirection disabled in config, skipping camera channel")
	} else {
		c.cameraChannel = channels.NewCameraChannel(c.dvcManager)
		// Set logger for debugging format negotiation
		c.cameraChannel.SetLogger(func(msg string, args ...interface{}) {
			c.server.deps.Logger.Debug().Msgf(msg, args...)
		})

		// Set ready callback for camera
		c.cameraChannel.SetReadyCallback(func(cam *channels.CameraChannel) {
			// Camera channel is ready, but DON'T activate yet.
			// Wait for the USB host to actually start streaming (format change notification).
			// This prevents the client's camera from staying on when not in use.
			c.server.deps.Logger.Debug().Msg("RDP: camera channel ready, waiting for USB host to start streaming")
		})

		// Set frame callback to forward to UVC gadget
		c.cameraChannel.SetFrameCallback(func(frame []byte, width, height, pixelFormat uint32) {
			if c.server.deps.Camera == nil {
				return
			}
			// Hot path - no logging here
			_ = c.server.deps.Camera.SendFrame(frame, width, height, pixelFormat)
		})

		// Set stop callback to notify when RDP client stops camera stream
		c.cameraChannel.SetStopCallback(func() {
			c.server.deps.Logger.Debug().Msg("RDP: camera stream stopped by client")
			if c.server.deps.Camera != nil {
				c.server.deps.Camera.SetEnabled(false)
			}
		})

		// Subscribe to USB host format changes for dynamic format negotiation
		if c.server.deps.Camera != nil {
			if formatChan := c.server.deps.Camera.SubscribeFormatChanges(); formatChan != nil {
				go c.handleCameraFormatChanges(formatChan)
			}
		}

		if err := c.cameraChannel.Open(); err != nil {
			c.server.deps.Logger.Debug().Err(err).Msg("RDP: failed to open camera channel")
		}
	}

	c.server.deps.Logger.Debug().Msg("RDP: dynamic virtual channels initialized")

	// Start a goroutine to check if RDPGFX becomes ready, otherwise fall back to bitmap mode
	go c.checkGFXReadinessAndFallback()
}

// checkGFXReadinessAndFallback waits for RDPGFX to become ready.
// If it doesn't become ready within timeout, falls back to bitmap updates.
func (c *Connection) checkGFXReadinessAndFallback() {
	// Wait up to 3 seconds for RDPGFX to become ready
	timeout := time.After(3 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-c.stopChan:
			return
		case <-timeout:
			// RDPGFX didn't become ready - fall back to bitmap mode
			if !c.gfxSupported.Load() {
				c.server.deps.Logger.Info().Msg("RDP: RDPGFX not supported, falling back to bitmap updates")
				if c.server.deps.Video != nil {
					c.jpegChan = c.server.deps.Video.SubscribeJPEG()
					c.startBitmapStreaming()
				}
			}
			return
		case <-ticker.C:
			if c.gfxSupported.Load() {
				// RDPGFX is ready, no need for fallback
				return
			}
		}
	}
}

// sendDVCData sends data on the drdynvc static channel.
// Note: The single Write() call is atomic at the TLS level,
// so concurrent calls to this function are safe.
//
// HOT PATH: Zero allocations using pooled buffers for typical packet sizes.
// For large payloads with kTLS enabled, uses scatter-gather for zero-copy.
func (c *Connection) sendDVCData(data []byte) error {
	if c.drdynvcID == 0 {
		return nil
	}

	// For large payloads, check if scatter-gather is available (kTLS enabled)
	// Scatter-gather avoids copying the payload into the header buffer
	if len(data) >= ScatterGatherThreshold {
		if sg := c.supportsScatterGather(); sg != nil {
			return c.sendDVCDataScatterGather(sg, data)
		}
	}

	// Use zero-allocation hot path for typical packet sizes
	return c.sendDVCDataHotPath(data)
}

// sendDVCDataHotPath sends DVC data using zero-allocation hot path.
// Uses pooled buffers to avoid heap allocations on every packet.
// Falls back to sendDVCDataFallback for packets too large for the pool.
func (c *Connection) sendDVCDataHotPath(data []byte) error {
	if c.drdynvcID == 0 {
		return nil
	}

	// Packet layout:
	// [TPKT header 4][X.224 header 3][MCS header 6-8][VC header 8][DVC data]
	// MCS header is 6 bytes for data < 128, 8 bytes otherwise
	const (
		tpktHeaderLen    = 4
		x224HeaderLen    = 3
		mcsHeaderBaseLen = 6
		vcHeaderLen      = 8
		channelFlagFirst = 0x01
		channelFlagLast  = 0x02
	)

	vcPayloadLen := len(data)
	mcsLenFieldSize := 1
	if vcPayloadLen+vcHeaderLen >= 128 {
		mcsLenFieldSize = 2
	}
	mcsHeaderLen := mcsHeaderBaseLen + mcsLenFieldSize

	totalPacketLen := tpktHeaderLen + x224HeaderLen + mcsHeaderLen + vcHeaderLen + vcPayloadLen

	// Get buffer from pool
	bufPtr := vcPDUPool.Get().(*[]byte)
	buf := *bufPtr

	if totalPacketLen > len(buf) {
		// Rare path: packet too large for pool
		vcPDUPool.Put(bufPtr)
		return c.sendDVCDataFallback(data)
	}
	defer vcPDUPool.Put(bufPtr)

	packet := buf[:totalPacketLen]
	pos := 0

	// TPKT header (4 bytes, big-endian length)
	packet[pos] = protocol.TPKTVersion
	packet[pos+1] = 0
	binary.BigEndian.PutUint16(packet[pos+2:pos+4], uint16(totalPacketLen))
	pos += tpktHeaderLen

	// X.224 Data TPDU header (3 bytes)
	packet[pos] = 2                      // LI
	packet[pos+1] = protocol.X224Data    // Code
	packet[pos+2] = protocol.X224DataEOT // EOT
	pos += x224HeaderLen

	// MCS Send Data Indication header
	packet[pos] = byte(protocol.MCSSendDataIndication << 2)
	relativeUserID := c.userID - protocol.MCSUserIDBase
	binary.BigEndian.PutUint16(packet[pos+1:pos+3], relativeUserID)
	binary.BigEndian.PutUint16(packet[pos+3:pos+5], c.drdynvcID)
	packet[pos+5] = 0x70 // High priority, begin+end segment
	pos += 6

	// MCS length field (PER encoded)
	mcsDataLen := vcHeaderLen + vcPayloadLen
	if mcsDataLen < 128 {
		packet[pos] = byte(mcsDataLen)
		pos++
	} else {
		packet[pos] = byte(0x80 | (mcsDataLen >> 8))
		packet[pos+1] = byte(mcsDataLen)
		pos += 2
	}

	// VC PDU header (8 bytes, little-endian)
	binary.LittleEndian.PutUint32(packet[pos:pos+4], uint32(vcPayloadLen))
	binary.LittleEndian.PutUint32(packet[pos+4:pos+8], channelFlagFirst|channelFlagLast)
	pos += vcHeaderLen

	// DVC data payload
	copy(packet[pos:], data)

	return c.writeWithDeadline(func() error {
		_, err := c.conn.Write(packet)
		return err
	})
}

// sendDVCDataFallback handles oversized packets that don't fit in the pool.
func (c *Connection) sendDVCDataFallback(data []byte) error {
	return c.sendStaticChannelDataFallback(c.drdynvcID, data)
}

// sendStaticChannelDataHotPath sends data on a static channel using zero-allocation hot path.
// Used for RDPSND (audio output) and Clipboard.
// HOT PATH: Zero heap allocations for typical packet sizes.
func (c *Connection) sendStaticChannelDataHotPath(channelID uint16, data []byte) error {
	if channelID == 0 {
		return nil
	}

	// Packet layout:
	// [TPKT header 4][X.224 header 3][MCS header 6-8][VC header 8][data]
	// MCS header is 6 bytes for data < 128, 8 bytes otherwise
	const (
		tpktHeaderLen    = 4
		x224HeaderLen    = 3
		mcsHeaderBaseLen = 6
		vcHeaderLen      = 8
		channelFlagFirst = 0x01
		channelFlagLast  = 0x02
	)

	vcPayloadLen := len(data)
	mcsLenFieldSize := 1
	if vcPayloadLen+vcHeaderLen >= 128 {
		mcsLenFieldSize = 2
	}
	mcsHeaderLen := mcsHeaderBaseLen + mcsLenFieldSize

	totalPacketLen := tpktHeaderLen + x224HeaderLen + mcsHeaderLen + vcHeaderLen + vcPayloadLen

	// Get buffer from pool
	bufPtr := vcPDUPool.Get().(*[]byte)
	buf := *bufPtr

	if totalPacketLen > len(buf) {
		// Rare path: packet too large for pool
		vcPDUPool.Put(bufPtr)
		return c.sendStaticChannelDataFallback(channelID, data)
	}
	defer vcPDUPool.Put(bufPtr)

	packet := buf[:totalPacketLen]
	pos := 0

	// TPKT header (4 bytes, big-endian length)
	packet[pos] = protocol.TPKTVersion
	packet[pos+1] = 0
	binary.BigEndian.PutUint16(packet[pos+2:pos+4], uint16(totalPacketLen))
	pos += tpktHeaderLen

	// X.224 Data TPDU header (3 bytes)
	packet[pos] = 2                      // LI
	packet[pos+1] = protocol.X224Data    // Code
	packet[pos+2] = protocol.X224DataEOT // EOT
	pos += x224HeaderLen

	// MCS Send Data Indication header
	packet[pos] = byte(protocol.MCSSendDataIndication << 2)
	relativeUserID := c.userID - protocol.MCSUserIDBase
	binary.BigEndian.PutUint16(packet[pos+1:pos+3], relativeUserID)
	binary.BigEndian.PutUint16(packet[pos+3:pos+5], channelID)
	packet[pos+5] = 0x70 // High priority, begin+end segment
	pos += 6

	// MCS length field (PER encoded)
	mcsDataLen := vcHeaderLen + vcPayloadLen
	if mcsDataLen < 128 {
		packet[pos] = byte(mcsDataLen)
		pos++
	} else {
		packet[pos] = byte(0x80 | (mcsDataLen >> 8))
		packet[pos+1] = byte(mcsDataLen)
		pos += 2
	}

	// VC PDU header (8 bytes, little-endian)
	binary.LittleEndian.PutUint32(packet[pos:pos+4], uint32(vcPayloadLen))
	binary.LittleEndian.PutUint32(packet[pos+4:pos+8], channelFlagFirst|channelFlagLast)
	pos += vcHeaderLen

	// Data payload
	copy(packet[pos:], data)

	return c.writeWithDeadline(func() error {
		_, err := c.conn.Write(packet)
		return err
	})
}

// sendStaticChannelDataFallback handles oversized packets that don't fit in the pool.
func (c *Connection) sendStaticChannelDataFallback(channelID uint16, data []byte) error {
	const (
		channelFlagFirst = 0x01
		channelFlagLast  = 0x02
		vcHeaderSize     = 8
	)

	vcPDU := make([]byte, vcHeaderSize+len(data))
	binary.LittleEndian.PutUint32(vcPDU[0:4], uint32(len(data)))
	binary.LittleEndian.PutUint32(vcPDU[4:8], channelFlagFirst|channelFlagLast)
	copy(vcPDU[8:], data)

	return c.writeWithDeadline(func() error {
		return protocol.WriteSendDataIndicationPooled(c.conn, c.userID, channelID, vcPDU)
	})
}

// SendFrame sends an H.264 video frame to the client.
// The frame should contain raw H.264 NAL units.
//
// HOT PATH: Called for every video frame (~30-60 fps).
// Early bailout checks are ordered by cost (cheapest first).
func (c *Connection) SendFrame(frame []byte) {
	// Track all attempted frames for diagnostics
	c.frameStats.attempted.Add(1)

	// Fast path: single atomic check for closed connection
	if c.closed.Load() {
		return
	}

	// Debug check for potentially corrupted frames (zeros = green in YUV)
	// This helps diagnose intermittent green screen issues
	if len(frame) > 0 && len(frame) < 16 {
		// Extremely small frame - likely corrupt
		c.server.deps.Logger.Warn().
			Int("frameLen", len(frame)).
			Msg("RDP: suspiciously small frame received")
	}

	// Fast path: nil check (no atomic)
	if c.gfxChannel == nil {
		c.frameRequested.Store(false)
		return
	}

	// Fast path: check if channel is ready
	if !c.gfxChannel.IsReady() {
		c.frameStats.dropNotReady.Add(1)
		c.frameRequested.Store(false)
		return
	}

	// Detect keyframe by checking NAL unit type
	// This is more expensive (scans up to 1KB) so we do it after ready check
	isKeyframe := isH264Keyframe(frame)

	// Debug: Validate frame integrity to help diagnose green screen issues
	// Green frames (zeros in YUV) can occur if frame data is corrupted
	if len(frame) >= 8 {
		// Check if frame starts with valid H.264 start code (00 00 00 01 or 00 00 01)
		hasValidStart := (frame[0] == 0 && frame[1] == 0 && frame[2] == 0 && frame[3] == 1) ||
			(frame[0] == 0 && frame[1] == 0 && frame[2] == 1)

		// Check if first 8 bytes are all zeros (indicates corrupted frame)
		allZeros := frame[0] == 0 && frame[1] == 0 && frame[2] == 0 && frame[3] == 0 &&
			frame[4] == 0 && frame[5] == 0 && frame[6] == 0 && frame[7] == 0

		if allZeros || !hasValidStart {
			c.server.deps.Logger.Warn().
				Int("frameLen", len(frame)).
				Bool("isKeyframe", isKeyframe).
				Bool("allZeros", allZeros).
				Bool("hasValidStart", hasValidStart).
				Hex("firstBytes", frame[:min(16, len(frame))]).
				Msg("RDP: potentially corrupted frame detected")
		}
	}

	// Don't send non-keyframes until we've sent a keyframe first
	// The H.264 decoder needs SPS/PPS (which come with keyframes) before it can decode
	if !isKeyframe && !c.hasReceivedKeyframe.Load() {
		dropped := c.frameStats.dropNoKeyframe.Add(1)
		// If we've dropped 30+ frames waiting for keyframe (~1 sec at 30fps), request one
		if dropped%30 == 0 && c.server.deps.Video != nil {
			c.server.deps.Video.RequestKeyframe()
		}
		c.frameRequested.Store(false)
		return
	}

	// Adaptive backpressure for WAN/high-latency connections:
	// Drop P-frames proactively when queue is filling up to prevent complete queue saturation.
	// This gives the network time to catch up while maintaining keyframe delivery.
	// IMPORTANT: Request keyframe IMMEDIATELY when dropping starts to minimize green frames.
	if !isKeyframe {
		if c.gfxChannel.ShouldDropPFrame() {
			// Queue >90% full - drop all P-frames, only keyframes through
			c.frameStats.dropBackpressure.Add(1)
			c.frameRequested.Store(false)

			// Request keyframe IMMEDIATELY when entering backpressure (once per episode).
			// Dropped P-frames break the decode chain - the sooner we get a keyframe, the better.
			// The flag resets when queue drains, so we request again if backpressure recurs.
			if !c.frameStats.backpressureKeyframeRequested.Swap(true) && c.server.deps.Video != nil {
				c.server.deps.Video.RequestKeyframe()
				c.server.deps.Logger.Debug().
					Int("pending", c.gfxChannel.GetPendingFrames()).
					Msg("RDP: requesting keyframe due to P-frame backpressure")
			}
			return
		}
		// Queue drained below drop threshold - reset keyframe request flag for next episode
		c.frameStats.backpressureKeyframeRequested.Store(false)

		if c.gfxChannel.ShouldRateLimitPFrame() {
			// Queue >80% full - drop every other P-frame (rate limiting)
			if c.frameStats.attempted.Load()%2 == 0 {
				c.frameStats.dropBackpressure.Add(1)
				c.frameRequested.Store(false)
				return
			}
		}
	}

	// Track that we've sent a keyframe
	if isKeyframe {
		c.hasReceivedKeyframe.Store(true)
	}

	// Send via RDPGFX with zero-copy
	if err := c.gfxChannel.SendH264Frame(frame, isKeyframe); err != nil {
		switch err {
		case channels.ErrGFXBackpressure:
			// Queue completely full - drop this frame
			// With adaptive backpressure, this should rarely happen (P-frames dropped earlier)
			// Only reset keyframe state if we're dropping a keyframe (rare) since that
			// breaks the decode chain. Dropping P-frames is less critical with adaptive dropping.
			c.frameStats.dropBackpressure.Add(1)
			if isKeyframe {
				c.hasReceivedKeyframe.Store(false)
				c.server.deps.Logger.Warn().
					Int("pending", c.gfxChannel.GetPendingFrames()).
					Msg("RDP: keyframe dropped due to full queue, decoder will need reset")
				// Request immediate keyframe from encoder to minimize recovery time
				if c.server.deps.Video != nil {
					c.server.deps.Video.RequestKeyframe()
				}
			}
		case channels.ErrGFXNoCodec:
			// Client doesn't support AVC420/AVC444 - log once and stop sending frames
			// This is not a transient error, so don't count against write errors
			if c.hasReceivedKeyframe.CompareAndSwap(true, false) {
				c.server.deps.Logger.Warn().Msg("RDP: client does not support H.264 (AVC420/AVC444), video disabled")
			}
		default:
			c.server.deps.Logger.Debug().Err(err).Msg("RDP: failed to send frame")

			// Track consecutive write errors
			errCount := c.consecutiveWriteErrors.Add(1)
			if errCount >= 5 {
				// Too many consecutive write errors - connection is unhealthy
				c.server.deps.Logger.Warn().
					Int32("errorCount", errCount).
					Msg("RDP: closing connection due to consecutive write errors")
				go c.Close() // Close asynchronously to avoid blocking
			}
		}
	} else {
		// Successfully sent frame
		c.frameStats.sent.Add(1)

		// Reset error counter on successful send
		c.consecutiveWriteErrors.Store(0)

		// Check if connection appears stale (no acks received for a while)
		if c.gfxChannel.IsConnectionStale() {
			c.server.deps.Logger.Warn().
				Int("pendingFrames", c.gfxChannel.GetPendingFrames()).
				Msg("RDP: closing stale connection (no frame acks received)")
			go c.Close()
		}
	}

	// Diagnostic logging: only log if there are NEW drops since last interval.
	// Uses 30-second intervals to avoid log spam. Only warns on active backpressure.
	now := time.Now().UnixMilli()
	lastLog := c.frameStats.lastLogTime.Load()
	if now-lastLog > 30000 && c.frameStats.lastLogTime.CompareAndSwap(lastLog, now) {
		dropNotReady := c.frameStats.dropNotReady.Load()
		dropNoKeyframe := c.frameStats.dropNoKeyframe.Load()
		dropBackpressure := c.frameStats.dropBackpressure.Load()
		totalDropped := dropNotReady + dropNoKeyframe + dropBackpressure
		lastDrops := c.frameStats.lastLogDrops.Swap(totalDropped)
		newDrops := totalDropped - lastDrops

		// Only log if there were new drops in this interval
		if newDrops > 0 {
			// Use Warn for backpressure (indicates network issues), Debug for startup drops
			if dropBackpressure > 0 {
				c.server.deps.Logger.Warn().
					Uint64("newDrops", newDrops).
					Uint64("backpressure", dropBackpressure).
					Int("pending", c.gfxChannel.GetPendingFrames()).
					Msg("RDP: frames dropped due to backpressure")
			} else {
				c.server.deps.Logger.Debug().
					Uint64("newDrops", newDrops).
					Uint64("notReady", dropNotReady).
					Uint64("noKeyframe", dropNoKeyframe).
					Msg("RDP: frames dropped during initialization")
			}
		}
	}

	c.frameRequested.Store(false)
}

// isH264Keyframe checks if the frame contains an IDR (keyframe).
// This is a fast check that looks for NAL unit type 5 (IDR) or 7 (SPS).
// HOT PATH: Limits scan to first 1KB since SPS/IDR NALs are always at frame start.
func isH264Keyframe(data []byte) bool {
	if len(data) < 5 {
		return false
	}

	// Limit scan to first 1KB - SPS/IDR NALs are always at frame start
	scanLimit := len(data) - 4
	if scanLimit > 1024 {
		scanLimit = 1024
	}

	// Look for start codes and check NAL type
	for i := 0; i < scanLimit; i++ {
		// Check for 3-byte or 4-byte start code
		if data[i] == 0 && data[i+1] == 0 {
			var nalType byte
			if data[i+2] == 1 {
				// 3-byte start code
				nalType = data[i+3] & 0x1F
			} else if data[i+2] == 0 && data[i+3] == 1 && i+4 < len(data) {
				// 4-byte start code
				nalType = data[i+4] & 0x1F
			} else {
				continue
			}

			// NAL type 5 = IDR, NAL type 7 = SPS (precedes keyframe)
			if nalType == 5 || nalType == 7 {
				return true
			}
		}
	}

	return false
}
