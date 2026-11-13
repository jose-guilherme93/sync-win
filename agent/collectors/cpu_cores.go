package collectors

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// CollectCPUCores reads /proc/stat and returns per-core total and idle
// counters plus per-core usage percentages.
//
// previous counters are optional: when they are missing or mismatched in
// length, the percentages come back zero (first sample). `idle + iowait`
// counts as idle.
func CollectCPUCores(args ...interface{}) ([]uint64, []uint64, []float64) {
	var prevTotal, prevIdle []uint64
	procRoot := "/proc"
	for _, a := range args {
		switch v := a.(type) {
		case string:
			if v != "" {
				procRoot = v
			}
		case []uint64:
			if prevTotal == nil {
				prevTotal = v
			} else if prevIdle == nil {
				prevIdle = v
			}
		}
	}

	file, err := os.Open(filepath.Join(procRoot, "stat"))
	if err != nil {
		// Fall back to the legacy hardcoded path.
		file, err = os.Open("/proc/stat")
		if err != nil {
			return prevTotal, prevIdle, nil
		}
	}
	defer file.Close()

	var totals []uint64
	var idles []uint64
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "cpu") {
			break
		}
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		if fields[0] == "cpu" {
			continue
		}
		var total uint64
		for _, f := range fields[1:] {
			v, _ := strconv.ParseUint(f, 10, 64)
			total += v
		}
		// fields[4]=idle, fields[5]=iowait (both counted as idle).
		var idle uint64
		if len(fields) >= 5 {
			iv, _ := strconv.ParseUint(fields[4], 10, 64)
			idle += iv
		}
		if len(fields) >= 6 {
			iw, _ := strconv.ParseUint(fields[5], 10, 64)
			idle += iw
		}
		totals = append(totals, total)
		idles = append(idles, idle)
	}

	if prevTotal == nil || len(prevTotal) != len(totals) ||
		prevIdle == nil || len(prevIdle) != len(totals) {
		return totals, idles, make([]float64, len(totals))
	}

	percents := make([]float64, len(totals))
	for i := range totals {
		totalDelta := int64(totals[i]) - int64(prevTotal[i])
		idleDelta := int64(idles[i]) - int64(prevIdle[i])
		if totalDelta > 0 {
			busy := float64(totalDelta-idleDelta) / float64(totalDelta) * 100.0
			if busy < 0 {
				busy = 0
			}
			if busy > 100 {
				busy = 100
			}
			percents[i] = busy
		}
	}
	return totals, idles, percents
}
