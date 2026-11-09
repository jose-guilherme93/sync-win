package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The automatic updater runs as root from its own systemd unit and has no
// channel back to the dashboard: it prints to the journal and exits. The
// dashboard needs to answer "which devices are stuck, and why", so the outcome
// is written to a small file that the daemon already has permission to read and
// that rides back on the next telemetry post.
//
// This is deliberately a separate file from agent-state.json. The daemon owns
// that one and rewrites it every cycle; the updater writing to it as root would
// leave a root-owned file the service user could no longer replace.
const agentUpdateStateFile = "agent-update-state.json"

// Update status values. "unknown" is not stored: an absent file means the
// updater has never run on this device, which is different from a failure and
// must not be presented as one.
const (
	agentUpdateRequested = "requested" // daemon saw a newer version and asked
	agentUpdateCurrent   = "current"   // checked, nothing to do
	agentUpdateUpdated   = "updated"   // binary replaced and restarted
	agentUpdateFailed    = "failed"    // last attempt failed; Error says why
)

type agentUpdateState struct {
	Status      string `json:"status,omitempty"`
	FromVersion string `json:"from_version,omitempty"`
	ToVersion   string `json:"to_version,omitempty"`
	Error       string `json:"error,omitempty"`
	CheckedAt   string `json:"checked_at,omitempty"`
}

// systemStateDir is where the installed units keep agent state. The daemon gets
// it from systemd's StateDirectory; the root updater may not, so it is named
// here as the shared location.
const systemStateDir = "/var/lib/sync-win"

func stateDirectoryFromEnv() string {
	raw := strings.TrimSpace(os.Getenv("STATE_DIRECTORY"))
	if raw == "" {
		return ""
	}
	// systemd passes a colon-separated list; the first entry is the primary.
	if i := strings.IndexByte(raw, ':'); i >= 0 {
		raw = raw[:i]
	}
	return strings.TrimSpace(raw)
}

func agentUpdateStatePath() string {
	// The daemon resolves its state through systemd's StateDirectory. The
	// updater runs from its own unit, and older devices installed a template
	// without StateDirectory, so for root the same lookup falls into
	// /root/.local/state and the dashboard would never see the outcome. Prefer
	// an explicit directory, then the shared system path that both units use,
	// and only then the daemon's own resolution.
	if dir := stateDirectoryFromEnv(); dir != "" {
		return filepath.Join(dir, agentUpdateStateFile)
	}
	// The system updater runs as root and must write where the daemon reads.
	// Root's XDG_STATE_HOME would be /root/.local/state, so the shared path wins
	// for uid 0 even when the environment happens to set one.
	if os.Geteuid() == 0 {
		if info, err := os.Stat(systemStateDir); err == nil && info.IsDir() {
			return filepath.Join(systemStateDir, agentUpdateStateFile)
		}
	}
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return filepath.Join(dir, "sync-win", agentUpdateStateFile)
	}
	if info, err := os.Stat(systemStateDir); err == nil && info.IsDir() {
		return filepath.Join(systemStateDir, agentUpdateStateFile)
	}
	return filepath.Join(filepath.Dir(stateFilePath()), agentUpdateStateFile)
}

func loadAgentUpdateState() agentUpdateState {
	var state agentUpdateState
	data, err := os.ReadFile(agentUpdateStatePath())
	if err != nil {
		return state
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return agentUpdateState{}
	}
	return state
}

// saveAgentUpdateState records the outcome for the dashboard. Best effort: a
// failure here must never turn a successful update into a reported failure.
func saveAgentUpdateState(state agentUpdateState) {
	path := agentUpdateStatePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	state.CheckedAt = time.Now().UTC().Format(time.RFC3339)
	payload, err := json.Marshal(state)
	if err != nil {
		return
	}
	// The directory is writable by the service user while this runs as root, so
	// a predictable name could be a symlink planted there to make root truncate
	// another file. Create the temp file exclusively and write through its
	// descriptor.
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+agentUpdateStateFile+".*")
	if err != nil {
		return
	}
	tmpName := tmp.Name()
	_ = tmp.Chmod(0o644)
	if _, err := tmp.Write(payload); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
	}
}

// applyUpdateStateToStats attaches the last updater outcome to the telemetry the
// daemon is about to send. A correlate of saveAgentUpdateState; kept apart so
// the producer can be tested without running the whole collector.
func applyUpdateStateToStats(stats *telemetryStats) {
	state := loadAgentUpdateState()
	stats.AgentUpdateStatus = state.Status
	stats.AgentUpdateTarget = state.ToVersion
	stats.AgentUpdateError = state.Error
	stats.AgentUpdateAt = state.CheckedAt
}
