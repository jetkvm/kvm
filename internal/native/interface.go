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

	// JPEG encoder methods
	JpegStart(quality int) error
	JpegStop() error
	JpegSetQuality(quality int) error
	JpegGetQuality() (int, error)
	JpegIsRunning() (bool, error)

	// H.264 encoder methods
	VideoRequestKeyframe() error

	// RGA RGB encoder methods (hardware YUV to BGRX conversion)
	RgbStart() error
	RgbStop() error
	RgbIsRunning() (bool, error)

	// H.264/YUV to MJPEG Transcoder methods (BETA feature)
	// Used for camera redirection to convert various formats to MJPEG
	// inputWidth/inputHeight: source resolution (0 = auto-detect for H.264)
	// outputWidth/outputHeight: target resolution for RGA scaling (0 = same as input)
	TranscodeInit(inputWidth, inputHeight, outputWidth, outputHeight, fps, quality uint32, outputCb func([]byte)) error
	TranscodeShutdown()
	TranscodeIsRunning() bool
	TranscodeFeedH264(data []byte) error  // H.264 (slowest - requires SW decode)
	TranscodeFeedNV12(data []byte) error  // NV12 (fastest - direct HW encode)
	TranscodeFeedI420(data []byte) error  // I420 (fast - NEON convert + HW encode)
	TranscodeFeedYUY2(data []byte) error  // YUY2 (fast - NEON convert + HW encode)
}
