package store

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	t.Setenv("LEM_SECRET_KEY", "test-key-at-least-16-chars")
	st, err := NewStore(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestNotificationConfigRoundTrip(t *testing.T) {
	st := newTestStore(t)

	ownerID := "test-owner"
	// ListNotificationConfigs returns empty for unknown owner.
	configs, err := st.ListNotificationConfigs(ownerID)
	if err != nil {
		t.Fatal(err)
	}
	if len(configs) != 0 {
		t.Fatalf("expected 0 configs, got %d", len(configs))
	}

	// Save a config.
	cfg := json.RawMessage(`{"bot_token":"1234567890:ABCdefGHIjklMNOpqrSTUvwxYZ_1234567890","chat_id":"123"}`)
	events := []string{"device_offline", "sync_error"}
	if err := st.SaveNotificationConfig(ownerID, "telegram", true, events, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	// Retrieve it.
	got, err := st.GetNotificationConfig(ownerID, "telegram")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected config, got nil")
	}
	if got.Provider != "telegram" {
		t.Errorf("provider = %q, want telegram", got.Provider)
	}
	if !got.Enabled {
		t.Error("expected enabled")
	}
	if len(got.Events) != 2 || got.Events[0] != "device_offline" {
		t.Errorf("events = %v, want [device_offline sync_error]", got.Events)
	}
	if len(got.Config) == 0 {
		t.Fatal("expected non-empty decrypted config")
	}
	// Verify the decrypted config matches the original.
	var plain map[string]string
	if err := json.Unmarshal(got.Config, &plain); err != nil {
		t.Fatalf("unmarshal decrypted config: %v", err)
	}
	if plain["chat_id"] != "123" {
		t.Errorf("chat_id = %q, want 123", plain["chat_id"])
	}
	if plain["bot_token"] != "1234567890:ABCdefGHIjklMNOpqrSTUvwxYZ_1234567890" {
		t.Errorf("bot_token mismatch after decrypt")
	}

	// Update.
	if err := st.SaveNotificationConfig(ownerID, "telegram", false, []string{"device_online"}, cfg); err != nil {
		t.Fatalf("update config: %v", err)
	}
	got2, err := st.GetNotificationConfig(ownerID, "telegram")
	if err != nil {
		t.Fatal(err)
	}
	if got2.Enabled {
		t.Error("expected disabled after update")
	}
	if len(got2.Events) != 1 || got2.Events[0] != "device_online" {
		t.Errorf("events = %v, want [device_online]", got2.Events)
	}
}

func TestNotificationEventCRUD(t *testing.T) {
	st := newTestStore(t)
	ownerID := "test-owner"

	// Insert events.
	id1, err := st.InsertNotificationEvent(ownerID, "device_offline", "dev-1", "laptop", "went offline")
	if err != nil {
		t.Fatal(err)
	}
	if id1 <= 0 {
		t.Fatalf("expected positive id, got %d", id1)
	}

	id2, err := st.InsertNotificationEvent(ownerID, "device_online", "dev-1", "laptop", "is back online")
	if err != nil {
		t.Fatal(err)
	}

	// List since 0 returns latest entries (oldest first).
	events, err := st.ListNotificationEvents(ownerID, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) < 2 {
		t.Fatalf("expected at least 2 events, got %d", len(events))
	}
	if events[0].ID != id1 {
		t.Errorf("first event id = %d, want %d", events[0].ID, id1)
	}

	// List since id1 returns only id2.
	events2, err := st.ListNotificationEvents(ownerID, id1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events2) != 1 || events2[0].ID != id2 {
		t.Errorf("since id1 expected [id2], got %v", events2)
	}

	// Mark as read.
	if err := st.MarkNotificationEventsRead(ownerID, []int64{id1}); err != nil {
		t.Fatal(err)
	}
	events3, _ := st.ListNotificationEvents(ownerID, 0, 10)
	for _, e := range events3 {
		if e.ID == id1 && !e.Read {
			t.Error("expected id1 to be marked as read")
		}
	}
}

func TestNotificationEventPrune(t *testing.T) {
	st := newTestStore(t)
	ownerID := "test-owner"

	// Insert more than maxNotificationEventsPerOwner.
	for i := 0; i < maxNotificationEventsPerOwner+10; i++ {
		if _, err := st.InsertNotificationEvent(ownerID, "sync_error", "dev-1", "pc", "err"); err != nil {
			t.Fatal(err)
		}
	}

	// Verify pruning.
	events, err := st.ListNotificationEvents(ownerID, 0, 500)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) > maxNotificationEventsPerOwner {
		t.Errorf("expected at most %d events, got %d", maxNotificationEventsPerOwner, len(events))
	}
}

func TestNotificationConfigEmptyOwnerRejected(t *testing.T) {
	st := newTestStore(t)
	err := st.SaveNotificationConfig("", "telegram", true, nil, json.RawMessage(`{}`))
	if err == nil {
		t.Error("expected error for empty owner_id")
	}
}

func TestBoolInt(t *testing.T) {
	if boolInt(true) != 1 {
		t.Error("expected 1 for true")
	}
	if boolInt(false) != 0 {
		t.Error("expected 0 for false")
	}
}

func TestNotificationEventConcurrent(t *testing.T) {
	st := newTestStore(t)
	ownerID := "test-owner"
	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func(i int) {
			defer func() { done <- struct{}{} }()
			for j := 0; j < 20; j++ {
				if _, err := st.InsertNotificationEvent(ownerID, "sync_error", "dev-1", "pc", "err"); err != nil {
					t.Errorf("insert %d/%d: %v", i, j, err)
				}
			}
		}(i)
	}
	for i := 0; i < 10; i++ {
		<-done
	}
	events, err := st.ListNotificationEvents(ownerID, 0, 500)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) == 0 {
		t.Error("expected at least one event")
	}
}

func TestMarkNotificationEventsReadIgnoresWrongOwner(t *testing.T) {
	st := newTestStore(t)
	id, _ := st.InsertNotificationEvent("owner-A", "sync_error", "dev-1", "pc", "err")
	// Try to mark with wrong owner.
	if err := st.MarkNotificationEventsRead("owner-B", []int64{id}); err != nil {
		t.Fatal(err)
	}
	// Should not be marked as read.
	events, _ := st.ListNotificationEvents("owner-A", 0, 10)
	for _, e := range events {
		if e.ID == id && e.Read {
			t.Error("event should not be marked read for wrong owner")
		}
	}
}
