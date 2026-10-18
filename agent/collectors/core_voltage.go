package collectors

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// CollectCoreVoltageV returns the CPU core voltage in volts, or 0 when the
// host exposes no matching sensor.
//
// hwmon is the only portable source: every voltage channel is in millivolts
// and carries a label. A channel is only accepted when its label names a CPU
// rail (Vcore, CPU, VDDCR, ...) *and* the reading is in the plausible core
// range. Without that filter the first in*_input on a motherboard is usually a
// 12 V or 3.3 V rail, which would be shown as a CPU voltage.
func CollectCoreVoltageV() float64 {
	return collectCoreVoltageV("/sys/class/hwmon")
}

func collectCoreVoltageV(hwmonRoot string) float64 {
	inputs, err := filepath.Glob(filepath.Join(hwmonRoot, "hwmon*", "in*_input"))
	if err != nil || len(inputs) == 0 {
		return 0
	}
	sort.Strings(inputs)

	for _, input := range inputs {
		labelPath := strings.TrimSuffix(input, "_input") + "_label"
		label, err := os.ReadFile(labelPath)
		if err != nil {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(string(label)))
		if !isCPURail(name) {
			continue
		}
		data, err := os.ReadFile(input)
		if err != nil {
			continue
		}
		mv, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
		if err != nil {
			continue
		}
		volts := mv / 1000.0
		// Covers desktop and laptop core rails (0.6 V idle to ~1.5 V under
		// load); anything outside is a different rail mislabelled or a bogus
		// sensor, and reporting it would be worse than reporting nothing.
		if volts < 0.4 || volts > 2.5 {
			continue
		}
		return volts
	}
	return 0
}

func isCPURail(label string) bool {
	if label == "" {
		return false
	}
	for _, marker := range []string{"vcore", "core", "cpu", "vddcr", "svi"} {
		if strings.Contains(label, marker) {
			return true
		}
	}
	return false
}
