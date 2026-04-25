package crypto

import (
	"crypto/rand"
	"encoding/binary"
	"sort"
	"sync"

	"github.com/qoder/device-mgmt/internal/protocol"
)

// All known channels and actions that must be mapped
var allChannels = []string{
	protocol.ChannelSystem,
	protocol.ChannelShell,
	protocol.ChannelFile,
	protocol.ChannelProcess,
	protocol.ChannelTunnel,
	protocol.ChannelAudit,
	"ping", // special channel for keepalive
}

var allActions = []string{
	// System
	protocol.ActionSystemInfo,
	protocol.ActionSystemHeartbeat,
	protocol.ActionSystemRestart,
	protocol.ActionSystemUpgrade,
	protocol.ActionAuthChallenge,
	protocol.ActionAuthResponse,
	protocol.ActionDeviceOnline,
	protocol.ActionDeviceOffline,
	protocol.ActionSystemReconnect,
	// Shell
	protocol.ActionShellStart,
	protocol.ActionShellInput,
	protocol.ActionShellResize,
	protocol.ActionShellOutput,
	protocol.ActionShellClose,
	// File
	protocol.ActionFileBrowse,
	protocol.ActionFileStat,
	protocol.ActionFileUploadStart,
	protocol.ActionFileUploadChunk,
	protocol.ActionFileUploadAck,
	protocol.ActionFileUploadDone,
	protocol.ActionFileDownloadStart,
	protocol.ActionFileDownloadChunk,
	protocol.ActionFileDownloadAck,
	protocol.ActionFileDownloadDone,
	protocol.ActionFileRead,
	protocol.ActionFileWrite,
	protocol.ActionFileDelete,
	protocol.ActionFileMkdir,
	protocol.ActionFileMove,
	protocol.ActionFileChmod,
	protocol.ActionFileSearch,
	protocol.ActionFileProgress,
	protocol.ActionFileDownloadDir,
	protocol.ActionFileCreate,
	// Process
	protocol.ActionProcessList,
	protocol.ActionProcessKill,
	protocol.ActionProcessTop,
	// Tunnel
	protocol.ActionTunnelOpen,
	protocol.ActionTunnelClose,
	protocol.ActionTunnelData,
	protocol.ActionTunnelConnClose,
	protocol.ActionTunnelAck,
	protocol.ActionTunnelStatus,
	protocol.ActionSocks5Open,
	protocol.ActionSocks5Data,
	protocol.ActionSocks5Close,
	// Special
	"ping",
	"pong",
}

// Message type codes
const (
	TypeCodeRequest  uint8 = 0x01
	TypeCodeResponse uint8 = 0x02
	TypeCodeStream   uint8 = 0x03
	TypeCodeEvent    uint8 = 0x04
	TypeCodePing     uint8 = 0x05
	TypeCodePong     uint8 = 0x06
)

// CodeMap provides bidirectional mapping between strings and uint16 codes
type CodeMap struct {
	mu            sync.RWMutex
	channelToCode map[string]uint16
	codeToChannel map[uint16]string
	actionToCode  map[string]uint16
	codeToAction  map[uint16]string
	typeToCode    map[string]uint8
	codeToType    map[uint8]string
}

// BuildCodeMap creates a deterministic but opaque code mapping from a seed
func BuildCodeMap(seed []byte) *CodeMap {
	cm := &CodeMap{
		channelToCode: make(map[string]uint16),
		codeToChannel: make(map[uint16]string),
		actionToCode:  make(map[string]uint16),
		codeToAction:  make(map[uint16]string),
		typeToCode:    make(map[string]uint8),
		codeToType:    make(map[uint8]string),
	}

	// Use a deterministic PRNG seeded from the cryptographic seed
	// We use HMAC-DRNG style: hash seed to get initial state, then derive values
	state := make([]byte, 32)
	copy(state, seed)

	// Map channels
	cm.assignCodes(state, allChannels, cm.channelToCode, cm.codeToChannel)
	cm.assignCodes(state, allActions, cm.actionToCode, cm.codeToAction)

	// Map message types (deterministic, not seeded — these are few and well-known)
	typeMappings := []struct {
		s string
		c uint8
	}{
		{protocol.TypeRequest, TypeCodeRequest},
		{protocol.TypeResponse, TypeCodeResponse},
		{protocol.TypeStream, TypeCodeStream},
		{protocol.TypeEvent, TypeCodeEvent},
	}
	for _, m := range typeMappings {
		cm.typeToCode[m.s] = m.c
		cm.codeToType[m.c] = m.s
	}

	return cm
}

// advanceState updates the PRNG state
func advanceState(state []byte) {
	// Simple state advancement using XOR with incrementing counter
	for i := 0; i < len(state); i++ {
		state[i] ^= state[(i+7)%len(state)] + byte(i)
	}
}

// assignCodes assigns unique uint16 codes to a list of strings
func (cm *CodeMap) assignCodes(state []byte, items []string, toCode map[string]uint16, fromCode map[uint16]string) {
	usedCodes := make(map[uint16]bool)
	for _, item := range items {
		code := cm.deriveCode(state, usedCodes)
		toCode[item] = code
		fromCode[code] = item
		usedCodes[code] = true
		advanceState(state)
	}
}

// deriveCode derives a unique uint16 code from the state
func (cm *CodeMap) deriveCode(state []byte, used map[uint16]bool) uint16 {
	// Mix state bytes into a uint16
	for {
		code := binary.BigEndian.Uint16(state[0:2]) ^ binary.BigEndian.Uint16(state[16:18])
		// Ensure non-zero
		if code == 0 {
			advanceState(state)
			continue
		}
		if !used[code] {
			return code
		}
		advanceState(state)
	}
}

// GetChannelCode returns the uint16 code for a channel string
func (cm *CodeMap) GetChannelCode(channel string) uint16 {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.channelToCode[channel]
}

// ResolveChannelCode returns the channel string for a uint16 code
func (cm *CodeMap) ResolveChannelCode(code uint16) string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.codeToChannel[code]
}

// GetActionCode returns the uint16 code for an action string
func (cm *CodeMap) GetActionCode(action string) uint16 {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.actionToCode[action]
}

// ResolveActionCode returns the action string for a uint16 code
func (cm *CodeMap) ResolveActionCode(code uint16) string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.codeToAction[code]
}

// GetTypeCode returns the uint8 code for a message type string
func (cm *CodeMap) GetTypeCode(typeStr string) uint8 {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	if c, ok := cm.typeToCode[typeStr]; ok {
		return c
	}
	return 0
}

// ResolveTypeCode returns the message type string for a uint8 code
func (cm *CodeMap) ResolveTypeCode(code uint8) string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.codeToType[code]
}

// GenerateDeviceAlias generates a random 4-byte device alias
func GenerateDeviceAlias() uint32 {
	b := make([]byte, 4)
	rand.Read(b)
	return binary.BigEndian.Uint32(b)
}

// AllKnownChannels returns all channel strings used in the system
func AllKnownChannels() []string {
	return append([]string{}, allChannels...)
}

// AllKnownActions returns all action strings used in the system
func AllKnownActions() []string {
	return append([]string{}, allActions...)
}

// SortedByCode returns items sorted by their code values (for deterministic iteration)
func SortedByCode(items []string, toCode map[string]uint16) []string {
	sorted := make([]string, len(items))
	copy(sorted, items)
	sort.Slice(sorted, func(i, j int) bool {
		return toCode[sorted[i]] < toCode[sorted[j]]
	})
	return sorted
}
