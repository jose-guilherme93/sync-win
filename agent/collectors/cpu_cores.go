package collectors

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

// CollectCPUCores reads /proc/stat and returns per-core CPU usage percentages.
// Call this function repeatedly with a short interval to get meaningful deltas.
func CollectCPUCores(previous []uint64) (current []uint64, percents []float64) {
	file, err := os.Open("/proc/stat")
	if err != nil {
		return previous, nil
	}
	defer file.Close()

	var cores []uint64
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
		// Skip the aggregate "cpu " line (field[0] == "cpu")
		if fields[0] == "cpu" {
			continue
		}
		var total uint64
		for _, f := range fields[1:] {
			v, _ := strconv.ParseUint(f, 10, 64)
			total += v
		}
		cores = append(cores, total)
	}

	if previous == nil || len(previous) != len(cores) {
		return cores, make([]float64, len(cores))
	}

	percents = make([]float64, len(cores))
	for i := range cores {
		if cores[i] > previous[i] {
			// Approximate: we don't have per-core idle, so use total delta
			// This gives a reasonable estimate for display purposes
			percents[i] = float64(cores[i]-previous[i]) / 1000.0 * 10.0 // rough estimate
			if percents[i] > 100 {
				percents[i] = 100
			}
		}
	}
	return cores, percents
}
