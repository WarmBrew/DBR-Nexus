package database

import (
	"context"
	"database/sql"
)

// User represents a system user
type User struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	Password    string `json:"-"` // Never expose password
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
	Enabled     bool   `json:"enabled"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// CreateUser creates a new user
func (d *DB) CreateUser(ctx context.Context, user *User) error {
	query := `INSERT INTO users (id, username, password, display_name, role, enabled) VALUES (?, ?, ?, ?, ?, ?)`
	enabled := 0
	if user.Enabled {
		enabled = 1
	}
	_, err := d.db.ExecContext(ctx, query, user.ID, user.Username, user.Password, user.DisplayName, user.Role, enabled)
	return err
}

// GetUserByUsername retrieves a user by username
func (d *DB) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	query := `SELECT id, username, password, display_name, role, enabled, created_at, updated_at FROM users WHERE username = ?`
	row := d.db.QueryRowContext(ctx, query, username)

	user := &User{}
	var enabled int
	err := row.Scan(&user.ID, &user.Username, &user.Password, &user.DisplayName, &user.Role, &enabled, &user.CreatedAt, &user.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	user.Enabled = enabled == 1
	return user, nil
}

// GetUserByID retrieves a user by ID
func (d *DB) GetUserByID(ctx context.Context, id string) (*User, error) {
	query := `SELECT id, username, password, display_name, role, enabled, created_at, updated_at FROM users WHERE id = ?`
	row := d.db.QueryRowContext(ctx, query, id)

	user := &User{}
	var enabled int
	err := row.Scan(&user.ID, &user.Username, &user.Password, &user.DisplayName, &user.Role, &enabled, &user.CreatedAt, &user.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	user.Enabled = enabled == 1
	return user, nil
}

// ListUsers retrieves all users
func (d *DB) ListUsers(ctx context.Context) ([]*User, error) {
	query := `SELECT id, username, display_name, role, enabled, created_at, updated_at FROM users ORDER BY created_at`
	rows, err := d.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*User
	for rows.Next() {
		user := &User{}
		var enabled int
		err := rows.Scan(&user.ID, &user.Username, &user.DisplayName, &user.Role, &enabled, &user.CreatedAt, &user.UpdatedAt)
		if err != nil {
			return nil, err
		}
		user.Enabled = enabled == 1
		users = append(users, user)
	}
	return users, nil
}

// UpdateUser updates a user
func (d *DB) UpdateUser(ctx context.Context, user *User) error {
	query := `UPDATE users SET display_name = ?, role = ?, enabled = ?, updated_at = datetime('now') WHERE id = ?`
	enabled := 0
	if user.Enabled {
		enabled = 1
	}
	_, err := d.db.ExecContext(ctx, query, user.DisplayName, user.Role, enabled, user.ID)
	return err
}

// UpdateUserPassword updates a user's password
func (d *DB) UpdateUserPassword(ctx context.Context, id, hashedPassword string) error {
	query := `UPDATE users SET password = ?, updated_at = datetime('now') WHERE id = ?`
	_, err := d.db.ExecContext(ctx, query, hashedPassword, id)
	return err
}

// DeleteUser removes a user
func (d *DB) DeleteUser(ctx context.Context, id string) error {
	query := `DELETE FROM users WHERE id = ?`
	_, err := d.db.ExecContext(ctx, query, id)
	return err
}
