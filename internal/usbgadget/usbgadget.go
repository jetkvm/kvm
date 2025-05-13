// Package usbgadget provides a high-level interface to manage USB gadgets
// THIS PACKAGE IS FOR INTERNAL USE ONLY AND ITS API MAY CHANGE WITHOUT NOTICE
package usbgadget

import (
	"os"
	"path"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// Devices is a struct that represents the USB devices that can be enabled on a USB gadget.
type Devices struct {
	AbsoluteMouse bool `json:"absolute_mouse"`
	RelativeMouse bool `json:"relative_mouse"`
	Keyboard      bool `json:"keyboard"`
	MassStorage   bool `json:"mass_storage"`
	EthernetEcm   bool `json:"ethernet_ecm"`
	EthernetEem   bool `json:"ethernet_eem"`
	EthernetNcm   bool `json:"ethernet_ncm"`
	EthernetRndis bool `json:"ethernet_rndis"`
}

// Config is a struct that represents the customizations for a USB gadget.
// TODO: rename to something else that won't confuse with the USB gadget configuration
type Config struct {
	VendorId     string `json:"vendor_id"`
	ProductId    string `json:"product_id"`
	SerialNumber string `json:"serial_number"`
	Manufacturer string `json:"manufacturer"`
	Product      string `json:"product"`

	isEmpty bool
}

var defaultUsbGadgetDevices = Devices{
	AbsoluteMouse: true,
	RelativeMouse: true,
	Keyboard:      true,
	MassStorage:   true,
	EthernetEcm:   false,
	EthernetEem:   false,
	EthernetNcm:   false,
	EthernetRndis: false,
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

	enabledDevices Devices

	absMouseAccumulatedWheelY float64

	lastUserInput time.Time

	log *zerolog.Logger
}

const configFSPath = "/sys/kernel/config"
const gadgetPath = "/sys/kernel/config/usb_gadget"

var defaultLogger = zerolog.New(os.Stdout).Level(zerolog.InfoLevel)

// NewUsbGadget creates a new UsbGadget.
func NewUsbGadget(name string, enabledDevices *Devices, config *Config, logger *zerolog.Logger) *UsbGadget {
	if logger == nil {
		logger = &defaultLogger
	}

	if enabledDevices == nil {
		enabledDevices = &defaultUsbGadgetDevices
	}

	if config == nil {
		config = &Config{isEmpty: true}
	}

	g := &UsbGadget{
		name:           name,
		kvmGadgetPath:  path.Join(gadgetPath, name),
		configC1Path:   path.Join(gadgetPath, name, "configs/c.1"),
		configMap:      defaultGadgetConfig,
		customConfig:   *config,
		configLock:     sync.Mutex{},
		keyboardLock:   sync.Mutex{},
		absMouseLock:   sync.Mutex{},
		relMouseLock:   sync.Mutex{},
		enabledDevices: *enabledDevices,
		lastUserInput:  time.Now(),
		log:            logger,

		absMouseAccumulatedWheelY: 0,
	}
	if err := g.Init(); err != nil {
		logger.Error().Err(err).Msg("failed to init USB gadget")
		return nil
	}

	return g
}
