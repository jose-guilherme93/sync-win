// Package notify defines the notification provider contract and the registry
// that lets the server support new channels (Telegram, webhooks, e-mail, ...)
// by adding a single file with an init() registration. Providers never see
// each other's configuration and a failing provider must not affect the rest.
package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// EventType identifies what happened on the fleet. Values are persisted in
// the database, sent over the API and matched against per-provider filters,
// so they must stay stable.
type EventType string

const (
	EventDeviceOffline  EventType = "device_offline"
	EventDeviceOnline   EventType = "device_online"
	EventSyncError      EventType = "sync_error"
	EventDeviceEnrolled EventType = "device_enrolled"
)

// AllEventTypes is the default subscription list for a freshly configured
// provider.
var AllEventTypes = []EventType{
	EventDeviceOffline,
	EventDeviceOnline,
	EventSyncError,
	EventDeviceEnrolled,
}

// ValidEventType reports whether t is a known event type.
func ValidEventType(t string) bool {
	for _, known := range AllEventTypes {
		if string(known) == t {
			return true
		}
	}
	return false
}

// Event is a single notification-worthy occurrence. Message is a short,
// human-readable, single-line summary safe for Telegram/Webhook bodies.
type Event struct {
	Type       EventType `json:"type"`
	DeviceID   string    `json:"device_id"`
	Hostname   string    `json:"hostname"`
	Message    string    `json:"message"`
	OccurredAt time.Time `json:"occurred_at"`
}

// Provider delivers events through one channel. Implementations must be
// safe for concurrent use and must honour ctx cancellation.
type Provider interface {
	// Name is the stable registry key, e.g. "telegram".
	Name() string
	// Label is the human-facing name shown in the dashboard.
	Label() string
	// Description is a short human-facing explanation.
	Description() string
	// PublicFields lists config JSON keys that may be echoed back to the
	// dashboard unmasked. Everything else is write-only.
	PublicFields() []string
	// Validate checks raw config before it is stored. config is the decrypted
	// JSON document supplied by the user.
	Validate(config json.RawMessage) error
	// Send delivers one event. It must respect the per-send timeout carried
	// by ctx.
	Send(ctx context.Context, client *http.Client, config json.RawMessage, e Event) error
	// Test sends a lightweight validation message so the dashboard can offer
	// a "Send test" button.
	Test(ctx context.Context, client *http.Client, config json.RawMessage) error
}

var (
	registryMu sync.RWMutex
	registry   = map[string]Provider{}
)

// Register installs a provider. It panics on duplicate names; that is a
// build-time programming error, not a runtime condition.
func Register(p Provider) {
	registryMu.Lock()
	defer registryMu.Unlock()
	name := p.Name()
	if name == "" {
		panic("notify: provider with empty name")
	}
	if _, exists := registry[name]; exists {
		panic("notify: duplicate provider " + name)
	}
	registry[name] = p
}

// Lookup returns the provider registered under name, or nil.
func Lookup(name string) Provider {
	registryMu.RLock()
	defer registryMu.RUnlock()
	return registry[name]
}

// Providers returns every registered provider sorted by name.
func Providers() []Provider {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]Provider, 0, len(registry))
	for _, p := range registry {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}

// SanitizeEvents normalizes a user-supplied event filter: unknown values are
// dropped and duplicates removed. An empty result means "no events" and is
// distinct from nil (unset).
func SanitizeEvents(raw []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(raw))
	for _, e := range raw {
		e = strings.TrimSpace(e)
		if e == "" || !ValidEventType(e) || seen[e] {
			continue
		}
		seen[e] = true
		out = append(out, e)
	}
	return out
}

// SubscribedTo reports whether events (a stored filter) contains t.
func SubscribedTo(events []string, t EventType) bool {
	for _, e := range events {
		if e == string(t) {
			return true
		}
	}
	return false
}

// FormatMessage builds the canonical one-line notification text.
func FormatMessage(e Event) string {
	host := e.Hostname
	if host == "" {
		host = e.DeviceID
	}
	var icon string
	switch e.Type {
	case EventDeviceOffline:
		icon = "🔴"
	case EventDeviceOnline:
		icon = "🟢"
	case EventSyncError:
		icon = "⚠️"
	case EventDeviceEnrolled:
		icon = "➕"
	default:
		icon = "ℹ️"
	}
	msg := strings.TrimSpace(e.Message)
	if msg == "" {
		msg = defaultMessage(e.Type)
	}
	return fmt.Sprintf("%s [%s] %s", icon, host, msg)
}

func defaultMessage(t EventType) string {
	switch t {
	case EventDeviceOffline:
		return "went offline"
	case EventDeviceOnline:
		return "is back online"
	case EventSyncError:
		return "reported a sync error"
	case EventDeviceEnrolled:
		return "was enrolled"
	default:
		return string(t)
	}
}
