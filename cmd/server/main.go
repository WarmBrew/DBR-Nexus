package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/qoder/device-mgmt/internal/logging"
	"github.com/qoder/device-mgmt/internal/server"
	"github.com/qoder/device-mgmt/internal/server/database"
	"go.uber.org/zap"
)

func main() {
	// Load config first so we can configure the logger
	configPath := "configs/server.yaml"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	cfg, err := server.LoadConfig(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}
	if cfg == nil {
		cfg = &server.Config{}
		cfg.Server.Addr = ":8443"
		cfg.Database.Path = "./data/qoder.db"
		cfg.Auth.JWTSecret = "change-me-in-production"
		cfg.Auth.JWTExpiry = 86400000000000 // 24h
		cfg.Auth.PSK = "default-psk"
		cfg.Logging.Level = "info"
		cfg.Logging.File = "./logs/server.log"
		cfg.Logging.MaxSize = 100
		cfg.Logging.MaxBackups = 30
		cfg.Logging.MaxAge = 30
		cfg.Tunnel.BindRange = "127.0.0.1:10000-20000"
		cfg.Agent.HeartbeatTimeout = 90000000000 // 90s
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

	// Initialize database
	db, err := database.New(cfg.Database.Path, logger)
	if err != nil {
		logger.Fatal("failed to init database", zap.Error(err))
	}
	defer db.Close()

	// Create server
	srv := server.New(cfg, db, logger)

	// Start server in goroutine
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		if err := srv.Start(ctx); err != nil {
			logger.Fatal("server failed", zap.Error(err))
		}
	}()

	logger.Info("device management server started")

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down server...")
	cancel()

	if err := srv.Shutdown(context.Background()); err != nil {
		logger.Error("server shutdown error", zap.Error(err))
	}

	logger.Info("server stopped")
}
