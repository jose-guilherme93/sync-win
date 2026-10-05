package collectors

import (
	"bufio"
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
		if len(fields) < 10 {
			continue
		}
		rxBytes, _ := strconv.ParseUint(fields[0], 10, 64)
		rxPackets, _ := strconv.ParseUint(fields[1], 10, 64)
		rxErrors, _ := strconv.ParseUint(fields[2], 10, 64)
		txBytes, _ := strconv.ParseUint(fields[8], 10, 64)
		txPackets, _ := strconv.ParseUint(fields[9], 10, 64)
		txErrors, _ := strconv.ParseUint(fields[10], 10, 64)
		interfaces = append(interfaces, NetworkInterface{
			Name:      name,
			RXBytes:   rxBytes,
			TXBytes:   txBytes,
			RXPackets: rxPackets,
			TXPackets: txPackets,
			RXErrors:  rxErrors,
			TXErrors:  txErrors,
		})
	}
	return interfaces
}
