package collectors

import (
	"os"
	"path/filepath"
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
