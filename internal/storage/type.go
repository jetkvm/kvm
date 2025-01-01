package storage

type StorageFileUpload struct {
	AlreadyUploadedBytes int64  `json:"alreadyUploadedBytes"`
	DataChannel          string `json:"dataChannel"`
}
