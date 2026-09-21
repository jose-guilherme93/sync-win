-- Structured logging, audit, and observability schema.
-- Append-only event store with smart indexing.

CREATE TABLE IF NOT EXISTS logs (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    ts            TEXT NOT NULL,
    level         TEXT NOT NULL,
    category      TEXT NOT NULL,
    event         TEXT NOT NULL,
    message       TEXT NOT NULL DEFAULT '',
    device_id     TEXT NOT NULL DEFAULT '',
    user_id       TEXT NOT NULL DEFAULT '',
    request_id    TEXT NOT NULL DEFAULT '',
    correlation_id TEXT NOT NULL DEFAULT '',
    duration_ms   INTEGER NOT NULL DEFAULT 0,
    status        INTEGER NOT NULL DEFAULT 0,
    metadata      TEXT NOT NULL DEFAULT '{}',
    redacted      INTEGER NOT NULL DEFAULT 0
);

-- Primary query index: time-based lookups (most common)
CREATE INDEX IF NOT EXISTS idx_logs_ts ON logs(ts);

-- Filter by level (dashboard, alerting)
CREATE INDEX IF NOT EXISTS idx_logs_level ON logs(level);

-- Device-specific queries (investigation, dashboard)
CREATE INDEX IF NOT EXISTS idx_logs_device ON logs(device_id, ts);

-- Request investigation (trace a single request)
CREATE INDEX IF NOT EXISTS idx_logs_request ON logs(request_id);

-- Correlation timeline (trace an operation across services)
CREATE INDEX IF NOT EXISTS idx_logs_correlation ON logs(correlation_id);

-- Event type filtering
CREATE INDEX IF NOT EXISTS idx_logs_event ON logs(event);

-- Composite index for common dashboard query: level + time range
CREATE INDEX IF NOT EXISTS idx_logs_level_ts ON logs(level, ts);

-- Composite index for device + level queries
CREATE INDEX IF NOT EXISTS idx_logs_device_level ON logs(device_id, level, ts);

-- HTTP access aggregation table (periodic flush from memory)
CREATE TABLE IF NOT EXISTS http_access (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    method          TEXT NOT NULL,
    path            TEXT NOT NULL,
    status_class    TEXT NOT NULL,
    count           INTEGER NOT NULL DEFAULT 1,
    avg_duration_ms REAL NOT NULL DEFAULT 0,
    max_duration_ms INTEGER NOT NULL DEFAULT 0,
    window_start    TEXT NOT NULL,
    window_end      TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_http_access_window ON http_access(window_start, window_end);
CREATE INDEX IF NOT EXISTS idx_http_access_path ON http_access(path, window_start);

-- Application metrics (numeric time-series, separate from logs)
CREATE TABLE IF NOT EXISTS metrics (
    id    INTEGER PRIMARY KEY AUTOINCREMENT,
    name  TEXT NOT NULL,
    value REAL NOT NULL,
    ts    TEXT NOT NULL,
    labels TEXT NOT NULL DEFAULT '{}'
);

CREATE INDEX IF NOT EXISTS idx_metrics_name_ts ON metrics(name, ts);
