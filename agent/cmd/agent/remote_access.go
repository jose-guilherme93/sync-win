package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
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

	"sync-win/agent/internal/remote"
)

// tunnelMessage is the control frame the server pushes over the agent's
// outbound tunnel. It is intentionally tiny: the server never names a host or
// port, so the agent decides where to connect.
type tunnelMessage struct {
	Type   string `json:"type"`
	Ticket string `json:"ticket"`
	User   string `json:"user"`
	Idle   int    `json:"idle_timeout_seconds"`
	Cols   int    `json:"cols"`
	Rows   int    `json:"rows"`
}

// ptySession is one live SSH session's local pseudo-terminal. The terminal size
// the browser reports is applied here, so the remote sshd gets a real window
// size: with a piped (pty-less) connection ssh allocates a 0x0 pty and
// full-screen tools like btop fail with "Failed to get size of terminal".
type ptySession struct {
	mu     sync.Mutex
	f      *os.File
	cancel context.CancelFunc
}

func (s *ptySession) resize(cols, rows int) {
	if cols <= 0 || rows <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f != nil {
		_ = pty.Setsize(s.f, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
	}
}

// kill ends the session's ssh process and closes its pty. It is what stops a
// closed browser session from leaving an orphaned `ssh -tt` that holds a
// session slot forever.
func (s *ptySession) kill() {
	s.mu.Lock()
	cancel, f := s.cancel, s.f
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if f != nil {
		_ = f.Close()
	}
}

// activeSessions maps a session ticket to its live PTY so a resize control
// message can find the session it belongs to.
var activeSessions sync.Map

// runRemoteTunnel keeps an outbound control link to the server for interactive
// access. It reconnects with capped exponential backoff and refuses sessions
// the local policy disables.
func runRemoteTunnel(ctx context.Context, serverURL, deviceID, deviceToken string) {
	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		conn, resp, err := websocket.Dial(ctx, tunnelURL(serverURL, deviceID, deviceToken), nil)
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			time.Sleep(backoff)
			backoff *= 2
			if backoff > 60*time.Second {
				backoff = 60 * time.Second
			}
			continue
		}
		backoff = time.Second
		log.Printf("remote tunnel connected")
		readTunnel(ctx, conn, serverURL, deviceID, deviceToken)
		_ = conn.Close(websocket.StatusNormalClosure, "")
		if ctx.Err() != nil {
			return
		}
		log.Printf("remote tunnel disconnected, reconnecting")
		time.Sleep(backoff)
	}
}

func readTunnel(ctx context.Context, conn *websocket.Conn, serverURL, deviceID, deviceToken string) {
	// A fresh control link means sessions from the previous one can no longer be
	// reached; end them so their ssh processes do not hold every session slot.
	activeSessions.Range(func(_, v any) bool {
		v.(*ptySession).kill()
		return true
	})
	sem := make(chan struct{}, syncwinContract.RemoteMaxSessions())
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		var msg tunnelMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		switch msg.Type {
		case "resize":
			if v, ok := activeSessions.Load(msg.Ticket); ok {
				v.(*ptySession).resize(msg.Cols, msg.Rows)
			}
		case "open":
			select {
			case sem <- struct{}{}:
				go func(m tunnelMessage) {
					defer func() { <-sem }()
					if err := runRemoteSession(ctx, serverURL, deviceID, deviceToken, m); err != nil {
						log.Printf("remote session ended: %v", err)
					}
				}(msg)
			default:
				log.Printf("remote session refused: %d sessions already active", cap(sem))
			}
		}
	}
}

// runRemoteSession performs one login: it injects an ephemeral key through the
// root helper, dials the local sshd, and pumps the terminal.
func runRemoteSession(ctx context.Context, serverURL, deviceID, deviceToken string, msg tunnelMessage) error {
	policy := loadLocalPolicy()
	if !remoteAccessAllowed(policy) {
		return fmt.Errorf("disabled by local policy")
	}
	user := strings.TrimSpace(msg.User)
	if user == "" {
		user = strings.TrimSpace(policy.SSHUser)
	}
	if user == "" {
		return fmt.Errorf("no target user configured (run: sync-win-agent set ssh-user <name>)")
	}
	host := strings.TrimSpace(syncwinContract.RemoteAccess.SSHHost)
	if host == "" {
		host = "127.0.0.1"
	}
	port := syncwinContract.RemoteAccess.SSHPort
	if port <= 0 {
		port = 22
	}

	keyDir, err := os.MkdirTemp("", "syncwin-remote-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(keyDir) }()
	keyPath := filepath.Join(keyDir, "id")

	if out, keyErr := exec.CommandContext(ctx, "ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", keyPath).CombinedOutput(); keyErr != nil {
		return fmt.Errorf("ssh-keygen: %v: %s", keyErr, strings.TrimSpace(string(out)))
	}
	pubBytes, readErr := os.ReadFile(keyPath + ".pub")
	if readErr != nil {
		return readErr
	}

	id := newSessionID()
	socket := strings.TrimSpace(syncwinContract.RemoteAccess.HelperSocketPath)
	if socket == "" {
		socket = "/run/sync-win/helper.sock"
	}
	if injectErr := remote.Inject(socket, user, strings.TrimSpace(string(pubBytes)), id); injectErr != nil {
		return fmt.Errorf("key injection failed: %w", injectErr)
	}
	defer func() {
		if revokeErr := remote.Revoke(socket, user, id); revokeErr != nil {
			log.Printf("remote key revoke failed: %v", revokeErr)
		}
	}()

	conn, resp, err := websocket.Dial(ctx, sessionURL(serverURL, deviceID, deviceToken, msg.Ticket), nil)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		return fmt.Errorf("session attach failed: %w", err)
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

	idle := time.Duration(msg.Idle) * time.Second
	if idle <= 0 {
		idle = time.Duration(syncwinContract.RemoteIdleSeconds()) * time.Second
	}
	ps := &ptySession{}
	activeSessions.Store(msg.Ticket, ps)
	defer activeSessions.Delete(msg.Ticket)
	return pumpSSH(ctx, conn, keyPath, user, host, port, idle, msg.Cols, msg.Rows, ps)
}

// pumpSSH runs `ssh -tt` on a local pseudo-terminal and pipes it to the session
// socket. The pty is what carries a real window size to the remote sshd.
func pumpSSH(ctx context.Context, conn *websocket.Conn, keyPath, user, host string, port int, idle time.Duration, cols, rows int, ps *ptySession) error {
	sessionCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	lastActivity := time.Now().UnixNano()
	touch := func() { atomic.StoreInt64(&lastActivity, time.Now().UnixNano()) }
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-sessionCtx.Done():
				return
			case <-ticker.C:
				if time.Since(time.Unix(0, atomic.LoadInt64(&lastActivity))) > idle {
					log.Printf("remote session idle timeout after %s", idle)
					cancel()
					return
				}
			}
		}
	}()

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
		user + "@" + host,
	}
	cmd := exec.CommandContext(sessionCtx, "ssh", args...)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	if cols <= 0 {
		cols = 120
	}
	if rows <= 0 {
		rows = 30
	}
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
	if err != nil {
		return err
	}
	ps.mu.Lock()
	ps.f = f
	ps.cancel = cancel
	ps.mu.Unlock()
	defer func() { _ = f.Close() }()

	// socket -> pty. When the socket closes the session is over, so cancel the
	// context to kill ssh: a `-tt` remote shell does not exit on stdin EOF, and
	// an orphaned ssh process would hold a session slot forever.
	go func() {
		defer cancel()
		for {
			_, data, err := conn.Read(sessionCtx)
			if err != nil {
				return
			}
			touch()
			if _, err := f.Write(data); err != nil {
				return
			}
		}
	}()

	// pty (stdout+stderr) -> socket
	buf := make([]byte, 32*1024)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			touch()
			if writeErr := conn.Write(sessionCtx, websocket.MessageBinary, buf[:n]); writeErr != nil {
				break
			}
		}
		if err != nil {
			break
		}
	}
	_ = cmd.Wait()
	return nil
}

func newSessionID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// A random id is not security-critical (the helper scopes by user and
		// marker); fall back to a time-based value rather than failing a login.
		return fmt.Sprintf("%032x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func tunnelURL(serverURL, deviceID, deviceToken string) string {
	return websocketBase(serverURL) + "/api/devices/" + url.PathEscape(deviceID) + "/tunnel?" + url.Values{"device_token": {deviceToken}}.Encode()
}

func sessionURL(serverURL, deviceID, deviceToken, ticket string) string {
	q := url.Values{"device_token": {deviceToken}, "ticket": {ticket}}
	return websocketBase(serverURL) + "/api/devices/" + url.PathEscape(deviceID) + "/tunnel/session?" + q.Encode()
}

func websocketBase(serverURL string) string {
	base := strings.TrimRight(strings.TrimSpace(serverURL), "/")
	switch {
	case strings.HasPrefix(base, "https://"):
		return "wss://" + strings.TrimPrefix(base, "https://")
	case strings.HasPrefix(base, "http://"):
		return "ws://" + strings.TrimPrefix(base, "http://")
	default:
		return base
	}
}
