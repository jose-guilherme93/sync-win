package collectors

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"time"
)

// AgentImpact holds the agent's own resource usage.
type AgentImpact struct {
	CPUPercent    float64 `json:"agent_cpu_percent"`
	MemoryBytes   uint64  `json:"agent_memory_bytes"`
	PreviousUtime uint64
	PreviousStime uint64
	PreviousAt    time.Time
}

// CollectAgentImpact reads /proc/self/stat and /proc/self/status
// to measure the agent process's own CPU and memory usage.
func CollectAgentImpact(prev AgentImpact) AgentImpact {
	result := AgentImpact{
		PreviousUtime: prev.PreviousUtime,
		PreviousStime: prev.PreviousStime,
		PreviousAt:    prev.PreviousAt,
	}

	// Read memory from /proc/self/status
	if file, err := os.Open("/proc/self/status"); err == nil {
		defer file.Close()
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "VmRSS:") {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					kb, _ := strconv.ParseUint(fields[1], 10, 64)
					result.MemoryBytes = kb * 1024
				}
			}
		}
	}

	// Read CPU times from /proc/self/stat
	// Fields: pid comm state ppid ... utime(14) stime(15)
	if file, err := os.Open("/proc/self/stat"); err == nil {
		defer file.Close()
		scanner := bufio.NewScanner(file)
		if scanner.Scan() {
			line := scanner.Text()
			// Find the closing paren of comm to skip it
			lastParen := strings.LastIndex(line, ")")
			if lastParen >= 0 {
				fields := strings.Fields(line[lastParen+2:]) // skip ") "
				if len(fields) >= 13 {
					utime, _ := strconv.ParseUint(fields[11], 10, 64) // field index 12-2=10 relative to after )
					stime, _ := strconv.ParseUint(fields[12], 10, 64) // field index 13-2=11 relative to after )

					now := time.Now()
					if !prev.PreviousAt.IsZero() && now.After(prev.PreviousAt) {
						elapsed := now.Sub(prev.PreviousAt).Seconds()
						if elapsed > 0 {
							totalDelta := (utime + stime) - (prev.PreviousUtime + prev.PreviousStime)
							// Get CPU count for percentage
							cpuCount := getCPUCount()
							ticksPerSecond := 100.0 // USER_HZ on most Linux systems
							result.CPUPercent = float64(totalDelta) / (elapsed * ticksPerSecond * float64(cpuCount)) * 100.0
							if result.CPUPercent > 100 {
								result.CPUPercent = 100
							}
						}
					}
					result.PreviousUtime = utime
					result.PreviousStime = stime
					result.PreviousAt = now
				}
			}
		}
	}

	return result
}

func getCPUCount() int {
	file, err := os.Open("/proc/stat")
	if err != nil {
		return 1
	}
	defer file.Close()

	count := 0
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "cpu") && len(line) > 3 && line[3] != ' ' {
			count++
		}
	}
	if count == 0 {
		return 1
	}
	return count
}
