package kvm

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/jetkvm/kvm/resource"
	"github.com/rs/zerolog"

	"github.com/pion/webrtc/v4/pkg/media"
)

var ctrlSocketConn net.Conn

type CtrlAction struct {
	Action string                 `json:"action"`
	Seq    int32                  `json:"seq,omitempty"`
	Params map[string]interface{} `json:"params,omitempty"`
}

type CtrlResponse struct {
	Seq    int32                  `json:"seq,omitempty"`
	Error  string                 `json:"error,omitempty"`
	Errno  int32                  `json:"errno,omitempty"`
	Result map[string]interface{} `json:"result,omitempty"`
	Event  string                 `json:"event,omitempty"`
	Data   json.RawMessage        `json:"data,omitempty"`
}

type nativeOutput struct {
	mu     *sync.Mutex
	logger *zerolog.Event
}

func (w *nativeOutput) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.logger.Msg(string(p))
	return len(p), nil
}

type EventHandler func(event CtrlResponse)

var seq int32 = 1

var ongoingRequests = make(map[int32]chan *CtrlResponse)

var lock = &sync.Mutex{}

func CallCtrlAction(action string, params map[string]interface{}) (*CtrlResponse, error) {
	lock.Lock()
	defer lock.Unlock()
	ctrlAction := CtrlAction{
		Action: action,
		Seq:    seq,
		Params: params,
	}

	responseChan := make(chan *CtrlResponse)
	ongoingRequests[seq] = responseChan
	seq++

	jsonData, err := json.Marshal(ctrlAction)
	if err != nil {
		delete(ongoingRequests, ctrlAction.Seq)
		return nil, fmt.Errorf("error marshaling ctrl action: %w", err)
	}

	scopedLogger := nativeLogger.With().
		Str("action", ctrlAction.Action).
		Interface("params", ctrlAction.Params).Logger()

	scopedLogger.Debug().Msg("sending ctrl action")

	err = WriteCtrlMessage(jsonData)
	if err != nil {
		delete(ongoingRequests, ctrlAction.Seq)
		return nil, ErrorfL(&scopedLogger, "error writing ctrl message", err)
	}

	select {
	case response := <-responseChan:
		delete(ongoingRequests, seq)
		if response.Error != "" {
			return nil, ErrorfL(
				&scopedLogger,
				"error native response: %s",
				errors.New(response.Error),
			)
		}
		return response, nil
	case <-time.After(5 * time.Second):
		close(responseChan)
		delete(ongoingRequests, seq)
		return nil, ErrorfL(&scopedLogger, "timeout waiting for response", nil)
	}
}

func WriteCtrlMessage(message []byte) error {
	if ctrlSocketConn == nil {
		return fmt.Errorf("ctrl socket not conn ected")
	}
	_, err := ctrlSocketConn.Write(message)
	return err
}

var nativeCtrlSocketListener net.Listener  //nolint:unused
var nativeVideoSocketListener net.Listener //nolint:unused

var ctrlClientConnected = make(chan struct{})

func waitCtrlClientConnected() {
	<-ctrlClientConnected
}

func StartNativeSocketServer(socketPath string, handleClient func(net.Conn), isCtrl bool) net.Listener {
	scopedLogger := nativeLogger.With().
		Str("socket_path", socketPath).
		Logger()

	// Remove the socket file if it already exists
	if _, err := os.Stat(socketPath); err == nil {
		if err := os.Remove(socketPath); err != nil {
			scopedLogger.Warn().Err(err).Msg("failed to remove existing socket file")
			os.Exit(1)
		}
	}

	listener, err := net.Listen("unixpacket", socketPath)
	if err != nil {
		scopedLogger.Warn().Err(err).Msg("failed to start server")
		os.Exit(1)
	}

	scopedLogger.Info().Msg("server listening")

	go func() {
		conn, err := listener.Accept()
		listener.Close()
		if err != nil {
			scopedLogger.Warn().Err(err).Msg("failed to accept socket")
		}
		if isCtrl {
			close(ctrlClientConnected)
			scopedLogger.Debug().Msg("first native ctrl socket client connected")
		}
		handleClient(conn)
	}()

	return listener
}

func StartNativeCtrlSocketServer() {
	nativeCtrlSocketListener = StartNativeSocketServer("/var/run/jetkvm_ctrl.sock", handleCtrlClient, true)
	nativeLogger.Debug().Msg("native app ctrl sock started")
}

func StartNativeVideoSocketServer() {
	nativeVideoSocketListener = StartNativeSocketServer("/var/run/jetkvm_video.sock", handleVideoClient, false)
	nativeLogger.Debug().Msg("native app video sock started")
}

func handleCtrlClient(conn net.Conn) {
	defer conn.Close()

	scopedLogger := nativeLogger.With().
		Str("addr", conn.RemoteAddr().String()).
		Str("type", "ctrl").
		Logger()

	scopedLogger.Info().Msg("native ctrl socket client connected")
	if ctrlSocketConn != nil {
		scopedLogger.Debug().Msg("closing existing native socket connection")
		ctrlSocketConn.Close()
	}

	ctrlSocketConn = conn

	// Restore HDMI EDID if applicable
	go restoreHdmiEdid()

	readBuf := make([]byte, 4096)
	for {
		n, err := conn.Read(readBuf)
		if err != nil {
			scopedLogger.Warn().Err(err).Msg("error reading from ctrl sock")
			break
		}
		readMsg := string(readBuf[:n])

		ctrlResp := CtrlResponse{}
		err = json.Unmarshal([]byte(readMsg), &ctrlResp)
		if err != nil {
			scopedLogger.Warn().Err(err).Str("data", readMsg).Msg("error parsing ctrl sock msg")
			continue
		}
		scopedLogger.Trace().Interface("data", ctrlResp).Msg("ctrl sock msg")

		if ctrlResp.Seq != 0 {
			responseChan, ok := ongoingRequests[ctrlResp.Seq]
			if ok {
				responseChan <- &ctrlResp
			}
		}
		switch ctrlResp.Event {
		case "video_input_state":
			HandleVideoStateMessage(ctrlResp)
		}
	}

	scopedLogger.Debug().Msg("ctrl sock disconnected")
}

func handleVideoClient(conn net.Conn) {
	defer conn.Close()

	scopedLogger := nativeLogger.With().
		Str("addr", conn.RemoteAddr().String()).
		Str("type", "video").
		Logger()

	scopedLogger.Info().Msg("native video socket client connected")

	inboundPacket := make([]byte, maxFrameSize)
	lastFrame := time.Now()
	for {
		n, err := conn.Read(inboundPacket)
		if err != nil {
			scopedLogger.Warn().Err(err).Msg("error during read")
			return
		}
		now := time.Now()
		sinceLastFrame := now.Sub(lastFrame)
		lastFrame = now
		if currentSession != nil {
			err := currentSession.VideoTrack.WriteSample(media.Sample{Data: inboundPacket[:n], Duration: sinceLastFrame})
			if err != nil {
				scopedLogger.Warn().Err(err).Msg("error writing sample")
			}
		}
	}
}

func ExtractAndRunNativeBin() error {
	binaryPath := "/userdata/jetkvm/bin/jetkvm_native"
	if err := ensureBinaryUpdated(binaryPath); err != nil {
		return fmt.Errorf("failed to extract binary: %w", err)
	}

	// Make the binary executable
	if err := os.Chmod(binaryPath, 0755); err != nil {
		return fmt.Errorf("failed to make binary executable: %w", err)
	}
	// Run the binary in the background
	cmd := exec.Command(binaryPath)

	nativeOutputLock := sync.Mutex{}
	nativeStdout := &nativeOutput{
		mu:     &nativeOutputLock,
		logger: nativeLogger.Info().Str("pipe", "stdout"),
	}
	nativeStderr := &nativeOutput{
		mu:     &nativeOutputLock,
		logger: nativeLogger.Info().Str("pipe", "stderr"),
	}

	// Redirect stdout and stderr to the current process
	cmd.Stdout = nativeStdout
	cmd.Stderr = nativeStderr

	// Set the process group ID so we can kill the process and its children when this process exits
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid:   true,
		Pdeathsig: syscall.SIGKILL,
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start binary: %w", err)
	}

	//TODO: add auto restart
	go func() {
		<-appCtx.Done()
		nativeLogger.Info().Int("pid", cmd.Process.Pid).Msg("killing process")
		err := cmd.Process.Kill()
		if err != nil {
			nativeLogger.Warn().Err(err).Msg("failed to kill process")
			return
		}
	}()

	nativeLogger.Info().Int("pid", cmd.Process.Pid).Msg("jetkvm_native binary started")

	return nil
}

func shouldOverwrite(destPath string, srcHash []byte) bool {
	if srcHash == nil {
		nativeLogger.Debug().Msg("error reading embedded jetkvm_native.sha256, doing overwriting")
		return true
	}

	dstHash, err := os.ReadFile(destPath + ".sha256")
	if err != nil {
		nativeLogger.Debug().Msg("error reading existing jetkvm_native.sha256, doing overwriting")
		return true
	}

	return !bytes.Equal(srcHash, dstHash)
}

func ensureBinaryUpdated(destPath string) error {
	srcFile, err := resource.ResourceFS.Open("jetkvm_native")
	if err != nil {
		return err
	}
	defer srcFile.Close()

	srcHash, err := resource.ResourceFS.ReadFile("jetkvm_native.sha256")
	if err != nil {
		nativeLogger.Debug().Msg("error reading embedded jetkvm_native.sha256, proceeding with update")
		srcHash = nil
	}

	_, err = os.Stat(destPath)
	if shouldOverwrite(destPath, srcHash) || err != nil {
		nativeLogger.Info().Msg("writing jetkvm_native")
		_ = os.Remove(destPath)
		destFile, err := os.OpenFile(destPath, os.O_CREATE|os.O_RDWR, 0755)
		if err != nil {
			return err
		}
		_, err = io.Copy(destFile, srcFile)
		destFile.Close()
		if err != nil {
			return err
		}
		if srcHash != nil {
			err = os.WriteFile(destPath+".sha256", srcHash, 0644)
			if err != nil {
				return err
			}
		}
		nativeLogger.Info().Msg("jetkvm_native updated")
	}

	return nil
}

// Restore the HDMI EDID value from the config.
// Called after successful connection to jetkvm_native.
func restoreHdmiEdid() {
	if config.EdidString != "" {
		nativeLogger.Info().Str("edid", config.EdidString).Msg("Restoring HDMI EDID")
		_, err := CallCtrlAction("set_edid", map[string]interface{}{"edid": config.EdidString})
		if err != nil {
			nativeLogger.Warn().Err(err).Msg("Failed to restore HDMI EDID")
		}
	}
}
