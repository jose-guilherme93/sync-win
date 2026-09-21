package collectors

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

// CollectNetwork reads /proc/net/dev and returns per-interface counters.
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
