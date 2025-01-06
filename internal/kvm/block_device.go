package kvm

import (
	"context"
	"errors"
	"log"
	"net"
	"os"
	"time"

	"github.com/jetkvm/kvm/internal/logging"
	"github.com/pojntfx/go-nbd/pkg/client"
	"github.com/pojntfx/go-nbd/pkg/server"
)

type remoteImageBackend struct {
}

func (r remoteImageBackend) ReadAt(p []byte, off int64) (n int, err error) {
	VirtualMediaStateMutex.RLock()
	logging.Logger.Debugf("currentVirtualMediaState is %v", CurrentVirtualMediaState)
	logging.Logger.Debugf("read size: %d, off: %d", len(p), off)
	if CurrentVirtualMediaState == nil {
		return 0, errors.New("image not mounted")
	}
	source := CurrentVirtualMediaState.Source
	mountedImageSize := CurrentVirtualMediaState.Size
	VirtualMediaStateMutex.RUnlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	readLen := int64(len(p))
	if off+readLen > mountedImageSize {
		readLen = mountedImageSize - off
	}
	var data []byte
	if source == WebRTC {
		data, err = WebRTCDiskReader.Read(ctx, off, readLen)
		if err != nil {
			return 0, err
		}
		n = copy(p, data)
		return n, nil
	} else if source == HTTP {
		return HttpRangeReader.ReadAt(p, off)
	} else {
		return 0, errors.New("unknown image source")
	}
}

func (r remoteImageBackend) WriteAt(p []byte, off int64) (n int, err error) {
	return 0, errors.New("not supported")
}

func (r remoteImageBackend) Size() (int64, error) {
	VirtualMediaStateMutex.Lock()
	defer VirtualMediaStateMutex.Unlock()
	if CurrentVirtualMediaState == nil {
		return 0, errors.New("no virtual media state")
	}
	return CurrentVirtualMediaState.Size, nil
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

	// Remove the socket file if it already exists
	if _, err := os.Stat(nbdSocketPath); err == nil {
		if err := os.Remove(nbdSocketPath); err != nil {
			log.Fatalf("Failed to remove existing socket file %s: %v", nbdSocketPath, err)
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
	log.Println("nbd server exited:", err)
}

func (d *NBDDevice) runClientConn() {
	err := client.Connect(d.clientConn, d.dev, &client.Options{
		ExportName: "jetkvm",
		BlockSize:  uint32(4 * 1024),
	})
	log.Println("nbd client exited:", err)
}

func (d *NBDDevice) Close() {
	if d.dev != nil {
		err := client.Disconnect(d.dev)
		if err != nil {
			log.Println("error disconnecting nbd client:", err)
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
