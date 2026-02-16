package kvm

import (
	"errors"

	halNBD "github.com/jetkvm/kvm/internal/hal/nbd"
)

type remoteImageBackend struct {
}

func (r remoteImageBackend) ReadAt(p []byte, off int64) (n int, err error) {
	virtualMediaStateMutex.RLock()
	logger.Debug().Interface("currentVirtualMediaState", currentVirtualMediaState).Msg("currentVirtualMediaState")
	logger.Debug().Int64("read size", int64(len(p))).Int64("off", off).Msg("read size and off")
	if currentVirtualMediaState == nil {
		virtualMediaStateMutex.RUnlock()
		return 0, errors.New("image not mounted")
	}
	source := currentVirtualMediaState.Source
	reader := httpRangeReader // capture under lock
	virtualMediaStateMutex.RUnlock()

	switch source {
	case HTTP:
		if reader == nil {
			return 0, errors.New("http reader not initialized")
		}
		return reader.ReadAt(p, off)
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

type NBDDevice = halNBD.Device

func NewNBDDevice() *NBDDevice {
	scopedLogger := nbdLogger.With().
		Str("socket_path", halNBD.DefaultSocketPath).
		Str("device_path", halNBD.DefaultDevicePath).
		Logger()
	return halNBD.NewDevice(halNBD.Options{
		Backend: &remoteImageBackend{},
		Logger:  &scopedLogger,
	})
}
