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
