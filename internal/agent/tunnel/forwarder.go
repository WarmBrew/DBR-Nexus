package tunnel

import (
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/qoder/device-mgmt/internal/protocol"
	"go.uber.org/zap"
)

// WSClient interface for sending messages back to server
type WSClient interface {
	Send(env *protocol.Envelope) error
	Request(env *protocol.Envelope) (*protocol.Envelope, error)
}

// Forwarder handles tunnel (port forwarding) operations on the agent
type Forwarder struct {
	client      WSClient
	tunnels     map[string]*RemoteTunnel
	socks5Conns map[string]*socks5Conn // connID -> SOCKS5 connection
	mu          sync.Mutex
	logger      *zap.Logger
}

// socks5Conn wraps a SOCKS5 TCP connection with a write mutex
type socks5Conn struct {
	conn    net.Conn
	writeMu sync.Mutex
}

// RemoteTunnel represents an active tunnel on the agent side
type RemoteTunnel struct {
	ID         string
	RemoteAddr string
	Conns      map[string]net.Conn
	writeMu    sync.Mutex // serializes writes to all connections in this tunnel
}

// NewForwarder creates a new tunnel forwarder
func NewForwarder(logger *zap.Logger, client WSClient) *Forwarder {
	return &Forwarder{
		tunnels:     make(map[string]*RemoteTunnel),
		socks5Conns: make(map[string]*socks5Conn),
		logger:      logger,
		client:      client,
	}
}

// HandleOpen opens a tunnel
func (f *Forwarder) HandleOpen(ctx context.Context, tunnelID, remoteAddr string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, exists := f.tunnels[tunnelID]; exists {
		return fmt.Errorf("tunnel %s already exists", tunnelID)
	}
	f.tunnels[tunnelID] = &RemoteTunnel{ID: tunnelID, RemoteAddr: remoteAddr, Conns: make(map[string]net.Conn)}
	f.logger.Info("tunnel opened", zap.String("tunnel_id", tunnelID), zap.String("remote", remoteAddr))
	return nil
}

// HandleData receives data and writes it to the remote connection
func (f *Forwarder) HandleData(tunnelID, connID, data string) error {
	f.mu.Lock()
	tunnel, exists := f.tunnels[tunnelID]
	if !exists {
		f.mu.Unlock()
		return fmt.Errorf("tunnel %s not found", tunnelID)
	}

	conn, connExists := tunnel.Conns[connID]
	f.mu.Unlock()

	if !connExists {
		// Dial without holding the mutex to avoid blocking all tunnel operations.
		// After dial, re-acquire mutex to check if another goroutine already created
		// a connection for this connID (race window between unlock and dial).
		newConn, err := net.DialTimeout("tcp", tunnel.RemoteAddr, 10*time.Second)
		if err != nil {
			return fmt.Errorf("dial remote %s: %w", tunnel.RemoteAddr, err)
		}

		f.mu.Lock()
		// Double-check: another goroutine might have dialed for this connID already
		if existing, ok := tunnel.Conns[connID]; ok {
			// Another goroutine won the race; close our new connection and use theirs
			f.mu.Unlock()
			newConn.Close()
			conn = existing
		} else {
			tunnel.Conns[connID] = newConn
			f.mu.Unlock()
			go f.readRemote(tunnelID, connID, newConn)
			conn = newConn
		}
	}

	decoded, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return fmt.Errorf("decode data: %w", err)
	}
	// Serialize writes to prevent out-of-order TCP data when multiple
	// HandleData goroutines run concurrently (e.g. SSH sends rapid small packets).
	tunnel.writeMu.Lock()
	_, err = conn.Write(decoded)
	tunnel.writeMu.Unlock()
	return err
}

// HandleClose closes a tunnel and all its connections
func (f *Forwarder) HandleClose(tunnelID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	tunnel, exists := f.tunnels[tunnelID]
	if !exists {
		return nil
	}
	for connID, conn := range tunnel.Conns {
		conn.Close()
		delete(tunnel.Conns, connID)
	}
	delete(f.tunnels, tunnelID)
	f.logger.Info("tunnel closed", zap.String("tunnel_id", tunnelID))
	return nil
}

// HandleConnClose closes a specific connection within a tunnel
func (f *Forwarder) HandleConnClose(tunnelID, connID string) error {
	f.mu.Lock()
	tunnel, exists := f.tunnels[tunnelID]
	if !exists {
		f.mu.Unlock()
		return fmt.Errorf("tunnel %s not found", tunnelID)
	}
	conn, ok := tunnel.Conns[connID]
	if ok {
		delete(tunnel.Conns, connID)
	}
	f.mu.Unlock()
	if ok {
		conn.Close()
		f.logger.Debug("tunnel connection closed", zap.String("tunnel_id", tunnelID), zap.String("conn_id", connID))
	}
	return nil
}

func (f *Forwarder) readRemote(tunnelID, connID string, conn net.Conn) {
	defer func() {
		// Only close the connection; do NOT delete from the map here.
		// The server may still send data for this connID before it detects the close.
		// HandleConnClose (triggered by server) will clean up the map entry.
		conn.Close()
		// Notify server that this connection is done so it can clean up its side.
		env, _ := protocol.NewEnvelope(uuid.New().String(), protocol.ChannelTunnel, protocol.TypeStream, protocol.ActionTunnelConnClose,
			protocol.TunnelDataPayload{TunnelID: tunnelID, ConnID: connID})
		if err := f.client.Send(env); err != nil {
			f.logger.Debug("failed to send conn close notification", zap.String("tunnel_id", tunnelID), zap.String("conn_id", connID))
		}
	}()
	buf := make([]byte, 32*1024)
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			encoded := base64.StdEncoding.EncodeToString(buf[:n])
			env, _ := protocol.NewEnvelope(uuid.New().String(), protocol.ChannelTunnel, protocol.TypeStream, protocol.ActionTunnelData,
				protocol.TunnelDataPayload{TunnelID: tunnelID, ConnID: connID, Data: encoded})
			if err := f.client.Send(env); err != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

// HandleRequest handles tunnel protocol requests
func (f *Forwarder) HandleRequest(ctx context.Context, env *protocol.Envelope) (*protocol.Envelope, error) {
	switch env.Action {
	case protocol.ActionTunnelOpen:
		var payload protocol.TunnelOpenPayload
		if err := env.DecodePayload(&payload); err != nil {
			return nil, err
		}
		if err := f.HandleOpen(ctx, payload.TunnelID, payload.RemoteAddr); err != nil {
			return nil, err
		}
		return protocol.NewResponse(env, map[string]string{"tunnel_id": payload.TunnelID})
	case protocol.ActionTunnelData:
		var payload protocol.TunnelDataPayload
		if err := env.DecodePayload(&payload); err != nil {
			return nil, err
		}
		if err := f.HandleData(payload.TunnelID, payload.ConnID, payload.Data); err != nil {
			return nil, err
		}
		return nil, nil
	case protocol.ActionTunnelClose:
		var payload protocol.TunnelClosePayload
		if err := env.DecodePayload(&payload); err != nil {
			return nil, err
		}
		if err := f.HandleClose(payload.TunnelID); err != nil {
			return nil, err
		}
		return protocol.NewResponse(env, map[string]string{})
	case protocol.ActionTunnelConnClose:
		var payload protocol.TunnelDataPayload
		if err := env.DecodePayload(&payload); err != nil {
			return nil, err
		}
		f.HandleConnClose(payload.TunnelID, payload.ConnID)
		return nil, nil
	case protocol.ActionSocks5Open:
		var payload protocol.Socks5OpenPayload
		if err := env.DecodePayload(&payload); err != nil {
			return nil, err
		}
		return f.handleSocks5Open(env, payload)
	case protocol.ActionSocks5Data:
		var payload protocol.Socks5DataPayload
		if err := env.DecodePayload(&payload); err != nil {
			return nil, err
		}
		if err := f.handleSocks5Data(payload); err != nil {
			return nil, err
		}
		return nil, nil
	case protocol.ActionSocks5Close:
		var payload protocol.Socks5ClosePayload
		if err := env.DecodePayload(&payload); err != nil {
			return nil, err
		}
		f.handleSocks5Close(payload.ConnID)
		return protocol.NewResponse(env, map[string]string{})
	default:
		return nil, fmt.Errorf("unknown tunnel action: %s", env.Action)
	}
}

func (f *Forwarder) Name() string             { return "tunnel" }
func (f *Forwarder) Init(a interface{}) error { return nil }
func (f *Forwarder) Shutdown() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, tunnel := range f.tunnels {
		for connID, conn := range tunnel.Conns {
			conn.Close()
			delete(tunnel.Conns, connID)
		}
		delete(f.tunnels, id)
	}
	for connID, sc := range f.socks5Conns {
		sc.conn.Close()
		delete(f.socks5Conns, connID)
	}
	return nil
}

// handleSocks5Open opens a TCP connection to the requested remote address
func (f *Forwarder) handleSocks5Open(env *protocol.Envelope, payload protocol.Socks5OpenPayload) (*protocol.Envelope, error) {
	result := protocol.Socks5OpenResult{ConnID: payload.ConnID}

	conn, err := net.DialTimeout("tcp", payload.RemoteAddr, 10*time.Second)
	if err != nil {
		result.OK = false
		result.Error = err.Error()
		f.logger.Warn("socks5 dial failed", zap.String("conn_id", payload.ConnID), zap.String("remote", payload.RemoteAddr), zap.Error(err))
	} else {
		result.OK = true
		sc := &socks5Conn{conn: conn}
		f.mu.Lock()
		f.socks5Conns[payload.ConnID] = sc
		f.mu.Unlock()
		go f.readSocks5Remote(payload.ConnID, sc)
		f.logger.Info("socks5 connection opened", zap.String("conn_id", payload.ConnID), zap.String("remote", payload.RemoteAddr))
	}

	return protocol.NewResponse(env, result)
}

// handleSocks5Data writes data to the SOCKS5 TCP connection
func (f *Forwarder) handleSocks5Data(payload protocol.Socks5DataPayload) error {
	f.mu.Lock()
	sc, ok := f.socks5Conns[payload.ConnID]
	f.mu.Unlock()
	if !ok {
		return fmt.Errorf("socks5 connection %s not found", payload.ConnID)
	}
	decoded, err := base64.StdEncoding.DecodeString(payload.Data)
	if err != nil {
		return fmt.Errorf("decode socks5 data: %w", err)
	}
	sc.writeMu.Lock()
	_, err = sc.conn.Write(decoded)
	sc.writeMu.Unlock()
	return err
}

// handleSocks5Close closes a SOCKS5 connection
func (f *Forwarder) handleSocks5Close(connID string) {
	f.mu.Lock()
	sc, ok := f.socks5Conns[connID]
	if ok {
		delete(f.socks5Conns, connID)
	}
	f.mu.Unlock()
	if ok {
		sc.conn.Close()
		f.logger.Info("socks5 connection closed", zap.String("conn_id", connID))
	}
}

// readSocks5Remote reads from the remote TCP connection and sends data back to server
func (f *Forwarder) readSocks5Remote(connID string, sc *socks5Conn) {
	defer func() {
		// Only close the connection; do NOT delete from the map here.
		// The server may still send data for this connID before it detects the close.
		// handleSocks5Close (triggered by server) will clean up the map entry.
		sc.conn.Close()
		// Notify server that this connection is done so it can clean up its side.
		env, _ := protocol.NewEnvelope(uuid.New().String(), protocol.ChannelTunnel, protocol.TypeStream, protocol.ActionSocks5Close,
			protocol.Socks5ClosePayload{ConnID: connID})
		if err := f.client.Send(env); err != nil {
			f.logger.Debug("failed to send socks5 close notification", zap.String("conn_id", connID))
		}
	}()
	buf := make([]byte, 32*1024)
	for {
		n, err := sc.conn.Read(buf)
		if n > 0 {
			encoded := base64.StdEncoding.EncodeToString(buf[:n])
			env, _ := protocol.NewEnvelope(uuid.New().String(), protocol.ChannelTunnel, protocol.TypeStream, protocol.ActionSocks5Data,
				protocol.Socks5DataPayload{ConnID: connID, Data: encoded})
			if err := f.client.Send(env); err != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}
