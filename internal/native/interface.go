package native

// NativeInterface defines the interface that both Native and NativeProxy implement
type NativeInterface interface {
	Start() error
	VideoSetSleepMode(enabled bool) error
	VideoGetSleepMode() (bool, error)
	VideoSleepModeSupported() bool
	VideoSetQualityFactor(factor float64) error
	VideoGetQualityFactor() (float64, error)
	VideoSetEDID(edid string) error
	VideoGetEDID() (string, error)
	VideoLogStatus() (string, error)
	VideoStop() error
	VideoStart() error
	GetLVGLVersion() (string, error)
	UIObjHide(objName string) (bool, error)
	UIObjShow(objName string) (bool, error)
	UISetVar(name string, value string)
	UIGetVar(name string) string
	UIObjAddState(objName string, state string) (bool, error)
	UIObjClearState(objName string, state string) (bool, error)
	UIObjAddFlag(objName string, flag string) (bool, error)
	UIObjClearFlag(objName string, flag string) (bool, error)
	UIObjSetOpacity(objName string, opacity int) (bool, error)
	UIObjFadeIn(objName string, duration uint32) (bool, error)
	UIObjFadeOut(objName string, duration uint32) (bool, error)
	UIObjSetLabelText(objName string, text string) (bool, error)
	UIObjSetImageSrc(objName string, image string) (bool, error)
	DisplaySetRotation(rotation uint16) (bool, error)
	UpdateLabelIfChanged(objName string, newText string)
	UpdateLabelAndChangeVisibility(objName string, newText string)
	SwitchToScreenIf(screenName string, shouldSwitch []string)
	SwitchToScreenIfDifferent(screenName string)
	DoNotUseThisIsForCrashTestingOnly()
}
