package store

import (
	"fmt"
	"testing"
	"time"
)

func TestAggregatorWritesExpectedResolutions(t *testing.T) {
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	device, err := s.RegisterDevice("aggregation-pc", "user", "owner", "")
	if err != nil {
		t.Fatal(err)
	}

	aggregator := NewAggregator(s)
	start := time.Now().UTC().Add(-time.Hour)
	for i := 0; i < rawPer1m*rawPer5m*rawPer1h; i++ {
		payload := []byte(fmt.Sprintf(`{"cpu_usage_percent":%d,"memory_used_bytes":50,"memory_total_bytes":100}`, i%100))
		aggregator.FeedRaw(device.ID, payload, start.Add(time.Duration(i)*10*time.Second))
	}

	for _, test := range []struct {
		resolution string
		want       int
	}{{"1m", 60}, {"5m", 12}, {"1h", 1}} {
		points, err := s.GetDownsampledRange(device.ID, test.resolution, start, start.Add(time.Hour))
		if err != nil {
			t.Fatalf("read %s points: %v", test.resolution, err)
		}
		if len(points) != test.want {
			t.Errorf("%s points = %d, want %d", test.resolution, len(points), test.want)
		}
	}
}

func TestGetBestHistoryUsesIntervalResolution(t *testing.T) {
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	device, err := s.RegisterDevice("history-pc", "user", "owner", "")
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	for _, resolution := range []string{"1m", "5m", "1h"} {
		for _, age := range []time.Duration{30 * time.Minute, 24 * time.Hour} {
			if err := s.AppendTelemetryDownsampled(TelemetryDownsampled{
				DeviceID: device.ID, Timestamp: now.Add(-age).Format(time.RFC3339), Resolution: resolution,
				SampleCount: 1,
			}); err != nil {
				t.Fatal(err)
			}
		}
	}

	for _, test := range []struct {
		window time.Duration
		want   string
	}{{time.Hour, "1m"}, {6 * time.Hour, "1m"}, {24 * time.Hour, "5m"}} {
		points, got, err := s.GetBestHistory(device.ID, now.Add(-test.window), now)
		if err != nil {
			t.Fatalf("history for %s: %v", test.window, err)
		}
		if got != test.want || len(points) == 0 {
			t.Errorf("history for %s = %s (%d points), want %s with data", test.window, got, len(points), test.want)
		}
	}
}
