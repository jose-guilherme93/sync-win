package collectors

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// CollectBattery reads /sys/class/power_supply/* and returns aggregated battery info.
func CollectBattery() (percent float64, status string) {
	entries, err := filepath.Glob("/sys/class/power_supply/*")
	if err != nil || len(entries) == 0 {
		return 0, ""
	}

	var totalCapacity float64
	var count int
	var charging bool
	var discharging bool

	for _, dir := range entries {
		// Read type to only consider batteries
		typeData, err := os.ReadFile(filepath.Join(dir, "type"))
		if err != nil || strings.TrimSpace(string(typeData)) != "Battery" {
			continue
		}

		// Read capacity
		capData, err := os.ReadFile(filepath.Join(dir, "capacity"))
		if err == nil {
			cap, err := strconv.ParseFloat(strings.TrimSpace(string(capData)), 64)
			if err == nil {
				totalCapacity += cap
				count++
			}
		}

		// Read status
		statusData, err := os.ReadFile(filepath.Join(dir, "status"))
		if err == nil {
			s := strings.TrimSpace(string(statusData))
			switch s {
			case "Charging":
				charging = true
			case "Discharging":
				discharging = true
			}
		}
	}

	if count == 0 {
		return 0, ""
	}
	percent = totalCapacity / float64(count)

	if charging {
		status = "charging"
	} else if discharging {
		status = "discharging"
	} else {
		status = "full"
	}
	return
}

// CollectPowerWatts reads instantaneous battery power draw in watts from
// /sys/class/power_supply/*. It prefers power_now (microwatts) and falls back
// to voltage_now * current_now (microvolts * microamps). Returns 0 when no
// battery or no power telemetry is available.
func CollectPowerWatts() float64 {
	entries, err := filepath.Glob("/sys/class/power_supply/*")
	if err != nil {
		return 0
	}

	var totalWatts float64
	for _, dir := range entries {
		typeData, err := os.ReadFile(filepath.Join(dir, "type"))
		if err != nil || strings.TrimSpace(string(typeData)) != "Battery" {
			continue
		}

		if power, ok := readMicroValue(dir, "power_now"); ok {
			totalWatts += power / 1_000_000
			continue
		}

		voltage, vok := readMicroValue(dir, "voltage_now")
		current, cok := readMicroValue(dir, "current_now")
		if vok && cok {
			totalWatts += (voltage * current) / 1_000_000_000_000
		}
	}
	return totalWatts
}

// readMicroValue reads a numeric sysfs attribute, returning false when the
// file is missing or unparseable.
func readMicroValue(dir, name string) (float64, bool) {
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return 0, false
	}
	value, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
	if err != nil {
		return 0, false
	}
	return value, true
}
