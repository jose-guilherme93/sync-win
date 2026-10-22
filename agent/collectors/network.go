package collectors

import (
	"bufio"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Interface kinds. The dashboard groups the links by these and uses them to
// explain why a link exists at all.
const (
	IfaceKindPhysical = "physical"
	IfaceKindWifi     = "wifi"
	IfaceKindTunnel   = "tunnel"
	IfaceKindBridge   = "bridge"
	IfaceKindVeth     = "veth"
	IfaceKindLoopback = "loopback"
	IfaceKindVirtual  = "virtual"
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

// tunnelInterfacePrefixes are deliberate tunnels. They are real links with
// their own traffic, so they are counted, and the dashboard shows them apart
// from the machine's physical NICs.
var tunnelInterfacePrefixes = []string{
	"wg",  // wireguard
	"tun", // generic tun
	"tap", // qemu/openssl tap
	"tailscale",
	"zt",    // zerotier
	"utun",  // macos-style tun
	"ppp",   // ppp
	"ipsec", // libreswan
	"gre",   // gre tunnels
	"sit",   // ipv6-in-ipv4
	"vpn",
	"nordlynx",
	"mullvad",
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

// IsTunnelInterface reports whether the interface is a VPN or overlay tunnel.
func IsTunnelInterface(name string) bool {
	for _, prefix := range tunnelInterfacePrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// InterfaceKind classifies a link. The name is only a hint: a wireless
// interface is recognised by its sysfs entry and a physical one by the
// backing device it has, so a renamed interface (wlp3s0, enp3s0, eth0) is
// still classified correctly.
func InterfaceKind(name string) string {
	switch {
	case name == "lo":
		return IfaceKindLoopback
	case strings.HasPrefix(name, "veth") || strings.HasPrefix(name, "vnet"):
		return IfaceKindVeth
	case strings.HasPrefix(name, "br-") || strings.HasPrefix(name, "br0") ||
		strings.HasPrefix(name, "docker") || strings.HasPrefix(name, "virbr"):
		return IfaceKindBridge
	case IsTunnelInterface(name):
		return IfaceKindTunnel
	}
	if _, err := os.Stat(filepath.Join("/sys/class/net", name, "wireless")); err == nil {
		return IfaceKindWifi
	}
	if _, err := os.Stat(filepath.Join("/sys/class/net", name, "phy80211")); err == nil {
		return IfaceKindWifi
	}
	// A physical NIC has a backing device in sysfs. A bond has no device of its
	// own but still carries real traffic, so it counts as physical too.
	if _, err := os.Stat(filepath.Join("/sys/class/net", name, "device")); err == nil {
		return IfaceKindPhysical
	}
	if _, err := os.Stat(filepath.Join("/sys/class/net", name, "bonding")); err == nil {
		return IfaceKindPhysical
	}
	// Everything else without a backing device is a software interface that
	// no physical traffic flows through.
	return IfaceKindVirtual
}

// CollectNetwork reads /proc/net/dev and returns per-interface counters.
//
// Every interface is reported, including the container and VM plumbing that
// used to be dropped here. The dashboard needs to see a veth pair or a docker
// bridge to explain where the traffic went, and the fleet totals are protected
// by the Counted flag instead of by hiding the interface.
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
		iface := NetworkInterface{
			Name:      name,
			RXBytes:   rxBytes,
			TXBytes:   txBytes,
			RXPackets: rxPackets,
			TXPackets: txPackets,
			RXErrors:  rxErrors,
			TXErrors:  txErrors,
			RXDropped: rxDropped,
			TXDropped: txDropped,
		}
		// The extra counters live past the fields the old parser read. A
		// shorter row means the kernel changed the format, so they are simply
		// left at zero instead of failing the whole collection.
		if len(fields) > 4 {
			iface.RXFIFOErrors, _ = strconv.ParseUint(fields[4], 10, 64)
		}
		if len(fields) > 7 {
			iface.Multicast, _ = strconv.ParseUint(fields[7], 10, 64)
		}
		if len(fields) > 12 {
			iface.TXFIFOErrors, _ = strconv.ParseUint(fields[12], 10, 64)
		}
		if len(fields) > 13 {
			iface.RXCollisions, _ = strconv.ParseUint(fields[13], 10, 64)
		}
		if len(fields) > 14 {
			iface.TXCarrierErrors, _ = strconv.ParseUint(fields[14], 10, 64)
		}
		interfaces = append(interfaces, enrichInterface(iface))
	}
	return interfaces
}

// enrichInterface fills the link metadata that /proc/net/dev does not carry.
// Every lookup is best-effort: a missing /sys file or an interface that
// disappeared between the two reads leaves the field empty rather than failing
// the whole collection.
func enrichInterface(iface NetworkInterface) NetworkInterface {
	iface.Kind = InterfaceKind(iface.Name)
	iface.Counted = !IsVirtualInterface(iface.Name)
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
	iface.Duplex = readSysText(iface.Name, "duplex")
	iface.Driver = readDriver(iface.Name)
	iface.CarrierChanges = uint64(readSysInt(iface.Name, "statistics/carrier_changes"))
	return iface
}

// readDriver returns the kernel driver behind an interface, for example
// "iwlwifi" or "virtio_net". It reads the device link in sysfs, which points
// at the driver directory.
func readDriver(name string) string {
	link, err := os.Readlink(filepath.Join("/sys/class/net", name, "device", "driver"))
	if err != nil {
		return ""
	}
	return filepath.Base(link)
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
