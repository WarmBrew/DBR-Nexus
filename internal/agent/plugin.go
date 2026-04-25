package agent

import (
	"context"

	"github.com/qoder/device-mgmt/internal/protocol"
	"go.uber.org/zap"
)

// Plugin is the interface for agent capability plugins
type Plugin interface {
	Name() string
	Init(a interface{}) error
	HandleRequest(ctx context.Context, env *protocol.Envelope) (*protocol.Envelope, error)
	Shutdown() error
}

// PluginRegistry manages agent plugins
type PluginRegistry struct {
	plugins map[string]Plugin
	logger  *zap.Logger
}

// NewPluginRegistry creates a new plugin registry
func NewPluginRegistry(logger *zap.Logger) *PluginRegistry {
	return &PluginRegistry{
		plugins: make(map[string]Plugin),
		logger:  logger,
	}
}

// Register adds a plugin to the registry
func (r *PluginRegistry) Register(p Plugin) {
	r.plugins[p.Name()] = p
	r.logger.Info("plugin registered", zap.String("name", p.Name()))
}

// Get retrieves a plugin by name
func (r *PluginRegistry) Get(name string) (Plugin, bool) {
	p, ok := r.plugins[name]
	return p, ok
}

// Route finds the plugin that handles the given channel
func (r *PluginRegistry) Route(channel string) Plugin {
	// Map channels to plugin names
	channelToPlugin := map[string]string{
		protocol.ChannelSystem:  "system",
		protocol.ChannelShell:   "shell",
		protocol.ChannelFile:    "file",
		protocol.ChannelProcess: "process",
		protocol.ChannelTunnel:  "tunnel",
	}

	if pluginName, ok := channelToPlugin[channel]; ok {
		if p, exists := r.plugins[pluginName]; exists {
			return p
		}
	}

	return nil
}

// InitAll initializes all registered plugins
func (r *PluginRegistry) InitAll(a *Agent) error {
	for _, p := range r.plugins {
		if err := p.Init(a); err != nil {
			return err
		}
	}
	return nil
}

// ShutdownAll shuts down all plugins
func (r *PluginRegistry) ShutdownAll() {
	for _, p := range r.plugins {
		if err := p.Shutdown(); err != nil {
			r.logger.Error("plugin shutdown error", zap.String("name", p.Name()), zap.Error(err))
		}
	}
}
