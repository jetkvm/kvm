package kvm

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jetkvm/kvm/resource"

	"github.com/gin-gonic/gin"
	"github.com/jetkvm/kvm/internal/logging"

	"github.com/psanford/httpreadat"

	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
)

const massStorageName = "mass_storage.usb0"

var massStorageFunctionPath = path.Join(gadgetPath, "jetkvm", "functions", massStorageName)

func writeFile(path string, data string) error {
	return os.WriteFile(path, []byte(data), 0644)
}

func setMassStorageImage(imagePath string) error {
	err := writeFile(path.Join(massStorageFunctionPath, "lun.0", "file"), imagePath)
	if err != nil {
		return fmt.Errorf("failed to set image path: %w", err)
	}
	return nil
}

func SetMassStorageMode(cdrom bool) error {
	mode := "0"
	if cdrom {
		mode = "1"
	}
	err := writeFile(path.Join(massStorageFunctionPath, "lun.0", "cdrom"), mode)
	if err != nil {
		return fmt.Errorf("failed to set cdrom mode: %w", err)
	}
	return nil
}

func OnDiskMessage(msg webrtc.DataChannelMessage) {
	fmt.Println("Disk Message, len:", len(msg.Data))
	DiskReadChan <- msg.Data
}

func mountImage(imagePath string) error {
	err := setMassStorageImage("")
	if err != nil {
		return fmt.Errorf("Remove Mass Storage Image Error", err)
	}
	err = setMassStorageImage(imagePath)
	if err != nil {
		return fmt.Errorf("Set Mass Storage Image Error", err)
	}
	return nil
}

var nbdDevice *NBDDevice

const imagesFolder = "/userdata/jetkvm/images"

func RPCMountBuiltInImage(filename string) error {
	log.Println("Mount Built-In Image", filename)
	_ = os.MkdirAll(imagesFolder, 0755)
	imagePath := filepath.Join(imagesFolder, filename)

	// Check if the file exists in the imagesFolder
	if _, err := os.Stat(imagePath); err == nil {
		return mountImage(imagePath)
	}

	// If not, try to find it in ResourceFS
	file, err := resource.ResourceFS.Open(filename)
	if err != nil {
		return fmt.Errorf("image %s not found in built-in resources: %w", filename, err)
	}
	defer file.Close()

	// Create the file in imagesFolder
	outFile, err := os.Create(imagePath)
	if err != nil {
		return fmt.Errorf("failed to create image file: %w", err)
	}
	defer outFile.Close()

	// Copy the content
	_, err = io.Copy(outFile, file)
	if err != nil {
		return fmt.Errorf("failed to write image file: %w", err)
	}

	// Mount the newly created image
	return mountImage(imagePath)
}

func GetMassStorageMode() (bool, error) {
	data, err := os.ReadFile(path.Join(massStorageFunctionPath, "lun.0", "cdrom"))
	if err != nil {
		return false, fmt.Errorf("failed to read cdrom mode: %w", err)
	}

	// Trim any whitespace characters. It has a newline at the end
	trimmedData := strings.TrimSpace(string(data))

	return trimmedData == "1", nil
}

type VirtualMediaUrlInfo struct {
	Usable bool
	Reason string //only populated if Usable is false
	Size   int64
}

func RPCCheckMountUrl(url string) (*VirtualMediaUrlInfo, error) {
	return nil, errors.New("not implemented")
}

type VirtualMediaSource string

const (
	WebRTC  VirtualMediaSource = "WebRTC"
	HTTP    VirtualMediaSource = "HTTP"
	Storage VirtualMediaSource = "Storage"
)

type VirtualMediaMode string

const (
	CDROM VirtualMediaMode = "CDROM"
	Disk  VirtualMediaMode = "Disk"
)

type VirtualMediaState struct {
	Source   VirtualMediaSource `json:"source"`
	Mode     VirtualMediaMode   `json:"mode"`
	Filename string             `json:"filename,omitempty"`
	URL      string             `json:"url,omitempty"`
	Size     int64              `json:"size"`
}

var CurrentVirtualMediaState *VirtualMediaState
var VirtualMediaStateMutex sync.RWMutex

func RPCGetVirtualMediaState() (*VirtualMediaState, error) {
	VirtualMediaStateMutex.RLock()
	defer VirtualMediaStateMutex.RUnlock()
	return CurrentVirtualMediaState, nil
}

func RPCUnmountImage() error {
	VirtualMediaStateMutex.Lock()
	defer VirtualMediaStateMutex.Unlock()
	err := setMassStorageImage("\n")
	if err != nil {
		fmt.Println("Remove Mass Storage Image Error", err)
	}
	//TODO: check if we still need it
	time.Sleep(500 * time.Millisecond)
	if nbdDevice != nil {
		nbdDevice.Close()
		nbdDevice = nil
	}
	CurrentVirtualMediaState = nil
	return nil
}

var HttpRangeReader *httpreadat.RangeReader

func RPCMountWithHTTP(url string, mode VirtualMediaMode) error {
	VirtualMediaStateMutex.Lock()
	if CurrentVirtualMediaState != nil {
		VirtualMediaStateMutex.Unlock()
		return fmt.Errorf("another virtual media is already mounted")
	}
	HttpRangeReader = httpreadat.New(url)
	n, err := HttpRangeReader.Size()
	if err != nil {
		VirtualMediaStateMutex.Unlock()
		return fmt.Errorf("failed to use http url: %w", err)
	}
	logging.Logger.Infof("using remote url %s with size %d", url, n)
	CurrentVirtualMediaState = &VirtualMediaState{
		Source: HTTP,
		Mode:   mode,
		URL:    url,
		Size:   n,
	}
	VirtualMediaStateMutex.Unlock()

	logging.Logger.Debug("Starting nbd device")
	nbdDevice = NewNBDDevice()
	err = nbdDevice.Start()
	if err != nil {
		logging.Logger.Errorf("failed to start nbd device: %v", err)
		return err
	}
	logging.Logger.Debug("nbd device started")
	//TODO: replace by polling on block device having right size
	time.Sleep(1 * time.Second)
	err = setMassStorageImage("/dev/nbd0")
	if err != nil {
		return err
	}
	logging.Logger.Info("usb mass storage mounted")
	return nil
}

func RPCMountWithWebRTC(filename string, size int64, mode VirtualMediaMode) error {
	VirtualMediaStateMutex.Lock()
	if CurrentVirtualMediaState != nil {
		VirtualMediaStateMutex.Unlock()
		return fmt.Errorf("another virtual media is already mounted")
	}
	CurrentVirtualMediaState = &VirtualMediaState{
		Source:   WebRTC,
		Mode:     mode,
		Filename: filename,
		Size:     size,
	}
	VirtualMediaStateMutex.Unlock()
	logging.Logger.Debugf("currentVirtualMediaState is %v", CurrentVirtualMediaState)
	logging.Logger.Debug("Starting nbd device")
	nbdDevice = NewNBDDevice()
	err := nbdDevice.Start()
	if err != nil {
		logging.Logger.Errorf("failed to start nbd device: %v", err)
		return err
	}
	logging.Logger.Debug("nbd device started")
	//TODO: replace by polling on block device having right size
	time.Sleep(1 * time.Second)
	err = setMassStorageImage("/dev/nbd0")
	if err != nil {
		return err
	}
	logging.Logger.Info("usb mass storage mounted")
	return nil
}

func RPCMountWithStorage(filename string, mode VirtualMediaMode) error {
	filename, err := sanitizeFilename(filename)
	if err != nil {
		return err
	}

	VirtualMediaStateMutex.Lock()
	defer VirtualMediaStateMutex.Unlock()
	if CurrentVirtualMediaState != nil {
		return fmt.Errorf("another virtual media is already mounted")
	}

	fullPath := filepath.Join(imagesFolder, filename)
	fileInfo, err := os.Stat(fullPath)
	if err != nil {
		return fmt.Errorf("failed to get file info: %w", err)
	}

	err = setMassStorageImage(fullPath)
	if err != nil {
		return fmt.Errorf("failed to set mass storage image: %w", err)
	}
	CurrentVirtualMediaState = &VirtualMediaState{
		Source:   Storage,
		Mode:     mode,
		Filename: filename,
		Size:     fileInfo.Size(),
	}
	return nil
}

type StorageSpace struct {
	BytesUsed int64 `json:"bytesUsed"`
	BytesFree int64 `json:"bytesFree"`
}

func RPCGetStorageSpace() (*StorageSpace, error) {
	var stat syscall.Statfs_t
	err := syscall.Statfs(imagesFolder, &stat)
	if err != nil {
		return nil, fmt.Errorf("failed to get storage stats: %v", err)
	}

	totalSpace := stat.Blocks * uint64(stat.Bsize)
	freeSpace := stat.Bfree * uint64(stat.Bsize)
	usedSpace := totalSpace - freeSpace

	return &StorageSpace{
		BytesUsed: int64(usedSpace),
		BytesFree: int64(freeSpace),
	}, nil
}

type StorageFile struct {
	Filename  string    `json:"filename"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"createdAt"`
}

type StorageFiles struct {
	Files []StorageFile `json:"files"`
}

func RPCListStorageFiles() (*StorageFiles, error) {
	files, err := os.ReadDir(imagesFolder)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %v", err)
	}

	storageFiles := make([]StorageFile, 0)
	for _, file := range files {
		if file.IsDir() {
			continue
		}

		info, err := file.Info()
		if err != nil {
			return nil, fmt.Errorf("failed to get file info: %v", err)
		}

		storageFiles = append(storageFiles, StorageFile{
			Filename:  file.Name(),
			Size:      info.Size(),
			CreatedAt: info.ModTime(),
		})
	}

	return &StorageFiles{Files: storageFiles}, nil
}

func sanitizeFilename(filename string) (string, error) {
	cleanPath := filepath.Clean(filename)
	if filepath.IsAbs(cleanPath) || strings.Contains(cleanPath, "..") {
		return "", errors.New("invalid filename")
	}
	sanitized := filepath.Base(cleanPath)
	if sanitized == "." || sanitized == string(filepath.Separator) {
		return "", errors.New("invalid filename")
	}
	return sanitized, nil
}

func RPCDeleteStorageFile(filename string) error {
	sanitizedFilename, err := sanitizeFilename(filename)
	if err != nil {
		return err
	}

	fullPath := filepath.Join(imagesFolder, sanitizedFilename)

	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		return fmt.Errorf("file does not exist: %s", filename)
	}

	err = os.Remove(fullPath)
	if err != nil {
		return fmt.Errorf("failed to delete file: %v", err)
	}

	return nil
}

type StorageFileUpload struct {
	AlreadyUploadedBytes int64  `json:"alreadyUploadedBytes"`
	DataChannel          string `json:"dataChannel"`
}

const UploadIdPrefix = "upload_"

func RPCStartStorageFileUpload(filename string, size int64) (*StorageFileUpload, error) {
	sanitizedFilename, err := sanitizeFilename(filename)
	if err != nil {
		return nil, err
	}

	filePath := path.Join(imagesFolder, sanitizedFilename)
	uploadPath := filePath + ".incomplete"

	if _, err := os.Stat(filePath); err == nil {
		return nil, fmt.Errorf("file already exists: %s", sanitizedFilename)
	}

	var alreadyUploadedBytes int64 = 0
	if stat, err := os.Stat(uploadPath); err == nil {
		alreadyUploadedBytes = stat.Size()
	}

	uploadId := UploadIdPrefix + uuid.New().String()
	file, err := os.OpenFile(uploadPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open file for upload: %v", err)
	}
	pendingUploadsMutex.Lock()
	pendingUploads[uploadId] = pendingUpload{
		File:                 file,
		Size:                 size,
		AlreadyUploadedBytes: alreadyUploadedBytes,
	}
	pendingUploadsMutex.Unlock()
	return &StorageFileUpload{
		AlreadyUploadedBytes: alreadyUploadedBytes,
		DataChannel:          uploadId,
	}, nil
}

type pendingUpload struct {
	File                 *os.File
	Size                 int64
	AlreadyUploadedBytes int64
}

var pendingUploads = make(map[string]pendingUpload)
var pendingUploadsMutex sync.Mutex

type UploadProgress struct {
	Size                 int64
	AlreadyUploadedBytes int64
}

func HandleUploadChannel(d *webrtc.DataChannel) {
	defer d.Close()
	uploadId := d.Label()
	pendingUploadsMutex.Lock()
	pendingUpload, ok := pendingUploads[uploadId]
	pendingUploadsMutex.Unlock()
	if !ok {
		logging.Logger.Warnf("upload channel opened for unknown upload: %s", uploadId)
		return
	}
	totalBytesWritten := pendingUpload.AlreadyUploadedBytes
	defer func() {
		pendingUpload.File.Close()
		if totalBytesWritten == pendingUpload.Size {
			newName := strings.TrimSuffix(pendingUpload.File.Name(), ".incomplete")
			err := os.Rename(pendingUpload.File.Name(), newName)
			if err != nil {
				logging.Logger.Errorf("failed to rename uploaded file: %v", err)
			} else {
				logging.Logger.Debugf("successfully renamed uploaded file to: %s", newName)
			}
		} else {
			logging.Logger.Warnf("uploaded ended before the complete file received")
		}
		pendingUploadsMutex.Lock()
		delete(pendingUploads, uploadId)
		pendingUploadsMutex.Unlock()
	}()
	uploadComplete := make(chan struct{})
	lastProgressTime := time.Now()
	d.OnMessage(func(msg webrtc.DataChannelMessage) {
		bytesWritten, err := pendingUpload.File.Write(msg.Data)
		if err != nil {
			logging.Logger.Errorf("failed to write to file: %v", err)
			close(uploadComplete)
			return
		}
		totalBytesWritten += int64(bytesWritten)

		sendProgress := false
		if time.Since(lastProgressTime) >= 200*time.Millisecond {
			sendProgress = true
		}
		if totalBytesWritten >= pendingUpload.Size {
			sendProgress = true
			close(uploadComplete)
		}

		if sendProgress {
			progress := UploadProgress{
				Size:                 pendingUpload.Size,
				AlreadyUploadedBytes: totalBytesWritten,
			}
			progressJSON, err := json.Marshal(progress)
			if err != nil {
				logging.Logger.Errorf("failed to marshal upload progress: %v", err)
			} else {
				err = d.SendText(string(progressJSON))
				if err != nil {
					logging.Logger.Errorf("failed to send upload progress: %v", err)
				}
			}
			lastProgressTime = time.Now()
		}
	})

	// Block until upload is complete
	<-uploadComplete
}

func HandleUploadHttp(c *gin.Context) {
	uploadId := c.Query("uploadId")
	pendingUploadsMutex.Lock()
	pendingUpload, ok := pendingUploads[uploadId]
	pendingUploadsMutex.Unlock()
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "Upload not found"})
		return
	}

	totalBytesWritten := pendingUpload.AlreadyUploadedBytes
	defer func() {
		pendingUpload.File.Close()
		if totalBytesWritten == pendingUpload.Size {
			newName := strings.TrimSuffix(pendingUpload.File.Name(), ".incomplete")
			err := os.Rename(pendingUpload.File.Name(), newName)
			if err != nil {
				logging.Logger.Errorf("failed to rename uploaded file: %v", err)
			} else {
				logging.Logger.Debugf("successfully renamed uploaded file to: %s", newName)
			}
		} else {
			logging.Logger.Warnf("uploaded ended before the complete file received")
		}
		pendingUploadsMutex.Lock()
		delete(pendingUploads, uploadId)
		pendingUploadsMutex.Unlock()
	}()

	reader := c.Request.Body
	buffer := make([]byte, 32*1024)
	for {
		n, err := reader.Read(buffer)
		if err != nil && err != io.EOF {
			logging.Logger.Errorf("failed to read from request body: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read upload data"})
			return
		}

		if n > 0 {
			bytesWritten, err := pendingUpload.File.Write(buffer[:n])
			if err != nil {
				logging.Logger.Errorf("failed to write to file: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to write upload data"})
				return
			}
			totalBytesWritten += int64(bytesWritten)
		}

		if err == io.EOF {
			break
		}
	}

	c.JSON(http.StatusOK, gin.H{"message": "Upload completed"})
}
