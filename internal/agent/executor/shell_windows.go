//go:build windows

package executor

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/UserExistsError/conpty"
	"github.com/qoder/device-mgmt/internal/protocol"
	"go.uber.org/zap"
)

// ShellManager manages shell sessions (Windows - uses ConPTY)
type ShellManager struct {
	sessions map[string]*ShellSession
	mu       sync.Mutex
	logger   *zap.Logger
	sendFn   func(*protocol.Envelope) error
}

// ShellSession represents an active shell session on Windows
type ShellSession struct {
	ID     string
	conpty *conpty.ConPty
	done   chan error
	cancel context.CancelFunc
}

// NewShellManager creates a new shell manager
func NewShellManager(logger *zap.Logger, sendFn func(*protocol.Envelope) error) *ShellManager {
	return &ShellManager{
		sessions: make(map[string]*ShellSession),
		logger:   logger,
		sendFn:   sendFn,
	}
}

// StartSession starts a new shell session using ConPTY
func (m *ShellManager) StartSession(ctx context.Context, id string, cols, rows uint16, shellPath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.sessions[id]; exists {
		return fmt.Errorf("session %s already exists", id)
	}

	if shellPath == "" {
		shellPath = getDefaultShell()
	}

	if cols == 0 {
		cols = 80
	}
	if rows == 0 {
		rows = 24
	}

	// Try ConPTY first (Windows 10 1809+)
	if conpty.IsConPtyAvailable() {
		session, err := m.startConPTY(ctx, id, shellPath, cols, rows)
		if err == nil {
			m.sessions[id] = session
			m.logger.Info("shell session started (ConPTY)", zap.String("session_id", id))
			return nil
		}
		// ConPTY failed, log and fall back
		m.logger.Warn("ConPTY failed, falling back to pipes", zap.Error(err))
	} else {
		m.logger.Warn("ConPTY not available on this Windows version, falling back to pipes")
	}

	// Fallback to pipes (limited: no echo, no prompt, no resize)
	session, err := m.startPipes(ctx, id, shellPath)
	if err != nil {
		return err
	}

	m.sessions[id] = session
	m.logger.Info("shell session started (pipes fallback)", zap.String("session_id", id))
	return nil
}

// startConPTY creates a shell session using Windows ConPTY API
func (m *ShellManager) startConPTY(ctx context.Context, id string, shellPath string, cols, rows uint16) (*ShellSession, error) {
	cpty, err := conpty.Start(shellPath, conpty.ConPtyDimensions(int(cols), int(rows)))
	if err != nil {
		return nil, fmt.Errorf("start ConPTY: %w", err)
	}

	_, cancel := context.WithCancel(ctx)
	session := &ShellSession{
		ID:     id,
		conpty: cpty,
		done:   make(chan error, 1),
		cancel: cancel,
	}

	// Read output from ConPTY
	go m.readOutput(session)

	// Wait for process exit
	go func() {
		exitCode, err := cpty.Wait(context.Background())
		if err != nil {
			m.logger.Debug("ConPTY wait error", zap.Error(err), zap.Uint32("exit_code", exitCode))
		}
		session.done <- fmt.Errorf("process exited with code %d", exitCode)
		m.CloseSession(id)
	}()

	return session, nil
}

// startPipes creates a shell session using stdin/stdout pipes (fallback)
func (m *ShellManager) startPipes(ctx context.Context, id string, shellPath string) (*ShellSession, error) {
	// Pipe-based fallback implementation
	type pipeSession struct {
		ID     string
		stdin  io.WriteCloser
		stdout io.ReadCloser
		done   chan error
		cancel context.CancelFunc
		pid    int
	}

	// We embed this in a ShellSession-like structure
	// Since ShellSession only uses conpty, we need an alternative
	// This is a simplified fallback using os/exec
	return nil, fmt.Errorf("pipe fallback not implemented - ConPTY required (Windows 10 1809+)")
}

// WriteInput writes data to a shell session
func (m *ShellManager) WriteInput(sessionID string, data string) error {
	m.mu.Lock()
	session, exists := m.sessions[sessionID]
	m.mu.Unlock()

	if !exists {
		return fmt.Errorf("session %s not found", sessionID)
	}

	decoded, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return fmt.Errorf("decode input: %w", err)
	}

	if session.conpty != nil {
		_, err = session.conpty.Write(decoded)
		return err
	}

	return fmt.Errorf("session %s has no I/O channel", sessionID)
}

// Resize resizes the terminal (supported with ConPTY)
func (m *ShellManager) Resize(sessionID string, cols, rows uint16) error {
	m.mu.Lock()
	session, exists := m.sessions[sessionID]
	m.mu.Unlock()

	if !exists {
		return fmt.Errorf("session %s not found", sessionID)
	}

	if session.conpty != nil {
		return session.conpty.Resize(int(cols), int(rows))
	}

	return nil // no-op for pipe fallback
}

// CloseSession closes a shell session
func (m *ShellManager) CloseSession(sessionID string) error {
	m.mu.Lock()
	session, exists := m.sessions[sessionID]
	if !exists {
		m.mu.Unlock()
		return nil
	}
	delete(m.sessions, sessionID)
	m.mu.Unlock()

	session.cancel()

	if session.conpty != nil {
		session.conpty.Close()
	}

	m.logger.Info("shell session closed", zap.String("session_id", sessionID))
	return nil
}

// CloseAll closes all shell sessions
func (m *ShellManager) CloseAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for id := range m.sessions {
		m.CloseSession(id)
	}
}

func (m *ShellManager) readOutput(session *ShellSession) {
	buf := make([]byte, 4096)
	for {
		n, err := session.conpty.Read(buf)
		if err != nil {
			if err != io.EOF {
				m.logger.Debug("shell read ended", zap.Error(err))
			}
			return
		}

		if n > 0 {
			encoded := base64.StdEncoding.EncodeToString(buf[:n])
			env, _ := protocol.NewEnvelope(
				protocol.GenerateID(),
				protocol.ChannelShell,
				protocol.TypeStream,
				protocol.ActionShellOutput,
				protocol.ShellOutputPayload{
					SessionID: session.ID,
					Data:      encoded,
				},
			)

			if err := m.sendFn(env); err != nil {
				m.logger.Warn("send shell output failed", zap.Error(err))
			}
		}
	}
}

// getDefaultShell returns the default shell path (Windows)
// Uses chcp 65001 to force UTF-8 output so the browser can render correctly
func getDefaultShell() string {
	if shell := os.Getenv("COMSPEC"); shell != "" {
		return shell + ` /K "chcp 65001 >nul"`
	}
	return `cmd.exe /K "chcp 65001 >nul"`
}

// HandleRequest handles shell protocol requests
func (m *ShellManager) HandleRequest(ctx context.Context, env *protocol.Envelope) (*protocol.Envelope, error) {
	switch env.Action {
	case protocol.ActionShellStart:
		var payload protocol.ShellStartPayload
		if err := env.DecodePayload(&payload); err != nil {
			return nil, err
		}
		if err := m.StartSession(ctx, payload.SessionID, payload.Cols, payload.Rows, payload.ShellPath); err != nil {
			return nil, err
		}
		return protocol.NewResponse(env, map[string]string{"session_id": payload.SessionID})

	case protocol.ActionShellInput:
		var payload protocol.ShellInputPayload
		if err := env.DecodePayload(&payload); err != nil {
			return nil, err
		}
		if err := m.WriteInput(payload.SessionID, payload.Data); err != nil {
			return nil, err
		}
		return nil, nil

	case protocol.ActionShellResize:
		var payload protocol.ShellResizePayload
		if err := env.DecodePayload(&payload); err != nil {
			return nil, err
		}
		if err := m.Resize(payload.SessionID, payload.Cols, payload.Rows); err != nil {
			m.logger.Warn("resize failed", zap.Error(err))
		}
		return protocol.NewResponse(env, map[string]string{})

	case protocol.ActionShellClose:
		var payload protocol.ShellClosePayload
		if err := env.DecodePayload(&payload); err != nil {
			return nil, err
		}
		if err := m.CloseSession(payload.SessionID); err != nil {
			return nil, err
		}
		return protocol.NewResponse(env, map[string]string{})

	default:
		return nil, fmt.Errorf("unknown shell action: %s", env.Action)
	}
}

// Name returns the plugin name
func (m *ShellManager) Name() string { return "shell" }

// Init initializes the plugin
func (m *ShellManager) Init(a interface{}) error { return nil }

// Shutdown shuts down the plugin
func (m *ShellManager) Shutdown() error {
	m.CloseAll()
	return nil
}
