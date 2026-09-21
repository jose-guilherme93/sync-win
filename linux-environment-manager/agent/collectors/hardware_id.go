package collectors

import (
	"crypto/sha256"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

// HardwareID holds stable hardware identifiers for device fingerprinting.
type HardwareID struct {
	CPUModel    string `json:"cpu_model"`
	RAMTotalKB  uint64 `json:"ram_total_kb"`
	PrimaryMAC  string `json:"primary_mac"`
	Fingerprint string `json:"fingerprint"`
}

// CollectHardwareID gathers stable hardware identifiers and computes a
// deterministic fingerprint. The fingerprint is a SHA-256 hash of the
// CPU model, total RAM, and primary Ethernet MAC address.
// These fields are chosen because they are stable across reinstalls,
// accessible without root, and sufficient to uniquely identify a machine.
func CollectHardwareID() HardwareID {
	hw := HardwareID{
		CPUModel:   readCPUIDModel(),
		RAMTotalKB: readRAMTotalKB(),
		PrimaryMAC: readPrimaryMAC(),
	}
	hw.Fingerprint = computeFingerprint(hw.CPUModel, hw.RAMTotalKB, hw.PrimaryMAC)
	return hw
}

// readCPUIDModel reads the CPU model name from /proc/cpuinfo.
func readCPUIDModel() string {
	data, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "model name") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return ""
}

// readRAMTotalKB reads total physical RAM in kB from /proc/meminfo.
func readRAMTotalKB() uint64 {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "MemTotal:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				kb, _ := strconv.ParseUint(fields[1], 10, 64)
				return kb
			}
		}
	}
	return 0
}

// readPrimaryMAC reads the MAC address of the primary non-loopback interface.
// Prefers interfaces starting with "en" (Ethernet) or "eth", then picks
// the alphabetically first non-lo interface.
func readPrimaryMAC() string {
	entries, err := os.ReadDir("/sys/class/net")
	if err != nil {
		return ""
	}

	var candidates []string
	for _, e := range entries {
		name := e.Name()
		if name == "lo" {
			continue
		}
		candidates = append(candidates, name)
	}
	if len(candidates) == 0 {
		return ""
	}

	// Prefer Ethernet-style names
	sort.Strings(candidates)
	for _, name := range candidates {
		if strings.HasPrefix(name, "en") || strings.HasPrefix(name, "eth") {
			return readMACFromFile(name)
		}
	}

	// Fallback: first non-lo interface
	return readMACFromFile(candidates[0])
}

// readMACFromFile reads the MAC address from /sys/class/net/<iface>/address.
func readMACFromFile(iface string) string {
	data, err := os.ReadFile("/sys/class/net/" + iface + "/address")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// computeFingerprint generates a deterministic SHA-256 hash from the hardware identifiers.
func computeFingerprint(cpuModel string, ramKB uint64, mac string) string {
	h := sha256.New()
	h.Write([]byte(cpuModel))
	h.Write([]byte{0}) // separator
	h.Write([]byte(strconv.FormatUint(ramKB, 10)))
	h.Write([]byte{0}) // separator
	h.Write([]byte(mac))
	return fmt.Sprintf("%x", h.Sum(nil))
}
