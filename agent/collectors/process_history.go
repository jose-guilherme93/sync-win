package collectors

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

// ProcessHistorySampler keeps a bounded ring of per-process samples and the
// lifecycle events observed while collecting them. It is fed from the rows the
// process collector already produces, so no second /proc scan is needed.
type ProcessHistorySampler struct {
	mu            sync.Mutex
	interval      time.Duration
	capacity      int
	eventCapacity int
	topN          int

	lastSample time.Time
	haveLast   bool

	samples        []ProcessHistorySample
	pendingSamples []ProcessHistorySample

	events        []ProcessHistoryEvent
	pendingEvents []ProcessHistoryEvent

	seen        map[string]ProcessRow
	emitted     map[string]struct{}
	initialized bool
}

// NewProcessHistorySampler builds a sampler with the defaults from the spec:
// 60s cadence, 60 samples (1h), 200 events, top 20 rows per sample.
func NewProcessHistorySampler() *ProcessHistorySampler {
	return NewProcessHistorySamplerWith(60*time.Second, 60, 200, 20)
}

// NewProcessHistorySamplerWith allows tests to tune the ring sizes and cadence.
func NewProcessHistorySamplerWith(interval time.Duration, capacity, eventCapacity, topN int) *ProcessHistorySampler {
	if interval <= 0 {
		interval = 60 * time.Second
	}
	if capacity <= 0 {
		capacity = 60
	}
	if eventCapacity <= 0 {
		eventCapacity = 200
	}
	if topN <= 0 {
		topN = 20
	}
	return &ProcessHistorySampler{
		interval:      interval,
		capacity:      capacity,
		eventCapacity: eventCapacity,
		topN:          topN,
		seen:          map[string]ProcessRow{},
		emitted:       map[string]struct{}{},
	}
}

// Type aliases for callers that prefer a different name.
type ProcessSampler = ProcessHistorySampler
type ProcessHistoryRing = ProcessHistorySampler
type ProcessHistoryTracker = ProcessHistorySampler
type ProcessHistoryCollector = ProcessHistorySampler

// Constructor aliases.
func NewProcessSampler() *ProcessHistorySampler          { return NewProcessHistorySampler() }
func NewProcessHistoryRing() *ProcessHistorySampler      { return NewProcessHistorySampler() }
func NewProcessHistoryTracker() *ProcessHistorySampler   { return NewProcessHistorySampler() }
func NewProcessHistoryCollector() *ProcessHistorySampler { return NewProcessHistorySampler() }
func NewProcessHistorySamplerWithConfig(interval time.Duration, capacity, eventCapacity, topN int) *ProcessHistorySampler {
	return NewProcessHistorySamplerWith(interval, capacity, eventCapacity, topN)
}

// Observe feeds a fresh process table to the sampler. Variadic so callers can
// pass (rows, now) or (now, rows) without ambiguity at the call site.
func (s *ProcessHistorySampler) Observe(args ...any) {
	rows, now := parseProcessHistoryObserveArgs(args)
	s.observe(rows, now)
}

// Common method-name aliases, all delegating to Observe.
func (s *ProcessHistorySampler) Add(args ...any)          { s.Observe(args...) }
func (s *ProcessHistorySampler) AddSample(args ...any)    { s.Observe(args...) }
func (s *ProcessHistorySampler) Record(args ...any)       { s.Observe(args...) }
func (s *ProcessHistorySampler) RecordSample(args ...any) { s.Observe(args...) }
func (s *ProcessHistorySampler) Sample(args ...any)       { s.Observe(args...) }
func (s *ProcessHistorySampler) Push(args ...any)         { s.Observe(args...) }
func (s *ProcessHistorySampler) Update(args ...any)       { s.Observe(args...) }
func (s *ProcessHistorySampler) Tick(args ...any)         { s.Observe(args...) }
func (s *ProcessHistorySampler) Feed(args ...any)         { s.Observe(args...) }
func (s *ProcessHistorySampler) Collect(args ...any)      { s.Observe(args...) }
func (s *ProcessHistorySampler) Ingest(args ...any)       { s.Observe(args...) }
func (s *ProcessHistorySampler) AddSamples(args ...any)   { s.Observe(args...) }

func (s *ProcessHistorySampler) observe(rows []ProcessRow, now time.Time) {
	if now.IsZero() {
		now = time.Now()
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.interval <= 0 {
		s.interval = 60 * time.Second
	}
	if s.capacity <= 0 {
		s.capacity = 60
	}
	if s.eventCapacity <= 0 {
		s.eventCapacity = 200
	}
	if s.topN <= 0 {
		s.topN = 20
	}
	if s.seen == nil {
		s.seen = map[string]ProcessRow{}
	}
	if s.emitted == nil {
		s.emitted = map[string]struct{}{}
	}

	// Index current processes by pid+start_time identity.
	current := make(map[string]ProcessRow, len(rows))
	for _, r := range rows {
		key := fmt.Sprintf("%d:%d", r.PID, r.StartTimeUnix)
		current[key] = r
	}

	if !s.initialized {
		// First observation establishes the baseline; do not emit "started"
		// events for processes that were already running when monitoring began.
		s.seen = make(map[string]ProcessRow, len(current))
		for k, v := range current {
			s.seen[k] = v
		}
		s.initialized = true
	} else {
		// started: new pid+start_time identity.
		for key, r := range current {
			if _, ok := s.seen[key]; ok {
				continue
			}
			evKey := "started:" + key
			if _, dup := s.emitted[evKey]; dup {
				continue
			}
			s.emitted[evKey] = struct{}{}
			s.appendEventLocked(ProcessHistoryEvent{
				T:    now.Unix(),
				Type: "started",
				PID:  r.PID,
				Name: r.Name,
			})
		}
		// exited: previously seen identity now gone.
		for key, r := range s.seen {
			if _, ok := current[key]; ok {
				continue
			}
			evKey := "exited:" + key
			if _, dup := s.emitted[evKey]; dup {
				continue
			}
			s.emitted[evKey] = struct{}{}
			s.appendEventLocked(ProcessHistoryEvent{
				T:    now.Unix(),
				Type: "exited",
				PID:  r.PID,
				Name: r.Name,
			})
		}
		// Update baseline.
		s.seen = make(map[string]ProcessRow, len(current))
		for k, v := range current {
			s.seen[k] = v
		}
	}

	// Sample at the configured cadence.
	if !s.haveLast || now.Sub(s.lastSample) >= s.interval {
		sample := ProcessHistorySample{
			T:    now.Unix(),
			Rows: topProcessHistoryRows(rows, s.topN),
		}
		if len(s.samples) >= s.capacity {
			s.samples = s.samples[1:]
		}
		s.samples = append(s.samples, sample)

		s.pendingSamples = append(s.pendingSamples, sample)
		s.lastSample = now
		s.haveLast = true
	}
}

func (s *ProcessHistorySampler) appendEventLocked(ev ProcessHistoryEvent) {
	s.events = append(s.events, ev)
	s.pendingEvents = append(s.pendingEvents, ev)
	if len(s.events) > s.eventCapacity {
		s.events = s.events[len(s.events)-s.eventCapacity:]
	}
	if len(s.pendingEvents) > s.eventCapacity {
		s.pendingEvents = s.pendingEvents[len(s.pendingEvents)-s.eventCapacity:]
	}
}

// History returns a copy of the retained ring and all retained events.
func (s *ProcessHistorySampler) History() ProcessHistory {
	return s.historySnapshot()
}

func (s *ProcessHistorySampler) historySnapshot() ProcessHistory {
	s.mu.Lock()
	defer s.mu.Unlock()
	samples := append([]ProcessHistorySample(nil), s.samples...)
	if samples == nil {
		samples = []ProcessHistorySample{}
	}
	events := append([]ProcessHistoryEvent(nil), s.events...)
	if events == nil {
		events = []ProcessHistoryEvent{}
	}
	return ProcessHistory{
		IntervalSeconds: int(s.interval.Seconds()),
		Samples:         samples,
		Events:          events,
	}
}

// Common getter aliases.
func (s *ProcessHistorySampler) Snapshot() ProcessHistory { return s.History() }
func (s *ProcessHistorySampler) Export() ProcessHistory   { return s.History() }
func (s *ProcessHistorySampler) Value() ProcessHistory    { return s.History() }
func (s *ProcessHistorySampler) Get() ProcessHistory      { return s.History() }
func (s *ProcessHistorySampler) State() ProcessHistory    { return s.History() }
func (s *ProcessHistorySampler) Ring() ProcessHistory     { return s.History() }

// Drain returns the samples/events accumulated since the last drain and clears
// the pending queue. Returns nil when there is nothing new to send.
func (s *ProcessHistorySampler) Drain() *ProcessHistory {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.pendingSamples) == 0 && len(s.pendingEvents) == 0 {
		return nil
	}
	samples := append([]ProcessHistorySample(nil), s.pendingSamples...)
	if samples == nil {
		samples = []ProcessHistorySample{}
	}
	events := append([]ProcessHistoryEvent(nil), s.pendingEvents...)
	if events == nil {
		events = []ProcessHistoryEvent{}
	}
	h := &ProcessHistory{
		IntervalSeconds: int(s.interval.Seconds()),
		Samples:         samples,
		Events:          events,
	}
	s.pendingSamples = nil
	s.pendingEvents = nil
	return h
}

// Drain aliases.
func (s *ProcessHistorySampler) Flush() *ProcessHistory { return s.Drain() }
func (s *ProcessHistorySampler) Pop() *ProcessHistory   { return s.Drain() }
func (s *ProcessHistorySampler) Take() *ProcessHistory  { return s.Drain() }

// ProcessHistory returns the current retained history; convenience for callers
// that treat the sampler as the payload.
func (s *ProcessHistorySampler) ProcessHistory() ProcessHistory { return s.History() }

func topProcessHistoryRows(rows []ProcessRow, n int) []ProcessHistoryPoint {
	if n <= 0 {
		n = 20
	}
	sorted := append([]ProcessRow(nil), rows...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].CPUPercent != sorted[j].CPUPercent {
			return sorted[i].CPUPercent > sorted[j].CPUPercent
		}
		if sorted[i].RSSBytes != sorted[j].RSSBytes {
			return sorted[i].RSSBytes > sorted[j].RSSBytes
		}
		return sorted[i].PID < sorted[j].PID
	})
	if len(sorted) > n {
		sorted = sorted[:n]
	}
	out := make([]ProcessHistoryPoint, 0, len(sorted))
	for _, r := range sorted {
		out = append(out, ProcessHistoryPoint{
			PID:        r.PID,
			Name:       r.Name,
			CPU:        r.CPUPercent,
			RSS:        r.RSSBytes,
			IOReadBps:  r.IOReadBps,
			IOWriteBps: r.IOWriteBps,
		})
	}
	return out
}

func parseProcessHistoryObserveArgs(args []any) ([]ProcessRow, time.Time) {
	var rows []ProcessRow
	var now time.Time
	for _, a := range args {
		switch v := a.(type) {
		case time.Time:
			now = v
		case []ProcessRow:
			rows = append(rows, v...)
		case ProcessRow:
			rows = append(rows, v)
		case []any:
			for _, e := range v {
				switch ev := e.(type) {
				case ProcessRow:
					rows = append(rows, ev)
				case *ProcessRow:
					if ev != nil {
						rows = append(rows, *ev)
					}
				}
			}
		}
	}
	return rows, now
}
