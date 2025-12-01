package native

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jetkvm/kvm/internal/native/proto"
)

// Below are generated methods, do not edit manually

// Video methods
func (s *grpcServer) VideoSetSleepMode(ctx context.Context, req *pb.VideoSetSleepModeRequest) (*pb.Empty, error) {
	if err := s.native.VideoSetSleepMode(req.Enabled); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &pb.Empty{}, nil
}

func (s *grpcServer) VideoGetSleepMode(ctx context.Context, req *pb.Empty) (*pb.VideoGetSleepModeResponse, error) {
	enabled, err := s.native.VideoGetSleepMode()
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &pb.VideoGetSleepModeResponse{Enabled: enabled}, nil
}

func (s *grpcServer) VideoSleepModeSupported(ctx context.Context, req *pb.Empty) (*pb.VideoSleepModeSupportedResponse, error) {
	return &pb.VideoSleepModeSupportedResponse{Supported: s.native.VideoSleepModeSupported()}, nil
}

func (s *grpcServer) VideoSetQualityFactor(ctx context.Context, req *pb.VideoSetQualityFactorRequest) (*pb.Empty, error) {
	if err := s.native.VideoSetQualityFactor(req.Factor); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &pb.Empty{}, nil
}

func (s *grpcServer) VideoGetQualityFactor(ctx context.Context, req *pb.Empty) (*pb.VideoGetQualityFactorResponse, error) {
	factor, err := s.native.VideoGetQualityFactor()
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &pb.VideoGetQualityFactorResponse{Factor: factor}, nil
}

func (s *grpcServer) VideoSetEDID(ctx context.Context, req *pb.VideoSetEDIDRequest) (*pb.Empty, error) {
	if err := s.native.VideoSetEDID(req.Edid); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &pb.Empty{}, nil
}

func (s *grpcServer) VideoGetEDID(ctx context.Context, req *pb.Empty) (*pb.VideoGetEDIDResponse, error) {
	edid, err := s.native.VideoGetEDID()
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &pb.VideoGetEDIDResponse{Edid: edid}, nil
}

func (s *grpcServer) VideoLogStatus(ctx context.Context, req *pb.Empty) (*pb.VideoLogStatusResponse, error) {
	logStatus, err := s.native.VideoLogStatus()
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &pb.VideoLogStatusResponse{Status: logStatus}, nil
}

func (s *grpcServer) VideoStop(ctx context.Context, req *pb.Empty) (*pb.Empty, error) {
	if err := s.native.VideoStop(); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &pb.Empty{}, nil
}

func (s *grpcServer) VideoStart(ctx context.Context, req *pb.Empty) (*pb.Empty, error) {
	if err := s.native.VideoStart(); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &pb.Empty{}, nil
}

// UI methods
func (s *grpcServer) GetLVGLVersion(ctx context.Context, req *pb.Empty) (*pb.GetLVGLVersionResponse, error) {
	version, err := s.native.GetLVGLVersion()
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &pb.GetLVGLVersionResponse{Version: version}, nil
}

func (s *grpcServer) UIObjHide(ctx context.Context, req *pb.UIObjHideRequest) (*pb.UIObjHideResponse, error) {
	success, err := s.native.UIObjHide(req.ObjName)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &pb.UIObjHideResponse{Success: success}, nil
}

func (s *grpcServer) UIObjShow(ctx context.Context, req *pb.UIObjShowRequest) (*pb.UIObjShowResponse, error) {
	success, err := s.native.UIObjShow(req.ObjName)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &pb.UIObjShowResponse{Success: success}, nil
}

func (s *grpcServer) UISetVar(ctx context.Context, req *pb.UISetVarRequest) (*pb.Empty, error) {
	s.native.UISetVar(req.Name, req.Value)
	return &pb.Empty{}, nil
}

func (s *grpcServer) UIGetVar(ctx context.Context, req *pb.UIGetVarRequest) (*pb.UIGetVarResponse, error) {
	value := s.native.UIGetVar(req.Name)
	return &pb.UIGetVarResponse{Value: value}, nil
}

func (s *grpcServer) UIObjAddState(ctx context.Context, req *pb.UIObjAddStateRequest) (*pb.UIObjAddStateResponse, error) {
	success, err := s.native.UIObjAddState(req.ObjName, req.State)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &pb.UIObjAddStateResponse{Success: success}, nil
}

func (s *grpcServer) UIObjClearState(ctx context.Context, req *pb.UIObjClearStateRequest) (*pb.UIObjClearStateResponse, error) {
	success, err := s.native.UIObjClearState(req.ObjName, req.State)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &pb.UIObjClearStateResponse{Success: success}, nil
}

func (s *grpcServer) UIObjAddFlag(ctx context.Context, req *pb.UIObjAddFlagRequest) (*pb.UIObjAddFlagResponse, error) {
	success, err := s.native.UIObjAddFlag(req.ObjName, req.Flag)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &pb.UIObjAddFlagResponse{Success: success}, nil
}

func (s *grpcServer) UIObjClearFlag(ctx context.Context, req *pb.UIObjClearFlagRequest) (*pb.UIObjClearFlagResponse, error) {
	success, err := s.native.UIObjClearFlag(req.ObjName, req.Flag)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &pb.UIObjClearFlagResponse{Success: success}, nil
}

func (s *grpcServer) UIObjSetOpacity(ctx context.Context, req *pb.UIObjSetOpacityRequest) (*pb.UIObjSetOpacityResponse, error) {
	success, err := s.native.UIObjSetOpacity(req.ObjName, int(req.Opacity))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &pb.UIObjSetOpacityResponse{Success: success}, nil
}

func (s *grpcServer) UIObjFadeIn(ctx context.Context, req *pb.UIObjFadeInRequest) (*pb.UIObjFadeInResponse, error) {
	success, err := s.native.UIObjFadeIn(req.ObjName, req.Duration)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &pb.UIObjFadeInResponse{Success: success}, nil
}

func (s *grpcServer) UIObjFadeOut(ctx context.Context, req *pb.UIObjFadeOutRequest) (*pb.UIObjFadeOutResponse, error) {
	success, err := s.native.UIObjFadeOut(req.ObjName, req.Duration)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &pb.UIObjFadeOutResponse{Success: success}, nil
}

func (s *grpcServer) UIObjSetLabelText(ctx context.Context, req *pb.UIObjSetLabelTextRequest) (*pb.UIObjSetLabelTextResponse, error) {
	success, err := s.native.UIObjSetLabelText(req.ObjName, req.Text)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &pb.UIObjSetLabelTextResponse{Success: success}, nil
}

func (s *grpcServer) UIObjSetImageSrc(ctx context.Context, req *pb.UIObjSetImageSrcRequest) (*pb.UIObjSetImageSrcResponse, error) {
	success, err := s.native.UIObjSetImageSrc(req.ObjName, req.Image)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &pb.UIObjSetImageSrcResponse{Success: success}, nil
}

func (s *grpcServer) DisplaySetRotation(ctx context.Context, req *pb.DisplaySetRotationRequest) (*pb.DisplaySetRotationResponse, error) {
	success, err := s.native.DisplaySetRotation(uint16(req.Rotation))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &pb.DisplaySetRotationResponse{Success: success}, nil
}

func (s *grpcServer) UpdateLabelIfChanged(ctx context.Context, req *pb.UpdateLabelIfChangedRequest) (*pb.Empty, error) {
	s.native.UpdateLabelIfChanged(req.ObjName, req.NewText)
	return &pb.Empty{}, nil
}

func (s *grpcServer) UpdateLabelAndChangeVisibility(ctx context.Context, req *pb.UpdateLabelAndChangeVisibilityRequest) (*pb.Empty, error) {
	s.native.UpdateLabelAndChangeVisibility(req.ObjName, req.NewText)
	return &pb.Empty{}, nil
}

func (s *grpcServer) SwitchToScreenIf(ctx context.Context, req *pb.SwitchToScreenIfRequest) (*pb.Empty, error) {
	s.native.SwitchToScreenIf(req.ScreenName, req.ShouldSwitch)
	return &pb.Empty{}, nil
}

func (s *grpcServer) SwitchToScreenIfDifferent(ctx context.Context, req *pb.SwitchToScreenIfDifferentRequest) (*pb.Empty, error) {
	s.native.SwitchToScreenIfDifferent(req.ScreenName)
	return &pb.Empty{}, nil
}

func (s *grpcServer) DoNotUseThisIsForCrashTestingOnly(ctx context.Context, req *pb.Empty) (*pb.Empty, error) {
	s.native.DoNotUseThisIsForCrashTestingOnly()
	return &pb.Empty{}, nil
}
