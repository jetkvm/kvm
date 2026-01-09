// Package usbgadget provides a high-level interface to manage USB gadgets
// THIS PACKAGE IS FOR INTERNAL USE ONLY AND ITS API MAY CHANGE WITHOUT NOTICE
package usbgadget

import (
	"context"
	"os"
	"path"
	"sync"
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

	lastUserInput time.Time

	tx     *UsbGadgetTransaction
	txLock sync.Mutex

	onKeyboardStateChange *func(state KeyboardState)
	onKeysDownChange      *func(state KeysDownState)
	onKeepAliveReset      *func()

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
		lastUserInput:        time.Now(),
		log:                  logger,

		strictMode: config.strictMode,

		logSuppressionCounter: make(map[string]int),

		absMouseAccumulatedWheelY: 0,
	}
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

	// Cancel keyboard listener first to prevent it from using stale file handles
	if u.keyboardStateCancel != nil {
		u.keyboardStateCancel()
	}

	closeFile := func(file **os.File, name string) {
		if *file != nil {
			if err := (*file).Close(); err != nil {
				u.log.Debug().Err(err).Msgf("failed to close %s HID file", name)
			}
			*file = nil
		}
	}

	closeFile(&u.keyboardHidFile, "keyboard")
	closeFile(&u.absMouseHidFile, "absolute mouse")
	closeFile(&u.relMouseHidFile, "relative mouse")
}

// PreOpenHidFiles opens all HID files to reduce input latency.
// Uses retry logic since USB re-enumeration on the host can take 1-2 seconds.
// With UVC enabled, enumeration can take 3+ seconds due to complex descriptor negotiation.
func (u *UsbGadget) PreOpenHidFiles() {
	// Initial delay for USB gadget to bind and create device files
	// 500ms gives the kernel time to create device nodes after UDC binding
	time.Sleep(500 * time.Millisecond)

	// Retry configuration: try up to 15 times with 300ms intervals (total ~5s)
	// This accommodates UVC which requires longer USB enumeration time
	const maxRetries = 15
	const retryDelay = 300 * time.Millisecond

	openHidFileWithRetry := func(file **os.File, path string, name string) {
		if *file != nil {
			return // Already open
		}

		for attempt := 1; attempt <= maxRetries; attempt++ {
			f, err := os.OpenFile(path, os.O_RDWR, 0666)
			if err == nil {
				*file = f
				u.log.Debug().
					Str("path", path).
					Int("attempt", attempt).
					Msgf("successfully opened %s HID file", name)
				return
			}

			if attempt < maxRetries {
				time.Sleep(retryDelay)
			} else {
				u.log.Warn().
					Err(err).
					Str("path", path).
					Msgf("failed to open %s HID file after %d attempts", name, maxRetries)
			}
		}
	}

	if u.enabledDevices.Keyboard {
		// Keyboard uses a different open method, so retry it separately
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
		openHidFileWithRetry(&u.absMouseHidFile, "/dev/hidg1", "absolute mouse")
	}

	if u.enabledDevices.RelativeMouse {
		openHidFileWithRetry(&u.relMouseHidFile, "/dev/hidg2", "relative mouse")
	}
}
