-- Per-owner sync configuration: extra save-game folders edited from the web
-- dashboard and consumed by agents during their preference sync cycle.
CREATE TABLE IF NOT EXISTS sync_configs (
  owner_id        TEXT PRIMARY KEY,
  extra_dirs_json TEXT NOT NULL DEFAULT '[]',
  updated_at      TEXT NOT NULL
);
