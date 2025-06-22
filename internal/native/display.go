package native

import "slices"

func (n *Native) UpdateLabelIfChanged(objName string, newText string) {
	l := n.lD.Trace().Str("obj", objName).Str("text", newText)

	changed, err := n.LabelSetText(objName, newText)
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
		_, _ = n.ObjHide(objName)
		_, _ = n.ObjHide(containerName)
	} else {
		_, _ = n.ObjShow(objName)
		_, _ = n.ObjShow(containerName)
	}

	n.UpdateLabelIfChanged(objName, newText)
}

func (n *Native) SwitchToScreenIf(screenName string, shouldSwitch []string) {
	currentScreen := n.GetCurrentScreen()
	if currentScreen == screenName {
		return
	}
	if !slices.Contains(shouldSwitch, currentScreen) {
		displayLogger.Trace().Str("from", currentScreen).Str("to", screenName).Msg("skipping screen switch")
		return
	}
	displayLogger.Info().Str("from", currentScreen).Str("to", screenName).Msg("switching screen")
	n.SwitchToScreen(screenName)
}

func (n *Native) SwitchToScreenIfDifferent(screenName string) {
	n.SwitchToScreenIf(screenName, []string{})
}
