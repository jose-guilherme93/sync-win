package collectors

import (
	"path/filepath"
	"testing"
)

const tailscaleStatusFixture = `{
  "BackendState": "Running",
  "MagicDNSSuffix": "tail5df2c3.ts.net",
  "CurrentTailnet": {
    "Name": "example.github",
    "MagicDNSSuffix": "tail5df2c3.ts.net",
    "MagicDNSEnabled": true
  },
  "Self": {
    "HostName": "workstation",
    "DNSName": "workstation.tail5df2c3.ts.net.",
    "OS": "linux",
    "TailscaleIPs": ["100.64.0.1", "fd7a:115c:a1e0::1"],
    "Online": true,
    "ExitNode": false,
    "ExitNodeOption": false
  },
  "Peer": {
    "nodekey:alpha": {
      "HostName": "alpha",
      "DNSName": "alpha.tail5df2c3.ts.net.",
      "OS": "linux",
      "TailscaleIPs": ["100.64.0.2"],
      "Online": true,
      "CurAddr": "203.0.113.10:41641",
      "Relay": "sao",
      "LastSeen": "0001-01-01T00:00:00Z",
      "Tags": ["tag:server"],
      "ExitNodeOption": true
    },
    "nodekey:bravo": {
      "HostName": "bravo",
      "DNSName": "bravo.tail5df2c3.ts.net.",
      "OS": "macOS",
      "TailscaleIPs": ["100.64.0.3"],
      "Online": true,
      "CurAddr": "",
      "Relay": "gru"
    },
    "nodekey:charlie": {
      "HostName": "charlie",
      "DNSName": "charlie.tail5df2c3.ts.net.",
      "OS": "windows",
      "TailscaleIPs": ["100.64.0.4"],
      "Online": false,
      "Relay": "sao",
      "LastSeen": "2026-10-29T10:00:00Z",
      "ExitNode": true
    }
  }
}`

func TestParseTailscaleStatus(t *testing.T) {
	overlay, err := parseTailscaleStatus([]byte(tailscaleStatusFixture), 10)
	if err != nil {
		t.Fatalf("parseTailscaleStatus: %v", err)
	}
	if overlay.Kind != "tailscale" || overlay.State != "running" {
		t.Errorf("kind/state = %q/%q, want tailscale/running", overlay.Kind, overlay.State)
	}
	if overlay.Name != "example.github" || overlay.MagicDNS != "tail5df2c3.ts.net" {
		t.Errorf("network = %q/%q, want the tailnet name and suffix", overlay.Name, overlay.MagicDNS)
	}
	if overlay.Self == nil || overlay.Self.HostName != "workstation" {
		t.Fatalf("self = %+v, want the workstation", overlay.Self)
	}
	if !overlay.Self.Online {
		t.Error("self is not online although the backend is running")
	}
	if len(overlay.Peers) != 3 {
		t.Fatalf("peers = %d, want 3", len(overlay.Peers))
	}
	if overlay.Peers[0].HostName != "alpha" || !overlay.Peers[0].Direct {
		t.Errorf("first peer = %+v, want alpha on a direct path", overlay.Peers[0])
	}
	if overlay.Peers[0].LastSeen != "" {
		t.Errorf("last_seen = %q, want empty for the zero time", overlay.Peers[0].LastSeen)
	}
	if !overlay.Peers[0].ExitNodeOption || len(overlay.Peers[0].Tags) != 1 {
		t.Errorf("peer metadata = %+v, want the exit-node option and the tag", overlay.Peers[0])
	}
	if overlay.Peers[1].HostName != "bravo" || overlay.Peers[1].Direct {
		t.Errorf("second peer = %+v, want bravo on a relay", overlay.Peers[1])
	}
	if overlay.Peers[2].HostName != "charlie" || overlay.Peers[2].Online {
		t.Errorf("third peer = %+v, want offline charlie, online nodes first", overlay.Peers[2])
	}
}

// The cap must drop offline nodes before online ones: a big tailnet that has
// slept for months must still show who is reachable now.
func TestParseTailscaleStatusCapsOnlineFirst(t *testing.T) {
	overlay, err := parseTailscaleStatus([]byte(tailscaleStatusFixture), 2)
	if err != nil {
		t.Fatalf("parseTailscaleStatus: %v", err)
	}
	if len(overlay.Peers) != 2 {
		t.Fatalf("peers = %d, want 2", len(overlay.Peers))
	}
	for _, peer := range overlay.Peers {
		if !peer.Online {
			t.Errorf("capped peer %q is offline, want only online nodes kept", peer.HostName)
		}
	}
}

func TestParseTailscaleStatusRejectsGarbage(t *testing.T) {
	if _, err := parseTailscaleStatus([]byte("not json"), 10); err == nil {
		t.Fatal("parseTailscaleStatus accepted an invalid document")
	}
}

// A missing socket must be an error with a reason, not an empty tailnet.
func TestReadTailscaleStatusMissingSocket(t *testing.T) {
	_, err := readTailscaleStatus(filepath.Join(t.TempDir(), "missing.sock"))
	if err == nil {
		t.Fatal("readTailscaleStatus accepted a missing socket")
	}
}

func TestTailscaleInterface(t *testing.T) {
	ifaces := []NetworkInterface{
		{Name: "eth0", Kind: IfaceKindPhysical},
		{Name: "tailscale0", Kind: IfaceKindTunnel},
	}
	name, ok := tailscaleInterface(ifaces)
	if !ok || name != "tailscale0" {
		t.Errorf("tailscaleInterface = %q/%v, want tailscale0/true", name, ok)
	}
	if _, ok := tailscaleInterface([]NetworkInterface{{Name: "eth0", Kind: IfaceKindPhysical}}); ok {
		t.Error("tailscaleInterface found a tailnet on a plain ethernet link")
	}
}

func TestNormalizeTailscaleState(t *testing.T) {
	cases := map[string]string{
		"Running":    "running",
		"Stopped":    "stopped",
		"NeedsLogin": "needs login",
		"":           "unknown",
	}
	for input, want := range cases {
		if got := normalizeTailscaleState(input); got != want {
			t.Errorf("normalizeTailscaleState(%q) = %q, want %q", input, got, want)
		}
	}
}
