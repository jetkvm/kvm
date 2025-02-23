package kvm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/coder/websocket/wsjson"

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

	if config.CloudToken == "" {
		cloudLogger.Info("Starting websocket client due to adoption")
		go RunWebsocketClient()
	}

	config.CloudToken = tokenResp.SecretToken
	config.CloudURL = req.CloudAPI

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
	cloudLogger.Infof("websocket connected to %s", wsURL.String())
	runCtx, cancelRun := context.WithCancel(context.Background())
	defer cancelRun()
	go func() {
		for {
			time.Sleep(CloudWebSocketPingInterval)
			err := c.Ping(runCtx)
			if err != nil {
				cloudLogger.Warnf("websocket ping error: %v", err)
				cancelRun()
				return
			}
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
		var req WebRTCSessionRequest
		err = json.Unmarshal(msg, &req)
		if err != nil {
			cloudLogger.Warnf("unable to parse ws message: %v", string(msg))
			continue
		}

		cloudLogger.Infof("new session request: %v", req.OidcGoogle)
		cloudLogger.Tracef("session request info: %v", req)

		err = handleSessionRequest(runCtx, c, req)
		if err != nil {
			cloudLogger.Infof("error starting new session: %v", err)
			continue
		}
	}
}

func handleSessionRequest(ctx context.Context, c *websocket.Conn, req WebRTCSessionRequest) error {
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
	_ = wsjson.Write(context.Background(), c, gin.H{"sd": sd})
	return nil
}

func RunWebsocketClient() {
	for {
		err := runWebsocketClient()
		if err != nil {
			cloudLogger.Errorf("websocket client error: %v", err)
			time.Sleep(5 * time.Second)
		}
	}
}

type CloudState struct {
	Connected bool   `json:"connected"`
	URL       string `json:"url,omitempty"`
}

func rpcGetCloudState() CloudState {
	return CloudState{
		Connected: config.CloudToken != "" && config.CloudURL != "",
		URL:       config.CloudURL,
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
		config.CloudURL = ""
		config.GoogleIdentity = ""
		if err := SaveConfig(); err != nil {
			return fmt.Errorf("failed to save configuration after deregistering: %w", err)
		}

		return nil
	}

	return fmt.Errorf("deregister request failed with status: %s", resp.Status)
}
