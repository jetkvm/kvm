package native

type EmptyNativeInterface struct {
}

func (e *EmptyNativeInterface) Start() error { return nil }

func (e *EmptyNativeInterface) VideoSetSleepMode(enabled bool) error { return nil }

func (e *EmptyNativeInterface) VideoGetSleepMode() (bool, error) { return false, nil }

func (e *EmptyNativeInterface) VideoSleepModeSupported() bool {
	return false
}

func (e *EmptyNativeInterface) VideoSetQualityFactor(factor float64) error {
	return nil
}

func (e *EmptyNativeInterface) VideoGetQualityFactor() (float64, error) {
	return 0, nil
}

func (e *EmptyNativeInterface) VideoSetEDID(edid string) error {
	return nil
}

func (e *EmptyNativeInterface) VideoGetEDID() (string, error) {
	return "", nil
}

func (e *EmptyNativeInterface) VideoLogStatus() (string, error) {
	return "", nil
}

func (e *EmptyNativeInterface) VideoStop() error {
	return nil
}

func (e *EmptyNativeInterface) VideoStart() error {
	return nil
}

func (e *EmptyNativeInterface) GetLVGLVersion() (string, error) {
	return "", nil
}

func (e *EmptyNativeInterface) UIObjHide(objName string) (bool, error) {
	return false, nil
}

func (e *EmptyNativeInterface) UIObjShow(objName string) (bool, error) {
	return false, nil
}

func (e *EmptyNativeInterface) UISetVar(name string, value string) {
}

func (e *EmptyNativeInterface) UIGetVar(name string) string {
	return ""
}

func (e *EmptyNativeInterface) UIObjAddState(objName string, state string) (bool, error) {
	return false, nil
}

func (e *EmptyNativeInterface) UIObjClearState(objName string, state string) (bool, error) {
	return false, nil
}

func (e *EmptyNativeInterface) UIObjAddFlag(objName string, flag string) (bool, error) {
	return false, nil
}

func (e *EmptyNativeInterface) UIObjClearFlag(objName string, flag string) (bool, error) {
	return false, nil
}

func (e *EmptyNativeInterface) UIObjSetOpacity(objName string, opacity int) (bool, error) {
	return false, nil
}

func (e *EmptyNativeInterface) UIObjFadeIn(objName string, duration uint32) (bool, error) {
	return false, nil
}

func (e *EmptyNativeInterface) UIObjFadeOut(objName string, duration uint32) (bool, error) {
	return false, nil
}

func (e *EmptyNativeInterface) UIObjSetLabelText(objName string, text string) (bool, error) {
	return false, nil
}

func (e *EmptyNativeInterface) UIObjSetImageSrc(objName string, image string) (bool, error) {
	return false, nil
}

func (e *EmptyNativeInterface) DisplaySetRotation(rotation uint16) (bool, error) {
	return false, nil
}

func (e *EmptyNativeInterface) UpdateLabelIfChanged(objName string, newText string) {}

func (e *EmptyNativeInterface) UpdateLabelAndChangeVisibility(objName string, newText string) {}

func (e *EmptyNativeInterface) SwitchToScreenIf(screenName string, shouldSwitch []string) {}

func (e *EmptyNativeInterface) SwitchToScreenIfDifferent(screenName string) {}

func (e *EmptyNativeInterface) DoNotUseThisIsForCrashTestingOnly() {}

// JPEG encoder methods
func (e *EmptyNativeInterface) JpegStart(quality int) error { return nil }

func (e *EmptyNativeInterface) JpegStop() error { return nil }

func (e *EmptyNativeInterface) JpegSetQuality(quality int) error { return nil }

func (e *EmptyNativeInterface) JpegGetQuality() (int, error) { return 0, nil }

func (e *EmptyNativeInterface) JpegIsRunning() (bool, error) { return false, nil }

// H.264 encoder methods
func (e *EmptyNativeInterface) VideoRequestKeyframe() error { return nil }

// RGA RGB encoder methods
func (e *EmptyNativeInterface) RgbStart() error { return nil }

func (e *EmptyNativeInterface) RgbStop() error { return nil }

func (e *EmptyNativeInterface) RgbIsRunning() (bool, error) { return false, nil }
