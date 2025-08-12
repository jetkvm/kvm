//go:build linux

package kvm

import (
	"github.com/pojntfx/go-nbd/pkg/client"
)

func (d *NBDDevice) runClientConn() {
	err := client.Connect(d.clientConn, d.dev, &client.Options{
		ExportName: "jetkvm",
		BlockSize:  uint32(4 * 1024),
	})
	d.l.Info().Err(err).Msg("nbd client exited")
}

func (d *NBDDevice) Close() {
	if d.dev != nil {
		err := client.Disconnect(d.dev)
		if err != nil {
			d.l.Warn().Err(err).Msg("error disconnecting nbd client")
		}
		_ = d.dev.Close()
	}
	if d.listener != nil {
		_ = d.listener.Close()
	}
	if d.clientConn != nil {
		_ = d.clientConn.Close()
	}
	if d.serverConn != nil {
		_ = d.serverConn.Close()
	}
}
