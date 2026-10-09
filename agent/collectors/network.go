package collectors

import (
	"bufio"
	"net"
	"os"
	"strconv"
	"strings"
)

// virtualInterfacePrefixes are container/VM plumbing that must never be counted
// as network traffic. Traffic crossing a container shows up on the veth pair,
// on its bridge and on docker0 at the same time, so summing them inflates the
// dashboard numbers several times over. Real NICs and deliberate tunnels
// (tailscale, wireguard, tun/tap) are kept.
var virtualInterfacePrefixes = []string{
	"veth",    // container veth pairs
	"br-",     // docker bridges
	"br0",     // plain linux bridges
	"docker",  // docker0 + docker bridge aliases
	"virbr",   // libvirt / libnmcli bridges
	"vnet",    // kvm/qemu host taps
	"cni",     // container network interface
	"flannel", // flannel overlay
	"cali",    // calico overlay
	"kube-ipvs",
}

// IsVirtualInterface reports whether a network interface is container or VM
// plumbing whose byte counters duplicate traffic seen on a real interface.
func IsVirtualInterface(name string) bool {
	if name == "lo" {
		return true
	}
	for _, prefix := range virtualInterfacePrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// CollectNetwork reads /proc/net/dev and returns per-interface counters.
// Loopback, container and VM interfaces are dropped so the dashboard totals
// reflect real network activity instead of internal plumbing.
func CollectNetwork() []NetworkInterface {
	file, err := os.Open("/proc/net/dev")
	if err != nil {
		return nil
	}
	defer file.Close()

	var interfaces []NetworkInterface
	scanner := bufio.NewScanner(file)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		if lineNum <= 2 {
			continue // skip header lines
		}
		line := scanner.Text()
		colonIdx := strings.Index(line, ":")
		if colonIdx < 0 {
			continue
		}
		name := strings.TrimSpace(line[:colonIdx])
		if IsVirtualInterface(name) {
			continue
		}
		fields := strings.Fields(line[colonIdx+1:])
		if len(fields) < 12 {
			continue
		}
		// /proc/net/dev row: rx bytes packets errs drop fifo frame compressed
		// multicast | tx bytes packets errs drop fifo colls carrier compressed.
		rxBytes, _ := strconv.ParseUint(fields[0], 10, 64)
		rxPackets, _ := strconv.ParseUint(fields[1], 10, 64)
		rxErrors, _ := strconv.ParseUint(fields[2], 10, 64)
		rxDropped, _ := strconv.ParseUint(fields[3], 10, 64)
		txBytes, _ := strconv.ParseUint(fields[8], 10, 64)
		txPackets, _ := strconv.ParseUint(fields[9], 10, 64)
		txErrors, _ := strconv.ParseUint(fields[10], 10, 64)
		txDropped, _ := strconv.ParseUint(fields[11], 10, 64)
		interfaces = append(interfaces, enrichInterface(NetworkInterface{
			Name:      name,
			RXBytes:   rxBytes,
			TXBytes:   txBytes,
			RXPackets: rxPackets,
			TXPackets: txPackets,
			RXErrors:  rxErrors,
			TXErrors:  txErrors,
			RXDropped: rxDropped,
			TXDropped: txDropped,
		}))
	}
	return interfaces
}

// enrichInterface fills the link metadata that /proc/net/dev does not carry.
// Every lookup is best-effort: a missing /sys file or an interface that
// disappeared between the two reads leaves the field empty rather than failing
// the whole collection.
func enrichInterface(iface NetworkInterface) NetworkInterface {
	if ni, err := net.InterfaceByName(iface.Name); err == nil {
		iface.MTU = ni.MTU
		iface.MAC = ni.HardwareAddr.String()
		if addrs, err := ni.Addrs(); err == nil {
			for _, addr := range addrs {
				iface.Addresses = append(iface.Addresses, addr.String())
			}
		}
	}
	iface.OperState = readSysText(iface.Name, "operstate")
	iface.SpeedMbps = readSysInt(iface.Name, "speed")
	return iface
}

// readSysText returns the trimmed contents of /sys/class/net/<name>/<file>.
func readSysText(name, file string) string {
	data, err := os.ReadFile("/sys/class/net/" + name + "/" + file)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// readSysInt reads a single integer from /sys/class/net. Virtual interfaces
// report speed as -1 or with an error; both collapse to 0 ("unknown").
func readSysInt(name, file string) int {
	raw := readSysText(name, file)
	if raw == "" {
		return 0
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return 0
	}
	return value
}

