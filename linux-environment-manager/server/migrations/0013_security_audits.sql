-- Security audit reports from Lynis.
-- Stores structured results from security scans run by agents.
CREATE TABLE IF NOT EXISTS security_audits (
    id                TEXT PRIMARY KEY,
    device_id         TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    owner_id          TEXT NOT NULL,
    hardening_index   INTEGER NOT NULL DEFAULT 0,
    total_warnings    INTEGER NOT NULL DEFAULT 0,
    total_suggestions INTEGER NOT NULL DEFAULT 0,
    total_tests       INTEGER NOT NULL DEFAULT 0,
    tests_passed      INTEGER NOT NULL DEFAULT 0,
    lynis_version     TEXT NOT NULL DEFAULT '',
    os_info           TEXT NOT NULL DEFAULT '',
    kernel_version    TEXT NOT NULL DEFAULT '',
    report_json       TEXT NOT NULL DEFAULT '{}',
    created_at        TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_security_audits_device ON security_audits(device_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_security_audits_owner ON security_audits(owner_id, created_at DESC);
