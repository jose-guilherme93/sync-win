CREATE TABLE IF NOT EXISTS device_notes (
    id          TEXT PRIMARY KEY,
    device_id   TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    owner_id    TEXT NOT NULL,
    content     TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_device_notes_device ON device_notes(device_id);
CREATE INDEX IF NOT EXISTS idx_device_notes_owner ON device_notes(owner_id);

CREATE TABLE IF NOT EXISTS device_attachments (
    id          TEXT PRIMARY KEY,
    device_id   TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    owner_id    TEXT NOT NULL,
    filename    TEXT NOT NULL,
    mime_type   TEXT NOT NULL DEFAULT 'application/octet-stream',
    size_bytes  INTEGER NOT NULL DEFAULT 0,
    data        BLOB NOT NULL,
    created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_device_attachments_device ON device_attachments(device_id);
CREATE INDEX IF NOT EXISTS idx_device_attachments_owner ON device_attachments(owner_id);
