package native

import (
	"context"

	pb "github.com/jetkvm/kvm/internal/native/proto"
)

// Below are generated methods, do not edit manually

// Video methods
func (c *GRPCClient) VideoSetSleepMode(enabled bool) error {
	_, err := c.client.VideoSetSleepMode(context.Background(), &pb.VideoSetSleepModeRequest{Enabled: enabled})
	return err
}

func (c *GRPCClient) VideoGetSleepMode() (bool, error) {
	resp, err := c.client.VideoGetSleepMode(context.Background(), &pb.Empty{})
	if err != nil {
		return false, err
	}
	return resp.Enabled, nil
}

func (c *GRPCClient) VideoSleepModeSupported() bool {
	resp, err := c.client.VideoSleepModeSupported(context.Background(), &pb.Empty{})
	if err != nil {
		return false
	}
	return resp.Supported
}

func (c *GRPCClient) VideoSetQualityFactor(factor float64) error {
	_, err := c.client.VideoSetQualityFactor(context.Background(), &pb.VideoSetQualityFactorRequest{Factor: factor})
	return err
}

func (c *GRPCClient) VideoGetQualityFactor() (float64, error) {
	resp, err := c.client.VideoGetQualityFactor(context.Background(), &pb.Empty{})
	if err != nil {
		return 0, err
	}
	return resp.Factor, nil
}

func (c *GRPCClient) VideoSetEDID(edid string) error {
	_, err := c.client.VideoSetEDID(context.Background(), &pb.VideoSetEDIDRequest{Edid: edid})
	return err
}

func (c *GRPCClient) VideoGetEDID() (string, error) {
	resp, err := c.client.VideoGetEDID(context.Background(), &pb.Empty{})
	if err != nil {
		return "", err
	}
	return resp.Edid, nil
}

func (c *GRPCClient) VideoLogStatus() (string, error) {
	resp, err := c.client.VideoLogStatus(context.Background(), &pb.Empty{})
	if err != nil {
		return "", err
	}
	return resp.Status, nil
}

func (c *GRPCClient) VideoStop() error {
	_, err := c.client.VideoStop(context.Background(), &pb.Empty{})
	return err
}

func (c *GRPCClient) VideoStart() error {
	_, err := c.client.VideoStart(context.Background(), &pb.Empty{})
	return err
}

// UI methods
func (c *GRPCClient) GetLVGLVersion() (string, error) {
	resp, err := c.client.GetLVGLVersion(context.Background(), &pb.Empty{})
	if err != nil {
		return "", err
	}
	return resp.Version, nil
}

func (c *GRPCClient) UIObjHide(objName string) (bool, error) {
	resp, err := c.client.UIObjHide(context.Background(), &pb.UIObjHideRequest{ObjName: objName})
	if err != nil {
		return false, err
	}
	return resp.Success, nil
}

func (c *GRPCClient) UIObjShow(objName string) (bool, error) {
	resp, err := c.client.UIObjShow(context.Background(), &pb.UIObjShowRequest{ObjName: objName})
	if err != nil {
		return false, err
	}
	return resp.Success, nil
}

func (c *GRPCClient) UISetVar(name string, value string) {
	_, _ = c.client.UISetVar(context.Background(), &pb.UISetVarRequest{Name: name, Value: value})
}

func (c *GRPCClient) UIGetVar(name string) string {
	resp, err := c.client.UIGetVar(context.Background(), &pb.UIGetVarRequest{Name: name})
	if err != nil {
		return ""
	}
	return resp.Value
}

func (c *GRPCClient) UIObjAddState(objName string, state string) (bool, error) {
	resp, err := c.client.UIObjAddState(context.Background(), &pb.UIObjAddStateRequest{ObjName: objName, State: state})
	if err != nil {
		return false, err
	}
	return resp.Success, nil
}

func (c *GRPCClient) UIObjClearState(objName string, state string) (bool, error) {
	resp, err := c.client.UIObjClearState(context.Background(), &pb.UIObjClearStateRequest{ObjName: objName, State: state})
	if err != nil {
		return false, err
	}
	return resp.Success, nil
}

func (c *GRPCClient) UIObjAddFlag(objName string, flag string) (bool, error) {
	resp, err := c.client.UIObjAddFlag(context.Background(), &pb.UIObjAddFlagRequest{ObjName: objName, Flag: flag})
	if err != nil {
		return false, err
	}
	return resp.Success, nil
}

func (c *GRPCClient) UIObjClearFlag(objName string, flag string) (bool, error) {
	resp, err := c.client.UIObjClearFlag(context.Background(), &pb.UIObjClearFlagRequest{ObjName: objName, Flag: flag})
	if err != nil {
		return false, err
	}
	return resp.Success, nil
}

func (c *GRPCClient) UIObjSetOpacity(objName string, opacity int) (bool, error) {
	resp, err := c.client.UIObjSetOpacity(context.Background(), &pb.UIObjSetOpacityRequest{ObjName: objName, Opacity: int32(opacity)})
	if err != nil {
		return false, err
	}
	return resp.Success, nil
}

func (c *GRPCClient) UIObjFadeIn(objName string, duration uint32) (bool, error) {
	resp, err := c.client.UIObjFadeIn(context.Background(), &pb.UIObjFadeInRequest{ObjName: objName, Duration: duration})
	if err != nil {
		return false, err
	}
	return resp.Success, nil
}

func (c *GRPCClient) UIObjFadeOut(objName string, duration uint32) (bool, error) {
	resp, err := c.client.UIObjFadeOut(context.Background(), &pb.UIObjFadeOutRequest{ObjName: objName, Duration: duration})
	if err != nil {
		return false, err
	}
	return resp.Success, nil
}

func (c *GRPCClient) UIObjSetLabelText(objName string, text string) (bool, error) {
	resp, err := c.client.UIObjSetLabelText(context.Background(), &pb.UIObjSetLabelTextRequest{ObjName: objName, Text: text})
	if err != nil {
		return false, err
	}
	return resp.Success, nil
}

func (c *GRPCClient) UIObjSetImageSrc(objName string, image string) (bool, error) {
	resp, err := c.client.UIObjSetImageSrc(context.Background(), &pb.UIObjSetImageSrcRequest{ObjName: objName, Image: image})
	if err != nil {
		return false, err
	}
	return resp.Success, nil
}

func (c *GRPCClient) DisplaySetRotation(rotation uint16) (bool, error) {
	resp, err := c.client.DisplaySetRotation(context.Background(), &pb.DisplaySetRotationRequest{Rotation: uint32(rotation)})
	if err != nil {
		return false, err
	}
	return resp.Success, nil
}

func (c *GRPCClient) UpdateLabelIfChanged(objName string, newText string) {
	_, _ = c.client.UpdateLabelIfChanged(context.Background(), &pb.UpdateLabelIfChangedRequest{ObjName: objName, NewText: newText})
}

func (c *GRPCClient) UpdateLabelAndChangeVisibility(objName string, newText string) {
	_, _ = c.client.UpdateLabelAndChangeVisibility(context.Background(), &pb.UpdateLabelAndChangeVisibilityRequest{ObjName: objName, NewText: newText})
}

func (c *GRPCClient) SwitchToScreenIf(screenName string, shouldSwitch []string) {
	_, _ = c.client.SwitchToScreenIf(context.Background(), &pb.SwitchToScreenIfRequest{ScreenName: screenName, ShouldSwitch: shouldSwitch})
}

func (c *GRPCClient) SwitchToScreenIfDifferent(screenName string) {
	_, _ = c.client.SwitchToScreenIfDifferent(context.Background(), &pb.SwitchToScreenIfDifferentRequest{ScreenName: screenName})
}

func (c *GRPCClient) DoNotUseThisIsForCrashTestingOnly() {
	_, _ = c.client.DoNotUseThisIsForCrashTestingOnly(context.Background(), &pb.Empty{})
}
