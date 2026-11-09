package collectors

import (
	"strings"
	"testing"
)

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
