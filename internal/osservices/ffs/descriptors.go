// Package ffs is the OS Services USB function on Linux: a FunctionFS
// instance with one vendor interface (class 0xff, subclass 0x4a, protocol
// 0x01), one bulk OUT and one bulk IN endpoint, and Microsoft OS 1.0
// descriptors so that Windows binds WinUSB without a driver package
// (Transport, USB interface).
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

func interfaceDescriptor(b *bytes.Buffer) {
	b.Write([]byte{
		9,    // bLength
		4,    // bDescriptorType: interface
		0,    // bInterfaceNumber
		0,    // bAlternateSetting
		2,    // bNumEndpoints
		0xff, // bInterfaceClass
		0x4a, // bInterfaceSubClass
		0x01, // bInterfaceProtocol
		0,    // iInterface: none, as on the JetKVM Mini
	})
}

func endpointDescriptor(b *bytes.Buffer, address byte, packetSize uint16) {
	b.Write([]byte{7, 5, address, 2}) // bLength, endpoint, address, bulk
	_ = binary.Write(b, binary.LittleEndian, packetSize)
	b.WriteByte(0) // bInterval
}

// speedDescriptors are the interface and its endpoints at one speed. The
// OUT endpoint comes first, so FunctionFS names it ep1 and the IN endpoint
// ep2.
func speedDescriptors(b *bytes.Buffer, packetSize uint16) {
	interfaceDescriptor(b)
	endpointDescriptor(b, 0x01, packetSize)
	endpointDescriptor(b, 0x82, packetSize)
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
func Descriptors() []byte {
	var body bytes.Buffer
	speedDescriptors(&body, PacketSizeFS)
	speedDescriptors(&body, PacketSizeHS)
	extendedCompatID(&body)
	extendedProperties(&body)

	var b bytes.Buffer
	const headerLen = 4 * 6 // magic, length, flags, fs, hs and os counts
	_ = binary.Write(&b, binary.LittleEndian, uint32(descriptorsMagicV2))
	_ = binary.Write(&b, binary.LittleEndian, uint32(headerLen+body.Len()))
	_ = binary.Write(&b, binary.LittleEndian, uint32(hasFSDesc|hasHSDesc|hasMSOSDesc))
	_ = binary.Write(&b, binary.LittleEndian, uint32(3)) // fs_count
	_ = binary.Write(&b, binary.LittleEndian, uint32(3)) // hs_count
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
