package server

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"

	"github.com/creack/pty"
	"github.com/jetkvm/kvm/internal/logging"
	"github.com/pion/webrtc/v4"
)

type TerminalSize struct {
	Rows int `json:"rows"`
	Cols int `json:"cols"`
}

func HandleTerminalChannel(d *webrtc.DataChannel) {
	var ptmx *os.File
	var cmd *exec.Cmd
	d.OnOpen(func() {
		cmd = exec.Command("/bin/sh")
		var err error
		ptmx, err = pty.Start(cmd)
		if err != nil {
			logging.Logger.Errorf("Failed to start pty: %v", err)
			d.Close()
			return
		}

		go func() {
			buf := make([]byte, 1024)
			for {
				n, err := ptmx.Read(buf)
				if err != nil {
					if err != io.EOF {
						logging.Logger.Errorf("Failed to read from pty: %v", err)
					}
					break
				}
				err = d.Send(buf[:n])
				if err != nil {
					logging.Logger.Errorf("Failed to send pty output: %v", err)
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
			var size TerminalSize
			err := json.Unmarshal([]byte(msg.Data), &size)
			if err == nil {
				pty.Setsize(ptmx, &pty.Winsize{
					Rows: uint16(size.Rows),
					Cols: uint16(size.Cols),
				})
				return
			}
			logging.Logger.Errorf("Failed to parse terminal size: %v", err)
		}
		_, err := ptmx.Write(msg.Data)
		if err != nil {
			logging.Logger.Errorf("Failed to write to pty: %v", err)
		}
	})

	d.OnClose(func() {
		if ptmx != nil {
			ptmx.Close()
		}
		if cmd != nil && cmd.Process != nil {
			cmd.Process.Kill()
		}
	})
}
