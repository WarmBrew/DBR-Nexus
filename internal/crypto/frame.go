package crypto

import (
	"encoding/binary"
	"fmt"
)

// Frame constants
const (
	FrameVersion byte = 0x01

	// Flag bits
	FlagHasPadding  byte = 0x01
	FlagIsDummy     byte = 0x02
	FlagKeyRotation byte = 0x04

	// Routing header size: 16(msgID) + 16(refID) + 2(channel) + 2(action) + 1(type) + 4(alias) = 41
	RoutingHeaderSize = 41

	// MaxFrameSize limits the maximum allowed frame size (1MB) to prevent DoS
	MaxFrameSize = 1 << 20
)

// RoutingHeader contains the visible routing information
type RoutingHeader struct {
	MessageID   [16]byte // UUID binary
	ReferenceID [16]byte // UUID of request being responded to (zero for new)
	ChannelCode uint16   // mapped channel code
	ActionCode  uint16   // mapped action code
	TypeCode    uint8    // message type code
	DeviceAlias uint32   // device routing alias (0 for non-device messages)
}

// IsZero returns true if the ReferenceID is all zeros
func (h *RoutingHeader) IsRefZero() bool {
	for _, b := range h.ReferenceID {
		if b != 0 {
			return false
		}
	}
	return true
}

// ParseRoutingHeader parses a routing header from bytes
func ParseRoutingHeader(data []byte) (*RoutingHeader, error) {
	if len(data) < RoutingHeaderSize {
		return nil, fmt.Errorf("routing header too short: %d < %d", len(data), RoutingHeaderSize)
	}

	h := &RoutingHeader{}
	copy(h.MessageID[:], data[0:16])
	copy(h.ReferenceID[:], data[16:32])
	h.ChannelCode = binary.BigEndian.Uint16(data[32:34])
	h.ActionCode = binary.BigEndian.Uint16(data[34:36])
	h.TypeCode = data[36]
	h.DeviceAlias = binary.BigEndian.Uint32(data[37:41])

	return h, nil
}

// MarshalBinary serializes the routing header to bytes
func (h *RoutingHeader) MarshalBinary() ([]byte, error) {
	buf := make([]byte, RoutingHeaderSize)
	copy(buf[0:16], h.MessageID[:])
	copy(buf[16:32], h.ReferenceID[:])
	binary.BigEndian.PutUint16(buf[32:34], h.ChannelCode)
	binary.BigEndian.PutUint16(buf[34:36], h.ActionCode)
	buf[36] = h.TypeCode
	binary.BigEndian.PutUint32(buf[37:41], h.DeviceAlias)
	return buf, nil
}

// Frame represents a parsed binary frame (before decryption)
type Frame struct {
	Version   byte
	Flags     byte
	SeqNum    uint32
	Timestamp int64
	Header    *RoutingHeader
	HeaderMAC [16]byte
	Payload   []byte // encrypted inner payload (nonce + ciphertext + tag)
	Padding   []byte // raw padding bytes (if any)
}

// ParseFrame parses a binary frame from wire bytes (before decryption)
func ParseFrame(data []byte) (*Frame, error) {
	if len(data) < 15 { // minimum: version(1) + flags(1) + seq(4) + ts(8) + headerLen(1)
		return nil, fmt.Errorf("frame too short: %d", len(data))
	}
	if len(data) > MaxFrameSize {
		return nil, fmt.Errorf("frame too large: %d > %d", len(data), MaxFrameSize)
	}

	f := &Frame{
		Version:   data[0],
		Flags:     data[1],
		SeqNum:    binary.BigEndian.Uint32(data[2:6]),
		Timestamp: int64(binary.BigEndian.Uint64(data[6:14])),
	}

	headerLen := int(data[14])
	if headerLen != RoutingHeaderSize {
		return nil, fmt.Errorf("unexpected header length: %d", headerLen)
	}

	offset := 15

	// Parse routing header
	if len(data) < offset+RoutingHeaderSize {
		return nil, fmt.Errorf("frame too short for routing header")
	}
	header, err := ParseRoutingHeader(data[offset : offset+RoutingHeaderSize])
	if err != nil {
		return nil, err
	}
	f.Header = header
	offset += RoutingHeaderSize

	// Parse header HMAC
	if len(data) < offset+16 {
		return nil, fmt.Errorf("frame too short for header HMAC")
	}
	copy(f.HeaderMAC[:], data[offset:offset+16])
	offset += 16

	// The remaining data is payload + optional padding
	remaining := data[offset:]

	// Check for padding
	if f.Flags&FlagHasPadding != 0 {
		if len(remaining) < 2 {
			return nil, fmt.Errorf("frame too short for padding length")
		}
		paddingLen := int(binary.BigEndian.Uint16(remaining[len(remaining)-2:]))
		if paddingLen+2 > len(remaining) {
			return nil, fmt.Errorf("invalid padding length: %d > remaining %d", paddingLen, len(remaining))
		}
		f.Payload = remaining[:len(remaining)-paddingLen-2]
		f.Padding = remaining[len(remaining)-paddingLen-2 : len(remaining)-2]
	} else {
		f.Payload = remaining
	}

	return f, nil
}

// BuildFrame constructs a complete binary frame from components
func BuildFrame(seqNum uint32, timestamp int64, flags byte, header *RoutingHeader, headerMAC [16]byte, encryptedPayload []byte, padding []byte) []byte {
	headerBytes, _ := header.MarshalBinary()

	totalLen := 1 + // version
		1 + // flags
		4 + // seqnum
		8 + // timestamp
		1 + // header len
		RoutingHeaderSize + // routing header
		16 + // header HMAC
		len(encryptedPayload)

	if len(padding) > 0 {
		totalLen += len(padding) + 2 // padding + padding length
	}

	buf := make([]byte, totalLen)
	offset := 0

	buf[0] = FrameVersion
	buf[1] = flags
	binary.BigEndian.PutUint32(buf[2:6], seqNum)
	binary.BigEndian.PutUint64(buf[6:14], uint64(timestamp))
	buf[14] = byte(RoutingHeaderSize)
	offset = 15

	copy(buf[offset:], headerBytes)
	offset += RoutingHeaderSize

	copy(buf[offset:], headerMAC[:])
	offset += 16

	copy(buf[offset:], encryptedPayload)
	offset += len(encryptedPayload)

	if len(padding) > 0 {
		copy(buf[offset:], padding)
		offset += len(padding)
		binary.BigEndian.PutUint16(buf[offset:], uint16(len(padding)))
	}

	return buf
}

// MessageIDToBytes converts a UUID string to 16 bytes
func MessageIDToBytes(id string) [16]byte {
	var result [16]byte
	// UUID format: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx (36 chars)
	if len(id) != 36 {
		// Fallback: hash the string
		copy(result[:], []byte(id))
		return result
	}
	hexStr := id[0:8] + id[9:13] + id[14:18] + id[19:23] + id[24:36]
	for i := 0; i < 16; i++ {
		hi := hexVal(hexStr[i*2])
		lo := hexVal(hexStr[i*2+1])
		result[i] = hi<<4 | lo
	}
	return result
}

// BytesToMessageID converts 16 bytes to UUID string format
func BytesToMessageID(b [16]byte) string {
	return MessageIDToString(b)
}

// MessageIDToString converts 16 bytes back to UUID string format
func MessageIDToString(b [16]byte) string {
	const hex = "0123456789abcdef"
	buf := make([]byte, 36)
	for i := 0; i < 4; i++ {
		buf[i*2] = hex[b[i]>>4]
		buf[i*2+1] = hex[b[i]&0x0f]
	}
	buf[8] = '-'
	for i := 4; i < 6; i++ {
		buf[8+1+(i-4)*2] = hex[b[i]>>4]
		buf[8+2+(i-4)*2] = hex[b[i]&0x0f]
	}
	buf[13] = '-'
	for i := 6; i < 8; i++ {
		buf[13+1+(i-6)*2] = hex[b[i]>>4]
		buf[13+2+(i-6)*2] = hex[b[i]&0x0f]
	}
	buf[18] = '-'
	for i := 8; i < 10; i++ {
		buf[18+1+(i-8)*2] = hex[b[i]>>4]
		buf[18+2+(i-8)*2] = hex[b[i]&0x0f]
	}
	buf[23] = '-'
	for i := 10; i < 16; i++ {
		buf[23+1+(i-10)*2] = hex[b[i]>>4]
		buf[23+2+(i-10)*2] = hex[b[i]&0x0f]
	}
	return string(buf)
}

func hexVal(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10
	default:
		return 0
	}
}
