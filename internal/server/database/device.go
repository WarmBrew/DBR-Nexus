package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// Device represents a registered device
type Device struct {
	ID           string   `json:"id"`
	Hostname     string   `json:"hostname"`
	OS           string   `json:"os"`
	Arch         string   `json:"arch"`
	Kernel       string   `json:"kernel"`
	IP           string   `json:"ip"`
	IPInternal   string   `json:"ip_internal,omitempty"`
	IPLocation   string   `json:"ip_location,omitempty"`
	AgentVersion string   `json:"agent_version"`
	Labels       []string `json:"labels"`
	Tags         []string `json:"tags"`
	Notes        string   `json:"notes"`
	Status       string   `json:"status"`
	LastSeenAt   *string  `json:"last_seen_at"`
	SystemInfo   string   `json:"system_info,omitempty"`
	RegisteredAt string   `json:"registered_at"`
	UpdatedAt    string   `json:"updated_at"`
}

// DeviceFilter is used for filtering device queries
type DeviceFilter struct {
	Status  string
	Keyword string
	Limit   int
	Offset  int
}

const deviceColumns = `id, hostname, os, arch, kernel, ip, ip_internal, ip_location, agent_version, labels, tags, notes, status, last_seen_at, system_info, registered_at, updated_at`

// UpsertDevice inserts or updates a device
func (d *DB) UpsertDevice(ctx context.Context, dev *Device) error {
	labels, _ := json.Marshal(dev.Labels)
	tags, _ := json.Marshal(dev.Tags)

	var lastSeenArg interface{}
	if dev.LastSeenAt != nil {
		lastSeenArg = *dev.LastSeenAt
	}

	query := `
		INSERT INTO devices (id, hostname, os, arch, kernel, ip, ip_internal, ip_location, agent_version, labels, tags, notes, status, last_seen_at, system_info, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))
		ON CONFLICT(id) DO UPDATE SET
			hostname = excluded.hostname,
			os = excluded.os,
			arch = excluded.arch,
			kernel = excluded.kernel,
			ip = excluded.ip,
			ip_internal = excluded.ip_internal,
			ip_location = excluded.ip_location,
			agent_version = excluded.agent_version,
			labels = excluded.labels,
			tags = excluded.tags,
			status = excluded.status,
			last_seen_at = excluded.last_seen_at,
			system_info = excluded.system_info,
			updated_at = datetime('now')
	`
	_, err := d.db.ExecContext(ctx, query,
		dev.ID, dev.Hostname, dev.OS, dev.Arch, dev.Kernel, dev.IP,
		dev.IPInternal, dev.IPLocation,
		dev.AgentVersion, string(labels), string(tags), dev.Notes, dev.Status,
		lastSeenArg, dev.SystemInfo,
	)
	return err
}

func scanDevice(row interface {
	Scan(dest ...interface{}) error
}) (*Device, error) {
	dev := &Device{}
	var labels, tags string
	var lastSeen sql.NullString
	var sysInfo sql.NullString
	var notes sql.NullString
	var ipInternal sql.NullString
	var ipLocation sql.NullString

	err := row.Scan(&dev.ID, &dev.Hostname, &dev.OS, &dev.Arch, &dev.Kernel, &dev.IP,
		&ipInternal, &ipLocation,
		&dev.AgentVersion, &labels, &tags, &notes, &dev.Status, &lastSeen, &sysInfo,
		&dev.RegisteredAt, &dev.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	json.Unmarshal([]byte(labels), &dev.Labels)
	json.Unmarshal([]byte(tags), &dev.Tags)
	if notes.Valid {
		dev.Notes = notes.String
	}
	if lastSeen.Valid {
		dev.LastSeenAt = &lastSeen.String
	}
	if sysInfo.Valid {
		dev.SystemInfo = sysInfo.String
	}
	if ipInternal.Valid {
		dev.IPInternal = ipInternal.String
	}
	if ipLocation.Valid {
		dev.IPLocation = ipLocation.String
	}

	return dev, nil
}

// GetDevice retrieves a device by ID
func (d *DB) GetDevice(ctx context.Context, id string) (*Device, error) {
	query := `SELECT ` + deviceColumns + ` FROM devices WHERE id = ?`
	row := d.db.QueryRowContext(ctx, query, id)

	dev, err := scanDevice(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return dev, nil
}

// ListDevices retrieves all devices with optional filter
func (d *DB) ListDevices(ctx context.Context, filter DeviceFilter) ([]*Device, error) {
	query := `SELECT ` + deviceColumns + ` FROM devices WHERE 1=1`
	args := []interface{}{}

	if filter.Status != "" {
		query += " AND status = ?"
		args = append(args, filter.Status)
	}
	if filter.Keyword != "" {
		query += " AND (hostname LIKE ? OR ip LIKE ? OR notes LIKE ?)"
		kw := "%" + filter.Keyword + "%"
		args = append(args, kw, kw, kw)
	}

	query += " ORDER BY updated_at DESC"

	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", filter.Limit)
	}
	if filter.Offset > 0 {
		query += fmt.Sprintf(" OFFSET %d", filter.Offset)
	}

	rows, err := d.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var devices []*Device
	for rows.Next() {
		dev, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		devices = append(devices, dev)
	}

	return devices, nil
}

// UpdateDeviceStatus updates a device's online status
func (d *DB) UpdateDeviceStatus(ctx context.Context, id, status string) error {
	query := `UPDATE devices SET status = ?, last_seen_at = datetime('now'), updated_at = datetime('now') WHERE id = ?`
	_, err := d.db.ExecContext(ctx, query, status, id)
	return err
}

// UpdateDeviceNotes updates the device's notes/remarks
func (d *DB) UpdateDeviceNotes(ctx context.Context, id, notes string) error {
	query := `UPDATE devices SET notes = ?, updated_at = datetime('now') WHERE id = ?`
	_, err := d.db.ExecContext(ctx, query, notes, id)
	return err
}

// UpdateDeviceSystemInfo updates the device's system info JSON
func (d *DB) UpdateDeviceSystemInfo(ctx context.Context, id string, systemInfo string) error {
	query := `UPDATE devices SET system_info = ?, updated_at = datetime('now') WHERE id = ?`
	_, err := d.db.ExecContext(ctx, query, systemInfo, id)
	return err
}

// UpdateDeviceIP updates the device's IP and location info
func (d *DB) UpdateDeviceIP(ctx context.Context, id, ip, ipInternal, ipLocation string) error {
	query := `UPDATE devices SET ip = ?, ip_internal = ?, ip_location = ?, updated_at = datetime('now') WHERE id = ?`
	_, err := d.db.ExecContext(ctx, query, ip, ipInternal, ipLocation, id)
	return err
}

// DeleteDevice removes a device record
func (d *DB) DeleteDevice(ctx context.Context, id string) error {
	query := `DELETE FROM devices WHERE id = ?`
	_, err := d.db.ExecContext(ctx, query, id)
	return err
}
