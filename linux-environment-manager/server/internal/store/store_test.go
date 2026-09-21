package store

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRegisterDeviceAndSync(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	device, err := store.RegisterDevice("pc-a", "user-1", "owner-1", "")
	if err != nil {
		t.Fatalf("register device: %v", err)
	}
	if device.ID == "" || device.DeviceToken == "" {
		t.Fatal("device id or token missing")
	}

	if _, err := store.RecordHeartbeat(device.ID); err != nil {
		t.Fatalf("record heartbeat: %v", err)
	}

	saved, rejected, err := store.SavePreferenceBatch(device.ID, []PreferenceInput{{
		Category:     "kde",
		Filename:     "kdeglobals",
		RelativePath: "kdeglobals",
		Content:      "[General]\nColorScheme=Dark\n",
	}})
	if err != nil {
		t.Fatalf("save preference batch: %v", err)
	}
	if len(rejected) != 0 {
		t.Fatalf("expected no rejections, got %#v", rejected)
	}
	if len(saved) != 1 {
		t.Fatalf("expected 1 saved file, got %d", len(saved))
	}
	savedAgain, _, err := store.SavePreferenceBatch(device.ID, []PreferenceInput{{
		Category:     "kde",
		Filename:     "kdeglobals",
		RelativePath: "kdeglobals",
		Content:      "[General]\nColorScheme=Dark\n",
	}})
	if err != nil {
		t.Fatalf("repeat save: %v", err)
	}
	if len(savedAgain) != 0 {
		t.Fatalf("expected unchanged file to be skipped, got %d entries", len(savedAgain))
	}
	if saved[0].ContentHash == "" {
		t.Fatal("content hash missing")
	}

	device, err = store.GetDevice(device.ID)
	if err != nil {
		t.Fatalf("get device: %v", err)
	}
	if device.LastSyncAt.IsZero() {
		t.Fatal("last sync should be set")
	}
	if !device.LastSeenAt.After(time.Time{}) {
		t.Fatal("last seen should be set")
	}

	files, err := store.ListFiles(device.ID)
	if err != nil {
		t.Fatalf("list files: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file entry, got %d", len(files))
	}
}

func TestSavePreferenceBatchRejections(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	device, err := store.RegisterDevice("reject-pc", "user", "owner", "")
	if err != nil {
		t.Fatalf("register device: %v", err)
	}

	items := []PreferenceInput{
		{Category: "general", Filename: "ok.txt", RelativePath: "ok.txt", Content: "plain text\n"},
		{Category: "general", Filename: ".", RelativePath: "", Content: "missing name\n"},
		{Category: "general", Filename: strings.Repeat("long", 64), RelativePath: "x", Content: "name too long\n"},
		{Category: "bad category!", Filename: "cat.txt", RelativePath: "cat.txt", Content: "bad category\n"},
		{Category: "../escape", Filename: "esc.txt", RelativePath: "../../etc/passwd", Content: "traversal\n"},
		{Category: "general", Filename: "id_rsa", RelativePath: ".ssh/id_rsa", Content: "key material\n"},
		{Category: "general", Filename: "leak.txt", RelativePath: "leak.txt", Content: "token = AKIAABCDEFGHIJKLMNOP\n"},
		{Category: "general", Filename: "bin.txt", RelativePath: "bin.txt", Content: "text\x00binary"},
		{Category: "general", Filename: "big.txt", RelativePath: "big.txt", Content: strings.Repeat("a", maxFileBytes+1)},
	}
	for i := range items {
		if items[i].Category == "" && items[i].Filename != "" {
			items[i].Category = "general"
		}
	}

	saved, rejected, err := store.SavePreferenceBatch(device.ID, items)
	if err != nil {
		t.Fatalf("save preference batch: %v", err)
	}
	if len(saved) != 1 || saved[0].Filename != "ok.txt" {
		t.Fatalf("expected only ok.txt saved, got %#v", saved)
	}
	if len(rejected) != len(items)-1 {
		t.Fatalf("expected %d rejections, got %d: %#v", len(items)-1, len(rejected), rejected)
	}
	rejectedNames := map[string]string{}
	for _, rejection := range rejected {
		rejectedNames[rejection.Filename] = rejection.Reason
	}
	for _, name := range []string{".", strings.Repeat("long", 64), "cat.txt", "esc.txt", "id_rsa", "leak.txt", "bin.txt", "big.txt"} {
		if reason, ok := rejectedNames[name]; !ok || reason == "" {
			t.Fatalf("expected rejection for %q, got %#v", name, rejectedNames)
		}
	}

	files, err := store.ListFiles(device.ID)
	if err != nil {
		t.Fatalf("list files: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("rejected files must not be stored, got %d entries", len(files))
	}
}

func TestFileIDCollisionAfterDelete(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	device, err := store.RegisterDevice("ids-pc", "user", "owner", "")
	if err != nil {
		t.Fatalf("register device: %v", err)
	}

	names := []string{"a.conf", "b.conf", "c.conf"}
	var middle string
	for _, name := range names {
		batch, _, err := store.SavePreferenceBatch(device.ID, []PreferenceInput{{Category: "general", Filename: name, RelativePath: name, Content: name + "\n"}})
		if err != nil {
			t.Fatalf("save %s: %v", name, err)
		}
		if name == "b.conf" {
			middle = batch[0].ID
		}
	}
	if err := store.DeletePreferenceFile(device.ID, middle); err != nil {
		t.Fatalf("delete middle file: %v", err)
	}

	fourth, _, err := store.SavePreferenceBatch(device.ID, []PreferenceInput{{Category: "general", Filename: "d.conf", RelativePath: "d.conf", Content: "d\n"}})
	if err != nil {
		t.Fatalf("fourth save: %v", err)
	}
	files, err := store.ListFiles(device.ID)
	if err != nil {
		t.Fatalf("list files: %v", err)
	}
	seen := map[string]bool{}
	for _, file := range files {
		if seen[file.ID] {
			t.Fatalf("duplicate live file id %s", file.ID)
		}
		seen[file.ID] = true
	}
	if seen[middle] || fourth[0].ID == middle {
		t.Fatalf("new file reused deleted middle id: %s", middle)
	}

	deviceIDs := make([]string, 0, 4)
	for _, suffix := range []string{"x", "y", "z"} {
		registered, err := store.RegisterDevice("ids-pc-"+suffix, "user", "owner", "")
		if err != nil {
			t.Fatalf("register device %s: %v", suffix, err)
		}
		deviceIDs = append(deviceIDs, registered.ID)
	}
	if err := store.DeleteDevice(deviceIDs[1]); err != nil {
		t.Fatalf("delete middle device: %v", err)
	}
	replacement, err := store.RegisterDevice("ids-pc-w", "user", "owner", "")
	if err != nil {
		t.Fatalf("register replacement device: %v", err)
	}
	live := map[string]bool{replacement.ID: true}
	for i, id := range deviceIDs {
		if i == 1 {
			continue
		}
		live[id] = true
	}
	if len(live) != 3 {
		t.Fatalf("duplicate live device ids detected: %#v", live)
	}
	if live[deviceIDs[1]] || replacement.ID == deviceIDs[1] {
		t.Fatalf("device id collision around deleted middle device: %#v vs %s", live, deviceIDs[1])
	}
}

func TestSyncFailureCounter(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	device, err := store.RegisterDevice("retry-pc", "user", "owner", "")
	if err != nil {
		t.Fatalf("register device: %v", err)
	}

	for range 2 {
		if err := store.RecordSyncError(device.ID, "dial tcp: refused"); err != nil {
			t.Fatalf("record sync error: %v", err)
		}
	}
	devices, err := store.ListDevicesForOwner("owner")
	if err != nil {
		t.Fatalf("list devices: %v", err)
	}
	if devices[0].SyncFailures != 2 {
		t.Fatalf("expected 2 sync failures, got %d", devices[0].SyncFailures)
	}
	if devices[0].Status != "error" {
		t.Fatalf("expected error status, got %q", devices[0].Status)
	}

	if _, err := store.RecordHeartbeat(device.ID); err != nil {
		t.Fatalf("record heartbeat: %v", err)
	}
	devices, err = store.ListDevicesForOwner("owner")
	if err != nil {
		t.Fatalf("list devices after heartbeat: %v", err)
	}
	if devices[0].SyncFailures != 0 {
		t.Fatalf("heartbeat should reset sync failures, got %d", devices[0].SyncFailures)
	}
}

func TestSessions(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	user, err := store.CreateUser("session@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	session, err := store.CreateSession(user.ID)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if len(session.Token) < 32 {
		t.Fatalf("session token too weak: %q", session.Token)
	}
	ownerID, ok := store.SessionOwner(session.Token)
	if !ok || ownerID != user.ID {
		t.Fatalf("expected owner %s for token, got %s ok=%v", user.ID, ownerID, ok)
	}
	if _, ok := store.SessionOwner("not-a-token"); ok {
		t.Fatal("unknown token must not resolve")
	}
	if _, ok := store.SessionOwner(""); ok {
		t.Fatal("empty token must not resolve")
	}

	other, err := store.CreateSession(user.ID)
	if err != nil {
		t.Fatalf("second session: %v", err)
	}
	if other.Token == session.Token {
		t.Fatal("sessions must have unique tokens")
	}
}

func TestPasswordHashing(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	if _, err := store.CreateUser("hash@example.com", "short"); err == nil {
		t.Fatal("weak password must be rejected")
	}
	user, err := store.CreateUser("hash@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	parts := strings.Split(user.PasswordHash, "$")
	if len(parts) != 3 || parts[0] != "s256" || len(parts[1]) < 16 || len(parts[2]) != 64 {
		t.Fatalf("expected salted hash s256$salt$sha256hex, got %q", user.PasswordHash)
	}

	again, err := store.CreateUser("hash@example.com", "correct horse battery staple")
	if err == nil {
		t.Fatalf("duplicate email must fail, got user %s", again.ID)
	}
	if _, err := store.AuthenticateUser("hash@example.com", "wrong password entirely"); err == nil {
		t.Fatal("wrong password must fail")
	}
	authed, err := store.AuthenticateUser("hash@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if authed.ID != user.ID {
		t.Fatalf("authenticated wrong user: %s", authed.ID)
	}
	if !strings.HasPrefix(authed.PasswordHash, "s256$") {
		t.Fatalf("legacy hash should upgrade to salted format, got %q", authed.PasswordHash)
	}
}

func TestCompleteCommandScopedToDevice(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	deviceA, err := store.RegisterDevice("cmd-a", "user", "owner", "")
	if err != nil {
		t.Fatalf("register device a: %v", err)
	}
	deviceB, err := store.RegisterDevice("cmd-b", "user", "owner", "")
	if err != nil {
		t.Fatalf("register device b: %v", err)
	}

	command, err := store.QueueInstallApp(deviceA.ID, "flatpak", "org.mozilla.firefox")
	if err != nil {
		t.Fatalf("queue install: %v", err)
	}
	if err := store.CompleteCommand(command.ID, deviceB.ID, "completed", ""); err == nil {
		t.Fatal("completing another device's command must fail")
	}
	if err := store.CompleteCommand(command.ID+"-nope", deviceA.ID, "completed", ""); err == nil {
		t.Fatal("unknown command id must fail")
	}
	if err := store.CompleteCommand(command.ID, deviceA.ID, "completed", "done"); err != nil {
		t.Fatalf("complete command: %v", err)
	}
	if pending := store.ListPendingCommands(deviceA.ID); len(pending) != 0 {
		t.Fatalf("completed command still pending: %#v", pending)
	}
}

func TestLegacyJSONMigration(t *testing.T) {
	root := t.TempDir()
	legacy := `{
	  "devices": {
	    "dev-7": {"id":"dev-7","user_id":"u1","hostname":"old-pc","status":"online","last_seen_at":"2026-01-02T03:04:05Z","last_sync_at":"2026-01-02T03:05:00Z","device_token":"legacytoken12345","created_at":"2026-01-02T03:04:05Z","updated_at":"2026-01-02T03:04:05Z"}
	  },
	  "files": {
	    "file-1": {"id":"file-1","device_id":"dev-7","user_id":"u1","category":"shell","filename":"bashrc","relative_path":".bashrc","content":"old\n","content_hash":"x","size_bytes":4,"synced_at":"2026-01-02T03:05:00Z","status":"synced"},
	    "file-2": {"id":"file-2","device_id":"dev-7","user_id":"u1","category":"shell","filename":"bashrc","relative_path":".bashrc","content":"newer\n","content_hash":"y","size_bytes":6,"synced_at":"2026-01-02T04:05:00Z","status":"synced"},
	    "file-3": {"id":"file-3","device_id":"dev-7","user_id":"u1","category":"general","filename":"notes.txt","relative_path":"","content":"note\n","content_hash":"z","size_bytes":5,"synced_at":"2026-01-02T04:06:00Z","status":"synced"}
	  },
	  "users": {
	    "usr-1": {"id":"usr-1","email":"legacy@example.com","password_hash":"s256$abcdef0123456789$0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}
	  },
	  "commands": {
	    "cmd-1": {"id":"cmd-1","device_id":"dev-7","type":"install_app","name":"vim","source":"apt","status":"queued","created_at":"2026-01-02T03:06:00Z"}
	  },
	  "sessions": {}
	}`
	if err := os.WriteFile(filepath.Join(root, legacyStateName), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}

	storeA, err := NewStore(root)
	if err != nil {
		t.Fatalf("new store with legacy json: %v", err)
	}
	defer storeA.Close()

	devices, err := storeA.ListDevices()
	if err != nil || len(devices) != 1 {
		t.Fatalf("expected migrated device, got %d (%v)", len(devices), err)
	}
	if devices[0].OwnerID != "u1" {
		t.Fatalf("expected owner fallback from user_id, got %q", devices[0].OwnerID)
	}
	if devices[0].DeviceToken != "legacytoken12345" {
		t.Fatalf("token not preserved: %q", devices[0].DeviceToken)
	}
	files, err := storeA.ListFiles("dev-7")
	if err != nil {
		t.Fatalf("list migrated files: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("duplicate legacy files were not deduped, got %d", len(files))
	}
	for _, file := range files {
		if file.Filename == "bashrc" && file.Content != "newer\n" {
			t.Fatalf("dedupe kept wrong version: %q", file.Content)
		}
	}
	user, err := storeA.AuthenticateUser("legacy@example.com", "definitely-wrong")
	if err == nil || user.ID != "" {
		t.Fatal("migrated hash should not match a random password")
	}
	if _, ok := storeA.SessionOwner("nope"); ok {
		t.Fatal("empty sessions table should not resolve tokens")
	}
	if _, err := os.Stat(filepath.Join(root, legacyStateName)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("legacy file should be renamed after migration, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, legacyStateName+".migrated")); err != nil {
		t.Fatalf("migrated archive missing: %v", err)
	}

	storeB, err := NewStore(root)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer storeB.Close()
	devices, err = storeB.ListDevices()
	if err != nil || len(devices) != 1 {
		t.Fatalf("reopened store lost migrated device: %d (%v)", len(devices), err)
	}
}

func TestStorePersistsAcrossInstances(t *testing.T) {
	root := t.TempDir()

	storeA, err := NewStore(root)
	if err != nil {
		t.Fatalf("new store a: %v", err)
	}

	device, err := storeA.RegisterDevice("pc-b", "user-2", "owner-2", "")
	if err != nil {
		t.Fatalf("register device: %v", err)
	}

	_, _, err = storeA.SavePreferenceBatch(device.ID, []PreferenceInput{{
		Category:     "gtk",
		Filename:     "settings.ini",
		RelativePath: "settings.ini",
		Content:      "[Settings]\nTheme=dark\n",
	}})
	if err != nil {
		t.Fatalf("save preference batch: %v", err)
	}
	session, err := storeA.CreateSession(func() string {
		user, err := storeA.CreateUser("persist@example.com", "correct horse battery staple")
		if err != nil {
			t.Fatalf("create user: %v", err)
		}
		return user.ID
	}())
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := storeA.UpdateHardwareStats(device.ID, HardwareStats{
		CPUUsagePercent:  31.5,
		MemoryTotalBytes: 8 * 1024 * 1024 * 1024,
		MemoryUsedBytes:  3 * 1024 * 1024 * 1024,
		OperatingSystem:  "Test Linux",
	}); err != nil {
		t.Fatalf("update hardware stats: %v", err)
	}

	storeB, err := NewStore(root)
	if err != nil {
		t.Fatalf("new store b: %v", err)
	}

	devices, err := storeB.ListDevices()
	if err != nil {
		t.Fatalf("list devices: %v", err)
	}
	if len(devices) != 1 {
		t.Fatalf("expected 1 persisted device, got %d", len(devices))
	}
	if devices[0].Hardware.CPUUsagePercent != 31.5 {
		t.Fatalf("expected persisted cpu usage 31.5, got %v", devices[0].Hardware.CPUUsagePercent)
	}

	if ownerID, ok := storeB.SessionOwner(session.Token); !ok || ownerID == "" {
		t.Fatalf("session did not persist across instances (owner=%q ok=%v)", ownerID, ok)
	}

	files, err := storeB.ListFiles(device.ID)
	if err != nil {
		t.Fatalf("list files: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 persisted file, got %d", len(files))
	}
	if files[0].Filename != "settings.ini" {
		t.Fatalf("expected persisted filename settings.ini, got %q", files[0].Filename)
	}
}

func TestDeviceHealthTransitions(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	device, err := store.RegisterDevice("health-pc", "user", "owner", "")
	if err != nil {
		t.Fatalf("register device: %v", err)
	}
	if err := store.RecordSyncError(device.ID, "connection refused"); err != nil {
		t.Fatalf("record error: %v", err)
	}
	devices, err := store.ListDevicesForOwner("owner")
	if err != nil {
		t.Fatalf("list devices: %v", err)
	}
	if devices[0].Status != "error" || devices[0].LastError == "" {
		t.Fatalf("expected error health, got %#v", devices[0])
	}
	if _, err := store.UpdateHardwareStats(device.ID, HardwareStats{}); err != nil {
		t.Fatalf("recover device: %v", err)
	}
	devices, err = store.ListDevicesForOwner("owner")
	if err != nil {
		t.Fatalf("list recovered device: %v", err)
	}
	if devices[0].Status != "online" || devices[0].LastError != "" {
		t.Fatalf("expected recovered health, got %#v", devices[0])
	}
}

func TestDownsampledTelemetry(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	device, err := store.RegisterDevice("pc-agg", "user-1", "owner-1", "")
	if err != nil {
		t.Fatalf("register device: %v", err)
	}

	// Insert downsampled records
	now := time.Now().UTC()
	for i := 0; i < 10; i++ {
		ts := now.Add(-time.Duration(10-i) * time.Minute)
		entry := TelemetryDownsampled{
			DeviceID:    device.ID,
			Timestamp:   ts.Format(time.RFC3339),
			Resolution:  "1m",
			CPUAvg:      float64(i) * 5.0,
			CPUMin:      float64(i) * 4.0,
			CPUMax:      float64(i) * 6.0,
			MemAvg:      50.0,
			MemMin:      48.0,
			MemMax:      52.0,
			NetRXAvg:    100.0,
			NetTXAvg:    50.0,
			TempAvg:     55.0,
			TempMax:     60.0,
			PowerAvg:    15.0,
			SampleCount: 6,
		}
		if err := store.AppendTelemetryDownsampled(entry); err != nil {
			t.Fatalf("insert downsampled: %v", err)
		}
	}

	// Query range
	from := now.Add(-15 * time.Minute)
	to := now
	points, err := store.GetDownsampledRange(device.ID, "1m", from, to)
	if err != nil {
		t.Fatalf("query downsampled: %v", err)
	}
	if len(points) != 10 {
		t.Fatalf("expected 10 points, got %d", len(points))
	}
	if points[0].Resolution != "1m" {
		t.Fatalf("expected resolution 1m, got %s", points[0].Resolution)
	}
}

func TestRetentionSettings(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	// Get default settings
	rs, err := store.GetRetentionSettings("owner-1")
	if err != nil {
		t.Fatalf("get retention: %v", err)
	}
	if rs.Resolution1mDays != 7 {
		t.Fatalf("expected default 1m days = 7, got %d", rs.Resolution1mDays)
	}

	// Update settings
	rs.Resolution1mDays = 14
	rs.Resolution5mDays = 60
	if err := store.UpdateRetentionSettings(*rs); err != nil {
		t.Fatalf("update retention: %v", err)
	}

	// Verify update
	rs2, err := store.GetRetentionSettings("owner-1")
	if err != nil {
		t.Fatalf("get updated retention: %v", err)
	}
	if rs2.Resolution1mDays != 14 || rs2.Resolution5mDays != 60 {
		t.Fatalf("expected updated values, got 1m=%d 5m=%d", rs2.Resolution1mDays, rs2.Resolution5mDays)
	}
}

func TestCleanupDeviceTelemetry(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	device, err := store.RegisterDevice("pc-clean", "user-1", "owner-1", "")
	if err != nil {
		t.Fatalf("register device: %v", err)
	}

	// Insert raw telemetry
	payload := []byte(`{"cpu_usage_percent":50,"memory_used_bytes":1000,"memory_total_bytes":2000}`)
	if err := store.AppendTelemetry(device.ID, payload); err != nil {
		t.Fatalf("append telemetry: %v", err)
	}

	// Insert downsampled
	entry := TelemetryDownsampled{
		DeviceID:   device.ID,
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
		Resolution: "1m",
		CPUAvg:     50.0,
	}
	if err := store.AppendTelemetryDownsampled(entry); err != nil {
		t.Fatalf("insert downsampled: %v", err)
	}

	// Cleanup
	if err := store.CleanupDeviceTelemetry(device.ID); err != nil {
		t.Fatalf("cleanup: %v", err)
	}

	// Verify empty
	points, err := store.GetTelemetryHistory(device.ID, 100)
	if err != nil {
		t.Fatalf("query after cleanup: %v", err)
	}
	if len(points) != 0 {
		t.Fatalf("expected 0 raw points after cleanup, got %d", len(points))
	}

	downsampled, err := store.GetDownsampledRange(device.ID, "1m", time.Now().UTC().Add(-1*time.Hour), time.Now().UTC())
	if err != nil {
		t.Fatalf("query downsampled after cleanup: %v", err)
	}
	if len(downsampled) != 0 {
		t.Fatalf("expected 0 downsampled points after cleanup, got %d", len(downsampled))
	}
}

func TestResolutionForInterval(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		fromOffset time.Duration
		toOffset   time.Duration
		want       string
	}{
		{-1 * time.Hour, 0, "raw"},           // 1 hour → raw
		{-3 * time.Hour, 0, "1m"},            // 3 hours → 1m
		{-12 * time.Hour, 0, "5m"},           // 12 hours → 5m
		{-7 * 24 * time.Hour, 0, "1h"},       // 7 days → 1h
		{-30 * 24 * time.Hour, 0, "1h"},      // 30 days → 1h
	}

	for _, tt := range tests {
		got := ResolutionForInterval(now.Add(tt.fromOffset), now.Add(tt.toOffset))
		if got != tt.want {
			t.Errorf("ResolutionForInterval(%s) = %s, want %s", tt.fromOffset, got, tt.want)
		}
	}
}
