package collectors

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

// TCPStats is the kernel's TCP view, taken from /proc/net/snmp. The counters
// are cumulative since boot, which is why the agent also reports a per-second
// delta for retransmissions: a retransmit rate of zero for a week means the
// link is clean, and a cumulative number alone cannot say that.
type TCPStats struct {
	ActiveOpens   uint64  `json:"active_opens"`
	PassiveOpens  uint64  `json:"passive_opens"`
	AttemptFails  uint64  `json:"attempt_fails"`
	EstabResets   uint64  `json:"estab_resets"`
	CurrEstab     uint64  `json:"curr_estab"`
	InSegs        uint64  `json:"in_segs"`
	OutSegs       uint64  `json:"out_segs"`
	RetransSegs   uint64  `json:"retrans_segs"`
	InErrs        uint64  `json:"in_errs"`
	OutRsts       uint64  `json:"out_rsts"`
	RetransPerSec float64 `json:"retrans_per_sec"`
	// RetransPercent is the share of outgoing segments that were
	// retransmitted, since boot. Above a few percent the path is lossy.
	RetransPercent float64 `json:"retrans_percent"`
}

// UDPStats is the kernel's UDP view, also from /proc/net/snmp.
type UDPStats struct {
	InDatagrams  uint64 `json:"in_datagrams"`
	NoPorts      uint64 `json:"no_ports"`
	InErrors     uint64 `json:"in_errors"`
	OutDatagrams uint64 `json:"out_datagrams"`
	RcvbufErrors uint64 `json:"rcvbuf_errors"`
	SndbufErrors uint64 `json:"sndbuf_errors"`
}

// SocketCounts is the state breakdown of the socket table. A device with
// thousands of TIME_WAIT sockets or a pile of CLOSE_WAIT sockets is a device
// with a problem, and the raw totals hide it.
type SocketCounts struct {
	Established uint64 `json:"established"`
	TimeWait    uint64 `json:"time_wait"`
	CloseWait   uint64 `json:"close_wait"`
	SynSent     uint64 `json:"syn_sent"`
	SynRecv     uint64 `json:"syn_recv"`
	FinWait1    uint64 `json:"fin_wait1"`
	FinWait2    uint64 `json:"fin_wait2"`
	Listen      uint64 `json:"listen"`
	UDP         uint64 `json:"udp"`
}

// ConntrackState is the netfilter connection tracker. The count against the
// limit is the single most common reason a Linux box starts dropping
// connections.
type ConntrackState struct {
	Count int `json:"count"`
	Max   int `json:"max"`
}

// NetworkHealth is the packet-level health of the device's stack. Everything
// here comes from /proc and /sys, so the agent can read it without any
// privilege: it is counters, not configuration.
type NetworkHealth struct {
	TCP         TCPStats        `json:"tcp"`
	UDP         UDPStats        `json:"udp"`
	Sockets     SocketCounts    `json:"sockets"`
	Conntrack   *ConntrackState `json:"conntrack,omitempty"`
	CollectedAt string          `json:"collected_at,omitempty"`
}

// CollectNetworkHealth reads the kernel's network counters.
//
// It never fails: a missing /proc file yields the counters that were
// readable and leaves the rest at zero. A health block with zeros is still
// useful, and an error would take the whole telemetry cycle down for a
// detail the dashboard can live without.
func CollectNetworkHealth() NetworkHealth {
	health := NetworkHealth{}
	if tcp, udp, ok := parseProcNetSNMP("/proc/net/snmp"); ok {
		health.TCP = tcp
		health.UDP = udp
	}
	health.Sockets = countSocketStates()
	health.Conntrack = readConntrack()
	return health
}

// parseProcNetSNMP reads the Tcp and Udp sections of /proc/net/snmp. The file
// lists the same headers twice: the first block names the fields, the second
// gives the values in the same order.
func parseProcNetSNMP(path string) (TCPStats, UDPStats, bool) {
	file, err := os.Open(path)
	if err != nil {
		return TCPStats{}, UDPStats{}, false
	}
	defer file.Close()

	var tcp TCPStats
	var udp UDPStats
	found := false

	headers := map[string][]string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		protocol := fields[0]
		values := fields[1:]
		switch protocol {
		case "Tcp:":
			if _, ok := headers[protocol]; !ok {
				headers[protocol] = values
				continue
			}
			tcp = tcpStatsFrom(headers[protocol], values)
			found = true
		case "Udp:":
			if _, ok := headers[protocol]; !ok {
				headers[protocol] = values
				continue
			}
			udp = udpStatsFrom(headers[protocol], values)
		}
	}
	return tcp, udp, found
}

func tcpStatsFrom(headers, values []string) TCPStats {
	stats := TCPStats{}
	pick := func(name string) uint64 {
		for i, header := range headers {
			if header == name && i < len(values) {
				value, _ := strconv.ParseUint(values[i], 10, 64)
				return value
			}
		}
		return 0
	}
	stats.ActiveOpens = pick("ActiveOpens")
	stats.PassiveOpens = pick("PassiveOpens")
	stats.AttemptFails = pick("AttemptFails")
	stats.EstabResets = pick("EstabResets")
	stats.CurrEstab = pick("CurrEstab")
	stats.InSegs = pick("InSegs")
	stats.OutSegs = pick("OutSegs")
	stats.RetransSegs = pick("RetransSegs")
	stats.InErrs = pick("InErrs")
	stats.OutRsts = pick("OutRsts")
	if stats.OutSegs > 0 {
		stats.RetransPercent = float64(stats.RetransSegs) / float64(stats.OutSegs) * 100
	}
	return stats
}

func udpStatsFrom(headers, values []string) UDPStats {
	stats := UDPStats{}
	pick := func(name string) uint64 {
		for i, header := range headers {
			if header == name && i < len(values) {
				value, _ := strconv.ParseUint(values[i], 10, 64)
				return value
			}
		}
		return 0
	}
	stats.InDatagrams = pick("InDatagrams")
	stats.NoPorts = pick("NoPorts")
	stats.InErrors = pick("InErrors")
	stats.OutDatagrams = pick("OutDatagrams")
	stats.RcvbufErrors = pick("RcvbufErrors")
	stats.SndbufErrors = pick("SndbufErrors")
	return stats
}

// socketStates maps the TCP state byte from /proc/net/tcp onto a name. The
// file lists the state in hex on the fourth column.
var socketStates = map[string]string{
	"01": "ESTABLISHED",
	"02": "SYN_SENT",
	"03": "SYN_RECV",
	"04": "FIN_WAIT1",
	"05": "FIN_WAIT2",
	"06": "TIME_WAIT",
	"07": "CLOSE",
	"08": "CLOSE_WAIT",
	"09": "LAST_ACK",
	"0A": "LISTEN",
	"0B": "CLOSING",
}

// countSocketStates counts the entries of the socket table by state. IPv4 and
// IPv6 are counted together: an operator asks "how many connections are open",
// not "how many on each family".
func countSocketStates() SocketCounts {
	counts := SocketCounts{}
	for _, path := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		counts.addFrom(path)
	}
	counts.UDP = countLines("/proc/net/udp") + countLines("/proc/net/udp6")
	return counts
}

func (c *SocketCounts) addFrom(path string) {
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 4 {
			continue
		}
		switch socketStates[fields[3]] {
		case "ESTABLISHED":
			c.Established++
		case "TIME_WAIT":
			c.TimeWait++
		case "CLOSE_WAIT":
			c.CloseWait++
		case "SYN_SENT":
			c.SynSent++
		case "SYN_RECV":
			c.SynRecv++
		case "FIN_WAIT1":
			c.FinWait1++
		case "FIN_WAIT2":
			c.FinWait2++
		case "LISTEN":
			c.Listen++
		}
	}
}

// countLines counts the entries of a socket file, skipping the header.
func countLines(path string) uint64 {
	file, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	var count uint64
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		count++
	}
	// The first line is the header.
	if count > 0 {
		count--
	}
	return count
}

// readConntrack reads the netfilter connection tracker counters. The files
// live in /proc/sys and are world readable, so no privilege is needed. A
// kernel built without conntrack simply has no file there.
func readConntrack() *ConntrackState {
	return readConntrackFrom("/proc/sys/net/netfilter/nf_conntrack_count",
		"/proc/sys/net/netfilter/nf_conntrack_max")
}

// readConntrackFrom reads the conntrack counter files at the given paths.
func readConntrackFrom(countPath, maxPath string) *ConntrackState {
	count, err := os.ReadFile(countPath)
	if err != nil {
		return nil
	}
	max, err := os.ReadFile(maxPath)
	if err != nil {
		return nil
	}
	state := &ConntrackState{}
	state.Count, _ = strconv.Atoi(strings.TrimSpace(string(count)))
	state.Max, _ = strconv.Atoi(strings.TrimSpace(string(max)))
	return state
}
