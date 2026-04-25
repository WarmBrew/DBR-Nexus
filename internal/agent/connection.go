package agent

import (
	"context"
	"crypto/ecdh"
	"crypto/hmac"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	mathrand "math/rand"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	svcrypto "github.com/qoder/device-mgmt/internal/crypto"
	"github.com/qoder/device-mgmt/internal/protocol"
	"go.uber.org/zap"
)

// Client manages the WebSocket connection to the server
type Client struct {
	serverURL    string
	psk          string
	deviceID     string
	conn         *websocket.Conn
	mu           sync.Mutex
	pending      map[string]chan *protocol.Envelope
	sendCh       chan *protocol.Envelope
	rawSendCh    chan []byte // for Obfuscator dummy frames (concurrent-safe)
	onConnect    []func()
	onDisconnect []func()
	logger       *zap.Logger
	closed       bool
	lastPong     time.Time
	session      *svcrypto.Session // nil = cleartext mode
	deviceAlias  uint32
	obfuscator   *svcrypto.Obfuscator
	stopCh       chan struct{}   // signals all goroutines to stop
	wg           sync.WaitGroup  // tracks running goroutines
}

// NewClient creates a new WebSocket client
func NewClient(serverURL, psk, deviceID string, logger *zap.Logger) *Client {
	return &Client{
		serverURL: serverURL,
		psk:       psk,
		deviceID:  deviceID,
		pending:   make(map[string]chan *protocol.Envelope),
		sendCh:    make(chan *protocol.Envelope, 256),
		rawSendCh: make(chan []byte, 64),
		logger:    logger,
		stopCh:    make(chan struct{}),
	}
}

// Connect establishes a WebSocket connection and performs PSK authentication
func (c *Client) Connect(ctx context.Context) error {
	// Stop any previous goroutines before starting new connection
	c.stopAllGoroutines()

	c.mu.Lock()
	c.closed = false
	c.session = nil
	c.deviceAlias = 0
	// Create new stop channel and send channels for this connection
	c.stopCh = make(chan struct{})
	c.sendCh = make(chan *protocol.Envelope, 256)
	c.rawSendCh = make(chan []byte, 64)
	c.mu.Unlock()

	dialer := &websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
		ReadBufferSize:   4096,
		WriteBufferSize:  4096,
	}

	// Enable TLS for wss:// connections with InsecureSkipVerify for self-signed certs
	if strings.HasPrefix(c.serverURL, "wss://") {
		dialer.TLSClientConfig = &tls.Config{
			InsecureSkipVerify: true, // Allow self-signed certificates
		}
	}

	conn, _, err := dialer.DialContext(ctx, c.serverURL, nil)
	if err != nil {
		return fmt.Errorf("dial server: %w", err)
	}

	conn.SetReadDeadline(time.Now().Add(30 * time.Second))

	c.conn = conn
	c.lastPong = time.Now()
	c.logger.Info("connected to server")

	// Read the PSK challenge
	_, msg, err := conn.ReadMessage()
	if err != nil {
		conn.Close()
		return fmt.Errorf("read challenge: %w", err)
	}

	var challenge protocol.Envelope
	if err := json.Unmarshal(msg, &challenge); err != nil {
		conn.Close()
		return fmt.Errorf("parse challenge: %w", err)
	}

	if challenge.Action != protocol.ActionAuthChallenge {
		conn.Close()
		return fmt.Errorf("expected auth.challenge, got %s", challenge.Action)
	}

	var challengePayload protocol.AuthChallengePayload
	challenge.DecodePayload(&challengePayload)

	// Compute HMAC-SHA256 of nonce using PSK
	mac := hmac.New(sha256.New, []byte(c.psk))
	mac.Write([]byte(challengePayload.Nonce))
	hmacHex := hex.EncodeToString(mac.Sum(nil))

	// Build auth response payload
	authPayload := protocol.AuthResponsePayload{
		DeviceID:     c.deviceID,
		HMAC:         hmacHex,
		AgentVersion: "2.0.0",
		Hostname:     getHostname(),
		OS:           getOS(),
		Arch:         getArch(),
		IPInternal:   getInternalIPs(),
	}

	// If server supports encryption (v2 challenge with pubkey), do key exchange
	var agentPrivateKey *ecdh.PrivateKey
	var agentNonce []byte

	if challengePayload.ServerPubKey != "" && challengePayload.Version >= 2 {
		agentPrivateKey, err = ecdh.X25519().GenerateKey(cryptorand.Reader)
		if err != nil {
			c.logger.Warn("failed to generate X25519 key, proceeding without encryption", zap.Error(err))
		} else {
			agentNonce = make([]byte, 16)
			cryptorand.Read(agentNonce)

			authPayload.AgentPubKey = base64.StdEncoding.EncodeToString(agentPrivateKey.PublicKey().Bytes())
			authPayload.AgentNonce = base64.StdEncoding.EncodeToString(agentNonce)
		}
	}

	// Send auth response
	resp, _ := protocol.NewEnvelope(
		challenge.ID,
		protocol.ChannelSystem,
		protocol.TypeResponse,
		protocol.ActionAuthResponse,
		authPayload,
	)

	data, _ := json.Marshal(resp)
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		conn.Close()
		return fmt.Errorf("send auth response: %w", err)
	}

	// Wait for server's auth confirmation
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))

	if agentPrivateKey != nil && authPayload.AgentPubKey != "" {
		// Server may send an "encryption_start" text message before the encrypted confirm
		// Read messages until we get either a text "encryption_start" or a binary frame
		for {
			msgType, confirmMsg, err := conn.ReadMessage()
			if err != nil {
				conn.Close()
				return fmt.Errorf("read auth confirmation: %w", err)
			}

			if msgType == websocket.TextMessage {
				// The only acceptable text message is "encryption_start"
				var helloCheck struct {
					Type        string `json:"type"`
					ChallengeID string `json:"challenge_id"`
				}
				if err := json.Unmarshal(confirmMsg, &helloCheck); err == nil && helloCheck.Type == "encryption_start" {
					continue // read the next message (should be binary encrypted confirm)
				}
				// Any other text message after sending X25519 pubkey is rejected
				conn.Close()
				return fmt.Errorf("unexpected cleartext message during encrypted handshake")
			}

			if msgType == websocket.BinaryMessage {
				// This should be the encrypted key confirm
				// We need to compute the shared secret first
				serverPubKeyBytes, err := base64.StdEncoding.DecodeString(challengePayload.ServerPubKey)
				if err != nil {
					conn.Close()
					return fmt.Errorf("decode server pubkey: %w", err)
				}
				serverPubKey, err := ecdh.X25519().NewPublicKey(serverPubKeyBytes)
				if err != nil {
					conn.Close()
					return fmt.Errorf("parse server pubkey: %w", err)
				}
				sharedSecret, err := agentPrivateKey.ECDH(serverPubKey)
				if err != nil {
					conn.Close()
					return fmt.Errorf("compute ECDH: %w", err)
				}

				serverNonce, _ := base64.StdEncoding.DecodeString(challengePayload.ServerNonce)

				// Auth tag: HMAC-SHA256(PSK, serverNonce || agentPubKey)
				tagMAC := hmac.New(sha256.New, []byte(c.psk))
				tagMAC.Write(serverNonce)
				tagMAC.Write(agentPrivateKey.PublicKey().Bytes())
				authTag := tagMAC.Sum(nil)

				keys, err := svcrypto.DeriveKeys(&svcrypto.KeyDerivationContext{
					SharedSecret: sharedSecret,
					SessionLabel: "dbr-nexus-agent-v1",
					ServerNonce:  serverNonce,
					ClientNonce:  agentNonce,
					AuthTag:      authTag,
				})
				if err != nil {
					conn.Close()
					return fmt.Errorf("derive keys: %w", err)
				}

				session, err := svcrypto.NewSession(keys)
				if err != nil {
					conn.Close()
					return fmt.Errorf("create session: %w", err)
				}

				// Decrypt the key confirm
				confirmEnv, _, isDummy, err := session.DecryptFrame(confirmMsg)
				if err != nil {
					conn.Close()
					return fmt.Errorf("decrypt key confirm: %w", err)
				}
				if isDummy {
					continue
				}
				if confirmEnv != nil && confirmEnv.Error != nil {
					conn.Close()
					return fmt.Errorf("auth rejected: %s", confirmEnv.Error.Message)
				}

				c.session = session
				c.logger.Info("encryption established with server")

				// Extract device alias from confirm payload if present
				if confirmEnv != nil {
					var confirmData struct {
						DeviceAlias uint32 `json:"device_alias"`
					}
					confirmEnv.DecodePayload(&confirmData)
					c.deviceAlias = confirmData.DeviceAlias
				}
				break
			}
		}
	} else {
		// Legacy cleartext auth confirmation
		_, confirmMsg, err := conn.ReadMessage()
		if err != nil {
			conn.Close()
			return fmt.Errorf("read auth confirmation: %w", err)
		}
		var confirmEnv protocol.Envelope
		if err := json.Unmarshal(confirmMsg, &confirmEnv); err != nil {
			conn.Close()
			return fmt.Errorf("parse auth confirmation: %w", err)
		}
		if confirmEnv.Error != nil {
			conn.Close()
			return fmt.Errorf("auth rejected: %s", confirmEnv.Error.Message)
		}
	}

	c.logger.Info("authenticated with server", zap.Bool("encrypted", c.session != nil))

	// Set up ping/pong handler for connection health monitoring
	conn.SetPongHandler(func(appData string) error {
		c.mu.Lock()
		c.lastPong = time.Now()
		c.mu.Unlock()
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	conn.SetReadDeadline(time.Now().Add(60 * time.Second))

	// Start message pumps with WaitGroup tracking
	c.wg.Add(3)
	go c.readPump()
	go c.writePump()
	go c.pingLoop()

	// Start obfuscator if encrypted
	if c.session != nil {
		obfCfg := svcrypto.DefaultObfuscatorConfig()
		obf := svcrypto.NewObfuscator(obfCfg, c.session, func(frame []byte) error {
			select {
			case c.rawSendCh <- frame:
				return nil
			default:
				return fmt.Errorf("raw send buffer full")
			}
		})
		c.obfuscator = obf
		obf.Start()
	}

	// Fire onConnect callbacks
	for _, cb := range c.onConnect {
		cb()
	}

	return nil
}

// Send sends an envelope to the server (non-blocking via channel)
func (c *Client) Send(env *protocol.Envelope) error {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()

	if conn == nil {
		return fmt.Errorf("not connected")
	}

	select {
	case c.sendCh <- env:
		return nil
	default:
		return fmt.Errorf("send buffer full")
	}
}

// Request sends a request and waits for response
func (c *Client) Request(ctx context.Context, env *protocol.Envelope, timeout time.Duration) (*protocol.Envelope, error) {
	respCh := make(chan *protocol.Envelope, 1)
	c.mu.Lock()
	c.pending[env.ID] = respCh
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.pending, env.ID)
		c.mu.Unlock()
	}()

	if err := c.Send(env); err != nil {
		return nil, err
	}

	select {
	case resp := <-respCh:
		return resp, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("request timeout")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// OnConnect registers a callback for connection events
func (c *Client) OnConnect(fn func()) {
	c.onConnect = append(c.onConnect, fn)
}

// OnDisconnect registers a callback for disconnection events
func (c *Client) OnDisconnect(fn func()) {
	c.onDisconnect = append(c.onDisconnect, fn)
}

// stopAllGoroutines signals all running goroutines to stop and waits for them
func (c *Client) stopAllGoroutines() {
	c.mu.Lock()
	stopCh := c.stopCh
	if stopCh != nil {
		select {
		case <-stopCh:
			// Already closed
		default:
			close(stopCh)
		}
	}
	c.mu.Unlock()

	// Wait for all goroutines to finish
	c.wg.Wait()

	// Stop obfuscator if running
	if c.obfuscator != nil {
		c.obfuscator.Stop()
		c.obfuscator = nil
	}
}

// Close closes the connection
func (c *Client) Close() error {
	c.stopAllGoroutines()

	c.mu.Lock()
	c.closed = true
	conn := c.conn
	c.conn = nil
	c.mu.Unlock()

	if conn != nil {
		return conn.Close()
	}
	return nil
}

// IsClosed returns whether the client is closed
func (c *Client) IsClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

// ConnectWithRetry connects with exponential backoff retry
func (c *Client) ConnectWithRetry(ctx context.Context, cfg ReconnectConfig) error {
	delay := cfg.InitialDelay
	attempt := 0

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		err := c.Connect(ctx)
		if err == nil {
			return nil
		}

		attempt++
		c.logger.Warn("connection failed, retrying",
			zap.Int("attempt", attempt),
			zap.Error(err),
			zap.Duration("delay", delay),
		)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}

		jitter := time.Duration(mathrand.Int63n(int64(delay / 2)))
		delay = time.Duration(float64(delay) * cfg.Multiplier)
		delay += jitter
		if delay > cfg.MaxDelay {
			delay = cfg.MaxDelay
		}
	}
}

// readPump reads messages from the server
func (c *Client) readPump() {
	defer func() {
		c.wg.Done()
		c.mu.Lock()
		wasClosed := c.closed
		c.closed = true
		conn := c.conn
		c.conn = nil
		c.mu.Unlock()

		if conn != nil {
			conn.Close()
		}

		if !wasClosed {
			for _, cb := range c.onDisconnect {
				cb()
			}
		}
	}()

	for {
		msgType, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				c.logger.Error("read error", zap.Error(err))
			} else {
				c.logger.Info("connection closed", zap.Error(err))
			}
			return
		}

		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))

		var env *protocol.Envelope

		if c.session != nil && msgType == websocket.BinaryMessage {
			decrypted, _, isDummy, err := c.session.DecryptFrame(message)
			if err != nil {
				c.logger.Warn("decrypt error", zap.Error(err))
				continue
			}
			if isDummy {
				continue
			}
			if decrypted == nil {
				continue
			}
			env = decrypted
		} else {
			var parsed protocol.Envelope
			if err := json.Unmarshal(message, &parsed); err != nil {
				c.logger.Warn("invalid message", zap.Error(err))
				continue
			}
			env = &parsed
		}

		// Route to pending request if it's a response
		if env.Type == protocol.TypeResponse || env.Type == protocol.TypeStream {
			c.mu.Lock()
			if ch, ok := c.pending[env.ID]; ok {
				ch <- env
				if env.Type == protocol.TypeResponse {
					delete(c.pending, env.ID)
				}
			}
			c.mu.Unlock()
		}

		// Dispatch to agent handler
		if handler := c.getHandler(); handler != nil {
			go handler(env)
		}
	}
}

// writePump writes messages to the server
func (c *Client) writePump() {
	defer c.wg.Done()

	for {
		// Priority: real messages over dummy frames
		select {
		case <-c.stopCh:
			return
		case env, ok := <-c.sendCh:
			if !ok {
				return
			}
			if err := c.writeEnvelope(env); err != nil {
				c.logger.Error("send error", zap.Error(err))
				return
			}
			continue
		default:
		}

		select {
		case <-c.stopCh:
			return
		case env, ok := <-c.sendCh:
			if !ok {
				return
			}
			if err := c.writeEnvelope(env); err != nil {
				c.logger.Error("send error", zap.Error(err))
				return
			}
		case rawFrame, ok := <-c.rawSendCh:
			if !ok {
				return
			}
			if err := c.conn.WriteMessage(websocket.BinaryMessage, rawFrame); err != nil {
				c.logger.Error("raw send error", zap.Error(err))
				return
			}
		}
	}
}

// writeEnvelope encrypts (if session exists) and writes a single envelope
func (c *Client) writeEnvelope(env *protocol.Envelope) error {
	// Special handling for internal keepalive ping
	if env.ID == "__ping__" {
		return c.conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(5*time.Second))
	}

	if c.session != nil {
		frame, err := c.session.EncryptFrame(env, c.deviceAlias, "")
		if err != nil {
			return fmt.Errorf("encrypt: %w", err)
		}
		return c.conn.WriteMessage(websocket.BinaryMessage, frame)
	}
	data, err := json.Marshal(env)
	if err != nil {
		return err
	}
	return c.conn.WriteMessage(websocket.TextMessage, data)
}

// pingLoop sends periodic application-level keepalive messages via the sendCh
// to avoid concurrent WebSocket writes from multiple goroutines.
func (c *Client) pingLoop() {
	ticker := time.NewTicker(20 * time.Second)
	defer func() {
		ticker.Stop()
		c.wg.Done()
	}()

	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
		}

		c.mu.Lock()
		if c.closed || c.conn == nil {
			c.mu.Unlock()
			return
		}
		lastPong := c.lastPong
		c.mu.Unlock()

		// Check for stale connection before sending
		if !lastPong.IsZero() && time.Since(lastPong) > 90*time.Second {
			c.logger.Warn("no pong received for 90s, closing connection")
			c.conn.Close()
			return
		}

		// Send WebSocket protocol-level ping through writePump channel
		// to avoid concurrent write with writePump
		select {
		case <-c.stopCh:
			return
		case c.sendCh <- &protocol.Envelope{
			ID:        "__ping__",
			Type:      protocol.TypeRequest,
			Channel:   protocol.ChannelSystem,
			Action:    "keepalive",
			Timestamp: time.Now().UnixMilli(),
		}:
		default:
			// sendCh full — connection is likely stalled
		}
	}
}

var messageHandler func(*protocol.Envelope)

func (c *Client) getHandler() func(*protocol.Envelope) {
	return messageHandler
}

// SetMessageHandler sets the global message handler
func SetMessageHandler(fn func(*protocol.Envelope)) {
	messageHandler = fn
}

// getHostname returns the system hostname
func getHostname() string {
	hostname, err := hostname()
	if err != nil {
		return "unknown"
	}
	return hostname
}

func getOS() string {
	return operatingSystem()
}

func getArch() string {
	return architecture()
}

// Math helper for backoff
func init() {
	_ = math.Pi // ensure math import
}
