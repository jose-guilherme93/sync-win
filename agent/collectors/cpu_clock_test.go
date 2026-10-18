package collectors

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCollectCPUClockPrefersCpufreq(t *testing.T) {
	root := t.TempDir()
	sysCPU := filepath.Join(root, "cpu")
	cpuinfo := filepath.Join(root, "cpuinfo")

	writeFixture(t, filepath.Join(sysCPU, "cpu0", "cpufreq", "scaling_cur_freq"), "2400000\n")
	writeFixture(t, filepath.Join(sysCPU, "cpu1", "cpufreq", "scaling_cur_freq"), "2600000\n")
	// The fallback must not win when cpufreq is present.
	writeFixture(t, cpuinfo, "cpu MHz : 100.000\ncpu MHz : 100.000\n")

	if got := collectCPUClockMHz(sysCPU, cpuinfo); got != 2500 {
		t.Fatalf("clock = %v, want 2500", got)
	}
}

func TestCollectCPUClockFallsBackToCpuinfo(t *testing.T) {
	root := t.TempDir()
	sysCPU := filepath.Join(root, "cpu")
	cpuinfo := filepath.Join(root, "cpuinfo")

	writeFixture(t, cpuinfo, "processor : 0\ncpu MHz : 1500.000\nprocessor : 1\ncpu MHz : 2500.000\n")

	if got := collectCPUClockMHz(sysCPU, cpuinfo); got != 2000 {
		t.Fatalf("clock = %v, want 2000", got)
	}
}

func TestCollectCPUClockMissingSources(t *testing.T) {
	root := t.TempDir()
	if got := collectCPUClockMHz(filepath.Join(root, "cpu"), filepath.Join(root, "cpuinfo")); got != 0 {
		t.Fatalf("clock = %v, want 0", got)
	}
}
