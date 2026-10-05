package logging

import (
	"testing"
	"time"
)

// Owner-scoped log queries used to be
// `(user_id = ? OR device_id IN (SELECT id FROM devices WHERE owner_id = ?))`.
// The OR against a subquery prevents SQLite from using any index, so every page
// of the log viewer scanned the whole table. These tests pin the indexed form.
func TestOwnerScopedQueriesUseIndexedOwnerColumn(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	store := NewStore(db)

	for _, id := range []string{"dev-a", "dev-b"} {
		if _, err := db.Exec("INSERT INTO devices (id, owner_id) VALUES (?, ?)", id, "owner-"+id); err != nil {
			t.Fatalf("seed device %s: %v", id, err)
		}
	}

	write := func(deviceID, userID, event string) {
		err := store.WriteSingle(&LogEntry{
			Timestamp: time.Now().UTC(),
			Level:     LevelInfo,
			Category:  CatHTTP,
			Event:     Event(event),
			Message:   "entry for " + event,
			DeviceID:  deviceID,
			UserID:    userID,
		})
		if err != nil {
			t.Fatalf("write %s: %v", event, err)
		}
	}
	write("dev-a", "usr-a", "http_request")
	write("dev-b", "usr-b", "http_request")
	write("", "usr-a", "login") // user-scoped, no device

	// The device owner is what scopes the entry, not the user who happened to
	// trigger it.
	result, err := store.Query(QueryParams{OwnerID: "owner-dev-a", Limit: 50})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if result.Total != 1 {
		t.Fatalf("owner-dev-a: expected 1 entry, got %d", result.Total)
	}
	if got := result.Entries[0].DeviceID; got != "dev-a" {
		t.Errorf("owner-dev-a: got device %q, want dev-a", got)
	}

	// A user-scoped entry is visible to the owner matching its user_id.
	result, err = store.Query(QueryParams{OwnerID: "usr-a", Limit: 50})
	if err != nil {
		t.Fatalf("query user scope: %v", err)
	}
	if result.Total != 1 {
		t.Fatalf("usr-a: expected 1 entry, got %d", result.Total)
	}
	if got := result.Entries[0].Event; string(got) != "login" {
		t.Errorf("usr-a: got event %q, want login", got)
	}
}

// The owner_id value must be persisted, not recomputed per query, otherwise the
// index cannot be used.
func TestWriteBatchPersistsOwnerID(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	store := NewStore(db)

	if _, err := db.Exec("INSERT INTO devices (id, owner_id) VALUES ('dev-1', 'owner-1')"); err != nil {
		t.Fatal(err)
	}
	if err := store.WriteBatch([]*LogEntry{
		{Timestamp: time.Now().UTC(), Level: LevelInfo, Category: CatHTTP, Event: "http_request", DeviceID: "dev-1"},
		{Timestamp: time.Now().UTC(), Level: LevelInfo, Category: CatHTTP, Event: "http_request", UserID: "usr-9"},
	}); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}

	var deviceOwner, userOwner string
	if err := db.QueryRow("SELECT owner_id FROM logs WHERE device_id = 'dev-1'").Scan(&deviceOwner); err != nil {
		t.Fatal(err)
	}
	if deviceOwner != "owner-1" {
		t.Errorf("device entry owner_id = %q, want owner-1", deviceOwner)
	}
	if err := db.QueryRow("SELECT owner_id FROM logs WHERE user_id = 'usr-9'").Scan(&userOwner); err != nil {
		t.Fatal(err)
	}
	if userOwner != "usr-9" {
		t.Errorf("user entry owner_id = %q, want usr-9", userOwner)
	}
}
