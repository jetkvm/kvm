//go:build !linux

package native

func panicPlatformNotSupported() {
	panic("platform not supported")
}

func setUpNativeHandlers() {
	panicPlatformNotSupported()
}

func uiSetVar(name string, value string) {
	panicPlatformNotSupported()
}

func uiGetVar(name string) string {
	panicPlatformNotSupported()
	return ""
}

func uiSwitchToScreen(screen string) {
	panicPlatformNotSupported()
}

func uiGetCurrentScreen() string {
	panicPlatformNotSupported()
	return ""
}

func uiObjAddState(objName string, state string) (bool, error) {
	panicPlatformNotSupported()
	return false, nil
}

func uiObjClearState(objName string, state string) (bool, error) {
	panicPlatformNotSupported()
	return false, nil
}

func uiObjAddFlag(objName string, flag string) (bool, error) {
	panicPlatformNotSupported()
	return false, nil
}

func uiObjClearFlag(objName string, flag string) (bool, error) {
	panicPlatformNotSupported()
	return false, nil
}

func uiObjHide(objName string) (bool, error) {
	panicPlatformNotSupported()
	return false, nil
}

func uiObjShow(objName string) (bool, error) {
	panicPlatformNotSupported()
	return false, nil
}

func uiObjSetOpacity(objName string, opacity int) (bool, error) {
	panicPlatformNotSupported()
	return false, nil
}

func uiObjFadeIn(objName string, duration uint32) (bool, error) {
	panicPlatformNotSupported()
	return false, nil
}

func uiObjFadeOut(objName string, duration uint32) (bool, error) {
	panicPlatformNotSupported()
	return false, nil
}

func uiLabelSetText(objName string, text string) (bool, error) {
	panicPlatformNotSupported()
	return false, nil
}

func uiImgSetSrc(objName string, src string) (bool, error) {
	panicPlatformNotSupported()
	return false, nil
}

func uiDispSetRotation(rotation uint16) (bool, error) {
	panicPlatformNotSupported()
	return false, nil
}

func uiEventCodeToName(code int) string {
	panicPlatformNotSupported()
	return ""
}

func uiGetLVGLVersion() string {
	panicPlatformNotSupported()
	return ""
}

func videoGetStreamQualityFactor() (float64, error) {
	panicPlatformNotSupported()
	return 0, nil
}

func videoSetStreamQualityFactor(factor float64) error {
	panicPlatformNotSupported()
	return nil
}

func videoLogStatus() string {
	panicPlatformNotSupported()
	return ""
}

func videoGetEDID() (string, error) {
	panicPlatformNotSupported()
	return "", nil
}

func videoSetEDID(edid string) error {
	panicPlatformNotSupported()
	return nil
}

func videoGetStreamingStatus() VideoStreamingStatus {
	panicPlatformNotSupported()
	return VideoStreamingStatusInactive
}

func crash() {
	panicPlatformNotSupported()
}

func mjpegSetEnabled(enabled bool) {
	panicPlatformNotSupported()
}

func mjpegGetEnabled() bool {
	panicPlatformNotSupported()
	return false
}

func mjpegSetFrameDivisor(divisor int) {
	panicPlatformNotSupported()
}

func mjpegGetFrameDivisor() int {
	panicPlatformNotSupported()
	return 2
}

func mjpegSetQuality(quality float32) {
	panicPlatformNotSupported()
}

func mjpegGetQuality() float32 {
	panicPlatformNotSupported()
	return 0.8
}

func videoInit(factor float64) error {
	panicPlatformNotSupported()
	return nil
}

func videoShutdown() {
	panicPlatformNotSupported()
}

func videoStart() {
	panicPlatformNotSupported()
}

func videoStop() {
	panicPlatformNotSupported()
}

func uiInit(rotation uint16) {
	panicPlatformNotSupported()
}

func uiTick() {
	panicPlatformNotSupported()
}
