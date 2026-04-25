package tunnel

import (
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"sync"

	"github.com/qoder/device-mgmt/internal/protocol"
	"go.uber.org/zap"
)

// Socks5Server implements a SOCKS5 proxy that forwards connections via WebSocket
type Socks5Server struct {
	client   WSClient
	listener net.Listener
	conns    map[string]net.Conn
	mu       sync.Mutex
	logger   *zap.Logger
	cancel   context.CancelFunc
}

// NewSocks5Server creates a new SOCKS5 server
func NewSocks5Server(logger *zap.Logger, client WSClient) *Socks5Server {
	return &Socks5Server{
		conns:  make(map[string]net.Conn),
		logger: logger,
		client: client,
	}
}

// Start starts the SOCKS5 listener on the given address
func (s *Socks5Server) Start(ctx context.Context, addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("socks5 listen: %w", err)
	}
	s.listener = ln

	ctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	go s.acceptLoop(ctx)
	s.logger.Info("socks5 proxy started", zap.String("addr", addr))
	return nil
}

// Stop stops the SOCKS5 server
func (s *Socks5Server) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	if s.listener != nil {
		s.listener.Close()
	}
	s.mu.Lock()
	for id, conn := range s.conns {
		conn.Close()
		delete(s.conns, id)
	}
	s.mu.Unlock()
}

func (s *Socks5Server) acceptLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handleConnection(conn)
	}
}

func (s *Socks5Server) handleConnection(conn net.Conn) {
	defer conn.Close()

	// SOCKS5 handshake
	buf := make([]byte, 256)

	// Read version and auth methods
	n, err := conn.Read(buf)
	if err != nil || n < 2 {
		return
	}
	if buf[0] != 0x05 {
		return // Not SOCKS5
	}

	// No auth required
	conn.Write([]byte{0x05, 0x00})

	// Read request
	n, err = conn.Read(buf)
	if err != nil || n < 7 {
		return
	}

	if buf[0] != 0x05 || buf[1] != 0x01 {
		// Only support CONNECT
		conn.Write([]byte{0x05, 0x07, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}

	var host string
	var port int

	switch buf[3] {
	case 0x01: // IPv4
		if n < 10 {
			return
		}
		host = fmt.Sprintf("%d.%d.%d.%d", buf[4], buf[5], buf[6], buf[7])
		port = int(buf[8])<<8 | int(buf[9])
	case 0x03: // Domain
		domainLen := int(buf[4])
		if n < 5+domainLen+2 {
			return
		}
		host = string(buf[5 : 5+domainLen])
		port = int(buf[5+domainLen])<<8 | int(buf[5+domainLen+1])
	case 0x04: // IPv6
		if n < 22 {
			return
		}
		host = fmt.Sprintf("[%x:%x:%x:%x:%x:%x:%x:%x]",
			buf[4:6], buf[6:8], buf[8:10], buf[10:12],
			buf[12:14], buf[14:16], buf[16:18], buf[18:20])
		port = int(buf[20])<<8 | int(buf[21])
	default:
		conn.Write([]byte{0x05, 0x08, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}

	remoteAddr := fmt.Sprintf("%s:%d", host, port)
	connID := protocol.GenerateID()

	// Open SOCKS5 tunnel via WebSocket
	env, _ := protocol.NewEnvelope(
		protocol.GenerateID(),
		protocol.ChannelTunnel,
		protocol.TypeRequest,
		protocol.ActionSocks5Open,
		protocol.Socks5OpenPayload{ConnID: connID, RemoteAddr: remoteAddr},
	)
	resp, err := s.client.Request(env)
	if err != nil {
		conn.Write([]byte{0x05, 0x05, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}

	var result protocol.Socks5OpenResult
	resp.DecodePayload(&result)
	if !result.OK {
		conn.Write([]byte{0x05, 0x05, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}

	// Success response
	conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0})

	// Register connection
	s.mu.Lock()
	s.conns[connID] = conn
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.conns, connID)
		s.mu.Unlock()
		// Notify server of close
		closeEnv, _ := protocol.NewEnvelope(
			protocol.GenerateID(),
			protocol.ChannelTunnel,
			protocol.TypeRequest,
			protocol.ActionSocks5Close,
			protocol.Socks5ClosePayload{ConnID: connID},
		)
		s.client.Send(closeEnv)
	}()

	// Relay data: read from SOCKS5 client, send to remote
	relayBuf := make([]byte, 32*1024)
	for {
		n, err := conn.Read(relayBuf)
		if n > 0 {
			encoded := base64.StdEncoding.EncodeToString(relayBuf[:n])
			env, _ := protocol.NewEnvelope(
				protocol.GenerateID(),
				protocol.ChannelTunnel,
				protocol.TypeStream,
				protocol.ActionSocks5Data,
				protocol.Socks5DataPayload{ConnID: connID, Data: encoded},
			)
			if err := s.client.Send(env); err != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

// HandleSocks5Data receives data from remote and writes to the local SOCKS5 connection
func (s *Socks5Server) HandleSocks5Data(connID, data string) error {
	s.mu.Lock()
	conn, ok := s.conns[connID]
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("socks5 connection %s not found", connID)
	}
	decoded, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return fmt.Errorf("decode socks5 data: %w", err)
	}
	_, err = conn.Write(decoded)
	return err
}

// HandleSocks5Close closes a SOCKS5 connection
func (s *Socks5Server) HandleSocks5Close(connID string) {
	s.mu.Lock()
	conn, ok := s.conns[connID]
	if ok {
		delete(s.conns, connID)
	}
	s.mu.Unlock()
	if ok {
		conn.Close()
	}
}

// Request interface for SOCKS5 server
func (s *Socks5Server) Request(env *protocol.Envelope) (*protocol.Envelope, error) {
	return nil, fmt.Errorf("socks5 server does not support request method directly")
}
