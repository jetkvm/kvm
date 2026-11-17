//go:build !linux || !arm || !cgo

package native

import "errors"

var errNotSupported = errors.New("native: the C bridge is available only on linux/arm")

func setUpNativeHandlers() {
	panic(errNotSupported)
}

func crash() {
	panic(errNotSupported)
}

func uiInit(rotation uint16) {}

func uiTick() {}

func uiSetVar(name string, value string) {}

func uiGetVar(name string) string {
	return ""
}

func uiSwitchToScreen(screen string) {}

func uiGetCurrentScreen() string {
	return ""
}

func uiObjAddState(objName string, state string) (bool, error) {
	return false, errNotSupported
}

func uiObjClearState(objName string, state string) (bool, error) {
	return false, errNotSupported
}

func uiObjAddFlag(objName string, flag string) (bool, error) {
	return false, errNotSupported
}

func uiObjClearFlag(objName string, flag string) (bool, error) {
	return false, errNotSupported
}

func uiObjHide(objName string) (bool, error) {
	return false, errNotSupported
}

func uiObjShow(objName string) (bool, error) {
	return false, errNotSupported
}

func uiObjSetOpacity(objName string, opacity int) (bool, error) {
	return false, errNotSupported
}

func uiObjFadeIn(objName string, duration uint32) (bool, error) {
	return false, errNotSupported
}

func uiObjFadeOut(objName string, duration uint32) (bool, error) {
	return false, errNotSupported
}

func uiLabelSetText(objName string, text string) (bool, error) {
	return false, errNotSupported
}

func uiImgSetSrc(objName string, src string) (bool, error) {
	return false, errNotSupported
}

func uiDispSetRotation(rotation uint16) (bool, error) {
	return false, errNotSupported
}

func uiEventCodeToName(code int) string {
	return ""
}

func uiGetLVGLVersion() string {
	return ""
}

func videoInit(factor float64) error {
	return errNotSupported
}

func videoShutdown() {}

func videoStart() {}

func videoStop() {}

func videoGetStreamingStatus() VideoStreamingStatus {
	return VideoStreamingStatusInactive
}

func videoLogStatus() string {
	return ""
}

func videoGetStreamQualityFactor() (float64, error) {
	return 0, errNotSupported
}

func videoSetStreamQualityFactor(factor float64) error {
	return errNotSupported
}

func videoSetCodecType(codecType int) error {
	return errNotSupported
}

func videoGetCodecType() (int, error) {
	return 0, errNotSupported
}

func videoGetEDID() (string, error) {
	return "", errNotSupported
}

func videoSetEDID(edid string) error {
	return errNotSupported
}
