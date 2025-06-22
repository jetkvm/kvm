//go:build linux

package native

import (
	"fmt"
	"strconv"
	"sync"
	"time"
	"unsafe"

	"github.com/rs/zerolog"
)

// #cgo LDFLAGS: -Lcgo/build -Lcgo/build/lib -ljknative -llvgl
// #cgo CFLAGS: -Icgo -Icgo/ui -Icgo/build/_deps/lvgl-src
// #include "ctrl.h"
// #include <stdlib.h>
// typedef const char cchar_t;
// typedef const uint8_t cuint8_t;
// extern void jetkvm_video_state_handler(jetkvm_video_state_t *state);
// static inline void jetkvm_setup_video_state_handler() {
//     jetkvm_set_video_state_handler(&jetkvm_video_state_handler);
// }
// extern void jetkvm_go_log_handler(int level, cchar_t *filename, cchar_t *funcname, int line, cchar_t *message);
// static inline void jetkvm_setup_log_handler() {
//     jetkvm_set_log_handler(&jetkvm_go_log_handler);
// }
// extern void jetkvm_video_handler(cuint8_t *frame, ssize_t len);
// static inline void jetkvm_setup_video_handler() {
//     jetkvm_set_video_handler(&jetkvm_video_handler);
// }
import "C"

var (
	jkInstance     *Native
	jkInstanceLock sync.RWMutex
	jkVideoChan    chan []byte = make(chan []byte)
)

func setUpJkInstance(instance *Native) {
	jkInstanceLock.Lock()
	defer jkInstanceLock.Unlock()

	if jkInstance == nil {
		jkInstance = instance
	}

	if jkInstance != instance {
		panic("jkInstance is already set")
	}
}

//export jetkvm_video_state_handler
func jetkvm_video_state_handler(state *C.jetkvm_video_state_t) {
	jkInstanceLock.RLock()
	defer jkInstanceLock.RUnlock()

	if jkInstance != nil {
		// convert state to VideoState
		videoState := VideoState{
			Ready:          bool(state.ready),
			Error:          C.GoString(state.error),
			Width:          int(state.width),
			Height:         int(state.height),
			FramePerSecond: float64(state.frame_per_second),
		}
		jkInstance.handleVideoStateMessage(videoState)
	}
}

//export jetkvm_go_log_handler
func jetkvm_go_log_handler(level C.int, filename *C.cchar_t, funcname *C.cchar_t, line C.int, message *C.cchar_t) {
	l := nativeLogger.With().
		Str("file", C.GoString(filename)).
		Str("function", C.GoString(funcname)).
		Int("line", int(line)).
		Logger()

	gLevel := zerolog.Level(level)
	switch gLevel {
	case zerolog.DebugLevel:
		l.Debug().Msg(C.GoString(message))
	case zerolog.InfoLevel:
		l.Info().Msg(C.GoString(message))
	case zerolog.WarnLevel:
		l.Warn().Msg(C.GoString(message))
	case zerolog.ErrorLevel:
		l.Error().Msg(C.GoString(message))
	case zerolog.PanicLevel:
		l.Panic().Msg(C.GoString(message))
	case zerolog.FatalLevel:
		l.Fatal().Msg(C.GoString(message))
	case zerolog.TraceLevel:
		l.Trace().Msg(C.GoString(message))
	case zerolog.NoLevel:
		l.Info().Msg(C.GoString(message))
	default:
		l.Info().Msg(C.GoString(message))
	}
}

//export jetkvm_video_handler
func jetkvm_video_handler(frame *C.cuint8_t, len C.ssize_t) {
	jkVideoChan <- C.GoBytes(unsafe.Pointer(frame), C.int(len))
}

func setVideoStateHandler() {
	C.jetkvm_setup_video_state_handler()
}

func setLogHandler() {
	C.jetkvm_setup_log_handler()
}

func setVideoHandler() {
	C.jetkvm_setup_video_handler()
}

func (n *Native) StartNativeVideo() {
	setUpJkInstance(n)

	setVideoStateHandler()
	setLogHandler()
	setVideoHandler()

	C.jetkvm_set_app_version(C.CString(n.appVersion.String()))

	C.jetkvm_ui_init()

	n.UpdateLabelIfChanged("boot_screen_version", n.appVersion.String())

	go func() {
		for {
			C.jetkvm_ui_tick()
			time.Sleep(5 * time.Millisecond)
		}
	}()

	if C.jetkvm_video_init() != 0 {
		nativeLogger.Error().Msg("failed to initialize video")
		return
	}

	C.jetkvm_video_start()

	close(n.ready)
}

func (n *Native) StopNativeVideo() {
	C.jetkvm_video_stop()
}

func (n *Native) SwitchToScreen(screen string) {
	screenCStr := C.CString(screen)
	defer C.free(unsafe.Pointer(screenCStr))
	C.jetkvm_ui_load_screen(screenCStr)
}

func (n *Native) GetCurrentScreen() string {
	screenCStr := C.jetkvm_ui_get_current_screen()
	return C.GoString(screenCStr)
}

func (n *Native) ObjSetState(objName string, state string) (bool, error) {
	objNameCStr := C.CString(objName)
	defer C.free(unsafe.Pointer(objNameCStr))
	stateCStr := C.CString(state)
	defer C.free(unsafe.Pointer(stateCStr))
	C.jetkvm_ui_set_state(objNameCStr, stateCStr)
	return true, nil
}

func (n *Native) ObjAddFlag(objName string, flag string) (bool, error) {
	objNameCStr := C.CString(objName)
	defer C.free(unsafe.Pointer(objNameCStr))
	flagCStr := C.CString(flag)
	defer C.free(unsafe.Pointer(flagCStr))
	C.jetkvm_ui_add_flag(objNameCStr, flagCStr)
	return true, nil
}

func (n *Native) ObjClearFlag(objName string, flag string) (bool, error) {
	objNameCStr := C.CString(objName)
	defer C.free(unsafe.Pointer(objNameCStr))
	flagCStr := C.CString(flag)
	defer C.free(unsafe.Pointer(flagCStr))
	C.jetkvm_ui_clear_flag(objNameCStr, flagCStr)
	return true, nil
}

func (n *Native) ObjHide(objName string) (bool, error) {
	return n.ObjAddFlag(objName, "LV_OBJ_FLAG_HIDDEN")
}

func (n *Native) ObjShow(objName string) (bool, error) {
	return n.ObjClearFlag(objName, "LV_OBJ_FLAG_HIDDEN")
}

func (n *Native) ObjSetOpacity(objName string, opacity int) (bool, error) {
	objNameCStr := C.CString(objName)
	defer C.free(unsafe.Pointer(objNameCStr))

	C.jetkvm_ui_set_opacity(objNameCStr, C.u_int8_t(opacity))
	return true, nil
}

func (n *Native) ObjFadeIn(objName string, duration uint32) (bool, error) {
	objNameCStr := C.CString(objName)
	defer C.free(unsafe.Pointer(objNameCStr))

	C.jetkvm_ui_fade_in(objNameCStr, C.u_int32_t(duration))

	return true, nil
}

func (n *Native) ObjFadeOut(objName string, duration uint32) (bool, error) {
	objNameCStr := C.CString(objName)
	defer C.free(unsafe.Pointer(objNameCStr))

	C.jetkvm_ui_fade_out(objNameCStr, C.u_int32_t(duration))

	return true, nil
}

func (n *Native) LabelSetText(objName string, text string) (bool, error) {
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

func (n *Native) ImgSetSrc(objName string, src string) (bool, error) {
	objNameCStr := C.CString(objName)
	defer C.free(unsafe.Pointer(objNameCStr))

	srcCStr := C.CString(src)
	defer C.free(unsafe.Pointer(srcCStr))

	C.jetkvm_ui_set_image(objNameCStr, srcCStr)

	return true, nil
}

func (n *Native) DispSetRotation(rotation string) (bool, error) {
	rotationInt, err := strconv.Atoi(rotation)
	if err != nil {
		return false, err
	}
	nativeLogger.Info().Int("rotation", rotationInt).Msg("setting rotation")
	// C.jetkvm_ui_set_rotation(C.u_int8_t(rotationInt))
	return true, nil
}

func (n *Native) GetStreamQualityFactor() (float64, error) {
	factor := C.jetkvm_video_get_quality_factor()
	return float64(factor), nil
}

func (n *Native) SetStreamQualityFactor(factor float64) error {
	C.jetkvm_video_set_quality_factor(C.float(factor))
	return nil
}

func (n *Native) GetEDID() (string, error) {
	edidCStr := C.jetkvm_video_get_edid_hex()
	return C.GoString(edidCStr), nil
}

func (n *Native) SetEDID(edid string) error {
	edidCStr := C.CString(edid)
	defer C.free(unsafe.Pointer(edidCStr))
	C.jetkvm_video_set_edid(edidCStr)
	return nil
}
