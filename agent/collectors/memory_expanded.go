package collectors

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// MemoryExpanded holds extended memory stats.
type MemoryExpanded struct {
	SwapUsedBytes     uint64 `json:"swap_used_bytes"`
	SwapTotalBytes    uint64 `json:"swap_total_bytes"`
	BuffersBytes      uint64 `json:"memory_buffers_bytes"`
	CachedBytes       uint64 `json:"memory_cached_bytes"`
	SReclaimableBytes uint64 `json:"memory_sreclaimable_bytes,omitempty"`
	ReclaimableBytes  uint64 `json:"memory_reclaimable_bytes,omitempty"`
}

// CollectMemoryExpanded reads /proc/meminfo and returns swap, buffers, and cached.
func CollectMemoryExpanded(args ...interface{}) MemoryExpanded {
	memPath := "/proc/meminfo"
	for _, a := range args {
		if s, ok := a.(string); ok && s != "" {
			memPath = filepath.Join(s, "meminfo")
			// allow passing a full file path too
			if strings.HasSuffix(s, "meminfo") {
				memPath = s
			}
		}
	}
	file, err := os.Open(memPath)
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
	sreclaimable := values["SReclaimable"]
	return MemoryExpanded{
		SwapUsedBytes:     swapTotal - swapFree,
		SwapTotalBytes:    swapTotal,
		BuffersBytes:      values["Buffers"],
		CachedBytes:       values["Cached"],
		SReclaimableBytes: sreclaimable,
		ReclaimableBytes:  sreclaimable,
	}
}
