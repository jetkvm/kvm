package kvm

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	gin_logger "github.com/gin-contrib/logger"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
	"golang.org/x/crypto/bcrypt"
)

//nolint:typecheck
//go:embed all:static
var staticFiles embed.FS

type WebRTCSessionRequest struct {
	Sd         string   `json:"sd"`
	OidcGoogle string   `json:"OidcGoogle,omitempty"`
	IP         string   `json:"ip,omitempty"`
	ICEServers []string `json:"iceServers,omitempty"`
}

type SetPasswordRequest struct {
	Password string `json:"password"`
}

type LoginRequest struct {
	Password string `json:"password"`
}

type ChangePasswordRequest struct {
	OldPassword string `json:"oldPassword"`
	NewPassword string `json:"newPassword"`
}

type LocalDevice struct {
	AuthMode *string `json:"authMode"`
	DeviceID string  `json:"deviceId"`
}

type DeviceStatus struct {
	IsSetup bool `json:"isSetup"`
}

type SetupRequest struct {
	LocalAuthMode string `json:"localAuthMode"`
	Password      string `json:"password,omitempty"`
}

func setupRouter() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	gin.DisableConsoleColor()
	r := gin.Default()
	r.Use(gin_logger.SetLogger(
		gin_logger.WithLogger(func(*gin.Context, zerolog.Logger) zerolog.Logger {
			return *ginLogger
		}),
	))
	staticFS, _ := fs.Sub(staticFiles, "static")

	// Add a custom middleware to set cache headers for images
	// This is crucial for optimizing the initial welcome screen load time
	// By enabling caching, we ensure that pre-loaded images are stored in the browser cache
	// This allows for a smoother enter animation and improved user experience on the welcome screen
	r.Use(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/static/") {
			ext := filepath.Ext(c.Request.URL.Path)
			if ext == ".jpg" || ext == ".jpeg" || ext == ".png" || ext == ".gif" || ext == ".webp" {
				c.Header("Cache-Control", "public, max-age=300") // Cache for 5 minutes
			}
		}
		c.Next()
	})

	r.StaticFS("/static", http.FS(staticFS))
	r.POST("/auth/login-local", handleLogin)

	// We use this to determine if the device is setup
	r.GET("/device/status", handleDeviceStatus)

	// We use this to provide the UI with the device configuration
	r.GET("/device/ui-config.js", handleDeviceUIConfig)

	// We use this to setup the device in the welcome page
	r.POST("/device/setup", handleSetup)

	// A Prometheus metrics endpoint.
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))

	// Protected routes (allows both password and noPassword modes)
	protected := r.Group("/")
	protected.Use(protectedMiddleware())
	{
		/*
		 * Legacy WebRTC session endpoint
		 *
		 * This endpoint is maintained for backward compatibility when users upgrade from a version
		 * using the legacy HTTP-based signaling method to the new WebSocket-based signaling method.
		 *
		 * During the upgrade process, when the "Rebooting device after update..." message appears,
		 * the browser still runs the previous JavaScript code which polls this endpoint to establish
		 * a new WebRTC session. Once the session is established, the page will automatically reload
		 * with the updated code.
		 *
		 * Without this endpoint, the stale JavaScript would fail to establish a connection,
		 * causing users to see the "Rebooting device after update..." message indefinitely
		 * until they manually refresh the page, leading to a confusing user experience.
		 */
		protected.POST("/webrtc/session", handleWebRTCSession)
		protected.GET("/webrtc/signaling/client", handleLocalWebRTCSignal)
		protected.POST("/cloud/register", handleCloudRegister)
		protected.GET("/cloud/state", handleCloudState)
		protected.GET("/device", handleDevice)
		protected.POST("/auth/logout", handleLogout)

		protected.POST("/auth/password-local", handleCreatePassword)
		protected.PUT("/auth/password-local", handleUpdatePassword)
		protected.DELETE("/auth/local-password", handleDeletePassword)
		protected.POST("/storage/upload", handleUploadHttp)
	}

	// Catch-all route for SPA
	r.NoRoute(func(c *gin.Context) {
		if c.Request.Method == "GET" && c.NegotiateFormat(gin.MIMEHTML) == gin.MIMEHTML {
			c.FileFromFS("/", http.FS(staticFS))
			return
		}
		c.Status(http.StatusNotFound)
	})

	return r
}

// TODO: support multiple sessions?
var currentSession *Session

func handleWebRTCSession(c *gin.Context) {
	var req WebRTCSessionRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	session, err := newSession(SessionConfig{})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err})
		return
	}

	sd, err := session.ExchangeOffer(req.Sd)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err})
		return
	}
	if currentSession != nil {
		writeJSONRPCEvent("otherSessionConnected", nil, currentSession)
		peerConn := currentSession.peerConnection
		go func() {
			time.Sleep(1 * time.Second)
			_ = peerConn.Close()
		}()
	}
	currentSession = session
	c.JSON(http.StatusOK, gin.H{"sd": sd})
}

var (
	pingMessage = []byte("ping")
	pongMessage = []byte("pong")
)

func handleLocalWebRTCSignal(c *gin.Context) {
	// get the source from the request
	source := c.ClientIP()
	connectionID := uuid.New().String()

	scopedLogger := websocketLogger.With().
		Str("component", "websocket").
		Str("source", source).
		Str("sourceType", "local").
		Logger()

	scopedLogger.Info().Msg("new websocket connection established")

	// Create WebSocket options with InsecureSkipVerify to bypass origin check
	wsOptions := &websocket.AcceptOptions{
		InsecureSkipVerify: true, // Allow connections from any origin
		OnPingReceived: func(ctx context.Context, payload []byte) bool {
			scopedLogger.Info().Bytes("payload", payload).Msg("ping frame received")

			metricConnectionTotalPingReceivedCount.WithLabelValues("local", source).Inc()
			metricConnectionLastPingReceivedTimestamp.WithLabelValues("local", source).SetToCurrentTime()

			return true
		},
	}

	wsCon, err := websocket.Accept(c.Writer, c.Request, wsOptions)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Now use conn for websocket operations
	defer wsCon.Close(websocket.StatusNormalClosure, "")

	err = wsjson.Write(context.Background(), wsCon, gin.H{"type": "device-metadata", "data": gin.H{"deviceVersion": builtAppVersion}})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	err = handleWebRTCSignalWsMessages(wsCon, false, source, connectionID, &scopedLogger)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
}

func handleWebRTCSignalWsMessages(
	wsCon *websocket.Conn,
	isCloudConnection bool,
	source string,
	connectionID string,
	scopedLogger *zerolog.Logger,
) error {
	runCtx, cancelRun := context.WithCancel(context.Background())
	defer cancelRun()

	// connection type
	var sourceType string
	if isCloudConnection {
		sourceType = "cloud"
	} else {
		sourceType = "local"
	}

	l := scopedLogger.With().
		Str("source", source).
		Str("sourceType", sourceType).
		Str("connectionID", connectionID).
		Logger()

	l.Info().Msg("new websocket connection established")

	go func() {
		for {
			time.Sleep(WebsocketPingInterval)

			if ctxErr := runCtx.Err(); ctxErr != nil {
				if !errors.Is(ctxErr, context.Canceled) {
					l.Warn().Str("error", ctxErr.Error()).Msg("websocket connection closed")
				} else {
					l.Trace().Str("error", ctxErr.Error()).Msg("websocket connection closed as the context was canceled")
				}
				return
			}

			// set the timer for the ping duration
			timer := prometheus.NewTimer(prometheus.ObserverFunc(func(v float64) {
				metricConnectionLastPingDuration.WithLabelValues(sourceType, source).Set(v)
				metricConnectionPingDuration.WithLabelValues(sourceType, source).Observe(v)
			}))

			l.Trace().Msg("sending ping frame")
			err := wsCon.Ping(runCtx)

			if err != nil {
				l.Warn().Str("error", err.Error()).Msg("websocket ping error")
				cancelRun()
				return
			}

			// dont use `defer` here because we want to observe the duration of the ping
			duration := timer.ObserveDuration()

			metricConnectionTotalPingSentCount.WithLabelValues(sourceType, source).Inc()
			metricConnectionLastPingTimestamp.WithLabelValues(sourceType, source).SetToCurrentTime()

			l.Trace().Str("duration", duration.String()).Msg("received pong frame")
		}
	}()

	if isCloudConnection {
		// create a channel to receive the disconnect event, once received, we cancelRun
		cloudDisconnectChan = make(chan error)
		defer func() {
			close(cloudDisconnectChan)
			cloudDisconnectChan = nil
		}()
		go func() {
			for err := range cloudDisconnectChan {
				if err == nil {
					continue
				}
				cloudLogger.Info().Err(err).Msg("disconnecting from cloud due to")
				cancelRun()
			}
		}()
	}

	for {
		typ, msg, err := wsCon.Read(runCtx)
		if err != nil {
			l.Warn().Str("error", err.Error()).Msg("websocket read error")
			return err
		}
		if typ != websocket.MessageText {
			// ignore non-text messages
			continue
		}

		var message struct {
			Type string          `json:"type"`
			Data json.RawMessage `json:"data"`
		}

		if bytes.Equal(msg, pingMessage) {
			l.Info().Str("message", string(msg)).Msg("ping message received")
			err = wsCon.Write(context.Background(), websocket.MessageText, pongMessage)
			if err != nil {
				l.Warn().Str("error", err.Error()).Msg("unable to write pong message")
				return err
			}

			metricConnectionTotalPingReceivedCount.WithLabelValues(sourceType, source).Inc()
			metricConnectionLastPingReceivedTimestamp.WithLabelValues(sourceType, source).SetToCurrentTime()

			continue
		}

		err = json.Unmarshal(msg, &message)
		if err != nil {
			l.Warn().Str("error", err.Error()).Msg("unable to parse ws message")
			continue
		}

		if message.Type == "offer" {
			l.Info().Msg("new session request received")
			var req WebRTCSessionRequest
			err = json.Unmarshal(message.Data, &req)
			if err != nil {
				l.Warn().Str("error", err.Error()).Msg("unable to parse session request data")
				continue
			}

			if req.OidcGoogle != "" {
				l.Info().Str("oidcGoogle", req.OidcGoogle).Msg("new session request with OIDC Google")
			}

			metricConnectionSessionRequestCount.WithLabelValues(sourceType, source).Inc()
			metricConnectionLastSessionRequestTimestamp.WithLabelValues(sourceType, source).SetToCurrentTime()
			err = handleSessionRequest(runCtx, wsCon, req, isCloudConnection, source, &l)
			if err != nil {
				l.Warn().Str("error", err.Error()).Msg("error starting new session")
				continue
			}
		} else if message.Type == "new-ice-candidate" {
			l.Info().Str("data", string(message.Data)).Msg("The client sent us a new ICE candidate")
			var candidate webrtc.ICECandidateInit

			// Attempt to unmarshal as a ICECandidateInit
			if err := json.Unmarshal(message.Data, &candidate); err != nil {
				l.Warn().Str("error", err.Error()).Msg("unable to parse incoming ICE candidate data")
				continue
			}

			if candidate.Candidate == "" {
				l.Warn().Msg("empty incoming ICE candidate, skipping")
				continue
			}

			l.Info().Str("data", fmt.Sprintf("%v", candidate)).Msg("unmarshalled incoming ICE candidate")

			if currentSession == nil {
				l.Warn().Msg("no current session, skipping incoming ICE candidate")
				continue
			}

			l.Info().Str("data", fmt.Sprintf("%v", candidate)).Msg("adding incoming ICE candidate to current session")
			if err = currentSession.peerConnection.AddICECandidate(candidate); err != nil {
				l.Warn().Str("error", err.Error()).Msg("failed to add incoming ICE candidate to our peer connection")
			}
		}
	}
}

func handleLogin(c *gin.Context) {
	if config.LocalAuthMode == "noPassword" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Login is disabled in noPassword mode"})
		return
	}

	var req LoginRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	err := bcrypt.CompareHashAndPassword([]byte(config.HashedPassword), []byte(req.Password))
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid password"})
		return
	}

	config.LocalAuthToken = uuid.New().String()

	// Set the cookie
	c.SetCookie("authToken", config.LocalAuthToken, 7*24*60*60, "/", "", false, true)

	c.JSON(http.StatusOK, gin.H{"message": "Login successful"})
}

func handleLogout(c *gin.Context) {
	config.LocalAuthToken = ""
	if err := SaveConfig(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save configuration"})
		return
	}

	// Clear the auth cookie
	c.SetCookie("authToken", "", -1, "/", "", false, true)
	c.JSON(http.StatusOK, gin.H{"message": "Logout successful"})
}

func protectedMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if config.LocalAuthMode == "noPassword" {
			c.Next()
			return
		}

		authToken, err := c.Cookie("authToken")
		if err != nil || authToken != config.LocalAuthToken || authToken == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			c.Abort()
			return
		}

		c.Next()
	}
}

func RunWebServer() {
	r := setupRouter()
	//if strings.Contains(builtAppVersion, "-dev") {
	//	pprof.Register(r)
	//}
	err := r.Run(":80")
	if err != nil {
		panic(err)
	}
}

func handleDevice(c *gin.Context) {
	response := LocalDevice{
		AuthMode: &config.LocalAuthMode,
		DeviceID: GetDeviceID(),
	}

	c.JSON(http.StatusOK, response)
}

func handleCreatePassword(c *gin.Context) {
	if config.HashedPassword != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Password already set"})
		return
	}

	// We only allow users with noPassword mode to set a new password
	// Users with password mode are not allowed to set a new password without providing the old password
	// We have a PUT endpoint for changing the password, use that instead
	if config.LocalAuthMode != "noPassword" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Password mode is not enabled"})
		return
	}

	var req SetPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash password"})
		return
	}

	config.HashedPassword = string(hashedPassword)
	config.LocalAuthToken = uuid.New().String()
	config.LocalAuthMode = "password"
	if err := SaveConfig(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save configuration"})
		return
	}

	// Set the cookie
	c.SetCookie("authToken", config.LocalAuthToken, 7*24*60*60, "/", "", false, true)

	c.JSON(http.StatusCreated, gin.H{"message": "Password set successfully"})
}

func handleUpdatePassword(c *gin.Context) {
	if config.HashedPassword == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Password is not set"})
		return
	}

	// We only allow users with password mode to change their password
	// Users with noPassword mode are not allowed to change their password
	if config.LocalAuthMode != "password" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Password mode is not enabled"})
		return
	}

	var req ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.OldPassword == "" || req.NewPassword == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(config.HashedPassword), []byte(req.OldPassword)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Incorrect old password"})
		return
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash new password"})
		return
	}

	config.HashedPassword = string(hashedPassword)
	config.LocalAuthToken = uuid.New().String()
	if err := SaveConfig(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save configuration"})
		return
	}

	// Set the cookie
	c.SetCookie("authToken", config.LocalAuthToken, 7*24*60*60, "/", "", false, true)

	c.JSON(http.StatusOK, gin.H{"message": "Password updated successfully"})
}

func handleDeletePassword(c *gin.Context) {
	if config.HashedPassword == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Password is not set"})
		return
	}

	if config.LocalAuthMode != "password" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Password mode is not enabled"})
		return
	}

	var req LoginRequest // Reusing LoginRequest struct for password
	if err := c.ShouldBindJSON(&req); err != nil || req.Password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(config.HashedPassword), []byte(req.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Incorrect password"})
		return
	}

	// Disable password
	config.HashedPassword = ""
	config.LocalAuthToken = ""
	config.LocalAuthMode = "noPassword"
	if err := SaveConfig(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save configuration"})
		return
	}

	c.SetCookie("authToken", "", -1, "/", "", false, true)

	c.JSON(http.StatusOK, gin.H{"message": "Password disabled successfully"})
}

func handleDeviceStatus(c *gin.Context) {
	response := DeviceStatus{
		IsSetup: config.LocalAuthMode != "",
	}

	c.JSON(http.StatusOK, response)
}

func handleCloudState(c *gin.Context) {
	response := CloudState{
		Connected: config.CloudToken != "",
		URL:       config.CloudURL,
		AppURL:    config.CloudAppURL,
	}

	c.JSON(http.StatusOK, response)
}

func handleDeviceUIConfig(c *gin.Context) {
	config, _ := json.Marshal(gin.H{
		"CLOUD_API":      config.CloudURL,
		"DEVICE_VERSION": builtAppVersion,
	})
	if config == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to marshal config"})
		return
	}

	response := fmt.Sprintf("window.JETKVM_CONFIG = %s;", config)

	c.Data(http.StatusOK, "text/javascript; charset=utf-8", []byte(response))
}

func handleSetup(c *gin.Context) {
	// Check if the device is already set up
	if config.LocalAuthMode != "" || config.HashedPassword != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Device is already set up"})
		return
	}

	var req SetupRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.LocalAuthMode != "password" && req.LocalAuthMode != "noPassword" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid localAuthMode"})
		return
	}

	config.LocalAuthMode = req.LocalAuthMode

	if req.LocalAuthMode == "password" {
		if req.Password == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Password is required for password mode"})
			return
		}

		// Hash the password
		hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash password"})
			return
		}

		config.HashedPassword = string(hashedPassword)
		config.LocalAuthToken = uuid.New().String()

		// Set the cookie
		c.SetCookie("authToken", config.LocalAuthToken, 7*24*60*60, "/", "", false, true)
	} else {
		// For noPassword mode, ensure the password field is empty
		config.HashedPassword = ""
		config.LocalAuthToken = ""
	}

	err := SaveConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save config"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Device setup completed successfully"})
}
