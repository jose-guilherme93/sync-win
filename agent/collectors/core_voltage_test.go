package collectors

import (
	"path/filepath"
	"testing"
)

func TestCollectCoreVoltageMatchesCPURail(t *testing.T) {
	root := filepath.Join(t.TempDir(), "hwmon")

	// A 12 V rail sorts before the CPU rail; the label filter is what stops it
	// from being reported as a core voltage.
	writeFixture(t, filepath.Join(root, "hwmon0", "in0_label"), "12V\n")
	writeFixture(t, filepath.Join(root, "hwmon0", "in0_input"), "12000\n")
	writeFixture(t, filepath.Join(root, "hwmon0", "in1_label"), "Vcore\n")
	writeFixture(t, filepath.Join(root, "hwmon0", "in1_input"), "1350\n")

	if got := collectCoreVoltageV(root); got != 1.35 {
		t.Fatalf("voltage = %v, want 1.35", got)
	}
}

func TestCollectCoreVoltageIgnoresNonCPURails(t *testing.T) {
	root := filepath.Join(t.TempDir(), "hwmon")

	writeFixture(t, filepath.Join(root, "hwmon0", "in0_label"), "+12V\n")
	writeFixture(t, filepath.Join(root, "hwmon0", "in0_input"), "12000\n")
	writeFixture(t, filepath.Join(root, "hwmon0", "in1_label"), "+3.3V\n")
	writeFixture(t, filepath.Join(root, "hwmon0", "in1_input"), "3300\n")

	if got := collectCoreVoltageV(root); got != 0 {
		t.Fatalf("voltage = %v, want 0", got)
	}
}

func TestCollectCoreVoltageRejectsImplausibleReading(t *testing.T) {
	root := filepath.Join(t.TempDir(), "hwmon")

	writeFixture(t, filepath.Join(root, "hwmon0", "in0_label"), "Vcore\n")
	writeFixture(t, filepath.Join(root, "hwmon0", "in0_input"), "12000\n")

	if got := collectCoreVoltageV(root); got != 0 {
		t.Fatalf("voltage = %v, want 0", got)
	}
}

func TestCollectCoreVoltageMissing(t *testing.T) {
	if got := collectCoreVoltageV(filepath.Join(t.TempDir(), "hwmon")); got != 0 {
		t.Fatalf("voltage = %v, want 0", got)
	}
}
