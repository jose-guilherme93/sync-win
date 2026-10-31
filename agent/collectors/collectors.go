package collectors

// Collector defines the interface for hardware metric collectors.
// Each collector reads from /proc or /sys and returns structured data.
type Collector interface {
	Name() string
}

// NetworkInterface holds per-interface network counters and link metadata.
// The metadata (addresses, MAC, MTU, operstate, speed) comes from the kernel
// via net.Interfaces() and /sys/class/net, and is what lets the dashboard act
// as a real network view instead of a pair of byte counters.
//
// Container and VM plumbing is reported too, but flagged. It used to be
// dropped at the source, which hid the bridges and veth pairs an operator
// needs to see when a container is the thing eating the bandwidth. Counted
// says whether the interface contributes to the fleet totals; the agent sums
// only the counted ones, so a veth pair can never inflate the numbers twice.
type NetworkInterface struct {
	Name      string   `json:"name"`
	MAC       string   `json:"mac,omitempty"`
	MTU       int      `json:"mtu,omitempty"`
	OperState string   `json:"oper_state,omitempty"`
	SpeedMbps int      `json:"speed_mbps,omitempty"`
	Addresses []string `json:"addresses,omitempty"`
	RXBytes   uint64   `json:"rx_bytes"`
	TXBytes   uint64   `json:"tx_bytes"`
	RXPackets uint64   `json:"rx_packets"`
	TXPackets uint64   `json:"tx_packets"`
	RXErrors  uint64   `json:"rx_errors"`
	TXErrors  uint64   `json:"tx_errors"`
	RXDropped uint64   `json:"rx_dropped"`
	TXDropped uint64   `json:"tx_dropped"`
	RXRate    float64  `json:"rx_rate"`
	TXRate    float64  `json:"tx_rate"`
	// Kind classifies the link: physical, wifi, tunnel, bridge, veth,
	// loopback or virtual. It is what the dashboard groups by.
	Kind string `json:"kind,omitempty"`
	// Counted is true when the interface contributes to the fleet totals.
	// False means the bytes are counted somewhere else already.
	Counted bool `json:"counted"`
	// Driver is the kernel driver behind the link (iwlwifi, r8169, virtio_net).
	Driver string `json:"driver,omitempty"`
	// Duplex is full or half on a wired link, empty when the driver does not
	// report it.
	Duplex string `json:"duplex,omitempty"`
	// CarrierChanges counts how often the link went up or down since boot. A
	// link that flaps shows up here and nowhere else.
	CarrierChanges uint64 `json:"carrier_changes,omitempty"`
	// RXFIFOErrors, TXFIFOErrors, RXCollisions and TXCarrierErrors are the
	// hardware-level counters that separate "the network is congested" from
	// "the cable is broken".
	RXFIFOErrors    uint64 `json:"rx_fifo_errors,omitempty"`
	TXFIFOErrors    uint64 `json:"tx_fifo_errors,omitempty"`
	RXCollisions    uint64 `json:"rx_collisions,omitempty"`
	TXCarrierErrors uint64 `json:"tx_carrier_errors,omitempty"`
	// Multicast is the received multicast packet count.
	Multicast uint64 `json:"multicast,omitempty"`
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

// ProcessInfo is one diagnosable process row. The agent enriches only the
// reported rows (top consumers and problem states); the counts in
// ProcessSummary cover every process on the machine.
type ProcessInfo struct {
	PID            int     `json:"pid"`
	PPID           int     `json:"ppid,omitempty"`
	Name           string  `json:"name"`
	User           string  `json:"user,omitempty"`
	State          string  `json:"state,omitempty"`
	CPUPercent     float64 `json:"cpu_percent"`
	MemRSS         uint64  `json:"mem_rss_bytes"`
	MemVirt        uint64  `json:"mem_virt_bytes,omitempty"`
	Threads        int     `json:"threads,omitempty"`
	ElapsedSeconds int64   `json:"elapsed_seconds,omitempty"`
	Command        string  `json:"command,omitempty"`
}

// ProcessSummary counts every process seen in /proc by state. It is what stops
// a top-ten list from being mistaken for the whole machine.
type ProcessSummary struct {
	Total     int `json:"total"`
	Running   int `json:"running"`
	Sleeping  int `json:"sleeping"`
	DiskSleep int `json:"disk_sleep"`
	Stopped   int `json:"stopped"`
	Zombie    int `json:"zombie"`
	Other     int `json:"other"`
	Threads   int `json:"threads"`
}

// ProcessSnapshot is one collection cycle: the diagnosable rows plus the full
// state tally. Stopped and zombie processes get their own lists so a busy
// machine cannot crowd them out of the top consumers.
type ProcessSnapshot struct {
	ByCPU   []ProcessInfo
	ByMem   []ProcessInfo
	Stopped []ProcessInfo
	Zombies []ProcessInfo
	Summary ProcessSummary
}
