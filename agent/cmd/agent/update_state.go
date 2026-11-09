package main

import (
	"encoding/json"
	"os"
	"path/filepath"
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

func agentUpdateStatePath() string {
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
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, payload, 0o644); err != nil {
		os.Remove(tmp)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
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
