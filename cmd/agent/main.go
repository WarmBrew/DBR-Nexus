package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/qoder/device-mgmt/internal/agent"
	"github.com/qoder/device-mgmt/internal/agent/executor"
	"github.com/qoder/device-mgmt/internal/agent/filemanager"
	"github.com/qoder/device-mgmt/internal/agent/tunnel"
	"github.com/qoder/device-mgmt/internal/logging"
	"github.com/qoder/device-mgmt/internal/protocol"
	"go.uber.org/zap"
)

// These variables can be overridden via -ldflags at build time
// Example: go build -ldflags "-X main.defaultServerURL=ws://1.2.3.4:8443/ws/agent -X main.defaultPSK=mypsk"
var defaultServerURL string
var defaultPSK string

func main() {
	// Load config first so we can configure the logger
	configPath := "configs/agent.yaml"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	cfg, err := agent.LoadConfig(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}
	if cfg == nil {
		cfg = &agent.Config{}
		cfg.Server.URL = "ws://localhost:8443/ws/agent"
		cfg.Server.PSK = "default-psk"
		cfg.Server.Reconnect = agent.ReconnectConfig{
			InitialDelay: 1000000000,
			MaxDelay:     60000000000,
			Multiplier:   2.0,
		}
		cfg.Heartbeat.Interval = 30000000000
		cfg.Device.ID = ""
		cfg.Logging.Level = "info"
		cfg.Logging.File = "./logs/agent.log"
		cfg.Logging.MaxSize = 50
		cfg.Logging.MaxBackups = 10
		cfg.Logging.MaxAge = 7
		cfg.Tunnel.MaxConnections = 100
	}

	// Override config with ldflags-injected values
	if defaultServerURL != "" {
		cfg.Server.URL = defaultServerURL
	}
	if defaultPSK != "" {
		cfg.Server.PSK = defaultPSK
	}

	// Initialize logger with file rotation from config
	logger, err := logging.NewLogger(logging.Config{
		Level:      cfg.Logging.Level,
		File:       cfg.Logging.File,
		MaxSize:    cfg.Logging.MaxSize,
		MaxBackups: cfg.Logging.MaxBackups,
		MaxAge:     cfg.Logging.MaxAge,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to init logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	// Create agent
	a := agent.New(cfg, logger)

	// Register plugins
	sendFn := func(env *protocol.Envelope) error {
		return a.Client().Send(env)
	}

	shellMgr := executor.NewShellManager(logger, sendFn)
	processMgr := executor.NewProcessManager(logger)
	systemCollector := executor.NewSystemCollector(logger)
	fileMgr := filemanager.NewManager(logger, sendFn)
	tunnelForwarder := tunnel.NewForwarder(logger, &wsClientAdapter{client: a.Client()})

	a.Plugins().Register(shellMgr)
	a.Plugins().Register(processMgr)
	a.Plugins().Register(systemCollector)
	a.Plugins().Register(fileMgr)
	a.Plugins().Register(tunnelForwarder)

	// Start agent
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		if err := a.Start(ctx); err != nil {
			logger.Fatal("agent failed", zap.Error(err))
		}
	}()

	logger.Info("device management agent started")

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down agent...")
	cancel()
	a.Stop()

	logger.Info("agent stopped")
}

// wsClientAdapter adapts agent.Client to tunnel.WSClient interface
type wsClientAdapter struct {
	client *agent.Client
}

func (a *wsClientAdapter) Send(env *protocol.Envelope) error {
	return a.client.Send(env)
}

func (a *wsClientAdapter) Request(env *protocol.Envelope) (*protocol.Envelope, error) {
	return a.client.Request(context.Background(), env, 30*time.Second)
}
