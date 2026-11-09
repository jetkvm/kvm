package ffs

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

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

// Function is an open FunctionFS instance: ep0 for its events and control
// requests, ep1 (OUT) and ep2 (IN) for the bulk streams.
type Function struct {
	ep0 *os.File
	out *os.File
	in  *os.File
	aio *aioEndpoint
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

// Open opens ep0 of the instance mounted at dir and writes the descriptors
// and strings; then the function can bind to the UDC. The bulk endpoints
// appear once ep0 has both.
func Open(dir string) (*Function, error) {
	ep0, err := os.OpenFile(filepath.Join(dir, "ep0"), os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("open ep0: %w", err)
	}
	f := &Function{ep0: ep0}
	if _, err := ep0.Write(Descriptors()); err != nil {
		f.Close()
		return nil, fmt.Errorf("write descriptors: %w", err)
	}
	if _, err := ep0.Write(Strings()); err != nil {
		f.Close()
		return nil, fmt.Errorf("write strings: %w", err)
	}
	if f.out, err = os.OpenFile(filepath.Join(dir, "ep1"), os.O_RDWR, 0); err != nil {
		f.Close()
		return nil, fmt.Errorf("open ep1: %w", err)
	}
	if f.in, err = os.OpenFile(filepath.Join(dir, "ep2"), os.O_RDWR, 0); err != nil {
		f.Close()
		return nil, fmt.Errorf("open ep2: %w", err)
	}
	if f.aio, err = newAIOEndpoint(int(f.in.Fd()), writeChunk); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

// Close closes every file of the function. With ep0 closed while the
// function is bound, the kernel unbinds the whole gadget.
func (f *Function) Close() {
	if f.aio != nil {
		f.aio.close()
	}
	for _, file := range []*os.File{f.in, f.out, f.ep0} {
		if file != nil {
			_ = file.Close()
		}
	}
}

// ReadEvents waits for ep0 events. A Setup event must be answered with Reply
// or Stall before the next call: reading events with a request pending
// stalls it.
func (f *Function) ReadEvents() ([]Event, error) {
	buf := make([]byte, 4*eventLen)
	n, err := f.ep0.Read(buf)
	if err != nil {
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

// Reply sends the data stage of the pending IN request.
func (f *Function) Reply(data []byte) error {
	_, err := f.ep0.Write(data)
	return err
}

// Stall stalls the pending request: a read of ep0 for an IN request, a write
// for an OUT request, which FunctionFS answers with EL2HLT.
func (f *Function) Stall(setup Setup) error {
	// os.File skips the system call for an empty buffer.
	fd := int(f.ep0.Fd())
	var err error
	if setup.In() {
		_, err = unix.Read(fd, nil)
	} else {
		_, err = unix.Write(fd, nil)
	}
	if errors.Is(err, unix.EL2HLT) {
		return nil
	}
	return err
}

// Read reads the OUT endpoint. It waits while the function is disabled.
func (f *Function) Read(buf []byte) (int, error) {
	return f.out.Read(buf)
}

// Write sends data on the IN endpoint in one transfer of at most 64 KiB,
// and cancels it when the host has not read it within timeout. An empty
// data is a zero-length packet.
func (f *Function) Write(data []byte, timeout time.Duration) (int, error) {
	return f.aio.transfer(iocbCmdPwrite, data, timeout)
}
