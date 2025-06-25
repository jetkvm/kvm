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

func afPacketComputeSize(targetSizeMb int, snaplen int, pageSize int) (
	frameSize int, blockSize int, numBlocks int, err error) {
	if snaplen < pageSize {
		frameSize = pageSize / (pageSize / snaplen)
	} else {
		frameSize = (snaplen/pageSize + 1) * pageSize
	}

	// 128 is the default from the gopacket library so just use that
	blockSize = frameSize * 128
	numBlocks = (targetSizeMb * 1024 * 1024) / blockSize

	if numBlocks == 0 {
		return 0, 0, 0, fmt.Errorf("interface buffersize is too small")
	}

	return frameSize, blockSize, numBlocks, nil
}

func afPacketNewTPacket(ifName string) (*afpacket.TPacket, error) {
	szFrame, szBlock, numBlocks, err := afPacketComputeSize(
		afPacketBufferSize,
		afPacketSnaplen,
		os.Getpagesize())
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
		afpacket.TPacketVersion3)
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

	_, _, ep := unix.Syscall(unix.SYS_IOCTL, uintptr(fd),
		unix.SIOCADDMULTI, uintptr(unsafe.Pointer(ifr)))

	if ep != 0 {
		return syscall.Errno(ep)
	}
	return nil
}
