package store

import (
	"encoding/json"
	"testing"
	"time"
)

func newAgentUsageStore(t *testing.T) *Store {
	t.Helper()
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestAgentUsageSeriesReadsOnlyReportedSamples(t *testing.T) {
	s := newAgentUsageStore(t)
	user, err := s.CreateUser("usage@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	device, err := s.RegisterDevice("pc-usage", user.ID, user.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	silent, err := s.RegisterDevice("pc-silent", user.ID, user.ID, "")
	if err != nil {
		t.Fatal(err)
	}

	report := func(deviceID string, rss uint64, cpu float64) {
		t.Helper()
		payload, marshalErr := json.Marshal(map[string]any{
			"cpu_usage_percent":  12.0,
			"memory_total_bytes": 16 << 30,
			"agent_memory_bytes": rss,
			"agent_cpu_usage":    cpu,
		})
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		if appendErr := s.AppendTelemetry(deviceID, payload); appendErr != nil {
			t.Fatal(appendErr)
		}
	}

	report(device.ID, 12_000_000, 0.41)
	report(device.ID, 12_400_000, 0.52)
	// An agent that never reports its own cost must not appear as a flat zero
	// line: absent is not idle.
	payload, _ := json.Marshal(map[string]any{"cpu_usage_percent": 9.0})
	if appendErr := s.AppendTelemetry(silent.ID, payload); appendErr != nil {
		t.Fatal(appendErr)
	}

	now := time.Now().UTC()
	series, err := s.AgentUsageSeries(user.ID, now.Add(-time.Hour), now.Add(time.Minute), 60)
	if err != nil {
		t.Fatal(err)
	}

	points := series[device.ID]
	if len(points) != 2 {
		t.Fatalf("points for the reporting device = %d, want 2", len(points))
	}
	if points[0].MemoryBytes != 12_000_000 || points[1].MemoryBytes != 12_400_000 {
		t.Fatalf("RSS values = %d, %d", points[0].MemoryBytes, points[1].MemoryBytes)
	}
	if points[1].CPUPercent != 0.52 {
		t.Fatalf("CPU = %v, want 0.52", points[1].CPUPercent)
	}
	if points[0].Timestamp == "" {
		t.Error("every point needs a timestamp for the sparkline")
	}
	if _, found := series[silent.ID]; found {
		t.Fatal("a device that never reported agent cost must have no series")
	}
}

func TestAgentUsageSeriesDownsamplesToTheCap(t *testing.T) {
	s := newAgentUsageStore(t)
	user, err := s.CreateUser("usage2@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	device, err := s.RegisterDevice("pc-many", user.ID, user.ID, "")
	if err != nil {
		t.Fatal(err)
	}

	// AppendTelemetry stamps the wall clock, so insert the rows directly to get
	// a spread of samples inside the window.
	s.mu.Lock()
	base := time.Now().UTC().Add(-30 * time.Minute)
	for i := 0; i < 200; i++ {
		payload, _ := json.Marshal(map[string]any{
			"agent_memory_bytes": 10_000_000 + i,
			"agent_cpu_usage":    0.4,
		})
		at := base.Add(time.Duration(i) * time.Second)
		if _, execErr := s.db.Exec(
			"INSERT INTO telemetry_raw (device_id, payload, received_at) VALUES (?, ?, ?)",
			device.ID, string(payload), timeText(at),
		); execErr != nil {
			s.mu.Unlock()
			t.Fatal(execErr)
		}
	}
	s.mu.Unlock()

	now := time.Now().UTC()
	series, err := s.AgentUsageSeries(user.ID, now.Add(-time.Hour), now.Add(time.Minute), 20)
	if err != nil {
		t.Fatal(err)
	}
	points := series[device.ID]
	if len(points) > 20 {
		t.Fatalf("points = %d, want at most the cap", len(points))
	}
	if len(points) < 10 {
		t.Fatalf("points = %d, the downsample dropped too much", len(points))
	}
	if points[0].Timestamp >= points[len(points)-1].Timestamp {
		t.Fatalf("timestamps are not increasing: %s then %s", points[0].Timestamp, points[len(points)-1].Timestamp)
	}
}
