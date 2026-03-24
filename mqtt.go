package kvm

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gwatts/rootcerts"
	"github.com/jetkvm/kvm/internal/logging"
	"github.com/jetkvm/kvm/internal/sync"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

var mqttLogger = logging.GetSubsystemLogger("mqtt")

// publishTimeout is the maximum time to wait for a single MQTT publish to complete.
const publishTimeout = 5 * time.Second

type MQTTConfig struct {
	Enabled           bool   `json:"enabled"`
	Broker            string `json:"broker"`
	Port              int    `json:"port"`
	Username          string `json:"username"`
	Password          string `json:"password"`
	BaseTopic         string `json:"base_topic"`
	UseTLS            bool   `json:"use_tls"`
	TLSInsecure       bool   `json:"tls_insecure"`
	EnableHADiscovery bool   `json:"enable_ha_discovery"`
	EnableActions     bool   `json:"enable_actions"`
	DebounceMs        int    `json:"debounce_ms"`
}

var mqttManager *MQTTManager

type MQTTManager struct {
	client    mqtt.Client
	deviceID  string
	baseTopic string
	connected atomic.Bool
	lastError atomic.Value // stores string; cleared on successful connect

	updateRequested atomic.Bool // set when an update is triggered via MQTT, cleared when OTA finishes

	debounceMs int
	done       chan struct{} // closed on Close() to stop background goroutines

	// Debounce state for ATX HDD LED OFF transitions.
	// When the HDD LED turns off, publishing is delayed by debounceMs.
	// If it turns back on within that window, the OFF is suppressed,
	// keeping the published state as ON during rapid flickering.
	atxDebounceMu    sync.Mutex
	atxDebounceTimer *time.Timer
	atxLastPublished *ATXState

	// Cached virtual media options to avoid redundant discovery republishes.
	lastVMOptions []string

	// Cached update state to avoid calling getUpdateStatus on every tick.
	lastUpdateCheck   time.Time
	lastUpdatePayload *mqttUpdateState
}

type mqttStatusPayload struct {
	Online bool `json:"online"`
}

// topic returns a fully qualified topic string using baseTopic.
func (m *MQTTManager) topic(parts ...string) string {
	return m.baseTopic + "/" + strings.Join(parts, "/")
}

// validateBaseTopic checks that the base topic does not contain MQTT wildcards or invalid characters.
func validateBaseTopic(topic string) error {
	if strings.ContainsAny(topic, "+#") {
		return fmt.Errorf("base topic must not contain MQTT wildcards (+ or #)")
	}
	if strings.Contains(topic, " ") {
		return fmt.Errorf("base topic must not contain spaces")
	}
	if topic == "" {
		return fmt.Errorf("base topic must not be empty")
	}
	return nil
}

func NewMQTTManager(cfg *MQTTConfig, deviceID string) (*MQTTManager, error) {
	if cfg == nil || !cfg.Enabled {
		return nil, fmt.Errorf("MQTT is not enabled")
	}

	baseTopic := cfg.BaseTopic
	if baseTopic == "" {
		baseTopic = "jetkvm"
	}
	if err := validateBaseTopic(baseTopic); err != nil {
		return nil, err
	}
	// Ensure baseTopic includes deviceID
	if !strings.Contains(baseTopic, deviceID) {
		baseTopic = baseTopic + "/" + deviceID
	}

	m := &MQTTManager{
		deviceID:   deviceID,
		baseTopic:  baseTopic,
		debounceMs: cfg.DebounceMs,
		done:       make(chan struct{}),
	}

	scheme := "tcp"
	port := cfg.Port
	if port == 0 {
		port = 1883
	}
	if cfg.UseTLS {
		scheme = "ssl"
	}
	brokerURL := fmt.Sprintf("%s://%s:%d", scheme, cfg.Broker, port)

	opts := mqtt.NewClientOptions()
	opts.AddBroker(brokerURL)
	opts.SetClientID(fmt.Sprintf("jetkvm-%s", deviceID))
	opts.SetUsername(cfg.Username)
	opts.SetPassword(cfg.Password)
	opts.SetAutoReconnect(true)
	opts.SetConnectRetry(true)
	opts.SetCleanSession(false)
	opts.SetConnectRetryInterval(10 * time.Second)
	opts.SetConnectTimeout(10 * time.Second)

	if cfg.UseTLS {
		tlsConfig := &tls.Config{
			InsecureSkipVerify: cfg.TLSInsecure, //nolint:gosec
		}
		if !cfg.TLSInsecure {
			tlsConfig.RootCAs = rootcerts.ServerCertPool()
		}
		opts.SetTLSConfig(tlsConfig)
	}

	// Will message: offline status
	willPayload, _ := json.Marshal(mqttStatusPayload{Online: false})
	opts.SetWill(m.topic("status"), string(willPayload), 1, true)

	opts.OnConnect = m.onConnect
	opts.OnConnectionLost = m.onConnectionLost

	m.client = mqtt.NewClient(opts)

	// Connect in the background — with ConnectRetry(true) this will keep
	// retrying automatically without blocking the caller (startup).
	token := m.client.Connect()
	go func() {
		token.Wait()
		if token.Error() != nil {
			m.setLastError(token.Error())
			mqttLogger.Warn().Err(token.Error()).Str("broker", brokerURL).Msg("initial MQTT connection attempt failed, will retry")
		}
	}()

	return m, nil
}

func (m *MQTTManager) setLastError(err error) {
	if err != nil {
		m.lastError.Store(err.Error())
	}
}

func (m *MQTTManager) clearLastError() {
	m.lastError.Store("")
}

// LastError returns the last connection error, or empty string if none.
func (m *MQTTManager) LastError() string {
	v := m.lastError.Load()
	if v == nil {
		return ""
	}
	return v.(string)
}

func (m *MQTTManager) onConnect(client mqtt.Client) {
	mqttLogger.Info().Str("deviceID", m.deviceID).Msg("connected to MQTT broker")
	m.connected.Store(true)
	m.clearLastError()

	// Publish online status
	m.publish(m.topic("status"), mqttStatusPayload{Online: true}, true)

	// Publish Home Assistant discovery configs if enabled
	if config.MqttConfig != nil && config.MqttConfig.EnableHADiscovery {
		m.publishHADiscovery()
	}

	// Subscribe to command topics
	m.subscribeCommands()

	// Immediately publish all current states so Home Assistant knows
	// the current state of all switches and sensors right away.
	if config.ActiveExtension == "atx-power" {
		m.publishATXState(ATXState{
			Power: ledPWRState.Load(),
			HDD:   ledHDDState.Load(),
		})
	}
	if config.ActiveExtension == "dc-power" {
		m.publishDCState(getDCState())
	}
	m.publishExtendedStates()
}

func (m *MQTTManager) onConnectionLost(client mqtt.Client, err error) {
	mqttLogger.Warn().Err(err).Msg("MQTT connection lost")
	m.connected.Store(false)
	m.setLastError(err)
}

// IsConnected returns the current connection state.
func (m *MQTTManager) IsConnected() bool {
	return m.connected.Load() && m.client.IsConnected()
}

// Close disconnects from the MQTT broker gracefully and stops background goroutines.
func (m *MQTTManager) Close() {
	// Signal all background goroutines to stop.
	close(m.done)

	m.atxDebounceMu.Lock()
	m.cancelATXDebounceTimerLocked()
	m.atxDebounceMu.Unlock()

	if m.client != nil && m.client.IsConnected() {
		m.publish(m.topic("status"), mqttStatusPayload{Online: false}, true)
		m.client.Disconnect(500)
	}
	m.connected.Store(false)
}

// publish marshals the payload to JSON and publishes to the topic.
func (m *MQTTManager) publish(topic string, payload interface{}, retained bool) {
	data, err := json.Marshal(payload)
	if err != nil {
		mqttLogger.Error().Err(err).Str("topic", topic).Msg("failed to marshal MQTT payload")
		return
	}
	token := m.client.Publish(topic, 1, retained, data)
	if !token.WaitTimeout(publishTimeout) {
		mqttLogger.Warn().Str("topic", topic).Msg("MQTT publish timed out")
		return
	}
	if token.Error() != nil {
		mqttLogger.Error().Err(token.Error()).Str("topic", topic).Msg("failed to publish MQTT message")
	}
}

// publishString publishes a raw string payload.
func (m *MQTTManager) publishString(topic string, payload string, retained bool) {
	token := m.client.Publish(topic, 1, retained, payload)
	if !token.WaitTimeout(publishTimeout) {
		mqttLogger.Warn().Str("topic", topic).Msg("MQTT publish timed out")
		return
	}
	if token.Error() != nil {
		mqttLogger.Error().Err(token.Error()).Str("topic", topic).Msg("failed to publish MQTT message")
	}
}

// actionsAllowed checks if MQTT actions are enabled in the config.
func (m *MQTTManager) actionsAllowed() bool {
	return config.MqttConfig != nil && config.MqttConfig.EnableActions
}

// --- JSON-RPC Handlers ---

type MQTTStatusResponse struct {
	Connected bool   `json:"connected"`
	Error     string `json:"error,omitempty"`
}

const mqttPasswordMask = "********"

func rpcGetMqttSettings() (*MQTTConfig, error) {
	cfg := config.MqttConfig
	if cfg == nil {
		return &MQTTConfig{}, nil
	}
	// Return a copy with the password masked to avoid leaking credentials.
	masked := *cfg
	if masked.Password != "" {
		masked.Password = mqttPasswordMask
	}
	return &masked, nil
}

func rpcSetMqttSettings(settings MQTTConfig) error {
	if settings.Enabled && settings.Broker == "" {
		return fmt.Errorf("broker address is required when MQTT is enabled")
	}
	if settings.Port < 1 || settings.Port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535")
	}
	if settings.BaseTopic == "" {
		settings.BaseTopic = "jetkvm"
	}
	if err := validateBaseTopic(settings.BaseTopic); err != nil {
		return err
	}

	oldConfig := config.MqttConfig

	// If the password is the mask placeholder, preserve the existing password.
	if settings.Password == mqttPasswordMask && oldConfig != nil {
		settings.Password = oldConfig.Password
	}

	// Cleanup before applying new settings
	if mqttManager != nil && mqttManager.IsConnected() {
		wasEnabled := oldConfig != nil && oldConfig.Enabled
		wasHADiscovery := oldConfig != nil && oldConfig.EnableHADiscovery

		// If MQTT is being disabled, clean up all topics and discovery entries
		if wasEnabled && !settings.Enabled {
			mqttManager.cleanupAllTopics()
		} else if wasHADiscovery && !settings.EnableHADiscovery {
			// If only HA Discovery is being disabled, remove discovery entries
			mqttManager.removeAllDiscovery()
		}
	}

	if settings.DebounceMs < 0 {
		settings.DebounceMs = 0
	}

	cfg := settings
	config.MqttConfig = &cfg

	if err := SaveConfig(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	// Reconnect MQTT
	restartMQTT()

	return nil
}

func rpcGetMqttStatus() (MQTTStatusResponse, error) {
	connected := false
	lastError := ""
	if mqttManager != nil {
		connected = mqttManager.IsConnected()
		lastError = mqttManager.LastError()
	}
	return MQTTStatusResponse{Connected: connected, Error: lastError}, nil
}

// testMqttConnectionTimeout is the maximum time to wait for a test connection attempt.
const testMqttConnectionTimeout = 5 * time.Second

type MQTTTestResult struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

func rpcTestMqttConnection(settings MQTTConfig) (MQTTTestResult, error) {
	if settings.Broker == "" {
		return MQTTTestResult{Error: "broker address is required"}, nil
	}

	// If the password is the mask placeholder, use the existing password.
	if settings.Password == mqttPasswordMask && config.MqttConfig != nil {
		settings.Password = config.MqttConfig.Password
	}

	scheme := "tcp"
	port := settings.Port
	if port == 0 {
		port = 1883
	}
	if settings.UseTLS {
		scheme = "ssl"
	}
	brokerURL := fmt.Sprintf("%s://%s:%d", scheme, settings.Broker, port)

	opts := mqtt.NewClientOptions()
	opts.AddBroker(brokerURL)
	opts.SetClientID(fmt.Sprintf("jetkvm-%s-test", GetDeviceID()))
	opts.SetUsername(settings.Username)
	opts.SetPassword(settings.Password)
	opts.SetAutoReconnect(false)
	opts.SetConnectRetry(false)
	opts.SetConnectTimeout(testMqttConnectionTimeout)

	if settings.UseTLS {
		tlsConfig := &tls.Config{
			InsecureSkipVerify: settings.TLSInsecure, //nolint:gosec
		}
		if !settings.TLSInsecure {
			tlsConfig.RootCAs = rootcerts.ServerCertPool()
		}
		opts.SetTLSConfig(tlsConfig)
	}

	client := mqtt.NewClient(opts)
	token := client.Connect()
	token.Wait()

	if err := token.Error(); err != nil {
		return MQTTTestResult{Error: err.Error()}, nil
	}

	client.Disconnect(250)
	return MQTTTestResult{Success: true}, nil
}

// restartMQTT stops the existing MQTT connection and starts a new one if enabled.
func restartMQTT() {
	if mqttManager != nil {
		mqttManager.Close()
		mqttManager = nil
	}
	startMQTT()
}

func startMQTT() {
	if config.MqttConfig == nil || !config.MqttConfig.Enabled {
		mqttLogger.Info().Msg("MQTT is disabled")
		return
	}

	var err error
	mqttManager, err = NewMQTTManager(config.MqttConfig, GetDeviceID())
	if err != nil {
		mqttLogger.Warn().Err(err).Msg("failed to start MQTT")
		return
	}

	mqttManager.startPeriodicStatusUpdates(15 * time.Second)
	mqttLogger.Info().Msg("MQTT started")
}

// initMQTT initializes MQTT if enabled in config. Called from main.go.
func initMQTT() {
	startMQTT()
}
