package lldp

import (
	"fmt"
	"net"
	"os"
	"syscall"
	"unsafe"

	"github.com/google/gopacket/afpacket"
	"golang.org/x/sys/unix"
)

const (
	afPacketBufferSize = 2 // in MiB
	afPacketSnaplen    = 9216
)

// afpacketComputeSize computes the block_size and the num_blocks in such a way that the
// allocated mmap buffer is close to but smaller than targetSizeMb.
// The restriction is that the blockSize must be divisible by both the
// frameSize and pageSize.
//
// See also: https://github.com/google/gopacket/blob/master/examples/afpacket/afpacket.go#L118
func afPacketComputeSize(
	targetSizeMb int,
	snapLen int,
	pageSize int,
) (
	frameSize int,
	blockSize int,
	numBlocks int,
	err error,
) {
	if snapLen < pageSize {
		// When snapLen < pageSize, find the largest value <= pageSize that
		// is a multiple of snapLen and divides evenly into pageSize.
		// This ensures frameSize is a divisor of pageSize.
		// Example: snapLen=512, pageSize=4096 -> frameSize=512
		// Example: snapLen=1000, pageSize=4096 -> frameSize=1024
		frameSize = pageSize / (pageSize / snapLen)
	} else {
		// When snapLen >= pageSize, round up to the next multiple of pageSize.
		// Example: snapLen=9216, pageSize=4096 -> frameSize=12288 (3 pages)
		frameSize = ((snapLen / pageSize) + 1) * pageSize
	}

	// 128 is the default from the gopacket library so just use that
	blockSize = frameSize * 128
	numBlocks = (targetSizeMb * 1024 * 1024) / blockSize

	if numBlocks == 0 {
		return 0, 0, 0, fmt.Errorf("interface bufferSize is too small")
	}

	return frameSize, blockSize, numBlocks, nil
}

func afPacketNewTPacket(ifName string) (*afpacket.TPacket, error) {
	szFrame, szBlock, numBlocks, err := afPacketComputeSize(
		afPacketBufferSize,
		afPacketSnaplen,
		os.Getpagesize(),
	)
	if err != nil {
		return nil, err
	}

	return afpacket.NewTPacket(
		afpacket.OptInterface(ifName),
		afpacket.OptFrameSize(szFrame),
		afpacket.OptBlockSize(szBlock),
		afpacket.OptNumBlocks(numBlocks),
		afpacket.OptAddVLANHeader(false),
		afpacket.SocketRaw,
		afpacket.TPacketVersion3,
	)
}

type ifreq struct {
	ifrName   [IFNAMSIZ]byte
	ifrHwaddr syscall.RawSockaddr
}

func addMulticastAddr(ifName string, addr net.HardwareAddr) error {
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_DGRAM, 0)
	if err != nil {
		return err
	}
	defer syscall.Close(fd)

	var name [IFNAMSIZ]byte
	copy(name[:], []byte(ifName))

	ifr := &ifreq{
		ifrName:   name,
		ifrHwaddr: toRawSockaddr(addr),
	}

	_, _, ep := unix.Syscall(
		unix.SYS_IOCTL, uintptr(fd),
		unix.SIOCADDMULTI, uintptr(unsafe.Pointer(ifr)),
	)

	if ep != 0 {
		return syscall.Errno(ep)
	}
	return nil
}
