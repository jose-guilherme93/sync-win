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
)

// Request is one helper call.
type Request struct {
	Action Action `json:"action"`
	User   string `json:"user"`
	Key    string `json:"key,omitempty"`
	ID     string `json:"id"`
}

// Response is the helper's reply.
type Response struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

var (
	userPattern = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)
	idPattern   = regexp.MustCompile(`^[0-9a-f]{16,64}$`)
)

// ValidateReport reports why a request is unacceptable, or nil.
func (r Request) Validate() error {
	if !userPattern.MatchString(r.User) {
		return fmt.Errorf("invalid user: %q", r.User)
	}
	if !idPattern.MatchString(r.ID) {
		return fmt.Errorf("invalid session id")
	}
	switch r.Action {
	case ActionInject:
		if strings.TrimSpace(r.Key) == "" {
			return errors.New("inject requires a key")
		}
	case ActionRevoke:
	default:
		return fmt.Errorf("unknown action: %q", r.Action)
	}
	return nil
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
