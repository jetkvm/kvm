package ffs

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Linux native AIO, for bulk transfers that can be cancelled. A FunctionFS
// endpoint takes a synchronous write() until the host reads it, with no way
// to stop it from another thread; an AIO request can be cancelled, and the
// kernel then dequeues its USB request.

const (
	iocbCmdPread  = 0
	iocbCmdPwrite = 1
)

// iocb is struct iocb (64 bytes, little-endian layout of aio_key and
// aio_rw_flags).
type iocb struct {
	data      uint64
	key       uint32
	rwFlags   uint32
	opcode    uint16
	reqprio   int16
	fildes    uint32
	buf       uint64
	nbytes    uint64
	offset    int64
	reserved2 uint64
	flags     uint32
	resfd     uint32
}

// ioEvent is struct io_event.
type ioEvent struct {
	data uint64
	obj  uint64
	res  int64
	res2 int64
}

// ErrTimeout is a transfer that did not complete in time and was cancelled.
var ErrTimeout = errors.New("ffs: transfer timed out")

// aioEndpoint runs one transfer at a time on an endpoint file.
type aioEndpoint struct {
	mu  sync.Mutex
	ctx uintptr
	fd  int
	buf []byte
}

func newAIOEndpoint(fd int, size int) (*aioEndpoint, error) {
	var ctx uintptr
	if _, _, errno := unix.Syscall(unix.SYS_IO_SETUP, 1, uintptr(unsafe.Pointer(&ctx)), 0); errno != 0 {
		return nil, fmt.Errorf("io_setup: %w", errno)
	}
	return &aioEndpoint{ctx: ctx, fd: fd, buf: make([]byte, size)}, nil
}

func (e *aioEndpoint) close() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.ctx != 0 {
		_, _, _ = unix.Syscall(unix.SYS_IO_DESTROY, e.ctx, 0, 0)
		e.ctx = 0
	}
}

// getEvent waits up to timeout (forever when negative) for the completion of
// the one request in flight.
func (e *aioEndpoint) getEvent(timeout time.Duration) (ioEvent, bool, error) {
	var event ioEvent
	var ts *unix.Timespec
	if timeout >= 0 {
		t := unix.NsecToTimespec(int64(timeout))
		ts = &t
	}
	for {
		n, _, errno := unix.Syscall6(unix.SYS_IO_GETEVENTS, e.ctx, 1, 1,
			uintptr(unsafe.Pointer(&event)), uintptr(unsafe.Pointer(ts)), 0)
		if errno == unix.EINTR {
			continue
		}
		if errno != 0 {
			return event, false, fmt.Errorf("io_getevents: %w", errno)
		}
		return event, n == 1, nil
	}
}

// transfer runs one read or write of data (a write copies it first) and
// waits up to timeout; on a timeout it cancels the request and reaps it.
func (e *aioEndpoint) transfer(opcode uint16, data []byte, timeout time.Duration) (int, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.ctx == 0 {
		return 0, errors.New("ffs: endpoint closed")
	}
	if len(data) > len(e.buf) {
		return 0, fmt.Errorf("ffs: transfer of %d bytes above %d", len(data), len(e.buf))
	}
	if opcode == iocbCmdPwrite {
		copy(e.buf, data)
	}
	cb := iocb{opcode: opcode, fildes: uint32(e.fd), nbytes: uint64(len(data))}
	if len(data) > 0 {
		cb.buf = uint64(uintptr(unsafe.Pointer(&e.buf[0])))
	} else {
		cb.buf = uint64(uintptr(unsafe.Pointer(&e.buf[:1][0])))
	}
	cbs := [1]*iocb{&cb}
	if _, _, errno := unix.Syscall(unix.SYS_IO_SUBMIT, e.ctx, 1, uintptr(unsafe.Pointer(&cbs[0]))); errno != 0 {
		return 0, fmt.Errorf("io_submit: %w", errno)
	}
	event, done, err := e.getEvent(timeout)
	if err == nil && !done {
		var result ioEvent
		_, _, _ = unix.Syscall(unix.SYS_IO_CANCEL, e.ctx, uintptr(unsafe.Pointer(&cb)), uintptr(unsafe.Pointer(&result)))
		// The cancelled request completes; reap it before the buffer is
		// used again.
		event, done, err = e.getEvent(2 * time.Second)
		if err == nil && !done {
			err = errors.New("ffs: a cancelled transfer did not complete")
		}
		if err == nil {
			err = ErrTimeout
		}
	}
	runtime.KeepAlive(&cb)
	if err != nil {
		if errors.Is(err, ErrTimeout) && event.res > 0 {
			return int(event.res), err
		}
		return 0, err
	}
	if event.res < 0 {
		return 0, unix.Errno(-event.res)
	}
	if opcode == iocbCmdPread {
		copy(data, e.buf[:event.res])
	}
	return int(event.res), nil
}
