package tunnel

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

type fakeLink struct {
	mu     sync.Mutex
	sent   []AgentMessage
	closed bool
}

func (f *fakeLink) Send(m AgentMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, m)
	return nil
}

func (f *fakeLink) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

func (f *fakeLink) messages() []AgentMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]AgentMessage(nil), f.sent...)
}

func TestRequestSessionWithoutAgentFails(t *testing.T) {
	h := NewHub()
	if _, err := h.RequestSession("dev-1", "alice", 900, 0, 0); !errors.Is(err, ErrNoAgent) {
		t.Fatalf("want ErrNoAgent, got %v", err)
	}
}

func TestRequestSessionSendsOpen(t *testing.T) {
	h := NewHub()
	link := &fakeLink{}
	h.RegisterAgent("dev-1", link)

	ticket, err := h.RequestSession("dev-1", "alice", 900, 0, 0)
	if err != nil {
		t.Fatalf("RequestSession: %v", err)
	}
	if ticket == "" {
		t.Fatal("empty ticket")
	}
	msgs := link.messages()
	if len(msgs) != 1 {
		t.Fatalf("want 1 message, got %d", len(msgs))
	}
	got := msgs[0]
	if got.Type != "open" || got.Ticket != ticket || got.User != "alice" || got.Idle != 900 {
		t.Fatalf("unexpected open message: %+v", got)
	}
}

func TestAwaitSessionWrongDevice(t *testing.T) {
	h := NewHub()
	h.RegisterAgent("dev-1", &fakeLink{})
	ticket, err := h.RequestSession("dev-1", "", 900, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := h.AwaitSession(ctx, ticket, "dev-2"); !errors.Is(err, ErrUnknownTicket) {
		t.Fatalf("want ErrUnknownTicket for wrong device, got %v", err)
	}
}

func TestSessionPairing(t *testing.T) {
	h := NewHub()
	link := &fakeLink{}
	h.RegisterAgent("dev-1", link)

	ticket, err := h.RequestSession("dev-1", "", 900, 0, 0)
	if err != nil {
		t.Fatal(err)
	}

	serverEnd, clientEnd := net.Pipe()
	defer clientEnd.Close()
	if !h.DeliverSession(ticket, "dev-1", serverEnd) {
		t.Fatal("DeliverSession rejected a valid ticket")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn, err := h.AwaitSession(ctx, ticket, "dev-1")
	if err != nil {
		t.Fatalf("AwaitSession: %v", err)
	}
	if conn != serverEnd {
		t.Fatal("AwaitSession returned a different connection")
	}

	// The pipe must actually carry bytes.
	go func() { _, _ = clientEnd.Write([]byte("hello")) }()
	buf := make([]byte, 5)
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(buf) != "hello" {
		t.Fatalf("got %q", buf)
	}
}

func TestDeliverSessionRejectsWrongDeviceAndReuse(t *testing.T) {
	h := NewHub()
	h.RegisterAgent("dev-1", &fakeLink{})
	ticket, err := h.RequestSession("dev-1", "", 900, 0, 0)
	if err != nil {
		t.Fatal(err)
	}

	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	if h.DeliverSession(ticket, "other-device", a) {
		t.Fatal("ticket must be bound to its device")
	}
	if !h.DeliverSession(ticket, "dev-1", a) {
		t.Fatal("first delivery should succeed")
	}
	c, d := net.Pipe()
	defer c.Close()
	defer d.Close()
	if h.DeliverSession(ticket, "dev-1", c) {
		t.Fatal("second delivery for the same ticket must fail")
	}
}

func TestAwaitSessionUnknownTicket(t *testing.T) {
	h := NewHub()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := h.AwaitSession(ctx, "nope", "dev-1"); !errors.Is(err, ErrUnknownTicket) {
		t.Fatalf("want ErrUnknownTicket, got %v", err)
	}
}

func TestRegisterAgentReplacesAndClosesOldLink(t *testing.T) {
	h := NewHub()
	first := &fakeLink{}
	second := &fakeLink{}
	h.RegisterAgent("dev-1", first)
	h.RegisterAgent("dev-1", second)

	if !first.closed {
		t.Fatal("replaced link must be closed")
	}
	if second.closed {
		t.Fatal("new link must stay open")
	}
	// Unregistering the stale link must not evict the live one.
	h.UnregisterAgent("dev-1", first)
	if !h.AgentConnected("dev-1") {
		t.Fatal("stale unregister evicted the live link")
	}
	h.UnregisterAgent("dev-1", second)
	if h.AgentConnected("dev-1") {
		t.Fatal("live link should be gone after unregister")
	}
}
