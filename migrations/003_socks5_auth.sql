-- Add SOCKS5 authentication fields to tunnels table

ALTER TABLE tunnels ADD COLUMN socks5_user TEXT NOT NULL DEFAULT '';
ALTER TABLE tunnels ADD COLUMN socks5_pass TEXT NOT NULL DEFAULT '';
