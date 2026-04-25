package protocol

// ---- System payloads ----

// SystemInfo is the full system snapshot
type SystemInfo struct {
	Hostname string             `json:"hostname"`
	OS       string             `json:"os"`
	Arch     string             `json:"arch"`
	Kernel   string             `json:"kernel"`
	CPUs     int                `json:"cpus"`
	Memory   MemoryInfo         `json:"memory"`
	Disks    []DiskInfo         `json:"disks"`
	Network  []NetworkInterface `json:"network"`
	Uptime   int64              `json:"uptime"`
}

type MemoryInfo struct {
	Total       uint64  `json:"total"`
	Used        uint64  `json:"used"`
	UsedPercent float64 `json:"used_percent"`
}

type DiskInfo struct {
	Device      string  `json:"device"`
	Mountpoint  string  `json:"mountpoint"`
	Total       uint64  `json:"total"`
	Used        uint64  `json:"used"`
	UsedPercent float64 `json:"used_percent"`
}

type NetworkInterface struct {
	Name string   `json:"name"`
	IPs  []string `json:"ips"`
}

// HeartbeatPayload is the periodic metrics push from agent
type HeartbeatPayload struct {
	DeviceID string  `json:"device_id"`
	CPU      float64 `json:"cpu"`
	Memory   float64 `json:"mem"`
	Disk     float64 `json:"disk"`
	Uptime   int64   `json:"uptime"`
	Load1    float64 `json:"load1"`
	Load5    float64 `json:"load5"`
	Load15   float64 `json:"load15"`
}

// DeviceOnlinePayload is sent when a device comes online
type DeviceOnlinePayload struct {
	DeviceID    string `json:"device_id"`
	Hostname    string `json:"hostname"`
	IP          string `json:"ip"`
	OS          string `json:"os"`
	ConnectedAt int64  `json:"connected_at"`
}

// DeviceOfflinePayload is sent when a device goes offline
type DeviceOfflinePayload struct {
	DeviceID       string `json:"device_id"`
	Reason         string `json:"reason"`
	DisconnectedAt int64  `json:"disconnected_at"`
}

// AuthChallengePayload is the PSK challenge
type AuthChallengePayload struct {
	Nonce        string `json:"nonce"`
	ServerPubKey string `json:"server_pub_key,omitempty"` // X25519 public key (base64), v2 encryption
	ServerNonce  string `json:"server_nonce,omitempty"`   // 16-byte nonce for key derivation (base64), v2
	Version      int    `json:"version,omitempty"`        // Protocol version: 2 = encryption supported
}

// AuthResponsePayload is the agent's response to PSK challenge
type AuthResponsePayload struct {
	DeviceID     string `json:"device_id"`
	HMAC         string `json:"hmac"`
	AgentVersion string `json:"agent_version"`
	Hostname     string `json:"hostname"`
	OS           string `json:"os"`
	Arch         string `json:"arch"`
	IPInternal   string `json:"ip_internal"`
	AgentPubKey  string `json:"agent_pub_key,omitempty"` // X25519 public key (base64), v2 encryption
	AgentNonce   string `json:"agent_nonce,omitempty"`   // 16-byte nonce for key derivation (base64), v2
}

// ---- Shell payloads ----

// ShellStartPayload opens a PTY session
type ShellStartPayload struct {
	SessionID string `json:"session_id"`
	Cols      uint16 `json:"cols"`
	Rows      uint16 `json:"rows"`
	ShellPath string `json:"shell_path,omitempty"`
}

// ShellInputPayload sends keystrokes to PTY
type ShellInputPayload struct {
	SessionID string `json:"session_id"`
	Data      string `json:"data"` // base64 encoded
}

// ShellResizePayload resizes the PTY
type ShellResizePayload struct {
	SessionID string `json:"session_id"`
	Cols      uint16 `json:"cols"`
	Rows      uint16 `json:"rows"`
}

// ShellOutputPayload is streaming PTY output
type ShellOutputPayload struct {
	DeviceID  string `json:"device_id"`
	SessionID string `json:"session_id"`
	Data      string `json:"data"` // base64 encoded
}

// ShellClosePayload closes a PTY session
type ShellClosePayload struct {
	SessionID string `json:"session_id"`
}

// ---- File payloads ----

// FileBrowsePayload requests directory listing
type FileBrowsePayload struct {
	Path string `json:"path"`
}

// FileBrowseResult is the directory listing response
type FileBrowseResult struct {
	Path    string      `json:"path"`
	Entries []FileEntry `json:"entries"`
}

// FileEntry represents a file or directory entry
type FileEntry struct {
	Name    string `json:"name"`
	Path    string `json:"path,omitempty"`
	Type    string `json:"type"` // "dir" or "file"
	Size    int64  `json:"size"`
	Mode    string `json:"mode"`
	ModTime string `json:"mod_time"`
	UID     int    `json:"uid,omitempty"`
	GID     int    `json:"gid,omitempty"`
}

// FileStatPayload requests file metadata
type FileStatPayload struct {
	Path string `json:"path"`
}

// FileUploadStartPayload initiates a chunked upload
type FileUploadStartPayload struct {
	TransferID string `json:"transfer_id"`
	Path       string `json:"path"`
	Size       int64  `json:"size"`
}

// FileUploadAckPayload acknowledges an upload start
type FileUploadAckPayload struct {
	TransferID string `json:"transfer_id"`
	ChunkSize  int    `json:"chunk_size"`
}

// FileUploadChunkPayload is a chunk of file data
type FileUploadChunkPayload struct {
	TransferID string `json:"transfer_id"`
	Index      int    `json:"index"`
	Data       string `json:"data"` // base64 encoded
}

// FileUploadDonePayload signals upload completion
type FileUploadDonePayload struct {
	TransferID string `json:"transfer_id"`
}

// FileUploadDoneResult is the upload completion response
type FileUploadDoneResult struct {
	TransferID string `json:"transfer_id"`
	SHA256     string `json:"sha256"`
}

// FileDownloadStartPayload initiates a chunked download
type FileDownloadStartPayload struct {
	TransferID string `json:"transfer_id"`
	Path       string `json:"path"`
}

// FileDownloadStartResult is the download start response
type FileDownloadStartResult struct {
	TransferID string `json:"transfer_id"`
	TotalSize  int64  `json:"total_size"`
	ChunkSize  int    `json:"chunk_size"`
}

// FileDownloadChunkPayload is a chunk of downloaded data
type FileDownloadChunkPayload struct {
	TransferID string `json:"transfer_id"`
	Index      int    `json:"index"`
	Data       string `json:"data"` // base64 encoded
}

// FileDownloadAckPayload acknowledges a downloaded chunk
type FileDownloadAckPayload struct {
	TransferID string `json:"transfer_id"`
	Index      int    `json:"index"`
}

// FileReadPayload reads file content for editing
type FileReadPayload struct {
	Path string `json:"path"`
}

// FileReadResult is the file content response
type FileReadResult struct {
	Path     string `json:"path"`
	Content  string `json:"content"`
	Size     int64  `json:"size"`
	Language string `json:"language"`
}

// FileWritePayload writes file content
type FileWritePayload struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// FileDeletePayload deletes a file or directory
type FileDeletePayload struct {
	Path string `json:"path"`
}

// FileMkdirPayload creates a directory
type FileMkdirPayload struct {
	Path string `json:"path"`
}

// FileMovePayload moves/renames a file
type FileMovePayload struct {
	Src string `json:"src"`
	Dst string `json:"dst"`
}

// FileChmodPayload changes file permissions
type FileChmodPayload struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
}

// FileSearchPayload searches for files matching a pattern
type FileSearchPayload struct {
	Path     string `json:"path"`
	Pattern  string `json:"pattern"`
	MaxDepth int    `json:"max_depth,omitempty"`
}

// FileDownloadDirPayload requests directory download as ZIP
type FileDownloadDirPayload struct {
	TransferID string `json:"transfer_id"`
	Path       string `json:"path"`
}

// FileDownloadDirStartResult is the directory download start response
type FileDownloadDirStartResult struct {
	TransferID string `json:"transfer_id"`
	TotalSize  int64  `json:"total_size"`
	ChunkSize  int    `json:"chunk_size"`
	Name       string `json:"name"`
}

// FileCreatePayload creates a new empty file
type FileCreatePayload struct {
	Path string `json:"path"`
}

// FileProgressPayload reports transfer progress
type FileProgressPayload struct {
	DeviceID   string `json:"device_id"`
	TransferID string `json:"transfer_id"`
	BytesDone  int64  `json:"bytes_done"`
	BytesTotal int64  `json:"bytes_total"`
}

// ---- Process payloads ----

// ProcessListResult is the process list response
type ProcessListResult struct {
	Processes []ProcessInfo `json:"processes"`
}

// ProcessInfo represents a running process
type ProcessInfo struct {
	PID    int32   `json:"pid"`
	Name   string  `json:"name"`
	CPU    float64 `json:"cpu"`
	Memory float64 `json:"mem"`
	Status string  `json:"status"`
	Ppid   int32   `json:"ppid"`
}

// ProcessKillPayload kills a process
type ProcessKillPayload struct {
	PID    int32  `json:"pid"`
	Signal string `json:"signal,omitempty"`
}

// ProcessTopPayload requests streaming top stats
type ProcessTopPayload struct {
	IntervalMs int `json:"interval_ms"`
}

// ---- Tunnel payloads ----

// TunnelOpenPayload opens a tunnel
type TunnelOpenPayload struct {
	TunnelID   string `json:"tunnel_id"`
	RemoteAddr string `json:"remote_addr"`
}

// TunnelClosePayload closes a tunnel
type TunnelClosePayload struct {
	TunnelID string `json:"tunnel_id"`
}

// TunnelDataPayload carries TCP data through the tunnel
type TunnelDataPayload struct {
	TunnelID string `json:"tunnel_id"`
	ConnID   string `json:"conn_id"`
	Data     string `json:"data"` // base64 encoded
}

// TunnelAckPayload acknowledges data receipt
type TunnelAckPayload struct {
	TunnelID string `json:"tunnel_id"`
	ConnID   string `json:"conn_id"`
	Bytes    int    `json:"bytes"`
}

// TunnelStatusPayload reports tunnel status change
type TunnelStatusPayload struct {
	TunnelID   string `json:"tunnel_id"`
	State      string `json:"state"`
	LocalAddr  string `json:"local_addr,omitempty"`
	RemoteAddr string `json:"remote_addr,omitempty"`
}

// CreateTunnelRequest is the REST API request for creating a tunnel
type CreateTunnelRequest struct {
	DeviceID   string `json:"device_id"`
	RemoteAddr string `json:"remote_addr"`
}

// CreateTunnelResult is the REST API response for creating a tunnel
type CreateTunnelResult struct {
	ID         string  `json:"id"`
	LocalAddr  string  `json:"local_addr"`
	RemoteAddr string  `json:"remote_addr"`
	ExpiresAt  *string `json:"expires_at,omitempty"`
}

// ---- SOCKS5 payloads ----

// Socks5OpenPayload opens a SOCKS5 connection to a remote address
type Socks5OpenPayload struct {
	ConnID     string `json:"conn_id"`
	RemoteAddr string `json:"remote_addr"`
}

// Socks5DataPayload carries TCP data through SOCKS5 tunnel
type Socks5DataPayload struct {
	ConnID string `json:"conn_id"`
	Data   string `json:"data"` // base64 encoded
}

// Socks5ClosePayload closes a SOCKS5 connection
type Socks5ClosePayload struct {
	ConnID string `json:"conn_id"`
}

// Socks5OpenResult is the response for SOCKS5 open
type Socks5OpenResult struct {
	ConnID string `json:"conn_id"`
	OK     bool   `json:"ok"`
	Error  string `json:"error,omitempty"`
}
