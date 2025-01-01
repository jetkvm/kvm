package storage

import (
	"os"
	"sync"
)

type PendingUpload struct {
	File                 *os.File
	Size                 int64
	AlreadyUploadedBytes int64
}

var pendingUploads = make(map[string]PendingUpload)
var pendingUploadsMutex sync.Mutex

func GetPendingUpload(uploadId string) (PendingUpload, bool) {
	pendingUploadsMutex.Lock()
	defer pendingUploadsMutex.Unlock()
	upload, ok := pendingUploads[uploadId]
	return upload, ok
}

func AddPendingUpload(uploadId string, upload PendingUpload) {
	pendingUploadsMutex.Lock()
	defer pendingUploadsMutex.Unlock()
	pendingUploads[uploadId] = upload
}

func DeletePendingUpload(uploadId string) {
	pendingUploadsMutex.Lock()
	defer pendingUploadsMutex.Unlock()
	delete(pendingUploads, uploadId)
}
