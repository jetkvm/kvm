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
