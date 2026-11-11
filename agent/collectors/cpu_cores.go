package collectors

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// CoreStat is one core's cumulative jiffies, split so a delta can produce a
// real busy share. Keeping only the total forced the old estimate to scale
// total ticks to the interval, which read as ~10% per core on an idle machine
// and on a saturated one alike.
type CoreStat struct {
	Total uint64
	Idle  uint64
}

// CollectCPUCores reads /proc/stat and returns per-core total and idle counters
// plus per-core usage percentages.
//
// previous counters are optional: when they are missing or mismatched in
// length, the percentages come back zero (first sample). `idle + iowait` counts
// as idle. An optional procRoot string overrides /proc for tests. The counters
// are returned so the caller can keep them for the next delta.
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

	cores := parseCPUCores(file)
	totals := make([]uint64, len(cores))
	idles := make([]uint64, len(cores))
	for i, core := range cores {
		totals[i] = core.Total
		idles[i] = core.Idle
	}

	var previous []CoreStat
	if prevTotal != nil && prevIdle != nil &&
		len(prevTotal) == len(cores) && len(prevIdle) == len(cores) {
		previous = make([]CoreStat, len(cores))
		for i := range cores {
			previous[i] = CoreStat{Total: prevTotal[i], Idle: prevIdle[i]}
		}
	}
	_, percents := busiestSince(previous, cores)
	return totals, idles, percents
}

// busiestSince turns two cumulative samples into a real busy share per core.
// The idle delta is what makes the number honest: scaling total ticks to the
// interval reported the same ~10% on an idle machine and a saturated one.
func busiestSince(previous, cores []CoreStat) ([]CoreStat, []float64) {
	if previous == nil || len(previous) != len(cores) {
		return cores, make([]float64, len(cores))
	}

	percents := make([]float64, len(cores))
	for i := range cores {
		totalDelta := delta(cores[i].Total, previous[i].Total)
		idleDelta := delta(cores[i].Idle, previous[i].Idle)
		if totalDelta == 0 {
			continue
		}
		if idleDelta > totalDelta {
			idleDelta = totalDelta
		}
		percents[i] = float64(totalDelta-idleDelta) / float64(totalDelta) * 100
	}
	return cores, percents
}

// parseCPUCores reads the per-core lines of /proc/stat.
func parseCPUCores(reader io.Reader) []CoreStat {
	var cores []CoreStat
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "cpu") {
			break
		}
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		// Skip the aggregate "cpu " line (fields[0] == "cpu").
		if fields[0] == "cpu" {
			continue
		}
		var total uint64
		for _, f := range fields[1:] {
			value, _ := strconv.ParseUint(f, 10, 64)
			total += value
		}
		// idle is field 4; iowait (field 5) is also time the CPU was not busy.
		idle, _ := strconv.ParseUint(fields[4], 10, 64)
		if len(fields) > 5 {
			if iowait, err := strconv.ParseUint(fields[5], 10, 64); err == nil {
				idle += iowait
			}
		}
		cores = append(cores, CoreStat{Total: total, Idle: idle})
	}

	return cores
}
