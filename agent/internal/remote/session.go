package remote

// Interactive terminals are hosted here, in the root helper, and not in the
// agent.
//
// The agent is the process that gets replaced: every update restarts it, and a
// terminal owned by it dies with it -- which is how an operator running an
// install from the dashboard lost the only way into a machine. The helper is a
// separate root service that an agent restart never touches, so a session it
// hosts outlives the agent, its upgrade, and its rollback.
//
// The helper also decides where the session attaches. It reads the server URL
// from root-owned configuration, so the unprivileged agent cannot point the root
// helper at another host; the request only carries the ticket, the target
// account, the size and the device credentials the query needs.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/creack/pty"
)

// SessionSpec is one interactive session the helper should host.
type SessionSpec struct {
	// Ticket is the one-time server ticket, and the session's identity for
	// resize and for the authorized_keys marker.
	Ticket   string
	User     string
	Idle     time.Duration
	Cols     int
	Rows     int
	DeviceID string
	// Token is the device token the attach URL carries. It authenticates the
	// device, not the operator, and never chooses a destination.
	Token string
}

// SessionHost runs the interactive terminals of one device.
type SessionHost struct {
	MaxSessions int
	Idle        time.Duration
	SSHHost     string
	SSHPort     int
	// ServerURL resolves the attach destination from root-owned configuration.
	// It is a function so a unit rewrite is picked up without a helper restart.
	ServerURL func() (string, error)
	// Inject and Revoke install and remove the ephemeral key. Required.
	Inject func(user, key, id string) error
	Revoke func(user, id string) error

	// keyFn, dialFn and startFn are seams for tests.
	keyFn   func(ctx context.Context, keyPath string) error
	dialFn  func(ctx context.Context, rawURL string) (sessionConn, error)
	startFn func(ctx context.Context, spec SessionSpec, keyPath string) (sessionIO, error)

	mu       sync.Mutex
	sessions map[string]*hostedSession
	closed   bool
}

// sessionConn is the attach socket in the shape the pumps need. It is a seam so
// the session lifecycle can be tested without a server.
type sessionConn interface {
	Read(ctx context.Context) ([]byte, error)
	Write(ctx context.Context, data []byte) error
	Close() error
}

// sessionIO is the local ssh client's terminal.
type sessionIO interface {
	io.ReadWriteCloser
	Resize(cols, rows int) error
}

type hostedSession struct {
	ticket   string
	conn     sessionConn
	term     sessionIO
	cancel   context.CancelFunc
	activity int64

	closeOnce sync.Once
	onEnd     func()
}

func (s *hostedSession) touch() {
	atomic.StoreInt64(&s.activity, time.Now().UnixNano())
}

func (s *hostedSession) end() {
	s.closeOnce.Do(func() {
		// A hosted terminal is long-lived; its end is worth a line in the journal
		// because a session that never ends holds an ssh process and a key.
		log.Printf("remote session %s closed", shortTicket(s.ticket))
		if s.cancel != nil {
			s.cancel()
		}
		if s.conn != nil {
			_ = s.conn.Close()
		}
		if s.term != nil {
			_ = s.term.Close()
		}
		if s.onEnd != nil {
			s.onEnd()
		}
	})
}

// validate checks a spec before anything touches a file or the network.
func (s SessionSpec) validate() error {
	if !idPattern.MatchString(s.Ticket) {
		return fmt.Errorf("invalid session ticket")
	}
	if !userPattern.MatchString(s.User) {
		return fmt.Errorf("invalid target user")
	}
	if !devicePattern.MatchString(s.DeviceID) {
		return fmt.Errorf("invalid device id")
	}
	if strings.TrimSpace(s.Token) == "" || len(s.Token) > 512 {
		return fmt.Errorf("invalid device token")
	}
	if s.Cols < 0 || s.Rows < 0 || s.Cols > 4096 || s.Rows > 4096 {
		return fmt.Errorf("invalid terminal size")
	}
	return nil
}

// sessionURL builds the attach URL for a ticket on the configured server.
//
// The host comes from the caller, which is root-owned configuration; everything
// that varies is escaped, so neither the device id nor the ticket can change the
// destination or add a query parameter of its own.
func sessionURL(serverURL, deviceID, token, ticket string) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(serverURL), "/")
	if base == "" {
		return "", errors.New("server URL is empty")
	}
	parsed, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("invalid server URL: %w", err)
	}
	switch parsed.Scheme {
	case "https", "wss":
		parsed.Scheme = "wss"
	case "http", "ws":
		parsed.Scheme = "ws"
	default:
		return "", fmt.Errorf("unsupported server URL scheme %q", parsed.Scheme)
	}
	if parsed.Host == "" {
		return "", errors.New("server URL has no host")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/api/devices/" + url.PathEscape(deviceID) + "/tunnel/session"
	query := url.Values{"device_token": {token}, "ticket": {ticket}}
	parsed.RawQuery = query.Encode()
	parsed.Fragment = ""
	return parsed.String(), nil
}

// Open starts a hosted session and returns once it is attached. The terminal
// then keeps running after the return: that is the point of hosting it here.
func (h *SessionHost) Open(spec SessionSpec) error {
	if err := spec.validate(); err != nil {
		return err
	}
	if h.Inject == nil || h.Revoke == nil {
		return errors.New("session host has no key injector")
	}
	destination, err := h.ServerURL()
	if err != nil {
		return err
	}
	attachURL, err := sessionURL(destination, spec.DeviceID, spec.Token, spec.Ticket)
	if err != nil {
		return err
	}

	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return errors.New("helper is shutting down")
	}
	max := h.MaxSessions
	if max <= 0 {
		max = 3
	}
	if len(h.sessions) >= max {
		h.mu.Unlock()
		return fmt.Errorf("%d sessions already active", max)
	}
	if _, exists := h.sessions[spec.Ticket]; exists {
		h.mu.Unlock()
		return errors.New("session is already open")
	}
	session := &hostedSession{ticket: spec.Ticket}
	session.touch()
	// The slot is taken before the slow work, so two opens racing cannot both
	// pass the limit check.
	if h.sessions == nil {
		h.sessions = make(map[string]*hostedSession)
	}
	h.sessions[spec.Ticket] = session
	h.mu.Unlock()

	if err := h.start(session, spec, attachURL); err != nil {
		h.release(session)
		return err
	}
	log.Printf("remote session %s opened for %s", shortTicket(spec.Ticket), spec.User)
	return nil
}

// shortTicket is the first eight characters of a session ticket, enough to
// correlate the journal lines of one session.
func shortTicket(ticket string) string {
	if len(ticket) <= 8 {
		return ticket
	}
	return ticket[:8]
}

func (h *SessionHost) start(session *hostedSession, spec SessionSpec, attachURL string) error {
	keyDir, err := os.MkdirTemp("", "syncwin-remote-")
	if err != nil {
		return err
	}
	keyPath := filepath.Join(keyDir, "id")

	ctx, cancel := context.WithCancel(context.Background())
	session.cancel = cancel
	// Any failure before the pumps start must release the context and the
	// socket; the map slot is released by the caller.
	started := false
	defer func() {
		if !started {
			cancel()
		}
	}()

	generate := h.keyFn
	if generate == nil {
		generate = generateKey
	}
	if keyErr := generate(ctx, keyPath); keyErr != nil {
		_ = os.RemoveAll(keyDir)
		return keyErr
	}
	pub, err := os.ReadFile(keyPath + ".pub")
	if err != nil {
		_ = os.RemoveAll(keyDir)
		return err
	}
	if injectErr := h.Inject(spec.User, strings.TrimSpace(string(pub)), spec.Ticket); injectErr != nil {
		_ = os.RemoveAll(keyDir)
		return fmt.Errorf("key injection failed: %w", injectErr)
	}

	conn, err := h.dialSession(ctx, attachURL)
	if err != nil {
		_ = h.Revoke(spec.User, spec.Ticket)
		_ = os.RemoveAll(keyDir)
		return fmt.Errorf("session attach failed: %w", err)
	}
	session.conn = conn

	term, err := h.startProcess(ctx, spec, keyPath)
	if err != nil {
		_ = conn.Close()
		_ = h.Revoke(spec.User, spec.Ticket)
		_ = os.RemoveAll(keyDir)
		return err
	}
	session.term = term

	session.onEnd = func() {
		// A failed revoke is not fatal: the key carries its own expiry-time, so
		// it dies on its own rather than living forever.
		_ = h.Revoke(spec.User, spec.Ticket)
		_ = os.RemoveAll(keyDir)
		h.release(session)
	}
	started = true
	go h.pump(ctx, session)
	return nil
}

// pump relays the terminal until either side closes or the operator is idle.
func (h *SessionHost) pump(ctx context.Context, session *hostedSession) {
	defer session.end()

	idle := h.Idle
	if idle <= 0 {
		idle = 15 * time.Minute
	}
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if time.Since(time.Unix(0, atomic.LoadInt64(&session.activity))) > idle {
					session.end()
					return
				}
			}
		}
	}()

	// socket -> terminal. When the socket closes the session is over, because a
	// `-tt` shell does not exit on stdin EOF on its own.
	go func() {
		for {
			data, err := session.conn.Read(ctx)
			if err != nil {
				session.end()
				return
			}
			session.touch()
			if _, err := session.term.Write(data); err != nil {
				session.end()
				return
			}
		}
	}()

	buf := make([]byte, 32*1024)
	for {
		n, err := session.term.Read(buf)
		if n > 0 {
			session.touch()
			if writeErr := session.conn.Write(ctx, buf[:n]); writeErr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

// Resize applies the browser's terminal size to a live session.
func (h *SessionHost) Resize(ticket string, cols, rows int) error {
	if cols <= 0 || rows <= 0 {
		return nil
	}
	h.mu.Lock()
	session := h.sessions[ticket]
	h.mu.Unlock()
	if session == nil {
		return errors.New("no such session")
	}
	return session.term.Resize(cols, rows)
}

// Count reports how many terminals this device is carrying.
func (h *SessionHost) Count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.sessions)
}

// CloseAll ends every session. It runs when the helper is stopping, which is
// the only time a hosted terminal has to die with the helper.
func (h *SessionHost) CloseAll() {
	h.mu.Lock()
	h.closed = true
	sessions := make([]*hostedSession, 0, len(h.sessions))
	for _, s := range h.sessions {
		sessions = append(sessions, s)
	}
	h.mu.Unlock()
	for _, s := range sessions {
		s.end()
	}
}

func (h *SessionHost) release(session *hostedSession) {
	h.mu.Lock()
	if current, ok := h.sessions[session.ticket]; ok && current == session {
		delete(h.sessions, session.ticket)
	}
	h.mu.Unlock()
}

func (h *SessionHost) dialSession(ctx context.Context, rawURL string) (sessionConn, error) {
	dial := h.dialFn
	if dial == nil {
		dial = dialWebSocket
	}
	return dial(ctx, rawURL)
}

func (h *SessionHost) startProcess(ctx context.Context, spec SessionSpec, keyPath string) (sessionIO, error) {
	if h.startFn != nil {
		return h.startFn(ctx, spec, keyPath)
	}
	host := strings.TrimSpace(h.SSHHost)
	if host == "" {
		host = "127.0.0.1"
	}
	port := h.SSHPort
	if port <= 0 {
		port = 22
	}
	return defaultStartProcess(ctx, spec, keyPath, host, port)
}

// generateKey writes an ephemeral ed25519 pair at keyPath and keyPath+".pub".
func generateKey(ctx context.Context, keyPath string) error {
	out, err := exec.CommandContext(ctx, "ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", keyPath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("ssh-keygen: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func dialWebSocket(ctx context.Context, rawURL string) (sessionConn, error) {
	conn, resp, err := websocket.Dial(ctx, rawURL, nil)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		return nil, err
	}
	return &webSocketConn{conn: conn}, nil
}

type webSocketConn struct {
	conn *websocket.Conn
}

func (c *webSocketConn) Read(ctx context.Context) ([]byte, error) {
	_, data, err := c.conn.Read(ctx)
	return data, err
}

func (c *webSocketConn) Write(ctx context.Context, data []byte) error {
	return c.conn.Write(ctx, websocket.MessageBinary, data)
}

func (c *webSocketConn) Close() error {
	return c.conn.Close(websocket.StatusNormalClosure, "")
}

func defaultStartProcess(ctx context.Context, spec SessionSpec, keyPath, host string, port int) (sessionIO, error) {
	args := []string{
		"-tt",
		"-i", keyPath,
		"-o", "IdentitiesOnly=yes",
		"-o", "PreferredAuthentications=publickey",
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "LogLevel=ERROR",
		"-p", strconv.Itoa(port),
		spec.User + "@" + host,
	}
	cmd := exec.CommandContext(ctx, "ssh", args...)
	cmd.Env = sshEnv(spec.Ticket)
	cols, rows := spec.Cols, spec.Rows
	if cols <= 0 {
		cols = 120
	}
	if rows <= 0 {
		rows = 30
	}
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
	if err != nil {
		return nil, err
	}
	return &ptyTerm{file: f, cmd: cmd}, nil
}

type ptyTerm struct {
	file *os.File
	cmd  *exec.Cmd
}

func (p *ptyTerm) Read(b []byte) (int, error)  { return p.file.Read(b) }
func (p *ptyTerm) Write(b []byte) (int, error) { return p.file.Write(b) }

func (p *ptyTerm) Close() error {
	if p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
	err := p.file.Close()
	_ = p.cmd.Wait()
	return err
}

func (p *ptyTerm) Resize(cols, rows int) error {
	if p.file == nil {
		return errors.New("terminal is closed")
	}
	return pty.Setsize(p.file, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
}

// sshEnv is the environment of the local ssh client.
//
// SYNCWIN_REMOTE_SESSION marks the shell as belonging to a SyncWin terminal.
// The installer and the agent updater read it and move themselves behind PID 1
// before they restart the agent, because that restart is what ends this session.
func sshEnv(ticket string) []string {
	env := append(os.Environ(), "TERM=xterm-256color")
	if tag := strings.TrimSpace(ticket); tag != "" {
		env = append(env, "SYNCWIN_REMOTE_SESSION="+tag)
	}
	return env
}
