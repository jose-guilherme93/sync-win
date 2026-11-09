package collectors

import (
	"bufio"
	"io"
	"os"
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

// CollectCPUCores reads /proc/stat and returns per-core usage percentages.
// Call it repeatedly with a short interval to get meaningful deltas.
func CollectCPUCores(previous []CoreStat) (current []CoreStat, percents []float64) {
	file, err := os.Open("/proc/stat")
	if err != nil {
		return previous, nil
	}
	defer file.Close()
	cores := parseCPUCores(file)
	return busiestSince(previous, cores)
}

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
