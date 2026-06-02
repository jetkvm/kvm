package kvm

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/pprof"
	neturl "net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	stdsync "sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	gin_logger "github.com/gin-contrib/logger"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jetkvm/kvm/internal/diagnostics"
	"github.com/jetkvm/kvm/internal/logging"
	"github.com/jetkvm/kvm/internal/supervisor"
	"github.com/pion/webrtc/v4"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
	"github.com/vearutop/statigz"
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
	AuthMode     *string `json:"authMode"`
	DeviceID     string  `json:"deviceId"`
	LoopbackOnly bool    `json:"loopbackOnly"`
}

type DeviceStatus struct {
	IsSetup bool `json:"isSetup"`
}

type SetupRequest struct {
	LocalAuthMode string `json:"localAuthMode"`
	Password      string `json:"password,omitempty"`
}

var cachableFileExtensions = []string{
	".jpg", ".jpeg", ".png", ".svg", ".gif", ".webp", ".ico", ".woff2",
}

// MinPasswordLength is the minimum required length for new passwords.
// This is only enforced when setting or changing passwords, not when
// validating existing passwords (to maintain backward compatibility).
const MinPasswordLength = 8

// MaxPasswordLength is the maximum length bcrypt can hash. Go's bcrypt
// implementation rejects passwords over 72 bytes rather than silently
// truncating them.
const MaxPasswordLength = 72

const (
	// Cache durations for HTTP responses, in seconds.
	cacheImmutableMaxAge = 365 * 24 * 60 * 60 // 1 year
	cacheShortMaxAge     = 5 * 60             // 5 minutes

	// authTokenMaxAge is the lifetime of the authToken cookie, in seconds.
	authTokenMaxAge = 7 * 24 * 60 * 60 // 1 week
)

const companionPairRequestTTL = 5 * time.Minute
const companionSignatureMaxSkew = 60 * time.Second

type companionPairRequest struct {
	ID              string
	Direction       string
	RemoteAddr      string
	UserAgent       string
	RequestedAt     time.Time
	Status          string
	OTP             string
	CompanionID     string
	CompanionPubKey string
	RejectionReason string
}

type companionPairRequestBody struct {
	OTP                string `json:"otp"`
	CompanionPublicKey string `json:"companion_public_key"`
}

type companionPairApproveBody struct {
	OTP                string `json:"otp"`
	CompanionPublicKey string `json:"companion_public_key"`
}

type companionPairInitiateBody struct {
	CompanionURL string `json:"companion_url"`
}

type companionPermissionActionBody struct {
	Permission string `json:"permission"`
}

type companionIPEntry struct {
	IP        string `json:"ip"`
	Source    string `json:"source,omitempty"`
	Interface string `json:"interface,omitempty"`
}

type companionStatusSnapshot struct {
	CompanionID                      string          `json:"companion_id"`
	RemoteAddr                       string          `json:"remote_addr,omitempty"`
	LastSeenUnixMilli                int64           `json:"last_seen_unix_milli,omitempty"`
	NotificationPermissionGranted    bool            `json:"notification_permission_granted"`
	DisplayOverAppsPermissionGranted bool            `json:"display_over_apps_permission_granted"`
	BatteryUnrestrictedGranted       bool            `json:"battery_unrestricted_granted"`
	PairedJetKVMURLs                 []string        `json:"paired_jetkvm_urls,omitempty"`
	VisibleIPs                       []string        `json:"visible_ips,omitempty"`
	JetKVMUSBIdentity                string          `json:"jetkvm_usb_identity,omitempty"`
	TargetType                       string          `json:"target_type,omitempty"`
	PreferredMouseMode               string          `json:"preferred_mouse_mode,omitempty"`
	DisplayWidth                     int             `json:"display_width,omitempty"`
	DisplayHeight                    int             `json:"display_height,omitempty"`
	Evidence                         []string        `json:"evidence,omitempty"`
	Peripherals                      map[string]bool `json:"peripherals,omitempty"`
	PendingActions                   []string        `json:"pending_actions,omitempty"`
}

var (
	companionPairingLock    stdsync.Mutex
	companionPairingRequest *companionPairRequest
	companionNonceLock      stdsync.Mutex
	companionNonces         = map[string]map[string]time.Time{}
	companionStatusLock     stdsync.Mutex
	companionStatuses       = map[string]companionStatusSnapshot{}
	companionPendingActions = map[string]map[string]bool{}
)

func setupRouter() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	gin.DisableConsoleColor()
	r := gin.Default()
	r.Use(gin_logger.SetLogger(
		gin_logger.WithLogger(func(*gin.Context, zerolog.Logger) zerolog.Logger {
			return *ginLogger
		}),
	))

	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to get rooted static files subdirectory")
	}
	staticFileServer := http.StripPrefix("/static", statigz.FileServer(
		staticFS.(fs.ReadDirFS),
	))

	// Security headers middleware
	r.Use(func(c *gin.Context) {
		c.Header("X-Frame-Options", "DENY")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-XSS-Protection", "1; mode=block")
		c.Next()
	})

	// Add a custom middleware to set cache headers for images
	// This is crucial for optimizing the initial welcome screen load time
	// By enabling caching, we ensure that pre-loaded images are stored in the browser cache
	// This allows for a smoother enter animation and improved user experience on the welcome screen
	r.Use(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/static/assets/immutable/") {
			c.Header("Cache-Control", fmt.Sprintf("public, max-age=%d, immutable", cacheImmutableMaxAge))
			c.Next()
			return
		}

		if strings.HasPrefix(c.Request.URL.Path, "/static/") {
			ext := filepath.Ext(c.Request.URL.Path)
			if slices.Contains(cachableFileExtensions, ext) {
				c.Header("Cache-Control", fmt.Sprintf("public, max-age=%d", cacheShortMaxAge))
			}
		}

		c.Next()
	})

	r.GET("/robots.txt", func(c *gin.Context) {
		c.Header("Content-Type", "text/plain")
		c.Header("Cache-Control", fmt.Sprintf("public, max-age=%d, immutable", cacheImmutableMaxAge))
		c.String(http.StatusOK, "User-agent: *\nDisallow: /")
	})

	r.Any("/static/*w", func(c *gin.Context) {
		staticFileServer.ServeHTTP(c.Writer, c.Request)
	})

	// Public routes (no authentication required)
	r.POST("/auth/login-local", handleLogin)
	r.POST("/companion/pair", handleCompanionPair)
	r.GET("/companion/pair/:id", handleCompanionPairStatus)
	r.POST("/companion/pair/:id/claim", handleCompanionPairClaim)
	r.POST("/companion/target", handleCompanionTargetDeclaration)
	r.POST("/companion/unpair", handleCompanionUnpair)
	r.GET("/login-local", func(c *gin.Context) {
		if shouldUseAndroidNativeLogin(c) {
			handleAndroidNativeLogin(c)
			return
		}
		c.FileFromFS("/", http.FS(staticFS))
	})

	// We use this to determine if the device is setup
	r.GET("/device/status", handleDeviceStatus)

	// We use this to setup the device in the welcome page
	r.POST("/device/setup", handleSetup)

	// A Prometheus metrics endpoint.
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))

	// Developer mode protected routes
	developerModeRouter := r.Group("/developer/")
	developerModeRouter.Use(basicAuthProtectedMiddleware(true))
	{
		// pprof
		developerModeRouter.GET("/pprof/", gin.WrapF(pprof.Index))
		developerModeRouter.GET("/pprof/cmdline", gin.WrapF(pprof.Cmdline))
		developerModeRouter.GET("/pprof/profile", gin.WrapF(pprof.Profile))
		developerModeRouter.POST("/pprof/symbol", gin.WrapF(pprof.Symbol))
		developerModeRouter.GET("/pprof/symbol", gin.WrapF(pprof.Symbol))
		developerModeRouter.GET("/pprof/trace", gin.WrapF(pprof.Trace))
		developerModeRouter.GET("/pprof/allocs", gin.WrapH(pprof.Handler("allocs")))
		developerModeRouter.GET("/pprof/block", gin.WrapH(pprof.Handler("block")))
		developerModeRouter.GET("/pprof/goroutine", gin.WrapH(pprof.Handler("goroutine")))
		developerModeRouter.GET("/pprof/heap", gin.WrapH(pprof.Handler("heap")))
		developerModeRouter.GET("/pprof/mutex", gin.WrapH(pprof.Handler("mutex")))
		developerModeRouter.GET("/pprof/threadcreate", gin.WrapH(pprof.Handler("threadcreate")))

		logging.AttachSSEHandler(developerModeRouter)
	}

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

		protected.POST("/device/send-wol/:mac-addr", handleSendWOLMagicPacket)
		protected.GET("/companion/pair/requests", handleCompanionPairRequests)
		protected.POST("/companion/pair/initiate", handleCompanionPairInitiate)
		protected.POST("/companion/pair/:id/approve", handleCompanionPairApprove)
		protected.POST("/companion/pair/:id/reject", handleCompanionPairReject)
		protected.GET("/companion/status", handleCompanionStatus)
		protected.POST("/companion/:id/request-permission", handleCompanionPermissionRequest)
		protected.POST("/companion/unpair-admin", handleCompanionUnpairAdmin)

		protected.GET("/diagnostics", handleDiagnosticsDownload)
	}

	// Catch-all route for SPA
	r.NoRoute(func(c *gin.Context) {
		if c.Request.Method == "GET" && c.NegotiateFormat(gin.MIMEHTML) == gin.MIMEHTML {
			if shouldUseAndroidNativeLogin(c) && !hasValidLocalAuthCookie(c) {
				handleAndroidNativeLogin(c)
				return
			}
			c.FileFromFS("/", http.FS(staticFS))
			return
		}
		c.Status(http.StatusNotFound)
	})

	return r
}

func handleCompanionTargetDeclaration(c *gin.Context) {
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	companionID, ok := companionIDFromSignedRequest(c, bodyBytes)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid companion signature"})
		return
	}

	var declaration CompanionTargetDeclaration
	if err := json.Unmarshal(bodyBytes, &declaration); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	if declaration.TargetType != "android" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported target_type"})
		return
	}
	if declaration.State == "" {
		declaration.State = "connected"
	}
	if declaration.State != "connected" && declaration.State != "disconnected" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported state"})
		return
	}
	if !isValidJetKVMUSBIdentity(declaration.JetKVMUSBIdentity) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "jetkvm_usb_identity does not match this JetKVM"})
		return
	}
	if declaration.PreferredMouseMode != "" && declaration.PreferredMouseMode != "digitizer" &&
		declaration.PreferredMouseMode != "absolute" && declaration.PreferredMouseMode != "relative" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported preferred_mouse_mode"})
		return
	}
	if declaration.State == "connected" && len(declaration.Evidence) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "connection evidence is required"})
		return
	}
	if declaration.State == "connected" && (declaration.DisplayWidth <= 0 || declaration.DisplayHeight <= 0) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "display dimensions are required"})
		return
	}

	metadata := setCompanionTargetMetadata(declaration)
	applyDisplayModeForTarget(metadata)
	metadata = withDisplayReconnectStatus(metadata)
	rememberCompanionStatus(companionID, c.ClientIP(), declaration)
	pendingActions := takeCompanionPendingActions(companionID)
	logger.Info().
		Str("companion_id", companionID).
		Str("state", declaration.State).
		Strs("evidence", metadata.Evidence).
		Str("preferred_mouse_mode", metadata.PreferredMouseMode).
		Int("display_width", metadata.DisplayWidth).
		Int("display_height", metadata.DisplayHeight).
		Float64("display_aspect", metadata.DisplayAspect).
		Bool("hdmi_reconnect_required", metadata.HDMIReconnectRequired).
		Msg("companion target declaration received")
	response := gin.H{
		"target_type":              metadata.TargetType,
		"preferred_mouse_mode":     metadata.PreferredMouseMode,
		"display_width":            metadata.DisplayWidth,
		"display_height":           metadata.DisplayHeight,
		"display_aspect":           metadata.DisplayAspect,
		"evidence":                 metadata.Evidence,
		"source":                   metadata.Source,
		"last_seen_unix_milli":     metadata.LastSeenUnixMilli,
		"lease_expires_unix_milli": metadata.LeaseExpiresUnixMilli,
		"hdmi_reconnect_required":  metadata.HDMIReconnectRequired,
		"fallback_display_mode":    metadata.FallbackDisplayMode,
		"companion_notice":         metadata.CompanionNotice,
		"fresh":                    metadata.Fresh,
		"requested_actions":        pendingActions,
	}
	c.JSON(http.StatusOK, response)
}

func handleCompanionPair(c *gin.Context) {
	var body companionPairRequestBody
	if err := c.ShouldBindJSON(&body); err != nil && !errors.Is(err, io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid pairing request"})
		return
	}
	if !isValidCompanionOTP(body.OTP) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "valid 6 digit otp is required"})
		return
	}
	if !isValidCompanionPublicKey(body.CompanionPublicKey) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "valid companion public key is required"})
		return
	}

	request := getOrCreateCompanionPairingRequest(c, body.OTP, body.CompanionPublicKey)
	showCompanionPairingPrompt(request)
	c.JSON(http.StatusAccepted, companionPairingPendingResponse(request))
}

func handleCompanionPairInitiate(c *gin.Context) {
	var body companionPairInitiateBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "companion url is required"})
		return
	}
	companionURL := normalizeCompanionURL(body.CompanionURL)
	if companionURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "companion url is required"})
		return
	}

	request := createJetKVMInitiatedCompanionPairingRequest(companionURL)
	jetkvmURL := "https://" + c.Request.Host
	if err := notifyCompanionPairRequest(companionURL, jetkvmURL, request.ID); err != nil {
		companionPairingLock.Lock()
		request.Status = "rejected"
		request.RejectionReason = "failed to notify companion: " + err.Error()
		companionPairingLock.Unlock()
		c.JSON(http.StatusBadGateway, gin.H{"error": request.RejectionReason})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"paired":        false,
		"status":        "pending",
		"request_id":    request.ID,
		"companion_url": companionURL,
		"otp":           request.OTP,
	})
}

func handleCompanionPairStatus(c *gin.Context) {
	request := getCompanionPairingRequest(c.Param("id"))
	if request == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "pairing request not found"})
		return
	}
	if request.Status == "paired" {
		c.JSON(http.StatusOK, companionPairingResponseWithCompanion(request.CompanionID))
		return
	}
	c.JSON(http.StatusOK, companionPairingPendingResponse(request))
}

func handleCompanionPairRequests(c *gin.Context) {
	request := getCurrentCompanionPairingRequest()
	requests := []gin.H{}
	if request != nil && request.Status == "pending" {
		requests = append(requests, companionPairingPendingResponse(request))
	}
	c.JSON(http.StatusOK, gin.H{"requests": requests})
}

func handleCompanionStatus(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"companions":  getCompanionStatusSnapshots(),
		"visible_ips": getVisibleLocalIPEntries(),
	})
}

func handleCompanionPermissionRequest(c *gin.Context) {
	companionID := strings.TrimSpace(c.Param("id"))
	if companionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "companion id is required"})
		return
	}
	if _, ok := companionAuthorizations()[companionID]; !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "companion not paired"})
		return
	}

	var body companionPermissionActionBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "permission is required"})
		return
	}
	action := companionPermissionAction(body.Permission)
	if action == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported permission"})
		return
	}
	queueCompanionPendingAction(companionID, action)
	c.JSON(http.StatusAccepted, gin.H{"queued": true, "action": action})
}

func handleCompanionPairClaim(c *gin.Context) {
	request := getCompanionPairingRequest(c.Param("id"))
	if request == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "pairing request not found"})
		return
	}
	if request.Status != "pending" {
		c.JSON(http.StatusConflict, gin.H{"error": "pairing request is not pending"})
		return
	}
	if request.Direction != "jetkvm" {
		c.JSON(http.StatusConflict, gin.H{"error": "pairing request must be approved in JetKVM web UI"})
		return
	}

	var body companionPairApproveBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "otp is required"})
		return
	}
	if body.OTP != request.OTP {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "otp does not match"})
		return
	}
	if !isValidCompanionPublicKey(body.CompanionPublicKey) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "valid companion public key is required"})
		return
	}

	companionID := addCompanionAuthorization(body.CompanionPublicKey)
	if err := SaveConfig(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save companion pairing"})
		return
	}

	companionPairingLock.Lock()
	request.Status = "paired"
	request.CompanionID = companionID
	request.CompanionPubKey = body.CompanionPublicKey
	companionPairingLock.Unlock()
	c.JSON(http.StatusOK, companionPairingResponseWithCompanion(companionID))
}

func handleCompanionPairApprove(c *gin.Context) {
	request := getCompanionPairingRequest(c.Param("id"))
	if request == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "pairing request not found"})
		return
	}
	if request.Status != "pending" {
		c.JSON(http.StatusConflict, gin.H{"error": "pairing request is not pending"})
		return
	}

	var body companionPairApproveBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "otp is required"})
		return
	}
	if body.OTP != request.OTP {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "otp does not match"})
		return
	}
	if !isValidCompanionPublicKey(request.CompanionPubKey) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "valid companion public key is required"})
		return
	}

	companionID := addCompanionAuthorization(request.CompanionPubKey)
	if err := SaveConfig(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save companion pairing"})
		return
	}

	companionPairingLock.Lock()
	request.Status = "paired"
	request.CompanionID = companionID
	companionPairingLock.Unlock()

	logger.Info().
		Str("remote_addr", request.RemoteAddr).
		Str("request_id", request.ID).
		Msg("companion pairing approved")
	c.JSON(http.StatusOK, companionPairingResponseWithCompanion(companionID))
}

func handleCompanionPairReject(c *gin.Context) {
	request := getCompanionPairingRequest(c.Param("id"))
	if request == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "pairing request not found"})
		return
	}

	companionPairingLock.Lock()
	request.Status = "rejected"
	request.RejectionReason = "rejected on JetKVM"
	companionPairingLock.Unlock()

	logger.Info().
		Str("remote_addr", request.RemoteAddr).
		Str("request_id", request.ID).
		Msg("companion pairing rejected")
	c.JSON(http.StatusOK, companionPairingPendingResponse(request))
}

func handleCompanionUnpair(c *gin.Context) {
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	companionID, ok := companionIDFromSignedRequest(c, bodyBytes)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid companion signature"})
		return
	}
	removeCompanionAuthorization(companionID)
	forgetCompanionStatus(companionID)
	_ = SaveConfig()
	clearCompanionTargetMetadata()
	c.JSON(http.StatusOK, gin.H{"paired": false})
}

func handleCompanionUnpairAdmin(c *gin.Context) {
	clearCompanionPairing()
	c.JSON(http.StatusOK, gin.H{"paired": false})
}

func companionPairingResponse() gin.H {
	companions := companionAuthorizations()
	companionID := ""
	for id := range companions {
		companionID = id
		break
	}
	return companionPairingResponseWithCompanion(companionID)
}

func companionPairingResponseWithCompanion(companionID string) gin.H {
	deviceID := GetDeviceID()
	shortID := strings.ToLower(deviceID)
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}

	usbProduct := ""
	usbSerial := ""
	if config.UsbConfig != nil {
		usbProduct = config.UsbConfig.Product
		usbSerial = config.UsbConfig.SerialNumber
	}

	return gin.H{
		"paired":                true,
		"status":                "paired",
		"companion_id":          companionID,
		"jetkvm_device_id":      deviceID,
		"jetkvm_identity_token": shortID,
		"jetkvm_usb_product":    usbProduct,
		"jetkvm_usb_serial":     usbSerial,
	}
}

func companionPairingPendingResponse(request *companionPairRequest) gin.H {
	status := request.Status
	if status == "" {
		status = "pending"
	}
	response := gin.H{
		"paired":      false,
		"status":      status,
		"request_id":  request.ID,
		"direction":   request.Direction,
		"remote_addr": request.RemoteAddr,
		"user_agent":  request.UserAgent,
		"created_at":  request.RequestedAt.UnixMilli(),
	}
	if request.RejectionReason != "" {
		response["error"] = request.RejectionReason
	}
	return response
}

func getOrCreateCompanionPairingRequest(c *gin.Context, otp string, companionPublicKey string) *companionPairRequest {
	companionPairingLock.Lock()
	defer companionPairingLock.Unlock()

	now := time.Now()
	if companionPairingRequest != nil &&
		companionPairingRequest.Status == "pending" &&
		now.Sub(companionPairingRequest.RequestedAt) <= companionPairRequestTTL &&
		companionPairingRequest.CompanionPubKey == companionPublicKey {
		return companionPairingRequest
	}

	companionPairingRequest = &companionPairRequest{
		ID:              uuid.NewString(),
		Direction:       "companion",
		RemoteAddr:      c.ClientIP(),
		UserAgent:       c.GetHeader("User-Agent"),
		RequestedAt:     now,
		Status:          "pending",
		OTP:             otp,
		CompanionPubKey: companionPublicKey,
	}
	return companionPairingRequest
}

func createJetKVMInitiatedCompanionPairingRequest(companionURL string) *companionPairRequest {
	companionPairingLock.Lock()
	defer companionPairingLock.Unlock()

	companionPairingRequest = &companionPairRequest{
		ID:          uuid.NewString(),
		Direction:   "jetkvm",
		RemoteAddr:  companionURL,
		UserAgent:   "JetKVM",
		RequestedAt: time.Now(),
		Status:      "pending",
		OTP:         fmt.Sprintf("%06d", time.Now().UnixNano()%1000000),
	}
	return companionPairingRequest
}

func getCurrentCompanionPairingRequest() *companionPairRequest {
	companionPairingLock.Lock()
	defer companionPairingLock.Unlock()

	if companionPairingRequest == nil {
		return nil
	}
	if companionPairingRequest.Status == "pending" &&
		time.Since(companionPairingRequest.RequestedAt) > companionPairRequestTTL {
		companionPairingRequest.Status = "expired"
		companionPairingRequest.RejectionReason = "pairing request expired"
	}
	return companionPairingRequest
}

func getCompanionPairingRequest(id string) *companionPairRequest {
	companionPairingLock.Lock()
	defer companionPairingLock.Unlock()

	if companionPairingRequest == nil || companionPairingRequest.ID != id {
		return nil
	}
	if companionPairingRequest.Status == "pending" &&
		time.Since(companionPairingRequest.RequestedAt) > companionPairRequestTTL {
		companionPairingRequest.Status = "expired"
		companionPairingRequest.RejectionReason = "pairing request expired"
	}
	return companionPairingRequest
}

func showCompanionPairingPrompt(request *companionPairRequest) {
	logger.Info().
		Str("remote_addr", request.RemoteAddr).
		Str("request_id", request.ID).
		Msg("companion pairing pending approval")
	if nativeInstance != nil {
		nativeInstance.UpdateLabelAndChangeVisibility("cloud_status_label", "Companion pair request")
		nativeInstance.UpdateLabelAndChangeVisibility("home_info_ipv4_addr", request.RemoteAddr)
	}
}

func clearCompanionPairing() {
	config.CompanionPairing.Token = ""
	config.CompanionPairing.Tokens = nil
	config.CompanionPairing.Companions = nil
	_ = SaveConfig()
	clearCompanionTargetMetadata()
	clearCompanionStatuses()
	companionPairingLock.Lock()
	companionPairingRequest = nil
	companionPairingLock.Unlock()
}

func companionPairingTokens() []string {
	tokens := make([]string, 0, len(config.CompanionPairing.Tokens)+1)
	seen := map[string]bool{}
	for _, token := range config.CompanionPairing.Tokens {
		token = strings.TrimSpace(token)
		if token == "" || seen[token] {
			continue
		}
		tokens = append(tokens, token)
		seen[token] = true
	}
	legacyToken := strings.TrimSpace(config.CompanionPairing.Token)
	if legacyToken != "" && !seen[legacyToken] {
		tokens = append(tokens, legacyToken)
	}
	return tokens
}

func companionAuthorizations() map[string]CompanionAuthorization {
	result := map[string]CompanionAuthorization{}
	for id, authorization := range config.CompanionPairing.Companions {
		id = strings.TrimSpace(id)
		publicKey := strings.TrimSpace(authorization.PublicKey)
		if id != "" && publicKey != "" {
			result[id] = CompanionAuthorization{PublicKey: publicKey}
		}
	}
	return result
}

func addCompanionAuthorization(publicKey string) string {
	publicKey = strings.TrimSpace(publicKey)
	if config.CompanionPairing.Companions == nil {
		config.CompanionPairing.Companions = map[string]CompanionAuthorization{}
	}
	for id, authorization := range config.CompanionPairing.Companions {
		if authorization.PublicKey == publicKey {
			return id
		}
	}
	companionID := uuid.NewString()
	config.CompanionPairing.Companions[companionID] = CompanionAuthorization{PublicKey: publicKey}
	config.CompanionPairing.Token = ""
	config.CompanionPairing.Tokens = nil
	return companionID
}

func removeCompanionAuthorization(companionID string) {
	companionID = strings.TrimSpace(companionID)
	if companionID == "" || config.CompanionPairing.Companions == nil {
		return
	}
	delete(config.CompanionPairing.Companions, companionID)
	if len(config.CompanionPairing.Companions) == 0 {
		config.CompanionPairing.Companions = nil
	}
}

func rememberCompanionStatus(companionID string, remoteAddr string, declaration CompanionTargetDeclaration) {
	companionID = strings.TrimSpace(companionID)
	if companionID == "" {
		return
	}
	peripherals := map[string]bool{}
	for _, evidence := range declaration.Evidence {
		switch strings.ToLower(strings.TrimSpace(evidence)) {
		case "keyboard", "digitizer", "mouse", "monitor":
			peripherals[strings.ToLower(strings.TrimSpace(evidence))] = true
		}
	}
	status := companionStatusSnapshot{
		CompanionID:                      companionID,
		RemoteAddr:                       remoteAddr,
		LastSeenUnixMilli:                time.Now().UnixMilli(),
		NotificationPermissionGranted:    declaration.NotificationPermissionGranted,
		DisplayOverAppsPermissionGranted: declaration.DisplayOverAppsPermissionGranted,
		BatteryUnrestrictedGranted:       declaration.BatteryUnrestrictedGranted,
		PairedJetKVMURLs:                 cleanStringList(declaration.PairedJetKVMURLs),
		VisibleIPs:                       cleanStringList(declaration.VisibleIPs),
		JetKVMUSBIdentity:                strings.TrimSpace(declaration.JetKVMUSBIdentity),
		TargetType:                       strings.TrimSpace(declaration.TargetType),
		PreferredMouseMode:               strings.TrimSpace(declaration.PreferredMouseMode),
		DisplayWidth:                     declaration.DisplayWidth,
		DisplayHeight:                    declaration.DisplayHeight,
		Evidence:                         cleanStringList(declaration.Evidence),
		Peripherals:                      peripherals,
	}

	companionStatusLock.Lock()
	companionStatuses[companionID] = status
	companionStatusLock.Unlock()
}

func getCompanionStatusSnapshots() []companionStatusSnapshot {
	authorizations := companionAuthorizations()
	companionStatusLock.Lock()
	defer companionStatusLock.Unlock()

	snapshots := make([]companionStatusSnapshot, 0, len(authorizations))
	for companionID := range authorizations {
		status := companionStatuses[companionID]
		status.CompanionID = companionID
		if status.Peripherals == nil {
			status.Peripherals = map[string]bool{}
		}
		status.PendingActions = pendingCompanionActionsLocked(companionID)
		snapshots = append(snapshots, status)
	}
	return snapshots
}

func queueCompanionPendingAction(companionID string, action string) {
	companionStatusLock.Lock()
	defer companionStatusLock.Unlock()

	if companionPendingActions[companionID] == nil {
		companionPendingActions[companionID] = map[string]bool{}
	}
	companionPendingActions[companionID][action] = true
}

func takeCompanionPendingActions(companionID string) []string {
	companionStatusLock.Lock()
	defer companionStatusLock.Unlock()

	actions := pendingCompanionActionsLocked(companionID)
	delete(companionPendingActions, companionID)
	return actions
}

func pendingCompanionActionsLocked(companionID string) []string {
	pending := companionPendingActions[companionID]
	actions := make([]string, 0, len(pending))
	for action := range pending {
		actions = append(actions, action)
	}
	slices.Sort(actions)
	return actions
}

func forgetCompanionStatus(companionID string) {
	companionStatusLock.Lock()
	defer companionStatusLock.Unlock()

	delete(companionStatuses, companionID)
	delete(companionPendingActions, companionID)
}

func clearCompanionStatuses() {
	companionStatusLock.Lock()
	defer companionStatusLock.Unlock()

	companionStatuses = map[string]companionStatusSnapshot{}
	companionPendingActions = map[string]map[string]bool{}
}

func companionPermissionAction(permission string) string {
	switch strings.ToLower(strings.TrimSpace(permission)) {
	case "notification", "notifications":
		return "request_notification_permission"
	case "overlay", "display_over_apps", "display-over-apps":
		return "request_display_over_apps_permission"
	case "battery", "unrestricted_battery", "unrestricted-battery":
		return "request_unrestricted_battery_permission"
	default:
		return ""
	}
}

func getVisibleLocalIPEntries() []companionIPEntry {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	entries := []companionIPEntry{}
	seen := map[string]bool{}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ip := ipFromAddr(addr)
			if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
				continue
			}
			text := ip.String()
			if seen[text] {
				continue
			}
			seen[text] = true
			entries = append(entries, companionIPEntry{IP: text, Source: "backend", Interface: iface.Name})
		}
	}
	return entries
}

func ipFromAddr(addr net.Addr) net.IP {
	switch v := addr.(type) {
	case *net.IPNet:
		return v.IP
	case *net.IPAddr:
		return v.IP
	default:
		return nil
	}
}

func cleanStringList(values []string) []string {
	cleaned := []string{}
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		cleaned = append(cleaned, value)
	}
	return cleaned
}

func normalizeCompanionURL(rawURL string) string {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return ""
	}
	if !strings.Contains(trimmed, "://") {
		trimmed = "https://" + trimmed
	}
	parsed, err := neturl.Parse(trimmed)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return ""
	}
	if parsed.Port() == "" {
		parsed.Host = net.JoinHostPort(parsed.Hostname(), "8787")
		trimmed = parsed.String()
	}
	return strings.TrimRight(trimmed, "/")
}

func companionIDFromSignedRequest(c *gin.Context, body []byte) (string, bool) {
	companionID := strings.TrimSpace(c.GetHeader("X-JetKVM-Companion-ID"))
	if companionID == "" {
		return "", false
	}
	authorization, ok := companionAuthorizations()[companionID]
	if !ok || authorization.PublicKey == "" {
		return "", false
	}

	timestamp := strings.TrimSpace(c.GetHeader("X-JetKVM-Timestamp"))
	nonce := strings.TrimSpace(c.GetHeader("X-JetKVM-Nonce"))
	signatureText := strings.TrimSpace(c.GetHeader("X-JetKVM-Signature"))
	if timestamp == "" || nonce == "" || signatureText == "" {
		return "", false
	}

	requestTime, err := time.Parse(time.RFC3339Nano, timestamp)
	if err != nil {
		return "", false
	}
	now := time.Now()
	if requestTime.Before(now.Add(-companionSignatureMaxSkew)) || requestTime.After(now.Add(companionSignatureMaxSkew)) {
		return "", false
	}
	if hasCompanionNonce(companionID, nonce) {
		return "", false
	}

	publicKeyBytes, err := base64.StdEncoding.DecodeString(authorization.PublicKey)
	if err != nil {
		return "", false
	}
	publicKey, err := x509.ParsePKIXPublicKey(publicKeyBytes)
	if err != nil {
		return "", false
	}
	ecdsaPublicKey, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		return "", false
	}
	signature, err := base64.StdEncoding.DecodeString(signatureText)
	if err != nil {
		return "", false
	}
	bodyHashBytes := sha256.Sum256(body)
	bodyHash := hex.EncodeToString(bodyHashBytes[:])
	canonical := strings.Join([]string{
		c.Request.Method,
		c.Request.URL.Path,
		timestamp,
		nonce,
		bodyHash,
	}, "\n")
	digest := sha256.Sum256([]byte(canonical))
	if !ecdsa.VerifyASN1(ecdsaPublicKey, digest[:], signature) {
		return "", false
	}
	if !rememberCompanionNonce(companionID, nonce, now.Add(companionSignatureMaxSkew)) {
		return "", false
	}
	return companionID, true
}

func hasValidCompanionSignature(c *gin.Context, body []byte) bool {
	_, ok := companionIDFromSignedRequest(c, body)
	return ok
}

func hasCompanionNonce(companionID string, nonce string) bool {
	if len(nonce) < 16 || len(nonce) > 128 {
		return true
	}
	companionNonceLock.Lock()
	defer companionNonceLock.Unlock()
	pruneCompanionNoncesLocked(time.Now())

	for knownNonce := range companionNonces[companionID] {
		if subtle.ConstantTimeCompare([]byte(knownNonce), []byte(nonce)) == 1 {
			return true
		}
	}
	return false
}

func rememberCompanionNonce(companionID string, nonce string, expiresAt time.Time) bool {
	companionNonceLock.Lock()
	defer companionNonceLock.Unlock()
	pruneCompanionNoncesLocked(time.Now())

	if companionNonces[companionID] == nil {
		companionNonces[companionID] = map[string]time.Time{}
	}
	for knownNonce := range companionNonces[companionID] {
		if subtle.ConstantTimeCompare([]byte(knownNonce), []byte(nonce)) == 1 {
			return false
		}
	}
	companionNonces[companionID][nonce] = expiresAt
	return true
}

func pruneCompanionNoncesLocked(now time.Time) {
	for id, nonces := range companionNonces {
		for knownNonce, expiry := range nonces {
			if !expiry.After(now) {
				delete(nonces, knownNonce)
			}
		}
		if len(nonces) == 0 {
			delete(companionNonces, id)
		}
	}
}

func isValidCompanionPublicKey(publicKey string) bool {
	publicKey = strings.TrimSpace(publicKey)
	if publicKey == "" {
		return false
	}
	publicKeyBytes, err := base64.StdEncoding.DecodeString(publicKey)
	if err != nil {
		return false
	}
	parsed, err := x509.ParsePKIXPublicKey(publicKeyBytes)
	if err != nil {
		return false
	}
	_, ok := parsed.(*ecdsa.PublicKey)
	return ok
}

func notifyCompanionPairRequest(companionURL string, jetkvmURL string, requestID string) error {
	parsed, err := neturl.Parse(companionURL)
	if err != nil || parsed.Scheme != "https" {
		return fmt.Errorf("companion pairing notification requires https")
	}
	if parsedJetKVMURL, err := neturl.Parse(jetkvmURL); err != nil || parsedJetKVMURL.Scheme != "https" {
		return fmt.Errorf("JetKVM pairing URL requires https")
	}
	bodyText := fmt.Sprintf(
		`{"jetkvm_url":%q,"request_id":%q}`,
		jetkvmURL,
		requestID,
	)
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	var lastErr error
	for attempt := 1; attempt <= 5; attempt++ {
		req, err := http.NewRequest(http.MethodPost, companionURL+"/pair/request", strings.NewReader(bodyText))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			logger.Warn().
				Err(err).
				Str("companion_url", companionURL).
				Int("attempt", attempt).
				Msg("failed to notify companion pairing request")
		} else {
			defer resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return nil
			}
			lastErr = fmt.Errorf("companion returned HTTP %d", resp.StatusCode)
			logger.Warn().
				Int("status", resp.StatusCode).
				Str("companion_url", companionURL).
				Int("attempt", attempt).
				Msg("companion pairing request notification rejected")
		}
		if attempt < 5 {
			time.Sleep(time.Duration(attempt*attempt) * time.Second)
		}
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("companion notification failed")
}

func isValidCompanionOTP(otp string) bool {
	if len(otp) != 6 {
		return false
	}
	for _, r := range otp {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func isValidJetKVMUSBIdentity(identity string) bool {
	normalized := strings.ToLower(strings.TrimSpace(identity))
	if normalized == "" {
		return false
	}
	deviceID := strings.ToLower(GetDeviceID())
	if normalized == deviceID {
		return true
	}
	shortID := deviceID
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}
	return normalized == shortID
}

func shouldUseAndroidNativeLogin(c *gin.Context) bool {
	userAgent := c.GetHeader("User-Agent")
	if strings.Contains(userAgent, "JetKVMWebView/1") {
		return true
	}
	return c.Query("jetkvmAndroid") == "1"
}

func hasValidLocalAuthCookie(c *gin.Context) bool {
	if config.LocalAuthMode == "noPassword" {
		return true
	}
	authToken, err := c.Cookie("authToken")
	return err == nil && authToken != "" && authToken == config.LocalAuthToken
}

func handleAndroidNativeLogin(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.String(http.StatusOK, `<!doctype html>
<html>
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>JetKVM login</title>
  <style>
    html, body {
      background: #050814;
      color: #e5e7eb;
      font: 16px system-ui, sans-serif;
      height: 100%;
      margin: 0;
    }
    body {
      align-items: center;
      display: flex;
      justify-content: center;
      text-align: center;
    }
  </style>
</head>
<body>
  <div>Open the JetKVM Android login screen.</div>
  <script>
    (function () {
      if (window.JetKVMAndroid && window.JetKVMAndroid.showNativeLogin) {
        window.JetKVMAndroid.showNativeLogin(window.location.href);
      }
    })();
  </script>
</body>
</html>`)
}

// TODO: support multiple sessions?
var currentSession *Session

func handleWebRTCSession(c *gin.Context) {
	var req WebRTCSessionRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	session, err := newSession(SessionConfig{MDNSMode: config.NetworkConfig.MDNSMode.String})
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
		gadget.CancelAllAutoReleaseTimers()
		_ = rpcKeyboardReport(0, keyboardClearStateKeys)
		peerConn := currentSession.peerConnection
		go func() {
			time.Sleep(1 * time.Second)
			_ = peerConn.Close()
		}()
	}

	// Cancel any ongoing keyboard macro when session changes
	cancelKeyboardMacro()

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
			scopedLogger.Debug().Bytes("payload", payload).Msg("ping frame received")

			metricConnectionTotalPingReceivedCount.WithLabelValues("local", source).Inc()
			metricConnectionLastPingReceivedTimestamp.WithLabelValues("local", source).SetToCurrentTime()

			return true
		},
	}

	wsCon, err := websocket.Accept(c.Writer, c.Request, wsOptions)
	if err != nil {
		scopedLogger.Warn().Err(err).Msg("failed to accept websocket connection")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to establish WebSocket connection"})
		return
	}

	// Now use conn for websocket operations
	defer wsCon.Close(websocket.StatusNormalClosure, "")

	err = wsjson.Write(context.Background(), wsCon, gin.H{"type": "device-metadata", "data": gin.H{"deviceVersion": builtAppVersion}})
	if err != nil {
		scopedLogger.Warn().Err(err).Msg("failed to write device metadata")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to send device metadata"})
		return
	}

	err = handleWebRTCSignalWsMessages(wsCon, false, source, connectionID, &scopedLogger)
	if err != nil {
		scopedLogger.Warn().Err(err).Msg("websocket session ended with error")
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
	defer func() {
		if isCloudConnection {
			setCloudConnectionState(CloudConnectionStateDisconnected)
		}
		cancelRun()
	}()

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

	// Check rate limit before processing
	ip := c.ClientIP()
	if allowed, retryAfter := passwordRateLimiter.IsAllowed(ip); !allowed {
		c.Header("Retry-After", fmt.Sprintf("%d", retryAfter))
		c.JSON(http.StatusTooManyRequests, gin.H{
			"error":       "Too many failed attempts. Please try again later.",
			"retry_after": retryAfter,
		})
		return
	}

	var req LoginRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	err := bcrypt.CompareHashAndPassword([]byte(config.HashedPassword), []byte(req.Password))
	if err != nil {
		passwordRateLimiter.RecordFailure(ip)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid password"})
		return
	}

	// Clear rate limit on successful login
	passwordRateLimiter.RecordSuccess(ip)

	config.LocalAuthToken = uuid.New().String()

	if err := SaveConfig(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save configuration"})
		return
	}

	// Set the cookie
	c.SetCookie("authToken", config.LocalAuthToken, authTokenMaxAge, "/", "", false, true)

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

func sendErrorJsonThenAbort(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{"error": message})
	c.Abort()
}

func basicAuthProtectedMiddleware(requireDeveloperMode bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if requireDeveloperMode {
			devModeState, err := rpcGetDevModeState()
			if err != nil {
				sendErrorJsonThenAbort(c, http.StatusInternalServerError, "Failed to get developer mode state")
				return
			}

			if !devModeState.Enabled {
				sendErrorJsonThenAbort(c, http.StatusUnauthorized, "Developer mode is not enabled")
				return
			}
		}

		if config.LocalAuthMode == "noPassword" {
			sendErrorJsonThenAbort(c, http.StatusForbidden, "The resource is not available in noPassword mode")
			return
		}

		// calculate basic auth credentials
		_, password, ok := c.Request.BasicAuth()
		if !ok {
			c.Header("WWW-Authenticate", "Basic realm=\"JetKVM\"")
			sendErrorJsonThenAbort(c, http.StatusUnauthorized, "Basic auth is required")
			return
		}

		err := bcrypt.CompareHashAndPassword([]byte(config.HashedPassword), []byte(password))
		if err != nil {
			sendErrorJsonThenAbort(c, http.StatusUnauthorized, "Invalid password")
			return
		}

		c.Next()
	}
}

func getBindAddress(listenPort int) string {
	// Determine the binding address based on the config
	var bindAddress string
	useIPv4 := config.NetworkConfig.IPv4Mode.String != "disabled"
	useIPv6 := config.NetworkConfig.IPv6Mode.String != "disabled"

	if config.LocalLoopbackOnly {
		if useIPv4 && useIPv6 {
			bindAddress = fmt.Sprintf("localhost:%d", listenPort)
		} else if useIPv4 {
			bindAddress = fmt.Sprintf("127.0.0.1:%d", listenPort)
		} else if useIPv6 {
			bindAddress = fmt.Sprintf("[::1]:%d", listenPort)
		}
	} else {
		if useIPv4 && useIPv6 {
			bindAddress = fmt.Sprintf(":%d", listenPort)
		} else if useIPv4 {
			bindAddress = fmt.Sprintf("0.0.0.0:%d", listenPort)
		} else if useIPv6 {
			bindAddress = fmt.Sprintf("[::]:%d", listenPort)
		}
	}
	return bindAddress
}

func RunWebServer() {
	r := setupRouter()

	// Determine the binding address based on the config
	bindAddress := getBindAddress(80) // default port

	logger.Info().Str("bindAddress", bindAddress).Bool("loopbackOnly", config.LocalLoopbackOnly).Msg("Starting web server")
	if err := r.Run(bindAddress); err != nil {
		panic(err)
	}
}

func handleDevice(c *gin.Context) {
	response := LocalDevice{
		AuthMode:     &config.LocalAuthMode,
		DeviceID:     GetDeviceID(),
		LoopbackOnly: config.LocalLoopbackOnly,
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

	if len(req.Password) < MinPasswordLength {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Password must be at least 8 characters"})
		return
	}

	if len(req.Password) > MaxPasswordLength {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Password must be at most 72 characters"})
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
	c.SetCookie("authToken", config.LocalAuthToken, authTokenMaxAge, "/", "", false, true)

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

	// Validate new password length (not old password - may be shorter from before this requirement)
	if len(req.NewPassword) < MinPasswordLength {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Password must be at least 8 characters"})
		return
	}

	if len(req.NewPassword) > MaxPasswordLength {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Password must be at most 72 characters"})
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
	c.SetCookie("authToken", config.LocalAuthToken, authTokenMaxAge, "/", "", false, true)

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
	// Add CORS headers to allow cross-origin requests
	// This is safe because device/status is a public endpoint
	c.Header("Access-Control-Allow-Origin", "*")
	c.Header("Access-Control-Allow-Methods", "GET, OPTIONS")
	c.Header("Access-Control-Allow-Headers", "Content-Type")

	// Handle preflight requests
	if c.Request.Method == "OPTIONS" {
		c.AbortWithStatus(http.StatusNoContent)
		return
	}

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

func handleSetup(c *gin.Context) {
	// Check if the device is already set up
	if config.LocalAuthMode != "" || config.HashedPassword != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Device is already set up"})
		return
	}

	ip := c.ClientIP()
	if allowed, retryAfter := passwordRateLimiter.IsAllowed(ip); !allowed {
		c.Header("Retry-After", fmt.Sprintf("%d", retryAfter))
		c.JSON(http.StatusTooManyRequests, gin.H{
			"error":       "Too many failed attempts. Please try again later.",
			"retry_after": retryAfter,
		})
		return
	}

	var req SetupRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		passwordRateLimiter.RecordFailure(ip)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	if req.LocalAuthMode != "password" && req.LocalAuthMode != "noPassword" {
		passwordRateLimiter.RecordFailure(ip)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid localAuthMode"})
		return
	}

	config.LocalAuthMode = req.LocalAuthMode

	if req.LocalAuthMode == "password" {
		if req.Password == "" {
			passwordRateLimiter.RecordFailure(ip)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Password is required for password mode"})
			return
		}

		if len(req.Password) < MinPasswordLength {
			passwordRateLimiter.RecordFailure(ip)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Password must be at least 8 characters"})
			return
		}

		if len(req.Password) > MaxPasswordLength {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Password must be at most 72 characters"})
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
		c.SetCookie("authToken", config.LocalAuthToken, authTokenMaxAge, "/", "", false, true)
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

func handleSendWOLMagicPacket(c *gin.Context) {
	inputMacAddr := c.Param("mac-addr")
	macAddr, err := net.ParseMAC(inputMacAddr)
	if err != nil {
		logger.Warn().Err(err).Str("inputMacAddr", inputMacAddr).Msg("Invalid MAC address provided")
		c.String(http.StatusBadRequest, "Invalid mac address provided")
		return
	}

	macAddrString := macAddr.String()
	broadcastIP := c.Query("broadcastIP")
	err = rpcSendWOLMagicPacket(macAddrString, broadcastIP)
	if err != nil {
		logger.Warn().Err(err).Str("macAddrString", macAddrString).Msg("Failed to send WOL magic packet")
		c.String(http.StatusInternalServerError, "Failed to send WOL to %s: %v", macAddrString, err)
		return
	}

	c.String(http.StatusOK, "WOL sent to %s ", macAddr)
}

func handleDiagnosticsDownload(c *gin.Context) {
	pr, pw := io.Pipe()

	go func() {
		defer pw.Close()

		zw := zip.NewWriter(pw)

		// 1. Application log (full, no truncation)
		if err := addFileToZip(zw, "app.log", supervisor.AppLogPath); err != nil {
			logger.Warn().Err(err).Msg("failed to add app log to diagnostics zip")
		}

		// 2. System diagnostics
		var diagBuf bytes.Buffer
		diag := diagnostics.New(diagnostics.Options{
			Writer: &diagBuf,
			GetSessionInfo: func() diagnostics.SessionInfo {
				info := diagnostics.SessionInfo{
					ActiveSessions:    getActiveSessions(),
					HasCurrentSession: currentSession != nil,
				}
				if currentSession != nil {
					sessionInfo := currentSession.GetDiagnosticsInfo()
					info.ICEConnectionState = sessionInfo.ICEConnectionState
					info.SignalingState = sessionInfo.SignalingState
					info.ConnectionState = sessionInfo.ConnectionState
					info.DataChannels = sessionInfo.DataChannels
				}
				return info
			},
		})
		diag.LogAll("download")
		if err := addBytesToZip(zw, "system-diagnostics.txt", diagBuf.Bytes()); err != nil {
			logger.Warn().Err(err).Msg("failed to add system diagnostics to zip")
		}

		// 3. All crash dumps (full content)
		if entries, err := filepath.Glob(filepath.Join(supervisor.ErrorDumpDir, "jetkvm-*.log")); err == nil {
			for _, path := range entries {
				if err := addFileToZip(zw, "crashes/"+filepath.Base(path), path); err != nil {
					logger.Warn().Err(err).Str("path", path).Msg("failed to add crash dump to zip")
				}
			}
		}

		// 4. Configuration (with secrets redacted)
		redactedConfig := *config
		redactedConfig.CloudToken = ""
		redactedConfig.LocalAuthToken = ""
		redactedConfig.HashedPassword = ""
		redactedConfig.GoogleIdentity = ""
		if configData, err := json.MarshalIndent(redactedConfig, "", "  "); err == nil {
			if err := addBytesToZip(zw, "config.json", configData); err != nil {
				logger.Warn().Err(err).Msg("failed to add config to zip")
			}
		}

		// Close ZIP writer to write central directory (required for valid ZIP)
		if err := zw.Close(); err != nil {
			logger.Error().Err(err).Msg("failed to finalize diagnostics zip")
		}
	}()

	filename := fmt.Sprintf("jetkvm-diagnostics-%s.zip", time.Now().Format("20060102-150405"))
	extraHeaders := map[string]string{
		"Content-Disposition": fmt.Sprintf("attachment; filename=%s", filename),
	}

	c.DataFromReader(http.StatusOK, -1, "application/zip", pr, extraHeaders)
}

func addFileToZip(zw *zip.Writer, name, srcPath string) error {
	f, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer f.Close()

	w, err := zw.Create(name)
	if err != nil {
		return err
	}

	_, err = io.Copy(w, f)
	return err
}

func addBytesToZip(zw *zip.Writer, name string, data []byte) error {
	w, err := zw.Create(name)
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}
