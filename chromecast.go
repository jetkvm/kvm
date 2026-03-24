package kvm

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/jetkvm/kvm/internal/sync"
	"github.com/vishen/go-chromecast/cast"
	pb "github.com/vishen/go-chromecast/cast/proto"
	castdns "github.com/vishen/go-chromecast/dns"
)

// ---------------------------------------------------------------------------
// bufferedConn wraps cast.Connection with a buffered message channel to
// prevent the unbuffered recvMsgChan from deadlocking the receiveLoop.
// ---------------------------------------------------------------------------

type bufferedConn struct {
	inner      *cast.Connection
	bufferedCh chan *pb.CastMessage
}

func newBufferedConn() *bufferedConn {
	return &bufferedConn{
		inner: cast.NewConnection(),
	}
}

func (b *bufferedConn) Start(addr string, port int) error {
	if err := b.inner.Start(addr, port); err != nil {
		return err
	}
	// Pump from the unbuffered channel to a buffered one so receiveLoop never blocks
	b.bufferedCh = make(chan *pb.CastMessage, 32)
	go func() {
		for msg := range b.inner.MsgChan() {
			select {
			case b.bufferedCh <- msg:
			default:
				// Drop if buffer full (shouldn't happen)
			}
		}
		close(b.bufferedCh)
	}()
	return nil
}

func (b *bufferedConn) Send(requestID int, payload cast.Payload, sourceID, destinationID, namespace string) error {
	return b.inner.Send(requestID, payload, sourceID, destinationID, namespace)
}

func (b *bufferedConn) MsgChan() chan *pb.CastMessage { return b.bufferedCh }
func (b *bufferedConn) Close() error                   { return b.inner.Close() }

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	jetkvmNamespace = "urn:x-cast:com.jetkvm.cast"
	nsConnection        = "urn:x-cast:com.google.cast.tp.connection"
	nsReceiver          = "urn:x-cast:com.google.cast.receiver"
	castSender          = "sender-0"
	castReceiver        = "receiver-0"
)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

type ChromecastDevice struct {
	Name    string `json:"name"`
	UUID    string `json:"uuid"`
	Address string `json:"address"`
	Port    int    `json:"port"`
}

type CastStatus struct {
	Active     bool   `json:"active"`
	DeviceName string `json:"deviceName,omitempty"`
	Error      string `json:"error,omitempty"`
}

// castMessage is a simple payload for custom namespace messages.
type castMessage struct {
	Type string `json:"type"`
	IP   string `json:"ip,omitempty"`
}

func (m *castMessage) SetRequestId(_ int) {}

// receiverStatus is the subset of RECEIVER_STATUS we care about.
type receiverStatus struct {
	Status struct {
		Applications []struct {
			AppId       string `json:"appId"`
			TransportId string `json:"transportId"`
		} `json:"applications"`
	} `json:"status"`
}

// ---------------------------------------------------------------------------
// State
// ---------------------------------------------------------------------------

var castState struct {
	mu         sync.Mutex
	active     bool
	deviceName string
	conn       *bufferedConn
}

// ---------------------------------------------------------------------------
// mDNS discovery
// ---------------------------------------------------------------------------

func rpcDiscoverChromecasts() ([]ChromecastDevice, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	entryCh, err := castdns.DiscoverCastDNSEntries(ctx, nil)
	if err != nil {
		chromecastLogger.Warn().Err(err).Msg("mDNS discovery failed")
		return nil, fmt.Errorf("mDNS discovery failed: %w", err)
	}

	var devices []ChromecastDevice
	for entry := range entryCh {
		devices = append(devices, ChromecastDevice{
			Name:    entry.GetName(),
			UUID:    entry.GetUUID(),
			Address: entry.GetAddr(),
			Port:    entry.GetPort(),
		})
	}

	chromecastLogger.Info().Int("count", len(devices)).Msg("Chromecast discovery complete")
	return devices, nil
}

// ---------------------------------------------------------------------------
// Cast control — WebRTC via custom receiver
// ---------------------------------------------------------------------------

func getDeviceLocalIP() (string, error) {
	state := rpcGetNetworkState()
	if state != nil && state.IPv4Address != "" {
		return state.IPv4Address, nil
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "", err
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && ipnet.IP.To4() != nil {
			return ipnet.IP.String(), nil
		}
	}
	return "", fmt.Errorf("no suitable local IP found")
}

func rpcStartCasting(address string, port int) error {
	castState.mu.Lock()
	defer castState.mu.Unlock()

	if castState.active {
		return fmt.Errorf("already casting")
	}

	localIP, err := getDeviceLocalIP()
	if err != nil {
		return fmt.Errorf("cannot determine local IP: %w", err)
	}

	chromecastLogger.Info().Str("address", address).Int("port", port).Msg("starting cast")

	// Connect to Chromecast with buffered message channel
	conn := newBufferedConn()
	if err := conn.Start(address, port); err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}

	msgCh := conn.MsgChan()
	transportCh := make(chan string, 1)
	errCh := make(chan error, 1)

	go func() {
		timeout := time.After(15 * time.Second)
		for {
			select {
			case msg, ok := <-msgCh:
				if !ok {
					errCh <- fmt.Errorf("message channel closed")
					return
				}
				if msg.PayloadUtf8 == nil {
					continue
				}
				payload := *msg.PayloadUtf8
				chromecastLogger.Info().Str("payload", payload).Msg("received cast message")

				var generic map[string]interface{}
				if err := json.Unmarshal([]byte(payload), &generic); err != nil {
					continue
				}
				msgType, _ := generic["type"].(string)

				switch msgType {
				case "LAUNCH_ERROR":
					reason, _ := generic["reason"].(string)
					errCh <- fmt.Errorf("LAUNCH_ERROR: %s (is the device registered as a test device?)", reason)
					return

				case "LAUNCH_STATUS":
					// App is launching — poll for updated status after a brief delay
					go func() {
						time.Sleep(2 * time.Second)
						poll := cast.GetStatusHeader
						poll.SetRequestId(10)
						conn.Send(10, &poll, castSender, castReceiver, nsReceiver) //nolint:errcheck
					}()

				case "RECEIVER_STATUS":
					var status receiverStatus
					if err := json.Unmarshal([]byte(payload), &status); err != nil {
						continue
					}
					for _, app := range status.Status.Applications {
						if app.AppId == config.CastReceiverAppID {
							transportCh <- app.TransportId
							return
						}
					}
				}
			case <-timeout:
				errCh <- fmt.Errorf("timeout waiting for custom receiver to start")
				return
			}
		}
	}()

	// Send CONNECT to the receiver platform
	chromecastLogger.Info().Msg("sending CONNECT")
	connectHeader := cast.ConnectHeader // local copy
	if err := conn.Send(1, &connectHeader, castSender, castReceiver, nsConnection); err != nil {
		conn.Close() //nolint:errcheck
		return fmt.Errorf("CONNECT failed: %w", err)
	}

	// Request initial status (triggers Chromecast to send RECEIVER_STATUS)
	getStatus := cast.GetStatusHeader // local copy
	getStatus.SetRequestId(2)
	if err := conn.Send(2, &getStatus, castSender, castReceiver, nsReceiver); err != nil {
		conn.Close() //nolint:errcheck
		return fmt.Errorf("GET_STATUS failed: %w", err)
	}

	// Launch the custom receiver app
	chromecastLogger.Info().Str("appId", config.CastReceiverAppID).Msg("sending LAUNCH")
	launch := cast.LaunchRequest{
		PayloadHeader: cast.LaunchHeader,
		AppId:         config.CastReceiverAppID,
	}
	launch.SetRequestId(3)
	if err := conn.Send(3, &launch, castSender, castReceiver, nsReceiver); err != nil {
		conn.Close() //nolint:errcheck
		return fmt.Errorf("LAUNCH failed: %w", err)
	}

	// Wait for the custom receiver to start
	var transportID string
	select {
	case transportID = <-transportCh:
		chromecastLogger.Info().Str("transportId", transportID).Msg("custom receiver launched")
	case err := <-errCh:
		conn.Close() //nolint:errcheck
		return fmt.Errorf("failed to launch receiver: %w", err)
	}

	// Connect to the app's transport
	if err := conn.Send(3, &cast.ConnectHeader, castSender, transportID, nsConnection); err != nil {
		conn.Close() //nolint:errcheck
		return fmt.Errorf("transport CONNECT failed: %w", err)
	}

	// Send the JetKVM IP to the receiver so it can establish WebRTC
	chromecastLogger.Info().Str("ip", localIP).Msg("sending JetKVM IP to receiver")
	if err := conn.Send(4, &castMessage{
		Type: "connect",
		IP:   localIP,
	}, castSender, transportID, jetkvmNamespace); err != nil {
		conn.Close() //nolint:errcheck
		return fmt.Errorf("failed to send connect message: %w", err)
	}

	// Keep connection alive (receiveLoop handles PING/PONG automatically)
	castState.active = true
	castState.deviceName = fmt.Sprintf("%s:%d", address, port)
	castState.conn = conn

	chromecastLogger.Info().Str("device", castState.deviceName).Msg("casting started (WebRTC)")
	return nil
}

func rpcStopCasting() error {
	castState.mu.Lock()
	defer castState.mu.Unlock()

	if !castState.active {
		return nil
	}

	if castState.conn != nil {
		// Send stop message to receiver (best-effort)
		_ = castState.conn.Send(0, &cast.StopHeader, castSender, castReceiver, nsReceiver)
		_ = castState.conn.Close()
		castState.conn = nil
	}

	castState.active = false
	castState.deviceName = ""

	chromecastLogger.Info().Msg("casting stopped")
	return nil
}

func rpcGetCastingStatus() CastStatus {
	castState.mu.Lock()
	defer castState.mu.Unlock()

	return CastStatus{
		Active:     castState.active,
		DeviceName: castState.deviceName,
	}
}

type CastConfig struct {
	ReceiverAppID string `json:"receiverAppId"`
}

func rpcGetCastConfig() CastConfig {
	return CastConfig{
		ReceiverAppID: config.CastReceiverAppID,
	}
}

func rpcSetCastConfig(receiverAppID string) error {
	if receiverAppID == "" {
		return fmt.Errorf("receiver app ID cannot be empty")
	}
	old := config.CastReceiverAppID
	config.CastReceiverAppID = receiverAppID
	if err := SaveConfig(); err != nil {
		config.CastReceiverAppID = old
		return fmt.Errorf("failed to save config: %w", err)
	}
	return nil
}
