package ffs

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Setup is a control request, struct usb_ctrlrequest.
type Setup struct {
	RequestType uint8
	Request     uint8
	Value       uint16
	Index       uint16
	Length      uint16
}

// In reports whether the request's data stage goes to the host.
func (s Setup) In() bool { return s.RequestType&0x80 != 0 }

// Event is struct usb_functionfs_event.
type Event struct {
	Type  uint8
	Setup Setup
}

const eventLen = 12

// writeChunk is the largest bulk IN transfer.
const writeChunk = 64 * 1024

// ioctlEndpointDesc is FUNCTIONFS_ENDPOINT_DESC: _IOR('g', 130, struct
// usb_endpoint_descriptor), a struct of 9 bytes.
const (
	ioctlEndpointDesc  = 0x80096782
	endpointDescMaxLen = 9
)

// ErrStopped is the error of ReadEvents after Interrupt.
var ErrStopped = errors.New("ffs: stopped")

// Function is an open FunctionFS instance: ep0 for its events and control
// requests, and the endpoint files of the bulk OUT and bulk IN endpoints.
//
// The files are plain descriptors. ep0 blocks: ReadEvents waits in poll for
// ep0 and for the stop descriptor, so Interrupt ends the wait. The endpoint
// files do not block: while the function is disabled, their I/O fails with
// EAGAIN and does not wait in the kernel. A read of an enabled OUT endpoint
// still waits for data, until the host sends some or the function is
// disabled.
type Function struct {
	ep0  int
	out  int
	in   int
	stop int
	aio  *aioEndpoint
}

// Mounted reports whether a functionfs instance named name is mounted at
// dir.
func Mounted(dir, name string) bool {
	mounts, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return false
	}
	for _, line := range splitLines(string(mounts)) {
		var source, target, fstype string
		if n, _ := fmt.Sscanf(line, "%s %s %s", &source, &target, &fstype); n == 3 &&
			target == dir && fstype == "functionfs" && source == name {
			return true
		}
	}
	return false
}

func splitLines(text string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			lines = append(lines, text[start:i])
			start = i + 1
		}
	}
	if start < len(text) {
		lines = append(lines, text[start:])
	}
	return lines
}

// Mount mounts the functionfs instance name at dir, unless it is mounted
// there already. The instance must exist in configfs (functions/ffs.name).
func Mount(dir, name string) error {
	if Mounted(dir, name) {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := unix.Mount(name, dir, "functionfs", 0, ""); err != nil {
		return fmt.Errorf("mount functionfs %s at %s: %w", name, dir, err)
	}
	return nil
}

// retryEINTR repeats a system call that a signal interrupted.
func retryEINTR(call func() (int, error)) (int, error) {
	for {
		n, err := call()
		if !errors.Is(err, unix.EINTR) {
			return n, err
		}
	}
}

func openFile(path string, flags int) (int, error) {
	return retryEINTR(func() (int, error) { return unix.Open(path, flags|unix.O_CLOEXEC, 0) })
}

// Open opens ep0 of the instance mounted at dir and writes the descriptors
// and strings; then the function can bind to the UDC. The endpoint files
// appear once ep0 has both. spareIN selects the descriptors with the unused
// interrupt IN endpoint (Descriptors); its file stays closed.
func Open(dir string, spareIN bool) (*Function, error) {
	f := &Function{ep0: -1, out: -1, in: -1, stop: -1}
	var err error
	if f.stop, err = unix.Eventfd(0, unix.EFD_CLOEXEC|unix.EFD_NONBLOCK); err != nil {
		return nil, fmt.Errorf("eventfd: %w", err)
	}
	if f.ep0, err = openFile(filepath.Join(dir, "ep0"), unix.O_RDWR); err != nil {
		f.Close()
		return nil, fmt.Errorf("open ep0: %w", err)
	}
	if _, err := retryEINTR(func() (int, error) { return unix.Write(f.ep0, Descriptors(spareIN)) }); err != nil {
		f.Close()
		return nil, fmt.Errorf("write descriptors: %w", err)
	}
	if _, err := retryEINTR(func() (int, error) { return unix.Write(f.ep0, Strings()) }); err != nil {
		f.Close()
		return nil, fmt.Errorf("write strings: %w", err)
	}
	// The files have the endpoint numbers of the descriptors.
	in := "ep2"
	if spareIN {
		in = "ep3"
	}
	if f.out, err = openFile(filepath.Join(dir, "ep1"), unix.O_RDWR|unix.O_NONBLOCK); err != nil {
		f.Close()
		return nil, fmt.Errorf("open ep1: %w", err)
	}
	if f.in, err = openFile(filepath.Join(dir, in), unix.O_RDWR|unix.O_NONBLOCK); err != nil {
		f.Close()
		return nil, fmt.Errorf("open %s: %w", in, err)
	}
	if f.aio, err = newAIOEndpoint(f.in, writeChunk); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

// Interrupt makes ReadEvents return ErrStopped, now and on every later call.
func (f *Function) Interrupt() {
	one := [8]byte{1}
	_, _ = retryEINTR(func() (int, error) { return unix.Write(f.stop, one[:]) })
}

// Close closes every file of the function; no call may be in progress.
// With the last file closed, the instance takes new descriptors. With ep0
// closed while the function is bound, the kernel unbinds the whole gadget.
func (f *Function) Close() {
	if f.aio != nil {
		f.aio.close()
		f.aio = nil
	}
	for _, fd := range []*int{&f.in, &f.out, &f.ep0, &f.stop} {
		if *fd >= 0 {
			_ = unix.Close(*fd)
			*fd = -1
		}
	}
}

// ReadEvents waits for ep0 events. A Setup event must be answered with Reply
// or Stall before the next call: reading events with a request pending
// stalls it.
func (f *Function) ReadEvents() ([]Event, error) {
	buf := make([]byte, 4*eventLen)
	for {
		fds := []unix.PollFd{
			{Fd: int32(f.ep0), Events: unix.POLLIN},
			{Fd: int32(f.stop), Events: unix.POLLIN},
		}
		if _, err := unix.Poll(fds, -1); err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			return nil, fmt.Errorf("poll ep0: %w", err)
		}
		if fds[1].Revents != 0 {
			return nil, ErrStopped
		}
		if fds[0].Revents&unix.POLLIN == 0 {
			if fds[0].Revents != 0 {
				return nil, fmt.Errorf("poll ep0: events %#x", fds[0].Revents)
			}
			continue
		}
		n, err := unix.Read(f.ep0, buf)
		switch {
		case err == nil:
		case errors.Is(err, unix.EINTR):
			continue
		case errors.Is(err, unix.EIDRM), errors.Is(err, unix.EL2HLT):
			// A new event cancelled the pending control request (EIDRM), or
			// this read stalled a request that had no answer (EL2HLT). The
			// events are still there for the next read.
			continue
		default:
			return nil, err
		}
		events := make([]Event, 0, n/eventLen)
		for o := 0; o+eventLen <= n; o += eventLen {
			e := buf[o : o+eventLen]
			events = append(events, Event{
				Type: e[8],
				Setup: Setup{
					RequestType: e[0],
					Request:     e[1],
					Value:       binary.LittleEndian.Uint16(e[2:]),
					Index:       binary.LittleEndian.Uint16(e[4:]),
					Length:      binary.LittleEndian.Uint16(e[6:]),
				},
			})
		}
		return events, nil
	}
}

// Reply sends the data stage of the pending IN request.
func (f *Function) Reply(data []byte) error {
	_, err := unix.Write(f.ep0, data)
	return err
}

// Stall stalls the pending request: a read of ep0 for an IN request, a write
// for an OUT request, which FunctionFS answers with EL2HLT.
func (f *Function) Stall(setup Setup) error {
	var err error
	if setup.In() {
		_, err = unix.Read(f.ep0, nil)
	} else {
		_, err = unix.Write(f.ep0, nil)
	}
	if errors.Is(err, unix.EL2HLT) {
		return nil
	}
	return err
}

// Read reads the OUT endpoint. While the function is disabled, the error is
// EAGAIN.
func (f *Function) Read(buf []byte) (int, error) {
	n, err := retryEINTR(func() (int, error) { return unix.Read(f.out, buf) })
	if err != nil {
		return 0, err
	}
	return n, nil
}

// Write sends data on the IN endpoint in one transfer of at most 64 KiB,
// and cancels it when the host has not read it within timeout. An empty
// data is a zero-length packet. While the function is disabled, the error
// is EAGAIN.
func (f *Function) Write(data []byte, timeout time.Duration) (int, error) {
	return f.aio.transfer(iocbCmdPwrite, data, timeout)
}

// INEndpoint is the address of the bulk IN endpoint on the bus, for
// example 0x87. The function must be enabled.
func (f *Function) INEndpoint() (int, error) {
	var desc [endpointDescMaxLen]byte
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(f.in), ioctlEndpointDesc, uintptr(unsafe.Pointer(&desc[0])))
	if errno != 0 {
		return 0, errno
	}
	return int(desc[2]), nil
}
