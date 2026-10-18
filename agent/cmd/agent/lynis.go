package main

import (
	"os"
	"os/exec"
)

// lynisDetection remembers what was last reported. The availability of a
// package is not a per-cycle event: logging it every ten seconds filled the
// device Logs screen (which ships the agent's own journal) with duplicates.
var lynisDetection lynisState

type lynisState struct {
	known     bool
	available bool
	path      string
}

// update records the current detection and reports whether it differs from the
// last one. The first call is always a transition, so a fresh process still
// says where Lynis is exactly once.
func (s *lynisState) update(available bool, path string) bool {
	if s.known && s.available == available && s.path == path {
		return false
	}
	s.known = true
	s.available = available
	s.path = path
	return true
}

// findLynis looks on PATH first, then at the fixed locations a system package
// can land in when the service manager's PATH is minimal.
func findLynis() (string, bool) {
	if path, err := exec.LookPath("lynis"); err == nil {
		return path, true
	}
	for _, path := range []string{"/usr/bin/lynis", "/usr/sbin/lynis"} {
		if _, err := os.Stat(path); err == nil {
			return path, true
		}
	}
	return "", false
}
