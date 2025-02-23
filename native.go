package kvm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/jetkvm/kvm/resource"

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

	fmt.Println("sending ctrl action", string(jsonData))

	err = WriteCtrlMessage(jsonData)
	if err != nil {
		delete(ongoingRequests, ctrlAction.Seq)
		return nil, fmt.Errorf("error writing ctrl message: %w", err)
	}

	select {
	case response := <-responseChan:
		delete(ongoingRequests, seq)
		if response.Error != "" {
			return nil, fmt.Errorf("error native response: %s", response.Error)
		}
		return response, nil
	case <-time.After(5 * time.Second):
		close(responseChan)
		delete(ongoingRequests, seq)
		return nil, fmt.Errorf("timeout waiting for response")
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
	// Remove the socket file if it already exists
	if _, err := os.Stat(socketPath); err == nil {
		if err := os.Remove(socketPath); err != nil {
			log.Fatalf("Failed to remove existing socket file %s: %v", socketPath, err)
		}
	}

	listener, err := net.Listen("unixpacket", socketPath)
	if err != nil {
		log.Fatalf("Failed to start server on %s: %v", socketPath, err)
	}

	log.Printf("Server listening on %s", socketPath)

	go func() {
		conn, err := listener.Accept()
		listener.Close()
		if err != nil {
			logger.Errorf("failed to accept sock: %v", err)
		}
		if isCtrl {
			close(ctrlClientConnected)
			logger.Debug("first native ctrl socket client connected")
		}
		handleClient(conn)
	}()

	return listener
}

func StartNativeCtrlSocketServer() {
	nativeCtrlSocketListener = StartNativeSocketServer("/var/run/jetkvm_ctrl.sock", handleCtrlClient, true)
	logger.Debug("native app ctrl sock started")
}

func StartNativeVideoSocketServer() {
	nativeVideoSocketListener = StartNativeSocketServer("/var/run/jetkvm_video.sock", handleVideoClient, false)
	logger.Debug("native app video sock started")
}

func handleCtrlClient(conn net.Conn) {
	defer conn.Close()

	logger.Debug("native socket client connected")
	if ctrlSocketConn != nil {
		logger.Debugf("closing existing native socket connection")
		ctrlSocketConn.Close()
	}

	ctrlSocketConn = conn

	// Restore HDMI EDID if applicable
	go restoreHdmiEdid()

	readBuf := make([]byte, 4096)
	for {
		n, err := conn.Read(readBuf)
		if err != nil {
			logger.Errorf("error reading from ctrl sock: %v", err)
			break
		}
		readMsg := string(readBuf[:n])
		logger.Tracef("ctrl sock msg: %v", readMsg)
		ctrlResp := CtrlResponse{}
		err = json.Unmarshal([]byte(readMsg), &ctrlResp)
		if err != nil {
			logger.Warnf("error parsing ctrl sock msg: %v", err)
			continue
		}
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

	logger.Debug("ctrl sock disconnected")
}

func handleVideoClient(conn net.Conn) {
	defer conn.Close()

	log.Printf("Native video socket client connected: %v", conn.RemoteAddr())

	inboundPacket := make([]byte, maxFrameSize)
	lastFrame := time.Now()
	for {
		n, err := conn.Read(inboundPacket)
		if err != nil {
			log.Printf("error during read: %s\n", err)
			return
		}
		now := time.Now()
		sinceLastFrame := now.Sub(lastFrame)
		lastFrame = now
		//fmt.Println("Video packet received", n, sinceLastFrame)
		if currentSession != nil {
			err := currentSession.VideoTrack.WriteSample(media.Sample{Data: inboundPacket[:n], Duration: sinceLastFrame})
			if err != nil {
				log.Println("Error writing sample", err)
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

	// Redirect stdout and stderr to the current process
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

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
		logger.Infof("killing process PID: %d", cmd.Process.Pid)
		err := cmd.Process.Kill()
		if err != nil {
			logger.Errorf("failed to kill process: %v", err)
			return
		}
	}()

	fmt.Printf("Binary started with PID: %d\n", cmd.Process.Pid)

	return nil
}

func shouldOverwrite(destPath string, srcHash []byte) bool {
	if srcHash == nil {
		logger.Debug("error reading embedded jetkvm_native.sha256, doing overwriting")
		return true
	}

	dstHash, err := os.ReadFile(destPath + ".sha256")
	if err != nil {
		logger.Debug("error reading existing jetkvm_native.sha256, doing overwriting")
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
		logger.Debug("error reading embedded jetkvm_native.sha256, proceeding with update")
		srcHash = nil
	}

	_, err = os.Stat(destPath)
	if shouldOverwrite(destPath, srcHash) || err != nil {
		logger.Info("writing jetkvm_native")
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
		logger.Info("jetkvm_native updated")
	}

	return nil
}

// Restore the HDMI EDID value from the config.
// Called after successful connection to jetkvm_native.
func restoreHdmiEdid() {
	if config.EdidString != "" {
		logger.Infof("Restoring HDMI EDID to %v", config.EdidString)
		_, err := CallCtrlAction("set_edid", map[string]interface{}{"edid": config.EdidString})
		if err != nil {
			logger.Errorf("Failed to restore HDMI EDID: %v", err)
		}
	}
}
