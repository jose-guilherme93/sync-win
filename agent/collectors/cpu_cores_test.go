package collectors

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCollectCPUCoresComputesPercent(t *testing.T) {
	root := t.TempDir()
	stat := "cpu  100 0 0 0 0 0 0 0 0 0\n" +
		"cpu0 10 0 0 90 0 0 0 0 0 0\n" +
		"cpu1 0 0 0 100 0 0 0 0 0 0\n"
	if err := os.WriteFile(filepath.Join(root, "stat"), []byte(stat), 0o644); err != nil {
		t.Fatal(err)
	}
	prevTotal := []uint64{40, 50}
	prevIdle := []uint64{40, 50}
	totals, idles, percents := CollectCPUCores(prevTotal, prevIdle, root)
	if len(percents) != 2 {
		t.Fatalf("len percents = %d, want 2", len(percents))
	}
	// cpu0: total delta 60, idle delta 50 (90-40) => busy 10/60*100=16.666...
	if percents[0] < 16.6 || percents[0] > 16.7 {
		t.Errorf("cpu0 percent = %v, want ~16.67", percents[0])
	}
	// cpu1: idle delta 50, total delta 50 => 0%
	if percents[1] != 0 {
		t.Errorf("cpu1 percent = %v, want 0", percents[1])
	}
	if totals[0] != 100 || idles[0] != 90 {
		t.Errorf("totals/idles = %v/%v, want 100/90", totals[0], idles[0])
	}
}

func TestPerCoreUsageUsesTheIdleDelta(t *testing.T) {
	before := parseCPUCores(strings.NewReader("cpu  100 0 100 800 0 0 0 0 0 0\ncpu0 50 0 50 400 0 0 0 0 0 0\ncpu1 50 0 50 400 0 0 0 0 0 0\n"))
	// cpu0 spent 800 of its 1000 new ticks busy; cpu1 spent 200 of 1000.
	after := parseCPUCores(strings.NewReader("cpu  200 0 200 1600 0 0 0 0 0 0\ncpu0 250 0 250 500 0 0 0 0 0 0\ncpu1 100 0 100 800 0 0 0 0 0 0\n"))
	_, percents := busiestSince(before, after)
	if len(percents) != 2 {
		t.Fatalf("percents = %v", percents)
	}
	if percents[0] < 79 || percents[0] > 81 {
		t.Errorf("cpu0 = %.1f%%, want ~80", percents[0])
	}
	if percents[1] < 19 || percents[1] > 21 {
		t.Errorf("cpu1 = %.1f%%, want ~20", percents[1])
	}
}

func TestPerCoreUsageCountsIowaitAsIdle(t *testing.T) {
	before := parseCPUCores(strings.NewReader("cpu  0 0 0 0 0 0 0 0 0 0\ncpu0 0 0 0 0 0 0 0 0 0 0\n"))
	// 500 busy, 300 idle, 200 iowait: the one core was 50% busy.
	after := parseCPUCores(strings.NewReader("cpu  0 0 0 0 0 0 0 0 0 0\ncpu0 500 0 0 300 200 0 0 0 0 0\n"))
	_, percents := busiestSince(before, after)
	if percents[0] < 49 || percents[0] > 51 {
		t.Fatalf("cpu0 = %.1f%%, want ~50", percents[0])
	}
}
