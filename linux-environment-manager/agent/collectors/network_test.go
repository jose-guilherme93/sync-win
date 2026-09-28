package collectors

import "testing"

func TestIsVirtualInterface(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		// Container and VM plumbing duplicates traffic seen on a real NIC.
		{"lo", true},
		{"veth7975c2e", true},
		{"br-1591eb55b9a7", true},
		{"br0", true},
		{"docker0", true},
		{"virbr0", true},
		{"vnet0", true},
		{"cni0", true},
		{"flannel.1", true},
		{"cali0", true},
		// Real interfaces and deliberate tunnels must survive.
		{"eth0", false},
		{"wlan0", false},
		{"enp3s0", false},
		{"tailscale0", false},
		{"wg0", false},
		{"tun0", false},
		{"tap0", false},
		// A name that merely contains a prefix is not a match.
		{"vethbr0x", true},
		{"ethbr0", false},
		{"bonding", false},
	}

	for _, tt := range tests {
		if got := IsVirtualInterface(tt.name); got != tt.want {
			t.Errorf("IsVirtualInterface(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

// The dashboard used to sum every interface except loopback, which counted
// container traffic once per veth, once per bridge and again on docker0. Only
// real interfaces may reach the network totals.
func TestCollectNetworkExcludesVirtualInterfaces(t *testing.T) {
	for _, iface := range CollectNetwork() {
		if IsVirtualInterface(iface.Name) {
			t.Errorf("CollectNetwork returned virtual interface %q", iface.Name)
		}
	}
}
