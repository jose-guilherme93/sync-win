// Package remote implements the privileged half of interactive access: a root
// helper that injects a short-lived public key into a target user's
// authorized_keys, and the client the unprivileged agent uses to ask for it.
//
// The agent service runs with NoNewPrivileges=yes and cannot escalate on its
// own. It talks to the helper over a root-owned Unix socket and the helper only
// performs one narrowly-scoped job per request: append or remove a single,
// fully-formed, restricted authorized_keys line for one named user.
package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
	"time"
)

// Action is a helper operation.
type Action string

const (
	ActionInject Action = "inject"
	ActionRevoke Action = "revoke"
	// ActionSessionOpen hosts a terminal in the helper. A session lives here
	// because the agent is the process that gets replaced on every update.
	ActionSessionOpen   Action = "session-open"
	ActionSessionResize Action = "session-resize"
	ActionSessionCount  Action = "session-count"
)

// Request is one helper call.
type Request struct {
	Action Action `json:"action"`
	User   string `json:"user"`
	Key    string `json:"key,omitempty"`
	ID     string `json:"id"`

	// Session fields. Ticket is both the session's identity and the
	// authorized_keys marker; Device and Token only build the attach URL.
	Ticket string `json:"ticket,omitempty"`
	Device string `json:"device_id,omitempty"`
	Token  string `json:"device_token,omitempty"`
	Cols   int    `json:"cols,omitempty"`
	Rows   int    `json:"rows,omitempty"`
	Idle   int    `json:"idle_timeout_seconds,omitempty"`
}

// Response is the helper's reply.
type Response struct {
	OK       bool   `json:"ok"`
	Error    string `json:"error,omitempty"`
	Sessions int    `json:"sessions,omitempty"`
}

var (
	userPattern   = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)
	idPattern     = regexp.MustCompile(`^[0-9a-f]{16,64}$`)
	devicePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)
)

// ValidateReport reports why a request is unacceptable, or nil.
func (r Request) Validate() error {
	switch r.Action {
	case ActionInject, ActionRevoke, ActionSessionOpen:
		// Only the actions that touch a user account name one.
		if !userPattern.MatchString(r.User) {
			return fmt.Errorf("invalid user: %q", r.User)
		}
	}
	switch r.Action {
	case ActionInject:
		if !idPattern.MatchString(r.ID) {
			return fmt.Errorf("invalid session id")
		}
		if strings.TrimSpace(r.Key) == "" {
			return errors.New("inject requires a key")
		}
	case ActionRevoke:
		if !idPattern.MatchString(r.ID) {
			return fmt.Errorf("invalid session id")
		}
	case ActionSessionOpen:
		spec := r.Spec()
		if err := spec.validate(); err != nil {
			return err
		}
	case ActionSessionResize:
		if !idPattern.MatchString(r.Ticket) {
			return fmt.Errorf("invalid session ticket")
		}
		if r.Cols < 0 || r.Rows < 0 || r.Cols > 4096 || r.Rows > 4096 {
			return fmt.Errorf("invalid terminal size")
		}
	case ActionSessionCount:
	default:
		return fmt.Errorf("unknown action: %q", r.Action)
	}
	return nil
}

// Spec is the session request in the shape the session host takes.
func (r Request) Spec() SessionSpec {
	return SessionSpec{
		Ticket:   strings.TrimSpace(r.Ticket),
		User:     strings.TrimSpace(r.User),
		DeviceID: strings.TrimSpace(r.Device),
		Token:    strings.TrimSpace(r.Token),
		Cols:     r.Cols,
		Rows:     r.Rows,
		Idle:     time.Duration(r.Idle) * time.Second,
	}
}

// Call sends one request to the helper and returns its response.
func Call(socketPath string, req Request, timeout time.Duration) (Response, error) {
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(context.Background(), "unix", socketPath)
	if err != nil {
		return Response{}, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return Response{}, err
	}
	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return Response{}, err
	}
	return resp, nil
}

// Inject asks the helper to install a key for a user.
func Inject(socketPath, user, key, id string) error {
	return expectOK(socketPath, Request{Action: ActionInject, User: user, Key: key, ID: id})
}

// Revoke asks the helper to remove a key for a user.
func Revoke(socketPath, user, id string) error {
	return expectOK(socketPath, Request{Action: ActionRevoke, User: user, ID: id})
}

// OpenSession asks the helper to host a terminal. It returns once the session is
// attached; the terminal then outlives this call, and outlives the agent.
func OpenSession(socketPath string, spec SessionSpec) error {
	req := Request{
		Action: ActionSessionOpen,
		User:   spec.User,
		Ticket: spec.Ticket,
		Device: spec.DeviceID,
		Token:  spec.Token,
		Cols:   spec.Cols,
		Rows:   spec.Rows,
		Idle:   int(spec.Idle / time.Second),
	}
	// Attaching waits for a dial and an ssh handshake, so it gets its own,
	// longer deadline than a key operation.
	resp, err := Call(socketPath, req, 30*time.Second)
	if err != nil {
		return err
	}
	if !resp.OK {
		if resp.Error == "" {
			return errors.New("helper rejected the session")
		}
		return errors.New(resp.Error)
	}
	return nil
}

// ResizeSession forwards a terminal size to the helper that hosts the session.
func ResizeSession(socketPath, ticket string, cols, rows int) error {
	return expectOK(socketPath, Request{Action: ActionSessionResize, Ticket: ticket, Cols: cols, Rows: rows})
}

// SessionCount reports how many terminals the helper is hosting.
func SessionCount(socketPath string) (int, error) {
	resp, err := Call(socketPath, Request{Action: ActionSessionCount}, 0)
	if err != nil {
		return 0, err
	}
	if !resp.OK {
		return 0, errors.New("helper rejected the request")
	}
	return resp.Sessions, nil
}

func expectOK(socketPath string, req Request) error {
	resp, err := Call(socketPath, req, 0)
	if err != nil {
		return err
	}
	if !resp.OK {
		if resp.Error == "" {
			return errors.New("helper rejected the request")
		}
		return errors.New(resp.Error)
	}
	return nil
}
