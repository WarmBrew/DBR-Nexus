package protocol

// Channel constants for message multiplexing
const (
	ChannelSystem  = "system"  // Heartbeat, info, agent lifecycle
	ChannelShell   = "shell"   // Interactive terminal
	ChannelFile    = "file"    // File operations
	ChannelProcess = "process" // Process management
	ChannelTunnel  = "tunnel"  // Port forwarding
	ChannelAudit   = "audit"   // Audit event push
)

// Message type constants
const (
	TypeRequest  = "request"
	TypeResponse = "response"
	TypeStream   = "stream"
	TypeEvent    = "event"
)
