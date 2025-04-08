package kvm

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
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
	r.GET("/webrtc/signaling", handleWebRTCSignal)

	// Protected routes (allows both password and noPassword modes)
	protected := r.Group("/")
	protected.Use(protectedMiddleware())
	{
		protected.POST("/webrtc/session", handleWebRTCSession)
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

func handleWebRTCSignal(c *gin.Context) {
	cloudLogger.Infof("new websocket connection established")
	// Create WebSocket options with InsecureSkipVerify to bypass origin check
	wsOptions := &websocket.AcceptOptions{
		InsecureSkipVerify: true, // Allow connections from any origin
	}

	wsCon, err := websocket.Accept(c.Writer, c.Request, wsOptions)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Now use conn for websocket operations
	defer wsCon.Close(websocket.StatusNormalClosure, "")
	err = handleWebRTCSignalWsConnection(wsCon, false)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
}

func handleWebRTCSignalWsConnection(wsCon *websocket.Conn, isCloudConnection bool) error {
	runCtx, cancelRun := context.WithCancel(context.Background())
	defer cancelRun()

	// Add connection tracking to detect reconnections
	connectionID := uuid.New().String()
	cloudLogger.Infof("new websocket connection established with ID: %s", connectionID)

	// Add a mutex to protect against concurrent access to session state
	sessionMutex := &sync.Mutex{}

	// Track processed offers to avoid duplicates
	processedOffers := make(map[string]bool)

	go func() {
		for {
			time.Sleep(CloudWebSocketPingInterval)

			// set the timer for the ping duration
			timer := prometheus.NewTimer(prometheus.ObserverFunc(func(v float64) {
				metricCloudConnectionLastPingDuration.Set(v)
				metricCloudConnectionPingDuration.Observe(v)
			}))

			cloudLogger.Infof("pinging websocket")
			err := wsCon.Ping(runCtx)

			if err != nil {
				cloudLogger.Warnf("websocket ping error: %v", err)
				cancelRun()
				return
			}

			// dont use `defer` here because we want to observe the duration of the ping
			timer.ObserveDuration()

			metricCloudConnectionTotalPingCount.Inc()
			metricCloudConnectionLastPingTimestamp.SetToCurrentTime()
		}
	}()

	for {
		typ, msg, err := wsCon.Read(runCtx)
		if err != nil {
			cloudLogger.Warnf("websocket read error: %v", err)
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

		err = json.Unmarshal(msg, &message)
		if err != nil {
			cloudLogger.Warnf("unable to parse ws message: %v", string(msg))
			continue
		}

		if message.Type == "offer" {
			cloudLogger.Infof("new session request received")
			var req WebRTCSessionRequest
			err = json.Unmarshal(message.Data, &req)
			if err != nil {
				cloudLogger.Warnf("unable to parse session request data: %v", string(message.Data))
				continue
			}

			// Create a hash of the offer to deduplicate
			offerHash := fmt.Sprintf("%x", sha256.Sum256(message.Data))

			sessionMutex.Lock()
			isDuplicate := processedOffers[offerHash]
			if !isDuplicate {
				processedOffers[offerHash] = true
			}
			sessionMutex.Unlock()

			if isDuplicate {
				cloudLogger.Infof("duplicate offer detected, ignoring: %s", offerHash[:8])
				continue
			}

			cloudLogger.Infof("new session request: %v", req.OidcGoogle)
			cloudLogger.Tracef("session request info: %v", req)

			metricCloudConnectionSessionRequestCount.Inc()
			metricCloudConnectionLastSessionRequestTimestamp.SetToCurrentTime()
			err = handleSessionRequest(runCtx, wsCon, req, isCloudConnection)
			if err != nil {
				cloudLogger.Infof("error starting new session: %v", err)
				continue
			}
		} else if message.Type == "new-ice-candidate" {
			cloudLogger.Infof("client has sent us a new ICE candidate: %v", string(message.Data))
			var candidate webrtc.ICECandidateInit

			// Attempt to unmarshal as a ICECandidateInit
			if err := json.Unmarshal(message.Data, &candidate); err != nil {
				cloudLogger.Warnf("unable to parse ICE candidate data: %v", string(message.Data))
				continue
			}

			if candidate.Candidate == "" {
				cloudLogger.Warnf("empty ICE candidate, skipping")
				continue
			}

			cloudLogger.Infof("unmarshalled ICE candidate: %v", candidate)

			if currentSession == nil {
				cloudLogger.Infof("no current session, skipping ICE candidate")
				continue
			}

			cloudLogger.Infof("adding ICE candidate to current session: %v", candidate)
			if err = currentSession.peerConnection.AddICECandidate(candidate); err != nil {
				cloudLogger.Warnf("failed to add ICE candidate: %v", err)
			}
		}
	}
}

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
