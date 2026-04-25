// API type definitions

export interface User {
  id: string;
  username: string;
  display_name: string;
  role: 'admin' | 'operator' | 'viewer';
  enabled: boolean;
}

export interface Device {
  id: string;
  hostname: string;
  os: string;
  arch: string;
  kernel: string;
  ip: string;
  ip_internal?: string;
  ip_location?: string;
  agent_version: string;
  labels: string[];
  tags: string[];
  notes: string;
  status: 'online' | 'offline';
  last_seen_at: string | null;
  system_info: string | null;
  registered_at: string;
  updated_at: string;
}

export interface FileEntry {
  name: string;
  path?: string;
  type: 'dir' | 'file';
  size: number;
  mode: string;
  mod_time: string;
  uid?: number;
  gid?: number;
}

export interface FileBrowseResult {
  path: string;
  entries: FileEntry[];
}

export interface ProcessInfo {
  pid: number;
  name: string;
  cpu: number;
  mem: number;
  status: string;
  ppid: number;
}

export interface Tunnel {
  id: string;
  device_id: string;
  local_addr: string;
  remote_addr: string;
  state: string;
  created_by: string;
  created_at: string;
  closed_at: string | null;
  bytes_sent: number;
  bytes_recv: number;
  socks5_user?: string;
  socks5_auth?: boolean;
  allowed_ips?: string;
  expires_at: string | null;
  tunnel_type: 'port_forward' | 'socks5';
}

export interface AuditEntry {
  id: number;
  user_id: string;
  device_id: string;
  action: string;
  resource: string;
  detail: string;
  source_ip: string;
  created_at: string;
}

export interface LoginResponse {
  token: string;
  expires_at: string;
  user: User;
}

// WebSocket envelope
export interface Envelope {
  id: string;
  channel: string;
  type: 'request' | 'response' | 'event' | 'stream';
  action: string;
  payload: any;
  ts: number;
  error?: { code: number; message: string };
}
