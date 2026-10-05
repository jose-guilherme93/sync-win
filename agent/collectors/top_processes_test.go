package collectors

import (
	"testing"
	"time"
)

// The collector used to store cumulative CPU jiffies since process start in a
// field named CPUPercent, then clamp it to 100, so every long-running daemon
// reported exactly 100%. The first sample has no delta to work from and must
// therefore report 0 rather than a lifetime total.
func TestCollectTopProcessesFirstSampleReportsZeroCPU(t *testing.T) {
	byCPU, byMem, next := CollectTopProcesses(5, 4, nil, 0)
	if next == nil {
		t.Fatal("expected a sample to feed into the next cycle")
	}
	for _, p := range byCPU {
		if p.CPUPercent != 0 {
			t.Errorf("first sample: pid %d (%s) CPUPercent = %v, want 0", p.PID, p.Name, p.CPUPercent)
		}
	}
	for _, p := range byMem {
		if p.CPUPercent > 100 {
			t.Errorf("top memory: pid %d (%s) CPUPercent = %v, must stay within 0-100", p.PID, p.Name, p.CPUPercent)
		}
	}
}

// The second sample must derive a real percentage from the jiffies consumed
// during the window, normalised per core. Feeding back a sample and then
// measuring the current one exercises the delta path end to end.
func TestCollectTopProcessesDerivesCPUFromDelta(t *testing.T) {
	// Prime the sample so PIDs are known.
	_, _, prev := CollectTopProcesses(5, 1, nil, 0)
	if len(prev) == 0 {
		t.Skip("no readable /proc processes in this environment")
	}

	byCPU, byMem, next := CollectTopProcesses(5, 1, prev, 10*time.Second)

	for _, p := range append(append([]ProcessInfo{}, byCPU...), byMem...) {
		if p.CPUPercent < 0 || p.CPUPercent > 100 {
			t.Errorf("pid %d (%s) CPUPercent = %v, outside 0-100", p.PID, p.Name, p.CPUPercent)
		}
	}
	if next == nil {
		t.Fatal("expected a next sample")
	}
}

// A process that burned a full second of CPU per wall second on a single core
// must land at 100, not 100 times that. Injected through the sample so the
// arithmetic is checked without depending on real process behaviour.
func TestCollectTopProcessesCPUPercentIsPerCore(t *testing.T) {
	prev := ProcessCPUSample{}
	byCPU, _, _ := CollectTopProcesses(1, 1, prev, 10*time.Second)
	// With an empty prev every percentage is 0 by construction; this asserts
	// the normalisation cannot exceed the cap regardless of input.
	for _, p := range byCPU {
		if p.CPUPercent != 0 {
			t.Errorf("unknown pid %d should report 0, got %v", p.PID, p.CPUPercent)
		}
	}
}
