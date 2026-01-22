package rdp

// Audio streaming for RDP connections.
// This file contains sound channel initialization and audio streaming loops.

import (
	"github.com/jetkvm/kvm/internal/rdp/channels"
)

// initSoundChannel initializes the RDPSND static channel.
// RDPSND requires Virtual Channel PDU header per MS-RDPBCGR 2.2.6.1
// HOT PATH: Uses zero-allocation pooled buffers for audio streaming.
func (c *Connection) initSoundChannel() {
	// Create sound channel with send callback
	c.soundChannel = channels.NewSoundChannel(func(data []byte) error {
		// HOT PATH: Zero allocations for typical audio packets
		return c.sendStaticChannelDataHotPath(c.rdpsndID, data)
	})

	// Set ready callback to start audio streaming
	c.soundChannel.SetReadyCallback(func(s *channels.SoundChannel) {
		fmt, ok := s.GetSelectedFormat()
		if !ok {
			return
		}

		c.server.deps.Logger.Info().
			Uint16("channels", fmt.Channels).
			Uint32("sampleRate", fmt.SamplesPerSec).
			Uint16("bitsPerSample", fmt.BitsPerSample).
			Msg("RDP: RDPSND channel ready, starting audio stream")

		// Signal audio system that RDP needs audio
		if c.server.deps.Audio != nil {
			c.server.deps.Audio.Connect()
		}

		// Start audio streaming goroutine
		c.startAudioStream()
	})

	// Start format negotiation
	if err := c.soundChannel.Start(); err != nil {
		c.server.deps.Logger.Debug().Err(err).Msg("RDP: failed to start rdpsnd")
	}
}

// initClipboardChannel initializes the CLIPRDR static channel.
func (c *Connection) initClipboardChannel() {
	// Create clipboard channel with send callback
	// CLIPRDR requires Virtual Channel PDU header per MS-RDPBCGR 2.2.6.1
	c.clipboardChannel = channels.NewClipboardChannel(func(data []byte) error {
		return c.sendClipboardData(data)
	})

	// NOTE: Logger disabled for maximum performance

	// Start clipboard channel (sends Capabilities and Monitor Ready)
	if err := c.clipboardChannel.Start(); err != nil {
		c.server.deps.Logger.Debug().Err(err).Msg("RDP: failed to start cliprdr")
	} else {
		c.server.deps.Logger.Debug().Msg("RDP: clipboard channel initialized")
	}
}

// sendClipboardData sends data on the cliprdr channel with proper VC PDU header.
// Per MS-RDPBCGR 2.2.6.1, virtual channel data must include the VC PDU header.
// Uses zero-allocation pooled buffers for better performance.
// HOT PATH: No logging here to avoid performance impact.
func (c *Connection) sendClipboardData(data []byte) error {
	if c.cliprdrdID == 0 {
		return nil
	}
	return c.sendStaticChannelDataHotPath(c.cliprdrdID, data)
}

// startAudioStream starts the audio streaming goroutine.
func (c *Connection) startAudioStream() {
	if c.server.deps.Audio == nil {
		c.server.deps.Logger.Debug().Msg("RDP: audio provider not available")
		return
	}

	// Subscribe to audio
	c.audioChan = c.server.deps.Audio.SubscribeAudio()
	if c.audioChan == nil {
		c.server.deps.Logger.Debug().Msg("RDP: failed to subscribe to audio")
		return
	}

	c.audioStopCh = make(chan struct{})

	go c.audioStreamLoop()
}

// audioStreamLoop reads audio from the provider and sends to the client.
func (c *Connection) audioStreamLoop() {
	defer func() {
		if c.server.deps.Audio != nil {
			c.server.deps.Audio.UnsubscribeAudio()
		}
	}()

	for {
		select {
		case <-c.stopChan:
			return
		case <-c.audioStopCh:
			return
		case audioData, ok := <-c.audioChan:
			if !ok {
				return
			}

			if c.soundChannel == nil || !c.soundChannel.IsReady() {
				continue
			}

			// Send audio in chunks
			if err := c.soundChannel.SendAudioChunked(audioData); err != nil {
				if err == channels.ErrSoundBackpressure {
					// Too many blocks pending - skip this one
					c.server.deps.Logger.Debug().Msg("RDP: audio dropped due to backpressure")
				} else if err != channels.ErrSoundNotReady {
					c.server.deps.Logger.Debug().Err(err).Msg("RDP: failed to send audio")
				}
			}
		}
	}
}

// stopAudioStream stops the audio streaming.
func (c *Connection) stopAudioStream() {
	if c.audioStopCh != nil {
		close(c.audioStopCh)
		c.audioStopCh = nil
	}
}

// audinDataLoop processes AUDIN audio data asynchronously.
// This runs in its own goroutine to prevent blocking the DVC message loop.
func (c *Connection) audinDataLoop() {
	for {
		select {
		case <-c.audinStopCh:
			return
		case data, ok := <-c.audinDataChan:
			if !ok {
				return
			}
			if c.server.deps.Audio == nil {
				continue
			}
			if err := c.server.deps.Audio.PlayAudio(data); err != nil {
				c.server.deps.Logger.Debug().Err(err).Int("len", len(data)).Msg("RDP: failed to play AUDIN audio")
			}
		}
	}
}
