// Package usbgadget provides a high-level interface to manage USB gadgets
// THIS PACKAGE IS FOR INTERNAL USE ONLY AND ITS API MAY CHANGE WITHOUT NOTICE
package usbgadget

import (
	"context"
	"os"
	"path"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jetkvm/kvm/internal/logging"
	"github.com/rs/zerolog"
)

// Devices is a struct that represents the USB devices that can be enabled on a USB gadget.
type Devices struct {
	AbsoluteMouse bool `json:"absolute_mouse"`
	RelativeMouse bool `json:"relative_mouse"`
	Keyboard      bool `json:"keyboard"`
	MassStorage   bool `json:"mass_storage"`
	Audio         bool `json:"audio"`
	UVC           bool `json:"uvc"` // USB Video Class (webcam passthrough)
}

// Equals checks if two Devices structs are equal.
func (d Devices) Equals(other Devices) bool {
	return d.AbsoluteMouse == other.AbsoluteMouse &&
		d.RelativeMouse == other.RelativeMouse &&
		d.Keyboard == other.Keyboard &&
		d.MassStorage == other.MassStorage &&
		d.Audio == other.Audio &&
		d.UVC == other.UVC
}

// Config is a struct that represents the customizations for a USB gadget.
// TODO: rename to something else that won't confuse with the USB gadget configuration
type Config struct {
	VendorId     string `json:"vendor_id"`
	ProductId    string `json:"product_id"`
	SerialNumber string `json:"serial_number"`
	Manufacturer string `json:"manufacturer"`
	Product      string `json:"product"`

	strictMode bool // when it's enabled, all warnings will be converted to errors
	isEmpty    bool
}

var defaultUsbGadgetDevices = Devices{
	AbsoluteMouse: true,
	RelativeMouse: true,
	Keyboard:      true,
	MassStorage:   true,
	Audio:         true,
	UVC:           false, // UVC disabled by default - requires uvc-gadget userspace helper
}

type KeysDownState struct {
	Modifier byte      `json:"modifier"`
	Keys     ByteSlice `json:"keys"`
}

// UsbGadget is a struct that represents a USB gadget.
type UsbGadget struct {
	name          string
	udc           string
	kvmGadgetPath string
	configC1Path  string

	configMap    map[string]gadgetConfigItem
	customConfig Config

	configLock sync.Mutex

	keyboardHidFile *os.File
	keyboardLock    sync.Mutex
	absMouseHidFile *os.File
	absMouseLock    sync.Mutex
	relMouseHidFile *os.File
	relMouseLock    sync.Mutex

	keyboardState byte          // keyboard latched state (NumLock, CapsLock, ScrollLock, Compose, Kana)
	keysDownState KeysDownState // keyboard dynamic state (modifier keys and pressed keys)

	kbdAutoReleaseLock   sync.Mutex
	kbdAutoReleaseTimers map[byte]*time.Timer

	keyboardStateLock   sync.Mutex
	keyboardStateCtx    context.Context
	keyboardStateCancel context.CancelFunc

	enabledDevices Devices

	strictMode bool // only intended for testing for now

	absMouseAccumulatedWheelY float64

	lastUserInput atomic.Int64

	tx     *UsbGadgetTransaction
	txLock sync.Mutex

	onKeyboardStateChange *func(state KeyboardState)
	onKeysDownChange      *func(state KeysDownState)
	onKeepAliveReset      *func()

	// consecutiveWriteErrors tracks sequential HID write failures.
	// Used to detect a disconnected remote PC when UDC state stays "configured".
	consecutiveWriteErrors atomic.Int32
	lastRecoveryTime       atomic.Int64 // UnixNano; cooldown between recovery attempts

	log *zerolog.Logger

	logSuppressionCounter map[string]int
	logSuppressionLock    sync.Mutex
}

const configFSPath = "/sys/kernel/config"
const gadgetPath = "/sys/kernel/config/usb_gadget"

var defaultLogger = logging.GetSubsystemLogger("usbgadget")

// NewUsbGadget creates a new UsbGadget.
func NewUsbGadget(name string, enabledDevices *Devices, config *Config, logger *zerolog.Logger) *UsbGadget {
	return newUsbGadget(name, defaultGadgetConfig, enabledDevices, config, logger)
}

func newUsbGadget(name string, configMap map[string]gadgetConfigItem, enabledDevices *Devices, config *Config, logger *zerolog.Logger) *UsbGadget {
	if logger == nil {
		logger = defaultLogger
	}

	if enabledDevices == nil {
		enabledDevices = &defaultUsbGadgetDevices
	}

	if config == nil {
		config = &Config{isEmpty: true}
	}

	keyboardCtx, keyboardCancel := context.WithCancel(context.Background())

	g := &UsbGadget{
		name:                 name,
		kvmGadgetPath:        path.Join(gadgetPath, name),
		configC1Path:         path.Join(gadgetPath, name, "configs/c.1"),
		configMap:            configMap,
		customConfig:         *config,
		configLock:           sync.Mutex{},
		keyboardLock:         sync.Mutex{},
		absMouseLock:         sync.Mutex{},
		relMouseLock:         sync.Mutex{},
		txLock:               sync.Mutex{},
		keyboardStateCtx:     keyboardCtx,
		keyboardStateCancel:  keyboardCancel,
		keyboardState:        0,
		keysDownState:        KeysDownState{Modifier: 0, Keys: []byte{0, 0, 0, 0, 0, 0}}, // must be initialized to hidKeyBufferSize (6) zero bytes
		kbdAutoReleaseTimers: make(map[byte]*time.Timer),
		enabledDevices:       *enabledDevices,
		log:                  logger,

		strictMode: config.strictMode,

		logSuppressionCounter: make(map[string]int),

		absMouseAccumulatedWheelY: 0,
	}
	g.lastUserInput.Store(time.Now().UnixNano())

	if err := g.Init(); err != nil {
		logger.Error().Err(err).Msg("failed to init USB gadget")
		return nil
	}

	return g
}

// Close cleans up resources used by the USB gadget
func (u *UsbGadget) Close() error {
	// Cancel keyboard state context
	if u.keyboardStateCancel != nil {
		u.keyboardStateCancel()
	}

	// Stop auto-release timer
	u.kbdAutoReleaseLock.Lock()
	for _, timer := range u.kbdAutoReleaseTimers {
		if timer != nil {
			timer.Stop()
		}
	}
	u.kbdAutoReleaseTimers = make(map[byte]*time.Timer)
	u.kbdAutoReleaseLock.Unlock()

	// Close HID files
	if u.keyboardHidFile != nil {
		u.keyboardHidFile.Close()
		u.keyboardHidFile = nil
	}
	if u.absMouseHidFile != nil {
		u.absMouseHidFile.Close()
		u.absMouseHidFile = nil
	}
	if u.relMouseHidFile != nil {
		u.relMouseHidFile.Close()
		u.relMouseHidFile = nil
	}

	return nil
}

// CloseHidFiles closes all open HID files and stops the keyboard listener
func (u *UsbGadget) CloseHidFiles() {
	u.log.Debug().Msg("closing HID files")

	// Close keyboard with proper locking to prevent race conditions with openKeyboardHidFile
	u.keyboardLock.Lock()
	// Cancel keyboard listener first to prevent it from using stale file handles
	if u.keyboardStateCancel != nil {
		u.keyboardStateCancel()
	}
	if u.keyboardHidFile != nil {
		if err := u.keyboardHidFile.Close(); err != nil {
			u.log.Debug().Err(err).Msg("failed to close keyboard HID file")
		}
		u.keyboardHidFile = nil
	}
	u.keyboardLock.Unlock()

	// Close mouse files with their respective locks
	u.absMouseLock.Lock()
	if u.absMouseHidFile != nil {
		if err := u.absMouseHidFile.Close(); err != nil {
			u.log.Debug().Err(err).Msg("failed to close absolute mouse HID file")
		}
		u.absMouseHidFile = nil
	}
	u.absMouseLock.Unlock()

	u.relMouseLock.Lock()
	if u.relMouseHidFile != nil {
		if err := u.relMouseHidFile.Close(); err != nil {
			u.log.Debug().Err(err).Msg("failed to close relative mouse HID file")
		}
		u.relMouseHidFile = nil
	}
	u.relMouseLock.Unlock()
}

// PreOpenHidFiles opens all HID files to reduce input latency.
// Uses retry logic since USB re-enumeration on the host can take 1-2 seconds.
// With UVC enabled, enumeration can take 3+ seconds due to complex descriptor negotiation.
func (u *UsbGadget) PreOpenHidFiles() {
	// Initial delay for USB gadget to bind and create device files.
	// With UVC enabled, the host takes significantly longer to enumerate the device
	// and may reset the connection during enumeration. Wait longer to ensure stability.
	initialDelay := 500 * time.Millisecond
	if u.enabledDevices.UVC {
		initialDelay = 3 * time.Second
		u.log.Info().Msg("UVC enabled: waiting 3s for USB enumeration before opening HID files")
	}
	time.Sleep(initialDelay)

	// Retry configuration: try up to 15 times with 300ms intervals (total ~5s)
	// This accommodates UVC which requires longer USB enumeration time
	const maxRetries = 15
	const retryDelay = 300 * time.Millisecond

	if u.enabledDevices.Keyboard {
		// Keyboard uses openKeyboardHidFile which has its own locking
		for attempt := 1; attempt <= maxRetries; attempt++ {
			if err := u.openKeyboardHidFile(); err == nil {
				u.log.Debug().Int("attempt", attempt).Msg("successfully opened keyboard HID file")
				break
			} else if attempt < maxRetries {
				time.Sleep(retryDelay)
			} else {
				u.log.Warn().Err(err).Msgf("failed to open keyboard HID file after %d attempts", maxRetries)
			}
		}
	}

	if u.enabledDevices.AbsoluteMouse {
		for attempt := 1; attempt <= maxRetries; attempt++ {
			u.absMouseLock.Lock()
			if u.absMouseHidFile != nil {
				u.absMouseLock.Unlock()
				u.log.Debug().Int("attempt", attempt).Msg("absolute mouse HID file already open")
				break
			}
			f, err := os.OpenFile("/dev/hidg1", os.O_RDWR, 0666)
			if err == nil {
				u.absMouseHidFile = f
				u.absMouseLock.Unlock()
				u.log.Debug().Int("attempt", attempt).Msg("successfully opened absolute mouse HID file")
				break
			}
			u.absMouseLock.Unlock()
			if attempt < maxRetries {
				time.Sleep(retryDelay)
			} else {
				u.log.Warn().Err(err).Msgf("failed to open absolute mouse HID file after %d attempts", maxRetries)
			}
		}
	}

	if u.enabledDevices.RelativeMouse {
		for attempt := 1; attempt <= maxRetries; attempt++ {
			u.relMouseLock.Lock()
			if u.relMouseHidFile != nil {
				u.relMouseLock.Unlock()
				u.log.Debug().Int("attempt", attempt).Msg("relative mouse HID file already open")
				break
			}
			f, err := os.OpenFile("/dev/hidg2", os.O_RDWR, 0666)
			if err == nil {
				u.relMouseHidFile = f
				u.relMouseLock.Unlock()
				u.log.Debug().Int("attempt", attempt).Msg("successfully opened relative mouse HID file")
				break
			}
			u.relMouseLock.Unlock()
			if attempt < maxRetries {
				time.Sleep(retryDelay)
			} else {
				u.log.Warn().Err(err).Msgf("failed to open relative mouse HID file after %d attempts", maxRetries)
			}
		}
	}

	// With UVC enabled, wait an additional period after opening to ensure
	// the USB connection has stabilized. The host may still be negotiating
	// video formats and could reset the connection.
	if u.enabledDevices.UVC {
		u.log.Info().Msg("UVC enabled: waiting 2s for USB connection to stabilize")
		time.Sleep(2 * time.Second)

		u.verifyAndReopenHidFiles()
	}
}

const (
	// hidRecoveryErrorThreshold is the number of consecutive HID write errors
	// before triggering file handle recovery.
	hidRecoveryErrorThreshold int32 = 5

	// hidRecoveryCooldown is the minimum interval between recovery attempts
	// to prevent rapid close/reopen cycles when the cable is disconnected.
	hidRecoveryCooldown = 5 * time.Second
)

// GetConsecutiveWriteErrors returns the current consecutive HID write error count.
func (u *UsbGadget) GetConsecutiveWriteErrors() int32 {
	return u.consecutiveWriteErrors.Load()
}

// NeedsHidRecovery reports whether HID file handles should be recovered.
// Returns true when consecutive write errors exceed the threshold and
// sufficient time has elapsed since the last recovery attempt.
func (u *UsbGadget) NeedsHidRecovery() bool {
	if u.consecutiveWriteErrors.Load() < hidRecoveryErrorThreshold {
		return false
	}
	lastRecovery := time.Unix(0, u.lastRecoveryTime.Load())
	return time.Since(lastRecovery) >= hidRecoveryCooldown
}

// RecoverHidFiles closes stale HID file handles, resetting error counters.
// The caller must call PreOpenHidFiles() afterward to reopen them.
// This handles the case where the USB cable is unplugged from the remote PC
// but the UDC state remains "configured", leaving all HID writes broken.
func (u *UsbGadget) RecoverHidFiles() {
	u.lastRecoveryTime.Store(time.Now().UnixNano())
	u.consecutiveWriteErrors.Store(0)
	u.log.Warn().Msg("consecutive HID write errors detected, recovering file handles")
	u.CloseHidFiles()
}

// TryRecoverHidFiles atomically checks if recovery is needed and performs it.
// Returns true if recovery was performed (files closed and reopened), false if not needed or on cooldown.
// This eliminates the TOCTOU gap from separate NeedsHidRecovery + RecoverHidFiles calls.
func (u *UsbGadget) TryRecoverHidFiles() bool {
	if !u.NeedsHidRecovery() {
		return false
	}
	u.RecoverHidFiles()
	u.PreOpenHidFiles()
	return true
}

// verifyAndReopenHidFiles checks if HID file handles are still valid and reopens them if stale.
func (u *UsbGadget) verifyAndReopenHidFiles() {
	u.keyboardLock.Lock()
	if u.keyboardHidFile != nil {
		if _, err := u.keyboardHidFile.Stat(); err != nil {
			u.log.Warn().Err(err).Msg("keyboard HID file became stale, reopening")
			u.keyboardHidFile.Close()
			u.keyboardHidFile = nil
		}
	}
	u.keyboardLock.Unlock()
	// openKeyboardHidFile has its own locking
	if err := u.openKeyboardHidFile(); err != nil {
		u.log.Error().Err(err).Msg("failed to reopen keyboard HID file")
	}

	u.absMouseLock.Lock()
	if u.absMouseHidFile != nil {
		if _, err := u.absMouseHidFile.Stat(); err != nil {
			u.log.Warn().Err(err).Msg("absolute mouse HID file became stale, reopening")
			u.absMouseHidFile.Close()
			u.absMouseHidFile = nil
			if f, err := os.OpenFile("/dev/hidg1", os.O_RDWR, 0666); err == nil {
				u.absMouseHidFile = f
			} else {
				u.log.Error().Err(err).Msg("failed to reopen absolute mouse HID file")
			}
		}
	}
	u.absMouseLock.Unlock()

	u.relMouseLock.Lock()
	if u.relMouseHidFile != nil {
		if _, err := u.relMouseHidFile.Stat(); err != nil {
			u.log.Warn().Err(err).Msg("relative mouse HID file became stale, reopening")
			u.relMouseHidFile.Close()
			u.relMouseHidFile = nil
			if f, err := os.OpenFile("/dev/hidg2", os.O_RDWR, 0666); err == nil {
				u.relMouseHidFile = f
			} else {
				u.log.Error().Err(err).Msg("failed to reopen relative mouse HID file")
			}
		}
	}
	u.relMouseLock.Unlock()
}
