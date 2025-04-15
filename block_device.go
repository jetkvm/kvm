package kvm

import (
	"context"
	"errors"
	"net"
	"os"
	"time"

	"github.com/pojntfx/go-nbd/pkg/server"
	"github.com/rs/zerolog"
)

type remoteImageBackend struct {
}

func (r remoteImageBackend) ReadAt(p []byte, off int64) (n int, err error) {
	virtualMediaStateMutex.RLock()
	logger.Debug().Interface("currentVirtualMediaState", currentVirtualMediaState).Msg("currentVirtualMediaState")
	logger.Debug().Int64("read size", int64(len(p))).Int64("off", off).Msg("read size and off")
	if currentVirtualMediaState == nil {
		return 0, errors.New("image not mounted")
	}
	source := currentVirtualMediaState.Source
	mountedImageSize := currentVirtualMediaState.Size
	virtualMediaStateMutex.RUnlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	readLen := int64(len(p))
	if off+readLen > mountedImageSize {
		readLen = mountedImageSize - off
	}
	var data []byte
	switch source {
	case WebRTC:
		data, err = webRTCDiskReader.Read(ctx, off, readLen)
		if err != nil {
			return 0, err
		}
		n = copy(p, data)
		return n, nil
	case HTTP:
		return httpRangeReader.ReadAt(p, off)
	default:
		return 0, errors.New("unknown image source")
	}
}

func (r remoteImageBackend) WriteAt(p []byte, off int64) (n int, err error) {
	return 0, errors.New("not supported")
}

func (r remoteImageBackend) Size() (int64, error) {
	virtualMediaStateMutex.Lock()
	defer virtualMediaStateMutex.Unlock()
	if currentVirtualMediaState == nil {
		return 0, errors.New("no virtual media state")
	}
	return currentVirtualMediaState.Size, nil
}

func (r remoteImageBackend) Sync() error {
	return nil
}

const nbdSocketPath = "/var/run/nbd.socket"
const nbdDevicePath = "/dev/nbd0"

type NBDDevice struct {
	listener   net.Listener
	serverConn net.Conn
	clientConn net.Conn
	dev        *os.File

	l *zerolog.Logger
}

func NewNBDDevice() *NBDDevice {
	return &NBDDevice{}
}

func (d *NBDDevice) Start() error {
	var err error

	if _, err := os.Stat(nbdDevicePath); os.IsNotExist(err) {
		return errors.New("NBD device does not exist")
	}

	d.dev, err = os.Open(nbdDevicePath)
	if err != nil {
		return err
	}

	if d.l == nil {
		scopedLogger := nbdLogger.With().
			Str("socket_path", nbdSocketPath).
			Str("device_path", nbdDevicePath).
			Logger()
		d.l = &scopedLogger
	}

	// Remove the socket file if it already exists
	if _, err := os.Stat(nbdSocketPath); err == nil {
		if err := os.Remove(nbdSocketPath); err != nil {
			d.l.Error().Err(err).Msg("failed to remove existing socket file")
			os.Exit(1)
		}
	}

	d.listener, err = net.Listen("unix", nbdSocketPath)
	if err != nil {
		return err
	}

	d.clientConn, err = net.Dial("unix", nbdSocketPath)
	if err != nil {
		return err
	}

	d.serverConn, err = d.listener.Accept()
	if err != nil {
		return err
	}
	go d.runServerConn()
	go d.runClientConn()
	return nil
}

func (d *NBDDevice) runServerConn() {
	err := server.Handle(
		d.serverConn,
		[]*server.Export{
			{
				Name:        "jetkvm",
				Description: "",
				Backend:     &remoteImageBackend{},
			},
		},
		&server.Options{
			ReadOnly:           true,
			MinimumBlockSize:   uint32(1024),
			PreferredBlockSize: uint32(4 * 1024),
			MaximumBlockSize:   uint32(16 * 1024),
			SupportsMultiConn:  false,
		})

	d.l.Info().Err(err).Msg("nbd server exited")
}
