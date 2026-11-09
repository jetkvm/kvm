// Package ffs is the OS Services USB function on Linux: a FunctionFS
// instance with one vendor interface (class 0xff, subclass 0x4a, protocol
// 0x01), one bulk OUT and one bulk IN endpoint, and Microsoft OS 1.0
// descriptors so that Windows binds WinUSB without a driver package
// (Transport, USB interface).
//
// The interface can have one more endpoint, an interrupt IN endpoint that
// nothing uses. The USB controller gives a function its IN endpoints in the
// order of the descriptors, so the unused endpoint takes the number the
// bulk IN endpoint would have, and the bulk IN endpoint takes the next one.
// The caller asks for it when the bulk IN endpoint would get a number that
// does not work on the device. A sender takes the bulk endpoints and ignores
// the rest.
package ffs

import (
	"bytes"
	"encoding/binary"
)

// FunctionFS constants (include/uapi/linux/usb/functionfs.h).
const (
	descriptorsMagicV2 = 3
	stringsMagic       = 2

	hasFSDesc   = 1
	hasHSDesc   = 2
	hasMSOSDesc = 8

	// Event types of struct usb_functionfs_event.
	EventBind    = 0
	EventUnbind  = 1
	EventEnable  = 2
	EventDisable = 3
	EventSetup   = 4
	EventSuspend = 5
	EventResume  = 6
)

// DeviceInterfaceGUID is JetKVM's DeviceInterfaceGUID for the interface.
const DeviceInterfaceGUID = "{5D14C7C2-6FC8-4F50-9A6E-3B4826C2E38A}"

// Packet sizes of the bulk endpoints at full and high speed.
const (
	PacketSizeFS = 64
	PacketSizeHS = 512
)

// Endpoint transfer types (bmAttributes).
const (
	transferBulk      = 2
	transferInterrupt = 3
)

// The spare interrupt IN endpoint: a small packet and a long interval (10 ms
// at full speed, 64 ms at high speed), as nothing reads it.
const (
	sparePacketSize = 8
	spareInterval   = 10
)

func interfaceDescriptor(b *bytes.Buffer, endpoints byte) {
	b.Write([]byte{
		9,         // bLength
		4,         // bDescriptorType: interface
		0,         // bInterfaceNumber
		0,         // bAlternateSetting
		endpoints, // bNumEndpoints
		0xff,      // bInterfaceClass
		0x4a,      // bInterfaceSubClass
		0x01,      // bInterfaceProtocol
		0,         // iInterface: none, as on the JetKVM Mini
	})
}

func endpointDescriptor(b *bytes.Buffer, address, attributes byte, packetSize uint16, interval byte) {
	b.Write([]byte{7, 5, address, attributes}) // bLength, endpoint, address, transfer type
	_ = binary.Write(b, binary.LittleEndian, packetSize)
	b.WriteByte(interval)
}

// speedDescriptors are the interface and its endpoints at one speed.
// FunctionFS names the endpoint files by the endpoint numbers here: ep1 is
// the bulk OUT endpoint; ep2 is the bulk IN endpoint, or with the spare
// endpoint ep2 is the spare and ep3 the bulk IN endpoint.
func speedDescriptors(b *bytes.Buffer, packetSize uint16, spareIN bool) {
	if !spareIN {
		interfaceDescriptor(b, 2)
		endpointDescriptor(b, 0x01, transferBulk, packetSize, 0)
		endpointDescriptor(b, 0x82, transferBulk, packetSize, 0)
		return
	}
	interfaceDescriptor(b, 3)
	endpointDescriptor(b, 0x01, transferBulk, packetSize, 0)
	endpointDescriptor(b, 0x82, transferInterrupt, sparePacketSize, spareInterval)
	endpointDescriptor(b, 0x83, transferBulk, packetSize, 0)
}

// osDescHeader is struct usb_os_desc_header: interface, dwLength,
// bcdVersion, wIndex, and bCount with a reserved byte (wIndex 4) or wCount
// (wIndex 5). FunctionFS before Linux 5.12 takes only bcdVersion 0x0001
// (the JetKVM runs 5.10); later kernels take it too, with a warning.
func osDescHeader(b *bytes.Buffer, length uint32, index uint16, count uint16) {
	b.WriteByte(0) // interface
	_ = binary.Write(b, binary.LittleEndian, length)
	_ = binary.Write(b, binary.LittleEndian, uint16(0x0001))
	_ = binary.Write(b, binary.LittleEndian, index)
	_ = binary.Write(b, binary.LittleEndian, count)
}

const osDescHeaderLen = 11

// extendedCompatID is the extended compat ID feature: interface 0 is
// "WINUSB".
func extendedCompatID(b *bytes.Buffer) {
	const entryLen = 24
	osDescHeader(b, osDescHeaderLen+entryLen, 4, 1)
	b.WriteByte(0) // bFirstInterfaceNumber
	b.WriteByte(1) // Reserved1, which the specification sets to 1
	b.Write([]byte("WINUSB\x00\x00"))
	b.Write(make([]byte, 8)) // SubCompatibleID
	b.Write(make([]byte, 6)) // Reserved2
}

// extendedProperties is the extended properties feature with
// DeviceInterfaceGUID (REG_SZ). FunctionFS takes the name and the data as
// 8-bit text with their terminators, and reports them to the host as UTF-16.
func extendedProperties(b *bytes.Buffer) {
	name := []byte("DeviceInterfaceGUID\x00")
	data := []byte(DeviceInterfaceGUID + "\x00")
	size := uint32(14 + len(name) + len(data))
	osDescHeader(b, osDescHeaderLen+size, 5, 1)
	_ = binary.Write(b, binary.LittleEndian, size)
	_ = binary.Write(b, binary.LittleEndian, uint32(1)) // REG_SZ
	_ = binary.Write(b, binary.LittleEndian, uint16(len(name)))
	b.Write(name)
	_ = binary.Write(b, binary.LittleEndian, uint32(len(data)))
	b.Write(data)
}

// Descriptors is the FunctionFS v2 descriptor blob that ep0 takes first.
// With spareIN the interface has the unused interrupt IN endpoint before
// the bulk IN endpoint.
func Descriptors(spareIN bool) []byte {
	var body bytes.Buffer
	speedDescriptors(&body, PacketSizeFS, spareIN)
	speedDescriptors(&body, PacketSizeHS, spareIN)
	extendedCompatID(&body)
	extendedProperties(&body)

	// The interface and each endpoint count as one descriptor.
	perSpeed := uint32(3)
	if spareIN {
		perSpeed = 4
	}

	var b bytes.Buffer
	const headerLen = 4 * 6 // magic, length, flags, fs, hs and os counts
	_ = binary.Write(&b, binary.LittleEndian, uint32(descriptorsMagicV2))
	_ = binary.Write(&b, binary.LittleEndian, uint32(headerLen+body.Len()))
	_ = binary.Write(&b, binary.LittleEndian, uint32(hasFSDesc|hasHSDesc|hasMSOSDesc))
	_ = binary.Write(&b, binary.LittleEndian, perSpeed)  // fs_count
	_ = binary.Write(&b, binary.LittleEndian, perSpeed)  // hs_count
	_ = binary.Write(&b, binary.LittleEndian, uint32(2)) // os_count
	b.Write(body.Bytes())
	return b.Bytes()
}

// Strings is the FunctionFS string blob that ep0 takes after the
// descriptors: no strings, as the interface names none.
func Strings() []byte {
	var b bytes.Buffer
	_ = binary.Write(&b, binary.LittleEndian, uint32(stringsMagic))
	_ = binary.Write(&b, binary.LittleEndian, uint32(16))
	_ = binary.Write(&b, binary.LittleEndian, uint32(0)) // str_count
	_ = binary.Write(&b, binary.LittleEndian, uint32(0)) // lang_count
	return b.Bytes()
}
