package usbgadget

import (
	"errors"
	"os"
	"path"
	"sync"
)

// ExternalFunction is a gadget function whose driver runs in user space
// (FunctionFS). Its driver must have written its descriptors before the
// gadget binds to the UDC, or the bind fails for every function. So the
// gadget links the function only after Prepare succeeded, and leaves it out
// of the configuration otherwise.
type ExternalFunction struct {
	// Key names the function in the gadget configuration.
	Key string
	// Instance is the FunctionFS instance: functions/ffs.<Instance>.
	Instance string
	// Order places the function among the others, which decides its
	// interface numbers.
	Order uint
	// Prepare readies the driver for the instance (mounts it and writes the
	// descriptors). The gadget calls it before every configuration; it
	// returns at once when the driver is ready.
	Prepare func() error
	// MSOSDescriptors turns on the Microsoft OS 1.0 descriptors of the
	// gadget, which a FunctionFS function fills.
	MSOSDescriptors bool
}

var (
	externalFunctionLock sync.Mutex
	externalFunction     *ExternalFunction
)

// RegisterExternalFunction registers the external function before
// NewUsbGadget.
func RegisterExternalFunction(f ExternalFunction) {
	externalFunctionLock.Lock()
	defer externalFunctionLock.Unlock()
	externalFunction = &f
}

func registeredExternalFunction() *ExternalFunction {
	externalFunctionLock.Lock()
	defer externalFunctionLock.Unlock()
	return externalFunction
}

func (f *ExternalFunction) configItem() gadgetConfigItem {
	name := "ffs." + f.Instance
	return gadgetConfigItem{
		order:      f.Order,
		device:     f.Key,
		path:       []string{"functions", name},
		configPath: []string{name},
	}
}

// Microsoft OS 1.0 descriptors: the vendor code of the OS feature requests
// and the signature in string descriptor 0xEE.
const (
	msOSVendorCode = "0xcd"
	msOSSignature  = "MSFT100"
)

// prepareExternalFunction creates the external function's instance and runs
// its Prepare. It reports whether the function can be linked. The caller
// holds configLock.
func (u *UsbGadget) prepareExternalFunction() bool {
	f := registeredExternalFunction()
	if f == nil {
		return false
	}
	if _, ok := u.configMap[f.Key]; !ok {
		return false
	}
	if _, err := os.Stat(configFSPath + "/usb_gadget"); err != nil {
		// configfs is not mounted yet; the transaction mounts it, and the
		// function joins at the next configuration.
		return false
	}
	dir := path.Join(u.kvmGadgetPath, "functions", "ffs."+f.Instance)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		u.log.Warn().Err(err).Str("function", f.Key).Msg("cannot create the external function instance")
		return false
	}
	if err := f.Prepare(); err != nil {
		u.log.Warn().Err(err).Str("function", f.Key).Msg("external function not ready; leaving it out of the gadget")
		return false
	}
	if f.MSOSDescriptors {
		if err := u.enableMSOSDescriptors(); err != nil {
			u.log.Warn().Err(err).Msg("cannot turn on the Microsoft OS descriptors")
		}
	}
	return true
}

// enableMSOSDescriptors turns on the gadget's Microsoft OS 1.0 descriptors
// for configuration c.1.
func (u *UsbGadget) enableMSOSDescriptors() error {
	osDesc := path.Join(u.kvmGadgetPath, "os_desc")
	if err := os.MkdirAll(u.configC1Path, 0o755); err != nil {
		return err
	}
	for name, value := range map[string]string{
		"b_vendor_code": msOSVendorCode,
		"qw_sign":       msOSSignature,
		"use":           "1",
	} {
		file := path.Join(osDesc, name)
		if current, err := os.ReadFile(file); err == nil && trimNewline(string(current)) == value {
			continue
		}
		if err := os.WriteFile(file, []byte(value), 0o644); err != nil {
			return err
		}
	}
	link := path.Join(osDesc, "c.1")
	if _, err := os.Lstat(link); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Symlink(u.configC1Path, link)
}

func trimNewline(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}
