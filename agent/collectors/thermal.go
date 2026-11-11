package collectors

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// cpuThermalTypeKeywords are substrings (lower-cased) that identify a thermal
// zone as reporting a CPU package temperature rather than a battery, NVMe or
// other sensor.
var cpuThermalTypeKeywords = []string{
	"x86_pkg_temp", "cpu-thermal", "coretemp", "k10temp",
	"soc_thermal", "cpu", "pkg_temp", "thermal-cpu",
}

func isCPUThermalType(t string) bool {
	lower := strings.ToLower(strings.TrimSpace(t))
	for _, kw := range cpuThermalTypeKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// CollectCPUTemperature reads thermal zones and returns the highest CPU
// temperature in Celsius. CPU-package zones are preferred; the overall
// maximum is used only when no CPU zone exists.
func CollectCPUTemperature(args ...interface{}) float64 {
	sysRoot := "/sys"
	for _, a := range args {
		if s, ok := a.(string); ok && s != "" {
			sysRoot = s
		}
	}
	entries, err := filepath.Glob(filepath.Join(sysRoot, "class", "thermal", "thermal_zone*", "temp"))
	if err != nil || len(entries) == 0 {
		return 0
	}
	var cpuMax, overallMax float64
	hasCPU := false
	for _, path := range entries {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		value, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
		if err != nil {
			continue
		}
		temp := value / 1000.0
		if temp > overallMax {
			overallMax = temp
		}
		typeData, _ := os.ReadFile(filepath.Join(filepath.Dir(path), "type"))
		if isCPUThermalType(string(typeData)) {
			hasCPU = true
			if temp > cpuMax {
				cpuMax = temp
			}
		}
	}
	if hasCPU {
		return cpuMax
	}
	return overallMax
}

// CollectGPUTemperature reads GPU-specific hwmon sensors and returns the
// highest temperature across all matches (not just the first one).
func CollectGPUTemperature(args ...interface{}) float64 {
	sysRoot := "/sys"
	for _, a := range args {
		if s, ok := a.(string); ok && s != "" {
			sysRoot = s
		}
	}
	entries, err := filepath.Glob(filepath.Join(sysRoot, "class", "drm", "card*", "device", "hwmon", "hwmon*", "temp*_input"))
	if err != nil || len(entries) == 0 {
		return 0
	}
	var maxTemp float64
	found := false
	for _, path := range entries {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		value, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
		if err != nil {
			continue
		}
		temp := value / 1000.0
		if !found || temp > maxTemp {
			maxTemp = temp
			found = true
		}
	}
	if !found {
		return 0
	}
	return maxTemp
}
