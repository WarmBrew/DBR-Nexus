package database

import (
	"context"
	"fmt"
)

// AuditEntry represents an audit log entry
type AuditEntry struct {
	ID        int64  `json:"id"`
	UserID    string `json:"user_id,omitempty"`
	DeviceID  string `json:"device_id,omitempty"`
	Action    string `json:"action"`
	Resource  string `json:"resource,omitempty"`
	Detail    string `json:"detail,omitempty"`
	SourceIP  string `json:"source_ip"`
	CreatedAt string `json:"created_at"`
}

// AuditFilter is used for filtering audit log queries
type AuditFilter struct {
	UserID   string
	DeviceID string
	Action   string
	From     string
	To       string
	Limit    int
	Offset   int
}

// CreateAuditLog inserts an audit log entry
func (d *DB) CreateAuditLog(ctx context.Context, entry *AuditEntry) error {
	query := `INSERT INTO audit_logs (user_id, device_id, action, resource, detail, source_ip) VALUES (?, ?, ?, ?, ?, ?)`
	_, err := d.db.ExecContext(ctx, query, entry.UserID, entry.DeviceID, entry.Action, entry.Resource, entry.Detail, entry.SourceIP)
	return err
}

// ListAuditLogs retrieves audit logs with filter
func (d *DB) ListAuditLogs(ctx context.Context, filter AuditFilter) ([]*AuditEntry, int, error) {
	countQuery := `SELECT COUNT(*) FROM audit_logs WHERE 1=1`
	dataQuery := `SELECT id, user_id, device_id, action, resource, detail, source_ip, created_at FROM audit_logs WHERE 1=1`
	args := []interface{}{}

	if filter.UserID != "" {
		clause := " AND user_id = ?"
		countQuery += clause
		dataQuery += clause
		args = append(args, filter.UserID)
	}
	if filter.DeviceID != "" {
		clause := " AND device_id = ?"
		countQuery += clause
		dataQuery += clause
		args = append(args, filter.DeviceID)
	}
	if filter.Action != "" {
		clause := " AND action = ?"
		countQuery += clause
		dataQuery += clause
		args = append(args, filter.Action)
	}
	if filter.From != "" {
		clause := " AND created_at >= ?"
		countQuery += clause
		dataQuery += clause
		args = append(args, filter.From)
	}
	if filter.To != "" {
		clause := " AND created_at <= ?"
		countQuery += clause
		dataQuery += clause
		args = append(args, filter.To)
	}

	var total int
	if err := d.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	dataQuery += " ORDER BY created_at DESC"
	if filter.Limit > 0 {
		dataQuery += fmt.Sprintf(" LIMIT %d", filter.Limit)
		args = append(args, filter.Limit)
	}
	if filter.Offset > 0 {
		dataQuery += fmt.Sprintf(" OFFSET %d", filter.Offset)
		args = append(args, filter.Offset)
	}

	rows, err := d.db.QueryContext(ctx, dataQuery, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var entries []*AuditEntry
	for rows.Next() {
		entry := &AuditEntry{}
		err := rows.Scan(&entry.ID, &entry.UserID, &entry.DeviceID, &entry.Action,
			&entry.Resource, &entry.Detail, &entry.SourceIP, &entry.CreatedAt)
		if err != nil {
			return nil, 0, err
		}
		entries = append(entries, entry)
	}

	return entries, total, nil
}
