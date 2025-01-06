package kvm

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jetkvm/kvm/internal/logging"
)

type RemoteImageReader interface {
	Read(ctx context.Context, offset int64, size int64) ([]byte, error)
}

type WebRTCDiskReaderStruct struct {
}

var WebRTCDiskReader WebRTCDiskReaderStruct

func (w *WebRTCDiskReaderStruct) Read(ctx context.Context, offset int64, size int64) ([]byte, error) {
	VirtualMediaStateMutex.RLock()
	if CurrentVirtualMediaState == nil {
		VirtualMediaStateMutex.RUnlock()
		return nil, errors.New("image not mounted")
	}
	if CurrentVirtualMediaState.Source != WebRTC {
		VirtualMediaStateMutex.RUnlock()
		return nil, errors.New("image not mounted from webrtc")
	}
	mountedImageSize := CurrentVirtualMediaState.Size
	VirtualMediaStateMutex.RUnlock()
	end := offset + size
	if end > mountedImageSize {
		end = mountedImageSize
	}
	req := DiskReadRequest{
		Start: uint64(offset),
		End:   uint64(end),
	}
	jsonBytes, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	if CurrentSession == nil || CurrentSession.DiskChannel == nil {
		return nil, errors.New("not active session")
	}

	logging.Logger.Debugf("reading from webrtc %v", string(jsonBytes))
	err = CurrentSession.DiskChannel.SendText(string(jsonBytes))
	if err != nil {
		return nil, err
	}
	buf := make([]byte, 0)
	for {
		select {
		case data := <-DiskReadChan:
			buf = data[16:]
		case <-ctx.Done():
			return nil, context.Canceled
		}
		if len(buf) >= int(end-offset) {
			break
		}
	}
	return buf, nil
}
