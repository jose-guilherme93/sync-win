package docker

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// Request represents a pending Docker operation queued by the UI and waiting
// for the agent to pick it up.
type Request struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	Target    string    `json:"target"`
	Payload   string    `json:"payload,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// Response represents the result of a Docker operation executed by the agent.
type Response struct {
	DeviceID  string    `json:"device_id"`
	RequestID string    `json:"request_id"`
	Status    string    `json:"status"`
	Message   string    `json:"message,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// Queue is a thread-safe in-memory request/response store for Docker operations.
// Requests are ephemeral with a configurable TTL; results are stored until consumed.
type Queue struct {
	mu           sync.RWMutex
	pending      map[string][]*Request // deviceID → pending requests
	results      map[string]*Response  // requestID → result
	owners       map[string]string     // requestID → deviceID
	ownerCreated map[string]time.Time  // requestID → enqueue time
	ttl          time.Duration
}

// NewQueue creates a new Docker request queue.
func NewQueue(ttl time.Duration) *Queue {
	return &Queue{
		pending:      make(map[string][]*Request),
		results:      make(map[string]*Response),
		owners:       make(map[string]string),
		ownerCreated: make(map[string]time.Time),
		ttl:          ttl,
	}
}

// Enqueue adds a request to the queue and returns its ID.
func (q *Queue) Enqueue(req *Request) string {
	q.mu.Lock()
	defer q.mu.Unlock()
	if req.ID == "" {
		req.ID = newRequestID()
	}
	if req.CreatedAt.IsZero() {
		req.CreatedAt = time.Now().UTC()
	}
	q.pending[req.Target] = append(q.pending[req.Target], req) // Target is deviceID for pending map
	q.owners[req.ID] = req.Target
	q.ownerCreated[req.ID] = req.CreatedAt
	return req.ID
}

// EnqueueForDevice adds a request for a specific device.
func (q *Queue) EnqueueForDevice(deviceID, reqType, target, payload string) string {
	q.mu.Lock()
	defer q.mu.Unlock()
	req := &Request{
		ID:        newRequestID(),
		Type:      reqType,
		Target:    target,
		Payload:   payload,
		CreatedAt: time.Now().UTC(),
	}
	q.pending[deviceID] = append(q.pending[deviceID], req)
	q.owners[req.ID] = deviceID
	q.ownerCreated[req.ID] = req.CreatedAt
	return req.ID
}

// DequeueAll returns and removes all pending requests for a device.
// Called by the agent at the start of each cycle.
func (q *Queue) DequeueAll(deviceID string) []*Request {
	q.mu.Lock()
	defer q.mu.Unlock()
	requests := q.pending[deviceID]
	delete(q.pending, deviceID)
	return requests
}

// StoreResult stores an agent's response for a request.
func (q *Queue) StoreResult(resp *Response) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if resp == nil || resp.RequestID == "" || q.owners[resp.RequestID] != resp.DeviceID {
		return
	}
	if resp.CreatedAt.IsZero() {
		resp.CreatedAt = time.Now().UTC()
	}
	q.results[resp.RequestID] = resp
}

// GetResult returns the result for a request, or nil if not yet available.
func (q *Queue) GetResult(deviceID, requestID string) *Response {
	q.mu.RLock()
	defer q.mu.RUnlock()
	resp := q.results[requestID]
	if resp == nil || resp.DeviceID != deviceID {
		return nil
	}
	return resp
}

// GetAndConsumeResult returns the result and removes it from the store.
func (q *Queue) GetAndConsumeResult(deviceID, requestID string) *Response {
	q.mu.Lock()
	defer q.mu.Unlock()
	resp, ok := q.results[requestID]
	if !ok || resp.DeviceID != deviceID {
		return nil
	}
	delete(q.results, requestID)
	delete(q.owners, requestID)
	delete(q.ownerCreated, requestID)
	return resp
}

// Cleanup removes expired pending requests and old results.
// Should be called periodically (e.g., every 30s via a goroutine).
func (q *Queue) Cleanup() {
	q.mu.Lock()
	defer q.mu.Unlock()
	now := time.Now()

	// Remove expired pending requests
	for deviceID, requests := range q.pending {
		var alive []*Request
		for _, req := range requests {
			if now.Sub(req.CreatedAt) < q.ttl {
				alive = append(alive, req)
			} else {
				delete(q.owners, req.ID)
				delete(q.ownerCreated, req.ID)
			}
		}
		if len(alive) == 0 {
			delete(q.pending, deviceID)
		} else {
			q.pending[deviceID] = alive
		}
	}

	// Remove old results (keep for 2x TTL after creation)
	for id, resp := range q.results {
		if now.Sub(resp.CreatedAt) > 2*q.ttl {
			delete(q.results, id)
			delete(q.owners, id)
			delete(q.ownerCreated, id)
		}
	}
	for id, created := range q.ownerCreated {
		if now.Sub(created) > 2*q.ttl {
			delete(q.owners, id)
			delete(q.ownerCreated, id)
		}
	}
}

// PendingCount returns the number of pending requests for a device (for diagnostics).
func (q *Queue) PendingCount(deviceID string) int {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return len(q.pending[deviceID])
}

func newRequestID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("secure random source failed: %v", err))
	}
	return "dreq-" + hex.EncodeToString(b)
}
