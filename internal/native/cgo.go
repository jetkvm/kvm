package native

var (
	_ func() error                       = setUpNativeHandlers
	_ func()                             = crash
	_ func(uint16)                       = uiInit
	_ func()                             = uiTick
	_ func(string, string)               = uiSetVar
	_ func(string) string                = uiGetVar
	_ func(string)                       = uiSwitchToScreen
	_ func() string                      = uiGetCurrentScreen
	_ func(string, string) (bool, error) = uiObjAddState
	_ func(string, string) (bool, error) = uiObjClearState
	_ func(string, string) (bool, error) = uiObjAddFlag
	_ func(string, string) (bool, error) = uiObjClearFlag
	_ func(string) (bool, error)         = uiObjHide
	_ func(string) (bool, error)         = uiObjShow
	_ func(string, int) (bool, error)    = uiObjSetOpacity
	_ func(string, uint32) (bool, error) = uiObjFadeIn
	_ func(string, uint32) (bool, error) = uiObjFadeOut
	_ func(string, string) (bool, error) = uiLabelSetText
	_ func(string, string) (bool, error) = uiImgSetSrc
	_ func(uint16) (bool, error)         = uiDispSetRotation
	_ func(int) string                   = uiEventCodeToName
	_ func() string                      = uiGetLVGLVersion
	_ func(float64) error                = videoInit
	_ func()                             = videoShutdown
	_ func()                             = videoStart
	_ func()                             = videoStop
	_ func() VideoStreamingStatus        = videoGetStreamingStatus
	_ func() string                      = videoLogStatus
	_ func() (float64, error)            = videoGetStreamQualityFactor
	_ func(float64) error                = videoSetStreamQualityFactor
	_ func(int) error                    = videoSetCodecType
	_ func() (int, error)                = videoGetCodecType
	_ func() (string, error)             = videoGetEDID
	_ func(string) error                 = videoSetEDID
)
