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
	conn   *grpc.ClientConn
	client pb.NativeServiceClient
	logger *zerolog.Logger

	eventStream pb.NativeService_StreamEventsClient
	eventM      sync.RWMutex
	eventCh     chan *pb.Event
	eventDone   chan struct{}

	closed bool
	closeM sync.Mutex
}

// NewGRPCClient creates a new gRPC client connected to the native service
func NewGRPCClient(socketPath string, logger *zerolog.Logger) (*GRPCClient, error) {
	// Connect to the Unix domain socket
	conn, err := grpc.NewClient(
		fmt.Sprintf("unix-abstract:%v", socketPath),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to gRPC server: %w", err)
	}

	client := pb.NewNativeServiceClient(conn)

	grpcClient := &GRPCClient{
		conn:      conn,
		client:    client,
		logger:    logger,
		eventCh:   make(chan *pb.Event, 100),
		eventDone: make(chan struct{}),
	}

	// Start event stream
	go grpcClient.startEventStream()

	return grpcClient, nil
}

func (c *GRPCClient) startEventStream() {
	for {
		c.closeM.Lock()
		if c.closed {
			c.closeM.Unlock()
			return
		}
		c.closeM.Unlock()

		ctx := context.Background()
		stream, err := c.client.StreamEvents(ctx, &pb.Empty{})
		if err != nil {
			c.logger.Warn().Err(err).Msg("failed to start event stream, retrying...")
			time.Sleep(1 * time.Second)
			continue
		}

		c.eventM.Lock()
		c.eventStream = stream
		c.eventM.Unlock()

		for {
			event, err := stream.Recv()
			if err == io.EOF {
				c.logger.Debug().Msg("event stream closed")
				break
			}
			if err != nil {
				c.logger.Warn().Err(err).Msg("event stream error")
				break
			}

			c.logger.Info().Str("type", event.Type).Msg("received event")

			select {
			case c.eventCh <- event:
			default:
				c.logger.Warn().Msg("event channel full, dropping event")
			}
		}

		c.eventM.Lock()
		c.eventStream = nil
		c.eventM.Unlock()

		// Wait before retrying
		time.Sleep(1 * time.Second)
	}
}

func (c *GRPCClient) checkIsReady(ctx context.Context) error {
	c.logger.Info().Msg("connection is idle, connecting...")
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
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	for {
		state := c.conn.GetState()
		if state == connectivity.Idle || state == connectivity.Ready {
			if err := c.checkIsReady(ctx); err != nil {
				time.Sleep(1 * time.Second)
				continue
			}
		}

		c.logger.Info().Str("state", state.String()).Msg("waiting for connection to be ready")
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

// OnEvent registers an event handler
func (c *GRPCClient) OnEvent(eventType string, handler func(data interface{})) {
	return
	// go func() {
	// 	for {
	// 		select {
	// 		case event := <-c.eventCh:
	// 			if event.Type == eventType {
	// 				var data interface{}
	// 				switch eventType {
	// 				case "video_state_change":
	// 					if event.VideoState != nil {
	// 						data = VideoState{
	// 							Ready:          event.VideoState.Ready,
	// 							Error:          event.VideoState.Error,
	// 							Width:          int(event.VideoState.Width),
	// 							Height:         int(event.VideoState.Height),
	// 							FramePerSecond: event.VideoState.FramePerSecond,
	// 						}
	// 					}
	// 				case "indev_event":
	// 					data = event.IndevEvent
	// 				case "rpc_event":
	// 					data = event.RpcEvent
	// 				case "video_frame":
	// 					if event.VideoFrame != nil {
	// 						data = map[string]interface{}{
	// 							"frame":    event.VideoFrame.Frame,
	// 							"duration": time.Duration(event.VideoFrame.DurationNs),
	// 						}
	// 					}
	// 				}
	// 				if data != nil {
	// 					handler(data)
	// 				}
	// 			}
	// 		case <-c.eventDone:
	// 			return
	// 		}
	// 	}
	// }()
}

// Close closes the gRPC client
func (c *GRPCClient) Close() error {
	c.closeM.Lock()
	defer c.closeM.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true

	close(c.eventDone)

	c.eventM.Lock()
	if c.eventStream != nil {
		c.eventStream.CloseSend()
	}
	c.eventM.Unlock()

	return c.conn.Close()
}

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
