package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/qoder/device-mgmt/internal/protocol"
)

// PaddingBuckets defines the target message sizes for padding
var PaddingBuckets = []int{128, 256, 512, 1024, 2048, 4096, 8192, 16384}

// MinFrameSize is the minimum frame size (smaller messages are padded to at least this)
const MinFrameSize = 128

// MaxTimestampSkew is the maximum acceptable time difference in seconds
const MaxTimestampSkew = 300

// Session manages the encryption state for a single WebSocket connection
type Session struct {
	keys     *DerivedKeys
	codeMap  *CodeMap
	seqNum   atomic.Uint32
	lastRecv atomic.Uint32
	mu       sync.Mutex
	gcm      cipher.AEAD
}

// NewSession creates a new encryption session from derived keys
func NewSession(keys *DerivedKeys) (*Session, error) {
	block, err := aes.NewCipher(keys.EncryptKey[:])
	if err != nil {
		return nil, fmt.Errorf("create AES cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}

	s := &Session{
		keys:    keys,
		codeMap: BuildCodeMap(keys.CodeMapSeed[:]),
		gcm:     gcm,
	}

	return s, nil
}

// CodeMap returns the session's code map
func (s *Session) CodeMap() *CodeMap {
	return s.codeMap
}

// NextSeqNum returns the next sequence number
func (s *Session) NextSeqNum() uint32 {
	return s.seqNum.Add(1)
}

// buildNonce constructs a 12-byte GCM nonce from the base + seqnum + random
func (s *Session) buildNonce(seqNum uint32) ([]byte, error) {
	nonce := make([]byte, 12)
	copy(nonce[0:4], s.keys.NonceBase[0:4])
	binary.BigEndian.PutUint32(nonce[4:8], seqNum)
	if _, err := rand.Read(nonce[8:12]); err != nil {
		return nil, err
	}
	return nonce, nil
}

// computeHeaderHMAC computes the truncated HMAC for the routing header
func (s *Session) computeHeaderHMAC(headerBytes []byte) [16]byte {
	mac := hmac.New(sha256.New, s.keys.HeaderMACKey[:])
	mac.Write(headerBytes)
	full := mac.Sum(nil)
	var result [16]byte
	copy(result[:], full[:16])
	return result
}

// EncryptFrame encrypts an envelope into a binary frame
func (s *Session) EncryptFrame(env *protocol.Envelope, deviceAlias uint32, refID string) ([]byte, error) {
	seqNum := s.NextSeqNum()
	now := time.Now().UnixMilli()

	// Build the inner payload: the full envelope in JSON (contains real channel/action/payload)
	innerJSON, err := json.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("marshal inner envelope: %w", err)
	}

	// Build routing header
	msgIDBytes := MessageIDToBytes(env.ID)
	var refIDBytes [16]byte
	if refID != "" {
		refIDBytes = MessageIDToBytes(refID)
	}

	channelCode := s.codeMap.GetChannelCode(env.Channel)
	actionCode := s.codeMap.GetActionCode(env.Action)
	typeCode := s.codeMap.GetTypeCode(env.Type)

	header := &RoutingHeader{
		MessageID:   msgIDBytes,
		ReferenceID: refIDBytes,
		ChannelCode: channelCode,
		ActionCode:  actionCode,
		TypeCode:    typeCode,
		DeviceAlias: deviceAlias,
	}

	headerBytes, err := header.MarshalBinary()
	if err != nil {
		return nil, err
	}

	// Compute header HMAC
	headerMAC := s.computeHeaderHMAC(headerBytes)

	// Compute padding BEFORE AAD to determine correct flags.
	// Skip padding for stream-type messages (shell, tunnel) to reduce latency.
	var padding []byte
	if env.Type != protocol.TypeStream {
		estimatedEncPayloadLen := 12 + len(innerJSON) + s.gcm.Overhead()
		frameBodySize := 15 + RoutingHeaderSize + 16 + estimatedEncPayloadLen
		padding = s.computePadding(frameBodySize)
	}

	// Determine flags (must match what BuildFrame will produce)
	flags := byte(0)
	if len(padding) > 0 {
		flags |= FlagHasPadding
	}

	// Build AAD: version + flags + seqnum + timestamp + header (flags must be correct!)
	aad := make([]byte, 0, 1+1+4+8+RoutingHeaderSize)
	aad = append(aad, FrameVersion)
	aad = append(aad, flags)
	aad = binary.BigEndian.AppendUint32(aad, seqNum)
	aad = binary.BigEndian.AppendUint64(aad, uint64(now))
	aad = append(aad, headerBytes...)

	// Encrypt inner payload
	nonce, err := s.buildNonce(seqNum)
	if err != nil {
		return nil, err
	}

	// GCM seal: nonce + encrypted + tag
	sealed := s.gcm.Seal(nil, nonce, innerJSON, aad)

	// Combine nonce + ciphertext
	encryptedPayload := make([]byte, 0, len(nonce)+len(sealed))
	encryptedPayload = append(encryptedPayload, nonce...)
	encryptedPayload = append(encryptedPayload, sealed...)

	// Build final frame (flags already includes FlagHasPadding if needed)
	return BuildFrame(seqNum, now, flags, header, headerMAC, encryptedPayload, padding), nil
}

// DecryptFrame decrypts a binary frame into an envelope
func (s *Session) DecryptFrame(frameData []byte) (env *protocol.Envelope, header *RoutingHeader, isDummy bool, err error) {
	frame, err := ParseFrame(frameData)
	if err != nil {
		return nil, nil, false, fmt.Errorf("parse frame: %w", err)
	}

	// Check version
	if frame.Version != FrameVersion {
		return nil, nil, false, fmt.Errorf("unsupported frame version: %d", frame.Version)
	}

	// Check timestamp skew
	now := time.Now().UnixMilli()
	diff := now - frame.Timestamp
	if diff < 0 {
		diff = -diff
	}
	if diff > MaxTimestampSkew*1000 {
		return nil, nil, false, fmt.Errorf("timestamp skew too large: %dms", diff)
	}

	// Check sequence number (anti-replay) — atomic CAS loop to prevent TOCTOU races
	for {
		lastSeq := s.lastRecv.Load()
		if frame.SeqNum <= lastSeq && lastSeq != 0 {
			return nil, nil, false, fmt.Errorf("replayed sequence number: %d <= %d", frame.SeqNum, lastSeq)
		}
		if s.lastRecv.CompareAndSwap(lastSeq, frame.SeqNum) {
			break
		}
		// Another frame updated lastRecv concurrently; retry
	}

	// Check for dummy
	if frame.Flags&FlagIsDummy != 0 {
		return nil, frame.Header, true, nil
	}

	// Verify header HMAC
	headerBytes, err := frame.Header.MarshalBinary()
	if err != nil {
		return nil, nil, false, err
	}
	expectedMAC := s.computeHeaderHMAC(headerBytes)
	if !hmac.Equal(frame.HeaderMAC[:], expectedMAC[:]) {
		return nil, nil, false, fmt.Errorf("header HMAC mismatch")
	}

	// Reconstruct AAD
	aad := make([]byte, 0, 1+1+4+8+RoutingHeaderSize)
	aad = append(aad, frame.Version)
	aad = append(aad, frame.Flags)
	aad = binary.BigEndian.AppendUint32(aad, frame.SeqNum)
	aad = binary.BigEndian.AppendUint64(aad, uint64(frame.Timestamp))
	aad = append(aad, headerBytes...)

	// Decrypt: first 12 bytes are nonce, rest is ciphertext+tag
	if len(frame.Payload) < 12+s.gcm.Overhead() {
		return nil, nil, false, fmt.Errorf("encrypted payload too short")
	}

	nonce := frame.Payload[:12]
	ciphertext := frame.Payload[12:]

	plainJSON, err := s.gcm.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return nil, nil, false, fmt.Errorf("GCM decrypt: %w", err)
	}

	// Unmarshal the inner envelope
	var envelope protocol.Envelope
	if err := json.Unmarshal(plainJSON, &envelope); err != nil {
		return nil, nil, false, fmt.Errorf("unmarshal inner envelope: %w", err)
	}

	return &envelope, frame.Header, false, nil
}

// EncryptDummy creates a dummy frame for traffic obfuscation
func (s *Session) EncryptDummy() ([]byte, error) {
	seqNum := s.NextSeqNum()
	now := time.Now().UnixMilli()

	// Random inner payload
	dummyData := make([]byte, 64)
	rand.Read(dummyData)

	// Random header
	var msgID [16]byte
	rand.Read(msgID[:])

	channelCode := s.codeMap.GetChannelCode("system")
	actionCode := s.codeMap.GetActionCode("system.heartbeat")
	header := &RoutingHeader{
		MessageID:   msgID,
		ChannelCode: channelCode,
		ActionCode:  actionCode,
		TypeCode:    TypeCodeEvent,
	}

	headerBytes, _ := header.MarshalBinary()
	headerMAC := s.computeHeaderHMAC(headerBytes)

	// Compute padding BEFORE AAD to determine correct flags
	estimatedEncPayloadLen := 12 + len(dummyData) + s.gcm.Overhead()
	frameBodySize := 15 + RoutingHeaderSize + 16 + estimatedEncPayloadLen
	padding := s.computePadding(frameBodySize)

	flags := byte(FlagIsDummy)
	if len(padding) > 0 {
		flags |= FlagHasPadding
	}

	aad := make([]byte, 0, 1+1+4+8+RoutingHeaderSize)
	aad = append(aad, FrameVersion)
	aad = append(aad, flags)
	aad = binary.BigEndian.AppendUint32(aad, seqNum)
	aad = binary.BigEndian.AppendUint64(aad, uint64(now))
	aad = append(aad, headerBytes...)

	nonce, err := s.buildNonce(seqNum)
	if err != nil {
		return nil, err
	}

	sealed := s.gcm.Seal(nil, nonce, dummyData, aad)
	encryptedPayload := append(nonce, sealed...)

	return BuildFrame(seqNum, now, flags, header, headerMAC, encryptedPayload, padding), nil
}

// IsDummyFrame checks if a raw binary frame has the dummy flag set
func IsDummyFrame(data []byte) bool {
	if len(data) < 2 {
		return false
	}
	return data[1]&FlagIsDummy != 0
}

// computePadding calculates padding bytes to fill to the nearest bucket
func (s *Session) computePadding(currentSize int) []byte {
	if currentSize >= PaddingBuckets[len(PaddingBuckets)-1] {
		return nil
	}

	// Find the smallest bucket that fits
	targetSize := PaddingBuckets[len(PaddingBuckets)-1]
	for _, bucket := range PaddingBuckets {
		if bucket >= currentSize+2 { // +2 for padding length field
			targetSize = bucket
			break
		}
	}

	paddingLen := targetSize - currentSize - 2 // -2 for length field
	if paddingLen <= 0 {
		return nil
	}

	padding := make([]byte, paddingLen)
	rand.Read(padding)
	return padding
}
