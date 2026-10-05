package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"lem/server/internal/logging"
	"lem/server/internal/store"
)

// sendTimeout bounds a single provider delivery attempt.
const sendTimeout = 10 * time.Second

// throttleWindow suppresses repeated notifications of the same type for the
// same device (e.g. a flapping agent bouncing between stale and online).
const throttleWindow = 15 * time.Minute

// watcherInterval is how often the offline detector sweeps device statuses.
const watcherInterval = 30 * time.Second

// Dispatcher routes events to every enabled, subscribed provider of an owner
// and mirrors them into the in-site inbox. It is the single entry point the
// HTTP handlers use, which keeps notification fan-out out of request paths.
type Dispatcher struct {
	store  *store.Store
	log    *logging.Logger
	client *http.Client

	mu       sync.Mutex
	lastSent map[string]time.Time // throttle key: "type\x00deviceID"

	// status watcher state
	watchMu sync.Mutex
	known   map[string]string // deviceID -> last observed status
	armed   bool              // false until the first sweep completed
	onFire  func(eventID int64)

	// SSE broadcaster for real-time notification push to browsers
	notifBroadcaster *NotifBroadcaster
}

// NewDispatcher builds a dispatcher. A nil logger disables failure logging.
func NewDispatcher(st *store.Store, log *logging.Logger) *Dispatcher {
	return &Dispatcher{
		store:            st,
		log:              log,
		client:           &http.Client{Timeout: sendTimeout},
		lastSent:         map[string]time.Time{},
		known:            map[string]string{},
		notifBroadcaster: NewNotifBroadcaster(),
	}
}

// NotifBroadcaster returns the SSE broadcaster for notification events.
func (d *Dispatcher) NotifBroadcaster() *NotifBroadcaster {
	return d.notifBroadcaster
}

// SetEventHook registers a callback invoked with the inbox id of every
// recorded event. Used by tests and future live-push transports.
func (d *Dispatcher) SetEventHook(hook func(eventID int64)) {
	d.watchMu.Lock()
	defer d.watchMu.Unlock()
	d.onFire = hook
}

// HTTPClient returns the shared HTTP client used for provider delivery.
func (d *Dispatcher) HTTPClient() *http.Client {
	return d.client
}

// Emit records the event in the owner inbox and fans out to the owner's
// enabled, subscribed providers. Delivery is asynchronous and throttled;
// Emit never blocks the caller for longer than the inbox insert.
func (d *Dispatcher) Emit(ownerID string, e Event) {
	if e.OccurredAt.IsZero() {
		e.OccurredAt = time.Now().UTC()
	}
	e.Message = FormatMessage(e)
	if d.throttled(e) {
		if d.log != nil {
			d.log.Info(logging.CatDevice, logging.EventNotifyThrottle,
				"notify emit: throttled",
				map[string]any{"notify_type": string(e.Type), "device_id": e.DeviceID, "owner_id": ownerID},
			)
		}
		return
	}
	id, err := d.store.InsertNotificationEvent(ownerID, string(e.Type), e.DeviceID, e.Hostname, e.Message)
	if err != nil {
		d.logFailure("inbox insert failed", e, err)
		return
	}
	if d.log != nil {
		d.log.Info(logging.CatDevice, logging.EventNotifyEmit,
			"notify emit: event recorded",
			map[string]any{"id": id, "notify_type": string(e.Type), "device_id": e.DeviceID, "owner_id": ownerID},
		)
	}
	d.watchMu.Lock()
	hook := d.onFire
	d.watchMu.Unlock()
	if hook != nil {
		hook(id)
	}
	// Broadcast to SSE subscribers for real-time push
	d.notifBroadcaster.Broadcast(ownerID, NotifEvent{
		ID:        id,
		Type:      string(e.Type),
		DeviceID:  e.DeviceID,
		Hostname:  e.Hostname,
		Message:   e.Message,
		CreatedAt: e.OccurredAt,
	})
	go d.deliver(ownerID, e)
}

// throttled implements the anti-flap window.
func (d *Dispatcher) throttled(e Event) bool {
	key := string(e.Type) + "\x00" + e.DeviceID
	now := time.Now()
	d.mu.Lock()
	defer d.mu.Unlock()
	if last, ok := d.lastSent[key]; ok && now.Sub(last) < throttleWindow {
		return true
	}
	d.lastSent[key] = now
	return false
}

// deliver sends the event to every enabled, subscribed provider.
func (d *Dispatcher) deliver(ownerID string, e Event) {
	configs, err := d.store.ListNotificationConfigs(ownerID)
	if err != nil {
		d.logFailure("list configs failed", e, err)
		return
	}
	if d.log != nil {
		d.log.Info(logging.CatDevice, logging.EventNotifyDeliver,
			"notify deliver: loaded configs",
			map[string]any{"owner_id": ownerID, "count": len(configs), "notify_type": string(e.Type), "device_id": e.DeviceID},
		)
	}
	for _, cfg := range configs {
		if !cfg.Enabled {
			continue
		}
		if !SubscribedTo(cfg.Events, e.Type) {
			continue
		}
		p := Lookup(cfg.Provider)
		if p == nil {
			if d.log != nil {
				d.log.Warn(logging.CatDevice, logging.EventNotifyDeliver,
					"notify deliver: unknown provider",
					map[string]any{"provider": cfg.Provider, "notify_type": string(e.Type), "device_id": e.DeviceID},
				)
			}
			continue
		}
		cfg := cfg
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
			defer cancel()
			if err := p.Send(ctx, d.client, cfg.Config, e); err != nil {
				d.logFailure("provider "+cfg.Provider+" send failed", e, err)
			} else if d.log != nil {
				d.log.Info(logging.CatDevice, logging.EventNotifyDeliver,
					"notify deliver: "+cfg.Provider+" send succeeded",
					map[string]any{"provider": cfg.Provider, "notify_type": string(e.Type), "device_id": e.DeviceID},
				)
			}
		}()
	}
}

func (d *Dispatcher) logFailure(msg string, e Event, err error) {
	if d.log == nil {
		return
	}
	d.log.Warn(logging.CatDevice, logging.EventAppError, msg, map[string]any{
		"notify_type": string(e.Type),
		"device_id":   e.DeviceID,
		"error":       err.Error(),
	})
}

// TestProvider validates and fires a provider's test message. Accepting an
// inline config lets the dashboard offer "test before save".
func (d *Dispatcher) TestProvider(ctx context.Context, providerName string, config json.RawMessage) error {
	p := Lookup(providerName)
	if p == nil {
		return errUnknownProvider(providerName)
	}
	if err := p.Validate(config); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	return p.Test(ctx, d.client, config)
}

type errUnknownProvider string

func (e errUnknownProvider) Error() string { return "unknown notification provider: " + string(e) }

// IsUnknownProvider reports whether err came from resolving an unregistered
// provider name (used to map it to HTTP 400).
func IsUnknownProvider(err error) bool {
	_, ok := err.(errUnknownProvider)
	return ok
}

// StartStatusWatcher sweeps device statuses and emits offline/online
// transitions. The first sweep only seeds the tracker so a server restart
// does not wake everybody with a storm of "device is offline" messages.
func (d *Dispatcher) StartStatusWatcher(ctx context.Context) {
	if d.log != nil {
		d.log.Info(logging.CatDevice, logging.EventStatusWatch,
			"status watcher started",
			map[string]any{"interval": watcherInterval.String()},
		)
	}
	go func() {
		ticker := time.NewTicker(watcherInterval)
		defer ticker.Stop()
		// Seed immediately so the first transitions appear after one interval.
		d.sweepStatuses()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				d.sweepStatuses()
			}
		}
	}()
}

func (d *Dispatcher) sweepStatuses() {
	devices, err := d.store.ListDevices()
	if err != nil {
		return
	}

	// Compute correct status for each device based on last_seen_at
	now := time.Now().UTC()
	for i := range devices {
		dev := &devices[i]
		// Don't override error or duplicate states
		if dev.Status == "error" || dev.Status == "duplicate" {
			continue
		}
		age := now.Sub(dev.LastSeenAt)
		var newStatus string
		switch {
		case age > 5*time.Minute:
			newStatus = "offline"
		case age > 30*time.Second:
			newStatus = "stale"
		default:
			newStatus = "online"
		}
		if newStatus != dev.Status {
			if err := d.store.UpdateDeviceStatus(dev.ID, newStatus); err == nil {
				dev.Status = newStatus
			}
		}
	}

	alive := make(map[string]bool, len(devices))
	for _, device := range devices {
		d.watchTransition(device)
		alive[device.ID] = true
	}
	d.watchMu.Lock()
	for id := range d.known {
		if !alive[id] {
			delete(d.known, id)
		}
	}
	d.armed = true
	d.watchMu.Unlock()
}

func (d *Dispatcher) watchTransition(device store.Device) {
	d.watchMu.Lock()
	prev, seen := d.known[device.ID]
	armed := d.armed
	d.known[device.ID] = device.Status
	d.watchMu.Unlock()

	if !seen || !armed || prev == device.Status {
		return
	}
	if d.log != nil {
		d.log.Info(logging.CatDevice, logging.EventStatusWatch,
			"status watcher: transition detected",
			map[string]any{"device_id": device.ID, "hostname": device.Hostname, "from": prev, "to": device.Status},
		)
	}
	switch {
	case prev == "online" && device.Status == "stale":
		// Device went stale (no report for >30s) - no notification, just log
	case prev != "offline" && device.Status == "offline":
		d.Emit(device.OwnerID, Event{Type: EventDeviceOffline, DeviceID: device.ID, Hostname: device.Hostname, Message: "went offline"})
	case prev == "offline" && device.Status == "online":
		d.Emit(device.OwnerID, Event{Type: EventDeviceOnline, DeviceID: device.ID, Hostname: device.Hostname, Message: "is back online"})
	}
}

// NotifEvent is the payload sent via SSE for real-time notification push.
type NotifEvent struct {
	ID        int64     `json:"id"`
	Type      string    `json:"type"`
	DeviceID  string    `json:"device_id"`
	Hostname  string    `json:"hostname"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
}

// NotifBroadcaster manages SSE subscriptions for notification events per owner.
type NotifBroadcaster struct {
	mu      sync.RWMutex
	clients map[string][]chan NotifEvent // ownerID → list of subscriber channels
}

// NewNotifBroadcaster creates a new notification SSE broadcaster.
func NewNotifBroadcaster() *NotifBroadcaster {
	return &NotifBroadcaster{
		clients: make(map[string][]chan NotifEvent),
	}
}

// Subscribe creates a new SSE channel for an owner and returns it.
func (b *NotifBroadcaster) Subscribe(ownerID string) chan NotifEvent {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := make(chan NotifEvent, 64)
	b.clients[ownerID] = append(b.clients[ownerID], ch)
	return ch
}

// Unsubscribe removes a channel from the subscriber list.
func (b *NotifBroadcaster) Unsubscribe(ownerID string, ch chan NotifEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	clients := b.clients[ownerID]
	for i, c := range clients {
		if c == ch {
			b.clients[ownerID] = append(clients[:i], clients[i+1:]...)
			// The channel is intentionally NOT closed. Broadcast snapshots the
			// subscriber list under RLock and sends after releasing it, so a
			// concurrent Unsubscribe that closed the channel would turn a send
			// into a panic ("send on closed channel") in callers that have no
			// recoverer (the status watcher goroutine). Consumers exit on their
			// request context, not on channel closure.
			break
		}
	}
	if len(b.clients[ownerID]) == 0 {
		delete(b.clients, ownerID)
	}
}

// Broadcast sends a notification event to all subscribers of an owner.
// Non-blocking: if a subscriber's channel is full, the message is dropped.
func (b *NotifBroadcaster) Broadcast(ownerID string, event NotifEvent) {
	b.mu.RLock()
	clients := make([]chan NotifEvent, len(b.clients[ownerID]))
	copy(clients, b.clients[ownerID])
	b.mu.RUnlock()

	for _, ch := range clients {
		select {
		case ch <- event:
		default:
			// Subscriber too slow, drop message
		}
	}
}
