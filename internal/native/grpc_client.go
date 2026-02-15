package native

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	pb "github.com/jetkvm/kvm/internal/native/proto"
)

// GRPCClient wraps the gRPC client for the native service
type GRPCClient struct {
	ctx    context.Context
	cancel context.CancelFunc

	conn   *grpc.ClientConn
	client pb.NativeServiceClient
	logger *zerolog.Logger

	eventStream pb.NativeService_StreamEventsClient
	eventM      sync.RWMutex
	eventCh     chan *pb.Event
	eventDone   chan struct{}

	onVideoStateChange func(state VideoState)
	onIndevEvent       func(event string)
	onRpcEvent         func(event string)

	closed bool
	closeM sync.Mutex
}

type grpcClientOptions struct {
	SocketPath         string
	Logger             *zerolog.Logger
	OnVideoStateChange func(state VideoState)
	OnIndevEvent       func(event string)
	OnRpcEvent         func(event string)
}

// NewGRPCClient creates a new gRPC client connected to the native service
func NewGRPCClient(opts grpcClientOptions) (*GRPCClient, error) {
	// Connect to the Unix domain socket
	conn, err := grpc.NewClient(
		fmt.Sprintf("unix-abstract:%v", opts.SocketPath),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to gRPC server: %w", err)
	}

	client := pb.NewNativeServiceClient(conn)

	ctx, cancel := context.WithCancel(context.Background())

	grpcClient := &GRPCClient{
		ctx:                ctx,
		cancel:             cancel,
		conn:               conn,
		client:             client,
		logger:             opts.Logger,
		eventCh:            make(chan *pb.Event, 100),
		eventDone:          make(chan struct{}),
		onVideoStateChange: opts.OnVideoStateChange,
		onIndevEvent:       opts.OnIndevEvent,
		onRpcEvent:         opts.OnRpcEvent,
	}

	// Start event stream
	go grpcClient.startEventStream()

	// Start event handler to process events from the channel
	go func() {
		for {
			select {
			case event := <-grpcClient.eventCh:
				grpcClient.handleEvent(event)
			case <-grpcClient.eventDone:
				return
			}
		}
	}()

	return grpcClient, nil
}

func (c *GRPCClient) handleEventStream(stream pb.NativeService_StreamEventsClient) {
	c.eventM.Lock()
	c.eventStream = stream
	defer func() {
		c.eventStream = nil
		c.eventM.Unlock()
	}()

	for {
		logger := c.logger.With().Interface("stream", stream).Logger()
		if stream == nil {
			logger.Error().Msg("event stream is nil")
			break
		}

		event, err := stream.Recv()

		if err != nil {
			if errors.Is(err, io.EOF) {
				logger.Debug().Msg("event stream closed")
			} else {
				logger.Warn().Err(err).Msg("event stream error")
			}
			break
		}

		// enrich the logger with the event type and data, if debug mode is enabled
		if c.logger.GetLevel() <= zerolog.DebugLevel {
			logger = logger.With().
				Str("type", event.Type).
				Interface("data", event.Data).
				Logger()
		}
		logger.Trace().Msg("received event")

		select {
		case c.eventCh <- event:
		default:
			logger.Warn().Msg("event channel full, dropping event")
		}
	}
}

func (c *GRPCClient) startEventStream() {
	for {
		// check if the client is closed
		c.closeM.Lock()
		if c.closed {
			c.closeM.Unlock()
			return
		}
		c.closeM.Unlock()

		// check if the context is done
		select {
		case <-c.ctx.Done():
			c.logger.Info().Msg("event stream context done, closing")
			return
		default:
		}

		stream, err := c.client.StreamEvents(c.ctx, &pb.Empty{})
		if err != nil {
			c.logger.Warn().Err(err).Msg("failed to start event stream, retrying ...")
			time.Sleep(5 * time.Second)
			continue
		}

		c.handleEventStream(stream)

		// Wait before retrying
		time.Sleep(1 * time.Second)
	}
}

func (c *GRPCClient) checkIsReady(ctx context.Context) error {
	c.logger.Trace().Msg("connection is idle, connecting ...")

	resp, err := c.client.IsReady(ctx, &pb.IsReadyRequest{})
	if err != nil {
		if errors.Is(err, status.Error(codes.Unavailable, "")) {
			return fmt.Errorf("timeout waiting for ready: %w", err)
		}
		return fmt.Errorf("failed to check if ready: %w", err)
	}
	if resp.Ready {
		return nil
	}
	return nil
}

// WaitReady waits for the gRPC connection to be ready
func (c *GRPCClient) WaitReady() error {
	ctx, cancel := context.WithTimeout(c.ctx, 60*time.Second)
	defer cancel()

	prevState := connectivity.Idle
	for {
		state := c.conn.GetState()
		c.logger.
			With().
			Str("state", state.String()).
			Int("prev_state", int(prevState)).
			Logger()

		prevState = state
		if state == connectivity.Idle || state == connectivity.Ready {
			if err := c.checkIsReady(ctx); err != nil {
				time.Sleep(1 * time.Second)
				continue
			}
		}

		c.logger.Info().Msg("waiting for connection to be ready")

		if state == connectivity.Ready {
			return nil
		}
		if state == connectivity.Shutdown {
			return fmt.Errorf("connection failed: %v", state)
		}

		if !c.conn.WaitForStateChange(ctx, state) {
			return ctx.Err()
		}
	}
}

func (c *GRPCClient) handleEvent(event *pb.Event) {
	switch event.Type {
	case "video_state_change":
		state := event.GetVideoState()
		if state == nil {
			c.logger.Warn().Msg("video state event is nil")
			return
		}
		c.onVideoStateChange(VideoState{
			Ready:          state.Ready,
			Error:          state.Error,
			Width:          int(state.Width),
			Height:         int(state.Height),
			FramePerSecond: state.FramePerSecond,
		})
	case "indev_event":
		c.onIndevEvent(event.GetIndevEvent())
	case "rpc_event":
		c.onRpcEvent(event.GetRpcEvent())
	default:
		c.logger.Warn().Str("type", event.Type).Msg("unknown event type")
	}
}

// Close closes the gRPC client
func (c *GRPCClient) Close() error {
	c.closeM.Lock()
	defer c.closeM.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true

	// cancel all ongoing operations
	c.cancel()

	close(c.eventDone)

	c.eventM.Lock()
	if c.eventStream != nil {
		if err := c.eventStream.CloseSend(); err != nil {
			c.logger.Warn().Err(err).Msg("failed to close event stream")
		}
	}
	c.eventM.Unlock()

	return c.conn.Close()
}
