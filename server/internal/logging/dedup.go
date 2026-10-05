package logging

import (
	"sync"
	"time"
)

// dedupEntry tracks repeated occurrences of the same event.
type dedupEntry struct {
	FirstSeen time.Time
	LastSeen  time.Time
	Count     int
	Level     Level
	Category  Category
	Event     Event
	DeviceID  string
	Message   string
}

// deduplicator prevents log storms by tracking repeated events.
type deduplicator struct {
	mu      sync.Mutex
	entries map[string]*dedupEntry
	maxAge  time.Duration
	maxSize int
	flushFn func(*dedupEntry)
	stopCh  chan struct{}
}

// newDeduplicator creates a deduplicator that flushes aggregated entries periodically.
func newDeduplicator(maxAge time.Duration, maxSize int, flushFn func(*dedupEntry)) *deduplicator {
	d := &deduplicator{
		entries: make(map[string]*dedupEntry),
		maxAge:  maxAge,
		maxSize: maxSize,
		flushFn: flushFn,
		stopCh:  make(chan struct{}),
	}
	go d.cleanupLoop()
	return d
}

func (d *deduplicator) stop() {
	close(d.stopCh)
}

// dedupKey generates a key for deduplication.
func dedupKey(level Level, category Category, event Event, deviceID string) string {
	return string(level.String() + ":" + string(category) + ":" + string(event) + ":" + deviceID)
}

// shouldDedup returns true if this event should be deduplicated.
func shouldDedup(level Level) bool {
	// Never dedup audit, fatal, or error events
	return level <= LevelWarn
}

// check returns true if the event should be logged (not a duplicate storm).
// If it's a duplicate, it increments the counter.
func (d *deduplicator) check(level Level, category Category, event Event, deviceID, message string) bool {
	if !shouldDedup(level) {
		return true
	}

	key := dedupKey(level, category, event, deviceID)

	d.mu.Lock()
	defer d.mu.Unlock()

	entry, exists := d.entries[key]
	if !exists {
		// Check if we need to flush old entries
		if len(d.entries) >= d.maxSize {
			d.flushOldestLocked()
		}
		d.entries[key] = &dedupEntry{
			FirstSeen: time.Now(),
			LastSeen:  time.Now(),
			Count:     1,
			Level:     level,
			Category:  category,
			Event:     event,
			DeviceID:  deviceID,
			Message:   message,
		}
		return true
	}

	entry.LastSeen = time.Now()
	entry.Count++

	// Allow first occurrence, then throttle
	if entry.Count <= 3 {
		return true
	}
	// After 3 occurrences, only allow every 100th
	if entry.Count%100 == 0 {
		return true
	}

	return false
}

// flushOldestLocked flushes the oldest entry. Must be called with lock held.
func (d *deduplicator) flushOldestLocked() {
	var oldestKey string
	var oldestTime time.Time
	for k, v := range d.entries {
		if oldestKey == "" || v.LastSeen.Before(oldestTime) {
			oldestKey = k
			oldestTime = v.LastSeen
		}
	}
	if oldestKey != "" && d.flushFn != nil {
		d.flushFn(d.entries[oldestKey])
		delete(d.entries, oldestKey)
	}
}

// cleanupLoop periodically flushes entries older than maxAge.
func (d *deduplicator) cleanupLoop() {
	ticker := time.NewTicker(d.maxAge)
	defer ticker.Stop()
	for {
		select {
		case <-d.stopCh:
			return
		case <-ticker.C:
			d.cleanup()
		}
	}
}

func (d *deduplicator) cleanup() {
	now := time.Now()
	d.mu.Lock()
	defer d.mu.Unlock()

	for key, entry := range d.entries {
		if now.Sub(entry.LastSeen) > d.maxAge {
			if entry.Count > 1 && d.flushFn != nil {
				d.flushFn(entry)
			}
			delete(d.entries, key)
		}
	}
}

// stats returns current deduplication statistics.
func (d *deduplicator) stats() map[string]any {
	d.mu.Lock()
	defer d.mu.Unlock()

	total := 0
	suppressed := 0
	for _, entry := range d.entries {
		total++
		if entry.Count > 3 {
			suppressed += entry.Count - 3
		}
	}
	return map[string]any{
		"tracked_events": total,
		"suppressed":     suppressed,
	}
}
