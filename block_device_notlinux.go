//go:build !linux

package kvm

import (
	"os"
)

func (d *NBDDevice) runClientConn() {
	d.l.Error().Msg("platform not supported")
	os.Exit(1)
}

func (d *NBDDevice) Close() {
	d.l.Error().Msg("platform not supported")
	os.Exit(1)
}
