package collectors

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// CollectCPUClockMHz returns the current average CPU frequency in MHz.
//
// cpufreq is the accurate source: scaling_cur_freq is what the governor asked
// for and cpuinfo_cur_freq is what the hardware reports. Both are in kHz. On
// hosts without cpufreq (most VMs and many containers) /proc/cpuinfo still has
// a "cpu MHz" line per logical CPU, which is the fallback. A missing source
// returns 0 rather than a fabricated value.
func CollectCPUClockMHz() float64 {
	return collectCPUClockMHz("/sys/devices/system/cpu", "/proc/cpuinfo")
}

func collectCPUClockMHz(sysCPU, cpuinfoPath string) float64 {
	for _, pattern := range []string{
		"cpu*/cpufreq/scaling_cur_freq",
		"cpu*/cpufreq/cpuinfo_cur_freq",
	} {
		paths, err := filepath.Glob(filepath.Join(sysCPU, pattern))
		if err != nil || len(paths) == 0 {
			continue
		}
		var sum float64
		var count int
		for _, path := range paths {
			if khz, ok := readKHz(path); ok {
				sum += khz
				count++
			}
		}
		if count > 0 {
			return sum / float64(count) / 1000.0
		}
	}

	file, err := os.Open(cpuinfoPath)
	if err != nil {
		return 0
	}
	defer func() { _ = file.Close() }()

	var sum float64
	var count int
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "cpu MHz") {
			continue
		}
		_, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		mhz, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err == nil && mhz > 0 {
			sum += mhz
			count++
		}
	}
	if count == 0 {
		return 0
	}
	return sum / float64(count)
}

func readKHz(path string) (float64, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	khz, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
	if err != nil || khz <= 0 {
		return 0, false
	}
	return khz, true
}
