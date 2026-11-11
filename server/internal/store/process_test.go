package store

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func newProcessStore(t *testing.T) *Store {
	t.Helper()
	s, err := NewStore(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestProcessTablesExist(t *testing.T) {
	s := newProcessStore(t)
	for _, table := range []string{"process_history", "process_events"} {
		var name string
		err := s.DB().QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name)
		if err != nil {
			t.Fatalf("table %q missing: %v", table, err)
		}
	}
	for _, index := range []string{"idx_process_history_range", "idx_process_events_device"} {
		var name string
		if err := s.DB().QueryRow("SELECT name FROM sqlite_master WHERE type='index' AND name=?", index).Scan(&name); err != nil {
			t.Fatalf("index %q missing: %v", index, err)
		}
	}
}

func TestProcessStateRoundTrips(t *testing.T) {
	s := newProcessStore(t)
	device, err := s.RegisterDevice("proc-pc", "user", "owner", "")
	if err != nil {
		t.Fatal(err)
	}

	snapshot := ProcessSnapshot{
		Processes:   []ProcessRow{{PID: 1000, PPID: 1, Name: "firefox", CPUPercent: 12.5, RSSBytes: 2048}},
		Stats:       &ProcessStats{Total: 2, Running: 1, Sleeping: 1, Load1: 0.5},
		CollectedAt: "2026-11-11T10:00:00Z",
		IOAvailable: true,
		Collected:   true,
	}
	if err := s.UpdateProcessState(device.ID, snapshot); err != nil {
		t.Fatalf("UpdateProcessState: %v", err)
	}
	got, err := s.GetProcessState(device.ID)
	if err != nil {
		t.Fatalf("GetProcessState: %v", err)
	}
	if len(got.Processes) != 1 || got.Processes[0].Name != "firefox" || got.Processes[0].CPUPercent != 12.5 {
		t.Fatalf("processes round-trip = %+v", got.Processes)
	}
	if got.Stats == nil || got.Stats.Total != 2 {
		t.Fatalf("stats round-trip = %+v", got.Stats)
	}
	if !got.IOAvailable || !got.Collected || got.CollectedAt != snapshot.CollectedAt {
		t.Fatalf("metadata round-trip = %+v", got)
	}
}

func TestProcessStateNeverCollected(t *testing.T) {
	s := newProcessStore(t)
	device, err := s.RegisterDevice("proc-new", "user", "owner", "")
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.GetProcessState(device.ID)
	if err != nil {
		t.Fatalf("GetProcessState on a fresh device: %v", err)
	}
	if got.Collected {
		t.Fatal("a device with no snapshot must report collected=false, not an empty list")
	}
	if got.Processes == nil {
		// nil is acceptable internally, but the API must serialise it as [].
		got.Processes = []ProcessRow{}
	}
	if len(got.Processes) != 0 {
		t.Fatalf("processes = %v", got.Processes)
	}
}

func TestQueueProcessCommandValidates(t *testing.T) {
	s := newProcessStore(t)
	device, err := s.RegisterDevice("proc-cmd", "user", "owner", "")
	if err != nil {
		t.Fatal(err)
	}

	valid := `{"pid":1000,"signal":"TERM","start_time_unix":1005}`
	command, err := s.QueueProcessCommand(device.ID, "process_signal", valid)
	if err != nil {
		t.Fatalf("valid process command rejected: %v", err)
	}
	if command.Type != "process_signal" || command.Payload != valid || command.Status != "queued" {
		t.Fatalf("queued command = %+v", command)
	}

	bad := []struct {
		name    string
		command string
		payload string
	}{
		{"unknown type", "process_torture", valid},
		{"oversized payload", "process_signal", `{"pid":1000,"signal":"TERM","start_time_unix":1005,"pad":"` + string(make([]byte, 5000)) + `"}`},
		{"not json", "process_signal", `{`},
		{"bad signal", "process_signal", `{"pid":1000,"signal":"HUP","start_time_unix":1005}`},
		{"bad pid", "process_signal", `{"pid":1,"signal":"TERM","start_time_unix":1005}`},
		{"unknown field", "process_signal", `{"pid":1000,"signal":"TERM","start_time_unix":1005,"x":1}`},
		{"nice out of range", "process_renice", `{"pid":1000,"nice":-100,"start_time_unix":1005}`},
		{"ionice class", "process_ionice", `{"pid":1000,"class":99,"level":0,"start_time_unix":1005}`},
		{"ionice level", "process_ionice", `{"pid":1000,"class":2,"level":8,"start_time_unix":1005}`},
	}
	for _, tc := range bad {
		if _, err := s.QueueProcessCommand(device.ID, tc.command, tc.payload); err == nil {
			t.Errorf("%s: QueueProcessCommand must reject %q", tc.name, tc.payload)
		}
	}
}

func TestProcessHistoryRetention(t *testing.T) {
	s := newProcessStore(t)
	device, err := s.RegisterDevice("proc-hist", "user", "owner", "")
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	history := ProcessHistory{
		IntervalSeconds: 60,
		Samples: []ProcessHistorySample{{
			T:    now.Unix(),
			Rows: []ProcessHistoryPoint{{PID: 1000, Name: "firefox", CPU: 12.5, RSS: 2048}},
		}},
		Events: []ProcessHistoryEvent{{T: now.Unix(), Type: "started", PID: 1001, Name: "vim"}},
	}
	if err := s.RecordProcessHistory(device.ID, history, now); err != nil {
		t.Fatalf("RecordProcessHistory: %v", err)
	}

	var rows int
	if err := s.DB().QueryRow("SELECT COUNT(*) FROM process_history WHERE device_id = ?", device.ID).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("process_history rows = %d, want 1", rows)
	}
	var events int
	if err := s.DB().QueryRow("SELECT COUNT(*) FROM process_events WHERE device_id = ?", device.ID).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events != 1 {
		t.Fatalf("process_events rows = %d, want 1", events)
	}

	// Backdate both tables beyond the default retention window and run the
	// same cleanup job the server schedules.
	old := timeText(now.AddDate(0, 0, -400))
	if _, err := s.DB().Exec("UPDATE process_history SET bucket = ? WHERE device_id = ?", old, device.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().Exec("UPDATE process_events SET ts = ? WHERE device_id = ?", old, device.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RunRetentionCleanup(); err != nil {
		t.Fatalf("RunRetentionCleanup: %v", err)
	}
	if err := s.DB().QueryRow("SELECT COUNT(*) FROM process_history WHERE device_id = ?", device.ID).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("old process_history rows survived cleanup: %d", rows)
	}
	if err := s.DB().QueryRow("SELECT COUNT(*) FROM process_events WHERE device_id = ?", device.ID).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events != 0 {
		t.Fatalf("old process_events rows survived cleanup: %d", events)
	}
}

func TestProcessHistoryQuery(t *testing.T) {
	s := newProcessStore(t)
	device, err := s.RegisterDevice("proc-query", "user", "owner", "")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	h := ProcessHistory{
		Samples: []ProcessHistorySample{{T: now.Unix(), Rows: []ProcessHistoryPoint{{PID: 7, Name: "bash", CPU: 3, RSS: 1024}}}},
	}
	if err := s.RecordProcessHistory(device.ID, h, now); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetProcessHistory(device.ID, now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("GetProcessHistory: %v", err)
	}
	if len(got.Series) == 0 {
		t.Fatalf("series empty: %+v", got)
	}
	if len(got.Buckets) == 0 {
		t.Fatalf("buckets empty: %+v", got)
	}
	if got.Series[0].Name != "bash" {
		t.Fatalf("series name = %q", got.Series[0].Name)
	}
	_ = fmt.Sprintf("%v", got)
}
