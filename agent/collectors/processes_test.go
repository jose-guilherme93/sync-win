package collectors

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// buildProcFixture writes a tiny /proc tree that the collector can read
// without depending on the host's processes. Every value is chosen so the
// expected arithmetic is obvious.
func buildProcFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mkdir := func(rel string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(root, rel), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	write("stat", "cpu  1 2 3 4\nbtime 1000\n")
	write("loadavg", "0.50 0.40 0.30 1/100 1234\n")
	write("pressure/cpu", "some avg10=1.50 avg60=1.00 avg300=0.50 total=1234\n")
	write("pressure/memory", "some avg10=2.50 avg60=2.00 avg300=1.50 total=1234\n")
	write("pressure/io", "some avg10=0.25 avg60=0.20 avg300=0.10 total=1234\n")
	write("vmstat", "nr_free_pages 1\noom_kill 3\n")

	// firefox: normal process, docker cgroup, readable io/fd/oom, one socket.
	write("1000/comm", "firefox\n")
	write("1000/cmdline", "/usr/lib/firefox\x00-contentproc\x00")
	write("1000/status", "Name:\tfirefox\nUid:\t1000\t1000\t1000\t1000\nVmRSS:\t2048 kB\nVmSwap:\t4 kB\nThreads:\t5\n")
	write("1000/stat", "1000 (firefox) S 900 1000 1000 0 -1 4194560 100 0 0 0 250 50 0 0 20 0 5 0 500 123456789 123 18446744073709551615\n")
	write("1000/io", "rchar: 100\nwchar: 200\nread_bytes: 4096\nwrite_bytes: 8192\n")
	write("1000/oom_score", "42\n")
	write("1000/cgroup", "0::/system.slice/docker-abcdef1234567890.scope\n")
	mkdir("1000/fd")
	if err := os.Symlink("socket:[12345]", filepath.Join(root, "1000", "fd", "3")); err != nil {
		t.Fatal(err)
	}

	// systemd: protected by name, no cmdline.
	write("1/comm", "systemd\n")
	write("1/status", "Name:\tsystemd\nUid:\t0\t0\t0\t0\nVmRSS:\t1024 kB\n")
	write("1/stat", "1 (systemd) S 0 1 1 0 -1 4194560 1 0 0 0 10 5 0 0 20 0 1 0 100 1000 10 18446744073709551615\n")
	write("1/cgroup", "0::/init.scope\n")

	// kworker: kernel thread (no cmdline, ppid 2).
	write("50/comm", "[kworker/0:0]\n")
	write("50/status", "Name:\t[kworker/0:0]\nUid:\t0\t0\t0\t0\nVmRSS:\t0 kB\n")
	write("50/stat", "50 ([kworker/0:0]) I 2 50 50 0 -1 69238880 1 0 0 0 1 0 0 0 20 0 1 0 200 0 0 18446744073709551615\n")
	write("50/cgroup", "0::/system.slice/ssh.service\n")

	// socket table: local 0100007F:1F90 (127.0.0.1:8080), inode 12345.
	write("net/tcp", "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n"+
		"   0: 0100007F:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 12345 1 0000000000000000 100 0 0 10 0\n")
	write("net/tcp6", "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n")
	write("net/udp", "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n")
	write("net/udp6", "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n")

	return root
}

// writeFileForTest is the small writer the guard tests reuse to extend the
// fixture after it was built.
func writeFileForTest(root, rel, content string) error {
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

func findRow(rows []ProcessRow, pid int) *ProcessRow {
	for i := range rows {
		if rows[i].PID == pid {
			return &rows[i]
		}
	}
	return nil
}

func TestCollectProcessesParsesProcFixture(t *testing.T) {
	root := buildProcFixture(t)
	now := time.Unix(2000, 0)

	rows, stats, next, err := CollectProcesses(root, 250, 1, nil, 0, now)
	if err != nil {
		t.Fatalf("CollectProcesses: %v", err)
	}

	firefox := findRow(rows, 1000)
	if firefox == nil {
		t.Fatal("firefox row missing")
	}
	if firefox.Name != "firefox" {
		t.Errorf("name = %q, want firefox", firefox.Name)
	}
	if firefox.PPID != 900 {
		t.Errorf("ppid = %d, want 900", firefox.PPID)
	}
	if firefox.Cmd != "/usr/lib/firefox -contentproc" {
		t.Errorf("cmd = %q, want NULs converted to spaces", firefox.Cmd)
	}
	if firefox.State != "S" {
		t.Errorf("state = %q, want S", firefox.State)
	}
	if firefox.Threads != 5 {
		t.Errorf("threads = %d, want 5", firefox.Threads)
	}
	if firefox.RSSBytes != 2048*1024 {
		t.Errorf("rss = %d, want %d", firefox.RSSBytes, 2048*1024)
	}
	if firefox.SwapBytes != 4*1024 {
		t.Errorf("swap = %d, want %d", firefox.SwapBytes, 4*1024)
	}
	if firefox.VSZBytes != 123456789 {
		t.Errorf("vsz = %d, want 123456789", firefox.VSZBytes)
	}
	if firefox.CPUTimeSeconds != 3.0 {
		t.Errorf("cpu_time_seconds = %v, want 3.0", firefox.CPUTimeSeconds)
	}
	if firefox.StartTimeUnix != 1005 {
		t.Errorf("start_time_unix = %d, want 1005", firefox.StartTimeUnix)
	}
	if firefox.Nice != 0 || firefox.Prio != 20 {
		t.Errorf("nice/prio = %d/%d, want 0/20", firefox.Nice, firefox.Prio)
	}
	if firefox.FDCount != 1 {
		t.Errorf("fd_count = %d, want 1", firefox.FDCount)
	}
	if firefox.OOMScore != 42 {
		t.Errorf("oom_score = %d, want 42", firefox.OOMScore)
	}
	if firefox.Container != "abcdef123456" {
		t.Errorf("container = %q, want the cgroup id prefix", firefox.Container)
	}
	if firefox.User == "" || firefox.User == "uid:" {
		t.Errorf("user = %q, want a resolved name or uid:<n> fallback", firefox.User)
	}
	if len(firefox.Ports) != 1 || firefox.Ports[0].Port != 8080 || firefox.Ports[0].Proto != "tcp" {
		t.Errorf("ports = %+v, want tcp/8080", firefox.Ports)
	}
	if firefox.Ports[0].Addr != "127.0.0.1" {
		t.Errorf("port addr = %q, want 127.0.0.1", firefox.Ports[0].Addr)
	}
	if firefox.Kernel {
		t.Error("firefox must not be flagged as a kernel thread")
	}

	systemd := findRow(rows, 1)
	if systemd == nil {
		t.Fatal("systemd row missing")
	}
	if systemd.User != "root" && systemd.UID != 0 {
		t.Errorf("systemd uid/user = %d/%q, want uid 0", systemd.UID, systemd.User)
	}

	kworker := findRow(rows, 50)
	if kworker == nil {
		t.Fatal("kworker row missing")
	}
	if !kworker.Kernel {
		t.Error("kworker must be flagged as a kernel thread")
	}
	if kworker.Unit != "ssh.service" {
		t.Errorf("unit = %q, want ssh.service", kworker.Unit)
	}

	if stats.Total != 3 {
		t.Errorf("stats.total = %d, want 3", stats.Total)
	}
	if stats.Running != 0 || stats.Sleeping != 2 || stats.Idle != 1 {
		t.Errorf("state counts = run %d sleep %d idle %d, want 0/2/1", stats.Running, stats.Sleeping, stats.Idle)
	}
	if stats.Load1 != 0.50 || stats.Load5 != 0.40 || stats.Load15 != 0.30 {
		t.Errorf("load = %v/%v/%v", stats.Load1, stats.Load5, stats.Load15)
	}
	if stats.PSICPUSomeAvg10 != 1.50 || stats.PSIMemSomeAvg10 != 2.50 || stats.PSIIOSomeAvg10 != 0.25 {
		t.Errorf("psi = %v/%v/%v", stats.PSICPUSomeAvg10, stats.PSIMemSomeAvg10, stats.PSIIOSomeAvg10)
	}
	if !stats.IOAvailable {
		t.Error("io_available must be true when at least one /proc/<pid>/io was readable")
	}

	// The sample feeds the next cycle and carries the cumulative counters.
	if next[1000].CPUJiffies != 300 {
		t.Errorf("next cpu jiffies = %d, want 300", next[1000].CPUJiffies)
	}
	if next[1000].ReadBytes != 4096 || next[1000].WriteBytes != 8192 {
		t.Errorf("next io = %d/%d, want 4096/8192", next[1000].ReadBytes, next[1000].WriteBytes)
	}
}

func TestCollectProcessesCPUDeltaMath(t *testing.T) {
	root := buildProcFixture(t)
	prev := ProcessSampleMap{
		1000: {CPUJiffies: 100, ReadBytes: 1024, WriteBytes: 2048},
	}

	rows, _, _, err := CollectProcesses(root, 250, 1, prev, 10*time.Second, time.Unix(2000, 0))
	if err != nil {
		t.Fatalf("CollectProcesses: %v", err)
	}
	firefox := findRow(rows, 1000)
	if firefox == nil {
		t.Fatal("firefox row missing")
	}
	// 200 jiffies / 100 Hz / 10 s / 1 core * 100 = 20%.
	if firefox.CPUPercent != 20 {
		t.Errorf("cpu_percent = %v, want 20", firefox.CPUPercent)
	}
	// One core's worth of jiffies is 100% regardless of how many cores exist.
	rows2, _, _, err := CollectProcesses(root, 250, 2, prev, 10*time.Second, time.Unix(2000, 0))
	if err != nil {
		t.Fatal(err)
	}
	if got := findRow(rows2, 1000).CPUPercent; got != 20 {
		t.Errorf("cpu_percent on 2 cores = %v, want 20 (per-core share)", got)
	}
	// IO rates are deltas too.
	if firefox.IOReadBps != 307.2 || firefox.IOWriteBps != 614.4 {
		t.Errorf("io rates = %v/%v, want 307.2/614.4", firefox.IOReadBps, firefox.IOWriteBps)
	}
}

func TestCollectProcessesGracefulAbsence(t *testing.T) {
	root := buildProcFixture(t)
	rows, stats, _, err := CollectProcesses(root, 250, 1, nil, 0, time.Unix(2000, 0))
	if err != nil {
		t.Fatal(err)
	}
	// systemd has no io, no fd dir and no oom_score.
	systemd := findRow(rows, 1)
	if systemd == nil {
		t.Fatal("systemd row missing")
	}
	if systemd.IOReadBps != 0 || systemd.IOWriteBps != 0 {
		t.Errorf("missing io must read 0, got %v/%v", systemd.IOReadBps, systemd.IOWriteBps)
	}
	if systemd.FDCount != 0 {
		t.Errorf("missing fd dir must read 0, got %d", systemd.FDCount)
	}
	if systemd.OOMScore != -1 {
		t.Errorf("missing oom_score must read -1, got %d", systemd.OOMScore)
	}
	if systemd.Container != "" {
		t.Errorf("init.scope must not map to a container, got %q", systemd.Container)
	}
	if stats.Total != 3 {
		t.Errorf("total = %d, want 3", stats.Total)
	}
}

func TestCollectProcessesSortsByCPUThenRSS(t *testing.T) {
	root := buildProcFixture(t)
	prev := ProcessSampleMap{
		1000: {CPUJiffies: 100},
		50:   {CPUJiffies: 0},
	}
	rows, _, _, err := CollectProcesses(root, 2, 1, prev, 10*time.Second, time.Unix(2000, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("limit = 2 produced %d rows", len(rows))
	}
	if rows[0].PID != 1000 {
		t.Errorf("first row = %d, want the busiest process 1000", rows[0].PID)
	}
}

func TestCollectProcessesIOFirstSeenNoSpike(t *testing.T) {
	root := buildProcFixture(t)
	// First sample: no previous sample for pid 1000 -> no IO rate.
	rows, _, next, err := CollectProcesses(root, 250, 1, nil, 10*time.Second, time.Unix(2000, 0))
	if err != nil {
		t.Fatal(err)
	}
	if r := findRow(rows, 1000); r == nil || r.IOReadBps != 0 || r.IOWriteBps != 0 {
		t.Fatalf("first-seen io rates = %v/%v, want 0/0", r.IOReadBps, r.IOWriteBps)
	}
	// Second sample using the returned sample: counters unchanged -> zero delta.
	rows2, _, _, err := CollectProcesses(root, 250, 1, next, 10*time.Second, time.Unix(2010, 0))
	if err != nil {
		t.Fatal(err)
	}
	if r := findRow(rows2, 1000); r == nil || r.IOReadBps != 0 || r.IOWriteBps != 0 {
		t.Fatalf("unchanged io rates = %v/%v, want 0/0", r.IOReadBps, r.IOWriteBps)
	}
}

func TestCollectProcessesMissingProc(t *testing.T) {
	_, _, _, err := CollectProcesses(filepath.Join(t.TempDir(), "absent"), 10, 1, nil, 0, time.Now())
	if err == nil {
		t.Fatal("a missing /proc root must return an error, not an empty success")
	}
}

func TestParsePasswdResolvesUID(t *testing.T) {
	users := parsePasswdFile("root:x:0:0:root:/root:/bin/bash\nalice:x:1000:1000:Alice:/home/alice:/bin/bash\n")
	if users[0] != "root" || users[1000] != "alice" {
		t.Fatalf("parsePasswdFile = %v", users)
	}
}

func TestParseCgroupMapsUnitAndContainer(t *testing.T) {
	cases := []struct {
		cgroup    string
		unit      string
		container string
	}{
		{"0::/system.slice/ssh.service", "ssh.service", ""},
		{"0::/system.slice/docker-0123456789abcdef.scope", "", "0123456789ab"},
		{"11:memory:/docker/0123456789abcdef", "", "0123456789ab"},
		{"0::/user.slice/user-1000.slice/session-2.scope", "", ""},
		{"", "", ""},
	}
	for _, tc := range cases {
		unit, container := parseCgroup(tc.cgroup)
		if unit != tc.unit || container != tc.container {
			t.Errorf("parseCgroup(%q) = %q/%q, want %q/%q", tc.cgroup, unit, container, tc.unit, tc.container)
		}
	}
}

func TestTruncateCmd(t *testing.T) {
	long := make([]byte, 400)
	for i := range long {
		long[i] = 'a'
	}
	got := truncateCmd(string(long))
	if len([]rune(got)) != 257 { // 256 runes plus the ellipsis
		t.Fatalf("truncated length = %d, want 257", len([]rune(got)))
	}
	runes := []rune(got)
	if string(runes[len(runes)-1:]) != "…" {
		t.Errorf("truncated command must end with an ellipsis, got %q", string(runes[len(runes)-1:]))
	}
}

// BenchmarkCollectProcesses measures the per-cycle cost of collecting over a
// synthetic /proc tree with ~400 processes, so the "single stat line first,
// enrich survivors only" claim is measured rather than asserted.
func BenchmarkCollectProcesses(b *testing.B) {
	root := b.TempDir()
	write := func(rel, content string) {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			b.Fatal(err)
		}
	}
	mkdir := func(rel string) {
		if err := os.MkdirAll(filepath.Join(root, rel), 0o755); err != nil {
			b.Fatal(err)
		}
	}
	write("stat", "cpu  1 2 3 4\nbtime 1000\n")
	write("loadavg", "0.50 0.40 0.30 1/100 1234\n")
	write("pressure/cpu", "some avg10=1.50 avg60=1.00 avg300=0.50 total=1234\n")
	write("pressure/memory", "some avg10=2.50 avg60=2.00 avg300=1.50 total=1234\n")
	write("pressure/io", "some avg10=0.25 avg60=0.20 avg300=0.10 total=1234\n")
	write("vmstat", "nr_free_pages 1\noom_kill 3\n")
	for i := 0; i < 400; i++ {
		pid := 1000 + i
		dir := filepath.Join(strconv.Itoa(pid))
		mkdir(dir)
		write(filepath.Join(dir, "stat"), fmt.Sprintf("%d (proc%d) S 1 0 0 0 0 0 0 0 0 0 %d %d 0 0 20 0 4 0 %d 123456 10 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0\n", pid, pid, i+1, i+2, 1000+i))
		write(filepath.Join(dir, "comm"), fmt.Sprintf("proc%d\n", pid))
		write(filepath.Join(dir, "status"), fmt.Sprintf("Name:\tproc%d\nUid:\t1000\t1000\t1000\t1000\nVmRSS:\t1024 kB\nVmSwap:\t0 kB\n", pid))
		write(filepath.Join(dir, "cmdline"), fmt.Sprintf("/usr/bin/proc%d --flag\\x00", pid))
		write(filepath.Join(dir, "cgroup"), "0::/system.slice/test.service\n")
		write(filepath.Join(dir, "io"), "read_bytes: 4096\nwrite_bytes: 8192\n")
		// fd dir
		fdDir := filepath.Join(dir, "fd")
		if err := os.MkdirAll(filepath.Join(root, fdDir), 0o755); err != nil {
			b.Fatal(err)
		}
	}
	// need strconv/fmt imports; add via init? Use fully qualified not possible. Instead rely on imports added below.
	_ = root
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _, _ = CollectProcesses(root, 250, 4, nil, 10*time.Second, time.Unix(2000, 0))
	}
}
