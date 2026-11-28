// Package installer builds the OS Services installer disc that the KVM
// mounts as a CD-ROM: an ISO 9660 image with a Joliet tree, volume label
// JETKVM, and one file, "Install OS Services.html", a page that redirects to
// the OS Services download for this firmware version, with a link in case
// the browser does not follow the redirect. The image is the one the JetKVM
// Mini firmware builds (builtin_installer_iso.py), with kvm= in place of
// mini= in the link.
package installer

import (
	"encoding/binary"
	"html"
	"net/url"
	"unicode/utf16"
)

const (
	// Filename is the disc's name in the virtual media state.
	Filename    = "Install OS Services.iso"
	VolumeLabel = "JETKVM"
	PageName    = "Install OS Services.html"
	InstallURL  = "https://jetkvm.com/os-services/install"

	sector      = 2048
	pageISOName = "INSTALL.HTM;1"
	// Zero sectors after the file, as genisoimage -pad adds, so that a
	// host's read-ahead past the file stays inside the volume.
	padSectors = 150

	pvdSector          = 16
	svdSector          = 17
	terminatorSector   = 18
	isoLPathSector     = 19
	isoMPathSector     = 20
	jolietLPathSector  = 21
	jolietMPathSector  = 22
	isoRootSector      = 23
	jolietRootSector   = 24
	fileSector         = 25
	pathTableSize      = 10
	volumeDescriptorSz = 883
)

// Fixed, so that the same version always gives the same image.
var (
	recordDate = []byte{126, 1, 1, 0, 0, 0, 0} // 2026-01-01 00:00:00 UTC
	volumeDate = []byte("2026010100000000\x00")
	noDate     = []byte("0000000000000000\x00")
)

// Link is the page's target for a firmware version.
func Link(version string) string {
	return InstallURL + "?kvm=" + url.QueryEscape(version)
}

// Page is "Install OS Services.html".
func Page(version string) []byte {
	link := html.EscapeString(Link(version))
	return []byte("<!doctype html>\n" +
		"<html lang=\"en\">\n" +
		"<head>\n" +
		"<meta charset=\"utf-8\">\n" +
		"<meta http-equiv=\"refresh\" content=\"0; url=" + link + "\">\n" +
		"<title>Install OS Services</title>\n" +
		"</head>\n" +
		"<body>\n" +
		"<p><a href=\"" + link + "\">Install OS Services</a></p>\n" +
		"</body>\n" +
		"</html>\n")
}

// Image is the whole disc for a firmware version.
func Image(version string) []byte {
	content := Page(version)
	fileSectors := (len(content) + sector - 1) / sector
	volumeSectors := fileSector + fileSectors + padSectors
	data := make([]byte, volumeSectors*sector)
	put := func(at int, payload []byte) { copy(data[at*sector:], payload) }

	put(pvdSector, volumeDescriptor(false, volumeSectors))
	put(svdSector, volumeDescriptor(true, volumeSectors))
	put(terminatorSector, []byte("\xffCD001\x01"))
	put(isoLPathSector, pathTable(isoRootSector, false))
	put(isoMPathSector, pathTable(isoRootSector, true))
	put(jolietLPathSector, pathTable(jolietRootSector, false))
	put(jolietMPathSector, pathTable(jolietRootSector, true))
	put(isoRootSector, directory(isoRootSector, []byte(pageISOName), len(content)))
	put(jolietRootSector, directory(jolietRootSector, ucs2(PageName), len(content)))
	put(fileSector, content)
	return data
}

func both16(v int) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint16(b, uint16(v))
	binary.BigEndian.PutUint16(b[2:], uint16(v))
	return b
}

func both32(v int) []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint32(b, uint32(v))
	binary.BigEndian.PutUint32(b[4:], uint32(v))
	return b
}

func ucs2(s string) []byte {
	units := utf16.Encode([]rune(s))
	b := make([]byte, 2*len(units))
	for i, u := range units {
		binary.BigEndian.PutUint16(b[2*i:], u)
	}
	return b
}

// text is an identifier field padded with spaces, in UCS-2 for Joliet.
func text(value string, size int, joliet bool) []byte {
	encoded, pad := []byte(value), []byte(" ")
	if joliet {
		encoded, pad = ucs2(value), ucs2(" ")
	}
	if len(encoded) > size {
		panic("installer: " + value + " does not fit its field")
	}
	for len(encoded)+len(pad) <= size {
		encoded = append(encoded, pad...)
	}
	for len(encoded) < size {
		encoded = append(encoded, ' ')
	}
	return encoded
}

func directoryRecord(identifier []byte, extent, length int, isDirectory bool) []byte {
	flags := byte(0)
	if isDirectory {
		flags = 2
	}
	record := []byte{0, 0}
	record = append(record, both32(extent)...)
	record = append(record, both32(length)...)
	record = append(record, recordDate...)
	record = append(record, flags, 0, 0)
	record = append(record, both16(1)...)
	record = append(record, byte(len(identifier)))
	record = append(record, identifier...)
	if len(identifier)%2 == 0 {
		record = append(record, 0)
	}
	record[0] = byte(len(record))
	return record
}

func directory(root int, name []byte, fileSize int) []byte {
	var records []byte
	records = append(records, directoryRecord([]byte{0}, root, sector, true)...)
	records = append(records, directoryRecord([]byte{1}, root, sector, true)...)
	records = append(records, directoryRecord(name, fileSector, fileSize, false)...)
	return records
}

func pathTable(root int, bigEndian bool) []byte {
	table := []byte{1, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	if bigEndian {
		binary.BigEndian.PutUint32(table[2:], uint32(root))
		binary.BigEndian.PutUint16(table[6:], 1)
	} else {
		binary.LittleEndian.PutUint32(table[2:], uint32(root))
		binary.LittleEndian.PutUint16(table[6:], 1)
	}
	return table
}

func volumeDescriptor(joliet bool, volumeSectors int) []byte {
	kind, root, lPath, mPath := byte(1), isoRootSector, isoLPathSector, isoMPathSector
	escapes := make([]byte, 32)
	if joliet {
		kind, root, lPath, mPath = 2, jolietRootSector, jolietLPathSector, jolietMPathSector
		// The escape sequence of UCS-2 level 3 names the Joliet tree.
		copy(escapes, "%/E")
	}
	d := []byte{kind}
	d = append(d, "CD001\x01\x00"...)
	d = append(d, text("", 32, joliet)...)
	d = append(d, text(VolumeLabel, 32, joliet)...)
	d = append(d, make([]byte, 8)...)
	d = append(d, both32(volumeSectors)...)
	d = append(d, escapes...)
	d = append(d, both16(1)...)
	d = append(d, both16(1)...)
	d = append(d, both16(sector)...)
	d = append(d, both32(pathTableSize)...)
	d = binary.LittleEndian.AppendUint32(d, uint32(lPath))
	d = append(d, make([]byte, 4)...)
	d = binary.BigEndian.AppendUint32(d, uint32(mPath))
	d = append(d, make([]byte, 4)...)
	d = append(d, directoryRecord([]byte{0}, root, sector, true)...)
	for _, size := range []int{128, 128, 128, 128, 37, 37, 37} {
		d = append(d, text("", size, joliet)...)
	}
	d = append(d, volumeDate...)
	d = append(d, volumeDate...)
	d = append(d, noDate...)
	d = append(d, volumeDate...)
	d = append(d, 1, 0)
	if len(d) != volumeDescriptorSz {
		panic("installer: a volume descriptor of the wrong size")
	}
	return d
}
