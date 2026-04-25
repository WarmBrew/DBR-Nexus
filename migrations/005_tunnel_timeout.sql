ALTER TABLE tunnels ADD COLUMN expires_at TEXT;
ALTER TABLE tunnels ADD COLUMN tunnel_type TEXT NOT NULL DEFAULT 'port_forward';
UPDATE tunnels SET tunnel_type = 'socks5' WHERE remote_addr = 'SOCKS5 代理';
