package usbgadget

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog"
)

// UsbGadgetInterface defines the interface for USB gadget operations
// This allows for mocking in tests and separating hardware operations from business logic
type UsbGadgetInterface interface {
	// Configuration methods
	Init() error
	UpdateGadgetConfig() error
	SetGadgetConfig(config *Config)
	SetGadgetDevices(devices *Devices)
	OverrideGadgetConfig(itemKey string, itemAttr string, value string) (error, bool)

	// Hardware control methods
	RebindUsb(ignoreUnbindError bool) error
	IsUDCBound() (bool, error)
	BindUDC() error
	UnbindUDC() error

	// HID file management
	PreOpenHidFiles()
	CloseHidFiles()

	// Transaction methods
	WithTransaction(fn func() error) error
	WithTransactionTimeout(fn func() error, timeout time.Duration) error

	// Path methods
	GetConfigPath(itemKey string) (string, error)
	GetPath(itemKey string) (string, error)

	// Input methods (matching actual UsbGadget implementation)
	KeyboardReport(modifier uint8, keys []uint8) error
	AbsMouseReport(x, y int, buttons uint8) error
	AbsMouseWheelReport(wheelY int8) error
	RelMouseReport(mx, my int8, buttons uint8) error
}

// Ensure UsbGadget implements the interface
var _ UsbGadgetInterface = (*UsbGadget)(nil)

// MockUsbGadget provides a mock implementation for testing
type MockUsbGadget struct {
	name           string
	enabledDevices Devices
	customConfig   Config
	log            *zerolog.Logger

	// Mock state
	initCalled         bool
	updateConfigCalled bool
	rebindCalled       bool
	udcBound           bool
	hidFilesOpen       bool
	transactionCount   int

	// Mock behavior controls
	ShouldFailInit         bool
	ShouldFailUpdateConfig bool
	ShouldFailRebind       bool
	ShouldFailUDCBind      bool
	InitDelay              time.Duration
	UpdateConfigDelay      time.Duration
	RebindDelay            time.Duration
}

// NewMockUsbGadget creates a new mock USB gadget for testing
func NewMockUsbGadget(name string, enabledDevices *Devices, config *Config, logger *zerolog.Logger) *MockUsbGadget {
	if enabledDevices == nil {
		enabledDevices = &defaultUsbGadgetDevices
	}
	if config == nil {
		config = &Config{isEmpty: true}
	}
	if logger == nil {
		logger = defaultLogger
	}

	return &MockUsbGadget{
		name:           name,
		enabledDevices: *enabledDevices,
		customConfig:   *config,
		log:            logger,
		udcBound:       false,
		hidFilesOpen:   false,
	}
}

// Init mocks USB gadget initialization
func (m *MockUsbGadget) Init() error {
	if m.InitDelay > 0 {
		time.Sleep(m.InitDelay)
	}
	if m.ShouldFailInit {
		return m.logError("mock init failure", nil)
	}
	m.initCalled = true
	m.udcBound = true
	m.log.Info().Msg("mock USB gadget initialized")
	return nil
}

// UpdateGadgetConfig mocks gadget configuration update
func (m *MockUsbGadget) UpdateGadgetConfig() error {
	if m.UpdateConfigDelay > 0 {
		time.Sleep(m.UpdateConfigDelay)
	}
	if m.ShouldFailUpdateConfig {
		return m.logError("mock update config failure", nil)
	}
	m.updateConfigCalled = true
	m.log.Info().Msg("mock USB gadget config updated")
	return nil
}

// SetGadgetConfig mocks setting gadget configuration
func (m *MockUsbGadget) SetGadgetConfig(config *Config) {
	if config != nil {
		m.customConfig = *config
	}
}

// SetGadgetDevices mocks setting enabled devices
func (m *MockUsbGadget) SetGadgetDevices(devices *Devices) {
	if devices != nil {
		m.enabledDevices = *devices
	}
}

// OverrideGadgetConfig mocks gadget config override
func (m *MockUsbGadget) OverrideGadgetConfig(itemKey string, itemAttr string, value string) (error, bool) {
	m.log.Info().Str("itemKey", itemKey).Str("itemAttr", itemAttr).Str("value", value).Msg("mock override gadget config")
	return nil, true
}

// RebindUsb mocks USB rebinding
func (m *MockUsbGadget) RebindUsb(ignoreUnbindError bool) error {
	if m.RebindDelay > 0 {
		time.Sleep(m.RebindDelay)
	}
	if m.ShouldFailRebind {
		return m.logError("mock rebind failure", nil)
	}
	m.rebindCalled = true
	m.log.Info().Msg("mock USB gadget rebound")
	return nil
}

// IsUDCBound mocks UDC binding status check
func (m *MockUsbGadget) IsUDCBound() (bool, error) {
	return m.udcBound, nil
}

// BindUDC mocks UDC binding
func (m *MockUsbGadget) BindUDC() error {
	if m.ShouldFailUDCBind {
		return m.logError("mock UDC bind failure", nil)
	}
	m.udcBound = true
	m.log.Info().Msg("mock UDC bound")
	return nil
}

// UnbindUDC mocks UDC unbinding
func (m *MockUsbGadget) UnbindUDC() error {
	m.udcBound = false
	m.log.Info().Msg("mock UDC unbound")
	return nil
}

// PreOpenHidFiles mocks HID file pre-opening
func (m *MockUsbGadget) PreOpenHidFiles() {
	m.hidFilesOpen = true
	m.log.Info().Msg("mock HID files pre-opened")
}

// CloseHidFiles mocks HID file closing
func (m *MockUsbGadget) CloseHidFiles() {
	m.hidFilesOpen = false
	m.log.Info().Msg("mock HID files closed")
}

// WithTransaction mocks transaction execution
func (m *MockUsbGadget) WithTransaction(fn func() error) error {
	return m.WithTransactionTimeout(fn, 60*time.Second)
}

// WithTransactionTimeout mocks transaction execution with timeout
func (m *MockUsbGadget) WithTransactionTimeout(fn func() error, timeout time.Duration) error {
	m.transactionCount++
	m.log.Info().Int("transactionCount", m.transactionCount).Msg("mock transaction started")

	// Execute the function in a mock transaction context
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- fn()
	}()

	select {
	case err := <-done:
		if err != nil {
			m.log.Error().Err(err).Msg("mock transaction failed")
		} else {
			m.log.Info().Msg("mock transaction completed")
		}
		return err
	case <-ctx.Done():
		m.log.Error().Dur("timeout", timeout).Msg("mock transaction timed out")
		return ctx.Err()
	}
}

// GetConfigPath mocks getting configuration path
func (m *MockUsbGadget) GetConfigPath(itemKey string) (string, error) {
	return "/mock/config/path/" + itemKey, nil
}

// GetPath mocks getting path
func (m *MockUsbGadget) GetPath(itemKey string) (string, error) {
	return "/mock/path/" + itemKey, nil
}

// KeyboardReport mocks keyboard input
func (m *MockUsbGadget) KeyboardReport(modifier uint8, keys []uint8) error {
	m.log.Debug().Uint8("modifier", modifier).Int("keyCount", len(keys)).Msg("mock keyboard input sent")
	return nil
}

// AbsMouseReport mocks absolute mouse input
func (m *MockUsbGadget) AbsMouseReport(x, y int, buttons uint8) error {
	m.log.Debug().Int("x", x).Int("y", y).Uint8("buttons", buttons).Msg("mock absolute mouse input sent")
	return nil
}

// AbsMouseWheelReport mocks absolute mouse wheel input
func (m *MockUsbGadget) AbsMouseWheelReport(wheelY int8) error {
	m.log.Debug().Int8("wheelY", wheelY).Msg("mock absolute mouse wheel input sent")
	return nil
}

// RelMouseReport mocks relative mouse input
func (m *MockUsbGadget) RelMouseReport(mx, my int8, buttons uint8) error {
	m.log.Debug().Int8("mx", mx).Int8("my", my).Uint8("buttons", buttons).Msg("mock relative mouse input sent")
	return nil
}

// Helper methods for mock
func (m *MockUsbGadget) logError(msg string, err error) error {
	if err == nil {
		err = fmt.Errorf("%s", msg)
	}
	m.log.Error().Err(err).Msg(msg)
	return err
}

// Mock state inspection methods for testing
func (m *MockUsbGadget) IsInitCalled() bool {
	return m.initCalled
}

func (m *MockUsbGadget) IsUpdateConfigCalled() bool {
	return m.updateConfigCalled
}

func (m *MockUsbGadget) IsRebindCalled() bool {
	return m.rebindCalled
}

func (m *MockUsbGadget) IsHidFilesOpen() bool {
	return m.hidFilesOpen
}

func (m *MockUsbGadget) GetTransactionCount() int {
	return m.transactionCount
}

func (m *MockUsbGadget) GetEnabledDevices() Devices {
	return m.enabledDevices
}

func (m *MockUsbGadget) GetCustomConfig() Config {
	return m.customConfig
}
