-- Add allowed_ips column for SOCKS5 source IP filtering
-- Comma-separated list of allowed client IPs; empty = allow all
ALTER TABLE tunnels ADD COLUMN allowed_ips TEXT DEFAULT '';
