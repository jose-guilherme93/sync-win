package collectors

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

// tailscaleStatusSocket is the local API socket every tailscaled build exposes
// on Linux. It is world readable on a default install (0666), which is what
// lets the unprivileged agent ask who else is in the tailnet without ever
// reading a key or running the `tailscale` binary.
const tailscaleStatusSocket = "/var/run/tailscale/tailscaled.sock"

// tailscaleStatusPath is the read-only status endpoint. It needs no
// authentication because the socket itself is the permission boundary.
const tailscaleStatusPath = "/localapi/v0/status"

// maxTailscaleStatusBytes bounds the response. A tailnet with a thousand nodes
// produces a large document, and the contract caps the peers that are kept
// anyway.
const maxTailscaleStatusBytes = 4 << 20

// OverlayNode is one machine in an overlay network: this device or a peer.
type OverlayNode struct {
	HostName string `json:"hostname"`
	DNSName  string `json:"dns_name,omitempty"`
	OS       string `json:"os,omitempty"`
	// IPs are the addresses inside the overlay, never the physical ones.
	IPs []string `json:"ips,omitempty"`
	// Online is the overlay's own view of the node. It is a real fact for a
	// tailnet, unlike the kernel's ARP state for a LAN peer.
	Online bool `json:"online"`
	// Direct is true when traffic is not going through a relay.
	Direct bool `json:"direct,omitempty"`
	// Relay is the DERP region the node is using, empty for a direct path.
	Relay          string   `json:"relay,omitempty"`
	ExitNode       bool     `json:"exit_node,omitempty"`
	ExitNodeOption bool     `json:"exit_node_option,omitempty"`
	Tags           []string `json:"tags,omitempty"`
	// LastSeen is when the control plane last saw the node. It is empty for a
	// node that is online right now, which is how the API reports it.
	LastSeen string `json:"last_seen,omitempty"`
}

// OverlayNetwork is one overlay the device participates in, with every peer
// the overlay knows about. Only Tailscale can be read without root today; the
// type is kept generic so a second overlay can be added without changing the
// dashboard.
type OverlayNetwork struct {
	Kind string `json:"kind"`
	// Name is the human name of the network: a tailnet name, for example.
	Name string `json:"name,omitempty"`
	// MagicDNS is the DNS suffix the overlay resolves names under.
	MagicDNS  string `json:"magic_dns,omitempty"`
	Interface string `json:"interface,omitempty"`
	// State is the backend state in lower case (running, stopped, needs login).
	State string        `json:"state"`
	Self  *OverlayNode  `json:"self,omitempty"`
	Peers []OverlayNode `json:"peers,omitempty"`
	// Error explains why the peer list is missing. A stopped daemon and an
	// unreadable socket are different failures and must not look like "no
	// peers".
	Error string `json:"error,omitempty"`
}

// OverlayLimits bounds one overlay collection.
type OverlayLimits struct {
	MaxNetworks int
	MaxPeers    int
}

// CollectNetworkOverlays returns the overlays this device takes part in.
//
// It never fails the caller: an overlay that cannot be read is reported with
// an Error, and an overlay that is not installed at all is simply absent. The
// two cases are deliberately different, because "Tailscale is not installed"
// and "the agent cannot read Tailscale" send an operator to different places.
func CollectNetworkOverlays(ifaces []NetworkInterface, limits OverlayLimits) []OverlayNetwork {
	var overlays []OverlayNetwork
	if overlay, ok := collectTailscale(ifaces, limits.MaxPeers); ok {
		overlays = append(overlays, overlay)
	}
	return capList(overlays, limits.MaxNetworks)
}

// collectTailscale reads the tailnet from the local API.
func collectTailscale(ifaces []NetworkInterface, maxPeers int) (OverlayNetwork, bool) {
	ifaceName, hasInterface := tailscaleInterface(ifaces)

	if _, err := os.Stat(tailscaleStatusSocket); err != nil {
		if !hasInterface {
			// No socket and no tailscale interface: the overlay is not
			// installed, which is not an error.
			return OverlayNetwork{}, false
		}
		return OverlayNetwork{
			Kind:      "tailscale",
			Interface: ifaceName,
			State:     "unknown",
			Error: fmt.Sprintf(
				"the %s interface exists but %s is not available (%v); the agent cannot list the tailnet",
				ifaceName, tailscaleStatusSocket, err,
			),
		}, true
	}

	raw, err := readTailscaleStatus(tailscaleStatusSocket)
	if err != nil {
		return OverlayNetwork{
			Kind:      "tailscale",
			Interface: ifaceName,
			State:     "unknown",
			Error:     err.Error(),
		}, true
	}

	overlay, err := parseTailscaleStatus(raw, maxPeers)
	if err != nil {
		return OverlayNetwork{
			Kind:      "tailscale",
			Interface: ifaceName,
			State:     "unknown",
			Error:     fmt.Sprintf("the tailscale status response could not be read: %v", err),
		}, true
	}
	overlay.Interface = ifaceName
	return overlay, true
}

// tailscaleInterface finds the tailscale tunnel in the interface list. Reading
// the interface is how the agent knows the overlay is installed even when the
// socket is not reachable.
func tailscaleInterface(ifaces []NetworkInterface) (string, bool) {
	for _, iface := range ifaces {
		if iface.Kind == IfaceKindTunnel && strings.HasPrefix(iface.Name, "tailscale") {
			return iface.Name, true
		}
	}
	return "", false
}

// readTailscaleStatus performs one GET over the local API socket.
func readTailscaleStatus(socketPath string) ([]byte, error) {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			dialer := net.Dialer{Timeout: 1500 * time.Millisecond}
			return dialer.DialContext(ctx, "unix", socketPath)
		},
		DisableKeepAlives: true,
	}
	defer transport.CloseIdleConnections()

	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://local-tailscaled.sock"+tailscaleStatusPath, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("reading %s failed: %w", socketPath, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		message := strings.TrimSpace(string(detail))
		if message == "" {
			message = http.StatusText(resp.StatusCode)
		}
		return nil, fmt.Errorf("the tailscale local API answered %s: %s", resp.Status, message)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxTailscaleStatusBytes))
}

// tailscalePeer is the subset of a status peer the dashboard needs. The keys,
// the user account and the capabilities are deliberately not decoded: the
// overlay map has no use for them and the payload stays small.
type tailscalePeer struct {
	HostName       string   `json:"HostName"`
	DNSName        string   `json:"DNSName"`
	OS             string   `json:"OS"`
	TailscaleIPs   []string `json:"TailscaleIPs"`
	Online         bool     `json:"Online"`
	CurAddr        string   `json:"CurAddr"`
	Relay          string   `json:"Relay"`
	ExitNode       bool     `json:"ExitNode"`
	ExitNodeOption bool     `json:"ExitNodeOption"`
	Tags           []string `json:"Tags"`
	LastSeen       string   `json:"LastSeen"`
}

type tailscaleTailnet struct {
	Name            string `json:"Name"`
	MagicDNSSuffix  string `json:"MagicDNSSuffix"`
	MagicDNSEnabled bool   `json:"MagicDNSEnabled"`
}

type tailscaleStatus struct {
	BackendState   string                   `json:"BackendState"`
	MagicDNSSuffix string                   `json:"MagicDNSSuffix"`
	CurrentTailnet *tailscaleTailnet        `json:"CurrentTailnet"`
	Self           *tailscalePeer           `json:"Self"`
	Peer           map[string]tailscalePeer `json:"Peer"`
}

// parseTailscaleStatus turns one status document into the overlay shape the
// dashboard reads.
func parseTailscaleStatus(raw []byte, maxPeers int) (OverlayNetwork, error) {
	var status tailscaleStatus
	if err := json.Unmarshal(raw, &status); err != nil {
		return OverlayNetwork{}, err
	}

	overlay := OverlayNetwork{
		Kind:  "tailscale",
		State: normalizeTailscaleState(status.BackendState),
	}
	if status.CurrentTailnet != nil {
		overlay.Name = strings.TrimSpace(status.CurrentTailnet.Name)
		overlay.MagicDNS = strings.TrimSpace(status.CurrentTailnet.MagicDNSSuffix)
	}
	if overlay.MagicDNS == "" {
		overlay.MagicDNS = strings.TrimSpace(status.MagicDNSSuffix)
	}
	if overlay.Name == "" {
		overlay.Name = overlay.MagicDNS
	}

	if status.Self != nil {
		self := overlayNodeFromPeer(status.Self)
		self.Online = self.Online || overlay.State == "running"
		overlay.Self = &self
	}

	peers := make([]OverlayNode, 0, len(status.Peer))
	for _, peer := range status.Peer {
		node := overlayNodeFromPeer(&peer)
		if node.HostName == "" {
			continue
		}
		peers = append(peers, node)
	}
	// Online nodes first, then by name. Capping the list must never drop the
	// nodes that are actually reachable.
	sort.Slice(peers, func(i, j int) bool {
		if peers[i].Online != peers[j].Online {
			return peers[i].Online
		}
		return strings.ToLower(peers[i].HostName) < strings.ToLower(peers[j].HostName)
	})
	overlay.Peers = capList(peers, maxPeers)
	return overlay, nil
}

// overlayNodeFromPeer copies only the fields the dashboard renders.
func overlayNodeFromPeer(peer *tailscalePeer) OverlayNode {
	hostName := strings.TrimSpace(peer.HostName)
	if hostName == "" {
		hostName = strings.TrimSuffix(strings.TrimSpace(peer.DNSName), ".")
	}
	lastSeen := strings.TrimSpace(peer.LastSeen)
	// The zero time is how the API says "online, so there is no last seen".
	if strings.HasPrefix(lastSeen, "0001-01-01") {
		lastSeen = ""
	}
	return OverlayNode{
		HostName:       hostName,
		DNSName:        strings.TrimSpace(peer.DNSName),
		OS:             strings.TrimSpace(peer.OS),
		IPs:            capList(append([]string(nil), peer.TailscaleIPs...), 4),
		Online:         peer.Online,
		Direct:         strings.TrimSpace(peer.CurAddr) != "",
		Relay:          strings.TrimSpace(peer.Relay),
		ExitNode:       peer.ExitNode,
		ExitNodeOption: peer.ExitNodeOption,
		Tags:           capList(append([]string(nil), peer.Tags...), 8),
		LastSeen:       lastSeen,
	}
}

// normalizeTailscaleState turns the API's CamelCase state into the words the
// dashboard prints.
func normalizeTailscaleState(state string) string {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "running":
		return "running"
	case "stopped":
		return "stopped"
	case "needslogin":
		return "needs login"
	case "starting":
		return "starting"
	case "":
		return "unknown"
	default:
		return strings.ToLower(strings.TrimSpace(state))
	}
}
