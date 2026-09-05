package kvm

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jetkvm/kvm/internal/sync"
	"github.com/jetkvm/kvm/resource"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
	"github.com/psanford/httpreadat"
)

// rebindAndRecoverHID performs a corrective USB rebind with recovery poller
// suppression, resets HID file handles, waits for the kernel to re-attach the
// HID function driver, and reopens the keyboard chardev.
func rebindAndRecoverHID(context string) error {
	setUSBRecoveryTimer(time.Now())
	if err := gadget.RebindUsb(true); err != nil {
		return fmt.Errorf("%s: corrective USB rebind failed: %w", context, err)
	}
	gadget.ResetHIDFiles()
	if !tryReopenKeyboard(context, false) {
		usbLogger.Warn().Msgf("keyboard HID file not ready after %s rebind", context)
	}
	setUSBRecoveryTimer(time.Now())
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

	// Suppress the auto-recovery poller BEFORE the rebind so it doesn't see
	// the transient "not attached" UDC state during the transaction's unbind/bind
	// and trigger a competing RebindUsb that corrupts HID chardev state.
	setUSBRecoveryTimer(time.Now())

	if err := gadget.UpdateGadgetConfig(); err != nil {
		return err
	}

	// USB gadget was rebound — HID device nodes were recreated.
	// Reset stale file handles so subsequent HID writes use fresh descriptors.
	gadget.ResetHIDFiles()

	// Give the kernel time to attach the HID function driver to new device nodes.
	time.Sleep(1 * time.Second)

	openErr := gadget.OpenKeyboardHidFile()
	if openErr != nil {
		usbLogger.Warn().Err(openErr).Msg("HID chardev broken after rebind, attempting corrective rebind")
		if err := rebindAndRecoverHID("mass-storage-mode-change"); err != nil {
			return err
		}
	}

	return nil
}

func mountImage(imagePath string) error {
	err := gadget.SetMassStorageImage("")
	if err != nil {
		return fmt.Errorf("remove mass storage image error: %w", err)
	}
	err = gadget.SetMassStorageImage(imagePath)
	if err != nil {
		return fmt.Errorf("set mass storage image error: %w", err)
	}
	err = gadget.SetMassStorageImage(imagePath)
	if err != nil {
		return fmt.Errorf("set Mass Storage Image Error: %w", err)
	}
	return nil
}

var nbdDevice *NBDDevice

const imagesFolder = "/userdata/jetkvm/images"

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

	filename, err := sanitizeFilename(filename)
	if err != nil {
		return fmt.Errorf("invalid filename: %w", err)
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
	Usable bool   `json:"usable"`
	Reason string `json:"reason,omitempty"`
	Size   int64  `json:"size"`
}

func rpcCheckMountUrl(rawURL string) (*VirtualMediaUrlInfo, error) {
	rawURL = strings.TrimSpace(rawURL)
	parsedURL, err := neturl.Parse(rawURL)
	if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		return &VirtualMediaUrlInfo{
			Usable: false,
			Reason: "Enter a valid HTTP or HTTPS image URL.",
		}, nil
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return &VirtualMediaUrlInfo{
			Usable: false,
			Reason: "Only HTTP and HTTPS image URLs can be mounted.",
		}, nil
	}

	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to check url: %w", err)
	}
	req.Header.Set("Range", "bytes=0-0")

	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		logger.Warn().Err(err).Str("url", rawURL).Msg("failed to check virtual media url")
		return &VirtualMediaUrlInfo{
			Usable: false,
			Reason: "The URL is not available. Check the image URL and try again.",
		}, nil
	}
	if err := resp.Body.Close(); err != nil {
		logger.Warn().Err(err).Str("url", rawURL).Msg("failed to close virtual media url check response")
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		status := fmt.Sprintf("HTTP %d", resp.StatusCode)
		if statusText := http.StatusText(resp.StatusCode); statusText != "" {
			status += " " + statusText
		}
		return &VirtualMediaUrlInfo{
			Usable: false,
			Reason: fmt.Sprintf("The URL is not available (%s). Check the image URL and try again.", status),
		}, nil
	}

	if resp.StatusCode != http.StatusPartialContent {
		return &VirtualMediaUrlInfo{
			Usable: false,
			Reason: "The URL is available, but the server does not support byte-range requests.",
		}, nil
	}

	contentRange := strings.TrimSpace(resp.Header.Get("Content-Range"))
	if strings.HasSuffix(contentRange, "/*") {
		return &VirtualMediaUrlInfo{
			Usable: false,
			Reason: "The URL is available, but the server did not report the image size.",
		}, nil
	}

	rangeFields := strings.Fields(contentRange)
	if len(rangeFields) != 2 || strings.ToLower(rangeFields[0]) != "bytes" {
		return &VirtualMediaUrlInfo{
			Usable: false,
			Reason: "The URL is available, but the server returned an unreadable range response.",
		}, nil
	}

	rangeParts := strings.Split(rangeFields[1], "/")
	if len(rangeParts) != 2 || rangeParts[1] == "*" {
		return &VirtualMediaUrlInfo{
			Usable: false,
			Reason: "The URL is available, but the server returned an unreadable range response.",
		}, nil
	}

	size, err := strconv.ParseInt(rangeParts[1], 10, 64)
	if err != nil {
		return &VirtualMediaUrlInfo{
			Usable: false,
			Reason: "The URL is available, but the server returned an unreadable range response.",
		}, nil
	}
	if size <= 0 {
		return &VirtualMediaUrlInfo{
			Usable: false,
			Reason: "The URL points to an empty file.",
		}, nil
	}

	return &VirtualMediaUrlInfo{
		Usable: true,
		Size:   size,
	}, nil
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

func unmountImageLocked() error {
	virtualMediaStateMutex.Lock()
	defer virtualMediaStateMutex.Unlock()

	err := gadget.SetMassStorageImage("\n")
	if err != nil {
		if !errors.Is(err, syscall.EBUSY) {
			return fmt.Errorf("failed to unmount image: %w", err)
		}

		logger.Warn().Err(err).Msg("unmount failed with EBUSY, force-ejecting via soft disconnect")

		if ejectErr := gadget.ForceEjectMassStorageImage(); ejectErr != nil {
			return fmt.Errorf("failed to unmount image: %w, %w", err, ejectErr)
		}
	}

	time.Sleep(500 * time.Millisecond)
	if nbdDevice != nil {
		nbdDevice.Close()
		nbdDevice = nil
	}
	currentVirtualMediaState = nil
	return nil
}

func rpcUnmountImage() error {
	if err := unmountImageLocked(); err != nil {
		return err
	}
	if mqttManager != nil {
		mqttManager.publishVirtualMediaState()
	}
	return nil
}

var httpRangeReader *httpreadat.RangeReader

func getInitialVirtualMediaState() (*VirtualMediaState, error) {
	cdromEnabled, err := getMassStorageCDROMEnabled()
	if err != nil {
		return nil, fmt.Errorf("failed to get mass storage cdrom enabled: %w", err)
	}

	diskPath, err := gadget.GetMassStorageImage()
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

func prepareHTTPMount(url string, mode VirtualMediaMode) error {
	url = strings.TrimSpace(url)

	virtualMediaStateMutex.RLock()
	alreadyMounted := currentVirtualMediaState != nil
	virtualMediaStateMutex.RUnlock()
	if alreadyMounted {
		return fmt.Errorf("another virtual media is already mounted")
	}

	urlInfo, err := rpcCheckMountUrl(url)
	if err != nil {
		return err
	}
	if !urlInfo.Usable {
		return errors.New(urlInfo.Reason)
	}

	virtualMediaStateMutex.Lock()
	defer virtualMediaStateMutex.Unlock()
	if currentVirtualMediaState != nil {
		return fmt.Errorf("another virtual media is already mounted")
	}
	httpRangeReader = httpreadat.New(url)
	logger.Info().Str("url", url).Int64("size", urlInfo.Size).Msg("using remote url")

	if err := setMassStorageMode(mode == CDROM); err != nil {
		return fmt.Errorf("failed to set mass storage mode: %w", err)
	}

	currentVirtualMediaState = &VirtualMediaState{
		Source: HTTP,
		Mode:   mode,
		URL:    url,
		Size:   urlInfo.Size,
	}
	return nil
}

func rpcMountWithHTTP(url string, mode VirtualMediaMode) error {
	if err := prepareHTTPMount(url, mode); err != nil {
		return err
	}

	logger.Debug().Msg("Starting nbd device")
	nbdDevice = NewNBDDevice()
	err := nbdDevice.Start()
	if err != nil {
		logger.Warn().Err(err).Msg("failed to start nbd device")
		return err
	}
	logger.Debug().Msg("nbd device started")
	//TODO: replace by polling on block device having right size
	time.Sleep(1 * time.Second)
	err = gadget.SetMassStorageImage("/dev/nbd0")
	if err != nil {
		return err
	}
	logger.Info().Msg("usb mass storage mounted")
	if mqttManager != nil {
		mqttManager.publishVirtualMediaState()
	}
	return nil
}

func prepareStorageMount(filename string, mode VirtualMediaMode) error {
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

	err = gadget.SetMassStorageImage(fullPath)
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

func rpcMountWithStorage(filename string, mode VirtualMediaMode) error {
	filename, err := sanitizeFilename(filename)
	if err != nil {
		return err
	}

	if err := prepareStorageMount(filename, mode); err != nil {
		return err
	}

	if mqttManager != nil {
		mqttManager.publishVirtualMediaState()
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

	pendingUploadsMutex.Lock()
	defer pendingUploadsMutex.Unlock()

	// A retry after a cancel must not race the transfer it replaces. A data
	// channel closes gracefully and still delivers what it had buffered, so
	// the old handler could keep appending while the new one measures the
	// partial file and appends too. Closing the old file ends that transfer.
	for id, p := range pendingUploads {
		if p.File.Name() != uploadPath {
			continue
		}
		p.writeLock.Lock()
		p.File.Close()
		p.writeLock.Unlock()
		delete(pendingUploads, id)
		logger.Info().Str("uploadId", id).Msg("upload superseded by a new start for the same file")
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
	pendingUploads[uploadId] = pendingUpload{
		File:                 file,
		Size:                 size,
		AlreadyUploadedBytes: alreadyUploadedBytes,
		writeLock:            &sync.Mutex{},
	}
	time.AfterFunc(uploadClaimTimeout, func() { expireUnclaimedUpload(uploadId) })
	return &StorageFileUpload{
		AlreadyUploadedBytes: alreadyUploadedBytes,
		DataChannel:          uploadId,
	}, nil
}

type pendingUpload struct {
	File                 *os.File
	Size                 int64
	AlreadyUploadedBytes int64
	// writeLock serialises the transport's writes with a supersede from
	// rpcStartStorageFileUpload, so nothing lands after the replacement
	// measured the partial file.
	writeLock *sync.Mutex
	claimed   bool
}

// uploadClaimTimeout bounds how long a started upload waits for its
// transport. The UI gives up on the start call after 30 s; an abort in that
// window used to leave the file open and the entry in the map until reboot.
const uploadClaimTimeout = 60 * time.Second

var pendingUploads = make(map[string]pendingUpload)
var pendingUploadsMutex sync.Mutex

// claimPendingUpload hands the upload to its transport.
func claimPendingUpload(uploadId string) (pendingUpload, bool) {
	pendingUploadsMutex.Lock()
	defer pendingUploadsMutex.Unlock()
	p, ok := pendingUploads[uploadId]
	if !ok {
		return pendingUpload{}, false
	}
	p.claimed = true
	pendingUploads[uploadId] = p
	return p, true
}

// expireUnclaimedUpload releases an upload whose transport never arrived.
func expireUnclaimedUpload(uploadId string) {
	pendingUploadsMutex.Lock()
	defer pendingUploadsMutex.Unlock()
	p, ok := pendingUploads[uploadId]
	if !ok || p.claimed {
		return
	}
	p.File.Close()
	delete(pendingUploads, uploadId)
	logger.Warn().Str("uploadId", uploadId).Msg("upload transport never arrived, releasing the upload")
}

func (p pendingUpload) write(data []byte) (int, error) {
	p.writeLock.Lock()
	defer p.writeLock.Unlock()
	return p.File.Write(data)
}

type UploadProgress struct {
	Size                 int64
	AlreadyUploadedBytes int64
}

func handleUploadChannel(d *webrtc.DataChannel) {
	defer d.Close()
	uploadId := d.Label()
	pendingUpload, ok := claimPendingUpload(uploadId)
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
	var finishOnce sync.Once
	finish := func() { finishOnce.Do(func() { close(uploadComplete) }) }
	// A client that cancels closes the channel. Without this the handler
	// blocked forever, keeping the file open and the pending entry alive.
	d.OnClose(finish)
	lastProgressTime := time.Now()
	d.OnMessage(func(msg webrtc.DataChannelMessage) {
		bytesWritten, err := pendingUpload.write(msg.Data)
		if err != nil {
			logger.Warn().Err(err).Str("uploadId", uploadId).Msg("failed to write to file")
			finish()
			return
		}
		totalBytesWritten += int64(bytesWritten)

		sendProgress := time.Since(lastProgressTime) >= 200*time.Millisecond
		if totalBytesWritten >= pendingUpload.Size {
			sendProgress = true
			finish()
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
	pendingUpload, ok := claimPendingUpload(uploadId)
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
			bytesWritten, err := pendingUpload.write(buffer[:n])
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
