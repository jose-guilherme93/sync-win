package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRemoveStaleBackupsKeepsTheLiveFiles(t *testing.T) {
	dir := t.TempDir()
	executable := filepath.Join(dir, "sync-win-agent")
	unit := filepath.Join(dir, "sync-win-agent.service")

	live := []string{executable, unit, filepath.Join(dir, "unrelated.bak.keep")}
	for _, path := range live {
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	backups := []string{
		executable + ".bak.aaaaaa",
		executable + ".bak.bbbbbb",
		executable + ".rollback",
		unit + ".bak.cccccc",
	}
	for _, path := range backups {
		if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	removed := removeStaleBackups(executable, unit)
	if len(removed) != len(backups) {
		t.Fatalf("removed %d files, want %d: %v", len(removed), len(backups), removed)
	}
	for _, path := range backups {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("backup still exists: %s", path)
		}
	}
	for _, path := range live {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("live file was removed: %s", path)
		}
	}
}

func TestRemoveStaleBackupsToleratesMissingPaths(t *testing.T) {
	if removed := removeStaleBackups("", ""); len(removed) != 0 {
		t.Fatalf("removed with no paths: %v", removed)
	}
}
