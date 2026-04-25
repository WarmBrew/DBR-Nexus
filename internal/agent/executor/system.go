package executor

import (
	"context"
	"fmt"
	"runtime"

	"github.com/qoder/device-mgmt/internal/protocol"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
	"go.uber.org/zap"
)

// SystemCollector collects system information
type SystemCollector struct {
	logger *zap.Logger
}

// NewSystemCollector creates a new system collector
func NewSystemCollector(logger *zap.Logger) *SystemCollector {
	return &SystemCollector{logger: logger}
}

// Collect gathers full system information
func (s *SystemCollector) Collect() (*protocol.SystemInfo, error) {
	info := &protocol.SystemInfo{
		Arch: runtime.GOARCH,
		OS:   runtime.GOOS,
	}

	// Host info
	if hostInfo, err := host.Info(); err == nil {
		info.Hostname = hostInfo.Hostname
		info.Kernel = hostInfo.KernelVersion
		info.Uptime = int64(hostInfo.Uptime)
	}

	// CPU count
	if cpuInfo, err := cpu.Info(); err == nil && len(cpuInfo) > 0 {
		info.CPUs = len(cpuInfo)
	}
	if counts, err := cpu.Counts(true); err == nil {
		info.CPUs = counts
	}

	// Memory
	if vm, err := mem.VirtualMemory(); err == nil {
		info.Memory = protocol.MemoryInfo{
			Total:       vm.Total,
			Used:        vm.Used,
			UsedPercent: vm.UsedPercent,
		}
	}

	// Disk
	if partitions, err := disk.Partitions(false); err == nil {
		for _, p := range partitions {
			if usage, err := disk.Usage(p.Mountpoint); err == nil {
				info.Disks = append(info.Disks, protocol.DiskInfo{
					Device:      p.Device,
					Mountpoint:  p.Mountpoint,
					Total:       usage.Total,
					Used:        usage.Used,
					UsedPercent: usage.UsedPercent,
				})
			}
		}
	}

	// Network
	if interfaces, err := net.Interfaces(); err == nil {
		for _, iface := range interfaces {
			if len(iface.Addrs) > 0 {
				var ips []string
				for _, addr := range iface.Addrs {
					ips = append(ips, addr.Addr)
				}
				info.Network = append(info.Network, protocol.NetworkInterface{
					Name: iface.Name,
					IPs:  ips,
				})
			}
		}
	}

	return info, nil
}

// HandleRequest handles system protocol requests
func (s *SystemCollector) HandleRequest(ctx context.Context, env *protocol.Envelope) (*protocol.Envelope, error) {
	switch env.Action {
	case protocol.ActionSystemInfo:
		info, err := s.Collect()
		if err != nil {
			return nil, err
		}
		return protocol.NewResponse(env, info)

	case protocol.ActionSystemRestart:
		// Agent restart is handled by the main agent loop
		return protocol.NewResponse(env, map[string]string{"status": "restart_initiated"})

	default:
		return nil, fmt.Errorf("unknown system action: %s", env.Action)
	}
}

// Name returns the plugin name
func (s *SystemCollector) Name() string { return "system" }

// Init initializes the plugin
func (s *SystemCollector) Init(a interface{}) error { return nil }

// Shutdown shuts down the plugin
func (s *SystemCollector) Shutdown() error { return nil }
