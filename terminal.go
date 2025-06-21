package kvm

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"

	"github.com/creack/pty"
	"github.com/pion/webrtc/v4"
)

type TerminalSize struct {
	Rows int `json:"rows"`
	Cols int `json:"cols"`
}

func handleTerminalChannel(d *webrtc.DataChannel) {
	scopedLogger := terminalLogger.With().
		Uint16("data_channel_id", *d.ID()).Logger()

	var ptmx *os.File
	var cmd *exec.Cmd
	d.OnOpen(func() {
		cmd = exec.Command("/bin/sh")
		var err error
		ptmx, err = pty.Start(cmd)
		if err != nil {
			scopedLogger.Warn().Err(err).Msg("Failed to start pty")
			d.Close()
			return
		}

		go func() {
			buf := make([]byte, 1024)
			for {
				n, err := ptmx.Read(buf)
				if err != nil {
					if err != io.EOF {
						scopedLogger.Warn().Err(err).Msg("Failed to read from pty")
					}
					break
				}
				err = d.Send(buf[:n])
				if err != nil {
					scopedLogger.Warn().Err(err).Msg("Failed to send pty output")
					break
				}
			}
		}()
	})

	d.OnMessage(func(msg webrtc.DataChannelMessage) {
		if ptmx == nil {
			return
		}
		if msg.IsString {
			maybeJson := bytes.TrimSpace(msg.Data)
			// Cheap check to see if this resembles JSON
			if len(maybeJson) > 1 && maybeJson[0] == '{' && maybeJson[len(maybeJson)-1] == '}' {
				var size TerminalSize
				err := json.Unmarshal(maybeJson, &size)
				if err == nil {
					err = pty.Setsize(ptmx, &pty.Winsize{
						Rows: uint16(size.Rows),
						Cols: uint16(size.Cols),
					})
					if err == nil {
						scopedLogger.Info().Int("rows", size.Rows).Int("cols", size.Cols).Msg("Set terminal size")
						return
					}
				}
				scopedLogger.Warn().Err(err).Msg("Failed to parse terminal size")
			}
		}
		_, err := ptmx.Write(msg.Data)
		if err != nil {
			scopedLogger.Warn().Err(err).Msg("Failed to write to pty")
		}
	})

	d.OnClose(func() {
		if ptmx != nil {
			ptmx.Close()
		}
		if cmd != nil && cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		scopedLogger.Info().Msg("Terminal channel closed")
	})

	d.OnError(func(err error) {
		scopedLogger.Warn().Err(err).Msg("Terminal channel error")
	})
}
