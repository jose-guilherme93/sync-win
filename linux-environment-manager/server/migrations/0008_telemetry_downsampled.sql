-- Downsampled telemetry for long-term history.
-- Data is automatically aggregated from raw telemetry at 1m/5m/1h intervals.
CREATE TABLE IF NOT EXISTS telemetry_downsampled (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    device_id    TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    timestamp    TEXT NOT NULL,
    resolution   TEXT NOT NULL CHECK(resolution IN ('1m', '5m', '1h')),
    cpu_avg      REAL,
    cpu_min      REAL,
    cpu_max      REAL,
    mem_avg      REAL,
    mem_min      REAL,
    mem_max      REAL,
    net_rx_avg   REAL,
    net_tx_avg   REAL,
    temp_avg     REAL,
    temp_max     REAL,
    power_avg    REAL,
    sample_count INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_telemetry_ds_lookup
    ON telemetry_downsampled(device_id, resolution, timestamp);
CREATE INDEX IF NOT EXISTS idx_telemetry_ds_cleanup
    ON telemetry_downsampled(device_id, timestamp);

-- Per-owner retention configuration.
CREATE TABLE IF NOT EXISTS retention_settings (
    owner_id            TEXT PRIMARY KEY,
    raw_hours           INTEGER NOT NULL DEFAULT 2,
    resolution_1m_days  INTEGER NOT NULL DEFAULT 7,
    resolution_5m_days  INTEGER NOT NULL DEFAULT 30,
    resolution_1h_days  INTEGER NOT NULL DEFAULT 365,
    updated_at          TEXT NOT NULL
);
