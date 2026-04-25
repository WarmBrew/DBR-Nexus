package agent

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"time"

	"github.com/qoder/device-mgmt/internal/protocol"
	"go.uber.org/zap"
)

// Agent is the core agent struct
type Agent struct {
	config    *Config
	client    *Client
	heartbeat *HeartbeatManager
	plugins   *PluginRegistry
	logger    *zap.Logger
	done      chan struct{}
}

// New creates a new Agent
func New(cfg *Config, logger *zap.Logger) *Agent {
	// Generate device ID if not set
	deviceID := cfg.Device.ID
	if deviceID == "" {
		deviceID = generateStableDeviceID()
	}

	client := NewClient(cfg.Server.URL, cfg.Server.PSK, deviceID, logger)
	heartbeat := NewHeartbeatManager(cfg.Heartbeat.Interval, client, logger)
	plugins := NewPluginRegistry(logger)

	return &Agent{
		config:    cfg,
		client:    client,
		heartbeat: heartbeat,
		plugins:   plugins,
		logger:    logger,
		done:      make(chan struct{}),
	}
}

// Start starts the agent
func (a *Agent) Start(ctx context.Context) error {
	a.logger.Info("agent starting",
		zap.String("device_id", a.client.deviceID),
		zap.String("server", a.config.Server.URL),
	)

	// Set up message handler
	SetMessageHandler(a.handleMessage)

	// Register onConnect callbacks
	a.client.OnConnect(func() {
		// Start heartbeat on connect
		go a.heartbeat.Start(ctx)
	})

	a.client.OnDisconnect(func() {
		// Stop heartbeat on disconnect
		a.heartbeat.Stop()
	})

	// Initialize plugins
	if err := a.plugins.InitAll(a); err != nil {
		return err
	}

	// Connect with retry
	reconnectCfg := a.config.Server.Reconnect
	if reconnectCfg.InitialDelay == 0 {
		reconnectCfg.InitialDelay = 1000000000 // 1s
	}
	if reconnectCfg.MaxDelay == 0 {
		reconnectCfg.MaxDelay = 60000000000 // 60s
	}
	if reconnectCfg.Multiplier == 0 {
		reconnectCfg.Multiplier = 2.0
	}

	// Initial connection
	if err := a.client.ConnectWithRetry(ctx, reconnectCfg); err != nil {
		return err
	}

	// Auto-reconnect loop
	go a.reconnectLoop(ctx)

	a.logger.Info("agent started successfully")
	return nil
}

// Stop stops the agent
func (a *Agent) Stop() {
	a.logger.Info("agent stopping")
	a.heartbeat.Stop()
	a.plugins.ShutdownAll()
	a.client.Close()
	close(a.done)
}

// Client returns the WebSocket client
func (a *Agent) Client() *Client {
	return a.client
}

// Plugins returns the plugin registry
func (a *Agent) Plugins() *PluginRegistry {
	return a.plugins
}

// DeviceID returns the device ID
func (a *Agent) DeviceID() string {
	return a.client.deviceID
}

// handleMessage dispatches incoming messages to plugins
func (a *Agent) handleMessage(env *protocol.Envelope) {
	plugin := a.plugins.Route(env.Channel)
	if plugin == nil {
		a.logger.Warn("no plugin for channel", zap.String("channel", env.Channel))
		return
	}

	ctx := context.Background()
	resp, err := plugin.HandleRequest(ctx, env)
	if err != nil {
		a.logger.Error("plugin handler error",
			zap.String("plugin", plugin.Name()),
			zap.String("action", env.Action),
			zap.Error(err),
		)

		errResp := protocol.NewErrorResponse(env, 500, err.Error())
		a.client.Send(errResp)
		return
	}

	if resp != nil {
		a.client.Send(resp)
	}
}

// reconnectLoop handles reconnection
func (a *Agent) reconnectLoop(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-a.done:
			return
		case <-ticker.C:
			// If disconnected, attempt to reconnect
			if a.client.IsClosed() {
				a.logger.Info("detected disconnection, attempting reconnect")
				reconnectCfg := a.config.Server.Reconnect
				if reconnectCfg.InitialDelay == 0 {
					reconnectCfg.InitialDelay = 1000000000
				}
				if reconnectCfg.MaxDelay == 0 {
					reconnectCfg.MaxDelay = 60000000000
				}
				if reconnectCfg.Multiplier == 0 {
					reconnectCfg.Multiplier = 2.0
				}
				if err := a.client.ConnectWithRetry(ctx, reconnectCfg); err != nil {
					a.logger.Error("reconnect failed", zap.Error(err))
				} else {
					a.logger.Info("reconnected successfully")
				}
			}
		}
	}
}

// generateStableDeviceID generates a deterministic device ID based on hostname.
// This ensures the same machine always gets the same ID across restarts.
func generateStableDeviceID() string {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}
	hash := sha256.Sum256([]byte(hostname))
	return fmt.Sprintf("%x-%x-%x-%x-%x", hash[:4], hash[4:6], hash[6:8], hash[8:10], hash[10:16])
}
