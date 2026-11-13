package collectors

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCollectCPUTemperaturePrefersCPUZone(t *testing.T) {
	root := t.TempDir()
	zones := map[string][2]string{
		"thermal_zone0": {"battery", "40000"},
		"thermal_zone1": {"x86_pkg_temp", "65000"},
	}
	for name, vals := range zones {
		dir := filepath.Join(root, "class", "thermal", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "type"), []byte(vals[0]), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "temp"), []byte(vals[1]), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got := CollectCPUTemperature(root)
	if got != 65 {
		t.Errorf("cpu temp = %v, want 65", got)
	}
}

func TestCollectGPUTemperatureTakesMax(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "class", "drm", "card0", "device", "hwmon", "hwmon0")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "temp1_input"), []byte("50000"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "temp2_input"), []byte("72000"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := CollectGPUTemperature(root)
	if got != 72 {
		t.Errorf("gpu temp = %v, want 72", got)
	}
}
