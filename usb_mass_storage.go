package kvm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
	"github.com/psanford/httpreadat"

	"github.com/jetkvm/kvm/resource"
)

func writeFile(path string, data string) error {
	return os.WriteFile(path, []byte(data), 0644)
}

func getMassStorageImage() (string, error) {
	massStorageFunctionPath, err := gadget.GetPath("mass_storage_lun0")
	if err != nil {
		return "", fmt.Errorf("failed to get mass storage path: %w", err)
	}

	imagePath, err := os.ReadFile(path.Join(massStorageFunctionPath, "file"))
	if err != nil {
		return "", fmt.Errorf("failed to get mass storage image path: %w", err)
	}
	return strings.TrimSpace(string(imagePath)), nil
}

func setMassStorageImage(imagePath string) error {
	massStorageFunctionPath, err := gadget.GetPath("mass_storage_lun0")
	if err != nil {
		return fmt.Errorf("failed to get mass storage path: %w", err)
	}

	if err := writeFile(path.Join(massStorageFunctionPath, "file"), imagePath); err != nil {
		return fmt.Errorf("failed to set image path: %w", err)
	}
	return nil
}

func setMassStorageMode(cdrom bool) error {
	mode := "0"
	if cdrom {
		mode = "1"
	}

	err, changed := gadget.OverrideGadgetConfig("mass_storage_lun0", "cdrom", mode)
	if err != nil {
		return fmt.Errorf("failed to set cdrom mode: %w", err)
	}

	if !changed {
		return nil
	}

	return gadget.UpdateGadgetConfig()
}

func mountImage(imagePath string) error {
	err := setMassStorageImage("")
	if err != nil {
		return fmt.Errorf("remove mass storage image error: %w", err)
	}
	err = setMassStorageImage(imagePath)
	if err != nil {
		return fmt.Errorf("set mass storage image error: %w", err)
	}
	err = setMassStorageImage(imagePath)
	if err != nil {
		return fmt.Errorf("set Mass Storage Image Error: %w", err)
	}
	return nil
}

var nbdDevice *NBDDevice

var imagesFolder = "/userdata/jetkvm/images"

func initImagesFolder() error {
	err := os.MkdirAll(imagesFolder, 0755)
	if err != nil {
		return fmt.Errorf("failed to create images folder: %w", err)
	}
	return nil
}

func rpcMountBuiltInImage(filename string) error {
	logger.Info().Str("filename", filename).Msg("Mount Built-In Image")
	if err := initImagesFolder(); err != nil {
		return err
	}

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

func getMassStorageCDROMEnabled() (bool, error) {
	massStorageFunctionPath, err := gadget.GetPath("mass_storage_lun0")
	if err != nil {
		return false, fmt.Errorf("failed to get mass storage path: %w", err)
	}
	data, err := os.ReadFile(path.Join(massStorageFunctionPath, "cdrom"))
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

func rpcCheckMountUrl(url string) (*VirtualMediaUrlInfo, error) {
	return nil, errors.New("not implemented")
}

type VirtualMediaSource string

const (
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

var currentVirtualMediaState *VirtualMediaState
var virtualMediaStateMutex sync.RWMutex

func rpcGetVirtualMediaState() (*VirtualMediaState, error) {
	virtualMediaStateMutex.RLock()
	defer virtualMediaStateMutex.RUnlock()
	return currentVirtualMediaState, nil
}

func rpcUnmountImage() error {
	virtualMediaStateMutex.Lock()
	defer virtualMediaStateMutex.Unlock()
	err := setMassStorageImage("\n")
	if err != nil {
		logger.Warn().Err(err).Msg("Remove Mass Storage Image Error")
	}
	//TODO: check if we still need it
	time.Sleep(500 * time.Millisecond)
	if nbdDevice != nil {
		nbdDevice.Close()
		nbdDevice = nil
	}
	currentVirtualMediaState = nil
	return nil
}

var httpRangeReader *httpreadat.RangeReader

func getInitialVirtualMediaState() (*VirtualMediaState, error) {
	cdromEnabled, err := getMassStorageCDROMEnabled()
	if err != nil {
		return nil, fmt.Errorf("failed to get mass storage cdrom enabled: %w", err)
	}

	diskPath, err := getMassStorageImage()
	if err != nil {
		return nil, fmt.Errorf("failed to get mass storage image: %w", err)
	}

	initialState := &VirtualMediaState{
		Source: Storage,
		Mode:   Disk,
	}

	if cdromEnabled {
		initialState.Mode = CDROM
	}

	switch diskPath {
	case "":
		return nil, nil
	case "/dev/nbd0":
		initialState.Source = HTTP
		initialState.URL = "/"
		initialState.Size = 1
	default:
		initialState.Filename = filepath.Base(diskPath)
		// get size from file
		logger.Info().Str("diskPath", diskPath).Msg("getting file size")
		info, err := os.Stat(diskPath)
		if err != nil {
			return nil, fmt.Errorf("failed to get file info: %w", err)
		}
		initialState.Size = info.Size()
	}

	return initialState, nil
}

func setInitialVirtualMediaState() error {
	virtualMediaStateMutex.Lock()
	defer virtualMediaStateMutex.Unlock()
	initialState, err := getInitialVirtualMediaState()
	if err != nil {
		return fmt.Errorf("failed to get initial virtual media state: %w", err)
	}
	currentVirtualMediaState = initialState

	logger.Info().Interface("initial_virtual_media_state", initialState).Msg("initial virtual media state set")
	return nil
}

func rpcMountWithHTTP(url string, mode VirtualMediaMode) error {
	virtualMediaStateMutex.Lock()
	if currentVirtualMediaState != nil {
		virtualMediaStateMutex.Unlock()
		return fmt.Errorf("another virtual media is already mounted")
	}
	httpRangeReader = httpreadat.New(url)
	n, err := httpRangeReader.Size()
	if err != nil {
		virtualMediaStateMutex.Unlock()
		return fmt.Errorf("failed to use http url: %w", err)
	}
	logger.Info().Str("url", url).Int64("size", n).Msg("using remote url")

	if err := setMassStorageMode(mode == CDROM); err != nil {
		return fmt.Errorf("failed to set mass storage mode: %w", err)
	}

	currentVirtualMediaState = &VirtualMediaState{
		Source: HTTP,
		Mode:   mode,
		URL:    url,
		Size:   n,
	}
	virtualMediaStateMutex.Unlock()

	logger.Debug().Msg("Starting nbd device")
	nbdDevice = NewNBDDevice()
	err = nbdDevice.Start()
	if err != nil {
		logger.Warn().Err(err).Msg("failed to start nbd device")
		return err
	}
	logger.Debug().Msg("nbd device started")
	//TODO: replace by polling on block device having right size
	time.Sleep(1 * time.Second)
	err = setMassStorageImage("/dev/nbd0")
	if err != nil {
		return err
	}
	logger.Info().Msg("usb mass storage mounted")
	return nil
}

func rpcMountWithStorage(filename string, mode VirtualMediaMode) error {
	filename, err := sanitizeFilename(filename)
	if err != nil {
		return err
	}

	virtualMediaStateMutex.Lock()
	defer virtualMediaStateMutex.Unlock()
	if currentVirtualMediaState != nil {
		return fmt.Errorf("another virtual media is already mounted")
	}

	fullPath := filepath.Join(imagesFolder, filename)
	fileInfo, err := os.Stat(fullPath)
	if err != nil {
		return fmt.Errorf("failed to get file info: %w", err)
	}

	if err := setMassStorageMode(mode == CDROM); err != nil {
		return fmt.Errorf("failed to set mass storage mode: %w", err)
	}

	err = setMassStorageImage(fullPath)
	if err != nil {
		return fmt.Errorf("failed to set mass storage image: %w", err)
	}
	currentVirtualMediaState = &VirtualMediaState{
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

func rpcGetStorageSpace() (*StorageSpace, error) {
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

func rpcListStorageFiles() (*StorageFiles, error) {
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

func rpcDeleteStorageFile(filename string) error {
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

const uploadIdPrefix = "upload_"

func rpcStartStorageFileUpload(filename string, size int64) (*StorageFileUpload, error) {
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

	uploadId := uploadIdPrefix + uuid.New().String()
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

func handleUploadChannel(d *webrtc.DataChannel) {
	defer d.Close()
	uploadId := d.Label()
	pendingUploadsMutex.Lock()
	pendingUpload, ok := pendingUploads[uploadId]
	pendingUploadsMutex.Unlock()
	if !ok {
		logger.Warn().Str("uploadId", uploadId).Msg("upload channel opened for unknown upload")
		return
	}
	totalBytesWritten := pendingUpload.AlreadyUploadedBytes
	defer func() {
		pendingUpload.File.Close()
		if totalBytesWritten == pendingUpload.Size {
			newName := strings.TrimSuffix(pendingUpload.File.Name(), ".incomplete")
			err := os.Rename(pendingUpload.File.Name(), newName)
			if err != nil {
				logger.Warn().Err(err).Str("uploadId", uploadId).Msg("failed to rename uploaded file")
			} else {
				logger.Debug().Str("uploadId", uploadId).Str("newName", newName).Msg("successfully renamed uploaded file")
			}
		} else {
			logger.Warn().Str("uploadId", uploadId).Msg("uploaded ended before the complete file received")
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
			logger.Warn().Err(err).Str("uploadId", uploadId).Msg("failed to write to file")
			close(uploadComplete)
			return
		}
		totalBytesWritten += int64(bytesWritten)

		sendProgress := time.Since(lastProgressTime) >= 200*time.Millisecond
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
				logger.Warn().Err(err).Str("uploadId", uploadId).Msg("failed to marshal upload progress")
			} else {
				err = d.SendText(string(progressJSON))
				if err != nil {
					logger.Warn().Err(err).Str("uploadId", uploadId).Msg("failed to send upload progress")
				}
			}
			lastProgressTime = time.Now()
		}
	})

	// Block until upload is complete
	<-uploadComplete
}

func handleUploadHttp(c *gin.Context) {
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
				logger.Warn().Err(err).Str("uploadId", uploadId).Msg("failed to rename uploaded file")
			} else {
				logger.Debug().Str("uploadId", uploadId).Str("newName", newName).Msg("successfully renamed uploaded file")
			}
		} else {
			logger.Warn().Str("uploadId", uploadId).Msg("uploaded ended before the complete file received")
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
			logger.Warn().Err(err).Str("uploadId", uploadId).Msg("failed to read from request body")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read upload data"})
			return
		}

		if n > 0 {
			bytesWritten, err := pendingUpload.File.Write(buffer[:n])
			if err != nil {
				logger.Warn().Err(err).Str("uploadId", uploadId).Msg("failed to write to file")
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

// Download state management
type DownloadState struct {
	Downloading bool    `json:"downloading"`
	Filename    string  `json:"filename,omitempty"`
	URL         string  `json:"url,omitempty"`
	TotalBytes  int64   `json:"totalBytes"`
	DoneBytes   int64   `json:"doneBytes"`
	Progress    float32 `json:"progress"`
	Error       string  `json:"error,omitempty"`
}

var currentDownload *DownloadState
var downloadMutex sync.Mutex
var downloadCancel context.CancelFunc

func rpcGetDownloadState() (*DownloadState, error) {
	downloadMutex.Lock()
	defer downloadMutex.Unlock()
	if currentDownload == nil {
		return &DownloadState{Downloading: false}, nil
	}
	return currentDownload, nil
}

func rpcCancelDownload() error {
	downloadMutex.Lock()
	defer downloadMutex.Unlock()
	if downloadCancel != nil {
		downloadCancel()
		downloadCancel = nil
	}
	return nil
}

func rpcDownloadFromUrl(url string, filename string) error {
	// Sanitize filename
	sanitizedFilename, err := sanitizeFilename(filename)
	if err != nil {
		return err
	}

	// Validate URL
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return errors.New("invalid URL: must start with http:// or https://")
	}

	// Check if already downloading
	downloadMutex.Lock()
	if currentDownload != nil && currentDownload.Downloading {
		downloadMutex.Unlock()
		return errors.New("another download is already in progress")
	}

	// Check if file already exists
	filePath := filepath.Join(imagesFolder, sanitizedFilename)
	if _, err := os.Stat(filePath); err == nil {
		downloadMutex.Unlock()
		return fmt.Errorf("file already exists: %s", sanitizedFilename)
	}

	// Initialize download state
	ctx, cancel := context.WithCancel(context.Background())
	downloadCancel = cancel
	currentDownload = &DownloadState{
		Downloading: true,
		Filename:    sanitizedFilename,
		URL:         url,
		Progress:    0,
	}
	downloadMutex.Unlock()

	// Start download in goroutine
	go performDownload(ctx, url, sanitizedFilename)

	return nil
}

func performDownload(ctx context.Context, url string, filename string) {
	downloadPath := filepath.Join(imagesFolder, filename+".incomplete")
	finalPath := filepath.Join(imagesFolder, filename)

	defer func() {
		downloadMutex.Lock()
		if currentDownload != nil {
			currentDownload.Downloading = false
		}
		downloadCancel = nil
		downloadMutex.Unlock()
		broadcastDownloadState()
	}()

	// Create HTTP request with context
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		setDownloadError(fmt.Sprintf("failed to create request: %v", err))
		return
	}

	// Perform request
	client := &http.Client{Timeout: 0} // No timeout for large downloads
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() == context.Canceled {
			setDownloadError("download cancelled")
			// Clean up incomplete file
			os.Remove(downloadPath)
		} else {
			setDownloadError(fmt.Sprintf("failed to download: %v", err))
		}
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		setDownloadError(fmt.Sprintf("server returned status %d", resp.StatusCode))
		return
	}

	totalSize := resp.ContentLength
	if totalSize <= 0 {
		setDownloadError("server did not provide content length")
		return
	}

	// Update state with total size
	downloadMutex.Lock()
	if currentDownload != nil {
		currentDownload.TotalBytes = totalSize
	}
	downloadMutex.Unlock()
	broadcastDownloadState()

	// Create file
	file, err := os.Create(downloadPath)
	if err != nil {
		setDownloadError(fmt.Sprintf("failed to create file: %v", err))
		return
	}
	defer file.Close()

	// Download with progress tracking
	var written int64
	buf := make([]byte, 32*1024)
	lastProgress := float32(0)

	for {
		select {
		case <-ctx.Done():
			setDownloadError("download cancelled")
			file.Close()
			os.Remove(downloadPath)
			return
		default:
		}

		nr, er := resp.Body.Read(buf)
		if nr > 0 {
			nw, ew := file.Write(buf[0:nr])
			if nw < nr {
				setDownloadError(fmt.Sprintf("short write: %d < %d", nw, nr))
				return
			}
			written += int64(nw)
			if ew != nil {
				setDownloadError(fmt.Sprintf("write error: %v", ew))
				return
			}

			progress := float32(written) / float32(totalSize)
			if progress-lastProgress >= 0.01 {
				downloadMutex.Lock()
				if currentDownload != nil {
					currentDownload.DoneBytes = written
					currentDownload.Progress = progress
				}
				downloadMutex.Unlock()
				broadcastDownloadState()
				lastProgress = progress
			}
		}
		if er != nil {
			if er == io.EOF {
				break
			}
			setDownloadError(fmt.Sprintf("read error: %v", er))
			return
		}
	}

	// Sync filesystem
	if err := file.Sync(); err != nil {
		setDownloadError(fmt.Sprintf("failed to sync file: %v", err))
		return
	}
	file.Close()

	// Rename to final filename
	if err := os.Rename(downloadPath, finalPath); err != nil {
		setDownloadError(fmt.Sprintf("failed to rename file: %v", err))
		return
	}

	// Update final state
	downloadMutex.Lock()
	if currentDownload != nil {
		currentDownload.DoneBytes = totalSize
		currentDownload.Progress = 1.0
	}
	downloadMutex.Unlock()

	logger.Info().Str("filename", filename).Int64("size", totalSize).Msg("download completed")
}

func setDownloadError(errMsg string) {
	downloadMutex.Lock()
	if currentDownload != nil {
		currentDownload.Error = errMsg
	}
	downloadMutex.Unlock()
	logger.Warn().Str("error", errMsg).Msg("download error")
}

func broadcastDownloadState() {
	downloadMutex.Lock()
	state := currentDownload
	downloadMutex.Unlock()

	if currentSession != nil && state != nil {
		writeJSONRPCEvent("downloadState", state, currentSession)
	}
}
