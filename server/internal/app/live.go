package app

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"sync-win/server/internal/store"
)

// Live mode lets the dashboard open a short lease on a device it is actively
// being watched. While the lease holds, the settings the agent polls every
// cycle report the fastest cadence; when it lapses the stored value resumes.
// Nothing here is persisted: a server restart simply drops the acceleration
// for at most one lease period. The minimum interval itself stays untouched —
// telemetry.interval_seconds_allowed keeps 2 s as its floor on purpose, since
// sampling at 1 s costs the user's machine.

const (
	// liveLeaseDuration is how long one POST /live activation holds. The
	// dashboard re-POSTs while the device screen is open, so the lease lapses
	// on its own shortly after the viewer leaves.
	liveLeaseDuration = 60 * time.Second
	// liveMaxWindow is the absolute ceiling counted from the first activation
	// of a window. Renewals cannot push it forward; once it passes, the lease
	// is refused until an explicit active:false is followed by active:true.
	liveMaxWindow = 15 * time.Minute
	// liveIntervalSeconds is the cadence served while a lease holds. It is the
	// same floor the collection contract allows; no faster value is ever sent.
	liveIntervalSeconds = 2
)

type liveLease struct {
	// startedAt is the first activation of the current ceiling window.
	startedAt time.Time
	// until is the current lease end.
	until time.Time
	// exhausted marks a window whose ceiling was reached; a new one needs an
	// explicit active:false first, so the entry is kept until that arrives.
	exhausted bool
}

// liveLeaseStore holds the per-device live leases in memory only.
type liveLeaseStore struct {
	mu     sync.Mutex
	leases map[string]liveLease
}

func newLiveLeaseStore() *liveLeaseStore {
	return &liveLeaseStore{leases: map[string]liveLease{}}
}

// set opens, renews or closes the lease for a device and returns whether a
// lease holds afterwards plus its end (zero for a closed lease). Expired
// entries are pruned here, on every use, so the map cannot grow unbounded.
// A nil store reports no lease; both constructors always initialize it.
func (l *liveLeaseStore) set(deviceID string, active bool, now time.Time) (bool, time.Time) {
	if l == nil {
		return false, time.Time{}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pruneLocked(now)

	if !active {
		delete(l.leases, deviceID)
		return false, time.Time{}
	}
	lease, ok := l.leases[deviceID]
	if ok {
		windowEnd := lease.startedAt.Add(liveMaxWindow)
		if lease.exhausted || !now.Before(windowEnd) {
			lease.exhausted = true
			l.leases[deviceID] = lease
			return false, windowEnd
		}
		until := now.Add(liveLeaseDuration)
		if until.After(windowEnd) {
			until = windowEnd
		}
		lease.until = until
		l.leases[deviceID] = lease
		return true, until
	}
	until := now.Add(liveLeaseDuration)
	l.leases[deviceID] = liveLease{startedAt: now, until: until}
	return true, until
}

// active reports whether a lease holds for the device right now.
func (l *liveLeaseStore) active(deviceID string, now time.Time) bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	lease, ok := l.leases[deviceID]
	return ok && now.Before(lease.until)
}

// pruneLocked drops entries whose window ended more than one lease period ago.
// They only existed to enforce the active:false-before-restart rule; after that
// grace they are stale and a fresh activation may open a new window.
func (l *liveLeaseStore) pruneLocked(now time.Time) {
	for id, lease := range l.leases {
		if now.After(lease.until) && now.Sub(lease.startedAt) > liveMaxWindow+liveLeaseDuration {
			delete(l.leases, id)
		}
	}
}

// deviceSettingsResponse augments the stored settings with the live lease
// state. The embedded struct keeps the wire shape: live_active rides alongside
// collection_interval_seconds, which is overridden to the live cadence while
// the lease holds. PATCH decoding ignores the extra field.
type deviceSettingsResponse struct {
	store.DeviceSettings
	LiveActive bool `json:"live_active"`
}

// handleDeviceLive opens, renews or closes a device's live lease. Like the
// sibling device routes it is owner-session only (CSRF is enforced by the
// global middleware) and a device that does not exist is reported as 404.
func (s *Server) handleDeviceLive(w http.ResponseWriter, r *http.Request, deviceID string) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !s.requireOwnedDevice(w, r, deviceID) {
		return
	}
	var req struct {
		Active bool `json:"active"`
	}
	if !s.decodeBody(w, r, &req) {
		return
	}
	active, until := s.liveLeases.set(deviceID, req.Active, s.liveClock())
	w.Header().Set("Content-Type", "application/json")
	resp := struct {
		Active bool  `json:"active"`
		Until  int64 `json:"until"`
	}{Active: active}
	if !until.IsZero() {
		resp.Until = until.Unix()
	}
	_ = json.NewEncoder(w).Encode(resp)
}

// liveClock is the lease clock; tests replace s.now.
func (s *Server) liveClock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}
