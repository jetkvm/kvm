package native

import (
	"slices"
	"time"
)

func (n *Native) setUIVars() {
	uiSetVar("app_version", n.appVersion.String())
}

func (n *Native) initUI() {
	uiInit()
	n.setUIVars()
}

func (n *Native) tickUI() {
	for {
		uiTick()
		time.Sleep(5 * time.Millisecond)
	}
}

func (n *Native) UIObjHide(objName string) (bool, error) {
	return uiObjHide(objName)
}

func (n *Native) UIObjShow(objName string) (bool, error) {
	return uiObjShow(objName)
}

func (n *Native) UIObjSetState(objName string, state string) (bool, error) {
	return uiObjSetState(objName, state)
}

func (n *Native) UIObjAddFlag(objName string, flag string) (bool, error) {
	return uiObjAddFlag(objName, flag)
}

func (n *Native) UIObjClearFlag(objName string, flag string) (bool, error) {
	return uiObjClearFlag(objName, flag)
}

func (n *Native) UIObjSetOpacity(objName string, opacity int) (bool, error) {
	return uiObjSetOpacity(objName, opacity)
}

func (n *Native) UIObjFadeIn(objName string, duration uint32) (bool, error) {
	return uiObjFadeIn(objName, duration)
}

func (n *Native) UIObjFadeOut(objName string, duration uint32) (bool, error) {
	return uiObjFadeOut(objName, duration)
}

func (n *Native) UIObjSetLabelText(objName string, text string) (bool, error) {
	return uiLabelSetText(objName, text)
}

func (n *Native) UIObjSetImageSrc(objName string, image string) (bool, error) {
	return uiImgSetSrc(objName, image)
}

func (n *Native) DisplaySetRotation(rotation string) (bool, error) {
	return uiDispSetRotation(rotation)
}

func (n *Native) UpdateLabelIfChanged(objName string, newText string) {
	l := n.lD.Trace().Str("obj", objName).Str("text", newText)

	changed, err := n.UIObjSetLabelText(objName, newText)
	if err != nil {
		n.lD.Warn().Str("obj", objName).Str("text", newText).Err(err).Msg("failed to update label")
		return
	}

	if changed {
		l.Msg("label changed")
	} else {
		l.Msg("label not changed")
	}
}

func (n *Native) UpdateLabelAndChangeVisibility(objName string, newText string) {
	containerName := objName + "_container"
	if newText == "" {
		_, _ = n.UIObjHide(objName)
		_, _ = n.UIObjHide(containerName)
	} else {
		_, _ = n.UIObjShow(objName)
		_, _ = n.UIObjShow(containerName)
	}

	n.UpdateLabelIfChanged(objName, newText)
}

func (n *Native) SwitchToScreenIf(screenName string, shouldSwitch []string) {
	currentScreen := uiGetCurrentScreen()
	if currentScreen == screenName {
		return
	}
	if !slices.Contains(shouldSwitch, currentScreen) {
		n.lD.Trace().Str("from", currentScreen).Str("to", screenName).Msg("skipping screen switch")
		return
	}
	n.lD.Info().Str("from", currentScreen).Str("to", screenName).Msg("switching screen")
	uiSwitchToScreen(screenName)
}

func (n *Native) SwitchToScreenIfDifferent(screenName string) {
	n.SwitchToScreenIf(screenName, []string{})
}
