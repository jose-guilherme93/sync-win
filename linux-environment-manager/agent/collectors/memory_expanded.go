package collectors

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

// MemoryExpanded holds extended memory stats.
type MemoryExpanded struct {
	SwapUsedBytes  uint64 `json:"swap_used_bytes"`
	SwapTotalBytes uint64 `json:"swap_total_bytes"`
	BuffersBytes   uint64 `json:"memory_buffers_bytes"`
	CachedBytes    uint64 `json:"memory_cached_bytes"`
}

// CollectMemoryExpanded reads /proc/meminfo and returns swap, buffers, and cached.
func CollectMemoryExpanded() MemoryExpanded {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return MemoryExpanded{}
	}
	defer file.Close()

	values := map[string]uint64{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 {
			value, _ := strconv.ParseUint(fields[1], 10, 64)
			values[strings.TrimSuffix(fields[0], ":")] = value * 1024 // kB to bytes
		}
	}

	swapTotal := values["SwapTotal"]
	swapFree := values["SwapFree"]
	return MemoryExpanded{
		SwapUsedBytes:  swapTotal - swapFree,
		SwapTotalBytes: swapTotal,
		BuffersBytes:   values["Buffers"],
		CachedBytes:    values["Cached"],
	}
}
