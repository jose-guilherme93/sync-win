package logging

import (
	"database/sql"
	"testing"
	"time"
)

// The logging package owns the settings row, but the schema lives in
// store.initSchema. This DDL mirrors it so the package can be tested in
// isolation, the same way logsTableDDL does for the logs table.
const logSettingsDDL = `
CREATE TABLE IF NOT EXISTS log_settings (
    id             INTEGER PRIMARY KEY CHECK (id = 1),
    level          TEXT NOT NULL DEFAULT 'INFO',
    console_level  TEXT NOT NULL DEFAULT 'WARN',
    retention_days INTEGER NOT NULL DEFAULT 14,
    updated_at     TEXT NOT NULL
);
`

func TestSettingsRoundTrip(t *testing.T) {
	db := setupTestDB(t)
	if _, err := db.Exec(logSettingsDDL); err != nil {
		t.Fatal("create log_settings table:", err)
	}
	store := NewStore(db)

	// Before the first write there is no row, and the caller must be able to
	// tell that apart from a deliberate value.
	if _, found, err := store.GetSettings(); err != nil || found {
		t.Fatalf("empty store: found=%v err=%v", found, err)
	}

	want := Settings{Level: "DEBUG", ConsoleLevel: "NOTICE", RetentionDays: 30}
	if err := store.SaveSettings(want); err != nil {
		t.Fatal(err)
	}
	got, found, err := store.GetSettings()
	if err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}

	// A second save must update the single row rather than fail on the primary
	// key or insert a second one.
	want.RetentionDays = 7
	if saveErr := store.SaveSettings(want); saveErr != nil {
		t.Fatal(saveErr)
	}
	got, _, err = store.GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	if got.RetentionDays != 7 {
		t.Fatalf("retention = %d, want 7", got.RetentionDays)
	}
}

func TestSettingsValidate(t *testing.T) {
	valid := Settings{Level: "INFO", ConsoleLevel: "WARN", RetentionDays: 14}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid settings rejected: %v", err)
	}

	for _, bad := range []Settings{
		{Level: "AUDIT", ConsoleLevel: "WARN", RetentionDays: 14}, // not a threshold
		{Level: "LOUD", ConsoleLevel: "WARN", RetentionDays: 14},
		{Level: "INFO", ConsoleLevel: "FATAL", RetentionDays: 14}, // not a threshold
		{Level: "INFO", ConsoleLevel: "WARN", RetentionDays: 0},
		{Level: "INFO", ConsoleLevel: "WARN", RetentionDays: 4000},
	} {
		if err := bad.Validate(); err == nil {
			t.Fatalf("accepted invalid settings %+v", bad)
		}
	}
}

func TestParseLevelStrictNeverFallsBack(t *testing.T) {
	if lvl, ok := ParseLevelStrict("debug"); !ok || lvl != LevelDebug {
		t.Fatalf("ParseLevelStrict(debug) = %v, %v", lvl, ok)
	}
	// The lenient parser folds anything unknown onto INFO; the strict one must
	// not, or a typo would silently reset the log level.
	if _, ok := ParseLevelStrict("nonsense"); ok {
		t.Fatal("ParseLevelStrict accepted an unknown level")
	}
}

func TestLoggerLevelIsMutableAtRuntime(t *testing.T) {
	db := setupTestDB(t)
	store := NewStore(db)

	cfg := DefaultConfig()
	cfg.Level = LevelInfo
	cfg.ConsoleLevel = LevelError
	cfg.FlushInterval = time.Hour // flush explicitly, never on a timer
	logger := New(cfg, store)
	defer logger.Stop()

	// A DEBUG entry below the INFO threshold is dropped before it is queued.
	logger.Debug(CatSystem, Event("probe"), "below-threshold")
	logger.flushQueue()
	if n := countMessages(t, db, "below-threshold"); n != 0 {
		t.Fatalf("dropped entry was persisted (%d rows)", n)
	}

	// Lowering the threshold to DEBUG at runtime must take effect immediately,
	// without a restart.
	logger.SetLevel(LevelDebug)
	if logger.Level() != LevelDebug {
		t.Fatalf("Level() = %s after SetLevel", logger.Level())
	}
	logger.Debug(CatSystem, Event("probe"), "after-lowering")
	logger.flushQueue()
	if n := countMessages(t, db, "after-lowering"); n != 1 {
		t.Fatalf("entry after lowering persisted %d rows, want 1", n)
	}
}

func countMessages(t *testing.T, db *sql.DB, msg string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM logs WHERE message = ?`, msg).Scan(&n); err != nil {
		t.Fatalf("count %q: %v", msg, err)
	}
	return n
}
