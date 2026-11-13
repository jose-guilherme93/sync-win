package store

import (
	"encoding/json"
	"math"
	"testing"
	"time"
)

// GetTelemetrySeries backs the compact /telemetry/series endpoint: only the
// chart series leave the store, never the raw ~8 KB hardware payload.

func TestGetTelemetrySeriesRawWindowExtractsSeries(t *testing.T) {
	st, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	device, err := st.RegisterDevice("pc-series", "user", "owner", "")
	if err != nil {
		t.Fatal(err)
	}

	sample := HardwareStats{
		CPUUsagePercent:  14.2,
		MemoryUsedBytes:  621 * 1024 * 1024,
		MemoryTotalBytes: 1000 * 1024 * 1024,
		CPUTemperature:   52,
		NetworkIFaces: []NetworkIface{
			{Name: "lo", RXRate: 999, TXRate: 999},
			{Name: "wlan0", RXRate: 1234, TXRate: 240},
		},
		DiskPartitions: []DiskPartition{
			{Mount: "/home", TotalBytes: 100, UsedPercent: 80},
			{Mount: "/", TotalBytes: 500, UsedPercent: 78},
		},
	}
	payload, err := json.Marshal(sample)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AppendTelemetry(device.ID, payload); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	points, resolution, err := st.GetTelemetrySeries(device.ID, now.Add(-2*time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
	if resolution != "raw" {
		t.Fatalf("resolution = %q, want raw", resolution)
	}
	if len(points) != 1 {
		t.Fatalf("len(points) = %d, want 1", len(points))
	}
	p := points[0]
	if p.T == 0 {
		t.Fatal("point timestamp missing")
	}
	if p.CPU == nil || *p.CPU != 14.2 {
		t.Fatalf("cpu = %v, want 14.2", p.CPU)
	}
	if p.Mem == nil || math.Abs(*p.Mem-62.1) > 0.01 {
		t.Fatalf("mem = %v, want ~62.1", p.Mem)
	}
	if p.Disk == nil || *p.Disk != 78 {
		t.Fatalf("disk = %v, want 78 (root partition)", p.Disk)
	}
	if p.RX == nil || *p.RX != 1234 {
		t.Fatalf("rx = %v, want 1234 (lo excluded)", p.RX)
	}
	if p.TX == nil || *p.TX != 240 {
		t.Fatalf("tx = %v, want 240", p.TX)
	}
	if p.Temp == nil || *p.Temp != 52 {
		t.Fatalf("temp = %v, want 52", p.Temp)
	}
}

func TestGetTelemetrySeriesAbsentFieldsAreNullNotZero(t *testing.T) {
	st, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	device, err := st.RegisterDevice("pc-null", "user", "owner", "")
	if err != nil {
		t.Fatal(err)
	}
	// A raw payload whose producer omitted the optional fields entirely; the
	// Go struct marshaller would write explicit zeros, so build the JSON by
	// hand to exercise true absence.
	payload := []byte(`{"cpu_usage_percent":7.5}`)
	if err := st.AppendTelemetry(device.ID, payload); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	points, _, err := st.GetTelemetrySeries(device.ID, now.Add(-2*time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 1 {
		t.Fatalf("len(points) = %d, want 1", len(points))
	}
	p := points[0]
	if p.CPU == nil || *p.CPU != 7.5 {
		t.Fatalf("cpu = %v, want 7.5", p.CPU)
	}
	for name, got := range map[string]*float64{"mem": p.Mem, "disk": p.Disk, "rx": p.RX, "tx": p.TX, "temp": p.Temp} {
		if got != nil {
			t.Fatalf("%s = %v, want null (field absent from payload)", name, *got)
		}
	}
}

func TestGetTelemetrySeriesDownsampledWindowUsesStoredTables(t *testing.T) {
	st, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	device, err := st.RegisterDevice("pc-agg", "user", "owner", "")
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	for i := 0; i < 3; i++ {
		agg := TelemetryDownsampled{
			DeviceID:    device.ID,
			Timestamp:   timeText(now.Add(-time.Duration(3-i) * time.Minute)),
			Resolution:  "1m",
			CPUAvg:      float64(10 + i),
			MemAvg:      40 + float64(i),
			NetRXAvg:    100 + float64(i),
			NetTXAvg:    50 + float64(i),
			TempAvg:     55,
			SampleCount: 6,
		}
		if err := st.AppendTelemetryDownsampled(agg); err != nil {
			t.Fatal(err)
		}
	}

	// 12h resolves to 1m, which is only available downsampled: no new table.
	points, resolution, err := st.GetTelemetrySeries(device.ID, now.Add(-12*time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
	if resolution != "1m" {
		t.Fatalf("resolution = %q, want 1m", resolution)
	}
	if len(points) != 3 {
		t.Fatalf("len(points) = %d, want 3", len(points))
	}
	if points[0].CPU == nil || *points[0].CPU != 10 {
		t.Fatalf("cpu = %v, want 10", points[0].CPU)
	}
	if points[0].Mem == nil || *points[0].Mem != 40 {
		t.Fatalf("mem = %v, want 40", points[0].Mem)
	}
	if points[0].Disk != nil {
		t.Fatalf("disk = %v, want null (disk usage is not stored downsampled)", *points[0].Disk)
	}
	if points[2].T <= points[0].T {
		t.Fatalf("points not ordered by time: %d then %d", points[0].T, points[2].T)
	}
}

func TestGetTelemetrySeriesEmptyWindowReturnsEmptyPoints(t *testing.T) {
	st, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	device, err := st.RegisterDevice("pc-empty", "user", "owner", "")
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	points, resolution, err := st.GetTelemetrySeries(device.ID, now.Add(-time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 0 {
		t.Fatalf("len(points) = %d, want 0", len(points))
	}
	if resolution == "" {
		t.Fatal("resolution must still be reported for an empty window")
	}
}
