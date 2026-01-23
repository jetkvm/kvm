package rdp

// Audio channel wiring: RDPSND (MS-RDPEA) and CLIPRDR (MS-RDPECLIP).

import (
	"github.com/jetkvm/kvm/internal/rdp/channels"
)

func (c *Connection) initSoundChannel() {
	c.soundChannel = channels.NewSoundChannel(func(data []byte) error {
		return c.sendStaticChannelDataHotPath(c.rdpsndID, data)
	})

	c.soundChannel.SetReadyCallback(func(s *channels.SoundChannel) {
		fmt, ok := s.GetSelectedFormat()
		if !ok {
			return
		}

		c.server.deps.Logger.Info().
			Uint16("channels", fmt.Channels).
			Uint32("sampleRate", fmt.SamplesPerSec).
			Uint16("bitsPerSample", fmt.BitsPerSample).
			Msg("RDPSND ready")

		if c.server.deps.Audio != nil {
			c.server.deps.Audio.Connect()
		}

		c.startAudioStream()
	})

	if err := c.soundChannel.Start(); err != nil {
		c.server.deps.Logger.Warn().Err(err).Msg("failed to start rdpsnd")
	}
}

func (c *Connection) initClipboardChannel() {
	c.clipboardChannel = channels.NewClipboardChannel(func(data []byte) error {
		return c.sendClipboardData(data)
	})

	if err := c.clipboardChannel.Start(); err != nil {
		c.server.deps.Logger.Warn().Err(err).Msg("failed to start cliprdr")
	}
}

func (c *Connection) sendClipboardData(data []byte) error {
	if c.cliprdrdID == 0 {
		return nil
	}
	return c.sendStaticChannelDataHotPath(c.cliprdrdID, data)
}

func (c *Connection) startAudioStream() {
	if c.server.deps.Audio == nil {
		return
	}

	c.audioChan = c.server.deps.Audio.SubscribeAudio()
	if c.audioChan == nil {
		return
	}

	c.audioStopCh = make(chan struct{})
	go c.audioStreamLoop()
}

func (c *Connection) audioStreamLoop() {
	defer func() {
		if c.server.deps.Audio != nil && c.audioChan != nil {
			c.server.deps.Audio.UnsubscribeAudio(c.audioChan)
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
			if err := c.soundChannel.SendAudioChunked(audioData); err != nil {
				if err == channels.ErrSoundBackpressure {
					c.server.deps.Logger.Debug().Msg("audio dropped: backpressure")
				} else if err != channels.ErrSoundNotReady {
					c.server.deps.Logger.Debug().Err(err).Msg("audio send failed")
				}
			}
		}
	}
}

func (c *Connection) stopAudioStream() {
	if c.audioStopCh != nil {
		close(c.audioStopCh)
		c.audioStopCh = nil
	}
}

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
				c.server.deps.Logger.Trace().Err(err).Msg("AUDIN playback failed")
			}
		}
	}
}
