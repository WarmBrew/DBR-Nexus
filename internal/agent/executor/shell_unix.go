//go:build !windows

package executor

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"

	"github.com/creack/pty"
	"github.com/qoder/device-mgmt/internal/protocol"
	"go.uber.org/zap"
)

// ShellManager manages PTY shell sessions (Unix)
type ShellManager struct {
	sessions map[string]*ShellSession
	mu       sync.Mutex
	logger   *zap.Logger
	sendFn   func(*protocol.Envelope) error
}

// ShellSession represents an active PTY session
type ShellSession struct {
	ID     string
	PTY    *os.File
	Cmd    *exec.Cmd
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

// StartSession starts a new PTY session
func (m *ShellManager) StartSession(ctx context.Context, id string, cols, rows uint16, shellPath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.sessions[id]; exists {
		return fmt.Errorf("session %s already exists", id)
	}

	if shellPath == "" {
		shellPath = getDefaultShell()
	}

	cmd := exec.CommandContext(ctx, shellPath)
	cmd.Env = os.Environ()

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{
		Cols: cols,
		Rows: rows,
	})
	if err != nil {
		return fmt.Errorf("start pty: %w", err)
	}

	_, cancel := context.WithCancel(ctx)
	session := &ShellSession{
		ID:     id,
		PTY:    ptmx,
		Cmd:    cmd,
		done:   make(chan error, 1),
		cancel: cancel,
	}

	m.sessions[id] = session

	go m.readOutput(session)

	go func() {
		err := cmd.Wait()
		session.done <- err
		m.CloseSession(id)
	}()

	m.logger.Info("shell session started", zap.String("session_id", id))
	return nil
}

// WriteInput writes data to a PTY session
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

	_, err = session.PTY.Write(decoded)
	return err
}

// Resize resizes a PTY session
func (m *ShellManager) Resize(sessionID string, cols, rows uint16) error {
	m.mu.Lock()
	session, exists := m.sessions[sessionID]
	m.mu.Unlock()

	if !exists {
		return fmt.Errorf("session %s not found", sessionID)
	}

	return pty.Setsize(session.PTY, &pty.Winsize{
		Cols: cols,
		Rows: rows,
	})
}

// CloseSession closes a PTY session
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
	session.PTY.Close()

	if session.Cmd.Process != nil {
		session.Cmd.Process.Kill()
	}

	m.logger.Info("shell session closed", zap.String("session_id", sessionID))
	return nil
}

// CloseAll closes all PTY sessions
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
		n, err := session.PTY.Read(buf)
		if err != nil {
			if err != io.EOF {
				m.logger.Debug("pty read ended", zap.Error(err))
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

// getDefaultShell returns the default shell path (Unix)
func getDefaultShell() string {
	if shell := os.Getenv("SHELL"); shell != "" {
		return shell
	}
	return "/bin/sh"
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
			return nil, err
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
