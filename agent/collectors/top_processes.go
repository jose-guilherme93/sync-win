package collectors

import (
	"fmt"
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

type procStat struct {
	pid        int
	name       string
	state      string
	ppid       int
	cpuTime    uint64
	threads    int
	startTicks uint64
	virt       uint64
	rss        uint64
}

// parseProcStat reads the fields the Process tab needs from one /proc/<pid>/stat
// line. The comm field is wrapped in parentheses and may itself contain spaces
// or parentheses, so the name is taken between the first "(" and the last ")".
func parseProcStat(pid int, line string) (procStat, bool) {
	open := strings.IndexByte(line, '(')
	close := strings.LastIndexByte(line, ')')
	if open < 0 || close <= open {
		return procStat{}, false
	}
	name := line[open+1 : close]
	fields := strings.Fields(line[close+2:])
	// state is field 3 in proc(5); after the name the array starts there.
	if len(fields) < 22 {
		return procStat{}, false
	}
	ppid, _ := strconv.Atoi(fields[1])
	utime, _ := strconv.ParseUint(fields[11], 10, 64)
	stime, _ := strconv.ParseUint(fields[12], 10, 64)
	threads, _ := strconv.Atoi(fields[17])
	startTicks, _ := strconv.ParseUint(fields[19], 10, 64)
	virt, _ := strconv.ParseUint(fields[20], 10, 64)
	rssPages, _ := strconv.ParseInt(fields[21], 10, 64)
	if rssPages < 0 {
		rssPages = 0
	}
	return procStat{
		pid:        pid,
		name:       name,
		state:      fields[0],
		ppid:       ppid,
		cpuTime:    utime + stime,
		threads:    threads,
		startTicks: startTicks,
		virt:       virt,
		rss:        uint64(rssPages) * uint64(os.Getpagesize()),
	}, true
}

// CollectProcesses takes one snapshot of /proc. Every process costs one stat
// read; only the rows that will be reported (top CPU, top memory, stopped,
// zombie) pay for status and cmdline reads. That makes the cycle cheaper than
// the previous comm+status+stat pass while carrying far more information.
//
// CPUPercent keeps the machine-share meaning used by the rest of the payload:
// a process saturating one core on a four-core machine reports 25. Threads are
// reported separately so a multi-threaded process is not mistaken for a small
// one.
func CollectProcesses(topLimit, problemLimit, cmdlineChars, cpuCores int, prev ProcessCPUSample, elapsed time.Duration) (ProcessSnapshot, ProcessCPUSample) {
	var snap ProcessSnapshot
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return snap, prev
	}

	if topLimit <= 0 {
		topLimit = 20
	}
	if problemLimit <= 0 {
		problemLimit = 10
	}
	if cmdlineChars <= 0 {
		cmdlineChars = 200
	}
	if cpuCores < 1 {
		cpuCores = 1
	}
	seconds := elapsed.Seconds()

	next := make(ProcessCPUSample, len(entries))
	rows := make([]procStat, 0, len(entries))

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		data, err := os.ReadFile("/proc/" + entry.Name() + "/stat")
		if err != nil {
			continue // the process exited between readdir and read
		}
		stat, ok := parseProcStat(pid, string(data))
		if !ok {
			continue
		}
		rows = append(rows, stat)
		next[pid] = stat.cpuTime

		switch stat.state {
		case "R":
			snap.Summary.Running++
		case "S":
			snap.Summary.Sleeping++
		case "D":
			snap.Summary.DiskSleep++
		case "T", "t":
			snap.Summary.Stopped++
		case "Z":
			snap.Summary.Zombie++
		default:
			snap.Summary.Other++
		}
		snap.Summary.Threads += stat.threads
	}
	snap.Summary.Total = len(rows)
	if len(rows) == 0 {
		return snap, next
	}

	uptimeSeconds := readUptimeSeconds()
	infos := make([]ProcessInfo, len(rows))
	for i, row := range rows {
		var cpuPercent float64
		if seconds > 0 {
			if before, ok := prev[row.pid]; ok && row.cpuTime > before {
				jiffies := float64(row.cpuTime - before)
				cpuPercent = jiffies / clockTicks / seconds / float64(cpuCores) * 100.0
				if cpuPercent < 0 {
					cpuPercent = 0
				}
				if cpuPercent > 100 {
					cpuPercent = 100
				}
			}
		}
		var age int64
		if uptimeSeconds > 0 {
			if value := uptimeSeconds - float64(row.startTicks)/clockTicks; value > 0 {
				age = int64(value)
			}
		}
		infos[i] = ProcessInfo{
			PID:            row.pid,
			PPID:           row.ppid,
			Name:           row.name,
			State:          row.state,
			CPUPercent:     cpuPercent,
			MemRSS:         row.rss,
			MemVirt:        row.virt,
			Threads:        row.threads,
			ElapsedSeconds: age,
		}
	}

	byCPU := append([]ProcessInfo(nil), infos...)
	sort.Slice(byCPU, func(i, j int) bool {
		if byCPU[i].CPUPercent != byCPU[j].CPUPercent {
			return byCPU[i].CPUPercent > byCPU[j].CPUPercent
		}
		return byCPU[i].MemRSS > byCPU[j].MemRSS
	})
	byCPU = head(byCPU, topLimit)

	byMem := append([]ProcessInfo(nil), infos...)
	sort.Slice(byMem, func(i, j int) bool {
		if byMem[i].MemRSS != byMem[j].MemRSS {
			return byMem[i].MemRSS > byMem[j].MemRSS
		}
		return byMem[i].CPUPercent > byMem[j].CPUPercent
	})
	byMem = head(byMem, topLimit)

	stopped := make([]ProcessInfo, 0, problemLimit)
	zombies := make([]ProcessInfo, 0, problemLimit)
	for _, proc := range infos {
		switch proc.State {
		case "T", "t":
			stopped = append(stopped, proc)
		case "Z":
			zombies = append(zombies, proc)
		}
	}
	sortOldestFirst(stopped)
	sortOldestFirst(zombies)
	stopped = head(stopped, problemLimit)
	zombies = head(zombies, problemLimit)

	// Enrich only the rows that will be sent: the user is not in stat and the
	// command line is a separate file per process.
	selected := make(map[int]struct{}, len(byCPU)+len(byMem)+len(stopped)+len(zombies))
	for _, list := range [][]ProcessInfo{byCPU, byMem, stopped, zombies} {
		for _, proc := range list {
			selected[proc.PID] = struct{}{}
		}
	}
	users := loadPasswdUsers()
	details := make(map[int]ProcessInfo, len(selected))
	for pid := range selected {
		user, command := readProcessDetails(pid, cmdlineChars, users, infos)
		details[pid] = ProcessInfo{User: user, Command: command}
	}
	applyDetails := func(list []ProcessInfo) {
		for i := range list {
			if detail, ok := details[list[i].PID]; ok {
				list[i].User = detail.User
				list[i].Command = detail.Command
			}
		}
	}
	applyDetails(byCPU)
	applyDetails(byMem)
	applyDetails(stopped)
	applyDetails(zombies)

	snap.ByCPU = byCPU
	snap.ByMem = byMem
	snap.Stopped = stopped
	snap.Zombies = zombies
	return snap, next
}

func head(list []ProcessInfo, limit int) []ProcessInfo {
	if limit > len(list) {
		limit = len(list)
	}
	return list[:limit]
}

// sortOldestFirst puts the process that has lived longest first, which is the
// useful order for stopped and zombie rows: the older it is, the longer it has
// been ignored.
func sortOldestFirst(list []ProcessInfo) {
	sort.Slice(list, func(i, j int) bool {
		if list[i].ElapsedSeconds != list[j].ElapsedSeconds {
			return list[i].ElapsedSeconds > list[j].ElapsedSeconds
		}
		return list[i].PID < list[j].PID
	})
}

func readUptimeSeconds() float64 {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0
	}
	value, _ := strconv.ParseFloat(fields[0], 64)
	return value
}

// loadPasswdUsers maps uid -> account name. /etc/passwd is a small file and one
// read per cycle is cheaper than shelling out to id for every reported row.
func loadPasswdUsers() map[string]string {
	data, err := os.ReadFile("/etc/passwd")
	if err != nil {
		return nil
	}
	users := make(map[string]string, 32)
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.SplitN(line, ":", 4)
		if len(fields) < 3 || fields[0] == "" {
			continue
		}
		users[fields[2]] = fields[0]
	}
	return users
}

func readProcessDetails(pid, cmdlineChars int, users map[string]string, infos []ProcessInfo) (string, string) {
	user := ""
	if data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid)); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "Uid:") {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					if name, ok := users[fields[1]]; ok {
						user = name
					} else {
						user = fields[1]
					}
				}
				break
			}
		}
	}

	command := ""
	if data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid)); err == nil {
		args := make([]string, 0, 8)
		for _, part := range strings.Split(string(data), "\x00") {
			if part != "" {
				args = append(args, part)
			}
		}
		command = strings.TrimSpace(strings.Join(args, " "))
	}
	if command == "" {
		for _, proc := range infos {
			if proc.PID == pid {
				command = "[" + proc.Name + "]"
				break
			}
		}
	}
	return user, truncateRunes(command, cmdlineChars)
}

func truncateRunes(value string, limit int) string {
	if limit <= 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}
