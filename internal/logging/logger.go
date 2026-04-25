package logging

import (
	"os"
	"path/filepath"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/lumberjack.v2"
)

// Config holds logging configuration shared by server and agent.
type Config struct {
	Level      string `mapstructure:"level"`       // debug, info, warn, error
	File       string `mapstructure:"file"`        // log file path, empty = no file logging
	MaxSize    int    `mapstructure:"max_size"`    // max size in MB before rotation (default 100)
	MaxBackups int    `mapstructure:"max_backups"` // max number of old log files to keep (default 30)
	MaxAge     int    `mapstructure:"max_age"`     // max days to retain old log files (default 30)
}

// NewLogger creates a zap.Logger that writes to both console and file (if configured).
func NewLogger(cfg Config) (*zap.Logger, error) {
	level := parseLevel(cfg.Level)

	encoderCfg := zap.NewProductionEncoderConfig()
	encoderCfg.TimeKey = "ts"
	encoderCfg.EncodeTime = zapcore.ISO8601TimeEncoder
	encoderCfg.EncodeLevel = zapcore.CapitalLevelEncoder

	cores := []zapcore.Core{}

	// Console core - human-readable format
	consoleEncoder := zapcore.NewConsoleEncoder(encoderCfg)
	consoleCore := zapcore.NewCore(
		consoleEncoder,
		zapcore.AddSync(os.Stdout),
		level,
	)
	cores = append(cores, consoleCore)

	// File core - JSON format for machine parsing, with rotation
	if cfg.File != "" {
		if err := os.MkdirAll(filepath.Dir(cfg.File), 0755); err != nil {
			return nil, err
		}

		maxSize := cfg.MaxSize
		if maxSize <= 0 {
			maxSize = 100
		}
		maxBackups := cfg.MaxBackups
		if maxBackups <= 0 {
			maxBackups = 30
		}
		maxAge := cfg.MaxAge
		if maxAge <= 0 {
			maxAge = 30
		}

		writer := &lumberjack.Logger{
			Filename:   cfg.File,
			MaxSize:    maxSize,
			MaxBackups: maxBackups,
			MaxAge:     maxAge,
			Compress:   true,
		}

		fileEncoder := zapcore.NewJSONEncoder(encoderCfg)
		fileCore := zapcore.NewCore(
			fileEncoder,
			zapcore.AddSync(writer),
			level,
		)
		cores = append(cores, fileCore)
	}

	core := zapcore.NewTee(cores...)
	return zap.New(core, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel)), nil
}

func parseLevel(s string) zapcore.Level {
	switch s {
	case "debug":
		return zapcore.DebugLevel
	case "info":
		return zapcore.InfoLevel
	case "warn":
		return zapcore.WarnLevel
	case "error":
		return zapcore.ErrorLevel
	default:
		return zapcore.InfoLevel
	}
}
