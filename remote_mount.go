package kvm

import (
	"context"
	"encoding/json"
	"errors"
)

type RemoteImageReader interface {
	Read(ctx context.Context, offset int64, size int64) ([]byte, error)
}

type WebRTCDiskReader struct {
}

var webRTCDiskReader WebRTCDiskReader

func (w *WebRTCDiskReader) Read(ctx context.Context, offset int64, size int64) ([]byte, error) {
	virtualMediaStateMutex.RLock()
	if currentVirtualMediaState == nil {
		virtualMediaStateMutex.RUnlock()
		return nil, errors.New("image not mounted")
	}
	if currentVirtualMediaState.Source != WebRTC {
		virtualMediaStateMutex.RUnlock()
		return nil, errors.New("image not mounted from webrtc")
	}
	mountedImageSize := currentVirtualMediaState.Size
	virtualMediaStateMutex.RUnlock()
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

	if currentSession == nil || currentSession.DiskChannel == nil {
		return nil, errors.New("not active session")
	}

	logger.Debugf("reading from webrtc %v", string(jsonBytes))
	err = currentSession.DiskChannel.SendText(string(jsonBytes))
	if err != nil {
		return nil, err
	}
	var buf []byte
	for {
		select {
		case data := <-diskReadChan:
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
