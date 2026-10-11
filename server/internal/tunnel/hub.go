// Package tunnel brokers interactive remote sessions between a browser and an
// agent that dialed out to the server.
//
// The agent never listens; it keeps one outbound control link to the server and
// dials loopback only. The server therefore cannot name a host or port — it
// asks for a session and the agent decides where to connect, which keeps a
// compromised server from pivoting into a device's LAN.
//
// The data plane is deliberately not multiplexed: each session is its own
// byte pipe. With at most a handful of concurrent sessions per device that is
// simpler to reason about than a stream multiplexer, and it keeps the control
// link free of payload.
package tunnel

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"sync"
	"time"
)

// InboundSessionTTL bounds how long the browser waits for the agent to attach
// the data connection after a session is requested.
const InboundSessionTTL = 20 * time.Second

var (
	// ErrNoAgent means the device has no live control link.
	ErrNoAgent = errors.New("device is not connected")
	// ErrUnknownTicket means the ticket is unknown, used, or expired.
	ErrUnknownTicket = errors.New("unknown or expired session ticket")
)

// AgentMessage is a control message the server pushes to an agent.
type AgentMessage struct {
	Type   string `json:"type"`
	Ticket string `json:"ticket,omitempty"`
	User   string `json:"user,omitempty"`
	Idle   int    `json:"idle_timeout_seconds,omitempty"`
	Cols   int    `json:"cols,omitempty"`
	Rows   int    `json:"rows,omitempty"`
}

// AgentLink is one device's outbound control connection. It is safe for the
// server to call Send from any goroutine.
type AgentLink interface {
	Send(msg AgentMessage) error
	Close() error
}

// SessionConn is the raw byte pipe for one session, adapted from the agent's
// data WebSocket.
type SessionConn interface {
	io.ReadWriteCloser
}

type pending struct {
	deviceID string
	user     string
	expires  time.Time
	ready    chan SessionConn
}

// Hub tracks live agent control links and pairs a browser's session request
// with the byte pipe the agent opens in response.
type Hub struct {
	mu      sync.Mutex
	agents  map[string]AgentLink
	pending map[string]*pending
}

// NewHub returns an empty hub.
func NewHub() *Hub {
	return &Hub{
		agents:  make(map[string]AgentLink),
		pending: make(map[string]*pending),
	}
}

// RegisterAgent replaces any previous control link for the device, closing the
// stale one. A reconnect must never leave two links that could both be asked to
// open a session.
func (h *Hub) RegisterAgent(deviceID string, link AgentLink) {
	h.mu.Lock()
	old := h.agents[deviceID]
	h.agents[deviceID] = link
	h.mu.Unlock()
	if old != nil && old != link {
		_ = old.Close()
	}
}

// UnregisterAgent drops the control link only if it is still the current one,
// so a slow teardown of a replaced connection cannot evict its successor.
func (h *Hub) UnregisterAgent(deviceID string, link AgentLink) {
	h.mu.Lock()
	if h.agents[deviceID] == link {
		delete(h.agents, deviceID)
	}
	h.mu.Unlock()
}

// AgentConnected reports whether the device has a live control link.
func (h *Hub) AgentConnected(deviceID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.agents[deviceID] != nil
}

// RequestSession asks a connected agent to open a session and returns the
// one-time ticket the agent will present when it attaches the data pipe. The
// initial terminal size travels with the request so the agent's PTY starts at
// the browser's dimensions.
func (h *Hub) RequestSession(deviceID, user string, idleSeconds, cols, rows int) (string, error) {
	h.mu.Lock()
	link := h.agents[deviceID]
	if link == nil {
		h.mu.Unlock()
		return "", ErrNoAgent
	}
	now := time.Now()
	for ticket, p := range h.pending {
		if now.After(p.expires) {
			delete(h.pending, ticket)
		}
	}
	ticket, err := newTicket()
	if err != nil {
		h.mu.Unlock()
		return "", err
	}
	h.pending[ticket] = &pending{
		deviceID: deviceID,
		user:     user,
		expires:  now.Add(InboundSessionTTL),
		ready:    make(chan SessionConn, 1),
	}
	h.mu.Unlock()

	// Send outside the lock: an agent link's writer may block on a slow socket.
	if err := link.Send(AgentMessage{Type: "open", Ticket: ticket, User: user, Idle: idleSeconds, Cols: cols, Rows: rows}); err != nil {
		h.consume(ticket)
		return "", err
	}
	return ticket, nil
}

// ResizeSession forwards a terminal resize to the agent's control link, which
// applies it to the matching session's PTY. Best-effort: a device that
// disconnected simply drops it.
func (h *Hub) ResizeSession(deviceID, ticket string, cols, rows int) error {
	h.mu.Lock()
	link := h.agents[deviceID]
	h.mu.Unlock()
	if link == nil {
		return ErrNoAgent
	}
	return link.Send(AgentMessage{Type: "resize", Ticket: ticket, Cols: cols, Rows: rows})
}

// PendingUser returns the account a pending ticket targets without consuming
// it, so the server can record the audit row once the session connects.
func (h *Hub) PendingUser(ticket, deviceID string) (string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	p, ok := h.pending[ticket]
	if !ok || p.deviceID != deviceID || time.Now().After(p.expires) {
		return "", false
	}
	return p.user, true
}

// AwaitSession blocks until the agent attaches the data pipe for the ticket, or
// the context is done. It only returns a pipe whose ticket was issued for
// deviceID, so a ticket cannot be replayed against another device.
func (h *Hub) AwaitSession(ctx context.Context, ticket, deviceID string) (SessionConn, error) {
	h.mu.Lock()
	p, ok := h.pending[ticket]
	h.mu.Unlock()
	if !ok || p.deviceID != deviceID {
		return nil, ErrUnknownTicket
	}
	select {
	case conn := <-p.ready:
		h.consume(ticket)
		if conn == nil {
			return nil, ErrUnknownTicket
		}
		return conn, nil
	case <-time.After(time.Until(p.expires)):
		h.consume(ticket)
		return nil, ErrUnknownTicket
	case <-ctx.Done():
		h.consume(ticket)
		return nil, ctx.Err()
	}
}

// DeliverSession hands the agent's data pipe to the waiting browser. It returns
// false if the ticket is unknown or already used, in which case the caller must
// close conn.
func (h *Hub) DeliverSession(ticket string, deviceID string, conn SessionConn) bool {
	h.mu.Lock()
	p, ok := h.pending[ticket]
	if !ok || p.deviceID != deviceID || time.Now().After(p.expires) {
		h.mu.Unlock()
		return false
	}
	h.mu.Unlock()
	select {
	case p.ready <- conn:
		return true
	default:
		return false
	}
}

func (h *Hub) consume(ticket string) {
	h.mu.Lock()
	delete(h.pending, ticket)
	h.mu.Unlock()
}

func newTicket() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
