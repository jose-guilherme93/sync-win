package collectors

import (
	"bufio"
	"context"
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DiskDeviceReader reports per-device throughput, latency and saturation, plus
// what SMART can tell us.
//
// Throughput and latency are deltas, so the reader keeps the previous counters.
// It is one instance owned by the daemon; there is no package-level state, so a
// test or the installer can create its own without inheriting a sample.
type DiskDeviceReader struct {
	mu      sync.Mutex
	prev    map[string]diskCounters
	prevAt  time.Time
	smartAt map[string]time.Time
	smart   map[string]*SmartHealth
}

type diskCounters struct {
	Reads        uint64
	SectorsRead  uint64
	Writes       uint64
	SectorsWrite uint64
	IOMs         uint64
	IOs          uint64
}

// SMART is an expensive, privileged call; re-running it every telemetry cycle
// would spawn a process every ten seconds for numbers that move in years.
const smartRefresh = 15 * time.Minute

func NewDiskDeviceReader() *DiskDeviceReader {
	return &DiskDeviceReader{
		prev:    map[string]diskCounters{},
		smartAt: map[string]time.Time{},
		smart:   map[string]*SmartHealth{},
	}
}

// Collect returns one entry per block device. A device whose counters were not
// seen before yields totals but no rate, which the dashboard should present as
// "no sample yet" rather than zero.
func (r *DiskDeviceReader) Collect(now time.Time) []DiskDevice {
	r.mu.Lock()
	defer r.mu.Unlock()

	stats := readDiskStats()
	devices := listBlockDevices()
	// Elapsed belongs to the sample, not to each device: updating prevAt inside
	// the loop made every device after the first see zero elapsed and report no
	// throughput at all.
	elapsed := now.Sub(r.prevAt).Seconds()

	out := make([]DiskDevice, 0, len(devices))
	for _, name := range devices {
		device := DiskDevice{Name: name}
		device.SizeBytes = readUint(filepath.Join("/sys/block", name, "size")) * 512
		device.Model = strings.TrimSpace(readString(filepath.Join("/sys/block", name, "device/model")))
		if device.Model == "" {
			device.Model = strings.TrimSpace(readString(filepath.Join("/sys/block", name, "model")))
		}
		device.Serial = strings.TrimSpace(readString(filepath.Join("/sys/block", name, "device/serial")))
		device.Rotational = strings.TrimSpace(readString(filepath.Join("/sys/block", name, "queue/rotational"))) == "1"
		device.Removable = strings.TrimSpace(readString(filepath.Join("/sys/block", name, "removable"))) == "1"
		device.TemperatureC = readBlockTemperature(name)
		device.SMART = r.smartFor(name, now)

		counters, seen := stats[name]
		if seen {
			device.ReadBytes = counters.SectorsRead * 512
			device.WriteBytes = counters.SectorsWrite * 512
			if previous, ok := r.prev[name]; ok {
				if elapsed > 0 {
					device.ReadRate = float64(delta(counters.SectorsRead, previous.SectorsRead)) * 512 / elapsed
					device.WriteRate = float64(delta(counters.SectorsWrite, previous.SectorsWrite)) * 512 / elapsed
					ios := delta(counters.Reads, previous.Reads) + delta(counters.Writes, previous.Writes)
					ioMs := delta(counters.IOMs, previous.IOMs)
					device.IOPS = float64(ios) / elapsed
					if ios > 0 {
						// Time spent doing I/O divided by I/Os is await: the
						// number an operator reads as "is this disk struggling".
						device.AwaitMs = float64(ioMs) / float64(ios)
					}
					device.UtilPercent = math.Min(100, float64(ioMs)/(elapsed*1000)*100)
				}
			}
		}
		r.prev[name] = counters
		out = append(out, device)
	}
	r.prevAt = now
	return out
}

func delta(current, previous uint64) uint64 {
	if current < previous {
		return 0
	}
	return current - previous
}

// listBlockDevices names the devices worth showing. Loopbacks, RAM disks and
// optical drives are noise; device-mapper and md are kept because LVM and RAID
// are exactly what an operator needs to see.
func listBlockDevices() []string {
	entries, err := os.ReadDir("/sys/block")
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		switch {
		case strings.HasPrefix(name, "loop"),
			strings.HasPrefix(name, "ram"),
			strings.HasPrefix(name, "zram"),
			strings.HasPrefix(name, "sr"):
			continue
		}
		out = append(out, name)
	}
	return out
}

func readDiskStats() map[string]diskCounters {
	file, err := os.Open("/proc/diskstats")
	if err != nil {
		return nil
	}
	defer func() { _ = file.Close() }()

	stats := make(map[string]diskCounters)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 14 {
			continue
		}
		counters := diskCounters{
			Reads:        atou(fields[3]),
			SectorsRead:  atou(fields[5]),
			Writes:       atou(fields[7]),
			SectorsWrite: atou(fields[9]),
			IOs:          atou(fields[11]),
			IOMs:         atou(fields[12]),
		}
		stats[fields[2]] = counters
	}
	return stats
}

func atou(value string) uint64 {
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0
	}
	return parsed
}

func readUint(path string) uint64 {
	return atou(strings.TrimSpace(readString(path)))
}

func readString(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

// readBlockTemperature looks for the drivetemp hwmon the kernel exposes for
// SATA/NVMe disks, in millidegrees.
func readBlockTemperature(name string) float64 {
	matches, err := filepath.Glob(filepath.Join("/sys/block", name, "device/hwmon*/hwmon*/temp1_input"))
	if err != nil || len(matches) == 0 {
		return 0
	}
	return float64(readUint(matches[0])) / 1000
}

// smartFor refreshes SMART at most every 15 minutes and caches the failure too:
// on a device without smartctl, spawning it every cycle would be pure cost.
func (r *DiskDeviceReader) smartFor(name string, now time.Time) *SmartHealth {
	if cached, ok := r.smart[name]; ok && now.Sub(r.smartAt[name]) < smartRefresh {
		return cached
	}
	health := readSmart(name)
	r.smart[name] = health
	r.smartAt[name] = now
	return health
}

// readSmart runs smartctl when it exists. The agent is unprivileged and most
// installs lack CAP_SYS_RAWIO, so a refusal is the normal outcome and must be
// reported as such, never as a device that passed.
func readSmart(name string) *SmartHealth {
	if name == "" || strings.HasPrefix(name, "dm-") || strings.HasPrefix(name, "md") {
		return &SmartHealth{Available: false, Reason: "virtual device"}
	}
	path, err := exec.LookPath("smartctl")
	if err != nil {
		return &SmartHealth{Available: false, Reason: "smartctl not installed"}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	// -l selftest is what makes the self-test log part of the report; without it
	// the drive details arrive but the history is empty.
	command := exec.CommandContext(ctx, path, "--json", "-H", "-A", "-i", "-l", "selftest", "-d", "auto", "/dev/"+name)
	const smartOutputLimit = 1 << 20
	capped := &cappedBuffer{limit: smartOutputLimit}
	command.Stdout = capped
	runErr := command.Run()
	out := []byte(capped.String())
	if len(out) >= smartOutputLimit {
		return &SmartHealth{Available: false, Reason: "smartctl output exceeded the size limit"}
	}
	if len(out) == 0 {
		reason := "smartctl failed"
		if runErr != nil {
			reason = runErr.Error()
		}
		return &SmartHealth{Available: false, Reason: reason}
	}

	var report map[string]any
	if err := json.Unmarshal(out, &report); err != nil {
		return &SmartHealth{Available: false, Reason: "smartctl output is not JSON"}
	}
	return parseSmartReport(report)
}

// parseSmartReport separates "the drive has no SMART" from "the drive is
// failing". A virtual or SCSI disk answers smartctl with exit 0 and no
// smart_status block at all; reporting that as a failed health check would be
// the worst possible false positive.
func parseSmartReport(report map[string]any) *SmartHealth {
	status, hasStatus := report["smart_status"].(map[string]any)
	_, hasATA := report["ata_smart_attributes"]
	nvme, hasNVMe := report["nvme_smart_health_information_log"].(map[string]any)
	if !hasNVMe {
		// Older smartctl builds nested the same keys under "nvme".
		nvme, hasNVMe = report["nvme"].(map[string]any)
	}
	if !hasStatus && !hasATA && !hasNVMe {
		return &SmartHealth{Available: false, Reason: "device does not expose SMART data"}
	}

	health := &SmartHealth{Available: true}
	if passed, ok := status["passed"].(bool); ok {
		health.Passed = passed
		health.StatusKnown = true
	}
	if temp, ok := report["temperature"].(map[string]any); ok {
		health.TemperatureC = number(temp["current"])
	}
	if power, ok := report["power_on_time"].(map[string]any); ok {
		health.PowerOnHours = int64(number(power["hours"]))
	}
	if hasNVMe {
		// NVMe counts data in 512 000-byte units.
		health.DataWrittenBytes = uint64(number(nvme["data_units_written"]) * 512000)
		health.WearPercent = int(number(nvme["percentage_used"]))
	}
	readATAAttributes(report, health)
	readSelfTest(report, health)
	return health
}

// readATAAttributes pulls the handful of ATA attributes that predict a failure
// from the generic attribute table.
func readATAAttributes(report map[string]any, health *SmartHealth) {
	ata, ok := report["ata_smart_attributes"].(map[string]any)
	if !ok {
		return
	}
	table, ok := ata["table"].([]any)
	if !ok {
		return
	}
	for _, entry := range table {
		attribute, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		id := int(number(attribute["id"]))
		raw, _ := attribute["raw"].(map[string]any)
		value := int64(number(raw["value"]))
		switch id {
		case 5:
			health.Reallocated = value
		case 197:
			health.Pending = value
		case 199:
			health.CRCErrors = value
		}
	}
}

// readSelfTest keeps the whole finished self-test log, newest first. SMART
// carries no calendar date, only the power-on hour, so that is what is stored.
func readSelfTest(report map[string]any, health *SmartHealth) {
	var entries []map[string]any
	if ata, ok := report["ata_smart_self_test_log"].(map[string]any); ok {
		if table, ok := ata["standard_table"].(map[string]any); ok {
			if list, ok := table["table"].([]any); ok {
				for _, item := range list {
					if entry, ok := item.(map[string]any); ok {
						entries = append(entries, entry)
					}
				}
			}
		}
	}
	// self-test log: top-level in current smartctl, nested in older builds.
	if log, ok := report["nvme_self_test_log"].(map[string]any); ok {
		if list, ok := log["table"].([]any); ok {
			for _, item := range list {
				if entry, ok := item.(map[string]any); ok {
					entries = append(entries, entry)
				}
			}
		}
	}
	if nested, ok := report["nvme"].(map[string]any); ok {
		if log, ok := nested["self_test_log"].(map[string]any); ok {
			if list, ok := log["table"].([]any); ok {
				for _, item := range list {
					if entry, ok := item.(map[string]any); ok {
						entries = append(entries, entry)
					}
				}
			}
		}
	}

	const maxTests = 10
	for _, entry := range entries {
		status := nestedString(entry, "status", "string")
		if status == "" {
			status = nestedString(entry, "status", "value")
		}
		if status == "" || strings.Contains(strings.ToLower(status), "progress") {
			continue
		}
		test := SmartTest{
			Type:   nestedString(entry, "type", "string"),
			Result: status,
			Hours:  int64(number(entry["lifetime_hours"])),
		}
		if test.Hours == 0 {
			test.Hours = int64(number(entry["power_on_hours"]))
		}
		health.Tests = append(health.Tests, test)
		if len(health.Tests) >= maxTests {
			break
		}
	}
	if len(health.Tests) > 0 {
		health.LastTestType = health.Tests[0].Type
		health.LastTestResult = health.Tests[0].Result
		health.LastTestHours = health.Tests[0].Hours
	}
}

func nestedString(entry map[string]any, key, field string) string {
	value, ok := entry[key].(map[string]any)
	if !ok {
		return ""
	}
	text, _ := value[field].(string)
	return strings.TrimSpace(text)
}

func number(value any) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case json.Number:
		f, _ := v.Float64()
		return f
	}
	return 0
}
