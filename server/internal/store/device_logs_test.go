package store

import (
	"testing"
	"time"
)

func registerLogDevice(t *testing.T, s *Store, hostname, ownerID string) Device {
	t.Helper()
	device, err := s.RegisterDevice(hostname, "user-"+hostname, ownerID, "")
	if err != nil {
		t.Fatalf("RegisterDevice: %v", err)
	}
	return device
}

func TestAppendDeviceLogsDeduplicatesReshippedWindow(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	device := registerLogDevice(t, store, "pc", "owner-1")

	// The agent re-ships an overlapping journal window on every sample, so the
	// same batch arrives repeatedly. Only genuinely new lines may be inserted,
	// otherwise the table grows without bound.
	batch := []DeviceLog{
		{Timestamp: "2026-10-07T13:00:00+00:00", Level: "info", Source: "systemd", Message: "Started unit."},
		{Timestamp: "2026-10-07T13:00:01+00:00", Level: "error", Source: "kernel", Message: "i/o error"},
	}

	inserted, err := store.AppendDeviceLogs(device.ID, device.OwnerID, batch)
	if err != nil {
		t.Fatalf("first AppendDeviceLogs: %v", err)
	}
	if inserted != 2 {
		t.Fatalf("first insert = %d, want 2", inserted)
	}

	inserted, err = store.AppendDeviceLogs(device.ID, device.OwnerID, batch)
	if err != nil {
		t.Fatalf("second AppendDeviceLogs: %v", err)
	}
	if inserted != 0 {
		t.Fatalf("re-shipping the same window inserted %d rows, want 0", inserted)
	}

	page, err := store.ListDeviceLogs(device.ID, DeviceLogQuery{})
	if err != nil {
		t.Fatalf("ListDeviceLogs: %v", err)
	}
	if page.Total != 2 {
		t.Fatalf("total = %d, want 2 after a duplicate shipment", page.Total)
	}

	// A partially overlapping window must add only its new lines.
	inserted, err = store.AppendDeviceLogs(device.ID, device.OwnerID, []DeviceLog{
		batch[0],
		{Timestamp: "2026-10-07T13:00:02+00:00", Level: "warn", Source: "sshd", Message: "Accepted publickey."},
	})
	if err != nil {
		t.Fatalf("third AppendDeviceLogs: %v", err)
	}
	if inserted != 1 {
		t.Fatalf("overlapping window inserted %d rows, want 1", inserted)
	}
}

func TestAppendDeviceLogsNormalizesLevelsAndTimestamps(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	device := registerLogDevice(t, store, "pc", "owner-1")

	// journald severity words and journalctl short-iso timestamps both arrive
	// from the agent. The short-iso offset has no colon, which RFC3339 rejects,
	// so it needs its own layout.
	inserted, err := store.AppendDeviceLogs(device.ID, device.OwnerID, []DeviceLog{
		{Timestamp: "2026-10-07T13:03:15-0300", Level: "warning", Source: "NetworkManager", Message: "carrier lost"},
		{Timestamp: "2026-10-07T16:03:15Z", Level: "CRIT", Source: "kernel", Message: "panic"},
		{Timestamp: "2026-10-07T16:03:16Z", Level: "something-unmapped", Source: "app", Message: "hello"},
	})
	if err != nil {
		t.Fatalf("AppendDeviceLogs: %v", err)
	}
	if inserted != 3 {
		t.Fatalf("inserted = %d, want 3", inserted)
	}

	page, err := store.ListDeviceLogs(device.ID, DeviceLogQuery{})
	if err != nil {
		t.Fatal(err)
	}
	bySource := map[string]DeviceLogEntry{}
	for _, e := range page.Entries {
		bySource[e.Source] = e
	}
	if got := bySource["NetworkManager"].Level; got != "warn" {
		t.Errorf("warning normalised to %q, want warn", got)
	}
	if got := bySource["kernel"].Level; got != "error" {
		t.Errorf("CRIT normalised to %q, want error", got)
	}
	if got := bySource["app"].Level; got != "info" {
		t.Errorf("unknown level normalised to %q, want info", got)
	}
	// The short-iso line must be stored as UTC so time filters can compare it.
	if got := bySource["NetworkManager"].TS; got != "2026-10-07T16:03:15Z" {
		t.Errorf("short-iso timestamp stored as %q, want 2026-10-07T16:03:15Z", got)
	}
}

func TestAppendDeviceLogsDropsUndatedLines(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	device := registerLogDevice(t, store, "pc", "owner-1")

	inserted, err := store.AppendDeviceLogs(device.ID, device.OwnerID, []DeviceLog{
		{Timestamp: "", Level: "info", Source: "app", Message: "no timestamp"},
		{Timestamp: "not-a-date", Level: "info", Source: "app", Message: "garbage"},
		{Timestamp: "2026-10-07T16:00:00Z", Level: "info", Source: "app", Message: "good"},
	})
	if err != nil {
		t.Fatalf("AppendDeviceLogs: %v", err)
	}
	if inserted != 1 {
		t.Fatalf("inserted = %d, want 1 (undated lines must be dropped)", inserted)
	}
	page, err := store.ListDeviceLogs(device.ID, DeviceLogQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 {
		t.Fatalf("total = %d, want 1", page.Total)
	}
}

func TestAppendDeviceLogsCapsBatchAndMessage(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	device := registerLogDevice(t, store, "pc", "owner-1")

	oversized := make([]DeviceLog, deviceLogMaxBatch+50)
	for i := range oversized {
		oversized[i] = DeviceLog{
			Timestamp: time.Now().UTC().Add(time.Duration(i) * time.Second).Format(time.RFC3339),
			Level:     "info",
			Source:    "app",
			Message:   string(make([]byte, deviceLogMaxMessageBytes+100)),
		}
	}
	inserted, err := store.AppendDeviceLogs(device.ID, device.OwnerID, oversized)
	if err != nil {
		t.Fatalf("AppendDeviceLogs: %v", err)
	}
	if inserted != deviceLogMaxBatch {
		t.Fatalf("inserted = %d, want the batch cap %d", inserted, deviceLogMaxBatch)
	}

	page, err := store.ListDeviceLogs(device.ID, DeviceLogQuery{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Entries[0].Message) != deviceLogMaxMessageBytes {
		t.Fatalf("stored message length = %d, want the cap %d",
			len(page.Entries[0].Message), deviceLogMaxMessageBytes)
	}
}

func TestListDeviceLogsFiltersPaginatesAndCounts(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	device := registerLogDevice(t, store, "pc", "owner-1")

	base := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	var batch []DeviceLog
	for i := 0; i < 30; i++ {
		level := "info"
		source := "systemd"
		switch i % 3 {
		case 0:
			level = "error"
			source = "kernel"
		case 1:
			level = "warn"
			source = "sshd"
		}
		batch = append(batch, DeviceLog{
			Timestamp: base.Add(time.Duration(i) * time.Minute).Format(time.RFC3339),
			Level:     level,
			Source:    source,
			Message:   "line",
		})
	}
	if _, err := store.AppendDeviceLogs(device.ID, device.OwnerID, batch); err != nil {
		t.Fatal(err)
	}

	// Newest first.
	page, err := store.ListDeviceLogs(device.ID, DeviceLogQuery{Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != 5 {
		t.Fatalf("page size = %d, want 5", len(page.Entries))
	}
	if page.Total != 30 {
		t.Fatalf("total = %d, want 30", page.Total)
	}
	if !page.Truncated {
		t.Error("truncated = false, want true when a page does not cover everything")
	}
	if page.Entries[0].TS <= page.Entries[4].TS {
		t.Error("entries are not ordered newest first")
	}

	// Second page continues where the first stopped.
	second, err := store.ListDeviceLogs(device.ID, DeviceLogQuery{Limit: 5, Offset: 5})
	if err != nil {
		t.Fatal(err)
	}
	if second.Entries[0].TS == page.Entries[0].TS {
		t.Error("offset 5 repeated the first row of the page")
	}

	last, err := store.ListDeviceLogs(device.ID, DeviceLogQuery{Limit: 10, Offset: 25})
	if err != nil {
		t.Fatal(err)
	}
	if len(last.Entries) != 5 {
		t.Fatalf("tail page size = %d, want 5", len(last.Entries))
	}
	if last.Truncated {
		t.Error("truncated = true on the final page")
	}

	// Level filter.
	errorsPage, err := store.ListDeviceLogs(device.ID, DeviceLogQuery{Level: "error", Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if errorsPage.Total != 10 {
		t.Fatalf("error total = %d, want 10", errorsPage.Total)
	}
	for _, e := range errorsPage.Entries {
		if e.Level != "error" {
			t.Fatalf("level filter leaked a %q entry", e.Level)
		}
	}
	// Counts ignore the level filter so the toolbar can show what each level
	// would yield.
	if errorsPage.Counts["error"] != 10 || errorsPage.Counts["warn"] != 10 || errorsPage.Counts["info"] != 10 {
		t.Errorf("counts = %v, want 10 per level", errorsPage.Counts)
	}

	// Source filter.
	sshd, err := store.ListDeviceLogs(device.ID, DeviceLogQuery{Source: "sshd", Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if sshd.Total != 10 {
		t.Fatalf("sshd total = %d, want 10", sshd.Total)
	}

	// Search.
	found, err := store.ListDeviceLogs(device.ID, DeviceLogQuery{Search: "line", Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if found.Total != 30 {
		t.Fatalf("search total = %d, want 30", found.Total)
	}
	none, err := store.ListDeviceLogs(device.ID, DeviceLogQuery{Search: "no-such-message", Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if none.Total != 0 {
		t.Fatalf("unmatched search total = %d, want 0", none.Total)
	}

	// Since filter drops everything before the cutoff.
	since, err := store.ListDeviceLogs(device.ID, DeviceLogQuery{
		Since: base.Add(20 * time.Minute),
		Limit: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if since.Total != 10 {
		t.Fatalf("since total = %d, want 10", since.Total)
	}

	// Sources list feeds the source dropdown.
	if len(found.Sources) != 3 {
		t.Errorf("sources = %v, want 3 distinct sources", found.Sources)
	}
}

func TestListDeviceLogsSearchTreatsWildcardsLiterally(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	device := registerLogDevice(t, store, "pc", "owner-1")

	if _, err := store.AppendDeviceLogs(device.ID, device.OwnerID, []DeviceLog{
		{Timestamp: "2026-10-07T12:00:00Z", Level: "info", Source: "app", Message: "disk usage 91%"},
		{Timestamp: "2026-10-07T12:00:01Z", Level: "info", Source: "app", Message: "nothing to see"},
	}); err != nil {
		t.Fatal(err)
	}

	// A bare % must not behave as a match-everything wildcard.
	page, err := store.ListDeviceLogs(device.ID, DeviceLogQuery{Search: "%", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 {
		t.Fatalf("search %% matched %d rows, want 1", page.Total)
	}
}

func TestListDeviceLogsIsScopedToDevice(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	first := registerLogDevice(t, store, "pc-a", "owner-1")
	second := registerLogDevice(t, store, "pc-b", "owner-1")

	if _, err := store.AppendDeviceLogs(first.ID, first.OwnerID, []DeviceLog{
		{Timestamp: "2026-10-07T12:00:00Z", Level: "info", Source: "app", Message: "from a"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendDeviceLogs(second.ID, second.OwnerID, []DeviceLog{
		{Timestamp: "2026-10-07T12:00:01Z", Level: "info", Source: "app", Message: "from b"},
	}); err != nil {
		t.Fatal(err)
	}

	page, err := store.ListDeviceLogs(first.ID, DeviceLogQuery{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 {
		t.Fatalf("device A total = %d, want 1", page.Total)
	}
	if page.Entries[0].Message != "from a" {
		t.Fatalf("device A got %q, want only its own line", page.Entries[0].Message)
	}
}

func TestDeleteDeviceRemovesDeviceLogs(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	device := registerLogDevice(t, store, "pc", "owner-1")

	if _, err := store.AppendDeviceLogs(device.ID, device.OwnerID, []DeviceLog{
		{Timestamp: "2026-10-07T12:00:00Z", Level: "info", Source: "app", Message: "orphan candidate"},
	}); err != nil {
		t.Fatal(err)
	}

	if err := store.DeleteDevice(device.ID); err != nil {
		t.Fatalf("DeleteDevice: %v", err)
	}

	// device_logs has no foreign key, so the rows must be gone explicitly.
	var remaining int
	if err := store.db.QueryRow("SELECT COUNT(*) FROM device_logs WHERE device_id = ?", device.ID).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("%d device log rows survived device deletion", remaining)
	}
}

func TestCleanupOldDeviceLogsRemovesOnlyExpiredRows(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	device := registerLogDevice(t, store, "pc", "owner-1")

	old := time.Now().UTC().AddDate(0, 0, -DeviceLogRetentionDays-1)
	recent := time.Now().UTC()
	if _, err := store.AppendDeviceLogs(device.ID, device.OwnerID, []DeviceLog{
		{Timestamp: old.Format(time.RFC3339), Level: "info", Source: "app", Message: "expired"},
		{Timestamp: recent.Format(time.RFC3339), Level: "info", Source: "app", Message: "kept"},
	}); err != nil {
		t.Fatal(err)
	}

	deleted, err := store.CleanupOldDeviceLogs(DeviceLogRetentionDays)
	if err != nil {
		t.Fatalf("CleanupOldDeviceLogs: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1", deleted)
	}

	page, err := store.ListDeviceLogs(device.ID, DeviceLogQuery{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || page.Entries[0].Message != "kept" {
		t.Fatalf("after cleanup got %d rows (%v), want only the recent one",
			page.Total, page.Entries)
	}

	if _, err := store.CleanupOldDeviceLogs(0); err == nil {
		t.Error("CleanupOldDeviceLogs(0) succeeded, want an error")
	}
}

func TestDeviceLogsSurviveTelemetryCyclesWithoutLogs(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	device := registerLogDevice(t, store, "pc", "owner-1")

	// This is the bug that emptied the log viewer: the agent samples the
	// journal only every Nth cycle, and each telemetry post overwrote the whole
	// hardware_json blob, so the last batch was erased by the next cycle.
	if _, err := store.AppendDeviceLogs(device.ID, device.OwnerID, []DeviceLog{
		{Timestamp: "2026-10-07T12:00:00Z", Level: "error", Source: "kernel", Message: "sampled once"},
	}); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 5; i++ {
		// Cycles where the agent did not sample the journal send no logs.
		if _, err := store.UpdateHardwareStats(device.ID, HardwareStats{CPUUsagePercent: float64(i)}); err != nil {
			t.Fatalf("UpdateHardwareStats: %v", err)
		}
	}

	page, err := store.ListDeviceLogs(device.ID, DeviceLogQuery{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 {
		t.Fatalf("after five log-less telemetry cycles total = %d, want 1", page.Total)
	}
	if page.Entries[0].Message != "sampled once" {
		t.Fatalf("kept %q, want the sampled line", page.Entries[0].Message)
	}
}