package collectors

import (
	"os"
	"path/filepath"
	"testing"
)

// The health block must never fail the telemetry cycle: a kernel without one
// of the files still produces the counters that could be read.
func TestCollectNetworkHealthReadsKernelCounters(t *testing.T) {
	health := CollectNetworkHealth()
	if health.TCP.OutSegs == 0 && health.Sockets.Established == 0 && health.Sockets.Listen == 0 {
		t.Error("health block is empty on a machine with a network stack")
	}
	if health.CollectedAt != "" {
		t.Error("CollectNetworkHealth stamped a collection time; the agent owns that field")
	}
}

// The socket table is counted by state, because a device with thousands of
// TIME_WAIT sockets looks healthy in the totals and is not.
func TestCountSocketStates(t *testing.T) {
	counts := countSocketStates()
	total := counts.Established + counts.TimeWait + counts.CloseWait + counts.SynSent +
		counts.SynRecv + counts.FinWait1 + counts.FinWait2 + counts.Listen
	if total == 0 {
		t.Error("no socket states were counted on a machine with listening sockets")
	}
}

// Conntrack is only present when the kernel was built with netfilter. A
// missing file must read as "unknown", not as "zero connections".
func TestReadConntrackMissing(t *testing.T) {
	if state := readConntrackFrom(filepath.Join(t.TempDir(), "missing"), filepath.Join(t.TempDir(), "missing-max")); state != nil {
		t.Errorf("readConntrackFrom(missing) = %+v, want nil", state)
	}
}

func TestReadConntrackFrom(t *testing.T) {
	dir := t.TempDir()
	count := filepath.Join(dir, "count")
	max := filepath.Join(dir, "max")
	if err := os.WriteFile(count, []byte("42\n"), 0o600); err != nil {
		t.Fatalf("write count: %v", err)
	}
	if err := os.WriteFile(max, []byte("65536\n"), 0o600); err != nil {
		t.Fatalf("write max: %v", err)
	}

	state := readConntrackFrom(count, max)
	if state == nil {
		t.Fatal("readConntrackFrom returned nil for a readable pair")
	}
	if state.Count != 42 || state.Max != 65536 {
		t.Errorf("conntrack = %+v, want 42/65536", state)
	}
}
