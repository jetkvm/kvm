//go:build !linux

package native

func panicNotImplemented() {
	panic("not supported")
}

func (n *Native) StartNativeVideo() {
	panicNotImplemented()
}

func (n *Native) StopNativeVideo() {
	panicNotImplemented()
}

func (n *Native) SwitchToScreen(screen string) {
	panicNotImplemented()
}

func (n *Native) GetCurrentScreen() string {
	panicNotImplemented()
}

func (n *Native) ObjSetState(objName string, state string) (bool, error) {
	panicNotImplemented()
	return false, nil
}

func (n *Native) ObjAddFlag(objName string, flag string) (bool, error) {
	panicNotImplemented()
	return false, nil
}

func (n *Native) ObjClearFlag(objName string, flag string) (bool, error) {
	panicNotImplemented()
	return false, nil
}

func (n *Native) ObjHide(objName string) (bool, error) {
	panicNotImplemented()
	return false, nil
}

func (n *Native) ObjShow(objName string) (bool, error) {
	panicNotImplemented()
	return false, nil
}

func (n *Native) ObjSetOpacity(objName string, opacity int) (bool, error) { // nolint:unused
	panicNotImplemented()
	return false, nil
}

func (n *Native) ObjFadeIn(objName string, duration uint32) (bool, error) {
	panicNotImplemented()
	return false, nil
}

func (n *Native) ObjFadeOut(objName string, duration uint32) (bool, error) {
	panicNotImplemented()
	return false, nil
}

func (n *Native) LabelSetText(objName string, text string) (bool, error) {
	panicNotImplemented()
	return false, nil
}

func (n *Native) ImgSetSrc(objName string, src string) (bool, error) {
	panicNotImplemented()
	return false, nil
}

func (n *Native) DispSetRotation(rotation string) (bool, error) {
	panicNotImplemented()
	return false, nil
}

func (n *Native) GetStreamQualityFactor() (float64, error) {
	panicNotImplemented()
	return 0, nil
}

func (n *Native) SetStreamQualityFactor(factor float64) error {
	panicNotImplemented()
	return nil
}
