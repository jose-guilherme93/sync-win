CREATE TABLE IF NOT EXISTS notification_configs (
    owner_id    TEXT NOT NULL,
    provider    TEXT NOT NULL,
    enabled     INTEGER NOT NULL DEFAULT 0,
    config_enc  TEXT NOT NULL DEFAULT '',  -- AES-GCM encrypted JSON, base64
    events      TEXT NOT NULL DEFAULT '["device_offline","device_online","sync_error","device_enrolled"]',
    created_at  TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at  TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (owner_id, provider)
);

CREATE TABLE IF NOT EXISTS notification_events (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    owner_id    TEXT NOT NULL,
    type        TEXT NOT NULL,
    device_id   TEXT NOT NULL DEFAULT '',
    hostname    TEXT NOT NULL DEFAULT '',
    message     TEXT NOT NULL,
    read        INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_notification_events_owner ON notification_events(owner_id, id);
