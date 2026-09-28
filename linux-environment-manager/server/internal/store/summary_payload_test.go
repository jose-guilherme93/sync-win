package store

import (
	"encoding/json"
	"strings"
	"testing"
)

// The dashboard polls /api/devices every 10 seconds and renders one card per
// device. This measures the JSON the browser actually receives, which is what
// drives render cost, and proves the heavy per-telemetry fields never reach the
// client through the list endpoint.
func TestDeviceSummaryPayloadSize(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	device, err := store.RegisterDevice("payload-host", "user", "owner", "")
	if err != nil {
		t.Fatalf("register device: %v", err)
	}

	iface := func(name string, rx, tx uint64) NetworkIface {
		return NetworkIface{Name: name, RXBytes: rx, TXBytes: tx, RXRate: float64(rx), TXRate: float64(tx)}
	}

	stats := HardwareStats{
		CPUUsagePercent:  42.5,
		MemoryUsedBytes:  8 << 30,
		MemoryTotalBytes: 16 << 30,
		CPUTemperature:   61,
		UptimeSeconds:    9000,
		LoadAverage:      "1.2 1.0 0.9",
		DiskReadRate:     1024,
		DiskWriteRate:    2048,
		AgentCPUUsage:    0.3,
		AgentMemoryBytes: 12 << 20,
		AgentVersion:     "0.6.1",
		KernelVersion:    "6.8.0",
		OperatingSystem:  "linux",
		CollectedAt:      "2026-01-01T00:00:00Z",
		NetworkIFaces: []NetworkIface{
			iface("lo", 100, 100),
			iface("eth0", 200, 210),
			iface("wlan0", 300, 310),
		},
		DiskPartitions: []DiskPartition{
			{Mount: "/", Device: "/dev/vda3", TotalBytes: 100, UsedBytes: 40, FreeBytes: 60, UsedPercent: 40},
		},
		CPUCoreUsage:       []float64{10, 20, 30, 40},
		DockerAvailable:    true,
		GPUTemperature:     48,
		MemoryBuffersBytes: 1 << 20,
		MemoryCachedBytes:  2 << 20,
		TopCPUProcesses:    []ProcessInfo{{PID: 1, Name: "init", CPUPercent: 1.5, MemRSSBytes: 1024}},
		TopMemProcesses:    []ProcessInfo{{PID: 2, Name: "sshd", CPUPercent: 0.5, MemRSSBytes: 2048}},
		DockerContainers:   []DockerContainer{{ID: "abc", Name: "web", Image: "nginx", State: "running"}},
		DockerInfo:         &DockerInfo{Version: "27", Total: 1},
	}
	if _, err := store.UpdateHardwareStats(device.ID, stats); err != nil {
		t.Fatalf("UpdateHardwareStats: %v", err)
	}

	// The column as stored, for reference.
	var stored string
	if err := store.db.QueryRow("SELECT hardware_json FROM devices WHERE id = ?", device.ID).Scan(&stored); err != nil {
		t.Fatalf("read hardware_json: %v", err)
	}

	// The response the dashboard actually receives.
	summaries, err := store.ListDeviceSummariesForOwner("owner")
	if err != nil {
		t.Fatalf("ListDeviceSummariesForOwner: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}
	encoded, err := json.Marshal(summaries[0])
	if err != nil {
		t.Fatalf("marshal summary: %v", err)
	}

	t.Logf("stored hardware_json = %d bytes", len(stored))
	t.Logf("summary response     = %d bytes", len(encoded))

	// Scalars the card renders must survive the projection.
	hw := summaries[0].Hardware
	if hw.CPUUsagePercent != 42.5 {
		t.Errorf("cpu_usage_percent = %v, want 42.5", hw.CPUUsagePercent)
	}
	if hw.MemoryTotalBytes != 16<<30 {
		t.Errorf("memory_total_bytes = %v, want %d", hw.MemoryTotalBytes, 16<<30)
	}
	if len(hw.DiskPartitions) != 1 {
		t.Errorf("disk_partitions lost: got %d", len(hw.DiskPartitions))
	}
	if len(hw.NetworkIFaces) != 3 {
		t.Errorf("network_ifaces lost: got %d", len(hw.NetworkIFaces))
	}

	// Per-core usage and the Docker summary are small enough to keep on the card.
	hw2 := summaries[0].Hardware
	if len(hw2.CPUCoreUsage) != 4 {
		t.Errorf("cpu_core_usage lost: got %d", len(hw2.CPUCoreUsage))
	}
	if hw2.DockerInfo == nil || hw2.DockerInfo.Version != "27" {
		t.Errorf("docker_info lost or wrong: %#v", hw2.DockerInfo)
	}
	if !hw2.DockerAvailable {
		t.Error("docker_available lost")
	}

	// The kilobyte-sized fields must never appear in the list response. The
	// dashboard card reads them from the device detail endpoint instead.
	for _, field := range []string{"docker_containers", "top_cpu_processes", "top_mem_processes", "logs"} {
		if strings.Contains(string(encoded), `"`+field+`"`) {
			t.Errorf("summary response leaks heavy field %q", field)
		}
	}
}
