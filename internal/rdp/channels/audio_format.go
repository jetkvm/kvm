package channels

import "encoding/binary"

// Shared audio format constants for RDPSND (output) and AUDIN (input).
// Both channels use identical preferred format: 16-bit PCM, stereo, 48kHz.
const (
	AudioPreferredChannels      = 2
	AudioPreferredSampleRate    = 48000
	AudioPreferredBitsPerSample = 16
	AudioPreferredBlockAlign    = AudioPreferredChannels * (AudioPreferredBitsPerSample / 8) // 4 bytes
	AudioPreferredBytesPerSec   = AudioPreferredSampleRate * AudioPreferredBlockAlign        // 192000 bytes/sec
)

// WAVEFORMATEX structure size (without extra data).
const WAVEFORMATEXSize = 18

// EncodeWAVEFORMATEX writes a WAVEFORMATEX structure to buf at the given offset.
// Returns the number of bytes written (always WAVEFORMATEXSize).
// buf must have at least offset + WAVEFORMATEXSize bytes available.
func EncodeWAVEFORMATEX(buf []byte, offset int, fmt AudioFormat) int {
	binary.LittleEndian.PutUint16(buf[offset:], fmt.FormatTag)
	binary.LittleEndian.PutUint16(buf[offset+2:], fmt.Channels)
	binary.LittleEndian.PutUint32(buf[offset+4:], fmt.SamplesPerSec)
	binary.LittleEndian.PutUint32(buf[offset+8:], fmt.AvgBytesPerSec)
	binary.LittleEndian.PutUint16(buf[offset+12:], fmt.BlockAlign)
	binary.LittleEndian.PutUint16(buf[offset+14:], fmt.BitsPerSample)
	binary.LittleEndian.PutUint16(buf[offset+16:], 0) // cbSize (no extra data)
	return WAVEFORMATEXSize
}

// EncodePreferredWAVEFORMATEX writes the preferred audio format (PCM stereo 48kHz) to buf.
// Returns the number of bytes written (always WAVEFORMATEXSize).
func EncodePreferredWAVEFORMATEX(buf []byte, offset int) int {
	return EncodeWAVEFORMATEX(buf, offset, AudioFormat{
		FormatTag:      WaveFormatPCM,
		Channels:       AudioPreferredChannels,
		SamplesPerSec:  AudioPreferredSampleRate,
		AvgBytesPerSec: AudioPreferredBytesPerSec,
		BlockAlign:     AudioPreferredBlockAlign,
		BitsPerSample:  AudioPreferredBitsPerSample,
	})
}

// ParseWAVEFORMATEX parses a WAVEFORMATEX structure from data at the given offset.
// Returns the parsed format, the cbSize field value, and true if successful.
// Returns zero values and false if data is too short.
func ParseWAVEFORMATEX(data []byte, offset int) (AudioFormat, uint16, bool) {
	if offset+WAVEFORMATEXSize > len(data) {
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

// IsPreferredFormat returns true if the format matches our preferred format.
func IsPreferredFormat(fmt AudioFormat) bool {
	return fmt.FormatTag == WaveFormatPCM &&
		fmt.Channels == AudioPreferredChannels &&
		fmt.SamplesPerSec == AudioPreferredSampleRate &&
		fmt.BitsPerSample == AudioPreferredBitsPerSample
}

// FindPreferredFormat searches formats for the preferred format.
// Returns the index and format if found, or -1 and zero value if not found.
func FindPreferredFormat(formats []AudioFormat) (int, AudioFormat) {
	// First pass: exact match
	for i, fmt := range formats {
		if IsPreferredFormat(fmt) {
			return i, fmt
		}
	}
	// Fallback: any PCM format
	for i, fmt := range formats {
		if fmt.FormatTag == WaveFormatPCM {
			return i, fmt
		}
	}
	return -1, AudioFormat{}
}
