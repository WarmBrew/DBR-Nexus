package server

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"crypto/subtle"

	"github.com/gorilla/websocket"
	svcrypto "github.com/qoder/device-mgmt/internal/crypto"
	"github.com/qoder/device-mgmt/internal/protocol"
	"go.uber.org/zap"
)

// downloadChunk carries a single file download chunk from agent to HTTP handler
type downloadChunk struct {
	Action     string // "file.download.chunk" or "file.download.done"
	TransferID string
	Data       string // base64 encoded, empty for "done"
}

// AgentConn represents a connected agent
type AgentConn struct {
	DeviceID    string
	Conn        *websocket.Conn
	Send        chan *protocol.Envelope
	RawSend     chan []byte // for Obfuscator dummy frames (concurrent-safe)
	Hub         *Hub
	Session     *svcrypto.Session // nil = cleartext mode (backward compat)
	DeviceAlias uint32            // 4-byte routing alias
	Obfuscator  *svcrypto.Obfuscator
	closeOnce   sync.Once // protects Send from double close
}

// CloseSend safely closes the Send and RawSend channels (idempotent)
func (ac *AgentConn) CloseSend() {
	ac.closeOnce.Do(func() {
		close(ac.Send)
		close(ac.RawSend)
	})
}

// BrowserConn represents a connected browser client
type BrowserConn struct {
	ID           string
	UserID       string
	Username     string
	Role         string
	Conn         *websocket.Conn
	Send         chan *protocol.Envelope
	Hub          *Hub
	Watching     map[string]bool   // device_ids being watched
	WatchMu      sync.RWMutex      // protects Watching map
	Session      *svcrypto.Session // nil = cleartext mode
	BrowserAlias uint32
	sendMu       sync.Mutex // protects Send channel from concurrent close+send
	sendClosed   bool       // true after CloseSend
	closeOnce    sync.Once  // protects CloseSend from double close
}

// CloseSend safely closes the Send channel (idempotent)
func (bc *BrowserConn) CloseSend() {
	bc.closeOnce.Do(func() {
		bc.sendMu.Lock()
		bc.sendClosed = true
		close(bc.Send)
		bc.sendMu.Unlock()
	})
}

// SafeSend sends an envelope to the browser, returning false if the connection is closed or buffer full.
// This is safe to call concurrently and will never panic on a closed channel.
func (bc *BrowserConn) SafeSend(env *protocol.Envelope) bool {
	bc.sendMu.Lock()
	if bc.sendClosed {
		bc.sendMu.Unlock()
		return false
	}
	select {
	case bc.Send <- env:
		bc.sendMu.Unlock()
		return true
	default:
		bc.sendMu.Unlock()
		return false
	}
}

// Hub maintains the set of active agent and browser connections
type Hub struct {
	// Agent connections: device_id -> *AgentConn
	agents sync.Map

	// Browser connections: connection_id -> *BrowserConn
	browsers sync.Map

	// Pending requests: envelope_id -> channel for response
	pendingRequests sync.Map

	// HTTP download streams: transfer_id -> channel of base64 chunks
	downloadStreams sync.Map

	// Device alias to device ID mapping
	deviceAliases sync.Map // uint32 alias -> deviceID string

	// Logger
	logger *zap.Logger

	// Callbacks for agent connect/disconnect
	OnAgentConnect      func(deviceID string, conn *AgentConn)
	OnAgentDisconnect   func(deviceID string)
	BeforeAgentRegister func(deviceID string) // called before new agent is stored (for server cleanup)
}

// NewHub creates a new Hub
func NewHub(logger *zap.Logger) *Hub {
	return &Hub{
		logger: logger,
	}
}

// RegisterAgent registers an agent connection
func (h *Hub) RegisterAgent(conn *AgentConn) {
	// Clean up stale connections/listeners from previous agent session.
	// This handles the race where a new agent connects before the old
	// agent's UnregisterAgent runs.
	h.CleanupDeviceConns(conn.DeviceID)
	if h.BeforeAgentRegister != nil {
		h.BeforeAgentRegister(conn.DeviceID)
	}

	// Store device alias mapping
	if conn.DeviceAlias != 0 {
		h.deviceAliases.Store(conn.DeviceAlias, conn.DeviceID)
	}

	// If same device already connected, close the old connection first
	if old, loaded := h.agents.LoadOrStore(conn.DeviceID, conn); loaded {
		oldConn := old.(*AgentConn)
		h.logger.Info("replacing existing agent connection", zap.String("device_id", conn.DeviceID))
		oldConn.CloseSend()
		oldConn.Conn.Close()
		// Clean up old device alias
		if oldConn.DeviceAlias != 0 {
			h.deviceAliases.Delete(oldConn.DeviceAlias)
		}
		if oldConn.Obfuscator != nil {
			oldConn.Obfuscator.Stop()
		}
	}

	h.logger.Info("agent registered", zap.String("device_id", conn.DeviceID),
		zap.Bool("encrypted", conn.Session != nil))

	if h.OnAgentConnect != nil {
		h.OnAgentConnect(conn.DeviceID, conn)
	}

	go h.readAgentPump(conn)
}

// UnregisterAgent removes an agent connection
func (h *Hub) UnregisterAgent(deviceID string, conn *AgentConn) {
	// Only delete if the stored agent is the exact same connection we're unregistering.
	// This prevents deleting a replacement agent during reconnection races.
	if stored, ok := h.agents.Load(deviceID); ok {
		if stored.(*AgentConn) != conn {
			h.logger.Debug("skipping unregister for replaced agent", zap.String("device_id", deviceID))
			return
		}
		h.agents.Delete(deviceID)
		conn.Conn.Close()
		// Clean up device alias
		if conn.DeviceAlias != 0 {
			h.deviceAliases.Delete(conn.DeviceAlias)
		}
		if conn.Obfuscator != nil {
			conn.Obfuscator.Stop()
		}
		h.logger.Info("agent unregistered", zap.String("device_id", deviceID))
	} else {
		return
	}

	h.pendingRequests.Range(func(key, value interface{}) bool {
		keyStr := key.(string)
		if strings.HasPrefix(keyStr, deviceID+":") {
			ch := value.(chan *protocol.Envelope)
			select {
			case ch <- &protocol.Envelope{
				Error: &protocol.ProtocolError{Code: 503, Message: "device disconnected"},
			}:
			default:
			}
			h.pendingRequests.Delete(key)
		}
		return true
	})

	if h.OnAgentDisconnect != nil {
		h.OnAgentDisconnect(deviceID)
	}
}

// RegisterBrowser registers a browser connection
func (h *Hub) RegisterBrowser(conn *BrowserConn) {
	h.browsers.Store(conn.ID, conn)
	h.logger.Info("browser registered",
		zap.String("conn_id", conn.ID),
		zap.String("user", conn.Username),
		zap.Bool("encrypted", conn.Session != nil))

	go h.readBrowserPump(conn)
}

// UnregisterBrowser removes a browser connection
func (h *Hub) UnregisterBrowser(connID string) {
	if conn, ok := h.browsers.LoadAndDelete(connID); ok {
		browserConn := conn.(*BrowserConn)
		browserConn.Conn.Close()
		h.logger.Info("browser unregistered", zap.String("conn_id", connID))
	}
}

// CleanupDeviceConns closes all SOCKS5 and tunnel TCP connections for a device.
// Uses LoadAndDelete so it runs exactly once even if called from multiple places
// (RegisterAgent and onAgentDisconnect).
func (h *Hub) CleanupDeviceConns(deviceID string) {
	// Close all SOCKS5 connections for this device
	if conns, ok := socks5DeviceConns.LoadAndDelete(deviceID); ok {
		conns.(*sync.Map).Range(func(key, value interface{}) bool {
			connID := key.(string)
			conn := value.(net.Conn)
			socks5Conns.Delete(connID)
			socks5ConnTunnel.Delete(connID)
			conn.Close()
			return true
		})
	}
	// Close all tunnel connections for this device
	if conns, ok := tunnelDeviceConns.LoadAndDelete(deviceID); ok {
		conns.(*sync.Map).Range(func(key, value interface{}) bool {
			connKey := key.(string)
			conn := value.(net.Conn)
			tunnelConns.Delete(connKey)
			conn.Close()
			return true
		})
	}
}

// SendToDevice sends an envelope to a specific agent
func (h *Hub) SendToDevice(deviceID string, env *protocol.Envelope) error {
	conn, ok := h.agents.Load(deviceID)
	if !ok {
		return ErrDeviceNotFound
	}

	agentConn := conn.(*AgentConn)
	select {
	case agentConn.Send <- env:
		return nil
	default:
		return ErrSendBufferFull
	}
}

// RequestToDevice sends a request and waits for response
func (h *Hub) RequestToDevice(deviceID string, env *protocol.Envelope, timeout time.Duration) (*protocol.Envelope, error) {
	// Register pending BEFORE sending to avoid TOCTOU race
	pendingKey := deviceID + ":" + env.ID
	respCh := make(chan *protocol.Envelope, 1)
	h.pendingRequests.Store(pendingKey, respCh)
	defer h.pendingRequests.Delete(pendingKey)

	if err := h.SendToDevice(deviceID, env); err != nil {
		return nil, err
	}

	select {
	case resp := <-respCh:
		return resp, nil
	case <-time.After(timeout):
		return nil, ErrRequestTimeout
	}
}

// BroadcastToWatchers sends an event to all browsers watching a device
func (h *Hub) BroadcastToWatchers(deviceID string, env *protocol.Envelope) {
	h.browsers.Range(func(key, value interface{}) bool {
		browserConn := value.(*BrowserConn)
		browserConn.WatchMu.RLock()
		watching := browserConn.Watching[deviceID]
		browserConn.WatchMu.RUnlock()
		if watching {
			browserConn.SafeSend(env)
		}
		return true
	})
}

// BroadcastEvent sends an event to all connected browsers
func (h *Hub) BroadcastEvent(env *protocol.Envelope) {
	h.browsers.Range(func(key, value interface{}) bool {
		browserConn := value.(*BrowserConn)
		browserConn.SafeSend(env)
		return true
	})
}

// GetAgent returns an agent connection by device ID
func (h *Hub) GetAgent(deviceID string) (*AgentConn, bool) {
	conn, ok := h.agents.Load(deviceID)
	if !ok {
		return nil, false
	}
	return conn.(*AgentConn), true
}

// IsDeviceOnline checks if a device is currently connected
func (h *Hub) IsDeviceOnline(deviceID string) bool {
	_, ok := h.agents.Load(deviceID)
	return ok
}

// ResolveDeviceAlias maps a device alias back to deviceID
func (h *Hub) ResolveDeviceAlias(alias uint32) (string, bool) {
	v, ok := h.deviceAliases.Load(alias)
	if !ok {
		return "", false
	}
	return v.(string), true
}

// --- Write Pumps ---

func writeAgentPump(conn *AgentConn) {
	defer conn.Conn.Close()
	for {
		// Priority: real messages over dummy frames
		// Try real messages first (non-blocking), then fall back to select
		select {
		case msg, ok := <-conn.Send:
			if !ok {
				return
			}
			if err := writeAgentMsg(conn, msg); err != nil {
				return
			}
			continue
		default:
		}

		select {
		case msg, ok := <-conn.Send:
			if !ok {
				return
			}
			if err := writeAgentMsg(conn, msg); err != nil {
				return
			}
		case rawFrame, ok := <-conn.RawSend:
			if !ok {
				return
			}
			if err := conn.Conn.WriteMessage(websocket.BinaryMessage, rawFrame); err != nil {
				return
			}
		}
	}
}

func writeAgentMsg(conn *AgentConn, msg *protocol.Envelope) error {
	if conn.Session != nil {
		frame, err := conn.Session.EncryptFrame(msg, conn.DeviceAlias, "")
		if err != nil {
			conn.Hub.logger.Warn("agent encrypt error", zap.Error(err))
			return nil
		}
		return conn.Conn.WriteMessage(websocket.BinaryMessage, frame)
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return nil
	}
	return conn.Conn.WriteMessage(websocket.TextMessage, data)
}

func writeBrowserPump(conn *BrowserConn) {
	defer conn.Conn.Close()
	for msg := range conn.Send {
		// Special handling for pong messages
		if msg.ID == "__pong__" && msg.Type == "pong" && msg.Payload != nil {
			if conn.Session != nil {
				// Encrypted pong: send as encrypted frame
				frame, err := conn.Session.EncryptFrame(msg, 0, "")
				if err != nil {
					continue
				}
				if err := conn.Conn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
					return
				}
			} else {
				if err := conn.Conn.WriteMessage(websocket.TextMessage, msg.Payload); err != nil {
					return
				}
			}
			continue
		}

		if conn.Session != nil {
			frame, err := conn.Session.EncryptFrame(msg, 0, "")
			if err != nil {
				conn.Hub.logger.Warn("browser encrypt error", zap.Error(err))
				continue
			}
			if err := conn.Conn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
				return
			}
		} else {
			data, err := json.Marshal(msg)
			if err != nil {
				continue
			}
			if err := conn.Conn.WriteMessage(websocket.TextMessage, data); err != nil {
				return
			}
		}
	}
}

// --- Read Pumps ---

func (h *Hub) readAgentPump(conn *AgentConn) {
	defer func() {
		conn.CloseSend()
		h.UnregisterAgent(conn.DeviceID, conn)
	}()

	go writeAgentPump(conn)

	for {
		msgType, message, err := conn.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				h.logger.Error("agent read error", zap.String("device_id", conn.DeviceID), zap.Error(err))
			}
			return
		}

		if conn.Session != nil && msgType == websocket.BinaryMessage {
			// Encrypted binary frame
			env, _, isDummy, err := conn.Session.DecryptFrame(message)
			if err != nil {
				h.logger.Warn("agent decrypt error", zap.String("device_id", conn.DeviceID), zap.Error(err))
				continue
			}
			if isDummy {
				continue // discard dummy traffic
			}
			if env != nil {
				h.handleAgentMessage(conn, env)
			}
		} else {
			// Cleartext JSON (backward compat or text message)
			var env protocol.Envelope
			if err := json.Unmarshal(message, &env); err != nil {
				h.logger.Warn("invalid message from agent", zap.Error(err))
				continue
			}
			h.handleAgentMessage(conn, &env)
		}
	}
}

func (h *Hub) readBrowserPump(conn *BrowserConn) {
	defer func() {
		conn.CloseSend()
		h.UnregisterBrowser(conn.ID)
	}()

	go writeBrowserPump(conn)

	for {
		msgType, message, err := conn.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				h.logger.Error("browser read error", zap.String("conn_id", conn.ID), zap.Error(err))
			}
			return
		}

		if conn.Session != nil && msgType == websocket.BinaryMessage {
			// Encrypted binary frame
			env, _, isDummy, err := conn.Session.DecryptFrame(message)
			if err != nil {
				h.logger.Warn("browser decrypt error", zap.String("conn_id", conn.ID), zap.Error(err))
				continue
			}
			if isDummy {
				continue
			}
			if env != nil {
				// Handle encrypted ping
				if env.Type == "request" && env.Channel == "ping" {
					var pingPayload struct {
						PingTs int64 `json:"ping_ts"`
					}
					env.DecodePayload(&pingPayload)
					pongEnv := &protocol.Envelope{
						ID:        env.ID,
						Type:      "response",
						Channel:   "ping",
						Action:    "pong",
						Payload:   json.RawMessage(fmt.Sprintf(`{"ping_ts":%d}`, pingPayload.PingTs)),
						Timestamp: time.Now().UnixMilli(),
					}
					conn.SafeSend(pongEnv)
					continue
				}
				h.handleBrowserMessage(conn, env)
			}
		} else {
			// Cleartext JSON
			var env protocol.Envelope
			if err := json.Unmarshal(message, &env); err != nil {
				h.logger.Warn("invalid message from browser", zap.Error(err))
				continue
			}

			// Handle application-level ping
			if env.Type == "ping" && env.ID == "" && env.Action == "" {
				var raw struct {
					Type   string `json:"type"`
					PingTs int64  `json:"ping_ts"`
				}
				if err2 := json.Unmarshal(message, &raw); err2 == nil && raw.Type == "ping" {
					pong, _ := json.Marshal(map[string]interface{}{"type": "pong", "ping_ts": raw.PingTs})
					pongEnv := &protocol.Envelope{
						ID:        "__pong__",
						Type:      "pong",
						Timestamp: time.Now().UnixMilli(),
						Payload:   json.RawMessage(pong),
					}
					conn.SafeSend(pongEnv)
				}
				continue
			}

			h.handleBrowserMessage(conn, &env)
		}
	}
}

// --- Message Handlers (unchanged logic, just pass Envelope) ---

func (h *Hub) handleAgentMessage(conn *AgentConn, env *protocol.Envelope) {
	switch env.Type {
	case protocol.TypeResponse:
		h.pendingRequests.Range(func(key, value interface{}) bool {
			keyStr := key.(string)
			if strings.HasSuffix(keyStr, ":"+env.ID) && strings.HasPrefix(keyStr, conn.DeviceID+":") {
				h.pendingRequests.Delete(keyStr)
				respCh := value.(chan *protocol.Envelope)
				select {
				case respCh <- env:
				default:
				}
				return false
			}
			return true
		})
		h.BroadcastToWatchers(conn.DeviceID, env)

	case protocol.TypeStream:
		if env.Action == protocol.ActionSocks5Data {
			var payload protocol.Socks5DataPayload
			if env.DecodePayload(&payload) == nil {
				decoded, err := base64.StdEncoding.DecodeString(payload.Data)
				if err == nil && len(decoded) > 0 {
					if rawConn, ok := socks5Conns.Load(payload.ConnID); ok {
						n, _ := rawConn.(net.Conn).Write(decoded)
						// Record downlink traffic for SOCKS5 tunnel
						if tunnelID, ok := socks5ConnTunnel.Load(payload.ConnID); ok {
							AddTunnelBytes(tunnelID.(string), 0, int64(n))
						}
					}
				}
			}
			return
		}
		if env.Action == protocol.ActionTunnelData {
			var payload protocol.TunnelDataPayload
			if env.DecodePayload(&payload) == nil {
				connKey := payload.TunnelID + ":" + payload.ConnID
				if rawConn, ok := tunnelConns.Load(connKey); ok {
					decoded, err := base64.StdEncoding.DecodeString(payload.Data)
					if err == nil {
						n, _ := rawConn.(net.Conn).Write(decoded)
						AddTunnelBytes(payload.TunnelID, 0, int64(n))
					}
				} else {
					h.logger.Debug("tunnel data dropped: connection not found",
						zap.String("tunnel_id", payload.TunnelID),
						zap.String("conn_id", payload.ConnID))
				}
			}
			return
		}
		if env.Action == protocol.ActionFileDownloadChunk || env.Action == protocol.ActionFileDownloadDone {
			var chunkPayload struct {
				TransferID string `json:"transfer_id"`
				Data       string `json:"data,omitempty"`
			}
			if env.DecodePayload(&chunkPayload) == nil && chunkPayload.TransferID != "" {
				if ch, ok := h.downloadStreams.Load(chunkPayload.TransferID); ok {
					select {
					case ch.(chan downloadChunk) <- downloadChunk{
						Action:     env.Action,
						TransferID: chunkPayload.TransferID,
						Data:       chunkPayload.Data,
					}:
					default:
						h.logger.Warn("download stream buffer full, dropping chunk",
							zap.String("transfer_id", chunkPayload.TransferID))
					}
				}
			}
		}
		h.forwardToAllBrowsers(env)

	case protocol.TypeEvent:
		if env.Action == protocol.ActionFileDownloadDone {
			var donePayload struct {
				TransferID string `json:"transfer_id"`
			}
			if env.DecodePayload(&donePayload) == nil && donePayload.TransferID != "" {
				if ch, ok := h.downloadStreams.Load(donePayload.TransferID); ok {
					select {
					case ch.(chan downloadChunk) <- downloadChunk{
						Action:     env.Action,
						TransferID: donePayload.TransferID,
					}:
					default:
					}
				}
			}
		}
		h.BroadcastEvent(env)
	}
}

func (h *Hub) forwardToAllBrowsers(env *protocol.Envelope) {
	h.browsers.Range(func(key, value interface{}) bool {
		browserConn := value.(*BrowserConn)
		if !browserConn.SafeSend(env) {
			h.logger.Debug("browser send skipped, connection closed or buffer full",
				zap.String("conn_id", browserConn.ID))
		}
		return true
	})
}

func (h *Hub) RegisterDownloadStream(transferID string, ch chan downloadChunk) {
	h.downloadStreams.Store(transferID, ch)
}

func (h *Hub) UnregisterDownloadStream(transferID string) {
	h.downloadStreams.Delete(transferID)
}

func (h *Hub) handleBrowserMessage(conn *BrowserConn, env *protocol.Envelope) {
	var payload struct {
		DeviceID string `json:"device_id"`
	}
	if err := env.DecodePayload(&payload); err == nil && payload.DeviceID != "" {
		needsResponse := env.Type == protocol.TypeRequest &&
			env.Action != protocol.ActionShellInput &&
			env.Action != protocol.ActionShellResize &&
			env.Action != protocol.ActionTunnelData

		// If a response is needed, register the pending channel BEFORE sending
		// to avoid the TOCTOU race where the response arrives before we register.
		if needsResponse {
			pendingKey := payload.DeviceID + ":" + env.ID
			respCh := make(chan *protocol.Envelope, 1)
			h.pendingRequests.Store(pendingKey, respCh)

			if err := h.SendToDevice(payload.DeviceID, env); err != nil {
				// Send failed: clean up pending and notify browser
				h.pendingRequests.Delete(pendingKey)
				errResp := protocol.NewErrorResponse(env, 503, err.Error())
				conn.SafeSend(errResp)
				return
			}

			go func() {
				select {
				case resp := <-respCh:
					conn.SafeSend(resp)
				case <-time.After(30 * time.Second):
					h.pendingRequests.Delete(pendingKey)
					errResp := protocol.NewErrorResponse(env, 504, "request timeout")
					conn.SafeSend(errResp)
				}
			}()
		} else {
			// Fire-and-forget: no response needed
			if err := h.SendToDevice(payload.DeviceID, env); err != nil {
				errResp := protocol.NewErrorResponse(env, 503, err.Error())
				conn.SafeSend(errResp)
			}
		}
	}
}

// Hub errors
var (
	ErrDeviceNotFound = &hubError{code: 404, message: "device not found or offline"}
	ErrSendBufferFull = &hubError{code: 503, message: "send buffer full"}
	ErrRequestTimeout = &hubError{code: 504, message: "request timeout"}
)

// socks5Conns tracks active SOCKS5 connections for routing data back
var socks5Conns sync.Map // connID -> net.Conn

// socks5ConnTunnel maps SOCKS5 connID to tunnelID for traffic statistics
var socks5ConnTunnel sync.Map // connID -> tunnelID

// socks5DeviceConns tracks SOCKS5 connections per device for cleanup on disconnect
var socks5DeviceConns sync.Map // deviceID -> *sync.Map (connID -> net.Conn)

// tunnelDeviceConns tracks tunnel connections per device for cleanup on disconnect
var tunnelDeviceConns sync.Map // deviceID -> *sync.Map (connKey -> net.Conn)

type hubError struct {
	code    int
	message string
}

func (e *hubError) Error() string {
	return e.message
}

// ServeSocks5Listener accepts TCP connections and handles SOCKS5 protocol,
// forwarding each connection to the agent as a dynamic tunnel
func (h *Hub) ServeSocks5Listener(tunnelID, deviceID string, ln net.Listener, socks5User, socks5Pass, allowedIPs string) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			h.logger.Debug("socks5 listener accept error", zap.String("tunnel_id", tunnelID), zap.Error(err))
			return
		}
		h.logger.Info("socks5: new connection", zap.String("remote", conn.RemoteAddr().String()))

		if !isAllowedIP(conn.RemoteAddr(), allowedIPs) {
			h.logger.Warn("socks5: connection rejected - IP not in allowed list",
				zap.String("remote", conn.RemoteAddr().String()),
				zap.String("allowed_ips", allowedIPs))
			conn.Close()
			continue
		}

		go h.handleSocks5Connection(tunnelID, deviceID, conn, socks5User, socks5Pass)
	}
}

func (h *Hub) handleSocks5Connection(tunnelID, deviceID string, conn net.Conn, socks5User, socks5Pass string) {
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(30 * time.Second))

	buf := make([]byte, 512)

	n, err := conn.Read(buf)
	if err != nil || n < 2 || buf[0] != 0x05 {
		h.logger.Debug("socks5: invalid greeting", zap.Error(err), zap.Int("n", n))
		return
	}

	requireAuth := socks5User != ""
	clientSupportsAuth := false

	if requireAuth {
		numMethods := int(buf[1])
		for i := 0; i < numMethods && i+2 < n; i++ {
			if buf[2+i] == 0x02 {
				clientSupportsAuth = true
				break
			}
		}
		if !clientSupportsAuth {
			h.logger.Warn("socks5: client does not support auth")
			conn.Write([]byte{0x05, 0xFF})
			return
		}
		conn.Write([]byte{0x05, 0x02})

		authBuf := make([]byte, 520)
		n, err = conn.Read(authBuf)
		if err != nil || n < 2 || authBuf[0] != 0x01 {
			h.logger.Debug("socks5: invalid auth sub-negotiation", zap.Error(err))
			return
		}
		uLen := int(authBuf[1])
		if uLen > 255 || n < 2+uLen+1 {
			h.logger.Debug("socks5: invalid auth username length", zap.Int("n", n), zap.Int("uLen", uLen))
			return
		}
		username := string(authBuf[2 : 2+uLen])
		pLen := int(authBuf[2+uLen])
		if pLen > 255 || n < 2+uLen+1+pLen {
			h.logger.Debug("socks5: invalid auth password length", zap.Int("n", n), zap.Int("pLen", pLen))
			return
		}
		password := string(authBuf[2+uLen+1 : 2+uLen+1+pLen])

		if subtle.ConstantTimeCompare([]byte(username), []byte(socks5User)) != 1 ||
			subtle.ConstantTimeCompare([]byte(password), []byte(socks5Pass)) != 1 {
			h.logger.Warn("socks5: auth failed", zap.String("username", username))
			conn.Write([]byte{0x01, 0x01})
			return
		}
		conn.Write([]byte{0x01, 0x00})
		h.logger.Info("socks5: auth succeeded", zap.String("username", socks5User))
	} else {
		conn.Write([]byte{0x05, 0x00})
	}

	n, err = conn.Read(buf)
	if err != nil || n < 7 || buf[1] != 0x01 {
		h.logger.Debug("socks5: invalid CONNECT request", zap.Error(err), zap.Int("n", n))
		conn.Write([]byte{0x05, 0x07, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}

	var host string
	var port int
	switch buf[3] {
	case 0x01:
		if n < 10 {
			return
		}
		host = fmt.Sprintf("%d.%d.%d.%d", buf[4], buf[5], buf[6], buf[7])
		port = int(buf[8])<<8 | int(buf[9])
	case 0x03:
		domainLen := int(buf[4])
		if n < 5+domainLen+2 {
			return
		}
		host = string(buf[5 : 5+domainLen])
		port = int(buf[5+domainLen])<<8 | int(buf[5+domainLen+1])
	case 0x04:
		if n < 22 {
			return
		}
		host = fmt.Sprintf("[%x:%x:%x:%x:%x:%x:%x:%x]",
			int(buf[4])<<8|int(buf[5]), int(buf[6])<<8|int(buf[7]),
			int(buf[8])<<8|int(buf[9]), int(buf[10])<<8|int(buf[11]),
			int(buf[12])<<8|int(buf[13]), int(buf[14])<<8|int(buf[15]),
			int(buf[16])<<8|int(buf[17]), int(buf[18])<<8|int(buf[19]))
		port = int(buf[20])<<8 | int(buf[21])
	default:
		conn.Write([]byte{0x05, 0x08, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}

	remoteAddr := fmt.Sprintf("%s:%d", host, port)
	connID := protocol.GenerateID()

	h.logger.Debug("socks5: CONNECT request", zap.String("remote", remoteAddr), zap.String("device_id", deviceID))

	env, _ := protocol.NewEnvelope(
		protocol.GenerateID(),
		protocol.ChannelTunnel,
		protocol.TypeRequest,
		protocol.ActionSocks5Open,
		protocol.Socks5OpenPayload{ConnID: connID, RemoteAddr: remoteAddr},
	)
	resp, err := h.RequestToDevice(deviceID, env, 10*time.Second)
	if err != nil || resp == nil || resp.Error != nil {
		h.logger.Warn("socks5: agent request failed", zap.String("remote", remoteAddr), zap.Error(err))
		conn.Write([]byte{0x05, 0x05, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}

	var result protocol.Socks5OpenResult
	resp.DecodePayload(&result)
	if !result.OK {
		h.logger.Warn("socks5: agent dial failed", zap.String("remote", remoteAddr), zap.String("error", result.Error))
		conn.Write([]byte{0x05, 0x05, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}

	conn.SetDeadline(time.Time{})
	h.logger.Info("socks5: connected", zap.String("remote", remoteAddr), zap.String("conn_id", connID))

	conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0})

	socks5Conns.Store(connID, conn)
	socks5ConnTunnel.Store(connID, tunnelID)
	// Track connection per device for cleanup on disconnect
	deviceConns, _ := socks5DeviceConns.LoadOrStore(deviceID, &sync.Map{})
	deviceConns.(*sync.Map).Store(connID, conn)
	defer func() {
		socks5Conns.Delete(connID)
		socks5ConnTunnel.Delete(connID)
		deviceConns.(*sync.Map).Delete(connID)
	}()

	relayBuf := make([]byte, 32*1024)
	for {
		n, err := conn.Read(relayBuf)
		if n > 0 {
			encoded := base64.StdEncoding.EncodeToString(relayBuf[:n])
			dataEnv, _ := protocol.NewEnvelope(
				protocol.GenerateID(),
				protocol.ChannelTunnel,
				protocol.TypeStream,
				protocol.ActionSocks5Data,
				protocol.Socks5DataPayload{ConnID: connID, Data: encoded},
			)
			if err := h.SendToDevice(deviceID, dataEnv); err != nil {
				return
			}
			AddTunnelBytes(tunnelID, int64(n), 0)
		}
		if err != nil {
			closeEnv, _ := protocol.NewEnvelope(
				protocol.GenerateID(),
				protocol.ChannelTunnel,
				protocol.TypeRequest,
				protocol.ActionSocks5Close,
				protocol.Socks5ClosePayload{ConnID: connID},
			)
			h.SendToDevice(deviceID, closeEnv)
			return
		}
	}
}

func isAllowedIP(addr net.Addr, allowedIPs string) bool {
	if allowedIPs == "" {
		return true
	}

	ipStr, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		ipStr = addr.String()
	}
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}

	for _, entry := range strings.Split(allowedIPs, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}

		if strings.Contains(entry, "/") {
			_, network, err := net.ParseCIDR(entry)
			if err != nil {
				continue
			}
			if network.Contains(ip) {
				return true
			}
		} else {
			allowedIP := net.ParseIP(entry)
			if allowedIP != nil && allowedIP.Equal(ip) {
				return true
			}
		}
	}

	return false
}
