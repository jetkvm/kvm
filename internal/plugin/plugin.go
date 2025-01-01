package plugin

import (
	"fmt"
	"kvm/internal/storage"
	"os"
	"path"

	"github.com/google/uuid"
)

const pluginsFolder = "/userdata/jetkvm/plugins"
const pluginsUploadFolder = pluginsFolder + "/_uploads"

func init() {
	_ = os.MkdirAll(pluginsUploadFolder, 0755)
}

func RpcPluginStartUpload(filename string, size int64) (*storage.StorageFileUpload, error) {
	sanitizedFilename, err := storage.SanitizeFilename(filename)
	if err != nil {
		return nil, err
	}

	filePath := path.Join(pluginsUploadFolder, sanitizedFilename)
	uploadPath := filePath + ".incomplete"

	if _, err := os.Stat(filePath); err == nil {
		return nil, fmt.Errorf("file already exists: %s", sanitizedFilename)
	}

	var alreadyUploadedBytes int64 = 0
	if stat, err := os.Stat(uploadPath); err == nil {
		alreadyUploadedBytes = stat.Size()
	}

	uploadId := "plugin_" + uuid.New().String()
	file, err := os.OpenFile(uploadPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open file for upload: %v", err)
	}

	storage.AddPendingUpload(uploadId, storage.PendingUpload{
		File:                 file,
		Size:                 size,
		AlreadyUploadedBytes: alreadyUploadedBytes,
	})

	return &storage.StorageFileUpload{
		AlreadyUploadedBytes: alreadyUploadedBytes,
		DataChannel:          uploadId,
	}, nil
}
