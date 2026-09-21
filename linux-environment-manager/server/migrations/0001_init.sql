-- LEM initial schema. Embedded into the server binary via go:embed;
-- this file is the single source of truth for migrations.
CREATE TABLE IF NOT EXISTS users (
    id            TEXT PRIMARY KEY,
    email         TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at    TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS sessions (
    token      TEXT PRIMARY KEY,
    owner_id   TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_sessions_created_at ON sessions(created_at);

CREATE TABLE IF NOT EXISTS devices (
    id             TEXT PRIMARY KEY,
    user_id        TEXT NOT NULL DEFAULT '',
    owner_id       TEXT NOT NULL DEFAULT '',
    hostname       TEXT NOT NULL,
    device_token   TEXT NOT NULL,
    last_seen_at   TEXT NOT NULL,
    last_sync_at   TEXT NOT NULL DEFAULT '',
    sync_failures  INTEGER NOT NULL DEFAULT 0,
    last_error     TEXT NOT NULL DEFAULT '',
    last_error_at  TEXT NOT NULL DEFAULT '',
    hardware_json  TEXT NOT NULL DEFAULT '{}',
    apps_json      TEXT NOT NULL DEFAULT '[]',
    created_at     TEXT NOT NULL,
    updated_at     TEXT NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_devices_owner_hostname ON devices(owner_id, hostname);

CREATE TABLE IF NOT EXISTS files (
    id            TEXT PRIMARY KEY,
    device_id     TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    user_id       TEXT NOT NULL DEFAULT '',
    category      TEXT NOT NULL,
    filename      TEXT NOT NULL,
    relative_path TEXT NOT NULL DEFAULT '',
    content       TEXT NOT NULL,
    content_hash  TEXT NOT NULL,
    size_bytes    INTEGER NOT NULL,
    synced_at     TEXT NOT NULL,
    status        TEXT NOT NULL DEFAULT 'synced'
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_files_path ON files(device_id, category, relative_path);
CREATE INDEX IF NOT EXISTS idx_files_device ON files(device_id);

CREATE TABLE IF NOT EXISTS commands (
    id           TEXT PRIMARY KEY,
    device_id    TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    type         TEXT NOT NULL,
    path         TEXT NOT NULL DEFAULT '',
    name         TEXT NOT NULL DEFAULT '',
    source       TEXT NOT NULL DEFAULT '',
    status       TEXT NOT NULL DEFAULT 'queued',
    message      TEXT NOT NULL DEFAULT '',
    created_at   TEXT NOT NULL,
    completed_at TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_commands_pending ON commands(device_id, status, created_at);
