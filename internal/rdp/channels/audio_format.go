package channels

import "encoding/binary"

// Shared audio format constants for RDPSND (output) and AUDIN (input).
// Both channels use identical preferred format: 16-bit PCM, stereo, 48kHz.
const (
	audioPreferredChannels      = 2
	audioPreferredSampleRate    = 48000
	audioPreferredBitsPerSample = 16
	audioPreferredBlockAlign    = audioPreferredChannels * (audioPreferredBitsPerSample / 8) // 4 bytes
	audioPreferredBytesPerSec   = audioPreferredSampleRate * audioPreferredBlockAlign        // 192000 bytes/sec
)

// waveformatexSize is the WAVEFORMATEX structure size (without extra data).
const waveformatexSize = 18

// encodeWAVEFORMATEX writes a WAVEFORMATEX structure to buf at the given offset.
// Returns the number of bytes written (always waveformatexSize).
// buf must have at least offset + waveformatexSize bytes available.
func encodeWAVEFORMATEX(buf []byte, offset int, fmt AudioFormat) int {
	binary.LittleEndian.PutUint16(buf[offset:], fmt.FormatTag)
	binary.LittleEndian.PutUint16(buf[offset+2:], fmt.Channels)
	binary.LittleEndian.PutUint32(buf[offset+4:], fmt.SamplesPerSec)
	binary.LittleEndian.PutUint32(buf[offset+8:], fmt.AvgBytesPerSec)
	binary.LittleEndian.PutUint16(buf[offset+12:], fmt.BlockAlign)
	binary.LittleEndian.PutUint16(buf[offset+14:], fmt.BitsPerSample)
	binary.LittleEndian.PutUint16(buf[offset+16:], 0) // cbSize (no extra data)
	return waveformatexSize
}

// encodePreferredWAVEFORMATEX writes the preferred audio format (PCM stereo 48kHz) to buf.
// Returns the number of bytes written (always waveformatexSize).
func encodePreferredWAVEFORMATEX(buf []byte, offset int) int {
	return encodeWAVEFORMATEX(buf, offset, AudioFormat{
		FormatTag:      waveFormatPCM,
		Channels:       audioPreferredChannels,
		SamplesPerSec:  audioPreferredSampleRate,
		AvgBytesPerSec: audioPreferredBytesPerSec,
		BlockAlign:     audioPreferredBlockAlign,
		BitsPerSample:  audioPreferredBitsPerSample,
	})
}

// parseWAVEFORMATEX parses a WAVEFORMATEX structure from data at the given offset.
// Returns the parsed format, the cbSize field value, and true if successful.
// Returns zero values and false if data is too short.
func parseWAVEFORMATEX(data []byte, offset int) (AudioFormat, uint16, bool) {
	if offset+waveformatexSize > len(data) {
		return AudioFormat{}, 0, false
	}

	fmt := AudioFormat{
		FormatTag:      binary.LittleEndian.Uint16(data[offset:]),
		Channels:       binary.LittleEndian.Uint16(data[offset+2:]),
		SamplesPerSec:  binary.LittleEndian.Uint32(data[offset+4:]),
		AvgBytesPerSec: binary.LittleEndian.Uint32(data[offset+8:]),
		BlockAlign:     binary.LittleEndian.Uint16(data[offset+12:]),
		BitsPerSample:  binary.LittleEndian.Uint16(data[offset+14:]),
	}
	cbSize := binary.LittleEndian.Uint16(data[offset+16:])

	return fmt, cbSize, true
}

// isPreferredFormat returns true if the format matches our preferred format.
func isPreferredFormat(fmt AudioFormat) bool {
	return fmt.FormatTag == waveFormatPCM &&
		fmt.Channels == audioPreferredChannels &&
		fmt.SamplesPerSec == audioPreferredSampleRate &&
		fmt.BitsPerSample == audioPreferredBitsPerSample
}

// findPreferredFormat searches formats for the preferred format.
// Returns the index and format if found, or -1 and zero value if not found.
func findPreferredFormat(formats []AudioFormat) (int, AudioFormat) {
	// First pass: exact match
	for i, fmt := range formats {
		if isPreferredFormat(fmt) {
			return i, fmt
		}
	}
	// Fallback: any PCM format
	for i, fmt := range formats {
		if fmt.FormatTag == waveFormatPCM {
			return i, fmt
		}
	}
	return -1, AudioFormat{}
}
