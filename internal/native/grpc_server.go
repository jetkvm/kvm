package native

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jetkvm/kvm/internal/native/proto"
)

// grpcServer wraps the Native instance and implements the gRPC service
type grpcServer struct {
	pb.UnimplementedNativeServiceServer
	native   *Native
	logger   *zerolog.Logger
	eventChs []chan *pb.Event
	eventM   sync.Mutex
}

// NewGRPCServer creates a new gRPC server for the native service
func NewGRPCServer(n *Native, logger *zerolog.Logger) *grpcServer {
	s := &grpcServer{
		native:   n,
		logger:   logger,
		eventChs: make([]chan *pb.Event, 0),
	}

	// Store original callbacks and wrap them to also broadcast events
	originalVideoStateChange := n.onVideoStateChange
	originalIndevEvent := n.onIndevEvent
	originalRpcEvent := n.onRpcEvent
	originalVideoFrameReceived := n.onVideoFrameReceived

	// Wrap callbacks to both call original and broadcast events
	n.onVideoStateChange = func(state VideoState) {
		if originalVideoStateChange != nil {
			originalVideoStateChange(state)
		}
		event := &pb.Event{
			Type: "video_state_change",
			Data: &pb.Event_VideoState{
				VideoState: &pb.VideoState{
					Ready:          state.Ready,
					Error:          state.Error,
					Width:          int32(state.Width),
					Height:         int32(state.Height),
					FramePerSecond: state.FramePerSecond,
				},
			},
		}
		s.broadcastEvent(event)
	}

	n.onIndevEvent = func(event string) {
		if originalIndevEvent != nil {
			originalIndevEvent(event)
		}
		s.broadcastEvent(&pb.Event{
			Type: "indev_event",
			Data: &pb.Event_IndevEvent{
				IndevEvent: event,
			},
		})
	}

	n.onRpcEvent = func(event string) {
		if originalRpcEvent != nil {
			originalRpcEvent(event)
		}
		s.broadcastEvent(&pb.Event{
			Type: "rpc_event",
			Data: &pb.Event_RpcEvent{
				RpcEvent: event,
			},
		})
	}

	n.onVideoFrameReceived = func(frame []byte, duration time.Duration) {
		if originalVideoFrameReceived != nil {
			originalVideoFrameReceived(frame, duration)
		}
		s.broadcastEvent(&pb.Event{
			Type: "video_frame",
			Data: &pb.Event_VideoFrame{
				VideoFrame: &pb.VideoFrame{
					Frame:      frame,
					DurationNs: duration.Nanoseconds(),
				},
			},
		})
	}

	return s
}

func (s *grpcServer) broadcastEvent(event *pb.Event) {
	s.eventM.Lock()
	defer s.eventM.Unlock()

	for _, ch := range s.eventChs {
		select {
		case ch <- event:
		default:
			// Channel full, skip
		}
	}
}

func (s *grpcServer) IsReady(ctx context.Context, req *pb.IsReadyRequest) (*pb.IsReadyResponse, error) {
	return &pb.IsReadyResponse{Ready: true, VideoReady: true}, nil
}

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

// StreamEvents streams events from the native process
func (s *grpcServer) StreamEvents(req *pb.Empty, stream pb.NativeService_StreamEventsServer) error {
	eventCh := make(chan *pb.Event, 100)

	// Register this channel for events
	s.eventM.Lock()
	s.eventChs = append(s.eventChs, eventCh)
	s.eventM.Unlock()

	// Unregister on exit
	defer func() {
		s.eventM.Lock()
		defer s.eventM.Unlock()
		for i, ch := range s.eventChs {
			if ch == eventCh {
				s.eventChs = append(s.eventChs[:i], s.eventChs[i+1:]...)
				break
			}
		}
		close(eventCh)
	}()

	// Stream events
	for {
		select {
		case event := <-eventCh:
			if err := stream.Send(event); err != nil {
				return err
			}
		case <-stream.Context().Done():
			return stream.Context().Err()
		}
	}
}

// StartGRPCServer starts the gRPC server on a Unix domain socket
func StartGRPCServer(server *grpcServer, socketPath string, logger *zerolog.Logger) (*grpc.Server, net.Listener, error) {

	lis, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to listen on socket: %w", err)
	}

	s := grpc.NewServer()
	pb.RegisterNativeServiceServer(s, server)

	go func() {
		if err := s.Serve(lis); err != nil {
			logger.Error().Err(err).Msg("gRPC server error")
		}
	}()

	logger.Info().Str("socket", socketPath).Msg("gRPC server started")
	return s, lis, nil
}
