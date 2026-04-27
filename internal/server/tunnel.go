package server

import (
	"encoding/base64"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/qoder/device-mgmt/internal/protocol"
)

// PortPool manages a pool of available local ports for tunnels
type PortPool struct {
	host    string
	minPort int
	maxPort int
	used    map[int]bool
	mu      sync.Mutex
}

// NewPortPool creates a new port pool from a bind range string (e.g. "127.0.0.1:10000-20000")
func NewPortPool(bindRange string) (*PortPool, error) {
	parts := strings.Split(bindRange, ":")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid bind range format: %s", bindRange)
	}

	host := parts[0]
	portRange := strings.Split(parts[1], "-")
	if len(portRange) != 2 {
		return nil, fmt.Errorf("invalid port range format: %s", parts[1])
	}

	minPort, err := strconv.Atoi(portRange[0])
	if err != nil {
		return nil, fmt.Errorf("invalid min port: %s", portRange[0])
	}

	maxPort, err := strconv.Atoi(portRange[1])
	if err != nil {
		return nil, fmt.Errorf("invalid max port: %s", portRange[1])
	}

	return &PortPool{
		host:    host,
		minPort: minPort,
		maxPort: maxPort,
		used:    make(map[int]bool),
	}, nil
}

// Allocate allocates an available port and returns the address string
func (p *PortPool) Allocate() (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for port := p.minPort; port <= p.maxPort; port++ {
		if !p.used[port] {
			ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", p.host, port))
			if err != nil {
				continue
			}
			ln.Close()

			p.used[port] = true
			return fmt.Sprintf("%s:%d", p.host, port), nil
		}
	}

	return "", fmt.Errorf("no available ports in range %d-%d", p.minPort, p.maxPort)
}

// Release releases a previously allocated port
func (p *PortPool) Release(addr string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	parts := strings.Split(addr, ":")
	if len(parts) == 2 {
		port, err := strconv.Atoi(parts[1])
		if err == nil {
			delete(p.used, port)
		}
	}
}

// Listen starts a TCP listener on the given address
func (p *PortPool) Listen(addr string) (net.Listener, error) {
	return net.Listen("tcp", addr)
}

// tunnelConns tracks active tunnel TCP connections for routing data back from agent
var tunnelConns sync.Map // "tunnelID:connID" -> net.Conn

// tunnelBytes tracks byte counters per tunnel for traffic statistics
var tunnelBytes sync.Map // tunnelID -> *tunnelByteCounter

type tunnelByteCounter struct {
	sent atomic.Int64 // bytes sent from server to agent (uplink)
	recv atomic.Int64 // bytes received from agent to server (downlink)
}

// AddTunnelBytes updates the byte counters for a tunnel
func AddTunnelBytes(tunnelID string, sent, recv int64) {
	val, _ := tunnelBytes.LoadOrStore(tunnelID, &tunnelByteCounter{})
	counter := val.(*tunnelByteCounter)
	if sent > 0 {
		counter.sent.Add(sent)
	}
	if recv > 0 {
		counter.recv.Add(recv)
	}
}

// GetAndResetTunnelBytes returns and resets the byte counters for a tunnel
func GetAndResetTunnelBytes(tunnelID string) (sent, recv int64) {
	val, ok := tunnelBytes.Load(tunnelID)
	if !ok {
		return 0, 0
	}
	counter := val.(*tunnelByteCounter)
	sent = counter.sent.Swap(0)
	recv = counter.recv.Swap(0)
	return
}

// ServeTunnelListener accepts TCP connections on a tunnel's local port
func (h *Hub) ServeTunnelListener(tunnelID, deviceID string, ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		connID := protocol.GenerateID()
		go h.relayTunnelConnection(tunnelID, deviceID, connID, conn)
	}
}

// CloseTunnelListener closes a tunnel's listener
func (h *Hub) CloseTunnelListener(tunnelID string) {
	// Handled by tunnel manager
}

// relayTunnelConnection relays data between a local TCP connection and the remote device via WebSocket
func (h *Hub) relayTunnelConnection(tunnelID, deviceID, connID string, conn net.Conn) {
	// Register connection so agent responses can be routed back to this TCP connection
	connKey := tunnelID + ":" + connID
	tunnelConns.Store(connKey, conn)
	defer func() {
		tunnelConns.Delete(connKey)
		conn.Close()
		// Notify agent to close the remote connection
		closeEnv, _ := protocol.NewEnvelope(
			protocol.GenerateID(),
			protocol.ChannelTunnel,
			protocol.TypeRequest,
			protocol.ActionTunnelConnClose,
			protocol.TunnelDataPayload{
				TunnelID: tunnelID,
				ConnID:   connID,
			},
		)
		h.SendToDevice(deviceID, closeEnv)
	}()

	buf := make([]byte, 32*1024)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			return
		}

		env, _ := protocol.NewEnvelope(
			protocol.GenerateID(),
			protocol.ChannelTunnel,
			protocol.TypeRequest,
			protocol.ActionTunnelData,
			protocol.TunnelDataPayload{
				TunnelID: tunnelID,
				ConnID:   connID,
				Data:     base64.StdEncoding.EncodeToString(buf[:n]),
			},
		)

		if err := h.SendToDevice(deviceID, env); err != nil {
			return
		}
		AddTunnelBytes(tunnelID, int64(n), 0)
	}
}
