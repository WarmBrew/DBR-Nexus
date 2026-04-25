package protocol

// System action constants
const (
	ActionSystemInfo      = "system.info"
	ActionSystemHeartbeat = "system.heartbeat"
	ActionSystemRestart   = "system.restart"
	ActionSystemUpgrade   = "system.upgrade"
	ActionAuthChallenge   = "auth.challenge"
	ActionAuthResponse    = "auth.response"
	ActionDeviceOnline    = "device.online"
	ActionDeviceOffline   = "device.offline"
	ActionSystemReconnect = "system.reconnect"
)

// Shell action constants
const (
	ActionShellStart  = "shell.start"
	ActionShellInput  = "shell.input"
	ActionShellResize = "shell.resize"
	ActionShellOutput = "shell.output"
	ActionShellClose  = "shell.close"
)

// File action constants
const (
	ActionFileBrowse        = "file.browse"
	ActionFileStat          = "file.stat"
	ActionFileUploadStart   = "file.upload.start"
	ActionFileUploadChunk   = "file.upload.chunk"
	ActionFileUploadAck     = "file.upload.ack"
	ActionFileUploadDone    = "file.upload.done"
	ActionFileDownloadStart = "file.download.start"
	ActionFileDownloadChunk = "file.download.chunk"
	ActionFileDownloadAck   = "file.download.ack"
	ActionFileDownloadDone  = "file.download.done"
	ActionFileRead          = "file.read"
	ActionFileWrite         = "file.write"
	ActionFileDelete        = "file.delete"
	ActionFileMkdir         = "file.mkdir"
	ActionFileMove          = "file.move"
	ActionFileChmod         = "file.chmod"
	ActionFileSearch        = "file.search"
	ActionFileProgress      = "file.progress"
	ActionFileDownloadDir   = "file.download.dir"
	ActionFileCreate        = "file.create"
)

// Process action constants
const (
	ActionProcessList = "process.list"
	ActionProcessKill = "process.kill"
	ActionProcessTop  = "process.top"
)

// Tunnel action constants
const (
	ActionTunnelOpen      = "tunnel.open"
	ActionTunnelClose     = "tunnel.close"
	ActionTunnelData      = "tunnel.data"
	ActionTunnelConnClose = "tunnel.conn.close"
	ActionTunnelAck       = "tunnel.ack"
	ActionTunnelStatus    = "tunnel.status"
	ActionSocks5Open      = "socks5.open"
	ActionSocks5Data      = "socks5.data"
	ActionSocks5Close     = "socks5.close"
)
