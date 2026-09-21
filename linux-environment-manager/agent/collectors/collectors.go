package collectors

// Collector defines the interface for hardware metric collectors.
// Each collector reads from /proc or /sys and returns structured data.
type Collector interface {
	Name() string
}

// NetworkInterface holds per-interface network counters.
type NetworkInterface struct {
	Name      string  `json:"name"`
	RXBytes   uint64  `json:"rx_bytes"`
	TXBytes   uint64  `json:"tx_bytes"`
	RXPackets uint64  `json:"rx_packets"`
	TXPackets uint64  `json:"tx_packets"`
	RXErrors  uint64  `json:"rx_errors"`
	TXErrors  uint64  `json:"tx_errors"`
	RXRate    float64 `json:"rx_rate"`
	TXRate    float64 `json:"tx_rate"`
}

// DiskPartition holds per-mount disk space info.
type DiskPartition struct {
	Mount       string  `json:"mount"`
	Device      string  `json:"device"`
	TotalBytes  uint64  `json:"total_bytes"`
	UsedBytes   uint64  `json:"used_bytes"`
	FreeBytes   uint64  `json:"free_bytes"`
	UsedPercent float64 `json:"used_percent"`
}

// ProcessInfo holds top process resource usage.
type ProcessInfo struct {
	PID        int     `json:"pid"`
	Name       string  `json:"name"`
	CPUPercent float64 `json:"cpu_percent"`
	MemRSS     uint64  `json:"mem_rss_bytes"`
}
