package kvm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/coder/websocket/wsjson"
	"github.com/pion/webrtc/v4"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/coreos/go-oidc/v3/oidc"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"
)

type CloudRegisterRequest struct {
	Token      string `json:"token"`
	CloudAPI   string `json:"cloudApi"`
	OidcGoogle string `json:"oidcGoogle"`
	ClientId   string `json:"clientId"`
}

const (
	// CloudWebSocketConnectTimeout is the timeout for the websocket connection to the cloud
	CloudWebSocketConnectTimeout = 1 * time.Minute
	// CloudAPIRequestTimeout is the timeout for cloud API requests
	CloudAPIRequestTimeout = 10 * time.Second
	// CloudOidcRequestTimeout is the timeout for OIDC token verification requests
	// should be lower than the websocket response timeout set in cloud-api
	CloudOidcRequestTimeout = 10 * time.Second
	// CloudWebSocketPingInterval is the interval at which the websocket client sends ping messages to the cloud
	CloudWebSocketPingInterval = 15 * time.Second
)

var (
	metricCloudConnectionStatus = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "jetkvm_cloud_connection_status",
			Help: "The status of the cloud connection",
		},
	)
	metricCloudConnectionEstablishedTimestamp = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "jetkvm_cloud_connection_established_timestamp",
			Help: "The timestamp when the cloud connection was established",
		},
	)
	metricCloudConnectionLastPingTimestamp = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "jetkvm_cloud_connection_last_ping_timestamp",
			Help: "The timestamp when the last ping response was received",
		},
	)
	metricCloudConnectionLastPingDuration = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "jetkvm_cloud_connection_last_ping_duration",
			Help: "The duration of the last ping response",
		},
	)
	metricCloudConnectionPingDuration = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name: "jetkvm_cloud_connection_ping_duration",
			Help: "The duration of the ping response",
			Buckets: []float64{
				0.1, 0.5, 1, 10,
			},
		},
	)
	metricCloudConnectionTotalPingCount = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "jetkvm_cloud_connection_total_ping_count",
			Help: "The total number of pings sent to the cloud",
		},
	)
	metricCloudConnectionSessionRequestCount = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "jetkvm_cloud_connection_session_total_request_count",
			Help: "The total number of session requests received from the cloud",
		},
	)
	metricCloudConnectionSessionRequestDuration = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name: "jetkvm_cloud_connection_session_request_duration",
			Help: "The duration of session requests",
			Buckets: []float64{
				0.1, 0.5, 1, 10,
			},
		},
	)
	metricCloudConnectionLastSessionRequestTimestamp = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "jetkvm_cloud_connection_last_session_request_timestamp",
			Help: "The timestamp of the last session request",
		},
	)
	metricCloudConnectionLastSessionRequestDuration = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "jetkvm_cloud_connection_last_session_request_duration",
			Help: "The duration of the last session request",
		},
	)
	metricCloudConnectionFailureCount = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "jetkvm_cloud_connection_failure_count",
			Help: "The number of times the cloud connection has failed",
		},
	)
)

var (
	cloudDisconnectChan chan error
	cloudDisconnectLock = &sync.Mutex{}
)

func cloudResetMetrics(established bool) {
	metricCloudConnectionLastPingTimestamp.Set(-1)
	metricCloudConnectionLastPingDuration.Set(-1)

	metricCloudConnectionLastSessionRequestTimestamp.Set(-1)
	metricCloudConnectionLastSessionRequestDuration.Set(-1)

	if established {
		metricCloudConnectionEstablishedTimestamp.SetToCurrentTime()
		metricCloudConnectionStatus.Set(1)
	} else {
		metricCloudConnectionEstablishedTimestamp.Set(-1)
		metricCloudConnectionStatus.Set(-1)
	}
}

func handleCloudRegister(c *gin.Context) {
	var req CloudRegisterRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "Invalid request body"})
		return
	}

	// Exchange the temporary token for a permanent auth token
	payload := struct {
		TempToken string `json:"tempToken"`
	}{
		TempToken: req.Token,
	}
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		c.JSON(500, gin.H{"error": "Failed to encode JSON payload: " + err.Error()})
		return
	}

	client := &http.Client{Timeout: CloudAPIRequestTimeout}

	apiReq, err := http.NewRequest(http.MethodPost, config.CloudURL+"/devices/token", bytes.NewBuffer(jsonPayload))
	if err != nil {
		c.JSON(500, gin.H{"error": "Failed to create register request: " + err.Error()})
		return
	}
	apiReq.Header.Set("Content-Type", "application/json")

	apiResp, err := client.Do(apiReq)
	if err != nil {
		c.JSON(500, gin.H{"error": "Failed to exchange token: " + err.Error()})
		return
	}
	defer apiResp.Body.Close()

	if apiResp.StatusCode != http.StatusOK {
		c.JSON(apiResp.StatusCode, gin.H{"error": "Failed to exchange token: " + apiResp.Status})
		return
	}

	var tokenResp struct {
		SecretToken string `json:"secretToken"`
	}
	if err := json.NewDecoder(apiResp.Body).Decode(&tokenResp); err != nil {
		c.JSON(500, gin.H{"error": "Failed to parse token response: " + err.Error()})
		return
	}

	if tokenResp.SecretToken == "" {
		c.JSON(500, gin.H{"error": "Received empty secret token"})
		return
	}

	config.CloudToken = tokenResp.SecretToken

	provider, err := oidc.NewProvider(c, "https://accounts.google.com")
	if err != nil {
		c.JSON(500, gin.H{"error": "Failed to initialize OIDC provider: " + err.Error()})
		return
	}

	oidcConfig := &oidc.Config{
		ClientID: req.ClientId,
	}

	verifier := provider.Verifier(oidcConfig)
	idToken, err := verifier.Verify(c, req.OidcGoogle)
	if err != nil {
		c.JSON(400, gin.H{"error": "Invalid OIDC token: " + err.Error()})
		return
	}

	config.GoogleIdentity = idToken.Audience[0] + ":" + idToken.Subject

	// Save the updated configuration
	if err := SaveConfig(); err != nil {
		c.JSON(500, gin.H{"error": "Failed to save configuration"})
		return
	}

	c.JSON(200, gin.H{"message": "Cloud registration successful"})
}

func disconnectCloud(reason error) {
	cloudDisconnectLock.Lock()
	defer cloudDisconnectLock.Unlock()

	if cloudDisconnectChan == nil {
		cloudLogger.Tracef("cloud disconnect channel is not set, no need to disconnect")
		return
	}

	// just in case the channel is closed, we don't want to panic
	defer func() {
		if r := recover(); r != nil {
			cloudLogger.Infof("cloud disconnect channel is closed, no need to disconnect: %v", r)
		}
	}()
	cloudDisconnectChan <- reason
}

func runWebsocketClient() error {
	if config.CloudToken == "" {
		time.Sleep(5 * time.Second)
		return fmt.Errorf("cloud token is not set")
	}

	wsURL, err := url.Parse(config.CloudURL)
	if err != nil {
		return fmt.Errorf("failed to parse config.CloudURL: %w", err)
	}

	if wsURL.Scheme == "http" {
		wsURL.Scheme = "ws"
	} else {
		wsURL.Scheme = "wss"
	}

	header := http.Header{}
	header.Set("X-Device-ID", GetDeviceID())
	header.Set("Authorization", "Bearer "+config.CloudToken)
	dialCtx, cancelDial := context.WithTimeout(context.Background(), CloudWebSocketConnectTimeout)

	defer cancelDial()
	c, _, err := websocket.Dial(dialCtx, wsURL.String(), &websocket.DialOptions{
		HTTPHeader: header,
	})
	if err != nil {
		return err
	}
	defer c.CloseNow() //nolint:errcheck
	cloudLogger.Infof("websocket connected to %s", wsURL)

	// set the metrics when we successfully connect to the cloud.
	cloudResetMetrics(true)

	runCtx, cancelRun := context.WithCancel(context.Background())
	defer cancelRun()
	go func() {
		for {
			time.Sleep(CloudWebSocketPingInterval)

			// set the timer for the ping duration
			timer := prometheus.NewTimer(prometheus.ObserverFunc(func(v float64) {
				metricCloudConnectionLastPingDuration.Set(v)
				metricCloudConnectionPingDuration.Observe(v)
			}))

			err := c.Ping(runCtx)

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
			cloudLogger.Infof("disconnecting from cloud due to: %v", err)
			cancelRun()
		}
	}()

	for {
		typ, msg, err := c.Read(runCtx)
		if err != nil {
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

			cloudLogger.Infof("new session request: %v", req.OidcGoogle)
			cloudLogger.Tracef("session request info: %v", req)

			metricCloudConnectionSessionRequestCount.Inc()
			metricCloudConnectionLastSessionRequestTimestamp.SetToCurrentTime()
			err = handleSessionRequest(runCtx, c, req)
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

func handleSessionRequest(ctx context.Context, c *websocket.Conn, req WebRTCSessionRequest) error {
	timer := prometheus.NewTimer(prometheus.ObserverFunc(func(v float64) {
		metricCloudConnectionLastSessionRequestDuration.Set(v)
		metricCloudConnectionSessionRequestDuration.Observe(v)
	}))
	defer timer.ObserveDuration()

	oidcCtx, cancelOIDC := context.WithTimeout(ctx, CloudOidcRequestTimeout)
	defer cancelOIDC()
	provider, err := oidc.NewProvider(oidcCtx, "https://accounts.google.com")
	if err != nil {
		_ = wsjson.Write(context.Background(), c, gin.H{
			"error": fmt.Sprintf("failed to initialize OIDC provider: %v", err),
		})
		cloudLogger.Errorf("failed to initialize OIDC provider: %v", err)
		return err
	}

	oidcConfig := &oidc.Config{
		SkipClientIDCheck: true,
	}

	verifier := provider.Verifier(oidcConfig)
	idToken, err := verifier.Verify(oidcCtx, req.OidcGoogle)
	if err != nil {
		return err
	}

	googleIdentity := idToken.Audience[0] + ":" + idToken.Subject
	if config.GoogleIdentity != googleIdentity {
		_ = wsjson.Write(context.Background(), c, gin.H{"error": "google identity mismatch"})
		return fmt.Errorf("google identity mismatch")
	}

	session, err := newSession(SessionConfig{
		ICEServers: req.ICEServers,
		LocalIP:    req.IP,
		IsCloud:    true,
		ws:         c,
	})
	if err != nil {
		_ = wsjson.Write(context.Background(), c, gin.H{"error": err})
		return err
	}

	sd, err := session.ExchangeOffer(req.Sd)
	if err != nil {
		_ = wsjson.Write(context.Background(), c, gin.H{"error": err})
		return err
	}
	if currentSession != nil {
		writeJSONRPCEvent("otherSessionConnected", nil, currentSession)
		peerConn := currentSession.peerConnection
		go func() {
			time.Sleep(1 * time.Second)
			_ = peerConn.Close()
		}()
	}

	cloudLogger.Info("new session accepted")
	cloudLogger.Tracef("new session accepted: %v", session)
	currentSession = session
	_ = wsjson.Write(context.Background(), c, gin.H{"type": "answer", "data": sd})
	return nil
}

func RunWebsocketClient() {
	for {
		// reset the metrics when we start the websocket client.
		cloudResetMetrics(false)

		// If the cloud token is not set, we don't need to run the websocket client.
		if config.CloudToken == "" {
			time.Sleep(5 * time.Second)
			continue
		}

		// If the network is not up, well, we can't connect to the cloud.
		if !networkState.Up {
			cloudLogger.Warn("waiting for network to be up, will retry in 3 seconds")
			time.Sleep(3 * time.Second)
			continue
		}

		// If the system time is not synchronized, the API request will fail anyway because the TLS handshake will fail.
		if isTimeSyncNeeded() && !timeSyncSuccess {
			cloudLogger.Warn("system time is not synced, will retry in 3 seconds")
			time.Sleep(3 * time.Second)
			continue
		}

		err := runWebsocketClient()
		if err != nil {
			cloudLogger.Errorf("websocket client error: %v", err)
			metricCloudConnectionStatus.Set(0)
			metricCloudConnectionFailureCount.Inc()
			time.Sleep(5 * time.Second)
		}
	}
}

type CloudState struct {
	Connected bool   `json:"connected"`
	URL       string `json:"url,omitempty"`
	AppURL    string `json:"appUrl,omitempty"`
}

func rpcGetCloudState() CloudState {
	return CloudState{
		Connected: config.CloudToken != "" && config.CloudURL != "",
		URL:       config.CloudURL,
		AppURL:    config.CloudAppURL,
	}
}

func rpcDeregisterDevice() error {
	if config.CloudToken == "" || config.CloudURL == "" {
		return fmt.Errorf("cloud token or URL is not set")
	}

	req, err := http.NewRequest(http.MethodDelete, config.CloudURL+"/devices/"+GetDeviceID(), nil)
	if err != nil {
		return fmt.Errorf("failed to create deregister request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+config.CloudToken)
	client := &http.Client{Timeout: CloudAPIRequestTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send deregister request: %w", err)
	}

	defer resp.Body.Close()
	// We consider both 200 OK and 404 Not Found as successful deregistration.
	// 200 OK means the device was found and deregistered.
	// 404 Not Found means the device is not in the database, which could be due to various reasons
	// (e.g., wrong cloud token, already deregistered). Regardless of the reason, we can safely remove it.
	if resp.StatusCode == http.StatusNotFound || (resp.StatusCode >= 200 && resp.StatusCode < 300) {
		config.CloudToken = ""
		config.GoogleIdentity = ""

		if err := SaveConfig(); err != nil {
			return fmt.Errorf("failed to save configuration after deregistering: %w", err)
		}

		cloudLogger.Infof("device deregistered, disconnecting from cloud")
		disconnectCloud(fmt.Errorf("device deregistered"))

		return nil
	}

	return fmt.Errorf("deregister request failed with status: %s", resp.Status)
}
