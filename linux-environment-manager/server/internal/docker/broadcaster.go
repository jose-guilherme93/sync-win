package docker

import (
	"encoding/json"
	"log"
	"sync"
)

// DockerState represents the cached Docker state for a device,
// sent via SSE to subscribed browsers.
type DockerState struct {
	Available  bool             `json:"available"`
	Containers []DockerSummary  `json:"containers"`
	Info       *DockerInfoData  `json:"info,omitempty"`
}

// DockerSummary is a lightweight container snapshot.
type DockerSummary struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Image  string `json:"image"`
	State  string `json:"state"`
	Status string `json:"status"`
}

// DockerInfoData holds engine info.
type DockerInfoData struct {
	Version   string `json:"version"`
	Total     int    `json:"total"`
	Running   int    `json:"running"`
	Stopped   int    `json:"stopped"`
	Paused    int    `json:"paused"`
	Images    int    `json:"images"`
	Driver    string `json:"driver"`
	NCPU      int    `json:"ncpu"`
}

// Broadcaster manages SSE subscriptions for Docker state per device.
type Broadcaster struct {
	mu      sync.RWMutex
	clients map[string][]chan []byte // deviceID → list of subscriber channels
}

// NewBroadcaster creates a new SSE broadcaster.
func NewBroadcaster() *Broadcaster {
	return &Broadcaster{
		clients: make(map[string][]chan []byte),
	}
}

// Subscribe creates a new SSE channel for a device and returns it.
// The channel is buffered (64 messages) to avoid blocking the broadcaster.
func (b *Broadcaster) Subscribe(deviceID string) chan []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := make(chan []byte, 64)
	b.clients[deviceID] = append(b.clients[deviceID], ch)
	return ch
}

// Unsubscribe removes a channel from the subscriber list.
func (b *Broadcaster) Unsubscribe(deviceID string, ch chan []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	clients := b.clients[deviceID]
	for i, c := range clients {
		if c == ch {
			b.clients[deviceID] = append(clients[:i], clients[i+1:]...)
			// The channel is intentionally NOT closed. Broadcast copies the
			// subscriber slice under RLock and sends after releasing it; closing
			// here would race that send into a panic. Consumers exit on their
			// request context.
			break
		}
	}
	if len(b.clients[deviceID]) == 0 {
		delete(b.clients, deviceID)
	}
}

// Broadcast sends Docker state to all subscribers of a device.
// Non-blocking: if a subscriber's channel is full, the message is dropped.
func (b *Broadcaster) Broadcast(deviceID string, state DockerState) {
	data, err := json.Marshal(state)
	if err != nil {
		log.Printf("docker broadcaster: marshal error: %v", err)
		return
	}
	b.mu.RLock()
	clients := make([]chan []byte, len(b.clients[deviceID]))
	copy(clients, b.clients[deviceID])
	b.mu.RUnlock()

	for _, ch := range clients {
		select {
		case ch <- data:
		default:
			// Subscriber too slow, drop message
		}
	}
}

// SubscriberCount returns the number of active subscribers for a device.
func (b *Broadcaster) SubscriberCount(deviceID string) int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.clients[deviceID])
}
