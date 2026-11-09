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
	Mount      string `json:"mount"`
	Device     string `json:"device"`
	FSType     string `json:"fstype,omitempty"`
	Options    string `json:"options,omitempty"`
	ReadOnly   bool   `json:"read_only,omitempty"`
	TotalBytes uint64 `json:"total_bytes"`
	UsedBytes  uint64 `json:"used_bytes"`
	FreeBytes  uint64 `json:"free_bytes"`
	// AvailBytes is what an unprivileged process can still write: the ext4
	// reserved blocks are counted as free by du and df -h but are not available.
	AvailBytes    uint64  `json:"avail_bytes,omitempty"`
	ReservedBytes uint64  `json:"reserved_bytes,omitempty"`
	UsedPercent   float64 `json:"used_percent"`
	InodesTotal   uint64  `json:"inodes_total,omitempty"`
	InodesUsed    uint64  `json:"inodes_used,omitempty"`
	InodesFree    uint64  `json:"inodes_free,omitempty"`
	InodesUsedPct float64 `json:"inodes_used_percent,omitempty"`
}

// SmartTest is one entry of the drive's self-test log.
type SmartTest struct {
	Type   string `json:"type,omitempty"`
	Result string `json:"result,omitempty"`
	Hours  int64  `json:"hours,omitempty"`
}

// DiskDevice is one physical (or virtual) block device, which is the unit an
// operator replaces and runs a SMART test against.
type DiskDevice struct {
	Name         string       `json:"name"`
	Model        string       `json:"model,omitempty"`
	Serial       string       `json:"serial,omitempty"`
	SizeBytes    uint64       `json:"size_bytes"`
	Rotational   bool         `json:"rotational"`
	Removable    bool         `json:"removable,omitempty"`
	TemperatureC float64      `json:"temperature_c,omitempty"`
	ReadBytes    uint64       `json:"read_bytes,omitempty"`
	WriteBytes   uint64       `json:"write_bytes,omitempty"`
	ReadRate     float64      `json:"read_rate,omitempty"`
	WriteRate    float64      `json:"write_rate,omitempty"`
	IOPS         float64      `json:"iops,omitempty"`
	AwaitMs      float64      `json:"await_ms,omitempty"`
	UtilPercent  float64      `json:"util_percent,omitempty"`
	SMART        *SmartHealth `json:"smart,omitempty"`
}

// SmartHealth is the subset of smartctl's report worth storing. Available=false
// carries the reason (smartctl missing, permission denied) so "not readable" is
// never shown as "healthy".
type SmartHealth struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
	Passed    bool   `json:"passed"`
	// StatusKnown is false when smartctl returned a report without a verdict
	// (ATA attributes but no smart_status). "No verdict" is not "failing".
	StatusKnown      bool    `json:"status_known,omitempty"`
	TemperatureC     float64 `json:"temperature_c,omitempty"`
	PowerOnHours     int64   `json:"power_on_hours,omitempty"`
	DataWrittenBytes uint64  `json:"data_written_bytes,omitempty"`
	WearPercent      int     `json:"wear_percent,omitempty"`
	Reallocated      int64   `json:"reallocated,omitempty"`
	Pending          int64   `json:"pending,omitempty"`
	CRCErrors        int64   `json:"crc_errors,omitempty"`
	LastTestType     string  `json:"last_test_type,omitempty"`
	LastTestResult   string  `json:"last_test_result,omitempty"`
	// SMART has no calendar date for a test; this is the power-on hour it ran.
	LastTestHours int64       `json:"last_test_hours,omitempty"`
	Tests         []SmartTest `json:"tests,omitempty"`
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
