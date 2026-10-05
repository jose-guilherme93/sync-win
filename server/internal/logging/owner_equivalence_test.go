package logging

import (
	"database/sql"
	"testing"
	"time"
)

// The owner filter moved from
//
//	(user_id = ? OR device_id IN (SELECT id FROM devices WHERE owner_id = ?))
//
// to the indexed `owner_id = ?`. owner_id is materialised on write as
// COALESCE((SELECT owner_id FROM devices WHERE id = device_id), user_id), so the
// two must select exactly the same rows. A behavioural change here would hide
// history from the log viewer, so the old predicate is asserted directly
// alongside the new one.
func TestOwnerFilterIsEquivalentToLegacyPredicate(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	store := NewStore(db)

	devices := map[string]string{"dev-1": "owner-1", "dev-2": "owner-2"}
	for id, owner := range devices {
		if _, err := db.Exec("INSERT INTO devices (id, owner_id) VALUES (?, ?)", id, owner); err != nil {
			t.Fatal(err)
		}
	}

	rows := []struct{ device, user, event string }{
		{"dev-1", "", "a"},        // device scoped
		{"dev-1", "someone", "b"}, // device scoped, foreign user
		{"dev-2", "", "c"},        // other owner
		{"", "owner-1", "d"},      // user scoped, matches owner-1
		{"", "usr-x", "e"},        // user scoped, no owner match
		{"", "", "f"},             // system log, belongs to nobody
	}
	for _, r := range rows {
		err := store.WriteSingle(&LogEntry{
			Timestamp: time.Now().UTC(),
			Level:     LevelInfo,
			Category:  CatHTTP,
			Event:     Event(r.event),
			DeviceID:  r.device,
			UserID:    r.user,
		})
		if err != nil {
			t.Fatalf("write %q: %v", r.event, err)
		}
	}

	for _, owner := range []string{"owner-1", "owner-2", "usr-x", "nobody"} {
		legacy := legacyOwnerEvents(t, db, owner)
		fresh, err := store.Query(QueryParams{OwnerID: owner, Limit: 100})
		if err != nil {
			t.Fatalf("query %s: %v", owner, err)
		}
		got := map[string]bool{}
		for _, e := range fresh.Entries {
			got[string(e.Event)] = true
		}
		if len(got) != len(legacy) {
			t.Errorf("owner %q: new filter returned %v, legacy predicate returns %v", owner, got, legacy)
			continue
		}
		for event := range legacy {
			if !got[event] {
				t.Errorf("owner %q: missing %q that the legacy predicate matched", owner, event)
			}
		}
	}
}

// legacyOwnerEvents runs the pre-migration predicate verbatim.
func legacyOwnerEvents(t *testing.T, db *sql.DB, owner string) map[string]bool {
	t.Helper()
	rows, err := db.Query(
		`SELECT event FROM logs WHERE (user_id = ? OR device_id IN (SELECT id FROM devices WHERE owner_id = ?))`,
		owner, owner)
	if err != nil {
		t.Fatalf("legacy query for %s: %v", owner, err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var event string
		if err := rows.Scan(&event); err != nil {
			t.Fatal(err)
		}
		out[event] = true
	}
	return out
}
