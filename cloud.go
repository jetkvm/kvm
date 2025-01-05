package kvm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"github.com/coder/websocket/wsjson"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"

	"github.com/gin-gonic/gin"
	"github.com/coder/websocket"
)

type CloudRegisterRequest struct {
	Token      string `json:"token"`
	CloudAPI   string `json:"cloudApi"`
	OidcGoogle string `json:"oidcGoogle"`
	ClientId   string `json:"clientId"`
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

	resp, err := http.Post(req.CloudAPI+"/devices/token", "application/json", bytes.NewBuffer(jsonPayload))
	if err != nil {
		c.JSON(500, gin.H{"error": "Failed to exchange token: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		c.JSON(resp.StatusCode, gin.H{"error": "Failed to exchange token: " + resp.Status})
		return
	}

	var tokenResp struct {
		SecretToken string `json:"secretToken"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		c.JSON(500, gin.H{"error": "Failed to parse token response: " + err.Error()})
		return
	}

	if tokenResp.SecretToken == "" {
		c.JSON(500, gin.H{"error": "Received empty secret token"})
		return
	}

	if config.CloudToken == "" {
		logger.Info("Starting websocket client due to adoption")
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
	dialCtx, cancelDial := context.WithTimeout(context.Background(), time.Minute)
	defer cancelDial()
	c, _, err := websocket.Dial(dialCtx, wsURL.String(), &websocket.DialOptions{
		HTTPHeader: header,
	})
	if err != nil {
		return err
	}
	defer c.CloseNow()
	logger.Infof("WS connected to %v", wsURL.String())
	runCtx, cancelRun := context.WithCancel(context.Background())
	defer cancelRun()
	go func() {
		for {
			time.Sleep(15 * time.Second)
			err := c.Ping(runCtx)
			if err != nil {
				logger.Warnf("websocket ping error: %v", err)
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
			logger.Warnf("unable to parse ws message: %v", string(msg))
			continue
		}

		err = handleSessionRequest(runCtx, c, req)
		if err != nil {
			logger.Infof("error starting new session: %v", err)
			continue
		}
	}
}

func handleSessionRequest(ctx context.Context, c *websocket.Conn, req WebRTCSessionRequest) error {
	oidcCtx, cancelOIDC := context.WithTimeout(ctx, time.Minute)
	defer cancelOIDC()
	provider, err := oidc.NewProvider(oidcCtx, "https://accounts.google.com")
	if err != nil {
		fmt.Println("Failed to initialize OIDC provider:", err)
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
		return fmt.Errorf("google identity mismatch")
	}

	session, err := newSession()
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
	currentSession = session
	_ = wsjson.Write(context.Background(), c, gin.H{"sd": sd})
	return nil
}

func RunWebsocketClient() {
	for {
		err := runWebsocketClient()
		if err != nil {
			fmt.Println("Websocket client error:", err)
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
	client := &http.Client{Timeout: 10 * time.Second}
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
