-- Telemetry history for real-time charts.
-- Stores time-series snapshots so the dashboard can display historical graphs.
CREATE TABLE IF NOT EXISTS telemetry_history (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    device_id   TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    timestamp   TEXT NOT NULL,
    payload     TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_telemetry_device_time
    ON telemetry_history(device_id, timestamp);
