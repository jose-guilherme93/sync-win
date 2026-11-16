package remote

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// The resize keeps a session alive across an agent restart, which only works if
// the destination is fixed by configuration and not by the request.
func TestSessionURLBuildsTheDeviceAttachPath(t *testing.T) {
	for _, tc := range []struct {
		base   string
		scheme string
	}{
		{"http://sync-win.local:8080", "ws"},
		{"https://sync-win.local", "wss"},
		{"ws://sync-win.local", "ws"},
		{"wss://sync-win.local/", "wss"},
	} {
		got, err := sessionURL(tc.base, "dev-1", "tok", strings.Repeat("a", 64))
		if err != nil {
			t.Fatalf("sessionURL(%q): %v", tc.base, err)
		}
		if !strings.HasPrefix(got, tc.scheme+"://sync-win.local") {
			t.Fatalf("sessionURL(%q) = %q, want scheme %s", tc.base, got, tc.scheme)
		}
		if !strings.Contains(got, "/api/devices/dev-1/tunnel/session?") {
			t.Fatalf("sessionURL(%q) = %q, want the attach path", tc.base, got)
		}
	}
}

func TestSessionURLEscapesItsInputs(t *testing.T) {
	got, err := sessionURL("http://sync-win.local", "dev/1?x=1", "tok en", strings.Repeat("b", 64))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "dev/1?x=1") {
		t.Fatalf("the device id was not escaped: %q", got)
	}
	if strings.Count(got, "?") != 1 {
		t.Fatalf("a value added a query of its own: %q", got)
	}
}

func TestSessionURLRejectsAnotherScheme(t *testing.T) {
	if _, err := sessionURL("file:///etc/passwd", "dev-1", "tok", strings.Repeat("a", 64)); err == nil {
		t.Fatal("a non-network scheme must be rejected")
	}
	if _, err := sessionURL("", "dev-1", "tok", strings.Repeat("a", 64)); err == nil {
		t.Fatal("an empty server URL must be rejected")
	}
}

func TestSessionSpecValidationRejectsHostileInput(t *testing.T) {
	valid := func() SessionSpec {
		return SessionSpec{
			Ticket:   strings.Repeat("a", 64),
			User:     "alice",
			DeviceID: "dev-1",
			Token:    "tok",
			Cols:     120,
			Rows:     30,
		}
	}
	if err := valid().validate(); err != nil {
		t.Fatalf("the baseline spec must be accepted: %v", err)
	}
	for _, tc := range []struct {
		name  string
		apply func(*SessionSpec)
	}{
		{"user with a shell", func(s *SessionSpec) { s.User = "alice; reboot" }},
		{"uppercase user", func(s *SessionSpec) { s.User = "Alice" }},
		{"newline in the ticket", func(s *SessionSpec) { s.Ticket = strings.Repeat("a", 60) + "\n--x" }},
		{"path traversal in the device", func(s *SessionSpec) { s.DeviceID = "../../etc" }},
		{"empty token", func(s *SessionSpec) { s.Token = "  " }},
		{"oversized terminal", func(s *SessionSpec) { s.Cols = 100000 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec := valid()
			tc.apply(&spec)
			if err := spec.validate(); err == nil {
				t.Fatal("the spec must be rejected")
			}
		})
	}
}

// --- fakes ------------------------------------------------------------------

type fakeConn struct {
	reads  chan []byte
	writes chan []byte
	closed chan struct{}
	once   sync.Once
}

func newFakeConn() *fakeConn {
	return &fakeConn{
		reads:  make(chan []byte, 8),
		writes: make(chan []byte, 64),
		closed: make(chan struct{}),
	}
}

func (c *fakeConn) Read(ctx context.Context) ([]byte, error) {
	select {
	case data := <-c.reads:
		return data, nil
	case <-c.closed:
		return nil, io.EOF
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (c *fakeConn) Write(ctx context.Context, data []byte) error {
	select {
	case c.writes <- data:
		return nil
	case <-c.closed:
		return io.EOF
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *fakeConn) Close() error {
	c.once.Do(func() { close(c.closed) })
	return nil
}

type fakeTerm struct {
	reads   chan []byte
	writes  chan []byte
	closed  chan struct{}
	resizes chan [2]int
	once    sync.Once
}

func newFakeTerm() *fakeTerm {
	return &fakeTerm{
		reads:   make(chan []byte, 8),
		writes:  make(chan []byte, 64),
		closed:  make(chan struct{}),
		resizes: make(chan [2]int, 8),
	}
}

func (t *fakeTerm) Read(b []byte) (int, error) {
	select {
	case data := <-t.reads:
		return copy(b, data), nil
	case <-t.closed:
		return 0, io.EOF
	}
}

func (t *fakeTerm) Write(b []byte) (int, error) {
	select {
	case t.writes <- append([]byte(nil), b...):
		return len(b), nil
	case <-t.closed:
		return 0, io.EOF
	}
}

func (t *fakeTerm) Close() error {
	t.once.Do(func() { close(t.closed) })
	return nil
}

func (t *fakeTerm) Resize(cols, rows int) error {
	select {
	case t.resizes <- [2]int{cols, rows}:
		return nil
	default:
		return errors.New("resize queue is full")
	}
}

func testHost(t *testing.T) (*SessionHost, *fakeTerm) {
	t.Helper()
	term := newFakeTerm()
	conn := newFakeConn()
	host := &SessionHost{
		MaxSessions: 2,
		Idle:        time.Minute,
		ServerURL:   func() (string, error) { return "http://sync-win.local", nil },
		Inject:      func(user, key, id string) error { return nil },
		Revoke:      func(user, id string) error { return nil },
		keyFn: func(ctx context.Context, keyPath string) error {
			if err := os.WriteFile(keyPath+".pub", []byte("ssh-ed25519 AAAA test@host\n"), 0o600); err != nil {
				return err
			}
			return os.WriteFile(keyPath, []byte("private"), 0o600)
		},
		dialFn: func(ctx context.Context, rawURL string) (sessionConn, error) { return conn, nil },
		startFn: func(ctx context.Context, spec SessionSpec, keyPath string) (sessionIO, error) {
			return term, nil
		},
	}
	t.Cleanup(host.CloseAll)
	return host, term
}

func spec() SessionSpec {
	return SessionSpec{
		Ticket:   strings.Repeat("a", 64),
		User:     "alice",
		Idle:     time.Minute,
		Cols:     120,
		Rows:     30,
		DeviceID: "dev-1",
		Token:    "tok",
	}
}

// The whole point: the session is hosted by a process that survives the agent.
func TestOpenHostsTheSessionAndCountsIt(t *testing.T) {
	host, term := testHost(t)
	if err := host.Open(spec()); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got := host.Count(); got != 1 {
		t.Fatalf("Count() = %d after opening a session, want 1", got)
	}

	if err := host.Resize(spec().Ticket, 200, 50); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	select {
	case size := <-term.resizes:
		if size != [2]int{200, 50} {
			t.Fatalf("resize = %v, want [200 50]", size)
		}
	case <-time.After(time.Second):
		t.Fatal("the resize never reached the terminal")
	}

	host.CloseAll()
	if got := host.Count(); got != 0 {
		t.Fatalf("Count() = %d after CloseAll, want 0", got)
	}
}

func TestOpenEnforcesTheSessionLimit(t *testing.T) {
	host, _ := testHost(t)
	host.MaxSessions = 1
	if err := host.Open(spec()); err != nil {
		t.Fatalf("first Open: %v", err)
	}
	second := spec()
	second.Ticket = strings.Repeat("b", 64)
	if err := host.Open(second); err == nil {
		t.Fatal("a second session past the limit must be refused")
	}
}

func TestOpenReleasesTheSlotWhenAttachingFails(t *testing.T) {
	host, _ := testHost(t)
	host.dialFn = func(ctx context.Context, rawURL string) (sessionConn, error) {
		return nil, errors.New("connection refused")
	}
	if err := host.Open(spec()); err == nil {
		t.Fatal("a failed attach must be reported")
	}
	if got := host.Count(); got != 0 {
		t.Fatalf("Count() = %d after a failed attach, want 0", got)
	}
}

func TestOpenRejectsDuplicateTickets(t *testing.T) {
	host, _ := testHost(t)
	if err := host.Open(spec()); err != nil {
		t.Fatal(err)
	}
	if err := host.Open(spec()); err == nil {
		t.Fatal("the same ticket must not be hosted twice")
	}
}

func TestOpenNeedsAKeyInjector(t *testing.T) {
	host, _ := testHost(t)
	host.Inject = nil
	if err := host.Open(spec()); err == nil {
		t.Fatal("a host without an injector must refuse the session")
	}
}

func TestResizeUnknownSessionIsAnError(t *testing.T) {
	host, _ := testHost(t)
	if err := host.Resize(strings.Repeat("c", 64), 80, 24); err == nil {
		t.Fatal("resizing a session that does not exist must be reported")
	}
}

// The shell inside the session must carry the marker the installer reads.
func TestSSHEnvMarksRemoteSession(t *testing.T) {
	env := sshEnv("ticket-abc")
	var marker bool
	for _, entry := range env {
		if entry == "SYNCWIN_REMOTE_SESSION=ticket-abc" {
			marker = true
		}
	}
	if !marker {
		t.Fatalf("the session marker is missing from the ssh environment")
	}
	for _, entry := range sshEnv("   ") {
		if strings.HasPrefix(entry, "SYNCWIN_REMOTE_SESSION=") {
			t.Fatalf("a blank ticket produced the marker %q", entry)
		}
	}
}
