package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// The logs table existed before the owner_id column was introduced. On such a
// database the CREATE TABLE IF NOT EXISTS in initSchema is a no-op, so any
// statement in that schema block that references owner_id fails and takes the
// whole server startup down with it. This reproduces an upgrade from a
// pre-owner_id database.
func TestUpgradeFromPreOwnerIDLogsTable(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "lem.db")

	// Create the database exactly as an older release would have: the logs
	// table without owner_id, plus a device that owns some of its rows.
	legacy, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	legacySchema := `
		CREATE TABLE devices (
			id TEXT PRIMARY KEY,
			owner_id TEXT NOT NULL DEFAULT '',
			hostname TEXT NOT NULL DEFAULT '',
			device_token TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE logs (
			id             INTEGER PRIMARY KEY AUTOINCREMENT,
			ts             TEXT NOT NULL,
			level          TEXT NOT NULL,
			category       TEXT NOT NULL,
			event          TEXT NOT NULL,
			message        TEXT NOT NULL DEFAULT '',
			device_id      TEXT NOT NULL DEFAULT '',
			user_id        TEXT NOT NULL DEFAULT '',
			request_id     TEXT NOT NULL DEFAULT '',
			correlation_id TEXT NOT NULL DEFAULT '',
			duration_ms    INTEGER NOT NULL DEFAULT 0,
			status         INTEGER NOT NULL DEFAULT 0,
			metadata       TEXT NOT NULL DEFAULT '{}',
			redacted       INTEGER NOT NULL DEFAULT 0
		);
		INSERT INTO devices (id, owner_id, hostname, device_token)
			VALUES ('dev-1', 'owner-1', 'legacy-host', 'tok');
		INSERT INTO logs (ts, level, category, event, device_id, user_id)
			VALUES ('2026-01-01T00:00:00Z', 'INFO', 'http', 'http_request', 'dev-1', 'usr-1');
		INSERT INTO logs (ts, level, category, event, device_id, user_id)
			VALUES ('2026-01-01T00:00:01Z', 'INFO', 'http', 'login', '', 'usr-2');
	`
	if _, err := legacy.Exec(legacySchema); err != nil {
		t.Fatalf("build legacy schema: %v", err)
	}
	if _, err := legacy.Exec("INSERT INTO logs (ts, level, category, event, device_id, user_id) VALUES ('2026-01-01T00:00:02Z','INFO','http','http_request','','')"); err != nil {
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	// Opening the store runs initSchema plus the migrations. It must succeed.
	t.Setenv("LEM_SECRET_KEY", "test-key-at-least-16-chars")
	st, err := NewStore(dir)
	if err != nil {
		t.Fatalf("upgrade must not fail: %v", err)
	}
	defer st.Close()

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// The column and its index must exist.
	var cols int
	if err := db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('logs') WHERE name='owner_id'").Scan(&cols); err != nil {
		t.Fatal(err)
	}
	if cols != 1 {
		t.Fatal("owner_id column missing after upgrade")
	}
	var idx int
	if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_logs_owner_ts'").Scan(&idx); err != nil {
		t.Fatal(err)
	}
	if idx != 1 {
		t.Error("idx_logs_owner_ts missing after upgrade")
	}

	// Existing rows must be backfilled so the owner's log viewer keeps showing
	// history. The device row resolves to owner-1, the user-only row to usr-2,
	// and a row with neither stays empty rather than being invented.
	rows, err := db.Query("SELECT device_id, user_id, owner_id FROM logs ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	got := map[string]string{}
	for rows.Next() {
		var device, user, owner string
		if err := rows.Scan(&device, &user, &owner); err != nil {
			t.Fatal(err)
		}
		switch {
		case device == "dev-1":
			got["device"] = owner
		case user == "usr-2":
			got["user"] = owner
		default:
			got["orphan"] = owner
		}
	}
	if got["device"] != "owner-1" {
		t.Errorf("device row owner_id = %q, want owner-1", got["device"])
	}
	if got["user"] != "usr-2" {
		t.Errorf("user row owner_id = %q, want usr-2", got["user"])
	}
	if got["orphan"] != "" {
		t.Errorf("unscoped row owner_id = %q, want empty", got["orphan"])
	}
}

// A database that already has the column must be left alone rather than
// re-altered, and the migration must stay idempotent.
func TestLogsOwnerMigrationIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LEM_SECRET_KEY", "test-key-at-least-16-chars")

	first, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	first.migrateAddLogsOwnerColumn()
	first.Close()

	second, err := NewStore(dir)
	if err != nil {
		t.Fatalf("second open must succeed: %v", err)
	}
	second.migrateAddLogsOwnerColumn()
	second.Close()

	if _, err := os.Stat(filepath.Join(dir, "lem.db")); err != nil {
		t.Fatal(err)
	}
}
