package kvm

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/jetkvm/kvm/internal/native"
	"github.com/rs/zerolog"
)

// RunNativeProcess runs the native process mode
func RunNativeProcess() {
	// Initialize logger
	logger := zerolog.New(os.Stderr).With().Timestamp().Logger()

	// Parse command line arguments (config is passed as first arg)
	if len(os.Args) < 2 {
		logger.Fatal().Msg("usage: native process requires config_json argument")
	}

	var config native.ProcessConfig
	if err := json.Unmarshal([]byte(os.Args[1]), &config); err != nil {
		logger.Fatal().Err(err).Msg("failed to parse config")
	}

	// Parse version strings
	var systemVersion *semver.Version
	if config.SystemVersion != "" {
		v, err := semver.NewVersion(config.SystemVersion)
		if err != nil {
			logger.Warn().Err(err).Str("version", config.SystemVersion).Msg("failed to parse system version")
		} else {
			systemVersion = v
		}
	}

	var appVersion *semver.Version
	if config.AppVersion != "" {
		v, err := semver.NewVersion(config.AppVersion)
		if err != nil {
			logger.Warn().Err(err).Str("version", config.AppVersion).Msg("failed to parse app version")
		} else {
			appVersion = v
		}
	}

	// Create native instance
	nativeInstance := native.NewNative(native.NativeOptions{
		Disable:              config.Disable,
		SystemVersion:        systemVersion,
		AppVersion:           appVersion,
		DisplayRotation:      config.DisplayRotation,
		DefaultQualityFactor: config.DefaultQualityFactor,
		OnVideoStateChange: func(state native.VideoState) {
			sendEvent("video_state_change", state)
		},
		OnIndevEvent: func(event string) {
			sendEvent("indev_event", event)
		},
		OnRpcEvent: func(event string) {
			sendEvent("rpc_event", event)
		},
		OnVideoFrameReceived: func(frame []byte, duration time.Duration) {
			sendEvent("video_frame", map[string]interface{}{
				"frame":    frame,
				"duration": duration.Nanoseconds(),
			})
		},
	})

	// Start native instance
	if err := nativeInstance.Start(); err != nil {
		logger.Fatal().Err(err).Msg("failed to start native instance")
	}

	// Create gRPC server
	grpcServer := native.NewGRPCServer(nativeInstance, &logger)

	// Determine socket path
	socketPath := os.Getenv("JETKVM_NATIVE_SOCKET")
	if socketPath == "" {
		// Default to a socket in /tmp
		socketPath = filepath.Join("/tmp", fmt.Sprintf("jetkvm-native-%d.sock", os.Getpid()))
	}

	// Start gRPC server
	server, lis, err := native.StartGRPCServer(grpcServer, socketPath, &logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to start gRPC server")
	}

	// Signal that we're ready by writing socket path to stdout (for parent to read)
	fmt.Fprintf(os.Stdout, "%s\n", socketPath)
	os.Stdout.Close()

	// Set up signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)

	// Wait for signal
	<-sigChan
	logger.Info().Msg("received termination signal")

	// Graceful shutdown
	server.GracefulStop()
	lis.Close()

	logger.Info().Msg("native process exiting")
}

// All JSON-RPC handlers have been removed - now using gRPC
	case "VideoSetSleepMode":
		var params struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			sendError(encoder, req.ID, -32602, "Invalid params", nil)
			return
		}
		err = n.VideoSetSleepMode(params.Enabled)

	case "VideoGetSleepMode":
		var result bool
		result, err = n.VideoGetSleepMode()
		if err == nil {
			sendResponse(encoder, req.ID, "result", result)
			return
		}

	case "VideoSleepModeSupported":
		result = n.VideoSleepModeSupported()
		sendResponse(encoder, req.ID, "result", result)
		return

	case "VideoSetQualityFactor":
		var params struct {
			Factor float64 `json:"factor"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			sendError(encoder, req.ID, -32602, "Invalid params", nil)
			return
		}
		err = n.VideoSetQualityFactor(params.Factor)

	case "VideoGetQualityFactor":
		var result float64
		result, err = n.VideoGetQualityFactor()
		if err == nil {
			sendResponse(encoder, req.ID, "result", result)
			return
		}

	case "VideoSetEDID":
		var params struct {
			EDID string `json:"edid"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			sendError(encoder, req.ID, -32602, "Invalid params", nil)
			return
		}
		err = n.VideoSetEDID(params.EDID)

	case "VideoGetEDID":
		var result string
		result, err = n.VideoGetEDID()
		if err == nil {
			sendResponse(encoder, req.ID, "result", result)
			return
		}

	case "VideoLogStatus":
		var result string
		result, err = n.VideoLogStatus()
		if err == nil {
			sendResponse(encoder, req.ID, "result", result)
			return
		}

	case "VideoStop":
		err = n.VideoStop()

	case "VideoStart":
		err = n.VideoStart()

	case "GetLVGLVersion":
		var result string
		result, err = n.GetLVGLVersion()
		if err == nil {
			sendResponse(encoder, req.ID, "result", result)
			return
		}

	case "UIObjHide":
		var params struct {
			ObjName string `json:"obj_name"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			sendError(encoder, req.ID, -32602, "Invalid params", nil)
			return
		}
		var result bool
		result, err = n.UIObjHide(params.ObjName)
		if err == nil {
			sendResponse(encoder, req.ID, "result", result)
			return
		}

	case "UIObjShow":
		var params struct {
			ObjName string `json:"obj_name"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			sendError(encoder, req.ID, -32602, "Invalid params", nil)
			return
		}
		var result bool
		result, err = n.UIObjShow(params.ObjName)
		if err == nil {
			sendResponse(encoder, req.ID, "result", result)
			return
		}

	case "UISetVar":
		var params struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			sendError(encoder, req.ID, -32602, "Invalid params", nil)
			return
		}
		n.UISetVar(params.Name, params.Value)
		sendResponse(encoder, req.ID, "result", nil)
		return

	case "UIGetVar":
		var params struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			sendError(encoder, req.ID, -32602, "Invalid params", nil)
			return
		}
		result = n.UIGetVar(params.Name)
		sendResponse(encoder, req.ID, "result", result)
		return

	case "UIObjAddState":
		var params struct {
			ObjName string `json:"obj_name"`
			State   string `json:"state"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			sendError(encoder, req.ID, -32602, "Invalid params", nil)
			return
		}
		var result bool
		result, err = n.UIObjAddState(params.ObjName, params.State)
		if err == nil {
			sendResponse(encoder, req.ID, "result", result)
			return
		}

	case "UIObjClearState":
		var params struct {
			ObjName string `json:"obj_name"`
			State   string `json:"state"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			sendError(encoder, req.ID, -32602, "Invalid params", nil)
			return
		}
		var result bool
		result, err = n.UIObjClearState(params.ObjName, params.State)
		if err == nil {
			sendResponse(encoder, req.ID, "result", result)
			return
		}

	case "UIObjAddFlag":
		var params struct {
			ObjName string `json:"obj_name"`
			Flag    string `json:"flag"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			sendError(encoder, req.ID, -32602, "Invalid params", nil)
			return
		}
		var result bool
		result, err = n.UIObjAddFlag(params.ObjName, params.Flag)
		if err == nil {
			sendResponse(encoder, req.ID, "result", result)
			return
		}

	case "UIObjClearFlag":
		var params struct {
			ObjName string `json:"obj_name"`
			Flag    string `json:"flag"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			sendError(encoder, req.ID, -32602, "Invalid params", nil)
			return
		}
		var result bool
		result, err = n.UIObjClearFlag(params.ObjName, params.Flag)
		if err == nil {
			sendResponse(encoder, req.ID, "result", result)
			return
		}

	case "UIObjSetOpacity":
		var params struct {
			ObjName string `json:"obj_name"`
			Opacity int    `json:"opacity"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			sendError(encoder, req.ID, -32602, "Invalid params", nil)
			return
		}
		var result bool
		result, err = n.UIObjSetOpacity(params.ObjName, params.Opacity)
		if err == nil {
			sendResponse(encoder, req.ID, "result", result)
			return
		}

	case "UIObjFadeIn":
		var params struct {
			ObjName  string `json:"obj_name"`
			Duration uint32 `json:"duration"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			sendError(encoder, req.ID, -32602, "Invalid params", nil)
			return
		}
		var result bool
		result, err = n.UIObjFadeIn(params.ObjName, params.Duration)
		if err == nil {
			sendResponse(encoder, req.ID, "result", result)
			return
		}

	case "UIObjFadeOut":
		var params struct {
			ObjName  string `json:"obj_name"`
			Duration uint32 `json:"duration"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			sendError(encoder, req.ID, -32602, "Invalid params", nil)
			return
		}
		var result bool
		result, err = n.UIObjFadeOut(params.ObjName, params.Duration)
		if err == nil {
			sendResponse(encoder, req.ID, "result", result)
			return
		}

	case "UIObjSetLabelText":
		var params struct {
			ObjName string `json:"obj_name"`
			Text    string `json:"text"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			sendError(encoder, req.ID, -32602, "Invalid params", nil)
			return
		}
		var result bool
		result, err = n.UIObjSetLabelText(params.ObjName, params.Text)
		if err == nil {
			sendResponse(encoder, req.ID, "result", result)
			return
		}

	case "UIObjSetImageSrc":
		var params struct {
			ObjName string `json:"obj_name"`
			Image   string `json:"image"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			sendError(encoder, req.ID, -32602, "Invalid params", nil)
			return
		}
		var result bool
		result, err = n.UIObjSetImageSrc(params.ObjName, params.Image)
		if err == nil {
			sendResponse(encoder, req.ID, "result", result)
			return
		}

	case "DisplaySetRotation":
		var params struct {
			Rotation uint16 `json:"rotation"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			sendError(encoder, req.ID, -32602, "Invalid params", nil)
			return
		}
		var result bool
		result, err = n.DisplaySetRotation(params.Rotation)
		if err == nil {
			sendResponse(encoder, req.ID, "result", result)
			return
		}

	case "UpdateLabelIfChanged":
		var params struct {
			ObjName string `json:"obj_name"`
			NewText string `json:"new_text"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			sendError(encoder, req.ID, -32602, "Invalid params", nil)
			return
		}
		n.UpdateLabelIfChanged(params.ObjName, params.NewText)
		sendResponse(encoder, req.ID, "result", nil)
		return

	case "UpdateLabelAndChangeVisibility":
		var params struct {
			ObjName string `json:"obj_name"`
			NewText string `json:"new_text"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			sendError(encoder, req.ID, -32602, "Invalid params", nil)
			return
		}
		n.UpdateLabelAndChangeVisibility(params.ObjName, params.NewText)
		sendResponse(encoder, req.ID, "result", nil)
		return

	case "SwitchToScreenIf":
		var params struct {
			ScreenName   string   `json:"screen_name"`
			ShouldSwitch []string `json:"should_switch"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			sendError(encoder, req.ID, -32602, "Invalid params", nil)
			return
		}
		n.SwitchToScreenIf(params.ScreenName, params.ShouldSwitch)
		sendResponse(encoder, req.ID, "result", nil)
		return

	case "SwitchToScreenIfDifferent":
		var params struct {
			ScreenName string `json:"screen_name"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			sendError(encoder, req.ID, -32602, "Invalid params", nil)
			return
		}
		n.SwitchToScreenIfDifferent(params.ScreenName)
		sendResponse(encoder, req.ID, "result", nil)
		return

	case "DoNotUseThisIsForCrashTestingOnly":
		n.DoNotUseThisIsForCrashTestingOnly()
		sendResponse(encoder, req.ID, "result", nil)
		return

	default:
		sendError(encoder, req.ID, -32601, fmt.Sprintf("Method not found: %s", req.Method), nil)
		return
	}

	if err != nil {
		sendError(encoder, req.ID, -32000, err.Error(), nil)
		return
	}

	sendResponse(encoder, req.ID, "result", result)
}

func sendResponse(encoder *json.Encoder, id interface{}, key string, result interface{}) {
	response := map[string]interface{}{
		"jsonrpc": "2.0",
		key:       result,
	}
	if id != nil {
		response["id"] = id
	}
	if err := encoder.Encode(response); err != nil {
		fmt.Fprintf(os.Stderr, "failed to send response: %v\n", err)
	}
}

func sendError(encoder *json.Encoder, id interface{}, code int, message string, data interface{}) {
	response := map[string]interface{}{
		"jsonrpc": "2.0",
		"error": map[string]interface{}{
			"code":    code,
			"message": message,
		},
	}
	if id != nil {
		response["id"] = id
	}
	if data != nil {
		response["error"].(map[string]interface{})["data"] = data
	}
	if err := encoder.Encode(response); err != nil {
		fmt.Fprintf(os.Stderr, "failed to send error: %v\n", err)
	}
}

func sendEvent(eventType string, data interface{}) {
	event := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "event",
		"params": map[string]interface{}{
			"type": eventType,
			"data": data,
		},
	}
	encoder := json.NewEncoder(os.Stdout)
	if err := encoder.Encode(event); err != nil {
		fmt.Fprintf(os.Stderr, "failed to send event: %v\n", err)
	}
}

