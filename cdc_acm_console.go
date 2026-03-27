package kvm

import (
	"io"
	"os"

	"github.com/pion/webrtc/v4"
)

const cdcACMDevicePath = "/dev/ttyGS0"

func handleCDCACMChannel(d *webrtc.DataChannel) {
	scopedLogger := cdcACMLogger.With().
		Uint16("data_channel_id", *d.ID()).Logger()

	var f *os.File
	d.OnOpen(func() {
		var err error
		f, err = os.OpenFile(cdcACMDevicePath, os.O_RDWR, 0)
		if err != nil {
			scopedLogger.Warn().Err(err).Str("path", cdcACMDevicePath).Msg("Failed to open CDC-ACM device")
			d.Close()
			return
		}

		go func() {
			buf := make([]byte, 1024)
			for {
				n, err := f.Read(buf)
				if err != nil {
					if err != io.EOF {
						scopedLogger.Warn().Err(err).Msg("Failed to read from CDC-ACM device")
					}
					break
				}
				if err := d.Send(buf[:n]); err != nil {
					scopedLogger.Warn().Err(err).Msg("Failed to send CDC-ACM output")
					break
				}
			}
		}()

		scopedLogger.Info().Msg("CDC-ACM console channel opened")
	})

	d.OnMessage(func(msg webrtc.DataChannelMessage) {
		if f == nil {
			return
		}
		if _, err := f.Write(msg.Data); err != nil {
			scopedLogger.Warn().Err(err).Msg("Failed to write to CDC-ACM device")
		}
	})

	d.OnClose(func() {
		if f != nil {
			f.Close()
		}
		scopedLogger.Info().Msg("CDC-ACM console channel closed")
	})

	d.OnError(func(err error) {
		scopedLogger.Warn().Err(err).Msg("CDC-ACM console channel error")
	})
}
