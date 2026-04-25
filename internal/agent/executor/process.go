package executor

import (
	"context"
	"fmt"

	"github.com/qoder/device-mgmt/internal/protocol"
	"github.com/shirou/gopsutil/v3/process"
	"go.uber.org/zap"
)

// ProcessManager manages process operations
type ProcessManager struct {
	logger *zap.Logger
}

// NewProcessManager creates a new process manager
func NewProcessManager(logger *zap.Logger) *ProcessManager {
	return &ProcessManager{logger: logger}
}

// ListProcesses returns a list of running processes
func (m *ProcessManager) ListProcesses() ([]protocol.ProcessInfo, error) {
	pids, err := process.Pids()
	if err != nil {
		return nil, fmt.Errorf("list pids: %w", err)
	}

	var processes []protocol.ProcessInfo
	for _, pid := range pids {
		p, err := process.NewProcess(pid)
		if err != nil {
			continue
		}

		name, _ := p.Name()
		cpuPercent, _ := p.CPUPercent()
		memPercent, _ := p.MemoryPercent()
		statusSlice, _ := p.Status()
		statusStr := ""
		if len(statusSlice) > 0 {
			statusStr = statusSlice[0]
		}
		ppid, _ := p.Ppid()

		processes = append(processes, protocol.ProcessInfo{
			PID:    pid,
			Name:   name,
			CPU:    cpuPercent,
			Memory: float64(memPercent),
			Status: statusStr,
			Ppid:   ppid,
		})
	}

	return processes, nil
}

// KillProcess kills a process by PID
func (m *ProcessManager) KillProcess(pid int32, signal string) error {
	p, err := process.NewProcess(pid)
	if err != nil {
		return fmt.Errorf("process %d not found: %w", pid, err)
	}

	return p.Kill()
}

// HandleRequest handles process protocol requests
func (m *ProcessManager) HandleRequest(ctx context.Context, env *protocol.Envelope) (*protocol.Envelope, error) {
	switch env.Action {
	case protocol.ActionProcessList:
		processes, err := m.ListProcesses()
		if err != nil {
			return nil, err
		}
		return protocol.NewResponse(env, protocol.ProcessListResult{Processes: processes})

	case protocol.ActionProcessKill:
		var payload protocol.ProcessKillPayload
		if err := env.DecodePayload(&payload); err != nil {
			return nil, err
		}
		if err := m.KillProcess(payload.PID, payload.Signal); err != nil {
			return nil, err
		}
		return protocol.NewResponse(env, map[string]string{})

	default:
		return nil, fmt.Errorf("unknown process action: %s", env.Action)
	}
}

// Name returns the plugin name
func (m *ProcessManager) Name() string { return "process" }

// Init initializes the plugin
func (m *ProcessManager) Init(a interface{}) error { return nil }

// Shutdown shuts down the plugin
func (m *ProcessManager) Shutdown() error { return nil }
