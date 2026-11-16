package main

import (
	"os"
	"path/filepath"
	"testing"
)

// The session list lives in the helper now, so the agent's count is whatever
// the helper answers. No helper means no sessions, not an error: a device
// without the helper still updates.
func TestActiveSessionCountWithoutHelperIsZero(t *testing.T) {
	original := syncwinContract.RemoteAccess.HelperSocketPath
	syncwinContract.RemoteAccess.HelperSocketPath = filepath.Join(t.TempDir(), "absent.sock")
	t.Cleanup(func() { syncwinContract.RemoteAccess.HelperSocketPath = original })

	// A missing helper is a degraded state, not an error: the updater must not
	// refuse to run because it cannot ask how many terminals are open.
	if got := activeSessionCount(); got != 0 {
		t.Fatalf("activeSessionCount() = %d without a helper, want 0", got)
	}
}

func TestHelperSocketHasADefault(t *testing.T) {
	if socket := helperSocket(); socket == "" {
		t.Fatal("the helper socket must never be empty")
	}
}

// The destination of a hosted session comes from root-owned configuration: the
// agent must not be able to name it. This is what keeps the root helper from
// becoming a proxy for a compromised agent.
func TestHelperServerURLComesFromTheAgentUnit(t *testing.T) {
	dir := t.TempDir()
	unit := filepath.Join(dir, "sync-win-agent.service")
	body := `[Service]
ExecStart=/usr/local/bin/sync-win-agent daemon --server http://sync-win.local:8080 --device-id dev-1 --device-token tok
`
	if err := os.WriteFile(unit, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	original := agentUnitPath
	agentUnitPath = unit
	t.Cleanup(func() { agentUnitPath = original })
	t.Setenv("SYNCWIN_SERVER", "")

	got, err := helperServerURL()
	if err != nil {
		t.Fatalf("helperServerURL: %v", err)
	}
	if got != "http://sync-win.local:8080" {
		t.Fatalf("helperServerURL() = %q, want the unit's --server value", got)
	}
}

func TestHelperServerURLFailsWithoutAUnit(t *testing.T) {
	original := agentUnitPath
	agentUnitPath = filepath.Join(t.TempDir(), "missing.service")
	t.Cleanup(func() { agentUnitPath = original })
	t.Setenv("SYNCWIN_SERVER", "")

	if _, err := helperServerURL(); err == nil {
		t.Fatal("a missing unit must be reported, not silently defaulted")
	}
}

func TestHelperServerURLFailsWithoutAServerArgument(t *testing.T) {
	dir := t.TempDir()
	unit := filepath.Join(dir, "sync-win-agent.service")
	if err := os.WriteFile(unit, []byte("[Service]\nExecStart=/usr/local/bin/sync-win-agent daemon\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	original := agentUnitPath
	agentUnitPath = unit
	t.Cleanup(func() { agentUnitPath = original })
	t.Setenv("SYNCWIN_SERVER", "")

	if _, err := helperServerURL(); err == nil {
		t.Fatal("a unit without --server must be reported")
	}
}
