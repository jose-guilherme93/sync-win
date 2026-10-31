package collectors

import (
	"os"
	"testing"
	"time"
)

func TestParseProcStatLine(t *testing.T) {
	// comm may contain spaces and parentheses; the parser must use the last ")".
	line := "42 (weird (name)) S 7 0 0 0 -1 0 0 0 0 0 300 200 0 0 20 0 8 0 5000 123456 10 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0"

	stat, ok := parseProcStat(42, line)
	if !ok {
		t.Fatal("valid stat line rejected")
	}
	if stat.name != "weird (name)" {
		t.Errorf("name = %q", stat.name)
	}
	if stat.state != "S" || stat.ppid != 7 {
		t.Errorf("state/ppid = %q/%d", stat.state, stat.ppid)
	}
	if stat.cpuTime != 500 {
		t.Errorf("cpuTime = %d, want 500 (utime+stime)", stat.cpuTime)
	}
	if stat.threads != 8 || stat.startTicks != 5000 {
		t.Errorf("threads/start = %d/%d", stat.threads, stat.startTicks)
	}
	if stat.virt != 123456 || stat.rss != 10*uint64(os.Getpagesize()) {
		t.Errorf("virt/rss = %d/%d", stat.virt, stat.rss)
	}

	if _, ok := parseProcStat(42, "not a stat line"); ok {
		t.Fatal("malformed line accepted")
	}
}

// The first sample has no delta to work from and must report 0 rather than a
// lifetime total.
func TestCollectProcessesFirstSampleReportsZeroCPU(t *testing.T) {
	snapshot, next := CollectProcesses(5, 5, 200, 4, nil, 0)
	if next == nil {
		t.Fatal("expected a sample to feed into the next cycle")
	}
	for _, p := range snapshot.ByCPU {
		if p.CPUPercent != 0 {
			t.Errorf("first sample: pid %d (%s) CPUPercent = %v, want 0", p.PID, p.Name, p.CPUPercent)
		}
	}
	for _, p := range snapshot.ByMem {
		if p.CPUPercent > 100 {
			t.Errorf("top memory: pid %d (%s) CPUPercent = %v, must stay within 0-100", p.PID, p.Name, p.CPUPercent)
		}
	}
}

// The second sample derives a real percentage from the jiffies consumed during
// the window and never exceeds the machine-share cap.
func TestCollectProcessesDerivesCPUFromDelta(t *testing.T) {
	_, prev := CollectProcesses(5, 5, 200, 1, nil, 0)
	if len(prev) == 0 {
		t.Skip("no readable /proc processes in this environment")
	}

	snapshot, next := CollectProcesses(5, 5, 200, 1, prev, 10*time.Second)
	lists := append(append([]ProcessInfo{}, snapshot.ByCPU...), snapshot.ByMem...)
	for _, p := range lists {
		if p.CPUPercent < 0 || p.CPUPercent > 100 {
			t.Errorf("pid %d (%s) CPUPercent = %v, outside 0-100", p.PID, p.Name, p.CPUPercent)
		}
	}
	if next == nil {
		t.Fatal("expected a next sample")
	}
}

// The summary must account for every process, and the problem lists must only
// contain their own states. A top-ten list shown as "the machine" is exactly
// the misleading impression this guards against.
func TestCollectProcessesSummarizesEveryState(t *testing.T) {
	snapshot, _ := CollectProcesses(5, 5, 200, 4, nil, 0)

	summary := snapshot.Summary
	if summary.Total == 0 {
		t.Skip("no readable /proc processes in this environment")
	}
	counted := summary.Running + summary.Sleeping + summary.DiskSleep + summary.Stopped + summary.Zombie + summary.Other
	if counted != summary.Total {
		t.Errorf("state counts = %d, total = %d", counted, summary.Total)
	}
	if summary.Threads < summary.Total {
		t.Errorf("threads = %d, must be at least one per process (%d)", summary.Threads, summary.Total)
	}

	for _, p := range snapshot.Stopped {
		if p.State != "T" && p.State != "t" {
			t.Errorf("stopped list contains pid %d in state %q", p.PID, p.State)
		}
	}
	for _, p := range snapshot.Zombies {
		if p.State != "Z" {
			t.Errorf("zombie list contains pid %d in state %q", p.PID, p.State)
		}
	}
	if len(snapshot.ByCPU) > 5 || len(snapshot.ByMem) > 5 {
		t.Errorf("top lists exceeded their limit: cpu=%d mem=%d", len(snapshot.ByCPU), len(snapshot.ByMem))
	}
}

// The enriched rows carry the facts a human needs to identify a process.
func TestCollectProcessesReportsUserAndCommandForTopRows(t *testing.T) {
	snapshot, _ := CollectProcesses(3, 3, 80, 4, nil, 0)
	if len(snapshot.ByCPU) == 0 {
		t.Skip("no readable /proc processes in this environment")
	}
	for _, p := range snapshot.ByCPU {
		if p.Command == "" {
			t.Errorf("pid %d (%s) has no command", p.PID, p.Name)
		}
		if len([]rune(p.Command)) > 81 {
			t.Errorf("pid %d command was not truncated: %d runes", p.PID, len([]rune(p.Command)))
		}
	}
}
