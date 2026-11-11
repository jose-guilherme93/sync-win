package collectors

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ProcessSample is the cumulative counter snapshot for one pid. CPU is in
// USER_HZ jiffies (utime + stime) and the IO counters are the kernel's byte
// counters; both are turned into rates against the previous sample.
type ProcessSample struct {
	CPUJiffies uint64
	ReadBytes  uint64
	WriteBytes uint64
}

// ProcessSampleMap maps a PID to its counters at the moment of a collection.
type ProcessSampleMap map[int]ProcessSample

// ProcessPort is one listening socket owned by a process, cross-referenced
// from /proc/net/{tcp,tcp6,udp,udp6} through the socket inode in
// /proc/<pid>/fd. It is one of the few genuinely useful joins in /proc.
type ProcessPort struct {
	Proto string `json:"proto"`
	Port  int    `json:"port"`
	Addr  string `json:"addr"`
}

// ProcessRow is the full process row the Processes tab renders. It is larger
// than ProcessInfo, which stays untouched for the Overview screen.
type ProcessRow struct {
	PID            int           `json:"pid"`
	PPID           int           `json:"ppid"`
	Name           string        `json:"name"`
	Cmd            string        `json:"cmd"`
	User           string        `json:"user"`
	UID            int           `json:"uid"`
	State          string        `json:"state"`
	Threads        int           `json:"threads"`
	RSSBytes       uint64        `json:"rss_bytes"`
	VSZBytes       uint64        `json:"vsz_bytes"`
	SwapBytes      uint64        `json:"swap_bytes"`
	CPUPercent     float64       `json:"cpu_percent"`
	CPUTimeSeconds float64       `json:"cpu_time_seconds"`
	StartTimeUnix  int64         `json:"start_time_unix"`
	UptimeSeconds  int64         `json:"uptime_seconds"`
	IOReadBps      float64       `json:"io_read_bps"`
	IOWriteBps     float64       `json:"io_write_bps"`
	FDCount        int           `json:"fd_count"`
	OOMScore       int           `json:"oom_score"`
	Nice           int           `json:"nice"`
	Prio           int           `json:"prio"`
	Cgroup         string        `json:"cgroup"`
	Unit           string        `json:"unit"`
	Container      string        `json:"container"`
	Ports          []ProcessPort `json:"ports"`
	Kernel         bool          `json:"kernel"`
}

// ProcessStats is the machine-wide summary that travels beside the rows.
// IOAvailable is internal bookkeeping: the wire field is derived from it so
// the dashboard can render "—" instead of a zero that looks like a measurement.
type ProcessStats struct {
	Total           int     `json:"total"`
	Threads         int     `json:"threads"`
	Running         int     `json:"running"`
	Sleeping        int     `json:"sleeping"`
	Stopped         int     `json:"stopped"`
	Zombie          int     `json:"zombie"`
	Blocked         int     `json:"blocked"`
	Idle            int     `json:"idle"`
	Load1           float64 `json:"load1"`
	Load5           float64 `json:"load5"`
	Load15          float64 `json:"load15"`
	PSICPUSomeAvg10 float64 `json:"psi_cpu_some_avg10"`
	PSIMemSomeAvg10 float64 `json:"psi_mem_some_avg10"`
	PSIIOSomeAvg10  float64 `json:"psi_io_some_avg10"`
	OOMKills24h     int     `json:"oom_kills_24h"`
	IOAvailable     bool    `json:"-"`
}

// procStat is the subset of /proc/<pid>/stat the collector and the command
// guard both need.
type procStat struct {
	Name       string
	State      byte
	PPID       int
	Utime      uint64
	Stime      uint64
	Priority   int
	Nice       int
	Threads    int
	StartTicks uint64
	VSize      uint64
	RSSPages   int64
}

func parseProcStat(data string) (procStat, error) {
	open := strings.IndexByte(data, '(')
	close := strings.LastIndexByte(data, ')')
	if open < 0 || close < open {
		return procStat{}, fmt.Errorf("malformed stat: missing comm")
	}
	out := procStat{Name: data[open+1 : close]}
	rest := strings.Fields(strings.TrimSpace(data[close+1:]))
	// After the comm, field 1 is state, 2 ppid, 14 utime, 15 stime,
	// 16 priority, 17 nice, 20 num_threads, 22 starttime, 23 vsize,
	// 24 rss. rest is zero-based.
	if len(rest) < 22 {
		return procStat{}, fmt.Errorf("malformed stat: %d fields", len(rest))
	}
	if len(rest[0]) == 0 {
		return procStat{}, fmt.Errorf("malformed stat: empty state")
	}
	out.State = rest[0][0]
	out.PPID, _ = strconv.Atoi(rest[1])
	out.Utime, _ = strconv.ParseUint(rest[11], 10, 64)
	out.Stime, _ = strconv.ParseUint(rest[12], 10, 64)
	out.Priority, _ = strconv.Atoi(rest[15])
	out.Nice, _ = strconv.Atoi(rest[16])
	out.Threads, _ = strconv.Atoi(rest[17])
	out.StartTicks, _ = strconv.ParseUint(rest[19], 10, 64)
	out.VSize, _ = strconv.ParseUint(rest[20], 10, 64)
	out.RSSPages, _ = strconv.ParseInt(rest[21], 10, 64)
	return out, nil
}

func readProcStat(procRoot string, pid int) (procStat, error) {
	data, err := os.ReadFile(filepath.Join(procRoot, strconv.Itoa(pid), "stat"))
	if err != nil {
		return procStat{}, err
	}
	return parseProcStat(string(data))
}

// readBootTime parses btime from /proc/stat.
func readBootTime(procRoot string) int64 {
	data, err := os.ReadFile(filepath.Join(procRoot, "stat"))
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "btime ") {
			if v, err := strconv.ParseInt(strings.TrimSpace(strings.TrimPrefix(line, "btime ")), 10, 64); err == nil {
				return v
			}
		}
	}
	return 0
}

// ProcessStartTimeUnix converts a stat starttime tick count into a Unix
// timestamp using the boot time. It is the value the dashboard shows and the
// guard compares to defeat PID reuse.
func ProcessStartTimeUnix(bootTime int64, startTicks uint64) int64 {
	if bootTime <= 0 {
		return 0
	}
	return bootTime + int64(startTicks/uint64(clockTicks))
}

// parsePasswdFile resolves UIDs from an /etc/passwd body. It is deliberately
// tolerant: a malformed line is skipped, not fatal.
func parsePasswdFile(data string) map[int]string {
	users := make(map[int]string)
	for _, line := range strings.Split(data, "\n") {
		fields := strings.Split(line, ":")
		if len(fields) < 3 || fields[0] == "" {
			continue
		}
		uid, err := strconv.Atoi(fields[2])
		if err != nil {
			continue
		}
		if _, exists := users[uid]; !exists {
			users[uid] = fields[0]
		}
	}
	return users
}

func readPasswd(procRoot string) map[int]string {
	// A fixture tree may carry its own passwd file next to the fake /proc, which
	// keeps the collector hermetic in tests without adding a parameter.
	if data, err := os.ReadFile(filepath.Join(procRoot, "passwd")); err == nil {
		return parsePasswdFile(string(data))
	}
	if data, err := os.ReadFile("/etc/passwd"); err == nil {
		return parsePasswdFile(string(data))
	}
	return map[int]string{}
}

// truncateName caps comm at 32 runes. The kernel itself caps it at 16 bytes,
// but a fixture or an unusual kernel can still exceed the wire contract.
func truncateName(name string) string {
	name = strings.TrimSpace(name)
	runes := []rune(name)
	if len(runes) > 32 {
		return string(runes[:32])
	}
	return name
}

// truncateCmd turns NUL-separated argv into a readable command and caps it at
// 256 runes with an ellipsis, exactly like the wire spec says.
func truncateCmd(raw string) string {
	raw = strings.ReplaceAll(raw, "\x00", " ")
	raw = strings.TrimSpace(raw)
	runes := []rune(raw)
	if len(runes) > 256 {
		return string(runes[:256]) + "…"
	}
	return raw
}

func truncateCgroup(cgroup string) string {
	if len(cgroup) > 200 {
		return cgroup[:200]
	}
	return cgroup
}

func isHexID(s string) bool {
	if len(s) < 12 {
		return false
	}
	for _, r := range s[:12] {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F') {
			return false
		}
	}
	return true
}

// parseCgroup derives the systemd unit and the Docker container identity from
// a cgroup path. The cgroup carries a container ID, never its human name; the
// dashboard resolves the name from the Docker state it already has.
func parseCgroup(raw string) (unit, container string) {
	line := raw
	if idx := strings.IndexByte(line, '\n'); idx >= 0 {
		line = line[:idx]
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return "", ""
	}
	// cgroup v1 prefixes the path with "N:controller:". cgroup v2 prefixes it
	// with "0::". Either way the interesting part is after the last ':'.
	if strings.Count(line, ":") >= 2 {
		if idx := strings.LastIndex(line, ":"); idx >= 0 {
			line = line[idx+1:]
		}
	}
	for _, segment := range strings.Split(line, "/") {
		if segment == "" {
			continue
		}
		if strings.HasPrefix(segment, "docker-") && strings.HasSuffix(segment, ".scope") {
			id := strings.TrimSuffix(strings.TrimPrefix(segment, "docker-"), ".scope")
			if isHexID(id) {
				return "", id[:12]
			}
		}
		if strings.HasPrefix(segment, "cri-containerd-") && strings.HasSuffix(segment, ".scope") {
			id := strings.TrimSuffix(strings.TrimPrefix(segment, "cri-containerd-"), ".scope")
			if isHexID(id) {
				return "", id[:12]
			}
		}
		if strings.HasPrefix(segment, "session-") && strings.HasSuffix(segment, ".scope") {
			continue
		}
		if strings.HasSuffix(segment, ".service") || strings.HasSuffix(segment, ".scope") {
			return segment, ""
		}
		// cgroup v1's /docker/<id> and bare container-id paths.
		if isHexID(segment) && len(segment) >= 12 {
			return "", segment[:12]
		}
	}
	return "", ""
}

// netTable maps a socket inode to the ports listening on it.
func netTable(procRoot string) map[int][]ProcessPort {
	ports := make(map[int][]ProcessPort)
	files := []struct{ name, proto string }{
		{"tcp", "tcp"}, {"tcp6", "tcp6"}, {"udp", "udp"}, {"udp6", "udp6"},
	}
	for _, f := range files {
		data, err := os.ReadFile(filepath.Join(procRoot, "net", f.name))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 10 || fields[0] == "sl" {
				continue
			}
			local := fields[1]
			colon := strings.LastIndexByte(local, ':')
			if colon < 0 {
				continue
			}
			addrHex, portHex := local[:colon], local[colon+1:]
			port64, err := strconv.ParseUint(portHex, 16, 16)
			if err != nil {
				continue
			}
			inode, err := strconv.Atoi(fields[9])
			if err != nil || inode == 0 {
				continue
			}
			ports[inode] = append(ports[inode], ProcessPort{
				Proto: f.proto,
				Port:  int(port64),
				Addr:  decodeProcAddr(addrHex),
			})
		}
	}
	return ports
}

// decodeProcAddr decodes the little-endian hex address /proc/net uses.
func decodeProcAddr(hexAddr string) string {
	raw, err := hex.DecodeString(hexAddr)
	if err != nil {
		return ""
	}
	if len(raw) == 4 {
		return fmt.Sprintf("%d.%d.%d.%d", raw[3], raw[2], raw[1], raw[0])
	}
	if len(raw) == 16 {
		// Each 32-bit word is stored little-endian.
		for word := 0; word < 4; word++ {
			base := word * 4
			raw[base], raw[base+3] = raw[base+3], raw[base]
			raw[base+1], raw[base+2] = raw[base+2], raw[base+1]
		}
		parts := make([]string, 8)
		for i := 0; i < 8; i++ {
			parts[i] = fmt.Sprintf("%02x", raw[i*2:i*2+2])
		}
		return strings.Join(parts, ":")
	}
	return ""
}

func processPorts(procRoot string, pid int, table map[int][]ProcessPort) []ProcessPort {
	if len(table) == 0 {
		return nil
	}
	fdDir := filepath.Join(procRoot, strconv.Itoa(pid), "fd")
	entries, err := os.ReadDir(fdDir)
	if err != nil {
		return nil
	}
	var out []ProcessPort
	seen := make(map[string]bool)
	for _, entry := range entries {
		target, err := os.Readlink(filepath.Join(fdDir, entry.Name()))
		if err != nil {
			continue
		}
		if !strings.HasPrefix(target, "socket:[") || !strings.HasSuffix(target, "]") {
			continue
		}
		inode, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(target, "socket:["), "]"))
		if err != nil {
			continue
		}
		for _, port := range table[inode] {
			key := fmt.Sprintf("%s/%s/%d", port.Proto, port.Addr, port.Port)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, port)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Port != out[j].Port {
			return out[i].Port < out[j].Port
		}
		return out[i].Proto < out[j].Proto
	})
	return out
}

func readLoadAverage(procRoot string) (l1, l5, l15 float64) {
	data, err := os.ReadFile(filepath.Join(procRoot, "loadavg"))
	if err != nil {
		return 0, 0, 0
	}
	fields := strings.Fields(string(data))
	if len(fields) < 3 {
		return 0, 0, 0
	}
	l1, _ = strconv.ParseFloat(fields[0], 64)
	l5, _ = strconv.ParseFloat(fields[1], 64)
	l15, _ = strconv.ParseFloat(fields[2], 64)
	return
}

func readPSI(procRoot, name string) float64 {
	data, err := os.ReadFile(filepath.Join(procRoot, "pressure", name))
	if err != nil {
		return 0
	}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "some ") {
			continue
		}
		for _, field := range strings.Fields(line) {
			if strings.HasPrefix(field, "avg10=") {
				v, _ := strconv.ParseFloat(strings.TrimPrefix(field, "avg10="), 64)
				return v
			}
		}
	}
	return 0
}

func readMemValue(status string, key string) uint64 {
	for _, line := range strings.Split(status, "\n") {
		if !strings.HasPrefix(line, key+":") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			kb, _ := strconv.ParseUint(fields[1], 10, 64)
			return kb * 1024
		}
	}
	return 0
}

func readStatusUID(status string) int {
	for _, line := range strings.Split(status, "\n") {
		if !strings.HasPrefix(line, "Uid:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			uid, _ := strconv.Atoi(fields[1])
			return uid
		}
	}
	return 0
}

func readProcIO(procRoot string, pid int) (read, write uint64, ok bool) {
	data, err := os.ReadFile(filepath.Join(procRoot, strconv.Itoa(pid), "io"))
	if err != nil {
		return 0, 0, false
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "read_bytes:":
			read, _ = strconv.ParseUint(fields[1], 10, 64)
		case "write_bytes:":
			write, _ = strconv.ParseUint(fields[1], 10, 64)
		}
	}
	return read, write, true
}

func readOOMScore(procRoot string, pid int) int {
	data, err := os.ReadFile(filepath.Join(procRoot, strconv.Itoa(pid), "oom_score"))
	if err != nil {
		return -1
	}
	v, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return -1
	}
	return v
}

func countFDs(procRoot string, pid int) int {
	entries, err := os.ReadDir(filepath.Join(procRoot, strconv.Itoa(pid), "fd"))
	if err != nil {
		return 0
	}
	return len(entries)
}

// CollectProcesses reads a /proc tree and returns the process rows, the
// machine summary and the counter sample to feed into the next cycle.
//
// procRoot is a parameter so the parsing can be exercised against a fixture;
// production passes "/proc". CPU percentages keep the established semantics:
// jiffies consumed in the window, normalised per logical core, so 100 means
// the whole machine was saturated.
func CollectProcesses(procRoot string, limit, cpuCores int, prev ProcessSampleMap, elapsed time.Duration, now time.Time) ([]ProcessRow, ProcessStats, ProcessSampleMap, error) {
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return nil, ProcessStats{}, prev, fmt.Errorf("read %s: %w", procRoot, err)
	}
	if cpuCores < 1 {
		cpuCores = 1
	}
	seconds := elapsed.Seconds()
	boot := readBootTime(procRoot)
	users := readPasswd(procRoot)

	var stats ProcessStats
	stats.OOMKills24h = -1
	stats.Load1, stats.Load5, stats.Load15 = readLoadAverage(procRoot)
	stats.PSICPUSomeAvg10 = readPSI(procRoot, "cpu")
	stats.PSIMemSomeAvg10 = readPSI(procRoot, "memory")
	stats.PSIIOSomeAvg10 = readPSI(procRoot, "io")

	next := make(ProcessSampleMap, len(entries))

	type cand struct {
		pid        int
		name       string
		stat       procStat
		cpuPercent float64
		cpuTime    uint64
	}
	cands := make([]cand, 0, len(entries))
	nowUnix := now.Unix()

	// Bulk pass: one /proc/<pid>/stat read per process. Only the state
	// counts and the CPU delta are computed here; the expensive per-process
	// reads (status, cmdline, cgroup, fd, io, oom_score) are deferred to the
	// survivors below.
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		stat, err := readProcStat(procRoot, pid)
		if err != nil {
			continue
		}
		commData, _ := os.ReadFile(filepath.Join(procRoot, entry.Name(), "comm"))
		name := truncateName(string(commData))
		if name == "" {
			name = stat.Name
		}

		cpuTime := stat.Utime + stat.Stime
		var cpuPercent float64
		if seconds > 0 {
			if before, ok := prev[pid]; ok && cpuTime > before.CPUJiffies {
				cpuPercent = float64(cpuTime-before.CPUJiffies) / clockTicks / seconds / float64(cpuCores) * 100.0
				if cpuPercent < 0 {
					cpuPercent = 0
				}
				if cpuPercent > 100 {
					cpuPercent = 100
				}
			}
		}

		// Read the IO counters for every process here, not just the selected
		// rows, so a process that was not selected in the previous cycle does
		// not present a zero baseline and turn its lifetime counter into a
		// spike the first time it is selected.
		readBytes, writeBytes, ioOK := readProcIO(procRoot, pid)
		if ioOK {
			stats.IOAvailable = true
		}
		next[pid] = ProcessSample{CPUJiffies: cpuTime, ReadBytes: readBytes, WriteBytes: writeBytes}

		stats.Total++
		stats.Threads += stat.Threads
		switch stat.State {
		case 'R':
			stats.Running++
		case 'S':
			stats.Sleeping++
		case 'D':
			stats.Blocked++
		case 'Z':
			stats.Zombie++
		case 'T', 't':
			stats.Stopped++
		case 'I':
			stats.Idle++
		}

		cands = append(cands, cand{pid: pid, name: name, stat: stat, cpuPercent: cpuPercent, cpuTime: cpuTime})
	}

	sort.Slice(cands, func(i, j int) bool {
		if cands[i].cpuPercent != cands[j].cpuPercent {
			return cands[i].cpuPercent > cands[j].cpuPercent
		}
		return cands[i].stat.RSSPages > cands[j].stat.RSSPages
	})
	if limit <= 0 || limit > len(cands) {
		limit = len(cands)
	}
	selected := cands[:limit]

	// Enrich only the survivors.
	table := netTable(procRoot)
	rows := make([]ProcessRow, 0, len(selected))
	for _, c := range selected {
		pid := c.pid
		stat := c.stat

		statusData, _ := os.ReadFile(filepath.Join(procRoot, strconv.Itoa(pid), "status"))
		status := string(statusData)
		uid := readStatusUID(status)
		user, ok := users[uid]
		if !ok {
			user = "uid:" + strconv.Itoa(uid)
		}

		readBytes, writeBytes, ioOK := readProcIO(procRoot, pid)
		if ioOK {
			stats.IOAvailable = true
		}
		var readBps, writeBps float64
		if seconds > 0 && ioOK {
			if before, ok := prev[pid]; ok {
				if readBytes > before.ReadBytes {
					readBps = float64(readBytes-before.ReadBytes) / seconds
				}
				if writeBytes > before.WriteBytes {
					writeBps = float64(writeBytes-before.WriteBytes) / seconds
				}
			}
		}
		next[pid] = ProcessSample{CPUJiffies: c.cpuTime, ReadBytes: readBytes, WriteBytes: writeBytes}

		cmdData, _ := os.ReadFile(filepath.Join(procRoot, strconv.Itoa(pid), "cmdline"))
		cmd := truncateCmd(string(cmdData))
		cgroupData, _ := os.ReadFile(filepath.Join(procRoot, strconv.Itoa(pid), "cgroup"))
		cgroup := truncateCgroup(strings.TrimSpace(string(cgroupData)))
		unit, container := parseCgroup(cgroup)

		kernel := len(strings.TrimSpace(string(cmdData))) == 0 && stat.PPID == 2
		cpuSeconds := float64(c.cpuTime) / clockTicks
		startUnix := ProcessStartTimeUnix(boot, stat.StartTicks)
		var uptime int64
		if startUnix > 0 && nowUnix > startUnix {
			uptime = nowUnix - startUnix
		}

		row := ProcessRow{
			PID:            pid,
			PPID:           stat.PPID,
			Name:           c.name,
			Cmd:            cmd,
			User:           user,
			UID:            uid,
			State:          string(stat.State),
			Threads:        stat.Threads,
			RSSBytes:       readMemValue(status, "VmRSS"),
			VSZBytes:       stat.VSize,
			SwapBytes:      readMemValue(status, "VmSwap"),
			CPUPercent:     c.cpuPercent,
			CPUTimeSeconds: cpuSeconds,
			StartTimeUnix:  startUnix,
			UptimeSeconds:  uptime,
			IOReadBps:      readBps,
			IOWriteBps:     writeBps,
			FDCount:        countFDs(procRoot, pid),
			OOMScore:       readOOMScore(procRoot, pid),
			Nice:           stat.Nice,
			Prio:           stat.Priority,
			Cgroup:         cgroup,
			Unit:           unit,
			Container:      container,
			Ports:          processPorts(procRoot, pid, table),
			Kernel:         kernel,
		}
		// ensure RSS from status if stat rss 0? keep stat
		rows = append(rows, row)
	}

	return rows, stats, next, nil
}

// ProcessHistory mirrors the wire shape for the slow-cycle process history.
type ProcessHistory struct {
	IntervalSeconds int                    `json:"interval_seconds"`
	Samples         []ProcessHistorySample `json:"samples"`
	Events          []ProcessHistoryEvent  `json:"events"`
}

type ProcessHistorySample struct {
	T    int64                 `json:"t"`
	Rows []ProcessHistoryPoint `json:"rows"`
}

type ProcessHistoryPoint struct {
	PID        int     `json:"pid"`
	Name       string  `json:"name"`
	CPU        float64 `json:"cpu"`
	RSS        uint64  `json:"rss"`
	IOReadBps  float64 `json:"io_read_bps"`
	IOWriteBps float64 `json:"io_write_bps"`
}

type ProcessHistoryEvent struct {
	T        int64  `json:"t"`
	Type     string `json:"type"`
	PID      int    `json:"pid"`
	Name     string `json:"name"`
	ExitCode *int   `json:"exit_code,omitempty"`
}

// ReadProcStat is the exported form of readProcStat, used by the agent command
// guard for renice/ionice fresh checks.
func ReadProcStat(procRoot string, pid int) (ProcStat, error) {
	return readProcStat(procRoot, pid)
}

// ProcStat is the exported alias of procStat.
type ProcStat = procStat
