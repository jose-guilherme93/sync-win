package notify

import (
	"path/filepath"
	"testing"

	"sync-win/server/internal/store"
)

func newDispatcherStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.NewStore(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// A device that comes back is the resolution of the alert that was raised
// when it went away. The anti-flap window must not swallow it, not even when
// the last "back online" was recent: that one belonged to another outage.
func TestRecoveryNoticeIsRecorded(t *testing.T) {
	st := newDispatcherStore(t)
	d := NewDispatcher(st, nil)
	const owner = "owner-1"

	// The device was already seen coming back a moment ago.
	d.Emit(owner, Event{Type: EventDeviceOnline, DeviceID: "dev-1", Hostname: "pc1"})
	// It goes away: the alert is raised.
	d.Emit(owner, Event{Type: EventDeviceOffline, DeviceID: "dev-1", Hostname: "pc1"})
	// It comes back: the resolution must be recorded.
	d.Emit(owner, Event{Type: EventDeviceOnline, DeviceID: "dev-1", Hostname: "pc1"})

	events, err := st.ListNotificationEvents(owner, 0, 10)
	if err != nil {
		t.Fatalf("ListNotificationEvents: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("events = %d (%v), want the online, the offline and the recovery", len(events), typesOf(events))
	}
	if events[2].Type != string(EventDeviceOnline) {
		t.Errorf("last event = %q, want the recovery", events[2].Type)
	}
}

// A device that flaps must not fill the inbox: one pair per window is
// enough, and the pair that closes the loop is the one that counts.
func TestFlappingDeviceIsThrottled(t *testing.T) {
	st := newDispatcherStore(t)
	d := NewDispatcher(st, nil)
	const owner = "owner-1"

	d.Emit(owner, Event{Type: EventDeviceOffline, DeviceID: "dev-2", Hostname: "pc2"})
	d.Emit(owner, Event{Type: EventDeviceOnline, DeviceID: "dev-2", Hostname: "pc2"})
	// A second outage inside the window is dropped, and so is the recovery
	// that follows it: there is no open alert to resolve.
	d.Emit(owner, Event{Type: EventDeviceOffline, DeviceID: "dev-2", Hostname: "pc2"})
	d.Emit(owner, Event{Type: EventDeviceOnline, DeviceID: "dev-2", Hostname: "pc2"})

	events, err := st.ListNotificationEvents(owner, 0, 10)
	if err != nil {
		t.Fatalf("ListNotificationEvents: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %d (%v), want one offline and one recovery", len(events), typesOf(events))
	}
}

func typesOf(events []store.NotificationEvent) []string {
	out := make([]string, 0, len(events))
	for _, e := range events {
		out = append(out, e.Type)
	}
	return out
}
