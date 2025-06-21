//go:build linux

package native

import (
	"fmt"
	"strconv"
	"time"
	"unsafe"
)

// #cgo LDFLAGS: -Ljetkvm-native
// #include "ctrl.h"
// #include <stdlib.h>
// extern void jetkvm_video_state_handler(jetkvm_video_state_t *state);
// static inline void jetkvm_setup_video_state_handler() {
//     jetkvm_set_video_state_handler(&jetkvm_video_state_handler);
// }
import "C"

//export jetkvm_video_state_handler
func jetkvm_video_state_handler(state *C.jetkvm_video_state_t) {
	nativeLogger.Info().Msg("video state handler")
	nativeLogger.Info().Msg(fmt.Sprintf("state: %+v", state))
}

func setVideoStateHandler() {
	C.jetkvm_setup_video_state_handler()
}

func (n *Native) StartNativeVideo() {
	setVideoStateHandler()
	C.jetkvm_ui_init()

	n.UpdateLabelIfChanged("boot_screen_version", n.AppVersion.String())

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
	return 1.0, nil
}

func (n *Native) SetStreamQualityFactor(factor float64) error {
	return nil
}

func (n *Native) GetEDID() (string, error) {
	return "", nil
}

func (n *Native) SetEDID(edid string) error {
	return nil
}
