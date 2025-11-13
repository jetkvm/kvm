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

// StreamEvents streams events from the native process
func (s *grpcServer) StreamEvents(req *pb.Empty, stream pb.NativeService_StreamEventsServer) error {
	setProcTitle("connected")
	defer setProcTitle("waiting")

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
