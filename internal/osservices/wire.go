// Package osservices is the KVM side of the IP-KVM OS Services protocol,
// version 5.0: the reset exchange, the byte-stream parser, the handshake and
// every service, as a single-threaded state machine (Session). The caller
// serializes every call and supplies the clock, randomness, the signing key,
// the IN endpoint and the effects through a Platform.
//
// It is a port of the JetKVM Mini firmware's os_services_session.c
// (SPDX-License-Identifier: MIT). Section names in comments refer to the
// sections of PROTOCOL.md in jetkvm/os-services.
package osservices

import (
	"encoding/binary"
	"unicode/utf8"
)

// Protocol version (Versions).
const (
	ProtocolMajor = 5
	ProtocolMinor = 0
)

// USB interface (Transport, USB interface).
const (
	USBClass         = 0xff
	USBSubclass      = 0x4a
	USBProtocol      = 0x01
	USBRequestReset  = 0x40
	USBRequestStatus = 0x41
	USBStatusLen     = 24
	TokenLen         = 16
)

// Phase is the phase in a RESET or STATUS answer.
type Phase uint8

const (
	PhaseResetting Phase = 0
	PhaseReady     Phase = 1
	PhaseActive    Phase = 2
	PhaseFaulted   Phase = 3
)

// Envelope and limits.
const (
	EnvelopeLen       = 8
	MaxControlPayload = 64 * 1024
	MaxFramePayload   = 8 * 1024 * 1024
	MaxDimension      = 16384
	MaxFPS            = 240
	// The KVM searches this much input for the session.hello of a new
	// session.
	HelloSearchLimit = 8 * 1024 * 1024
)

// Service IDs.
const (
	ServiceSession   = 0
	ServiceVideo     = 1
	ServiceClipboard = 2
	ServiceTerminal  = 3
	ServiceFile      = 4
	ServiceUpdate    = 5
	serviceCount     = 6
)

// Session service kinds.
const (
	sessionHello  = 1
	sessionReady  = 2
	sessionPing   = 3
	sessionClose  = 4
	sessionConfig = 5
	sessionInfo   = 6
)

// session.close reasons.
const (
	closeShutdown      = 0
	closeProtocolError = 2
	closeNotApproved   = 3
)

const (
	helloLen          = 52
	readyLen          = 116
	readySignedLen    = 52
	PublicKeyLen      = 32
	SignatureLen      = 64
	signatureContext  = "IP-KVM OS Services session.ready v5"
	fingerprintLen    = 8
	infoVersionMax    = 64
	infoDisplayMax    = 256
	infoDisplaysMax   = 64
	helloMessageLen   = EnvelopeLen + helloLen
	helloTokenOffset  = EnvelopeLen + 4
	searchTailLen     = helloMessageLen - 1
	CaptureDisplayMax = infoDisplayMax
)

// Video service kinds and fields.
const (
	videoCapabilities    = 1
	videoConfig          = 3
	videoStatus          = 4
	videoFrame           = 5
	videoRequestKeyframe = 6

	VideoCodecFlagH264      = 1 << 0
	VideoCodecFlagH265      = 1 << 1
	videoFlagSetsDisplay    = 1 << 2
	videoCapabilitiesLen    = 16
	videoConfigLen          = 20
	videoStatusMinLen       = 32
	videoFrameHeaderLen     = 12
	frameKeyframe           = 1 << 0
	frameParameterSets      = 1 << 1
	videoErrorDisplayAsleep = 1
)

// Codec is a video codec of the video service.
type Codec uint8

const (
	CodecH264 Codec = 0
	CodecH265 Codec = 1
)

func (c Codec) String() string {
	if c == CodecH265 {
		return "H.265"
	}
	return "H.264"
}

func (c Codec) flag() uint32 {
	if c == CodecH265 {
		return VideoCodecFlagH265
	}
	return VideoCodecFlagH264
}

// Layout is the place of the KVM display among the target's displays.
type Layout uint8

const (
	LayoutMirror Layout = 0
	LayoutExtend Layout = 1
)

// Clipboard service.
const (
	clipboardCapabilities = 1
	clipboardGet          = 2
	clipboardContent      = 3
	clipboardSet          = 4
	clipboardStatus       = 5
	clipboardConfig       = 7

	ClipboardFlagRead  = 1 << 0
	ClipboardFlagWrite = 1 << 1
	ClipboardMaxText   = 65528
)

// ClipboardStatus is the outcome of a clipboard request.
type ClipboardStatus uint8

const (
	ClipboardOK          ClipboardStatus = 0
	ClipboardUnsupported ClipboardStatus = 1
	ClipboardEmpty       ClipboardStatus = 2
	ClipboardDenied      ClipboardStatus = 3
	ClipboardFailed      ClipboardStatus = 4
)

func (s ClipboardStatus) String() string {
	switch s {
	case ClipboardOK:
		return "ok"
	case ClipboardUnsupported:
		return "unsupported"
	case ClipboardEmpty:
		return "empty"
	case ClipboardDenied:
		return "denied"
	default:
		return "failed"
	}
}

// TargetOS is the operating system the sender reports.
type TargetOS uint8

const (
	OSUnknown TargetOS = 0
	OSWindows TargetOS = 1
	OSMacOS   TargetOS = 2
	OSLinux   TargetOS = 3
)

func (o TargetOS) String() string {
	switch o {
	case OSWindows:
		return "windows"
	case OSMacOS:
		return "macos"
	case OSLinux:
		return "linux"
	default:
		return "unknown"
	}
}

// Terminal service.
const (
	terminalCapabilities = 1
	terminalOpen         = 2
	terminalOpened       = 3
	terminalInput        = 4
	terminalOutput       = 5
	terminalResize       = 6
	terminalClose        = 7
	terminalExited       = 8
	terminalCredit       = 9

	terminalFlagShell = 1 << 0
	// TerminalMaxData is the data of one terminal.input or terminal.output.
	TerminalMaxData   = MaxControlPayload - 4
	terminalSizeLen   = 8
	terminalStatusLen = 5
	terminalCreditLen = 8
	// TerminalOutputWindow is the terminal.output a terminal session may
	// have beyond the credit the KVM returned.
	TerminalOutputWindow = 128 * 1024

	terminalOpenedOK          = 0
	terminalOpenedUnsupported = 1
	terminalOpenedBusy        = 2
	terminalOpenedFailed      = 3

	terminalExitExited = 0
	terminalExitClosed = 1
	terminalExitFailed = 2
)

// File service.
const (
	fileCapabilities = 1
	fileDrop         = 2
	fileReady        = 3
	fileChunk        = 4
	fileCredit       = 5
	fileDone         = 6
	fileCancel       = 7

	fileFlagReceive   = 1 << 0
	chunkHeaderLen    = 12
	ChunkMaxData      = MaxControlPayload - chunkHeaderLen
	fileDropHeaderLen = 4
	fileDropEntryLen  = 14
	fileCreditLen     = 8
	fileDoneFixedLen  = 8
)

// FileStatus is how a transfer ended.
type FileStatus uint8

const (
	FileOK        FileStatus = 0
	FileFailed    FileStatus = 1
	FileCancelled FileStatus = 2
)

func (s FileStatus) String() string {
	switch s {
	case FileOK:
		return "ok"
	case FileCancelled:
		return "cancelled"
	default:
		return "failed"
	}
}

// Update service.
const (
	updateCapabilities = 1
	updateBegin        = 2
	updateReady        = 3
	updateChunk        = 4
	updateCredit       = 5
	updateStatus       = 6
	updateCancel       = 7

	updateFlagInstall   = 1 << 0
	UpdatePackageMax    = 64
	UpdateVersionMax    = 64
	updateBeginFixedLen = 44
	UpdateInstalling    = 0
	UpdateFailed        = 1
	UpdateCancelled     = 2
)

var le = binary.LittleEndian

func putEnvelope(p []byte, service, kind uint16, size uint32) {
	le.PutUint16(p, service)
	le.PutUint16(p[2:], kind)
	le.PutUint32(p[4:], size)
}

// payloadLimit is 8 MiB for video.frame, else 64 KiB.
func payloadLimit(service, kind uint16) uint32 {
	if service == ServiceVideo && kind == videoFrame {
		return MaxFramePayload
	}
	return MaxControlPayload
}

// UTF8Valid is the strict UTF-8 check: no overlongs, surrogates or code
// points past U+10FFFF. Go's utf8.Valid is that check.
func UTF8Valid(text []byte) bool {
	return utf8.Valid(text)
}

// FileNameValid checks a file.drop name (File service): one path component
// of valid UTF-8, not empty, not "." or "..", without '/', '\\' or a
// control character (U+0000 to U+001F and U+007F to U+009F).
func FileNameValid(name []byte) bool {
	if len(name) == 0 || !utf8.Valid(name) {
		return false
	}
	if string(name) == "." || string(name) == ".." {
		return false
	}
	for i, b := range name {
		if b < 0x20 || b == 0x7f || b == '/' || b == '\\' {
			return false
		}
		// U+0080 to U+009F: in valid UTF-8, 0xc2 is only ever a lead byte.
		if b == 0xc2 && i+1 < len(name) && name[i+1] <= 0x9f {
			return false
		}
	}
	return true
}

// FormatFingerprint formats the first 8 bytes of SHA-256(public key) as
// "AA:BB:...".
func FormatFingerprint(digest []byte) string {
	const hex = "0123456789ABCDEF"
	out := make([]byte, 0, fingerprintLen*3)
	for i := 0; i < fingerprintLen && i < len(digest); i++ {
		if i > 0 {
			out = append(out, ':')
		}
		out = append(out, hex[digest[i]>>4], hex[digest[i]&0x0f])
	}
	return string(out)
}

// walkNALUnits calls visit for the first byte of each NAL unit after a 3- or
// 4-byte start code, until visit returns false.
func walkNALUnits(data []byte, visit func(header byte) bool) {
	i := 0
	for i+3 < len(data) {
		switch {
		case data[i] == 0 && data[i+1] == 0 && data[i+2] == 1:
			i += 3
		case data[i] == 0 && data[i+1] == 0 && data[i+2] == 0 && i+4 < len(data) && data[i+3] == 1:
			i += 4
		default:
			i++
			continue
		}
		if i >= len(data) {
			return
		}
		if !visit(data[i]) {
			return
		}
	}
}

// nalVCL reports whether a NAL header is a VCL NAL unit, and whether it is
// a keyframe's (H.264 IDR; H.265 IDR_W_RADL, IDR_N_LP or CRA).
func nalVCL(codec Codec, header byte) (vcl, key bool) {
	if codec == CodecH265 {
		t := (header >> 1) & 0x3f
		return t <= 31, t >= 19 && t <= 21
	}
	t := header & 0x1f
	return t >= 1 && t <= 5, t == 5
}

// AnnexBIsKeyframe looks at the first VCL NAL unit of an access unit (the
// rule in video.frame).
func AnnexBIsKeyframe(codec Codec, data []byte) bool {
	keyframe := false
	walkNALUnits(data, func(header byte) bool {
		vcl, key := nalVCL(codec, header)
		if vcl {
			keyframe = key
			return false
		}
		return true
	})
	return keyframe
}

// parameterSetKind is 1 for SPS, 2 for PPS, 3 for VPS, 0 for others.
func parameterSetKind(codec Codec, header byte) int {
	if codec == CodecH265 {
		switch (header >> 1) & 0x3f {
		case 32:
			return 3
		case 33:
			return 1
		case 34:
			return 2
		}
		return 0
	}
	switch header & 0x1f {
	case 7:
		return 1
	case 8:
		return 2
	}
	return 0
}

// AnnexBHasParameterSets reports whether an access unit carries the
// parameter sets of its codec: SPS and PPS, and for H.265 also VPS.
func AnnexBHasParameterSets(codec Codec, data []byte) bool {
	var seen [4]bool
	complete := func() bool {
		return seen[1] && seen[2] && (codec != CodecH265 || seen[3])
	}
	walkNALUnits(data, func(header byte) bool {
		seen[parameterSetKind(codec, header)] = true
		return !complete()
	})
	return complete()
}
