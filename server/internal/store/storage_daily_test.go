package store

import (
	"testing"
	"time"
)

func TestStorageDailyKeepsOneRowPerMountPerDay(t *testing.T) {
	s := newAgentUsageStore(t)
	user, err := s.CreateUser("storage@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	device, err := s.RegisterDevice("pc-storage", user.ID, user.ID, "")
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	first := []DiskPartition{{Mount: "/", TotalBytes: 100, UsedBytes: 40, FreeBytes: 60}}
	if err := s.UpsertStorageDaily(device.ID, first, now); err != nil {
		t.Fatal(err)
	}
	// A later sample the same day replaces the row instead of appending.
	second := []DiskPartition{{Mount: "/", TotalBytes: 100, UsedBytes: 55, FreeBytes: 45}}
	if err := s.UpsertStorageDaily(device.ID, second, now); err != nil {
		t.Fatal(err)
	}

	history, err := s.StorageDailyHistory(device.ID, 7)
	if err != nil {
		t.Fatal(err)
	}
	points := history["/"]
	if len(points) != 1 {
		t.Fatalf("points = %d, want one row for the day", len(points))
	}
	if points[0].UsedBytes != 55 {
		t.Fatalf("used = %d, want the latest sample", points[0].UsedBytes)
	}
}

func TestStorageDailyHistoryGroupsByMountAndHonoursTheWindow(t *testing.T) {
	s := newAgentUsageStore(t)
	user, err := s.CreateUser("storage2@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	device, err := s.RegisterDevice("pc-storage2", user.ID, user.ID, "")
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	partitions := []DiskPartition{
		{Mount: "/", TotalBytes: 100, UsedBytes: 40, FreeBytes: 60},
		{Mount: "/home", TotalBytes: 200, UsedBytes: 150, FreeBytes: 50, InodesUsed: 10, InodesTotal: 100},
	}
	// Insert directly so the test can span distinct days.
	s.mu.Lock()
	for i := 0; i < 5; i++ {
		day := now.AddDate(0, 0, -i).Format("2006-01-02")
		for _, partition := range partitions {
			if _, err := s.db.Exec(`INSERT INTO storage_daily
				(device_id, mount, day, total_bytes, used_bytes, free_bytes, inodes_used, inodes_total, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				device.ID, partition.Mount, day, partition.TotalBytes,
				partition.UsedBytes+uint64(i), partition.FreeBytes, partition.InodesUsed, partition.InodesTotal, day); err != nil {
				s.mu.Unlock()
				t.Fatal(err)
			}
		}
	}
	s.mu.Unlock()

	history, err := s.StorageDailyHistory(device.ID, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(history["/"]) != 4 || len(history["/home"]) != 4 {
		t.Fatalf("mounts = %d/%d points, want 4 each (today and three back)", len(history["/"]), len(history["/home"]))
	}
	home := history["/home"]
	if home[0].InodesTotal != 100 || home[0].InodesUsed != 10 {
		t.Fatalf("inodes were not persisted: %+v", home[0])
	}
}
