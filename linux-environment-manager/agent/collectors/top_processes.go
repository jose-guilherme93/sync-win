package collectors

import (
	"bufio"
	"os"
	"sort"
	"strconv"
	"strings"
)

// CollectTopProcesses reads /proc to find the top processes by CPU and memory usage.
// cpuCores is the number of logical CPUs; CPU percent is divided by this value.
func CollectTopProcesses(limit int, cpuCores int) (byCPU []ProcessInfo, byMem []ProcessInfo) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, nil
	}

	var processes []ProcessInfo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue // not a PID directory
		}

		// Read comm (name)
		commPath := "/proc/" + entry.Name() + "/comm"
		commData, err := os.ReadFile(commPath)
		if err != nil {
			continue
		}
		name := strings.TrimSpace(string(commData))

		// Read status for memory
		var memRSS uint64
		statusPath := "/proc/" + entry.Name() + "/status"
		if file, err := os.Open(statusPath); err == nil {
			scanner := bufio.NewScanner(file)
			for scanner.Scan() {
				line := scanner.Text()
				if strings.HasPrefix(line, "VmRSS:") {
					fields := strings.Fields(line)
					if len(fields) >= 2 {
						kb, _ := strconv.ParseUint(fields[1], 10, 64)
						memRSS = kb * 1024
					}
					break
				}
			}
			file.Close()
		}

		// Read stat for CPU time (utime + stime)
		statPath := "/proc/" + entry.Name() + "/stat"
		var cpuTime uint64
		if file, err := os.Open(statPath); err == nil {
			scanner := bufio.NewScanner(file)
			if scanner.Scan() {
				line := scanner.Text()
				lastParen := strings.LastIndex(line, ")")
				if lastParen >= 0 {
					fields := strings.Fields(line[lastParen+2:])
					if len(fields) >= 13 {
						utime, _ := strconv.ParseUint(fields[11], 10, 64)
						stime, _ := strconv.ParseUint(fields[12], 10, 64)
						cpuTime = utime + stime
					}
				}
			}
			file.Close()
		}

		if memRSS == 0 && cpuTime == 0 {
			continue
		}

		// Approximate CPU% from total CPU time (higher = more CPU used over lifetime)
		processes = append(processes, ProcessInfo{
			PID:        pid,
			Name:       name,
			CPUPercent: float64(cpuTime),
			MemRSS:     memRSS,
		})
	}

	// Sort by CPU time (descending) for top CPU
	sort.Slice(processes, func(i, j int) bool {
		return processes[i].CPUPercent > processes[j].CPUPercent
	})
	if limit > len(processes) {
		limit = len(processes)
	}
	byCPU = make([]ProcessInfo, limit)
	copy(byCPU, processes[:limit])

	// Sort by memory (descending) for top memory
	sort.Slice(processes, func(i, j int) bool {
		return processes[i].MemRSS > processes[j].MemRSS
	})
	byMem = make([]ProcessInfo, limit)
	copy(byMem, processes[:limit])

	// Normalize CPU percent: divide by cpuCores and scale to 0-100 range
	if cpuCores < 1 {
		cpuCores = 1
	}
	if len(byCPU) > 0 && byCPU[0].CPUPercent > 0 {
		ticksPerSecond := 100.0 // USER_HZ
		for i := range byCPU {
			byCPU[i].CPUPercent = byCPU[i].CPUPercent / (ticksPerSecond * float64(cpuCores)) * 100.0
			if byCPU[i].CPUPercent > 100 {
				byCPU[i].CPUPercent = 100
			}
		}
	}

	return
}
