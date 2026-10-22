package collectors

import (
	"bufio"
	"net"
	"os"
	"strconv"
	"strings"
)

// NetworkRoute is one entry of the routing table.
type NetworkRoute struct {
	Destination string `json:"destination"`
	Gateway     string `json:"gateway,omitempty"`
	Interface   string `json:"interface"`
	Metric      int    `json:"metric,omitempty"`
	// Kind is default, network, host or link. It tells the reader whether
	// the route is the way out of the machine or a local shortcut.
	Kind string `json:"kind"`
	// Family is inet or inet6.
	Family string `json:"family"`
}

// NetworkNeighbor is one entry of the neighbor table: who else is on the
// local network, as the kernel currently sees them.
type NetworkNeighbor struct {
	IP        string `json:"ip"`
	MAC       string `json:"mac,omitempty"`
	Interface string `json:"interface"`
	State     string `json:"state,omitempty"`
	Family    string `json:"family"`
}

// WirelessLink is the radio quality of a wireless interface. The agent runs
// unprivileged, so the SSID, the channel and the negotiated bitrate are not
// available: those need the nl80211 API. Signal and noise are readable and
// are what an operator looks at when the link is slow.
type WirelessLink struct {
	Interface string `json:"interface"`
	// SignalDBM is the received signal. 0 means the kernel did not report it.
	SignalDBM int `json:"signal_dbm"`
	// Quality is the kernel's link quality, 0 to 70 by convention.
	Quality int `json:"quality"`
	// NoiseDBM is the noise floor.
	NoiseDBM int `json:"noise_dbm"`
}

// TunnelLink is a VPN or overlay interface on the device.
type TunnelLink struct {
	Interface string `json:"interface"`
	// Kind is the tunnel family: wireguard, tailscale, tun, ppp and so on.
	Kind string `json:"kind"`
	// State is the operstate of the link.
	State string `json:"state,omitempty"`
}

// NetworkTopology is the shape of the device's network: how it reaches the
// internet, what it resolves names with, and who else is on the segment.
//
// It changes on the order of minutes, not seconds, so it travels with the
// system inventory rather than with every telemetry cycle.
type NetworkTopology struct {
	DefaultGateway string            `json:"default_gateway,omitempty"`
	Gateways       []string          `json:"gateways,omitempty"`
	DNS            []string          `json:"dns,omitempty"`
	SearchDomains  []string          `json:"search_domains,omitempty"`
	Routes         []NetworkRoute    `json:"routes,omitempty"`
	Neighbors      []NetworkNeighbor `json:"neighbors,omitempty"`
	Wireless       []WirelessLink    `json:"wireless,omitempty"`
	Tunnels        []TunnelLink      `json:"tunnels,omitempty"`
	CollectedAt    string            `json:"collected_at,omitempty"`
}

// TopologyLimits bounds what one topology collection may report. The values
// come from the contract, so the limits the agent enforces and the limits the
// documentation states are the same numbers.
type TopologyLimits struct {
	MaxRoutes    int
	MaxNeighbors int
	MaxWireless  int
	MaxTunnels   int
}

// CollectNetworkTopology reads the network configuration of the device.
//
// The interfaces are passed in because the caller already has them: the
// tunnels and the wireless links are derived from that list instead of being
// read a second time.
func CollectNetworkTopology(ifaces []NetworkInterface, limits TopologyLimits) NetworkTopology {
	topology := NetworkTopology{}
	topology.Routes = capList(dedupeRoutes(readRoutes()), limits.MaxRoutes)
	topology.DNS, topology.SearchDomains = readResolvConf()
	topology.Neighbors = capList(readNeighbors(), limits.MaxNeighbors)
	topology.Wireless = capList(readWireless(), limits.MaxWireless)
	topology.Tunnels = capList(tunnelsFrom(ifaces), limits.MaxTunnels)

	for _, route := range topology.Routes {
		if route.Kind != "default" || !isUsableGateway(route.Gateway) {
			continue
		}
		if topology.DefaultGateway == "" {
			topology.DefaultGateway = route.Gateway
		}
		if !containsString(topology.Gateways, route.Gateway) {
			topology.Gateways = append(topology.Gateways, route.Gateway)
		}
	}
	return topology
}

// isUsableGateway reports whether a next hop can actually be used. The
// all-zero address means the route has no gateway, which is what a link-local
// or unreachable route looks like.
func isUsableGateway(gateway string) bool {
	switch gateway {
	case "", "0.0.0.0", "::":
		return false
	}
	return true
}

// dedupeRoutes drops the routes that repeat. The kernel lists one entry per
// source address, so a host with two IPv6 addresses shows the same default
// route twice.
func dedupeRoutes(routes []NetworkRoute) []NetworkRoute {
	seen := make(map[string]struct{}, len(routes))
	out := make([]NetworkRoute, 0, len(routes))
	for _, route := range routes {
		key := route.Family + "|" + route.Destination + "|" + route.Gateway + "|" + route.Interface + "|" + strconv.Itoa(route.Metric)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, route)
	}
	return out
}

// tunnelsFrom picks the tunnel interfaces out of the interface list.
func tunnelsFrom(ifaces []NetworkInterface) []TunnelLink {
	var tunnels []TunnelLink
	for _, iface := range ifaces {
		if iface.Kind != IfaceKindTunnel {
			continue
		}
		tunnels = append(tunnels, TunnelLink{
			Interface: iface.Name,
			Kind:      tunnelKind(iface.Name),
			State:     iface.OperState,
		})
	}
	return tunnels
}

// tunnelKind names the tunnel family behind an interface name.
func tunnelKind(name string) string {
	switch {
	case strings.HasPrefix(name, "wg"):
		return "wireguard"
	case strings.HasPrefix(name, "tailscale"):
		return "tailscale"
	case strings.HasPrefix(name, "zt"):
		return "zerotier"
	case strings.HasPrefix(name, "tun"):
		return "tun"
	case strings.HasPrefix(name, "tap"):
		return "tap"
	case strings.HasPrefix(name, "ppp"):
		return "ppp"
	case strings.HasPrefix(name, "ipsec"):
		return "ipsec"
	default:
		return "tunnel"
	}
}

// readRoutes reads the IPv4 routing table and the IPv6 default route.
func readRoutes() []NetworkRoute {
	return append(readIPv4Routes("/proc/net/route"), readIPv6DefaultRoute("/proc/net/ipv6_route")...)
}

// readIPv4Routes reads the IPv4 routing table.
func readIPv4Routes(path string) []NetworkRoute {
	var routes []NetworkRoute
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 8 || fields[0] == "Iface" {
			continue
		}
		destination := hexToIPv4(fields[1])
		gateway := hexToIPv4(fields[2])
		flags, _ := strconv.ParseUint(fields[3], 16, 32)
		metric, _ := strconv.Atoi(fields[6])

		kind := "network"
		switch {
		case destination == "0.0.0.0":
			kind = "default"
		case flags&0x4 != 0:
			kind = "host"
		}
		routes = append(routes, NetworkRoute{
			Destination: destination,
			Gateway:     gateway,
			Interface:   fields[0],
			Metric:      metric,
			Kind:        kind,
			Family:      "inet",
		})
	}
	return routes
}

// readIPv6DefaultRoute reads the default route from /proc/net/ipv6_route.
// The file is hexadecimal in every column, which is why it is parsed apart
// from the IPv4 table.
func readIPv6DefaultRoute(path string) []NetworkRoute {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = file.Close() }()

	var routes []NetworkRoute
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 10 {
			continue
		}
		destination := fields[0]
		if !isZeroIPv6(destination) {
			continue
		}
		nextHop := hexToIPv6(fields[4])
		metric, _ := strconv.Atoi(fields[5])
		iface := fields[9]
		routes = append(routes, NetworkRoute{
			Destination: "::/0",
			Gateway:     nextHop,
			Interface:   iface,
			Metric:      metric,
			Kind:        "default",
			Family:      "inet6",
		})
	}
	return routes
}

// readResolvConf reads the resolvers and the search list. A resolver on
// 127.0.0.53 is the systemd-resolved stub, which is worth showing as is: the
// real upstream servers live in resolved's own configuration.
func readResolvConf() ([]string, []string) {
	return readResolvConfFrom("/etc/resolv.conf")
}

// readResolvConfFrom reads a resolver configuration file.
func readResolvConfFrom(path string) ([]string, []string) {
	file, err := os.Open(path)
	if err != nil {
		return nil, nil
	}
	defer func() { _ = file.Close() }()

	var dns []string
	var search []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "nameserver":
			if !containsString(dns, fields[1]) {
				dns = append(dns, fields[1])
			}
		case "search":
			for _, domain := range fields[1:] {
				if !containsString(search, domain) {
					search = append(search, domain)
				}
			}
		case "domain":
			if !containsString(search, fields[1]) {
				search = append(search, fields[1])
			}
		}
	}
	return dns, search
}

// readNeighbors reads the ARP table and, when present, the IPv6 neighbor
// cache. Entries without a hardware address are dropped: they are unresolved
// requests, not devices.
func readNeighbors() []NetworkNeighbor {
	return append(readARPNeighbors("/proc/net/arp"), readIPv6Neighbors("/proc/net/ndisc_cache")...)
}

// readARPNeighbors reads the IPv4 neighbor table.
func readARPNeighbors(path string) []NetworkNeighbor {
	var neighbors []NetworkNeighbor
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 6 || fields[0] == "IP" {
			continue
		}
		mac := fields[3]
		if mac == "00:00:00:00:00:00" {
			continue
		}
		// The flags column is written as "0x2", which ParseUint does not
		// accept with the prefix, so it is stripped before the base is given.
		flags, _ := strconv.ParseUint(strings.TrimPrefix(fields[2], "0x"), 16, 32)
		state := "stale"
		switch {
		case flags&0x2 != 0:
			state = "reachable"
		case flags == 0:
			state = "failed"
		}
		neighbors = append(neighbors, NetworkNeighbor{
			IP:        fields[0],
			MAC:       mac,
			Interface: fields[5],
			State:     state,
			Family:    "inet",
		})
	}
	return neighbors
}

// readIPv6Neighbors reads /proc/net/ndisc_cache. The format is positional and
// varies with the kernel, so the lladdr and the device name are located by
// their keywords instead of by column number.
func readIPv6Neighbors(path string) []NetworkNeighbor {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = file.Close() }()

	var neighbors []NetworkNeighbor
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 3 {
			continue
		}
		ip := fields[1]
		mac := ""
		device := ""
		for i, field := range fields {
			switch field {
			case "lladdr":
				if i+1 < len(fields) {
					mac = fields[i+1]
				}
			case "dev":
				if i+1 < len(fields) {
					device = fields[i+1]
				}
			}
		}
		if mac == "" || !strings.Contains(ip, ":") {
			continue
		}
		neighbors = append(neighbors, NetworkNeighbor{
			IP:        ip,
			MAC:       mac,
			Interface: device,
			State:     "reachable",
			Family:    "inet6",
		})
	}
	return neighbors
}

// readWireless reads the radio quality of every wireless interface.
func readWireless() []WirelessLink {
	return readWirelessFrom("/proc/net/wireless")
}

// readWirelessFrom reads a wireless statistics file.
func readWirelessFrom(path string) []WirelessLink {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = file.Close() }()

	var links []WirelessLink
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		colon := strings.Index(line, ":")
		if colon < 0 {
			continue // header lines
		}
		name := strings.TrimSpace(line[:colon])
		fields := strings.Fields(line[colon+1:])
		if len(fields) < 4 {
			continue
		}
		link := parseWirelessNumber(fields[1])
		level := parseWirelessNumber(fields[2])
		noise := parseWirelessNumber(fields[3])
		links = append(links, WirelessLink{
			Interface: name,
			SignalDBM: level,
			Quality:   link,
			NoiseDBM:  noise,
		})
	}
	return links
}

// parseWirelessNumber reads a value like "-50." or "70." into an int.
func parseWirelessNumber(raw string) int {
	value, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(raw), "."), 64)
	if err != nil {
		return 0
	}
	return int(value)
}

// hexToIPv4 converts the little-endian hex address used by /proc/net/route
// into the usual dotted form.
func hexToIPv4(hex string) string {
	value, err := strconv.ParseUint(hex, 16, 32)
	if err != nil {
		return ""
	}
	return net.IPv4(byte(value), byte(value>>8), byte(value>>16), byte(value>>24)).String()
}

// hexToIPv6 converts the 32 character hex address of /proc/net/ipv6_route
// into the usual form.
func hexToIPv6(hex string) string {
	if len(hex) != 32 {
		return ""
	}
	bytes := make([]byte, 0, 16)
	for i := 0; i+2 <= len(hex); i += 2 {
		value, err := strconv.ParseUint(hex[i:i+2], 16, 8)
		if err != nil {
			return ""
		}
		bytes = append(bytes, byte(value))
	}
	return net.IP(bytes).String()
}

// isZeroIPv6 reports whether an IPv6 address in hex form is all zeroes.
func isZeroIPv6(hex string) bool {
	return strings.Trim(hex, "0") == ""
}

// containsString reports whether a slice already holds a value.
func containsString(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

// capList truncates a list to the contract's limit. A limit of zero means
// "no limit", which is how an unset contract value is treated.
func capList[T any](list []T, limit int) []T {
	if limit > 0 && len(list) > limit {
		return list[:limit]
	}
	return list
}
