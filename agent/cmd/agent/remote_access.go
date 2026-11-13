package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"

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
		readTunnel(ctx, conn, deviceID, deviceToken)
		_ = conn.Close(websocket.StatusNormalClosure, "")
		if ctx.Err() != nil {
			return
		}
		log.Printf("remote tunnel disconnected, reconnecting")
		time.Sleep(backoff)
	}
}

// readTunnel handles the control messages. The terminal itself is not started
// here: it is hosted by the root helper, in a service an agent update never
// restarts, so a session survives the agent being replaced under it.
func readTunnel(ctx context.Context, conn *websocket.Conn, deviceID, deviceToken string) {
	// The live-session limit lives in the helper, which owns the terminals. This
	// only bounds how many attaches the agent starts at once, so a burst of
	// "open" frames cannot spawn an unbounded number of goroutines.
	opening := make(chan struct{}, syncwinContract.RemoteMaxSessions())
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
			if err := remote.ResizeSession(helperSocket(), msg.Ticket, msg.Cols, msg.Rows); err != nil {
				log.Printf("remote resize dropped: %v", err)
			}
		case "open":
			select {
			case opening <- struct{}{}:
				go func(m tunnelMessage) {
					defer func() { <-opening }()
					if err := runRemoteSession(deviceID, deviceToken, m); err != nil {
						log.Printf("remote session failed: %v", err)
					}
				}(msg)
			default:
				log.Printf("remote session refused: too many attaches in flight")
			}
		}
	}
}

// runRemoteSession asks the helper to host one login.
//
// The agent keeps what only it knows: the local policy, and which account the
// session targets. It never opens a pseudo-terminal or a socket of its own,
// because those would die with it.
func runRemoteSession(deviceID, deviceToken string, msg tunnelMessage) error {
	policy := loadLocalPolicy()
	if !remoteAccessAllowed(policy) {
		return fmt.Errorf("disabled by local policy")
	}
	if strings.TrimSpace(msg.Ticket) == "" {
		return errors.New("session ticket is missing")
	}
	user := strings.TrimSpace(msg.User)
	if user == "" {
		user = strings.TrimSpace(policy.SSHUser)
	}
	if user == "" {
		return fmt.Errorf("no target user configured (run: sync-win-agent set ssh-user <name>)")
	}

	idle := time.Duration(msg.Idle) * time.Second
	if idle <= 0 {
		idle = time.Duration(syncwinContract.RemoteIdleSeconds()) * time.Second
	}
	spec := remote.SessionSpec{
		Ticket:   msg.Ticket,
		User:     user,
		Idle:     idle,
		Cols:     msg.Cols,
		Rows:     msg.Rows,
		DeviceID: deviceID,
		Token:    deviceToken,
	}
	if err := remote.OpenSession(helperSocket(), spec); err != nil {
		return err
	}
	log.Printf("remote session opened for %s (hosted by the helper)", user)
	return nil
}

// helperSocket is the root helper's Unix socket, the only channel the agent has
// to the privileged half of remote access.
func helperSocket() string {
	socket := strings.TrimSpace(syncwinContract.RemoteAccess.HelperSocketPath)
	if socket == "" {
		return "/run/sync-win/helper.sock"
	}
	return socket
}

// activeSessionCount reports how many terminals the helper is carrying. The
// updater asks before it restarts the agent: each of those terminals is an
// operator, and on a machine reached only this way it is their only way in.
func activeSessionCount() int {
	count, err := remote.SessionCount(helperSocket())
	if err != nil {
		return 0
	}
	return count
}

func tunnelURL(serverURL, deviceID, deviceToken string) string {
	return websocketBase(serverURL) + "/api/devices/" + url.PathEscape(deviceID) + "/tunnel?" + url.Values{"device_token": {deviceToken}}.Encode()
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
