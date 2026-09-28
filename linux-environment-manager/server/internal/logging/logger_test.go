package logging

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}

	// Apply migration
	migration, _ := os.ReadFile("../../migrations/0006_logging.sql")
	if _, err := db.Exec(string(migration)); err != nil {
		t.Fatal("apply migration:", err)
	}

	// The log store resolves owner_id from the owning device on insert, so the
	// minimal devices table has to exist even in the isolated log test DB.
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS devices (
		id TEXT PRIMARY KEY,
		owner_id TEXT NOT NULL DEFAULT ''
	)`); err != nil {
		t.Fatal("create devices stub:", err)
	}

	return db
}

func TestIsStreamingPath(t *testing.T) {
	// The notification SSE stream stays open for as long as the client is
	// connected, so its duration must never be reported as a slow request.
	tests := []struct {
		path string
		want bool
	}{
		{"/api/notifications/stream", true},
		{"/api/notifications/stream?ticket=abc", true},
		{"/api/notifications/inbox", false},
		{"/api/devices/abc/telemetry", false},
		{"/api/logs", false},
	}
	for _, tt := range tests {
		if got := isStreamingPath(tt.path); got != tt.want {
			t.Errorf("isStreamingPath(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestLevelString(t *testing.T) {
	tests := []struct {
		level Level
		want  string
	}{
		{LevelTrace, "TRACE"},
		{LevelDebug, "DEBUG"},
		{LevelInfo, "INFO"},
		{LevelNotice, "NOTICE"},
		{LevelWarn, "WARN"},
		{LevelError, "ERROR"},
		{LevelFatal, "FATAL"},
		{LevelAudit, "AUDIT"},
	}
	for _, tt := range tests {
		if got := tt.level.String(); got != tt.want {
			t.Errorf("Level(%d).String() = %q, want %q", tt.level, got, tt.want)
		}
	}
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input string
		want  Level
	}{
		{"DEBUG", LevelDebug},
		{"INFO", LevelInfo},
		{"WARN", LevelWarn},
		{"ERROR", LevelError},
		{"AUDIT", LevelAudit},
		{"invalid", LevelInfo}, // default
	}
	for _, tt := range tests {
		if got := ParseLevel(tt.input); got != tt.want {
			t.Errorf("ParseLevel(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestRedactMetadata(t *testing.T) {
	input := map[string]any{
		"name":      "test",
		"password":  "secret123",
		"token":     "abc123",
		"api_key":   "xyz789",
		"safe_data": "visible",
	}

	result, changed := redactMetadata(input)

	if !changed {
		t.Error("expected redactMetadata to detect changes")
	}
	if result["name"] != "test" {
		t.Errorf("name should not be redacted, got %v", result["name"])
	}
	if result["password"] != "[REDACTED]" {
		t.Errorf("password should be redacted, got %v", result["password"])
	}
	if result["token"] != "[REDACTED]" {
		t.Errorf("token should be redacted, got %v", result["token"])
	}
	if result["api_key"] != "[REDACTED]" {
		t.Errorf("api_key should be redacted, got %v", result["api_key"])
	}
	if result["safe_data"] != "visible" {
		t.Errorf("safe_data should not be redacted, got %v", result["safe_data"])
	}
}

func TestRedactInstallPath(t *testing.T) {
	if got := redactPath("/install/secret-enrollment-token"); got != "/install/[REDACTED]" {
		t.Fatalf("redactPath returned %q", got)
	}
	if got := redactPath("/api/devices/dev-1"); got != "/api/devices/dev-1" {
		t.Fatalf("non-secret path changed to %q", got)
	}
}

func TestDeduplication(t *testing.T) {
	var flushed []*dedupEntry
	d := newDeduplicator(time.Minute, 100, func(entry *dedupEntry) {
		flushed = append(flushed, entry)
	})
	defer d.stop()

	// First occurrence should pass
	if !d.check(LevelInfo, CatSystem, EventAppStarted, "", "msg1") {
		t.Error("first occurrence should pass")
	}

	// Second occurrence should pass
	if !d.check(LevelInfo, CatSystem, EventAppStarted, "", "msg1") {
		t.Error("second occurrence should pass")
	}

	// Third occurrence should pass
	if !d.check(LevelInfo, CatSystem, EventAppStarted, "", "msg1") {
		t.Error("third occurrence should pass")
	}

	// Fourth occurrence should be deduplicated
	if d.check(LevelInfo, CatSystem, EventAppStarted, "", "msg1") {
		t.Error("fourth occurrence should be deduplicated")
	}

	// Different event should pass
	if !d.check(LevelInfo, CatSystem, Event("other_event"), "", "msg2") {
		t.Error("different event should pass")
	}

	// Error level should never be deduplicated
	for i := 0; i < 10; i++ {
		if !d.check(LevelError, CatSystem, EventAppError, "", "error msg") {
			t.Error("error level should never be deduplicated")
		}
	}
}

func TestShouldPersistHTTP(t *testing.T) {
	tests := []struct {
		method   string
		path     string
		status   int
		duration int64
		want     bool
	}{
		{"GET", "/health", 200, 0, false},
		{"GET", "/api/agent/version", 200, 0, false},
		{"GET", "/api/devices/dev-1/telemetry", 200, 0, false},
		{"OPTIONS", "/api/devices", 204, 0, false},
		{"POST", "/api/devices/dev-1/sync", 200, 0, true},
		{"GET", "/api/devices", 200, 2000, true},       // slow
		{"GET", "/api/devices", 500, 0, true},          // error
		{"GET", "/api/devices", 404, 0, false},         // 404 GET
		{"DELETE", "/api/devices/dev-1", 200, 0, true}, // state change
		{"GET", "/api/logs", 200, 0, false},            // normal GET
		{"POST", "/api/logs", 200, 0, true},            // state change
	}
	for _, tt := range tests {
		if got := shouldPersistHTTP(tt.method, tt.path, tt.status, tt.duration); got != tt.want {
			t.Errorf("shouldPersistHTTP(%q, %q, %d, %d) = %v, want %v",
				tt.method, tt.path, tt.status, tt.duration, got, tt.want)
		}
	}
}

func TestIsNoisePath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/health", true},
		{"/api/agent/version", true},
		{"/api/agent/checksums", true},
		{"/api/devices/dev-1/telemetry", true},
		{"/api/devices/dev-1/heartbeat", true},
		{"/api/devices/dev-1/commands", true},
		{"/api/devices", false},
		{"/api/auth/login", false},
		{"/api/logs", false},
	}
	for _, tt := range tests {
		if got := isNoisePath(tt.path); got != tt.want {
			t.Errorf("isNoisePath(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestStoreWriteAndQuery(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := NewStore(db)

	// Write entries
	entries := []*LogEntry{
		{
			Timestamp: time.Now().UTC(),
			Level:     LevelInfo,
			Category:  CatDevice,
			Event:     EventDeviceRegistered,
			Message:   "device registered",
			DeviceID:  "dev-1",
			Metadata:  map[string]any{"hostname": "test-pc"},
		},
		{
			Timestamp: time.Now().UTC(),
			Level:     LevelError,
			Category:  CatDatabase,
			Event:     EventDBError,
			Message:   "connection failed",
		},
	}

	if err := store.WriteBatch(entries); err != nil {
		t.Fatal("WriteBatch:", err)
	}

	// Query all
	result, err := store.Query(QueryParams{Limit: 10})
	if err != nil {
		t.Fatal("Query:", err)
	}
	if result.Total != 2 {
		t.Errorf("expected 2 entries, got %d", result.Total)
	}

	// Query by level
	result, err = store.Query(QueryParams{Level: "ERROR"})
	if err != nil {
		t.Fatal("Query:", err)
	}
	if result.Total != 1 {
		t.Errorf("expected 1 error entry, got %d", result.Total)
	}

	// Query by device
	result, err = store.Query(QueryParams{DeviceID: "dev-1"})
	if err != nil {
		t.Fatal("Query:", err)
	}
	if result.Total != 1 {
		t.Errorf("expected 1 device entry, got %d", result.Total)
	}

	// Get by ID
	entry, err := store.GetByID(1)
	if err != nil {
		t.Fatal("GetByID:", err)
	}
	if entry.DeviceID != "dev-1" {
		t.Errorf("expected device_id dev-1, got %s", entry.DeviceID)
	}
}

func TestStoreRetention(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := NewStore(db)

	// Write old entry
	oldEntry := &LogEntry{
		Timestamp: time.Now().UTC().AddDate(0, 0, -30),
		Level:     LevelInfo,
		Category:  CatSystem,
		Event:     EventAppStarted,
	}
	store.WriteSingle(oldEntry)

	// Write recent entry
	recentEntry := &LogEntry{
		Timestamp: time.Now().UTC(),
		Level:     LevelInfo,
		Category:  CatSystem,
		Event:     EventAppStarted,
	}
	store.WriteSingle(recentEntry)

	// Cleanup 14 days
	affected, err := store.Cleanup(14)
	if err != nil {
		t.Fatal("Cleanup:", err)
	}
	if affected != 1 {
		t.Errorf("expected 1 deleted, got %d", affected)
	}

	// Check remaining
	result, _ := store.Query(QueryParams{Limit: 10})
	if result.Total != 1 {
		t.Errorf("expected 1 remaining, got %d", result.Total)
	}
}

func TestStoreAuditTrail(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := NewStore(db)

	// Write audit entry
	entry := &LogEntry{
		Timestamp: time.Now().UTC(),
		Level:     LevelAudit,
		Category:  CatAudit,
		Event:     EventDeviceRegistered,
		Message:   "device registered via enrollment",
		DeviceID:  "dev-1",
		UserID:    "usr-1",
	}
	store.WriteSingle(entry)

	// Query audit trail
	result, err := store.GetAuditTrail(10, 0)
	if err != nil {
		t.Fatal("GetAuditTrail:", err)
	}
	if result.Total != 1 {
		t.Errorf("expected 1 audit entry, got %d", result.Total)
	}
	if result.Entries[0].Level != LevelAudit {
		t.Errorf("expected audit level, got %v", result.Entries[0].Level)
	}
}

func TestLoggerConcurrency(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := NewStore(db)
	cfg := DefaultConfig()
	cfg.BatchSize = 50
	cfg.FlushInterval = 5 * time.Millisecond
	cfg.QueueSize = 50000
	logger := New(cfg, store)
	defer logger.Stop()

	// Launch many goroutines
	done := make(chan bool)
	for i := 0; i < 50; i++ {
		go func() {
			for j := 0; j < 20; j++ {
				logger.Info(CatSystem, EventAppStarted, "test message")
			}
			done <- true
		}()
	}

	// Wait for all
	for i := 0; i < 50; i++ {
		<-done
	}

	// Give time for flush
	time.Sleep(200 * time.Millisecond)

	// Check entries exist (at least some)
	result, _ := store.Query(QueryParams{Limit: 2000})
	if result.Total < 10 {
		t.Errorf("expected at least 10 entries, got %d", result.Total)
	}
}

func TestStatusClass(t *testing.T) {
	tests := []struct {
		status int
		want   string
	}{
		{200, "2xx"},
		{301, "3xx"},
		{404, "4xx"},
		{500, "5xx"},
		{100, "other"},
	}
	for _, tt := range tests {
		if got := statusClass(tt.status); got != tt.want {
			t.Errorf("statusClass(%d) = %q, want %q", tt.status, got, tt.want)
		}
	}
}

func TestFormatConsole(t *testing.T) {
	entry := &LogEntry{
		Timestamp: time.Date(2026, 8, 25, 12, 30, 45, 0, time.UTC),
		Level:     LevelWarn,
		Category:  CatDevice,
		Event:     EventDeviceOffline,
		DeviceID:  "dev-9",
		Message:   "heartbeat timeout",
	}

	msg := formatConsole(entry)

	if !contains(msg, "12:30:45") {
		t.Errorf("expected timestamp in output, got %s", msg)
	}
	if !contains(msg, "WARN") {
		t.Errorf("expected WARN level in output, got %s", msg)
	}
	if !contains(msg, "device_offline") {
		t.Errorf("expected event name in output, got %s", msg)
	}
	if !contains(msg, "device=dev-9") {
		t.Errorf("expected device_id in output, got %s", msg)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
