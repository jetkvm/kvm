//go:build linux

// TODO: use a generator to generate the cgo code for the native functions
// there's too much boilerplate code to write manually

package native

import (
	"fmt"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/rs/zerolog"
)

/*
#cgo LDFLAGS: -Lcgo/lib -ljknative -lopenh264_static -lrga_static -llvgl -lstdc++
#cgo CFLAGS: -Icgo -Icgo/include
#include "ctrl.h"
#include <stdlib.h>
#include <errno.h>

typedef const char cchar_t;
typedef const uint8_t cuint8_t;

extern void jetkvm_go_log_handler(int level, cchar_t *filename, cchar_t *funcname, int line, cchar_t *message);
static inline void jetkvm_cgo_setup_log_handler() {
    jetkvm_set_log_handler(&jetkvm_go_log_handler);
}

extern void jetkvm_go_video_state_handler(jetkvm_video_state_t *state);
static inline void jetkvm_cgo_setup_video_state_handler() {
    // Cast required because Go export doesn't support volatile qualifier
    jetkvm_set_video_state_handler((jetkvm_video_state_handler_t *)&jetkvm_go_video_state_handler);
}

extern void jetkvm_go_video_handler(cuint8_t *frame, ssize_t len);
static inline void jetkvm_cgo_setup_video_handler() {
    jetkvm_set_video_handler(&jetkvm_go_video_handler);
}

extern void jetkvm_go_jpeg_handler(cuint8_t *frame, ssize_t len);
static inline void jetkvm_cgo_setup_jpeg_handler() {
    jetkvm_set_jpeg_handler(&jetkvm_go_jpeg_handler);
}

extern void jetkvm_go_rgb_handler(cuint8_t *frame, ssize_t len, uint32_t width, uint32_t height);
static inline void jetkvm_cgo_setup_rgb_handler() {
    jetkvm_set_rgb_handler(&jetkvm_go_rgb_handler);
}

extern void jetkvm_go_indev_handler(int code);
static inline void jetkvm_cgo_setup_indev_handler() {
    jetkvm_set_indev_handler(&jetkvm_go_indev_handler);
}

extern void jetkvm_go_rpc_handler(cchar_t *method, cchar_t *params);
static inline void jetkvm_cgo_setup_rpc_handler() {
    jetkvm_set_rpc_handler(&jetkvm_go_rpc_handler);
}

// Transcoder output callback - declared here for CGO export
extern void goTranscodeOutputCallback(uint8_t *jpeg_data, size_t jpeg_len, void *user_data);
*/
import "C"

var (
	cgoLock sync.Mutex
)

//export jetkvm_go_video_state_handler
func jetkvm_go_video_state_handler(state *C.jetkvm_video_state_t) {
	videoState := VideoState{
		Ready:          bool(state.ready),
		Streaming:      VideoStreamingStatus(state.streaming),
		Error:          C.GoString(state.error),
		Width:          int(state.width),
		Height:         int(state.height),
		FramePerSecond: float64(state.frame_per_second),
	}
	videoStateChan <- videoState
}

//export jetkvm_go_log_handler
func jetkvm_go_log_handler(level C.int, filename *C.cchar_t, funcname *C.cchar_t, line C.int, message *C.cchar_t) {
	logMessage := nativeLogMessage{
		Level:    zerolog.Level(level),
		Message:  C.GoString(message),
		File:     C.GoString(filename),
		FuncName: C.GoString(funcname),
		Line:     int(line),
	}

	logChan <- logMessage
}

//export jetkvm_go_video_handler
func jetkvm_go_video_handler(frame *C.cuint8_t, len C.ssize_t) {
	videoFrameChan <- C.GoBytes(unsafe.Pointer(frame), C.int(len))
}

//export jetkvm_go_jpeg_handler
func jetkvm_go_jpeg_handler(frame *C.cuint8_t, len C.ssize_t) {
	select {
	case jpegFrameChan <- C.GoBytes(unsafe.Pointer(frame), C.int(len)):
	default:
		// Drop frame if channel is full (non-blocking)
	}
}

//export jetkvm_go_rgb_handler
func jetkvm_go_rgb_handler(frame *C.cuint8_t, len C.ssize_t, width C.uint32_t, height C.uint32_t) {
	// Try to acquire a buffer from the pool BEFORE copying data.
	// This prevents OOM from allocating 8MB per frame at 60fps.
	buf := rgbFrameBufferPool.acquire()
	if buf == nil {
		// All buffers in use - drop frame to prevent memory exhaustion
		return
	}

	// Ensure buffer is large enough (pool is sized for 1080p, but handle edge cases)
	frameLen := int(len)
	if cap(buf) < frameLen {
		// Shouldn't happen with properly sized pool, but handle gracefully
		rgbFrameBufferPool.release(buf)
		return
	}

	// Copy frame data from C memory into pooled Go buffer
	// This is the only allocation-free way to copy from CGO
	cSlice := unsafe.Slice((*byte)(unsafe.Pointer(frame)), frameLen)
	copy(buf[:frameLen], cSlice)

	// Determine format based on frame size:
	// - BGRX = 4 bytes/pixel, so len = width * height * 4
	// - YUV422 = 2 bytes/pixel, so len = width * height * 2
	expectedBGRXSize := uint32(width) * uint32(height) * 4
	format := RGBFrameFormatYUV422
	if uint32(len) == expectedBGRXSize {
		format = RGBFrameFormatBGRX
	}

	select {
	case rgbFrameChan <- RGBFrame{
		Data:   buf[:frameLen],
		Width:  uint32(width),
		Height: uint32(height),
		Format: format,
		pooled: true, // Mark as pooled so Release() returns it
	}:
	default:
		// Channel full - return buffer to pool and drop frame
		rgbFrameBufferPool.release(buf)
	}
}

//export jetkvm_go_indev_handler
func jetkvm_go_indev_handler(code C.int) {
	indevEventChan <- int(code)
}

//export jetkvm_go_rpc_handler
func jetkvm_go_rpc_handler(method *C.cchar_t, params *C.cchar_t) {
	rpcEventChan <- C.GoString(method)
}

var eventCodeToNameMap = map[int]string{}

func uiEventCodeToName(code int) string {
	name, ok := eventCodeToNameMap[code]
	if !ok {
		cCode := C.int(code)
		cName := C.jetkvm_ui_event_code_to_name(cCode)
		name = C.GoString(cName)
		eventCodeToNameMap[code] = name
	}

	return name
}

func setUpNativeHandlers() {
	cgoLock.Lock()
	defer cgoLock.Unlock()

	C.jetkvm_cgo_setup_log_handler()
	C.jetkvm_cgo_setup_video_state_handler()
	C.jetkvm_cgo_setup_video_handler()
	C.jetkvm_cgo_setup_jpeg_handler()
	C.jetkvm_cgo_setup_rgb_handler()
	C.jetkvm_cgo_setup_indev_handler()
	C.jetkvm_cgo_setup_rpc_handler()
}

func uiInit(rotation uint16) {
	cgoLock.Lock()
	defer cgoLock.Unlock()

	cRotation := C.u_int16_t(rotation)

	C.jetkvm_ui_init(cRotation)
}

func uiTick() {
	cgoLock.Lock()
	defer cgoLock.Unlock()

	C.jetkvm_ui_tick()
}

func videoInit(factor float64) error {
	cgoLock.Lock()
	defer cgoLock.Unlock()

	factorC := C.float(factor)

	ret := C.jetkvm_video_init(factorC)
	if ret != 0 {
		return fmt.Errorf("failed to initialize video: %d", ret)
	}
	return nil
}

func videoShutdown() {
	cgoLock.Lock()
	defer cgoLock.Unlock()

	C.jetkvm_video_shutdown()
}

func videoStart() {
	cgoLock.Lock()
	defer cgoLock.Unlock()

	C.jetkvm_video_start()
}

func videoStop() {
	cgoLock.Lock()
	defer cgoLock.Unlock()

	C.jetkvm_video_stop()
}

func videoGetStreamingStatus() VideoStreamingStatus {
	cgoLock.Lock()
	defer cgoLock.Unlock()

	isStreaming := C.jetkvm_video_get_streaming_status()

	return VideoStreamingStatus(isStreaming)
}

func videoLogStatus() string {
	cgoLock.Lock()
	defer cgoLock.Unlock()

	logStatus := C.jetkvm_video_log_status()
	defer C.free(unsafe.Pointer(logStatus))

	return C.GoString(logStatus)
}

func uiSetVar(name string, value string) {
	cgoLock.Lock()
	defer cgoLock.Unlock()

	nameCStr := C.CString(name)
	defer C.free(unsafe.Pointer(nameCStr))

	valueCStr := C.CString(value)
	defer C.free(unsafe.Pointer(valueCStr))

	C.jetkvm_ui_set_var(nameCStr, valueCStr)
}

func uiGetVar(name string) string {
	cgoLock.Lock()
	defer cgoLock.Unlock()

	nameCStr := C.CString(name)
	defer C.free(unsafe.Pointer(nameCStr))

	return C.GoString(C.jetkvm_ui_get_var(nameCStr))
}

func uiSwitchToScreen(screen string) {
	cgoLock.Lock()
	defer cgoLock.Unlock()

	screenCStr := C.CString(screen)
	defer C.free(unsafe.Pointer(screenCStr))
	C.jetkvm_ui_load_screen(screenCStr)
}

func uiGetCurrentScreen() string {
	cgoLock.Lock()
	defer cgoLock.Unlock()

	screenCStr := C.jetkvm_ui_get_current_screen()
	return C.GoString(screenCStr)
}

func uiObjAddState(objName string, state string) (bool, error) {
	cgoLock.Lock()
	defer cgoLock.Unlock()

	objNameCStr := C.CString(objName)
	defer C.free(unsafe.Pointer(objNameCStr))
	stateCStr := C.CString(state)
	defer C.free(unsafe.Pointer(stateCStr))
	C.jetkvm_ui_add_state(objNameCStr, stateCStr)
	return true, nil
}

func uiObjClearState(objName string, state string) (bool, error) {
	cgoLock.Lock()
	defer cgoLock.Unlock()

	objNameCStr := C.CString(objName)
	defer C.free(unsafe.Pointer(objNameCStr))
	stateCStr := C.CString(state)
	defer C.free(unsafe.Pointer(stateCStr))
	C.jetkvm_ui_clear_state(objNameCStr, stateCStr)
	return true, nil
}

func uiGetLVGLVersion() string {
	cgoLock.Lock()
	defer cgoLock.Unlock()

	return C.GoString(C.jetkvm_ui_get_lvgl_version())
}

// TODO: use Enum instead of string but it's not a hot path and performance is not a concern now
func uiObjAddFlag(objName string, flag string) (bool, error) {
	cgoLock.Lock()
	defer cgoLock.Unlock()

	objNameCStr := C.CString(objName)
	defer C.free(unsafe.Pointer(objNameCStr))
	flagCStr := C.CString(flag)
	defer C.free(unsafe.Pointer(flagCStr))
	C.jetkvm_ui_add_flag(objNameCStr, flagCStr)
	return true, nil
}

func uiObjClearFlag(objName string, flag string) (bool, error) {
	cgoLock.Lock()
	defer cgoLock.Unlock()

	objNameCStr := C.CString(objName)
	defer C.free(unsafe.Pointer(objNameCStr))
	flagCStr := C.CString(flag)
	defer C.free(unsafe.Pointer(flagCStr))
	C.jetkvm_ui_clear_flag(objNameCStr, flagCStr)
	return true, nil
}

func uiObjHide(objName string) (bool, error) {
	return uiObjAddFlag(objName, "LV_OBJ_FLAG_HIDDEN")
}

func uiObjShow(objName string) (bool, error) {
	return uiObjClearFlag(objName, "LV_OBJ_FLAG_HIDDEN")
}

func uiObjSetOpacity(objName string, opacity int) (bool, error) {
	cgoLock.Lock()
	defer cgoLock.Unlock()

	objNameCStr := C.CString(objName)
	defer C.free(unsafe.Pointer(objNameCStr))

	C.jetkvm_ui_set_opacity(objNameCStr, C.u_int8_t(opacity))
	return true, nil
}

func uiObjFadeIn(objName string, duration uint32) (bool, error) {
	cgoLock.Lock()
	defer cgoLock.Unlock()

	objNameCStr := C.CString(objName)
	defer C.free(unsafe.Pointer(objNameCStr))

	C.jetkvm_ui_fade_in(objNameCStr, C.u_int32_t(duration))

	return true, nil
}

func uiObjFadeOut(objName string, duration uint32) (bool, error) {
	cgoLock.Lock()
	defer cgoLock.Unlock()

	objNameCStr := C.CString(objName)
	defer C.free(unsafe.Pointer(objNameCStr))

	C.jetkvm_ui_fade_out(objNameCStr, C.u_int32_t(duration))

	return true, nil
}

func uiLabelSetText(objName string, text string) (bool, error) {
	cgoLock.Lock()
	defer cgoLock.Unlock()

	objNameCStr := C.CString(objName)
	defer C.free(unsafe.Pointer(objNameCStr))

	textCStr := C.CString(text)
	defer C.free(unsafe.Pointer(textCStr))

	ret := C.jetkvm_ui_set_text(objNameCStr, textCStr)
	if ret < 0 {
		return false, fmt.Errorf("failed to set text: %d", ret)
	}
	return ret == 0, nil
}

func uiImgSetSrc(objName string, src string) (bool, error) {
	cgoLock.Lock()
	defer cgoLock.Unlock()

	objNameCStr := C.CString(objName)
	defer C.free(unsafe.Pointer(objNameCStr))

	srcCStr := C.CString(src)
	defer C.free(unsafe.Pointer(srcCStr))

	C.jetkvm_ui_set_image(objNameCStr, srcCStr)

	return true, nil
}

func uiDispSetRotation(rotation uint16) (bool, error) {
	cgoLock.Lock()
	defer cgoLock.Unlock()

	nativeLogger.Info().Uint16("rotation", rotation).Msg("setting rotation")

	cRotation := C.u_int16_t(rotation)

	C.jetkvm_ui_set_rotation(cRotation)
	return true, nil
}

func videoGetStreamQualityFactor() (float64, error) {
	cgoLock.Lock()
	defer cgoLock.Unlock()

	factor := C.jetkvm_video_get_quality_factor()
	return float64(factor), nil
}

func videoSetStreamQualityFactor(factor float64) error {
	cgoLock.Lock()
	defer cgoLock.Unlock()

	C.jetkvm_video_set_quality_factor(C.float(factor))
	return nil
}

func videoGetEDID() (string, error) {
	cgoLock.Lock()
	defer cgoLock.Unlock()

	edidCStr := C.jetkvm_video_get_edid_hex()
	if edidCStr == nil {
		return "", nil
	}
	defer C.free(unsafe.Pointer(edidCStr))
	return C.GoString(edidCStr), nil
}

func videoSetEDID(edid string) error {
	cgoLock.Lock()
	defer cgoLock.Unlock()

	edidCStr := C.CString(edid)
	defer C.free(unsafe.Pointer(edidCStr))
	C.jetkvm_video_set_edid(edidCStr)
	return nil
}

// DO NOT USE THIS FUNCTION IN PRODUCTION
// This is only for testing purposes
func crash() {
	C.jetkvm_crash()
}

// JPEG encoder functions
func jpegStart(quality int) error {
	cgoLock.Lock()
	defer cgoLock.Unlock()

	ret := C.jetkvm_jpeg_start(C.int(quality))
	if ret != 0 {
		return fmt.Errorf("failed to start JPEG encoder: %d", ret)
	}
	return nil
}

func jpegStop() {
	cgoLock.Lock()
	defer cgoLock.Unlock()

	C.jetkvm_jpeg_stop()
}

func jpegSetQuality(quality int) error {
	cgoLock.Lock()
	defer cgoLock.Unlock()

	ret := C.jetkvm_jpeg_set_quality(C.int(quality))
	if ret != 0 {
		return fmt.Errorf("failed to set JPEG quality: %d", ret)
	}
	return nil
}

func jpegGetQuality() int {
	cgoLock.Lock()
	defer cgoLock.Unlock()

	return int(C.jetkvm_jpeg_get_quality())
}

func jpegIsRunning() bool {
	cgoLock.Lock()
	defer cgoLock.Unlock()

	return bool(C.jetkvm_jpeg_is_running())
}

// Request an IDR (keyframe) from the H.264 encoder
func videoRequestKeyframe() error {
	cgoLock.Lock()
	defer cgoLock.Unlock()

	ret := C.jetkvm_video_request_keyframe()
	if ret != 0 {
		return fmt.Errorf("failed to request keyframe: %d", ret)
	}
	return nil
}

// RGA RGB encoder functions (hardware YUV to BGRX conversion)
func rgbStart() error {
	cgoLock.Lock()
	defer cgoLock.Unlock()

	ret := C.jetkvm_rgb_start()
	if ret != 0 {
		return fmt.Errorf("failed to start RGB encoder: %d", ret)
	}
	return nil
}

func rgbStop() {
	cgoLock.Lock()
	defer cgoLock.Unlock()

	C.jetkvm_rgb_stop()
}

func rgbIsRunning() bool {
	cgoLock.Lock()
	defer cgoLock.Unlock()

	return bool(C.jetkvm_rgb_is_running())
}

// H.264 to MJPEG transcoder functions (BETA feature)
// Used for camera redirection when RDP client only sends H.264
//
// Zero-GC design: Uses sync.Pool for buffer reuse and atomic.Pointer for lock-free
// callback access. No allocations occur in the hot path after initial warmup.

// transcodeCallback is the atomic pointer to the current callback.
// Using atomic.Pointer instead of mutex for lock-free hot path access.
var transcodeCallback atomic.Pointer[func([]byte)]

// transcodeBufferPool provides reusable byte slices for JPEG output.
// Eliminates GC pressure by reusing buffers across frames.
// Initial capacity of 512KB covers most JPEG frames at high quality.
var transcodeBufferPool = sync.Pool{
	New: func() interface{} {
		// Pre-allocate 512KB buffer - covers most MJPEG frames
		// MJPEG at 1280x720 quality 75 is typically 30-100KB
		buf := make([]byte, 0, 512*1024)
		return &buf
	},
}

//export goTranscodeOutputCallback
func goTranscodeOutputCallback(jpegData *C.uint8_t, jpegLen C.size_t, userData unsafe.Pointer) {
	// Fast path: load callback atomically (no lock)
	cbPtr := transcodeCallback.Load()
	if cbPtr == nil || jpegLen == 0 {
		return
	}
	cb := *cbPtr

	// Get buffer from pool (zero allocation after warmup)
	bufPtr := transcodeBufferPool.Get().(*[]byte)
	buf := *bufPtr

	// Ensure capacity (may allocate during warmup, then stable)
	dataLen := int(jpegLen)
	if cap(buf) < dataLen {
		buf = make([]byte, dataLen, dataLen*2) // Double capacity for growth
	} else {
		buf = buf[:dataLen]
	}

	// Zero-copy access to C memory, then copy to Go buffer
	// This avoids C.GoBytes which allocates a new slice
	cSlice := unsafe.Slice((*byte)(unsafe.Pointer(jpegData)), dataLen)
	copy(buf, cSlice)

	// Call user callback with pooled buffer
	cb(buf)

	// Return buffer to pool for reuse
	// Reset slice to full capacity for next use
	*bufPtr = buf[:0]
	transcodeBufferPool.Put(bufPtr)
}

func transcodeInit(inputWidth, inputHeight, outputWidth, outputHeight, fps, quality uint32, outputCb func([]byte)) error {
	cgoLock.Lock()
	defer cgoLock.Unlock()

	// Store callback atomically
	transcodeCallback.Store(&outputCb)

	ret := C.jetkvm_transcode_init(
		C.uint32_t(inputWidth),
		C.uint32_t(inputHeight),
		C.uint32_t(outputWidth),
		C.uint32_t(outputHeight),
		C.uint32_t(fps),
		C.uint32_t(quality),
		C.jetkvm_transcode_output_cb(C.goTranscodeOutputCallback),
		nil,
	)
	if ret != 0 {
		transcodeCallback.Store(nil)
		return fmt.Errorf("failed to init transcoder: %d", ret)
	}
	return nil
}

func transcodeShutdown() {
	cgoLock.Lock()
	defer cgoLock.Unlock()

	C.jetkvm_transcode_shutdown()
	transcodeCallback.Store(nil)
}

// transcodeIsRunning returns true if the transcoder is active.
// Lock-free: C function performs atomic read, safe for concurrent access.
func transcodeIsRunning() bool {
	return bool(C.jetkvm_transcode_is_running())
}

func transcodeFeedH264(data []byte) error {
	if len(data) == 0 {
		return nil
	}

	// Don't hold the lock during feed - designed for hot path
	// C function is thread-safe (uses atomics internally)
	ret := C.jetkvm_transcode_feed_h264(
		(*C.uint8_t)(unsafe.Pointer(&data[0])),
		C.size_t(len(data)),
	)
	if ret != 0 {
		if ret == -C.ENOSYS {
			// Decoder not implemented - expected for BETA
			return fmt.Errorf("H.264 decoder not implemented (BETA)")
		}
		return fmt.Errorf("transcode feed failed: %d", ret)
	}
	return nil
}

// transcodeFeedNV12 sends NV12 data directly to hardware MJPEG encoder.
// This is the fastest path - no conversion needed.
func transcodeFeedNV12(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	ret := C.jetkvm_transcode_feed_nv12(
		(*C.uint8_t)(unsafe.Pointer(&data[0])),
		C.size_t(len(data)),
	)
	if ret != 0 {
		return fmt.Errorf("NV12 encode failed: %d", ret)
	}
	return nil
}

// transcodeFeedI420 converts I420 to NV12 using NEON, then hardware encodes.
func transcodeFeedI420(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	ret := C.jetkvm_transcode_feed_i420(
		(*C.uint8_t)(unsafe.Pointer(&data[0])),
		C.size_t(len(data)),
	)
	if ret != 0 {
		return fmt.Errorf("I420 encode failed: %d", ret)
	}
	return nil
}

// transcodeFeedYUY2 converts YUY2 to NV12 using NEON, then hardware encodes.
func transcodeFeedYUY2(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	ret := C.jetkvm_transcode_feed_yuy2(
		(*C.uint8_t)(unsafe.Pointer(&data[0])),
		C.size_t(len(data)),
	)
	if ret != 0 {
		return fmt.Errorf("YUY2 encode failed: %d", ret)
	}
	return nil
}
