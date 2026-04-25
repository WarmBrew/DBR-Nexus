-- Initial schema for device management system

CREATE TABLE IF NOT EXISTS users (
    id          TEXT PRIMARY KEY,
    username    TEXT NOT NULL UNIQUE,
    password    TEXT NOT NULL,
    display_name TEXT NOT NULL DEFAULT '',
    role        TEXT NOT NULL DEFAULT 'operator',
    enabled     INTEGER NOT NULL DEFAULT 1,
    created_at  TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS devices (
    id              TEXT PRIMARY KEY,
    hostname        TEXT NOT NULL DEFAULT '',
    os              TEXT NOT NULL DEFAULT '',
    arch            TEXT NOT NULL DEFAULT '',
    kernel          TEXT NOT NULL DEFAULT '',
    ip              TEXT NOT NULL DEFAULT '',
    agent_version   TEXT NOT NULL DEFAULT '',
    labels          TEXT NOT NULL DEFAULT '[]',
    tags            TEXT NOT NULL DEFAULT '[]',
    notes           TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL DEFAULT 'offline',
    last_seen_at    TEXT,
    system_info     TEXT,
    registered_at   TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at      TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS sessions (
    id              TEXT PRIMARY KEY,
    device_id       TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    remote_addr     TEXT NOT NULL DEFAULT '',
    connected_at    TEXT NOT NULL DEFAULT (datetime('now')),
    disconnected_at TEXT,
    duration_secs   INTEGER
);

CREATE TABLE IF NOT EXISTS tunnels (
    id              TEXT PRIMARY KEY,
    device_id       TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    local_addr      TEXT NOT NULL,
    remote_addr     TEXT NOT NULL,
    state           TEXT NOT NULL DEFAULT 'active',
    created_by      TEXT NOT NULL REFERENCES users(id),
    created_at      TEXT NOT NULL DEFAULT (datetime('now')),
    closed_at       TEXT,
    bytes_sent      INTEGER NOT NULL DEFAULT 0,
    bytes_recv      INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS tasks (
    id              TEXT PRIMARY KEY,
    device_id       TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    user_id         TEXT NOT NULL REFERENCES users(id),
    channel         TEXT NOT NULL,
    action          TEXT NOT NULL,
    params          TEXT,
    status          TEXT NOT NULL DEFAULT 'pending',
    result          TEXT,
    created_at      TEXT NOT NULL DEFAULT (datetime('now')),
    completed_at    TEXT
);

CREATE TABLE IF NOT EXISTS audit_logs (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id         TEXT REFERENCES users(id),
    device_id       TEXT REFERENCES devices(id),
    action          TEXT NOT NULL,
    resource        TEXT,
    detail          TEXT,
    source_ip       TEXT NOT NULL DEFAULT '',
    created_at      TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Indexes
CREATE INDEX IF NOT EXISTS idx_devices_status ON devices(status);
CREATE INDEX IF NOT EXISTS idx_sessions_device ON sessions(device_id);
CREATE INDEX IF NOT EXISTS idx_tunnels_device ON tunnels(device_id);
CREATE INDEX IF NOT EXISTS idx_tasks_device ON tasks(device_id);
CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);
CREATE INDEX IF NOT EXISTS idx_audit_logs_user ON audit_logs(user_id);
CREATE INDEX IF NOT EXISTS idx_audit_logs_device ON audit_logs(device_id);
CREATE INDEX IF NOT EXISTS idx_audit_logs_created ON audit_logs(created_at);
CREATE INDEX IF NOT EXISTS idx_audit_logs_action ON audit_logs(action);
