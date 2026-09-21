package collectors

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// CollectCPUTemperature reads thermal zones and returns the highest CPU temperature in Celsius.
func CollectCPUTemperature() float64 {
	entries, err := filepath.Glob("/sys/class/thermal/thermal_zone*/temp")
	if err != nil || len(entries) == 0 {
		return 0
	}
	var maxTemp float64
	for _, path := range entries {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		value, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
		if err != nil {
			continue
		}
		// Thermal zone values are in millidegrees Celsius
		temp := value / 1000.0
		if temp > maxTemp {
			maxTemp = temp
		}
	}
	return maxTemp
}

// CollectGPUTemperature reads GPU-specific thermal zones or hwmon sensors.
func CollectGPUTemperature() float64 {
	// Try /sys/class/drm/card*/device/hwmon/hwmon*/temp1_input
	entries, err := filepath.Glob("/sys/class/drm/card*/device/hwmon/hwmon*/temp*_input")
	if err == nil {
		for _, path := range entries {
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			value, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
			if err != nil {
				continue
			}
			return value / 1000.0
		}
	}
	return 0
}
