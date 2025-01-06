package plugin

import (
	"context"
	"errors"
	"fmt"
	"kvm/internal/jsonrpc"
	"log"
	"net"
	"os"
	"path"
	"slices"
	"time"
)

type PluginRpcStatus struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

var (
	PluginRpcStatusDisconnected         = PluginRpcStatus{"disconnected", ""}
	PluginRpcStatusUnknown              = PluginRpcStatus{"unknown", ""}
	PluginRpcStatusLoading              = PluginRpcStatus{"loading", ""}
	PluginRpcStatusPendingConfiguration = PluginRpcStatus{"pending-configuration", ""}
	PluginRpcStatusRunning              = PluginRpcStatus{"running", ""}
	PluginRpcStatusError                = PluginRpcStatus{"error", ""}
)

type PluginRpcSupportedMethods struct {
	SupportedRpcMethods []string `json:"supported_rpc_methods"`
}

type PluginRpcServer struct {
	install    *PluginInstall
	workingDir string

	listener net.Listener
	status   PluginRpcStatus
}

func NewPluginRpcServer(install *PluginInstall, workingDir string) *PluginRpcServer {
	return &PluginRpcServer{
		install:    install,
		workingDir: workingDir,
		status:     PluginRpcStatusDisconnected,
	}
}

func (s *PluginRpcServer) Start() error {
	socketPath := s.SocketPath()
	_ = os.Remove(socketPath)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("failed to listen on socket: %v", err)
	}
	s.listener = listener

	s.status = PluginRpcStatusDisconnected
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				// If the error indicates the listener is closed, break out
				if opErr, ok := err.(*net.OpError); ok && opErr.Err.Error() == "use of closed network connection" {
					log.Println("Listener closed, exiting accept loop.")
					return
				}

				log.Printf("Failed to accept connection: %v", err)
				continue
			}
			log.Printf("Accepted plugin rpc connection from %v", conn.RemoteAddr())

			go s.handleConnection(conn)
		}
	}()

	return nil
}

func (s *PluginRpcServer) Stop() error {
	if s.listener != nil {
		s.status = PluginRpcStatusDisconnected
		return s.listener.Close()
	}
	return nil
}

func (s *PluginRpcServer) Status() PluginRpcStatus {
	return s.status
}

func (s *PluginRpcServer) SocketPath() string {
	return path.Join(s.workingDir, "plugin.sock")
}

func (s *PluginRpcServer) handleConnection(conn net.Conn) {
	rpcserver := jsonrpc.NewJSONRPCServer(conn, map[string]*jsonrpc.RPCHandler{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go s.handleRpcStatus(ctx, rpcserver)

	// Read from the conn and write into rpcserver.HandleMessage
	buf := make([]byte, 65*1024)
	for {
		// TODO: if read 65k bytes, then likey there is more data to read... figure out how to handle this
		n, err := conn.Read(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				s.status = PluginRpcStatusDisconnected
			} else {
				log.Printf("Failed to read message: %v", err)
				s.status = PluginRpcStatusError
				s.status.Message = fmt.Errorf("failed to read message: %v", err).Error()
			}
			break
		}

		err = rpcserver.HandleMessage(buf[:n])
		if err != nil {
			log.Printf("Failed to handle message: %v", err)
			s.status = PluginRpcStatusError
			s.status.Message = fmt.Errorf("failed to handle message: %v", err).Error()
			continue
		}
	}
}

func (s *PluginRpcServer) handleRpcStatus(ctx context.Context, rpcserver *jsonrpc.JSONRPCServer) {
	s.status = PluginRpcStatusUnknown

	log.Printf("Plugin rpc server started. Getting supported methods...")
	var supportedMethodsResponse PluginRpcSupportedMethods
	err := rpcserver.Request("getPluginSupportedMethods", nil, &supportedMethodsResponse)
	if err != nil {
		log.Printf("Failed to get supported methods: %v", err)
		s.status = PluginRpcStatusError
		s.status.Message = fmt.Errorf("error getting supported methods: %v", err.Message).Error()
	}

	log.Printf("Plugin has supported methods: %v", supportedMethodsResponse.SupportedRpcMethods)

	if !slices.Contains(supportedMethodsResponse.SupportedRpcMethods, "getPluginStatus") {
		log.Printf("Plugin does not support getPluginStatus method")
		return
	}

	ticker := time.NewTicker(1 * time.Second)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			var statusResponse PluginRpcStatus
			err := rpcserver.Request("getPluginStatus", nil, &statusResponse)
			if err != nil {
				log.Printf("Failed to get status: %v", err)
				if err, ok := err.Data.(error); ok && errors.Is(err, net.ErrClosed) {
					s.status = PluginRpcStatusDisconnected
					break
				}

				s.status = PluginRpcStatusError
				s.status.Message = fmt.Errorf("error getting status: %v", err).Error()
				continue
			}

			s.status = statusResponse
		}
	}
}
