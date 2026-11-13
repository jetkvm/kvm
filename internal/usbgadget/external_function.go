package usbgadget

import (
	"errors"
	"os"
	"path"
	"sync"
	"syscall"
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
	// returns at once when the driver is ready. With spareIN the
	// descriptors declare one IN endpoint that nothing uses before the
	// function's own IN endpoint (externalNeedsSpareIN). spareIN changes
	// only after a Release.
	Prepare func(spareIN bool) error
	// Release closes the driver's files of the instance, so that the next
	// Prepare can write other descriptors. The gadget calls it with the
	// gadget detached from the UDC. After an error the driver still holds
	// the instance open, and the gadget calls Release again before the next
	// Prepare.
	Release func() error
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

// unreliableINEndpoint reports whether IN endpoint n of the RV1106 USB
// controller loses data: after the host enumerates the device again,
// endpoint 6 sends nothing and endpoint 8 sends stale data, until the
// controller is bound again (#1634).
func unreliableINEndpoint(n int) bool { return n == 6 || n == 8 }

// externalNeedsSpareIN reports whether the external function's descriptors
// need a spare IN endpoint before the function's own, which is the only IN
// endpoint it uses. Without the spare, that endpoint has the number after
// the IN endpoints of the functions before it; the spare takes that number
// and moves the function's endpoint to the next one. The numbers after 6
// and 8 are reliable.
func (u *UsbGadget) externalNeedsSpareIN(f *ExternalFunction) bool {
	return unreliableINEndpoint(u.inEndpointsBefore(f.Order) + 1)
}

// releaseExternalFunction detaches the gadget from the UDC and has the
// driver close the instance. With the gadget detached the kernel has
// unbound the function: I/O that waited on its endpoints has ended, and the
// last close of its files does not make the kernel unregister a bound
// gadget. The caller holds configLock and has excluded HID access
// (withHIDRebind); the configuration that follows attaches the gadget again.
func (u *UsbGadget) releaseExternalFunction(f *ExternalFunction) error {
	if f.Release == nil {
		return errors.New("the external function has no Release")
	}
	// As for a rebind: no HID descriptor stays open over the unbind, and the
	// host session ends before the functions shut down.
	u.ResetHIDFiles()
	_ = softDisconnect(u.udc)
	err := os.WriteFile(path.Join(u.kvmGadgetPath, "UDC"), []byte("\n"), 0o644)
	// ENODEV: the gadget is not attached.
	if err != nil && !errors.Is(err, syscall.ENODEV) {
		u.log.Warn().Err(err).Msg("cannot detach the gadget from the UDC")
	}
	return f.Release()
}

// prepareExternalFunction creates the external function's instance and runs
// its Prepare, after a Release when the descriptors must change. It reports
// whether the function can be linked. The caller holds configLock and has
// excluded HID access (withHIDRebind).
func (u *UsbGadget) prepareExternalFunction() bool {
	f := registeredExternalFunction()
	if f == nil {
		return false
	}
	if _, ok := u.configMap[f.Key]; !ok {
		return false
	}
	spareIN := u.externalNeedsSpareIN(f)
	if u.externalPrepared && (u.externalMustRelease || u.externalSpareIN != spareIN) {
		if err := u.releaseExternalFunction(f); err != nil {
			u.externalMustRelease = true
			u.log.Warn().Err(err).Str("function", f.Key).Msg("cannot release the external function; leaving it out of the gadget")
			return false
		}
		u.externalPrepared = false
		u.externalMustRelease = false
		u.log.Info().Str("function", f.Key).Bool("spare_in", spareIN).Msg("external function released for new descriptors")
	}
	// At boot configfs is not mounted yet; the transaction would mount it,
	// but the instance must exist before the transaction links it.
	if _, err := os.Stat(gadgetPath); err != nil {
		if err := mountConfigFS(configFSPath); err != nil {
			u.log.Warn().Err(err).Str("function", f.Key).Msg("cannot mount configfs for the external function")
			return false
		}
	}
	dir := path.Join(u.kvmGadgetPath, "functions", "ffs."+f.Instance)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		u.log.Warn().Err(err).Str("function", f.Key).Msg("cannot create the external function instance")
		return false
	}
	if err := f.Prepare(spareIN); err != nil {
		u.log.Warn().Err(err).Str("function", f.Key).Msg("external function not ready; leaving it out of the gadget")
		return false
	}
	u.externalPrepared = true
	u.externalSpareIN = spareIN
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
