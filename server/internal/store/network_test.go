package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func newNetworkStore(t *testing.T) *Store {
	t.Helper()
	s, err := NewStore(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestUpdateNetworkSectionRoundTrips(t *testing.T) {
	s := newNetworkStore(t)
	device, err := s.RegisterDevice("laptop", "user", "owner", "")
	if err != nil {
		t.Fatal(err)
	}

	section := NetworkSection{
		Topology: NetworkTopology{
			DefaultGateway: "192.168.2.1",
			Gateways:       []string{"192.168.2.1"},
			DNS:            []string{"127.0.0.53"},
			Routes:         []NetworkRoute{{Destination: "0.0.0.0", Gateway: "192.168.2.1", Interface: "enp3s0", Kind: "default", Family: "inet"}},
			Neighbors:      []NetworkNeighbor{{IP: "192.168.2.10", MAC: "aa:bb:cc:dd:ee:ff", Interface: "enp3s0", State: "reachable", Family: "inet"}},
			Wireless:       []WirelessLink{{Interface: "wlan0", SignalDBM: -50, Quality: 70}},
			Tunnels:        []TunnelLink{{Interface: "wg0", Kind: "wireguard", State: "up"}},
			Overlays: []OverlayNetwork{{
				Kind:     "tailscale",
				Name:     "example.github",
				MagicDNS: "tail5df2c3.ts.net",
				State:    "running",
				Self:     &OverlayNode{HostName: "laptop", Online: true, IPs: []string{"100.64.0.1"}},
				Peers: []OverlayNode{{
					HostName: "server",
					OS:       "linux",
					IPs:      []string{"100.64.0.2"},
					Online:   true,
					Direct:   true,
				}},
			}},
			CollectedAt: "2026-10-22T10:00:00Z",
		},
		Firewall: FirewallState{UFW: "active", Firewalld: "not installed", Nftables: "inactive"},
	}
	if updateErr := s.UpdateNetworkSection(context.Background(), device.ID, &section); updateErr != nil {
		t.Fatalf("UpdateNetworkSection: %v", updateErr)
	}

	got, err := s.GetNetworkSection(context.Background(), device.ID)
	if err != nil {
		t.Fatalf("GetNetworkSection: %v", err)
	}
	if got.Topology.DefaultGateway != "192.168.2.1" {
		t.Errorf("gateway = %q", got.Topology.DefaultGateway)
	}
	if len(got.Topology.Routes) != 1 || got.Topology.Routes[0].Interface != "enp3s0" {
		t.Errorf("routes = %#v", got.Topology.Routes)
	}
	if len(got.Topology.Neighbors) != 1 || got.Topology.Neighbors[0].MAC != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("neighbors = %#v", got.Topology.Neighbors)
	}
	if len(got.Topology.Overlays) != 1 {
		t.Fatalf("overlays = %#v, want the tailnet", got.Topology.Overlays)
	}
	overlay := got.Topology.Overlays[0]
	if overlay.Name != "example.github" || overlay.Self == nil || overlay.Self.HostName != "laptop" {
		t.Errorf("overlay = %#v, want the tailnet and its self node", overlay)
	}
	if len(overlay.Peers) != 1 || !overlay.Peers[0].Direct {
		t.Errorf("overlay peers = %#v, want the direct peer", overlay.Peers)
	}
	if got.Firewall.UFW != "active" || got.Firewall.Firewalld != "not installed" {
		t.Errorf("firewall = %#v", got.Firewall)
	}
}

// A device that never reported a topology yields an empty section instead of
// an error, so the screen can say "not reported yet" rather than fail.
func TestGetNetworkSectionWithoutReport(t *testing.T) {
	s := newNetworkStore(t)
	device, err := s.RegisterDevice("laptop", "user", "owner", "")
	if err != nil {
		t.Fatal(err)
	}
	section, err := s.GetNetworkSection(context.Background(), device.ID)
	if err != nil {
		t.Fatalf("GetNetworkSection: %v", err)
	}
	if len(section.Topology.Routes) != 0 {
		t.Errorf("routes = %#v, want none", section.Topology.Routes)
	}
}

func TestRecordNetworkSampleKeepsCountedInterfaces(t *testing.T) {
	s := newNetworkStore(t)
	device, err := s.RegisterDevice("laptop", "user", "owner", "")
	if err != nil {
		t.Fatal(err)
	}

	at := time.Now().UTC().Truncate(time.Minute)
	ifaces := []NetworkIface{
		{Name: "enp3s0", Counted: true, RXRate: 1000, TXRate: 500},
		{Name: "veth1234", Counted: false, RXRate: 1000, TXRate: 500},
	}
	if sampleErr := s.RecordNetworkSample(context.Background(), device.ID, ifaces, at); sampleErr != nil {
		t.Fatalf("RecordNetworkSample: %v", sampleErr)
	}
	// A second sample in the same minute must average with the first, not
	// replace it.
	if secondErr := s.RecordNetworkSample(context.Background(), device.ID, ifaces, at.Add(10*time.Second)); secondErr != nil {
		t.Fatalf("RecordNetworkSample: %v", secondErr)
	}

	history, err := s.GetNetworkHistory(context.Background(), device.ID, "1m", at.Add(-time.Hour), at.Add(time.Hour))
	if err != nil {
		t.Fatalf("GetNetworkHistory: %v", err)
	}
	points, ok := history.Interfaces["enp3s0"]
	if !ok {
		t.Fatalf("history = %#v, want the counted interface", history.Interfaces)
	}
	if len(points) != 1 {
		t.Fatalf("points = %d, want 1 bucket", len(points))
	}
	if points[0].RXRate != 1000 || points[0].TXRate != 500 {
		t.Errorf("rate = %v/%v, want 1000/500", points[0].RXRate, points[0].TXRate)
	}
	if points[0].Samples != 2 {
		t.Errorf("samples = %d, want 2", points[0].Samples)
	}
	if _, ok := history.Interfaces["veth1234"]; ok {
		t.Error("the container plumbing reached the traffic history")
	}
}

// The roll-up folds the minute buckets into five minute buckets and drops the
// minute rows it folded, so the table does not grow without bound.
func TestRollupNetworkHistory(t *testing.T) {
	s := newNetworkStore(t)
	device, err := s.RegisterDevice("laptop", "user", "owner", "")
	if err != nil {
		t.Fatal(err)
	}

	// The samples sit inside one five minute window, so the roll-up has to
	// produce a single bucket with the three samples in it.
	old := time.Now().UTC().Add(-72 * time.Hour).Truncate(5 * time.Minute)
	ifaces := []NetworkIface{{Name: "enp3s0", Counted: true, RXRate: 2000, TXRate: 1000}}
	for i := 0; i < 3; i++ {
		if sampleErr := s.RecordNetworkSample(context.Background(), device.ID, ifaces, old.Add(time.Duration(i)*time.Minute)); sampleErr != nil {
			t.Fatalf("RecordNetworkSample: %v", sampleErr)
		}
	}

	rolled, err := s.RollupNetworkHistory(context.Background())
	if err != nil {
		t.Fatalf("RollupNetworkHistory: %v", err)
	}
	if rolled != 3 {
		t.Errorf("rolled = %d rows, want 3", rolled)
	}

	history, err := s.GetNetworkHistory(context.Background(), device.ID, "5m", old.Add(-time.Hour), old.Add(time.Hour))
	if err != nil {
		t.Fatalf("GetNetworkHistory: %v", err)
	}
	points, ok := history.Interfaces["enp3s0"]
	if !ok || len(points) != 1 {
		t.Fatalf("five minute history = %#v, want one bucket", history.Interfaces)
	}
	if points[0].Samples != 3 {
		t.Errorf("samples = %d, want 3", points[0].Samples)
	}
	if points[0].RXRate != 2000 {
		t.Errorf("rate = %v, want 2000", points[0].RXRate)
	}

	// The minute rows must be gone once they were folded.
	minute, err := s.GetNetworkHistory(context.Background(), device.ID, "1m", old.Add(-time.Hour), old.Add(time.Hour))
	if err != nil {
		t.Fatalf("GetNetworkHistory: %v", err)
	}
	if len(minute.Interfaces) != 0 {
		t.Errorf("minute history = %#v, want it empty after the roll-up", minute.Interfaces)
	}
}
