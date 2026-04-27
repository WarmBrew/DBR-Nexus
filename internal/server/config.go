package server

import (
	"time"

	"github.com/spf13/viper"
)

// Config holds the server configuration
type Config struct {
	Server           ServerConfig           `mapstructure:"server"`
	Database         DatabaseConfig         `mapstructure:"database"`
	Auth             AuthConfig             `mapstructure:"auth"`
	Logging          LoggingConfig          `mapstructure:"logging"`
	Tunnel           TunnelConfig           `mapstructure:"tunnel"`
	Agent            AgentConfig            `mapstructure:"agent"`
	WebAccessControl WebAccessControlConfig `mapstructure:"web_access_control"`
	Encryption       EncryptionConfig       `mapstructure:"encryption"`
}

type ServerConfig struct {
	Addr string    `mapstructure:"addr"`
	TLS  TLSConfig `mapstructure:"tls"`
}

type TLSConfig struct {
	Cert string `mapstructure:"cert"`
	Key  string `mapstructure:"key"`
}

type DatabaseConfig struct {
	Path string `mapstructure:"path"`
}

type AuthConfig struct {
	JWTSecret string        `mapstructure:"jwt_secret"`
	JWTExpiry time.Duration `mapstructure:"jwt_expiry"`
	PSK       string        `mapstructure:"psk"`
}

type LoggingConfig struct {
	Level      string `mapstructure:"level"`       // debug, info, warn, error
	File       string `mapstructure:"file"`        // log file path, empty = no file logging
	MaxSize    int    `mapstructure:"max_size"`    // max size in MB before rotation (default 100)
	MaxBackups int    `mapstructure:"max_backups"` // max number of old log files to keep (default 30)
	MaxAge     int    `mapstructure:"max_age"`     // max days to retain old log files (default 30)
}

type TunnelConfig struct {
	BindRange          string        `mapstructure:"bind_range"`
	PortForwardTimeout time.Duration `mapstructure:"port_forward_timeout"`
	Socks5Timeout      time.Duration `mapstructure:"socks5_timeout"`
}

type AgentConfig struct {
	HeartbeatTimeout time.Duration `mapstructure:"heartbeat_timeout"`
}

// WebAccessControlConfig controls web UI access by source IP
type WebAccessControlConfig struct {
	Enabled    bool   `mapstructure:"enabled"`
	AllowedIPs string `mapstructure:"allowed_ips"` // comma-separated IPs and CIDRs
}

// EncryptionConfig controls application-layer traffic encryption
type EncryptionConfig struct {
	Enabled          bool `mapstructure:"enabled"`
	MinDummyInterval int  `mapstructure:"min_dummy_interval_ms"` // ms, default 5000
	MaxDummyInterval int  `mapstructure:"max_dummy_interval_ms"` // ms, default 15000
	JitterPercent    int  `mapstructure:"timing_jitter_percent"` // default 15
	MinFrameSize     int  `mapstructure:"min_frame_size"`        // default 128
}

// LoadConfig loads configuration from file and environment
func LoadConfig(path string) (*Config, error) {
	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("yaml")

	// Defaults
	v.SetDefault("server.addr", ":8443")
	v.SetDefault("database.path", "./data/qoder.db")
	v.SetDefault("auth.jwt_secret", "change-me-in-production")
	v.SetDefault("auth.jwt_expiry", "24h")
	v.SetDefault("auth.psk", "default-psk")
	v.SetDefault("logging.level", "info")
	v.SetDefault("logging.file", "./logs/server.log")
	v.SetDefault("logging.max_size", 100)
	v.SetDefault("logging.max_backups", 30)
	v.SetDefault("logging.max_age", 30)
	v.SetDefault("tunnel.bind_range", "127.0.0.1:10000-20000")
	v.SetDefault("tunnel.port_forward_timeout", "30m")
	v.SetDefault("tunnel.socks5_timeout", "1h")
	v.SetDefault("agent.heartbeat_timeout", "90s")
	v.SetDefault("web_access_control.enabled", false)
	v.SetDefault("web_access_control.allowed_ips", "")
	v.SetDefault("encryption.enabled", true)
	v.SetDefault("encryption.min_dummy_interval_ms", 5000)
	v.SetDefault("encryption.max_dummy_interval_ms", 15000)
	v.SetDefault("encryption.timing_jitter_percent", 15)
	v.SetDefault("encryption.min_frame_size", 128)

	if err := v.ReadInConfig(); err != nil {
		// Config file is optional, use defaults
		return nil, nil
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}
