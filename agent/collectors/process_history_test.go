package collectors

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func makeProcRows(n int) []ProcessRow {
	rows := make([]ProcessRow, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, ProcessRow{
			PID:           1000 + i,
			Name:          fmt.Sprintf("proc%d", i),
			CPUPercent:    float64(n - i),
			RSSBytes:      uint64((i + 1) * 1024),
			StartTimeUnix: int64(2000 + i),
		})
	}
	return rows
}

func TestProcessHistorySamplerCadence(t *testing.T) {
	s := NewProcessHistorySampler()
	rows := makeProcRows(5)
	t0 := time.Unix(1000, 0)
	s.Observe(rows, t0)
	s.Observe(rows, t0.Add(30*time.Second)) // within 60s
	if got := len(s.History().Samples); got != 1 {
		t.Fatalf("samples = %d, want 1 (cadence not respected)", got)
	}
	s.Observe(rows, t0.Add(60*time.Second))
	if got := len(s.History().Samples); got != 2 {
		t.Fatalf("samples = %d, want 2 after one full interval", got)
	}
}

func TestProcessHistorySamplerRingEviction(t *testing.T) {
	s := NewProcessHistorySamplerWith(1*time.Second, 3, 10, 2)
	rows := makeProcRows(2)
	base := time.Unix(2000, 0)
	for i := 0; i < 5; i++ {
		s.Observe(rows, base.Add(time.Duration(i)*time.Second))
	}
	if got := len(s.History().Samples); got != 3 {
		t.Fatalf("ring size = %d, want 3", got)
	}
	// oldest dropped: first sample T should be 2002, not 2000
	if s.History().Samples[0].T != 2002 {
		t.Fatalf("oldest sample T = %d, want 2002 (ring did not drop oldest)", s.History().Samples[0].T)
	}
}

func TestProcessHistorySamplerTop20Selection(t *testing.T) {
	s := NewProcessHistorySampler()
	rows := makeProcRows(25) // CPU values 25..1
	s.Observe(rows, time.Unix(3000, 0))
	h := s.History()
	if len(h.Samples) != 1 {
		t.Fatalf("samples = %d", len(h.Samples))
	}
	if len(h.Samples[0].Rows) != 20 {
		t.Fatalf("rows = %d, want 20", len(h.Samples[0].Rows))
	}
	// highest CPU first
	if h.Samples[0].Rows[0].CPU != 25 {
		t.Fatalf("first row CPU = %v, want 25", h.Samples[0].Rows[0].CPU)
	}
	if h.Samples[0].Rows[19].CPU != 6 {
		t.Fatalf("20th row CPU = %v, want 6", h.Samples[0].Rows[19].CPU)
	}
}

func TestProcessHistorySamplerEventDetection(t *testing.T) {
	s := NewProcessHistorySampler()
	t0 := time.Unix(4000, 0)
	a := []ProcessRow{{PID: 1, Name: "alpha", StartTimeUnix: 10, CPUPercent: 1, RSSBytes: 1}}
	b := []ProcessRow{{PID: 2, Name: "beta", StartTimeUnix: 20, CPUPercent: 2, RSSBytes: 2}}

	// baseline: no events
	s.Observe(a, t0)
	if len(s.History().Events) != 0 {
		t.Fatalf("baseline events = %d, want 0", len(s.History().Events))
	}

	// beta started, alpha exited
	s.Observe(b, t0.Add(60*time.Second))
	events := s.History().Events
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
	var started, exited bool
	for _, e := range events {
		if e.Type == "started" && e.PID == 2 {
			started = true
		}
		if e.Type == "exited" && e.PID == 1 {
			exited = true
		}
	}
	if !started || !exited {
		t.Fatalf("missing started/exited events: %+v", events)
	}
}

func TestProcessHistorySamplerNoDuplicateEvents(t *testing.T) {
	s := NewProcessHistorySampler()
	t0 := time.Unix(5000, 0)
	a := []ProcessRow{{PID: 1, Name: "alpha", StartTimeUnix: 10}}
	s.Observe(a, t0)
	s.Observe(a, t0.Add(60*time.Second)) // same set
	// re-adding same pid+start_time must not duplicate started
	started := 0
	for _, e := range s.History().Events {
		if e.Type == "started" && e.PID == 1 && e.T == t0.Unix() {
			started++
		}
	}
	// baseline produced no started; second run sees it already seen, so still none
	if started != 0 {
		t.Fatalf("duplicate started events = %d", started)
	}

	// now force a started by introducing then removing then re-observing same identity
	s2 := NewProcessHistorySampler()
	s2.Observe([]ProcessRow{}, t0)
	s2.Observe(a, t0.Add(60*time.Second))               // started
	s2.Observe([]ProcessRow{}, t0.Add(120*time.Second)) // exited
	s2.Observe(a, t0.Add(180*time.Second))              // would be started again, but emitted set blocks duplicate
	started = 0
	for _, e := range s2.History().Events {
		if e.Type == "started" && e.PID == 1 {
			started++
		}
	}
	if started != 1 {
		t.Fatalf("started events = %d, want 1 (duplicate not blocked)", started)
	}
}

func TestProcessHistorySamplerCarriesIORates(t *testing.T) {
	s := NewProcessHistorySampler()
	rows := []ProcessRow{
		{PID: 10, Name: "postgres", StartTimeUnix: 1, CPUPercent: 5, RSSBytes: 1024, IOReadBps: 4096, IOWriteBps: 2048},
		{PID: 11, Name: "nginx", StartTimeUnix: 2, CPUPercent: 9, RSSBytes: 2048, IOReadBps: 512, IOWriteBps: 256},
	}
	s.Observe(rows, time.Unix(7000, 0))
	h := s.History()
	if len(h.Samples) != 1 {
		t.Fatalf("samples = %d, want 1", len(h.Samples))
	}
	sample := h.Samples[0]
	if len(sample.Rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(sample.Rows))
	}
	// Highest CPU first: nginx.
	if sample.Rows[0].Name != "nginx" || sample.Rows[0].IOReadBps != 512 || sample.Rows[0].IOWriteBps != 256 {
		t.Fatalf("first row = %+v, want nginx with 512/256 B/s", sample.Rows[0])
	}
	if sample.Rows[1].Name != "postgres" || sample.Rows[1].IOReadBps != 4096 || sample.Rows[1].IOWriteBps != 2048 {
		t.Fatalf("second row = %+v, want postgres with 4096/2048 B/s", sample.Rows[1])
	}

	// The rate travels on the wire, not only in memory.
	payload, err := json.Marshal(h)
	if err != nil {
		t.Fatalf("marshal history: %v", err)
	}
	for _, want := range []string{`"io_read_bps":4096`, `"io_write_bps":2048`, `"io_read_bps":512`, `"io_write_bps":256`} {
		if !strings.Contains(string(payload), want) {
			t.Fatalf("payload %s misses %s", payload, want)
		}
	}
}

func TestProcessHistorySamplerIOPayloadIsBounded(t *testing.T) {
	s := NewProcessHistorySampler()
	rows := makeProcRows(50)
	for i := range rows {
		rows[i].IOReadBps = float64(i+1) * 1024
		rows[i].IOWriteBps = float64(i+1) * 512
	}
	s.Observe(rows, time.Unix(8000, 0))
	sample := s.History().Samples[0]
	if len(sample.Rows) != 20 {
		t.Fatalf("rows = %d, want the top 20 cap", len(sample.Rows))
	}
	for _, row := range sample.Rows {
		if row.IOReadBps <= 0 || row.IOWriteBps <= 0 {
			t.Fatalf("row %+v lost its IO while being capped", row)
		}
	}
	payload, err := json.Marshal(sample)
	if err != nil {
		t.Fatalf("marshal sample: %v", err)
	}
	if len(payload) > 4096 {
		t.Fatalf("sample payload = %d bytes, want <= 4096", len(payload))
	}
}

// contains is a tiny local helper: the collectors package has no strings import
// pressure and the payload assertions read better this way.
func contains(haystack, needle string) bool {
	return len(needle) == 0 || indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

func TestProcessHistorySamplerDrain(t *testing.T) {
	s := NewProcessHistorySampler()
	rows := makeProcRows(2)
	s.Observe(rows, time.Unix(6000, 0))
	ph := s.Drain()
	if ph == nil || len(ph.Samples) != 1 {
		t.Fatalf("drain samples = %v", ph)
	}
	if s.Drain() != nil {
		t.Fatal("second drain should be nil")
	}
	// ring still retains
	if len(s.History().Samples) != 1 {
		t.Fatal("ring lost sample after drain")
	}
}
