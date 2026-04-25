package agent

import (
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/mem"
)

func collectRealMetrics(m *Metrics) {
	// CPU usage
	if percentages, err := cpu.Percent(0, false); err == nil && len(percentages) > 0 {
		m.CPU = percentages[0]
	}

	// Memory usage
	if vm, err := mem.VirtualMemory(); err == nil {
		m.Memory = vm.UsedPercent
	}

	// Disk usage (root partition)
	if usage, err := disk.Usage("/"); err == nil {
		m.Disk = usage.UsedPercent
	}

	// Uptime
	if info, err := host.Info(); err == nil {
		m.Uptime = int64(info.Uptime)
	}

	// Load averages (Unix only, will return 0 on Windows)
	if avg, err := load.Avg(); err == nil {
		m.Load1 = avg.Load1
		m.Load5 = avg.Load5
		m.Load15 = avg.Load15
	}
}

func init() {
	// Ensure cpuPercent initialization
	cpu.Percent(1*time.Second, false)
}
