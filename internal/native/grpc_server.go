package native

import (
	"context"
	"fmt"
	"net"
	"sync"

	"github.com/rs/zerolog"
	"google.golang.org/grpc"

	pb "github.com/jetkvm/kvm/internal/native/proto"
)

// grpcServer wraps the Native instance and implements the gRPC service
type grpcServer struct {
	pb.UnimplementedNativeServiceServer
	native            *Native
	logger            *zerolog.Logger
	eventStreamChan   chan *pb.Event
	eventStreamMu     sync.Mutex
	eventStreamCtx    context.Context
	eventStreamCancel context.CancelFunc
}

// NewGRPCServer creates a new gRPC server for the native service
func NewGRPCServer(n *Native, logger *zerolog.Logger) *grpcServer {
	s := &grpcServer{
		native:          n,
		logger:          logger,
		eventStreamChan: make(chan *pb.Event, 100),
	}

	// Store original callbacks and wrap them to also broadcast events
	originalVideoStateChange := n.onVideoStateChange
	originalIndevEvent := n.onIndevEvent
	originalRpcEvent := n.onRpcEvent

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

	return s
}

func (s *grpcServer) broadcastEvent(event *pb.Event) {
	s.eventStreamChan <- event
}

func (s *grpcServer) IsReady(ctx context.Context, req *pb.IsReadyRequest) (*pb.IsReadyResponse, error) {
	return &pb.IsReadyResponse{Ready: true, VideoReady: true}, nil
}

// StreamEvents streams events from the native process
func (s *grpcServer) StreamEvents(req *pb.Empty, stream pb.NativeService_StreamEventsServer) error {
	setProcTitle("connected")
	defer setProcTitle("waiting")

	// Cancel previous stream if exists
	s.eventStreamMu.Lock()
	if s.eventStreamCancel != nil {
		s.logger.Debug().Msg("cancelling previous StreamEvents call")
		s.eventStreamCancel()
	}

	// Create a cancellable context for this stream
	ctx, cancel := context.WithCancel(stream.Context())
	s.eventStreamCtx = ctx
	s.eventStreamCancel = cancel
	s.eventStreamMu.Unlock()

	// Clean up when this stream ends
	defer func() {
		s.eventStreamMu.Lock()
		defer s.eventStreamMu.Unlock()
		if s.eventStreamCtx == ctx {
			s.eventStreamCancel = nil
			s.eventStreamCtx = nil
		}
		cancel()
	}()

	// Stream events
	for {
		select {
		case event := <-s.eventStreamChan:
			// Check if this stream is still the active one
			s.eventStreamMu.Lock()
			isActive := s.eventStreamCtx == ctx
			s.eventStreamMu.Unlock()

			if !isActive {
				s.logger.Debug().Msg("stream replaced by new call, exiting")
				return context.Canceled
			}

			if err := stream.Send(event); err != nil {
				return err
			}
		case <-ctx.Done():
			return ctx.Err()
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
