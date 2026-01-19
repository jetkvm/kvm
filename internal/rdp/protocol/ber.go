package protocol

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// BER (Basic Encoding Rules) utilities for MCS PDU encoding/decoding.
// Used in MCS Connect-Initial and Connect-Response PDUs.

// BER tag classes and types.
const (
	BERClassUniversal   = 0x00
	BERClassApplication = 0x40
	BERClassContext     = 0x80
	BERClassPrivate     = 0xC0

	BERPrimitive   = 0x00
	BERConstructed = 0x20
)

// BER universal tags.
const (
	BERTagBoolean     = 0x01
	BERTagInteger     = 0x02
	BERTagBitString   = 0x03
	BERTagOctetString = 0x04
	BERTagNull        = 0x05
	BERTagOID         = 0x06
	BERTagEnumerated  = 0x0A
	BERTagSequence    = 0x10
	BERTagSet         = 0x11
)

var (
	ErrBERTruncated       = errors.New("ber: truncated data")
	ErrBERInvalidLength   = errors.New("ber: invalid length encoding")
	ErrBERUnexpectedTag   = errors.New("ber: unexpected tag")
	ErrBERIntegerTooLarge = errors.New("ber: integer too large")
)

// BERReader provides methods to read BER-encoded data.
type BERReader struct {
	data []byte
	pos  int
}

// NewBERReader creates a new BER reader.
func NewBERReader(data []byte) *BERReader {
	return &BERReader{data: data}
}

// Remaining returns the number of unread bytes.
func (r *BERReader) Remaining() int {
	return len(r.data) - r.pos
}

// Bytes returns the remaining unread bytes.
func (r *BERReader) Bytes() []byte {
	return r.data[r.pos:]
}

// Skip advances the position by n bytes.
func (r *BERReader) Skip(n int) error {
	if r.pos+n > len(r.data) {
		return ErrBERTruncated
	}
	r.pos += n
	return nil
}

// ReadByte reads a single byte.
func (r *BERReader) ReadByte() (byte, error) {
	if r.pos >= len(r.data) {
		return 0, ErrBERTruncated
	}
	b := r.data[r.pos]
	r.pos++
	return b, nil
}

// ReadBytes reads n bytes.
func (r *BERReader) ReadBytes(n int) ([]byte, error) {
	if r.pos+n > len(r.data) {
		return nil, ErrBERTruncated
	}
	result := r.data[r.pos : r.pos+n]
	r.pos += n
	return result, nil
}

// ReadTag reads a BER tag.
func (r *BERReader) ReadTag() (byte, error) {
	return r.ReadByte()
}

// ReadLength reads a BER length.
func (r *BERReader) ReadLength() (int, error) {
	b, err := r.ReadByte()
	if err != nil {
		return 0, err
	}

	if b < 0x80 {
		// Short form
		return int(b), nil
	}

	if b == 0x80 {
		// Indefinite form (not supported)
		return 0, ErrBERInvalidLength
	}

	// Long form
	numBytes := int(b & 0x7F)
	if numBytes > 4 {
		return 0, ErrBERInvalidLength
	}

	lengthBytes, err := r.ReadBytes(numBytes)
	if err != nil {
		return 0, err
	}

	length := 0
	for _, lb := range lengthBytes {
		length = (length << 8) | int(lb)
	}

	return length, nil
}

// ReadTagAndLength reads tag and length together.
func (r *BERReader) ReadTagAndLength() (byte, int, error) {
	tag, err := r.ReadTag()
	if err != nil {
		return 0, 0, err
	}

	length, err := r.ReadLength()
	if err != nil {
		return 0, 0, err
	}

	return tag, length, nil
}

// ReadInteger reads a BER-encoded INTEGER.
func (r *BERReader) ReadInteger() (int, error) {
	tag, length, err := r.ReadTagAndLength()
	if err != nil {
		return 0, err
	}

	if tag != BERTagInteger {
		return 0, fmt.Errorf("%w: expected INTEGER (0x%02X), got 0x%02X",
			ErrBERUnexpectedTag, BERTagInteger, tag)
	}

	return r.readIntegerValue(length)
}

// ReadEnumerated reads a BER-encoded ENUMERATED.
func (r *BERReader) ReadEnumerated() (int, error) {
	tag, length, err := r.ReadTagAndLength()
	if err != nil {
		return 0, err
	}

	if tag != BERTagEnumerated {
		return 0, fmt.Errorf("%w: expected ENUMERATED (0x%02X), got 0x%02X",
			ErrBERUnexpectedTag, BERTagEnumerated, tag)
	}

	return r.readIntegerValue(length)
}

func (r *BERReader) readIntegerValue(length int) (int, error) {
	if length == 0 || length > 4 {
		return 0, ErrBERIntegerTooLarge
	}

	data, err := r.ReadBytes(length)
	if err != nil {
		return 0, err
	}

	value := 0
	for _, b := range data {
		value = (value << 8) | int(b)
	}

	// Note: Negative number sign extension not implemented as it's not used in RDP

	return value, nil
}

// ReadBoolean reads a BER-encoded BOOLEAN.
func (r *BERReader) ReadBoolean() (bool, error) {
	tag, length, err := r.ReadTagAndLength()
	if err != nil {
		return false, err
	}

	if tag != BERTagBoolean {
		return false, fmt.Errorf("%w: expected BOOLEAN (0x%02X), got 0x%02X",
			ErrBERUnexpectedTag, BERTagBoolean, tag)
	}

	if length != 1 {
		return false, ErrBERInvalidLength
	}

	b, err := r.ReadByte()
	if err != nil {
		return false, err
	}

	return b != 0, nil
}

// ReadOctetString reads a BER-encoded OCTET STRING.
func (r *BERReader) ReadOctetString() ([]byte, error) {
	tag, length, err := r.ReadTagAndLength()
	if err != nil {
		return nil, err
	}

	if tag != BERTagOctetString {
		return nil, fmt.Errorf("%w: expected OCTET STRING (0x%02X), got 0x%02X",
			ErrBERUnexpectedTag, BERTagOctetString, tag)
	}

	return r.ReadBytes(length)
}

// ReadApplicationTag reads an application-specific tag.
func (r *BERReader) ReadApplicationTag(expected byte) (int, error) {
	tag, length, err := r.ReadTagAndLength()
	if err != nil {
		return 0, err
	}

	expectedTag := BERClassApplication | BERConstructed | expected
	if tag != expectedTag {
		return 0, fmt.Errorf("%w: expected application tag 0x%02X, got 0x%02X",
			ErrBERUnexpectedTag, expectedTag, tag)
	}

	return length, nil
}

// ReadContextTag reads a context-specific tag.
func (r *BERReader) ReadContextTag(expected byte) (int, error) {
	tag, length, err := r.ReadTagAndLength()
	if err != nil {
		return 0, err
	}

	expectedTag := BERClassContext | BERConstructed | expected
	if tag != expectedTag {
		return 0, fmt.Errorf("%w: expected context tag 0x%02X, got 0x%02X",
			ErrBERUnexpectedTag, expectedTag, tag)
	}

	return length, nil
}

// BERWriter provides methods to write BER-encoded data.
type BERWriter struct {
	buf []byte
}

// NewBERWriter creates a new BER writer.
func NewBERWriter() *BERWriter {
	return &BERWriter{buf: make([]byte, 0, 256)}
}

// Bytes returns the written bytes.
func (w *BERWriter) Bytes() []byte {
	return w.buf
}

// Len returns the number of written bytes.
func (w *BERWriter) Len() int {
	return len(w.buf)
}

// WriteByte writes a single byte.
func (w *BERWriter) WriteByte(b byte) {
	w.buf = append(w.buf, b)
}

// WriteBytes writes multiple bytes.
func (w *BERWriter) WriteBytes(data []byte) {
	w.buf = append(w.buf, data...)
}

// WriteLength writes a BER length.
func (w *BERWriter) WriteLength(length int) {
	if length < 0x80 {
		w.WriteByte(byte(length))
		return
	}

	// Determine number of bytes needed
	if length <= 0xFF {
		w.WriteByte(0x81)
		w.WriteByte(byte(length))
	} else if length <= 0xFFFF {
		w.WriteByte(0x82)
		w.WriteByte(byte(length >> 8))
		w.WriteByte(byte(length))
	} else {
		w.WriteByte(0x83)
		w.WriteByte(byte(length >> 16))
		w.WriteByte(byte(length >> 8))
		w.WriteByte(byte(length))
	}
}

// WriteInteger writes a BER-encoded INTEGER.
func (w *BERWriter) WriteInteger(value int) {
	w.WriteByte(BERTagInteger)

	// Determine the minimum number of bytes needed
	var encoded []byte
	if value == 0 {
		encoded = []byte{0}
	} else {
		// Use big-endian encoding
		temp := make([]byte, 4)
		binary.BigEndian.PutUint32(temp, uint32(value))
		// Skip leading zeros (but keep at least one byte)
		start := 0
		for start < 3 && temp[start] == 0 {
			start++
		}
		// Add leading zero if high bit is set (to keep positive)
		if temp[start]&0x80 != 0 && value > 0 {
			encoded = append([]byte{0}, temp[start:]...)
		} else {
			encoded = temp[start:]
		}
	}

	w.WriteLength(len(encoded))
	w.WriteBytes(encoded)
}

// WriteBoolean writes a BER-encoded BOOLEAN.
func (w *BERWriter) WriteBoolean(value bool) {
	w.WriteByte(BERTagBoolean)
	w.WriteByte(1)
	if value {
		w.WriteByte(0xFF)
	} else {
		w.WriteByte(0x00)
	}
}

// WriteOctetString writes a BER-encoded OCTET STRING.
func (w *BERWriter) WriteOctetString(data []byte) {
	w.WriteByte(BERTagOctetString)
	w.WriteLength(len(data))
	w.WriteBytes(data)
}

// WriteApplicationTag writes an application-specific tag.
func (w *BERWriter) WriteApplicationTag(tag byte, length int) {
	w.WriteByte(BERClassApplication | BERConstructed | tag)
	w.WriteLength(length)
}

// WriteContextTag writes a context-specific tag.
func (w *BERWriter) WriteContextTag(tag byte, length int) {
	w.WriteByte(BERClassContext | BERConstructed | tag)
	w.WriteLength(length)
}

// WriteSequence writes a SEQUENCE header.
func (w *BERWriter) WriteSequence(length int) {
	w.WriteByte(BERTagSequence | BERConstructed)
	w.WriteLength(length)
}

// BERLengthSize returns the number of bytes needed to encode a length.
func BERLengthSize(length int) int {
	if length < 0x80 {
		return 1
	}
	if length <= 0xFF {
		return 2
	}
	if length <= 0xFFFF {
		return 3
	}
	return 4
}

// BERIntegerSize returns the size of a BER-encoded integer.
func BERIntegerSize(value int) int {
	if value == 0 {
		return 3 // tag + length + 0
	}

	bytes := 0
	temp := value
	for temp > 0 {
		bytes++
		temp >>= 8
	}

	// Add byte if high bit is set
	if value > 0 && (value>>(bytes*8-8))&0x80 != 0 {
		bytes++
	}

	return 1 + BERLengthSize(bytes) + bytes
}
