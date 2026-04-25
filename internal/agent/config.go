package agent

import (
	"time"

	"github.com/spf13/viper"
)

// Config holds the agent configuration
type Config struct {
	Server     ServerConfig     `mapstructure:"server"`
	Heartbeat  HeartbeatConfig  `mapstructure:"heartbeat"`
	Device     DeviceConfig     `mapstructure:"device"`
	Logging    LoggingConfig    `mapstructure:"logging"`
	Tunnel     TunnelConfig     `mapstructure:"tunnel"`
	Encryption EncryptionConfig `mapstructure:"encryption"`
}

type EncryptionConfig struct {
	Enabled       bool `mapstructure:"enabled"`
	JitterPercent int  `mapstructure:"timing_jitter_percent"` // default 15
}

type ServerConfig struct {
	URL       string          `mapstructure:"url"`
	PSK       string          `mapstructure:"psk"`
	Reconnect ReconnectConfig `mapstructure:"reconnect"`
}

type ReconnectConfig struct {
	InitialDelay time.Duration `mapstructure:"initial_delay"`
	MaxDelay     time.Duration `mapstructure:"max_delay"`
	Multiplier   float64       `mapstructure:"multiplier"`
}

type HeartbeatConfig struct {
	Interval time.Duration `mapstructure:"interval"`
}

type DeviceConfig struct {
	ID     string   `mapstructure:"id"`
	Labels []string `mapstructure:"labels"`
	Tags   []string `mapstructure:"tags"`
}

type LoggingConfig struct {
	Level      string `mapstructure:"level"`       // debug, info, warn, error
	File       string `mapstructure:"file"`        // log file path, empty = no file logging
	MaxSize    int    `mapstructure:"max_size"`    // max size in MB before rotation (default 100)
	MaxBackups int    `mapstructure:"max_backups"` // max number of old log files to keep (default 30)
	MaxAge     int    `mapstructure:"max_age"`     // max days to retain old log files (default 30)
}

type TunnelConfig struct {
	MaxConnections int `mapstructure:"max_connections"`
}

// LoadConfig loads configuration from file
func LoadConfig(path string) (*Config, error) {
	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("yaml")

	v.SetDefault("server.url", "ws://localhost:8443/ws/agent")
	v.SetDefault("server.psk", "default-psk")
	v.SetDefault("server.reconnect.initial_delay", "1s")
	v.SetDefault("server.reconnect.max_delay", "60s")
	v.SetDefault("server.reconnect.multiplier", 2.0)
	v.SetDefault("heartbeat.interval", "30s")
	v.SetDefault("device.id", "")
	v.SetDefault("logging.level", "info")
	v.SetDefault("logging.file", "./logs/agent.log")
	v.SetDefault("logging.max_size", 50)
	v.SetDefault("logging.max_backups", 10)
	v.SetDefault("logging.max_age", 7)
	v.SetDefault("tunnel.max_connections", 100)
	v.SetDefault("encryption.enabled", true)
	v.SetDefault("encryption.timing_jitter_percent", 15)

	if err := v.ReadInConfig(); err != nil {
		return nil, nil
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}
