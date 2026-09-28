package collectors

import (
	"bufio"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// clockTicks is USER_HZ, the resolution of the utime/stime fields in
// /proc/<pid>/stat on Linux. It is 100 on every mainstream architecture.
const clockTicks = 100.0

// ProcessCPUSample maps a PID to its cumulative CPU jiffies at the moment of
// the sample. Passing the previous sample back in is what turns a lifetime
// counter into a real percentage.
type ProcessCPUSample map[int]uint64

// CollectTopProcesses reads /proc to find the top processes by CPU and memory
// usage.
//
// prev and elapsed are the previous ProcessCPUSample and the wall time between
// the two collections. CPUPercent is the jiffies consumed during that window
// divided by the number of logical CPUs, so a process saturating a single core
// reports ~100 and a fully saturated machine tops out at 100. On the first
// collection (or after a PID is seen for the first time) there is no delta to
// work from, so CPUPercent is reported as 0 instead of a misleading lifetime
// total.
//
// The returned sample must be fed back in on the next call.
func CollectTopProcesses(limit int, cpuCores int, prev ProcessCPUSample, elapsed time.Duration) (byCPU []ProcessInfo, byMem []ProcessInfo, next ProcessCPUSample) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, nil, prev
	}

	if cpuCores < 1 {
		cpuCores = 1
	}
	seconds := elapsed.Seconds()

	var processes []ProcessInfo
	next = make(ProcessCPUSample, len(entries))

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

		next[pid] = cpuTime

		// Percentage over the sampling window, normalised per core.
		var cpuPercent float64
		if seconds > 0 {
			if before, ok := prev[pid]; ok && cpuTime > before {
				jiffies := float64(cpuTime - before)
				cpuPercent = jiffies / clockTicks / seconds / float64(cpuCores) * 100.0
				if cpuPercent < 0 {
					cpuPercent = 0
				}
				if cpuPercent > 100 {
					cpuPercent = 100
				}
			}
		}

		processes = append(processes, ProcessInfo{
			PID:        pid,
			Name:       name,
			CPUPercent: cpuPercent,
			MemRSS:     memRSS,
		})
	}

	if len(processes) == 0 {
		return nil, nil, next
	}
	if limit <= 0 || limit > len(processes) {
		limit = len(processes)
	}

	// Top CPU: the busiest processes in the window. Ties fall back to memory so
	// the ordering is stable between two samples with identical CPU usage.
	sort.Slice(processes, func(i, j int) bool {
		if processes[i].CPUPercent != processes[j].CPUPercent {
			return processes[i].CPUPercent > processes[j].CPUPercent
		}
		return processes[i].MemRSS > processes[j].MemRSS
	})
	byCPU = make([]ProcessInfo, limit)
	copy(byCPU, processes[:limit])

	// Top memory: highest resident set size, carrying the same CPU percentage
	// rather than a raw jiffies counter.
	sort.Slice(processes, func(i, j int) bool {
		if processes[i].MemRSS != processes[j].MemRSS {
			return processes[i].MemRSS > processes[j].MemRSS
		}
		return processes[i].CPUPercent > processes[j].CPUPercent
	})
	byMem = make([]ProcessInfo, limit)
	copy(byMem, processes[:limit])

	return byCPU, byMem, next
}
