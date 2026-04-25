package agent

import (
	"context"
	"time"

	"github.com/qoder/device-mgmt/internal/protocol"
	"go.uber.org/zap"
)

// HeartbeatManager handles periodic heartbeat sending
type HeartbeatManager struct {
	interval   time.Duration
	client     *Client
	logger     *zap.Logger
	cancelFunc context.CancelFunc
}

// NewHeartbeatManager creates a new heartbeat manager
func NewHeartbeatManager(interval time.Duration, client *Client, logger *zap.Logger) *HeartbeatManager {
	return &HeartbeatManager{
		interval: interval,
		client:   client,
		logger:   logger,
	}
}

// Start begins the heartbeat loop
func (h *HeartbeatManager) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	h.cancelFunc = cancel

	// Ensure interval is valid
	interval := h.interval
	if interval <= 0 {
		interval = 30 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	h.logger.Info("heartbeat started", zap.Duration("interval", interval))

	// Send first heartbeat immediately
	h.sendHeartbeat(ctx)

	for {
		select {
		case <-ctx.Done():
			h.logger.Info("heartbeat stopped")
			return
		case <-ticker.C:
			h.sendHeartbeat(ctx)
		}
	}
}

// Stop stops the heartbeat loop
func (h *HeartbeatManager) Stop() {
	if h.cancelFunc != nil {
		h.cancelFunc()
	}
}

func (h *HeartbeatManager) sendHeartbeat(ctx context.Context) {
	metrics := collectMetrics()

	env, err := protocol.NewEnvelope(
		protocol.GenerateID(),
		protocol.ChannelSystem,
		protocol.TypeEvent,
		protocol.ActionSystemHeartbeat,
		protocol.HeartbeatPayload{
			DeviceID: h.client.deviceID,
			CPU:      metrics.CPU,
			Memory:   metrics.Memory,
			Disk:     metrics.Disk,
			Uptime:   metrics.Uptime,
			Load1:    metrics.Load1,
			Load5:    metrics.Load5,
			Load15:   metrics.Load15,
		},
	)
	if err != nil {
		h.logger.Error("create heartbeat envelope", zap.Error(err))
		return
	}

	if err := h.client.Send(env); err != nil {
		h.logger.Warn("send heartbeat failed", zap.Error(err))
		return
	}
}

// Metrics holds system metrics
type Metrics struct {
	CPU    float64
	Memory float64
	Disk   float64
	Uptime int64
	Load1  float64
	Load5  float64
	Load15 float64
}

// collectMetrics collects current system metrics
func collectMetrics() *Metrics {
	m := &Metrics{}

	// Use gopsutil to collect real metrics
	// For now, return placeholder values
	m.CPU = 0
	m.Memory = 0
	m.Disk = 0
	m.Uptime = 0
	m.Load1 = 0
	m.Load5 = 0
	m.Load15 = 0

	// Try to get real metrics
	collectRealMetrics(m)

	return m
}
