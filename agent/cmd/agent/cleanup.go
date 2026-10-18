package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const agentUnitPath = "/etc/systemd/system/sync-win-agent.service"

// removeStaleBackups deletes this project's own backup files next to the binary
// and the unit. The installer used to leave one `.bak.XXXXXX` per run; a real
// device had five binary backups (~55 MB) and two unit backups.
func removeStaleBackups(executable, unitPath string) []string {
	var patterns []string
	if executable != "" {
		patterns = append(patterns, executable+".bak.*", executable+".rollback")
	}
	if unitPath != "" {
		patterns = append(patterns, unitPath+".bak.*")
	}

	var removed []string
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		for _, path := range matches {
			if err := os.Remove(path); err == nil {
				removed = append(removed, path)
			}
		}
	}
	return removed
}

// cleanupLegacyArtifacts removes what a LEM-era install, or a previous
// installer/updater, left behind. It runs as root from the update service: the
// daemon cannot touch /etc/systemd/system or /usr/local/bin.
//
// The old policy is migrated before /etc/lem is deleted, so an operator's SSH
// choice survives the cleanup.
func cleanupLegacyArtifacts(executable string) {
	removed := removeStaleBackups(executable, agentUnitPath)

	legacyUnits := []string{"lem-agent.service", "lem-agent-update.service", "lem-agent-update.timer"}
	unitRemoved := false
	for _, name := range legacyUnits {
		path := filepath.Join("/etc/systemd/system", name)
		if _, err := os.Stat(path); err != nil {
			continue
		}
		_ = exec.Command("systemctl", "disable", "--now", name).Run()
		if err := os.Remove(path); err == nil {
			removed = append(removed, path)
			unitRemoved = true
		}
	}

	for _, path := range []string{"/usr/local/bin/lem-agent", "/usr/bin/lem-agent"} {
		if err := os.Remove(path); err == nil {
			removed = append(removed, path)
		}
	}

	oldPolicy := "/etc/lem/policy.json"
	newPolicy := "/etc/sync-win/policy.json"
	if data, err := os.ReadFile(oldPolicy); err == nil {
		if _, statErr := os.Stat(newPolicy); statErr != nil {
			if err := os.MkdirAll(filepath.Dir(newPolicy), 0o755); err == nil {
				if err := os.WriteFile(newPolicy, data, 0o600); err == nil {
					removed = append(removed, "migrated "+newPolicy)
				}
			}
		}
	}
	for _, dir := range []string{"/var/lib/lem", "/var/log/lem", "/etc/lem"} {
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		if err := os.RemoveAll(dir); err == nil {
			removed = append(removed, dir)
		}
	}

	// The LEM service user has no process once its unit and directories are
	// gone. Leave it alone if something still runs as it.
	if err := exec.Command("id", "-u", "lem").Run(); err == nil {
		if err := exec.Command("pgrep", "-u", "lem").Run(); err != nil {
			if err := exec.Command("userdel", "lem").Run(); err == nil {
				removed = append(removed, "user lem")
			}
		}
	}

	if unitRemoved {
		_ = exec.Command("systemctl", "daemon-reload").Run()
	}

	if len(removed) > 0 {
		fmt.Printf("Cleaned old agent artifacts: %s\n", strings.Join(removed, ", "))
	}
}
