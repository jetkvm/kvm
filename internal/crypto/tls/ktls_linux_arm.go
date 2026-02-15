//go:build cgo && linux && arm

package tls

import (
	"syscall"
	"unsafe"
)

// WriteScatterGather writes multiple buffers as a single TLS record using scatter-gather I/O.
// This avoids copying the data into a single buffer before encryption.
//
// CRITICAL: This is the zero-copy hot path for video frame transmission.
//
// When kTLS is enabled (via SSL_OP_ENABLE_KTLS in OpenSSL 3.0+), the kernel handles
// TLS encryption. Using sendmsg() with scatter-gather iovecs allows the kernel to:
// 1. Encrypt data in-place without userspace copies
// 2. Use hardware crypto acceleration via the kernel crypto API
// 3. Reduce context switches by handling encryption in kernel space
//
// On RV1106, this leverages the hardware crypto accelerator via CONFIG_CRYPTO_DEV_ROCKCHIP.
func (c *opensslConn) WriteScatterGather(bufs ...[]byte) (int, error) {
	// Check if kTLS is enabled for sending
	if !c.IsKTLSSendEnabled() {
		// Fallback: assemble buffers and use regular SSL_write
		total := 0
		for _, b := range bufs {
			total += len(b)
		}
		if total == 0 {
			return 0, nil
		}
		combined := make([]byte, total)
		pos := 0
		for _, b := range bufs {
			copy(combined[pos:], b)
			pos += len(b)
		}
		return c.Write(combined)
	}

	// kTLS is enabled - use sendmsg() with scatter-gather
	// Build iovec array
	if len(bufs) == 0 {
		return 0, nil
	}

	iovecs := make([]syscall.Iovec, len(bufs))
	total := 0
	for i, b := range bufs {
		if len(b) > 0 {
			iovecs[i].Base = &b[0]
			iovecs[i].Len = uint32(len(b))
			total += len(b)
		}
	}

	if total == 0 {
		return 0, nil
	}

	// Use sendmsg with scatter-gather
	// When kTLS is enabled, the kernel will:
	// 1. Read data from each iovec
	// 2. Encrypt using the TLS session keys (potentially via hardware crypto)
	// 3. Send encrypted TLS records
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	var msghdr syscall.Msghdr
	msghdr.Iov = &iovecs[0]
	msghdr.Iovlen = uint32(len(iovecs))

	n, _, err := syscall.Syscall(syscall.SYS_SENDMSG, uintptr(c.fd), uintptr(unsafe.Pointer(&msghdr)), 0)
	if err != 0 {
		return 0, err
	}

	return int(n), nil
}

// WriteScatterGatherConn is the interface for connections that support scatter-gather writes.
type WriteScatterGatherConn interface {
	Conn
	// WriteScatterGather writes multiple buffers as a single TLS record.
	// Returns the total number of bytes written.
	WriteScatterGather(bufs ...[]byte) (int, error)
}
