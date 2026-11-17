package kvm

import (
	"errors"
	"fmt"
	"net/http"
	"path/filepath"

	"github.com/jetkvm/kvm/internal/imagedownload"
)

var imageDownloader = &imagedownload.Downloader{
	Dir: imagesFolder,
	Client: func() *http.Client {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.Proxy = config.NetworkConfig.GetTransportProxyFunc()
		return imagedownload.NewClient(transport)
	},
	FreeSpace: func() (int64, error) {
		space, err := rpcGetStorageSpace()
		if err != nil {
			return 0, err
		}
		return space.BytesFree, nil
	},
	OnChange: func(state imagedownload.State) {
		if state.Status != imagedownload.StatusDownloading {
			logger.Info().
				Str("filename", state.Filename).
				Str("status", string(state.Status)).
				Int64("received", state.Received).
				Str("error", state.Error).
				Msg("storage download ended")
		}
		if session := currentSession; session != nil {
			writeJSONRPCEvent("storageDownloadState", state, session)
		}
	},
}

func errIfDownloading(filename string) error {
	active := imageDownloader.ActiveFilename()
	if active != "" && filename == active+imagedownload.IncompleteSuffix {
		return errors.New("this file is being downloaded; cancel the download first")
	}
	return nil
}

func rpcStartStorageDownload(url string, filename string) (*imagedownload.State, error) {
	if err := initImagesFolder(); err != nil {
		return nil, err
	}
	filename, err := imagedownload.ResolveFilename(url, filename)
	if err != nil {
		return nil, err
	}
	partial := filename + imagedownload.IncompleteSuffix

	virtualMediaStateMutex.RLock()
	mounted := currentVirtualMediaState != nil && currentVirtualMediaState.Filename == partial
	virtualMediaStateMutex.RUnlock()
	if mounted {
		return nil, fmt.Errorf("%s is mounted; unmount it first", partial)
	}

	pendingUploadsMutex.Lock()
	defer pendingUploadsMutex.Unlock()
	for _, p := range pendingUploads {
		if p.File.Name() == filepath.Join(imagesFolder, partial) {
			return nil, errors.New("an upload of this file is in progress")
		}
	}

	state, err := imageDownloader.Start(url, filename)
	if err != nil {
		return nil, err
	}
	logger.Info().Str("filename", filename).Msg("storage download started")
	return &state, nil
}

func rpcGetStorageDownloadState() (*imagedownload.State, error) {
	state := imageDownloader.State()
	return &state, nil
}

func rpcCancelStorageDownload() error {
	imageDownloader.Cancel()
	return nil
}
