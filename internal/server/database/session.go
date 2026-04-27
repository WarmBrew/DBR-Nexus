package database

import (
	"context"
	"database/sql"
)

// tunnelColumns is the common column list for SELECT queries on the tunnels table
const tunnelColumns = `id, device_id, local_addr, remote_addr, state, created_by, created_at, closed_at, bytes_sent, bytes_recv, socks5_user, socks5_pass, allowed_ips, expires_at, tunnel_type`

// scanTunnel scans a row into a Tunnel struct
func scanTunnel(scanner interface{ Scan(...interface{}) error }) (*Tunnel, error) {
	t := &Tunnel{}
	var closedAt sql.NullString
	var expiresAt sql.NullString
	err := scanner.Scan(&t.ID, &t.DeviceID, &t.LocalAddr, &t.RemoteAddr, &t.State,
		&t.CreatedBy, &t.CreatedAt, &closedAt, &t.BytesSent, &t.BytesRecv, &t.Socks5User, &t.Socks5Pass, &t.AllowedIPs, &expiresAt, &t.TunnelType)
	if err != nil {
		return nil, err
	}
	if closedAt.Valid {
		t.ClosedAt = &closedAt.String
	}
	if expiresAt.Valid {
		t.ExpiresAt = &expiresAt.String
	}
	return t, nil
}

// Session represents an agent connection session
type Session struct {
	ID             string  `json:"id"`
	DeviceID       string  `json:"device_id"`
	RemoteAddr     string  `json:"remote_addr"`
	ConnectedAt    string  `json:"connected_at"`
	DisconnectedAt *string `json:"disconnected_at,omitempty"`
	DurationSecs   int     `json:"duration_secs,omitempty"`
}

// CreateSession inserts a new session
func (d *DB) CreateSession(ctx context.Context, s *Session) error {
	query := `INSERT INTO sessions (id, device_id, remote_addr) VALUES (?, ?, ?)`
	_, err := d.db.ExecContext(ctx, query, s.ID, s.DeviceID, s.RemoteAddr)
	return err
}

// CloseSession marks a session as disconnected
func (d *DB) CloseSession(ctx context.Context, id string) error {
	query := `UPDATE sessions SET disconnected_at = datetime('now'), duration_secs = CAST((julianday('now') - julianday(connected_at)) * 86400 AS INTEGER) WHERE id = ?`
	_, err := d.db.ExecContext(ctx, query, id)
	return err
}

// Tunnel represents an active tunnel
type Tunnel struct {
	ID         string  `json:"id"`
	DeviceID   string  `json:"device_id"`
	LocalAddr  string  `json:"local_addr"`
	RemoteAddr string  `json:"remote_addr"`
	State      string  `json:"state"`
	CreatedBy  string  `json:"created_by"`
	CreatedAt  string  `json:"created_at"`
	ClosedAt   *string `json:"closed_at,omitempty"`
	BytesSent  int64   `json:"bytes_sent"`
	BytesRecv  int64   `json:"bytes_recv"`
	Socks5User string  `json:"socks5_user,omitempty"`
	Socks5Pass string  `json:"-"` // never serialized to JSON, only used internally
	AllowedIPs string  `json:"allowed_ips,omitempty"`
	ExpiresAt  *string `json:"expires_at,omitempty"`
	TunnelType string  `json:"tunnel_type"`
}

// HasSocks5Auth returns whether this tunnel has SOCKS5 authentication configured
func (t *Tunnel) HasSocks5Auth() bool {
	return t.Socks5User != ""
}

// IsSocks5 returns whether this tunnel is a SOCKS5 proxy
func (t *Tunnel) IsSocks5() bool {
	return t.TunnelType == "socks5"
}

// CreateTunnel inserts a new tunnel
func (d *DB) CreateTunnel(ctx context.Context, t *Tunnel) error {
	query := `INSERT INTO tunnels (id, device_id, local_addr, remote_addr, state, created_by, socks5_user, socks5_pass, allowed_ips, expires_at, tunnel_type) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := d.db.ExecContext(ctx, query, t.ID, t.DeviceID, t.LocalAddr, t.RemoteAddr, t.State, t.CreatedBy, t.Socks5User, t.Socks5Pass, t.AllowedIPs, t.ExpiresAt, t.TunnelType)
	return err
}

// GetTunnel retrieves a tunnel by ID
func (d *DB) GetTunnel(ctx context.Context, id string) (*Tunnel, error) {
	query := `SELECT ` + tunnelColumns + ` FROM tunnels WHERE id = ?`
	row := d.db.QueryRowContext(ctx, query, id)

	t, err := scanTunnel(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return t, err
}

// ListTunnels retrieves all tunnels
func (d *DB) ListTunnels(ctx context.Context) ([]*Tunnel, error) {
	query := `SELECT ` + tunnelColumns + ` FROM tunnels ORDER BY created_at DESC`
	rows, err := d.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tunnels []*Tunnel
	for rows.Next() {
		t, err := scanTunnel(rows)
		if err != nil {
			return nil, err
		}
		tunnels = append(tunnels, t)
	}
	return tunnels, nil
}

// CloseTunnel marks a tunnel as closed
func (d *DB) CloseTunnel(ctx context.Context, id string) error {
	query := `UPDATE tunnels SET state = 'closed', closed_at = datetime('now') WHERE id = ?`
	_, err := d.db.ExecContext(ctx, query, id)
	return err
}

// UpdateTunnelStats updates tunnel byte counters
func (d *DB) UpdateTunnelStats(ctx context.Context, id string, bytesSent, bytesRecv int64) error {
	query := `UPDATE tunnels SET bytes_sent = bytes_sent + ?, bytes_recv = bytes_recv + ? WHERE id = ?`
	_, err := d.db.ExecContext(ctx, query, bytesSent, bytesRecv, id)
	return err
}

// UpdateTunnelExpiresAt updates the tunnel expiration time
func (d *DB) UpdateTunnelExpiresAt(ctx context.Context, id string, expiresAt string) error {
	query := `UPDATE tunnels SET expires_at = ? WHERE id = ?`
	_, err := d.db.ExecContext(ctx, query, expiresAt, id)
	return err
}

// DeleteTunnel permanently deletes a tunnel record from the database
func (d *DB) DeleteTunnel(ctx context.Context, id string) error {
	query := `DELETE FROM tunnels WHERE id = ?`
	_, err := d.db.ExecContext(ctx, query, id)
	return err
}

// ListActiveTunnelsWithExpiry retrieves all active tunnels that have an expiration time
func (d *DB) ListActiveTunnelsWithExpiry(ctx context.Context) ([]*Tunnel, error) {
	query := `SELECT ` + tunnelColumns + ` FROM tunnels WHERE state = 'active' AND expires_at IS NOT NULL ORDER BY created_at DESC`
	rows, err := d.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tunnels []*Tunnel
	for rows.Next() {
		t, err := scanTunnel(rows)
		if err != nil {
			return nil, err
		}
		tunnels = append(tunnels, t)
	}
	return tunnels, nil
}
